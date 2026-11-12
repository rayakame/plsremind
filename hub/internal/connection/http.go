// Package connection manages the integrations an admin installs for all users,
// such as the Discord bot, and each user's say over them.
package connection

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"github.com/rayakame/plsremind/hub/internal/apierr"
	"github.com/rayakame/plsremind/hub/internal/auth"
	"github.com/rayakame/plsremind/hub/internal/db"
	"github.com/rayakame/plsremind/hub/internal/db/sqlc"
	"github.com/rayakame/plsremind/hub/internal/deps"
	"github.com/rayakame/plsremind/hub/internal/eventlog"
	"github.com/rayakame/plsremind/hub/internal/httpx"
	"github.com/rayakame/plsremind/hub/internal/secret"
	"github.com/rayakame/plsremind/hub/internal/signal"
)

var providerPattern = regexp.MustCompile(`^[a-z0-9_-]{1,32}$`)

type Connection struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Provider    string    `json:"provider"`
	Enabled     bool      `json:"enabled"`
	// AllowedIntents is the data the admin lets the connection have.
	AllowedIntents []string   `json:"allowed_intents"`
	Token          string     `json:"token,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	LastUsedAt     *time.Time `json:"last_used_at"`
}

func view(c sqlc.Connection) Connection {
	out := Connection{
		ID:             c.ID,
		Name:           c.Name,
		Description:    c.Description,
		Provider:       c.Provider,
		Enabled:        c.Enabled,
		AllowedIntents: c.AllowedIntents,
		CreatedAt:      c.CreatedAt.UTC(),
	}
	if c.LastUsedAt != nil {
		used := c.LastUsedAt.UTC()
		out.LastUsedAt = &used
	}
	return out
}

// UserConnection is a connection as one user sees it.
type UserConnection struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Provider    string    `json:"provider"`
	Linked      bool      `json:"linked"`
	Enabled     bool      `json:"enabled"`
	// Remind says whether the user is reminded through this connection.
	Remind bool `json:"remind"`
	// Intents is the data the connection may have, so users can see what they share.
	Intents []string `json:"intents"`
}

func userView(row sqlc.ListUserConnectionsRow) UserConnection {
	enabled := row.Linked
	if row.Granted != nil {
		enabled = *row.Granted
	}
	remind := true
	if row.RemindChoice != nil {
		remind = *row.RemindChoice
	}
	return UserConnection{
		ID:          row.ID,
		Name:        row.Name,
		Description: row.Description,
		Provider:    row.Provider,
		Linked:      row.Linked,
		Enabled:     enabled,
		Remind:      remind,
		Intents:     row.AllowedIntents,
	}
}

func Register(api, user, admin fiber.Router, d *deps.Deps) {
	api.Get("/connection/users/lookup", requireConnection(), lookup(d))

	user.Get("/connections", listForUser(d))
	user.Put("/connections/:id", setGrant(d))

	admin.Get("/connections", index(d))
	admin.Post("/connections", create(d))
	admin.Patch("/connections/:id", update(d))
	admin.Delete("/connections/:id", remove(d))
	admin.Post("/connections/:id/token", rotate(d))
}

func requireConnection() fiber.Handler {
	return func(c fiber.Ctx) error {
		p, ok := auth.From(c)
		if !ok {
			return apierr.Unauthorized()
		}
		if p.Kind != auth.KindConnection {
			return apierr.Forbidden()
		}
		return c.Next()
	}
}

func lookup(d *deps.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		externalID := c.Query("external_id")
		if externalID == "" {
			return apierr.Field("external_id", "Is required.")
		}
		p, _ := auth.From(c)
		user, err := d.Q().GetGrantedUserByIdentity(c.Context(), sqlc.GetGrantedUserByIdentityParams{
			ConnectionID: p.CredentialID,
			ExternalID:   externalID,
		})
		if db.IsNotFound(err) {
			return apierr.NotFound()
		}
		if err != nil {
			return err
		}
		return c.JSON(fiber.Map{"id": user.ID, "display_name": user.DisplayName, "timezone": user.Timezone})
	}
}

func listForUser(d *deps.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		rows, err := d.Q().ListUserConnections(c.Context(), auth.Target(c))
		if err != nil {
			return err
		}
		connections := make([]UserConnection, 0, len(rows))
		for _, row := range rows {
			connections = append(connections, userView(row))
		}
		return c.JSON(connections)
	}
}

func setGrant(d *deps.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		id, err := httpx.ParamUUID(c, "id")
		if err != nil {
			return err
		}
		var in struct {
			Enabled *bool `json:"enabled"`
			Remind  *bool `json:"remind"`
		}
		if err := httpx.Body(c, &in); err != nil {
			return err
		}
		if in.Enabled == nil && in.Remind == nil {
			return apierr.Field("enabled", `Send "enabled", "remind" or both.`)
		}
		userID := auth.Target(c)
		var out UserConnection
		err = db.Tx(c.Context(), d.Pool, func(q *sqlc.Queries) error {
			conn, err := q.GetConnection(c.Context(), id)
			if db.IsNotFound(err) || (err == nil && !conn.Enabled) {
				return apierr.NotFound()
			}
			if err != nil {
				return err
			}
			if in.Enabled != nil {
				err = q.SetGrantEnabled(c.Context(), sqlc.SetGrantEnabledParams{UserID: userID, ConnectionID: id, Enabled: in.Enabled})
				if err != nil {
					return err
				}
			}
			if in.Remind != nil {
				err = q.SetGrantRemind(c.Context(), sqlc.SetGrantRemindParams{UserID: userID, ConnectionID: id, Remind: in.Remind})
				if err != nil {
					return err
				}
			}
			rows, err := q.ListUserConnections(c.Context(), userID)
			if err != nil {
				return err
			}
			for _, row := range rows {
				if row.ID == id {
					out = userView(row)
				}
			}
			p, _ := auth.From(c)
			if err := eventlog.Append(c.Context(), q, d.Now(), userID, eventlog.ActorFrom(p), "connection.updated", out); err != nil {
				return err
			}
			return signal.Send(c.Context(), q, signal.Signal{Type: signal.Grant, UserID: &userID, ConnectionID: &id})
		})
		if err != nil {
			return err
		}
		return c.JSON(out)
	}
}

func index(d *deps.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		rows, err := d.Q().ListConnections(c.Context())
		if err != nil {
			return err
		}
		connections := make([]Connection, 0, len(rows))
		for _, row := range rows {
			connections = append(connections, view(row))
		}
		return c.JSON(connections)
	}
}

func checkText(fields map[string]string, name, value string, min, max int) {
	n := utf8.RuneCountInString(value)
	switch {
	case n >= min && n <= max:
	case min == 0:
		fields[name] = fmt.Sprintf("Must be at most %d characters.", max)
	default:
		fields[name] = fmt.Sprintf("Must be %d to %d characters.", min, max)
	}
}

func checkIntents(fields map[string]string, in []string) []string {
	intents, ok := auth.NormalizeIntents(in)
	if !ok {
		fields["allowed_intents"] = "Must be a non-empty list of: " + strings.Join(auth.Intents, ", ") + "."
	}
	return intents
}

func create(d *deps.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		var in struct {
			Name           string    `json:"name"`
			Description    string    `json:"description"`
			Provider       string    `json:"provider"`
			AllowedIntents *[]string `json:"allowed_intents"`
		}
		if err := httpx.Body(c, &in); err != nil {
			return err
		}
		in.Name = strings.TrimSpace(in.Name)
		in.Description = strings.TrimSpace(in.Description)
		fields := map[string]string{}
		checkText(fields, "name", in.Name, 1, 64)
		checkText(fields, "description", in.Description, 0, 500)
		if !providerPattern.MatchString(in.Provider) {
			fields["provider"] = "Must be 1 to 32 characters: a-z, 0-9, underscore or dash."
		}
		intents := auth.Intents
		if in.AllowedIntents != nil {
			intents = checkIntents(fields, *in.AllowedIntents)
		}
		if len(fields) > 0 {
			return apierr.Validation(fields)
		}
		plain, hash := secret.NewToken(secret.PrefixConnection)
		row, err := d.Q().CreateConnection(c.Context(), sqlc.CreateConnectionParams{
			ID:             uuid.Must(uuid.NewV7()),
			Name:           in.Name,
			Description:    in.Description,
			Provider:       in.Provider,
			AllowedIntents: intents,
			TokenHash:      hash,
			CreatedAt:      d.Now(),
		})
		if err != nil {
			return err
		}
		out := view(row)
		out.Token = plain
		return c.Status(http.StatusCreated).JSON(out)
	}
}

func update(d *deps.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		id, err := httpx.ParamUUID(c, "id")
		if err != nil {
			return err
		}
		var in struct {
			Name           *string   `json:"name"`
			Description    *string   `json:"description"`
			Enabled        *bool     `json:"enabled"`
			AllowedIntents *[]string `json:"allowed_intents"`
		}
		if err := httpx.Body(c, &in); err != nil {
			return err
		}
		var updated sqlc.Connection
		err = db.Tx(c.Context(), d.Pool, func(q *sqlc.Queries) error {
			current, err := q.GetConnection(c.Context(), id)
			if db.IsNotFound(err) {
				return apierr.NotFound()
			}
			if err != nil {
				return err
			}
			params := sqlc.UpdateConnectionParams{
				ID: id, Name: current.Name, Description: current.Description,
				Enabled: current.Enabled, AllowedIntents: current.AllowedIntents,
			}
			fields := map[string]string{}
			if in.Name != nil {
				params.Name = strings.TrimSpace(*in.Name)
				checkText(fields, "name", params.Name, 1, 64)
			}
			if in.Description != nil {
				params.Description = strings.TrimSpace(*in.Description)
				checkText(fields, "description", params.Description, 0, 500)
			}
			if in.Enabled != nil {
				params.Enabled = *in.Enabled
			}
			if in.AllowedIntents != nil {
				params.AllowedIntents = checkIntents(fields, *in.AllowedIntents)
			}
			if len(fields) > 0 {
				return apierr.Validation(fields)
			}
			updated, err = q.UpdateConnection(c.Context(), params)
			if err != nil {
				return err
			}
			return signal.ForConnection(c.Context(), q, id)
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
		err = db.Tx(c.Context(), d.Pool, func(q *sqlc.Queries) error {
			n, err := q.DeleteConnection(c.Context(), id)
			if err != nil {
				return err
			}
			if n == 0 {
				return apierr.NotFound()
			}
			return signal.ForConnection(c.Context(), q, id)
		})
		if err != nil {
			return err
		}
		return c.SendStatus(http.StatusNoContent)
	}
}

func rotate(d *deps.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		id, err := httpx.ParamUUID(c, "id")
		if err != nil {
			return err
		}
		plain, hash := secret.NewToken(secret.PrefixConnection)
		var row sqlc.Connection
		err = db.Tx(c.Context(), d.Pool, func(q *sqlc.Queries) error {
			row, err = q.SetConnectionToken(c.Context(), sqlc.SetConnectionTokenParams{ID: id, TokenHash: hash})
			if db.IsNotFound(err) {
				return apierr.NotFound()
			}
			if err != nil {
				return err
			}
			return signal.ForConnection(c.Context(), q, id)
		})
		if err != nil {
			return err
		}
		out := view(row)
		out.Token = plain
		return c.JSON(out)
	}
}
