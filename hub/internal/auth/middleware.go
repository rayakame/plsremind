package auth

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"github.com/rayakame/plsremind/hub/internal/api"
	"github.com/rayakame/plsremind/hub/internal/core"
	"github.com/rayakame/plsremind/hub/internal/store"
	"github.com/rayakame/plsremind/hub/internal/store/sqlc"
)

func errDisabled() error {
	return api.NewError(http.StatusForbidden, "account_disabled", "This account is disabled.")
}

func errCSRF() error {
	return api.NewError(http.StatusForbidden, "csrf_failed", "The request did not come from this site.")
}

func isWrite(method string) bool {
	return method != fiber.MethodGet && method != fiber.MethodHead && method != fiber.MethodOptions
}

// Middleware identifies the caller. Requests without credentials continue anonymously.
// A request with bad credentials is not answered here: the failure is kept for Reject,
// so that the rate limiter in between still gets to count the attempt.
func Middleware(d *core.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		var p Principal
		var ok bool
		var err error
		if header := c.Get(fiber.HeaderAuthorization); header != "" {
			p, err = fromBearer(c, d, header)
			ok = err == nil
		} else {
			// Browsers name the page a request comes from. A write from another site is
			// refused even without a session, so a foreign page cannot log a visitor in.
			if origin := c.Get(fiber.HeaderOrigin); isWrite(c.Method()) && origin != "" && origin != d.Cfg.PublicURL {
				err = errCSRF()
			} else {
				p, ok, err = fromSession(c, d)
			}
		}
		switch {
		case err != nil:
			c.Locals(failureKey, err)
		case ok:
			c.Locals(principalKey, p)
		}
		return c.Next()
	}
}

// Failed reports whether the request carried credentials that were refused.
func Failed(c fiber.Ctx) bool {
	return c.Locals(failureKey) != nil
}

// Reject answers a request whose credentials Middleware refused.
func Reject() fiber.Handler {
	return func(c fiber.Ctx) error {
		if err, ok := c.Locals(failureKey).(error); ok {
			return err
		}
		return c.Next()
	}
}

const tokenTouchInterval = time.Minute

// fromBearer never falls back to anonymous: a caller that sends a token expects it to count.
func fromBearer(c fiber.Ctx, d *core.Deps, header string) (Principal, error) {
	token, found := strings.CutPrefix(header, "Bearer ")
	if !found {
		return Principal{}, api.Unauthorized()
	}
	return Authenticate(c.Context(), d, token)
}

// Authenticate resolves a personal or connection token to its principal.
func Authenticate(ctx context.Context, d *core.Deps, token string) (Principal, error) {
	switch {
	case strings.HasPrefix(token, core.PrefixPersonal):
		return fromPersonalToken(ctx, d, token)
	case strings.HasPrefix(token, core.PrefixConnection):
		return fromConnectionToken(ctx, d, token)
	default:
		return Principal{}, api.Unauthorized()
	}
}

func fromPersonalToken(ctx context.Context, d *core.Deps, token string) (Principal, error) {
	q := d.Q()
	row, err := q.GetPersonalTokenUser(ctx, core.HashToken(token))
	if store.IsNotFound(err) {
		return Principal{}, api.Unauthorized()
	}
	if err != nil {
		return Principal{}, err
	}
	if row.Disabled {
		return Principal{}, errDisabled()
	}
	now := d.Now()
	if row.LastUsedAt == nil || now.Sub(*row.LastUsedAt) >= tokenTouchInterval {
		if err := q.TouchPersonalToken(ctx, sqlc.TouchPersonalTokenParams{ID: row.TokenID, Now: &now}); err != nil {
			return Principal{}, err
		}
	}
	return Principal{Kind: KindPersonalToken, UserID: row.UserID, Role: row.Role, CredentialID: row.TokenID}, nil
}

func fromConnectionToken(ctx context.Context, d *core.Deps, token string) (Principal, error) {
	q := d.Q()
	conn, err := q.GetConnectionByToken(ctx, core.HashToken(token))
	if store.IsNotFound(err) {
		return Principal{}, api.Unauthorized()
	}
	if err != nil {
		return Principal{}, err
	}
	now := d.Now()
	if conn.LastUsedAt == nil || now.Sub(*conn.LastUsedAt) >= tokenTouchInterval {
		if err := q.TouchConnection(ctx, sqlc.TouchConnectionParams{ID: conn.ID, Now: &now}); err != nil {
			return Principal{}, err
		}
	}
	return Principal{Kind: KindConnection, CredentialID: conn.ID, ConnectionProvider: conn.Provider, Intents: conn.AllowedIntents}, nil
}

