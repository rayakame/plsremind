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
	"github.com/rayakame/plsremind/hub/internal/httpx"
	"github.com/rayakame/plsremind/hub/internal/secret"
)

var providerPattern = regexp.MustCompile(`^[a-z0-9_-]{1,32}$`)

type Connection struct {
	ID          uuid.UUID  `json:"id"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Provider    string     `json:"provider"`
	Enabled     bool       `json:"enabled"`
	Token       string     `json:"token,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	LastUsedAt  *time.Time `json:"last_used_at"`
}

func view(c sqlc.Connection) Connection {
	out := Connection{
		ID:          c.ID,
		Name:        c.Name,
		Description: c.Description,
		Provider:    c.Provider,
		Enabled:     c.Enabled,
		CreatedAt:   c.CreatedAt.UTC(),
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
}

func userView(row sqlc.ListUserConnectionsRow) UserConnection {
	enabled := row.Linked
	if row.Granted != nil {
		enabled = *row.Granted
	}
	return UserConnection{
		ID:          row.ID,
		Name:        row.Name,
		Description: row.Description,
		Provider:    row.Provider,
		Linked:      row.Linked,
		Enabled:     enabled,
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
		}
		if err := httpx.Body(c, &in); err != nil {
			return err
		}
		if in.Enabled == nil {
			return apierr.Field("enabled", "Is required.")
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
			err = q.SetConnectionGrant(c.Context(), sqlc.SetConnectionGrantParams{UserID: userID, ConnectionID: id, Enabled: *in.Enabled})
			if err != nil {
				return err
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
			return nil
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

func create(d *deps.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		var in struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Provider    string `json:"provider"`
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
		if len(fields) > 0 {
			return apierr.Validation(fields)
		}
		plain, hash := secret.NewToken(secret.PrefixConnection)
		row, err := d.Q().CreateConnection(c.Context(), sqlc.CreateConnectionParams{
			ID:          uuid.Must(uuid.NewV7()),
			Name:        in.Name,
			Description: in.Description,
			Provider:    in.Provider,
			TokenHash:   hash,
			CreatedAt:   d.Now(),
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
			Name        *string `json:"name"`
			Description *string `json:"description"`
			Enabled     *bool   `json:"enabled"`
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
			params := sqlc.UpdateConnectionParams{ID: id, Name: current.Name, Description: current.Description, Enabled: current.Enabled}
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
			if len(fields) > 0 {
				return apierr.Validation(fields)
			}
			updated, err = q.UpdateConnection(c.Context(), params)
			return err
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
		n, err := d.Q().DeleteConnection(c.Context(), id)
		if err != nil {
			return err
		}
		if n == 0 {
			return apierr.NotFound()
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
		row, err := d.Q().SetConnectionToken(c.Context(), sqlc.SetConnectionTokenParams{ID: id, TokenHash: hash})
		if db.IsNotFound(err) {
			return apierr.NotFound()
		}
		if err != nil {
			return err
		}
		out := view(row)
		out.Token = plain
		return c.JSON(out)
	}
}
