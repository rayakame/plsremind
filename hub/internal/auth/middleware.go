package auth

import (
	"net/http"

	"github.com/gofiber/fiber/v3"

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
		p, ok, err := fromSession(c, d)
		if err != nil {
			return err
		}
		if ok {
			c.Locals(principalKey, p)
		}
		return c.Next()
	}
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

// ResolveUser maps the :user path parameter to the user the request acts on.
func ResolveUser(d *deps.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		p, ok := From(c)
		if !ok {
			return apierr.Unauthorized()
		}
		if c.Params("user") != "@me" {
			return apierr.NotFound()
		}
		c.Locals(targetKey, p.UserID)
		return c.Next()
	}
}