// SessionPrincipal resolves the value of a session cookie. It does not check
// where the request came from; callers that accept writes do that themselves.
func SessionPrincipal(ctx context.Context, d *core.Deps, value string) (Principal, bool, error) {
	if value == "" {
		return Principal{}, false, nil
	}
	now := d.Now()
	q := d.Q()
	row, err := q.GetSessionUser(ctx, sqlc.GetSessionUserParams{TokenHash: core.HashToken(value), Now: now})
	if store.IsNotFound(err) {
		return Principal{}, false, nil
	}
	if err != nil {
		return Principal{}, false, err
	}
	if row.Disabled {
		return Principal{}, false, errDisabled()
	}
	if now.Sub(row.LastUsedAt) >= sessionRefresh {
		err := q.TouchSession(ctx, sqlc.TouchSessionParams{ID: row.SessionID, Now: now, ExpiresAt: now.Add(sessionLifetime)})
		if err != nil {
			return Principal{}, false, err
		}
	}
	return Principal{Kind: KindSession, UserID: row.UserID, Role: row.Role, CredentialID: row.SessionID}, true, nil
}

func fromSession(c fiber.Ctx, d *core.Deps) (Principal, bool, error) {
	p, ok, err := SessionPrincipal(c.Context(), d, c.Cookies(SessionCookie))
	if err != nil || !ok {
		return Principal{}, false, err
	}
	if isWrite(c.Method()) && c.Get(fiber.HeaderOrigin) != d.Cfg.PublicURL {
		return Principal{}, false, errCSRF()
	}
	return p, true, nil
}

// Require rejects anonymous requests.
func Require() fiber.Handler {
	return func(c fiber.Ctx) error {
		if _, ok := From(c); !ok {
			return api.Unauthorized()
		}
		return c.Next()
	}
}

// RequireSession allows only dashboard sessions, for endpoints that change account security.
func RequireSession() fiber.Handler {
	return func(c fiber.Ctx) error {
		p, ok := From(c)
		if !ok {
			return api.Unauthorized()
		}
		if p.Kind != KindSession {
			return api.Forbidden()
		}
		return c.Next()
	}
}

func RequireAdmin() fiber.Handler {
	return func(c fiber.Ctx) error {
		p, ok := From(c)
		if !ok {
			return api.Unauthorized()
		}
		if p.Kind != KindSession || p.Role != "admin" {
			return api.Forbidden()
		}
		return c.Next()
	}
}

// connectionMayUse is the complete list of what a connection can do for a user:
// their lists and items, plus reading the profile and the reminder plans, each
// only if the admin allows the connection that kind of data. Everything about
// the account itself stays out of reach.
func connectionMayUse(p Principal, method, rest string) bool {
	for prefix, intent := range map[string]string{"/lists": "lists", "/items": "items"} {
		if rest == prefix || strings.HasPrefix(rest, prefix+"/") {
			return HasIntent(p, intent)
		}
	}
	if method != fiber.MethodGet {
		return false
	}
	switch rest {
	case "":
		return HasIntent(p, "users")
	case "/reminder-plans":
		return HasIntent(p, "plans")
	}
	return false
}

// ResolveUser maps the :user path parameter to the user the request acts on.
// Sessions and personal tokens always act on "@me". A connection names the hub
// ID of a user who has granted it access.
func ResolveUser(d *core.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		p, ok := From(c)
		if !ok {
			return api.Unauthorized()
		}
		param := c.Params("user")
		if p.Kind != KindConnection {
			if param != "@me" {
				return api.NotFound()
			}
			c.Locals(targetKey, p.UserID)
			return c.Next()
		}

		userID, err := uuid.Parse(param)
		if err != nil {
			return api.NotFound()
		}
		_, rest, found := strings.Cut(c.Path(), "/users/"+param)
		if !found || !connectionMayUse(p, c.Method(), strings.TrimSuffix(rest, "/")) {
			return api.Forbidden()
		}
		_, err = d.Q().GetGrantedUser(c.Context(), sqlc.GetGrantedUserParams{ConnectionID: p.CredentialID, UserID: userID})
		if store.IsNotFound(err) {
			return api.NotFound()
		}
		if err != nil {
			return err
		}
		c.Locals(targetKey, userID)
		return c.Next()
	}
}
