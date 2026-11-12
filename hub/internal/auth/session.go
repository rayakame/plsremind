package auth

import (
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"github.com/rayakame/plsremind/hub/internal/core"
	"github.com/rayakame/plsremind/hub/internal/events"
	"github.com/rayakame/plsremind/hub/internal/store/sqlc"
)

const (
	SessionCookie   = "plsr_session"
	sessionLifetime = 30 * 24 * time.Hour
	sessionRefresh  = time.Hour
)

func sessionCookie(d *core.Deps, value string, expires time.Time) *fiber.Cookie {
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
func StartSession(c fiber.Ctx, d *core.Deps, q *sqlc.Queries, userID uuid.UUID) error {
	now := d.Now()
	plain, hash := core.NewToken(core.PrefixSession)
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
func EndSession(c fiber.Ctx, d *core.Deps) error {
	if value := c.Cookies(SessionCookie); value != "" {
		q := d.Q()
		if err := q.DeleteSessionByHash(c.Context(), core.HashToken(value)); err != nil {
			return err
		}
		// A gateway connection opened with this session must not outlive it.
		if p, ok := From(c); ok && p.Kind == KindSession {
			if err := events.NotifyUser(c.Context(), q, events.SignalUser, p.UserID); err != nil {
				return err
			}
		}
	}
	c.Cookie(sessionCookie(d, "", time.Unix(0, 0)))
	return nil
}
