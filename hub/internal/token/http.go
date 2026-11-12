// Package token manages personal API tokens.
package token

import (
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"github.com/rayakame/plsremind/hub/internal/apierr"
	"github.com/rayakame/plsremind/hub/internal/auth"
	"github.com/rayakame/plsremind/hub/internal/db"
	"github.com/rayakame/plsremind/hub/internal/db/sqlc"
	"github.com/rayakame/plsremind/hub/internal/delivery"
	"github.com/rayakame/plsremind/hub/internal/deps"
	"github.com/rayakame/plsremind/hub/internal/httpx"
	"github.com/rayakame/plsremind/hub/internal/secret"
	"github.com/rayakame/plsremind/hub/internal/signal"
)

const maxPerUser = 25

type Token struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
	// Remind marks the token as a way to be reminded: the hub then holds deliveries for it.
	Remind     bool       `json:"remind"`
	Token      string     `json:"token,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
}

func view(t sqlc.PersonalToken) Token {
	out := Token{ID: t.ID, Name: t.Name, Remind: t.Remind, CreatedAt: t.CreatedAt.UTC()}
	if t.LastUsedAt != nil {
		used := t.LastUsedAt.UTC()
		out.LastUsedAt = &used
	}
	return out
}

func Register(user fiber.Router, d *deps.Deps) {
	tokens := user.Group("/tokens", auth.RequireSession())
	tokens.Get("", list(d))
	tokens.Post("", create(d))
	tokens.Patch("/:id", update(d))
	tokens.Delete("/:id", remove(d))
}

func list(d *deps.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		rows, err := d.Q().ListPersonalTokens(c.Context(), auth.Target(c))
		if err != nil {
			return err
		}
		tokens := make([]Token, 0, len(rows))
		for _, row := range rows {
			tokens = append(tokens, view(row))
		}
		return c.JSON(tokens)
	}
}

func create(d *deps.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		var in struct {
			Name   string `json:"name"`
			Remind bool   `json:"remind"`
		}
		if err := httpx.Body(c, &in); err != nil {
			return err
		}
		name, err := cleanName(in.Name)
		if err != nil {
			return err
		}
		userID := auth.Target(c)
		plain, hash := secret.NewToken(secret.PrefixPersonal)
		var created sqlc.PersonalToken
		err = db.Tx(c.Context(), d.Pool, func(q *sqlc.Queries) error {
			// Locking the user row keeps two concurrent creates from both passing the limit.
			if _, err := q.LockUser(c.Context(), userID); err != nil {
				return err
			}
			count, err := q.CountPersonalTokens(c.Context(), userID)
			if err != nil {
				return err
			}
			if count >= maxPerUser {
				return apierr.Conflict("token_limit", "You have reached the maximum number of tokens.")
			}
			created, err = q.CreatePersonalToken(c.Context(), sqlc.CreatePersonalTokenParams{
				ID:        uuid.Must(uuid.NewV7()),
				UserID:    userID,
				Name:      name,
				Remind:    in.Remind,
				TokenHash: hash,
				CreatedAt: d.Now(),
			})
			return err
		})
		if err != nil {
			return err
		}
		out := view(created)
		out.Token = plain
		return c.Status(http.StatusCreated).JSON(out)
	}
}

func cleanName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if n := utf8.RuneCountInString(name); n < 1 || n > 64 {
		return "", apierr.Field("name", "Must be 1 to 64 characters.")
	}
	return name, nil
}

func update(d *deps.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		id, err := httpx.ParamUUID(c, "id")
		if err != nil {
			return err
		}
		var in struct {
			Name   *string `json:"name"`
			Remind *bool   `json:"remind"`
		}
		if err := httpx.Body(c, &in); err != nil {
			return err
		}
		if in.Name == nil && in.Remind == nil {
			return apierr.Field("name", `Send "name", "remind" or both.`)
		}
		var updated sqlc.PersonalToken
		err = db.Tx(c.Context(), d.Pool, func(q *sqlc.Queries) error {
			current, err := q.GetPersonalToken(c.Context(), sqlc.GetPersonalTokenParams{ID: id, UserID: auth.Target(c)})
			if db.IsNotFound(err) {
				return apierr.NotFound()
			}
			if err != nil {
				return err
			}
			params := sqlc.UpdatePersonalTokenParams{ID: id, Name: current.Name, Remind: current.Remind}
			if in.Name != nil {
				if params.Name, err = cleanName(*in.Name); err != nil {
					return err
				}
			}
			if in.Remind != nil {
				params.Remind = *in.Remind
			}
			updated, err = q.UpdatePersonalToken(c.Context(), params)
			if err != nil {
				return err
			}
			if !updated.Remind {
				if err := delivery.CancelForToken(c.Context(), q, d.Now(), id); err != nil {
					return err
				}
			}
			return signal.ForUser(c.Context(), q, signal.User, updated.UserID)
		})
		if err != nil {
			return err
		}
		return c.JSON(view(updated))
	}
}

func remove(d *deps.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		id, err := httpx.ParamUUID(c, "id")
		if err != nil {
			return err
		}
		userID := auth.Target(c)
		err = db.Tx(c.Context(), d.Pool, func(q *sqlc.Queries) error {
			if err := delivery.CancelForToken(c.Context(), q, d.Now(), id); err != nil {
				return err
			}
			n, err := q.DeletePersonalToken(c.Context(), sqlc.DeletePersonalTokenParams{ID: id, UserID: userID})
			if err != nil {
				return err
			}
			if n == 0 {
				return apierr.NotFound()
			}
			return signal.ForUser(c.Context(), q, signal.User, userID)
		})
		if err != nil {
			return err
		}
		return c.SendStatus(http.StatusNoContent)
	}
}
