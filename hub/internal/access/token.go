// Package access manages the credentials that act for users: personal API tokens
// and the connections an admin installs for everyone.
package access

import (
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"github.com/rayakame/plsremind/hub/internal/api"
	"github.com/rayakame/plsremind/hub/internal/auth"
	"github.com/rayakame/plsremind/hub/internal/core"
	"github.com/rayakame/plsremind/hub/internal/delivery"
	"github.com/rayakame/plsremind/hub/internal/events"
	"github.com/rayakame/plsremind/hub/internal/store"
	"github.com/rayakame/plsremind/hub/internal/store/sqlc"
)

const maxTokensPerUser = 25

type Token struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
	// Remind marks the token as a way to be reminded: the hub then holds deliveries for it.
	Remind     bool       `json:"remind"`
	Token      string     `json:"token,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
}

func viewToken(t sqlc.PersonalToken) Token {
	out := Token{ID: t.ID, Name: t.Name, Remind: t.Remind, CreatedAt: t.CreatedAt.UTC()}
	if t.LastUsedAt != nil {
		used := t.LastUsedAt.UTC()
		out.LastUsedAt = &used
	}
	return out
}

func RegisterTokens(user fiber.Router, d *core.Deps) {
	tokens := user.Group("/tokens", auth.RequireSession())
	tokens.Get("", listTokens(d))
	tokens.Post("", createToken(d))
	tokens.Patch("/:id", updateToken(d))
	tokens.Delete("/:id", removeToken(d))
}

func listTokens(d *core.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		rows, err := d.Q().ListPersonalTokens(c.Context(), auth.Target(c))
		if err != nil {
			return err
		}
		tokens := make([]Token, 0, len(rows))
		for _, row := range rows {
			tokens = append(tokens, viewToken(row))
		}
		return c.JSON(tokens)
	}
}

func createToken(d *core.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		var in struct {
			Name   string `json:"name"`
			Remind bool   `json:"remind"`
		}
		if err := api.Body(c, &in); err != nil {
			return err
		}
		name, err := cleanTokenName(in.Name)
		if err != nil {
			return err
		}
		userID := auth.Target(c)
		plain, hash := core.NewToken(core.PrefixPersonal)
		var created sqlc.PersonalToken
		err = store.UserTx(c.Context(), d.Pool, userID, func(q *sqlc.Queries) error {
			// Locking the user row keeps two concurrent creates from both passing the limit.
			if _, err := q.LockUser(c.Context(), userID); err != nil {
				return err
			}
			count, err := q.CountPersonalTokens(c.Context(), userID)
			if err != nil {
				return err
			}
			if count >= maxTokensPerUser {
				return api.Conflict("token_limit", "You have reached the maximum number of tokens.")
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
		out := viewToken(created)
		out.Token = plain
		return c.Status(http.StatusCreated).JSON(out)
	}
}

func cleanTokenName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if n := utf8.RuneCountInString(name); n < 1 || n > 64 {
		return "", api.Field("name", "Must be 1 to 64 characters.")
	}
	return name, nil
}

func updateToken(d *core.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		id, err := api.ParamUUID(c, "id")
		if err != nil {
			return err
		}
		var in struct {
			Name   *string `json:"name"`
			Remind *bool   `json:"remind"`
		}
		if err := api.Body(c, &in); err != nil {
			return err
		}
		if in.Name == nil && in.Remind == nil {
			return api.Field("name", `Send "name", "remind" or both.`)
		}
		var updated sqlc.PersonalToken
		err = store.UserTx(c.Context(), d.Pool, auth.Target(c), func(q *sqlc.Queries) error {
			current, err := q.GetPersonalToken(c.Context(), sqlc.GetPersonalTokenParams{ID: id, UserID: auth.Target(c)})
			if store.IsNotFound(err) {
				return api.NotFound()
			}
			if err != nil {
				return err
			}
			params := sqlc.UpdatePersonalTokenParams{ID: id, Name: current.Name, Remind: current.Remind}
			if in.Name != nil {
				if params.Name, err = cleanTokenName(*in.Name); err != nil {
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
			return events.NotifyUser(c.Context(), q, events.SignalUser, updated.UserID)
		})
		if err != nil {
			return err
		}
		return c.JSON(viewToken(updated))
	}
}

func removeToken(d *core.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		id, err := api.ParamUUID(c, "id")
		if err != nil {
			return err
		}
		userID := auth.Target(c)
		err = store.UserTx(c.Context(), d.Pool, userID, func(q *sqlc.Queries) error {
			if err := delivery.CancelForToken(c.Context(), q, d.Now(), id); err != nil {
				return err
			}
			n, err := q.DeletePersonalToken(c.Context(), sqlc.DeletePersonalTokenParams{ID: id, UserID: userID})
			if err != nil {
				return err
			}
			if n == 0 {
				return api.NotFound()
			}
			return events.NotifyUser(c.Context(), q, events.SignalUser, userID)
		})
		if err != nil {
			return err
		}
		return c.SendStatus(http.StatusNoContent)
	}
}
