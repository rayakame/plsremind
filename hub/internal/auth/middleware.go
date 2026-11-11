package auth

import (
	"net/http"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"github.com/rayakame/plsremind/hub/internal/apierr"
	"github.com/rayakame/plsremind/hub/internal/db"
	"github.com/rayakame/plsremind/hub/internal/db/sqlc"
	"github.com/rayakame/plsremind/hub/internal/deps"
	"github.com/rayakame/plsremind/hub/internal/secret"
)

func errDisabled() error {
	return apierr.New(http.StatusForbidden, "account_disabled", "This account is disabled.")
}

func isWrite(method string) bool {
	return method != fiber.MethodGet && method != fiber.MethodHead && method != fiber.MethodOptions
}

// Middleware identifies the caller. Requests without valid credentials continue anonymously.
func Middleware(d *deps.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		var p Principal
		var ok bool
		var err error
		if header := c.Get(fiber.HeaderAuthorization); header != "" {
			p, err = fromBearer(c, d, header)
			ok = err == nil
		} else {
			p, ok, err = fromSession(c, d)
		}
		if err != nil {
			return err
		}
		if ok {
			c.Locals(principalKey, p)
		}
		return c.Next()
	}
}

const tokenTouchInterval = time.Minute

// fromBearer never falls back to anonymous: a caller that sends a token expects it to count.
func fromBearer(c fiber.Ctx, d *deps.Deps, header string) (Principal, error) {
	token, found := strings.CutPrefix(header, "Bearer ")
	if !found {
		return Principal{}, apierr.Unauthorized()
	}
	switch {
	case strings.HasPrefix(token, secret.PrefixPersonal):
		return fromPersonalToken(c, d, token)
	case strings.HasPrefix(token, secret.PrefixConnection):
		return fromConnectionToken(c, d, token)
	default:
		return Principal{}, apierr.Unauthorized()
	}
}

func fromPersonalToken(c fiber.Ctx, d *deps.Deps, token string) (Principal, error) {
	q := d.Q()
	row, err := q.GetPersonalTokenUser(c.Context(), secret.Hash(token))
	if db.IsNotFound(err) {
		return Principal{}, apierr.Unauthorized()
	}
	if err != nil {
		return Principal{}, err
	}
	if row.Disabled {
		return Principal{}, errDisabled()
	}
	now := d.Now()
	if row.LastUsedAt == nil || now.Sub(*row.LastUsedAt) >= tokenTouchInterval {
		if err := q.TouchPersonalToken(c.Context(), sqlc.TouchPersonalTokenParams{ID: row.TokenID, Now: &now}); err != nil {
			return Principal{}, err
		}
	}
	return Principal{Kind: KindPersonalToken, UserID: row.UserID, Role: row.Role, CredentialID: row.TokenID}, nil
}

func fromConnectionToken(c fiber.Ctx, d *deps.Deps, token string) (Principal, error) {
	q := d.Q()
	conn, err := q.GetConnectionByToken(c.Context(), secret.Hash(token))
	if db.IsNotFound(err) {
		return Principal{}, apierr.Unauthorized()
	}
	if err != nil {
		return Principal{}, err
	}
	now := d.Now()
	if conn.LastUsedAt == nil || now.Sub(*conn.LastUsedAt) >= tokenTouchInterval {
		if err := q.TouchConnection(c.Context(), sqlc.TouchConnectionParams{ID: conn.ID, Now: &now}); err != nil {
			return Principal{}, err
		}
	}
	return Principal{Kind: KindConnection, CredentialID: conn.ID, ConnectionProvider: conn.Provider}, nil
}

func fromSession(c fiber.Ctx, d *deps.Deps) (Principal, bool, error) {
	value := c.Cookies(SessionCookie)
	if value == "" {
		return Principal{}, false, nil
	}
	now := d.Now()
	q := d.Q()
	row, err := q.GetSessionUser(c.Context(), sqlc.GetSessionUserParams{TokenHash: secret.Hash(value), Now: now})
	if db.IsNotFound(err) {
		return Principal{}, false, nil
	}
	if err != nil {
		return Principal{}, false, err
	}
	if row.Disabled {
		return Principal{}, false, errDisabled()
	}
	if isWrite(c.Method()) && c.Get(fiber.HeaderOrigin) != d.Cfg.PublicURL {
		return Principal{}, false, apierr.New(http.StatusForbidden, "csrf_failed", "The request did not come from this site.")
	}
	if now.Sub(row.LastUsedAt) >= sessionRefresh {
		err := q.TouchSession(c.Context(), sqlc.TouchSessionParams{ID: row.SessionID, Now: now, ExpiresAt: now.Add(sessionLifetime)})
		if err != nil {
			return Principal{}, false, err
		}
	}
	return Principal{Kind: KindSession, UserID: row.UserID, Role: row.Role, CredentialID: row.SessionID}, true, nil
}

// Require rejects anonymous requests.
func Require() fiber.Handler {
	return func(c fiber.Ctx) error {
		if _, ok := From(c); !ok {
			return apierr.Unauthorized()
		}
		return c.Next()
	}
}

// RequireSession allows only dashboard sessions, for endpoints that change account security.
func RequireSession() fiber.Handler {
	return func(c fiber.Ctx) error {
		p, ok := From(c)
		if !ok {
			return apierr.Unauthorized()
		}
		if p.Kind != KindSession {
			return apierr.Forbidden()
		}
		return c.Next()
	}
}

func RequireAdmin() fiber.Handler {
	return func(c fiber.Ctx) error {
		p, ok := From(c)
		if !ok {
			return apierr.Unauthorized()
		}
		if p.Kind != KindSession || p.Role != "admin" {
			return apierr.Forbidden()
		}
		return c.Next()
	}
}

// connectionMayUse is the complete list of what a connection can do for a user:
// their lists and items, plus reading the profile and the reminder plans.
// Everything about the account itself stays out of reach.
func connectionMayUse(method, rest string) bool {
	for _, prefix := range []string{"/lists", "/items"} {
		if rest == prefix || strings.HasPrefix(rest, prefix+"/") {
			return true
		}
	}
	return method == fiber.MethodGet && (rest == "" || rest == "/reminder-plans")
}

// ResolveUser maps the :user path parameter to the user the request acts on.
// Sessions and personal tokens always act on "@me". A connection names the hub
// ID of a user who has granted it access.
func ResolveUser(d *deps.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		p, ok := From(c)
		if !ok {
			return apierr.Unauthorized()
		}
		param := c.Params("user")
		if p.Kind != KindConnection {
			if param != "@me" {
				return apierr.NotFound()
			}
			c.Locals(targetKey, p.UserID)
			return c.Next()
		}

		userID, err := uuid.Parse(param)
		if err != nil {
			return apierr.NotFound()
		}
		_, rest, _ := strings.Cut(c.Path(), "/users/"+param)
		if !connectionMayUse(c.Method(), strings.TrimSuffix(rest, "/")) {
			return apierr.Forbidden()
		}
		_, err = d.Q().GetGrantedUser(c.Context(), sqlc.GetGrantedUserParams{ConnectionID: p.CredentialID, UserID: userID})
		if db.IsNotFound(err) {
			return apierr.NotFound()
		}
		if err != nil {
			return err
		}
		c.Locals(targetKey, userID)
		return c.Next()
	}
}
