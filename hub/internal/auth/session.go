package auth

import (
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"github.com/rayakame/plsremind/hub/internal/db/sqlc"
	"github.com/rayakame/plsremind/hub/internal/deps"
	"github.com/rayakame/plsremind/hub/internal/secret"
)

const (
	SessionCookie   = "plsr_session"
	sessionLifetime = 30 * 24 * time.Hour
	sessionRefresh  = time.Hour
)

func sessionCookie(d *deps.Deps, value string, expires time.Time) *fiber.Cookie {
	return &fiber.Cookie{
		Name:     SessionCookie,
		Value:    value,
		Path:     "/",
		Expires:  expires,
		HTTPOnly: true,
		Secure:   strings.HasPrefix(d.Cfg.PublicURL, "https://"),
		SameSite: fiber.CookieSameSiteLaxMode,
	}
}

// StartSession creates a session for the user and sets its cookie.
func StartSession(c fiber.Ctx, d *deps.Deps, q *sqlc.Queries, userID uuid.UUID) error {
	now := d.Now()
	plain, hash := secret.NewToken(secret.PrefixSession)
	expires := now.Add(sessionLifetime)
	err := q.CreateSession(c.Context(), sqlc.CreateSessionParams{
		ID:        uuid.Must(uuid.NewV7()),
		UserID:    userID,
		TokenHash: hash,
		Now:       now,
		ExpiresAt: expires,
	})
	if err != nil {
		return err
	}
	c.Cookie(sessionCookie(d, plain, expires))
	return nil
}

// EndSession deletes the current session, if any, and clears the cookie.
func EndSession(c fiber.Ctx, d *deps.Deps) error {
	if value := c.Cookies(SessionCookie); value != "" {
		if err := d.Q().DeleteSessionByHash(c.Context(), secret.Hash(value)); err != nil {
			return err
		}
	}
	c.Cookie(sessionCookie(d, "", time.Unix(0, 0)))
	return nil
}
