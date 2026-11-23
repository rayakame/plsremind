package access

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
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

var providerPattern = regexp.MustCompile(`^[a-z0-9_-]{1,32}$`)

type Connection struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Provider    string    `json:"provider"`
	Enabled     bool      `json:"enabled"`
	// AllowedIntents is the data the admin lets the connection have.
	AllowedIntents []string `json:"allowed_intents"`
	InstallURL     string   `json:"install_url"`
	// Reach is how many users the connection would remind now.
	Reach      int        `json:"reach"`
	Token      string     `json:"token,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
}

func viewConnection(c sqlc.Connection, reach int) Connection {
	out := Connection{
		ID:             c.ID,
		Name:           c.Name,
		Description:    c.Description,
		Provider:       c.Provider,
		Enabled:        c.Enabled,
		AllowedIntents: c.AllowedIntents,
		InstallURL:     c.InstallUrl,
		Reach:          reach,
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
	Intents    []string `json:"intents"`
	InstallURL string   `json:"install_url"`
	// LastDelivery is the newest delivery through this connection to the user that says how it went.
	LastDelivery *LastDelivery `json:"last_delivery"`
}

type LastDelivery struct {
	Status string    `json:"status"`
	Reason string    `json:"reason"`
	At     time.Time `json:"at"`
	Test   bool      `json:"test"`
}

func viewUserConnection(row sqlc.ListUserConnectionsRow) UserConnection {
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
		InstallURL:  row.InstallUrl,
	}
}

func userConnections(ctx context.Context, q *sqlc.Queries, userID uuid.UUID) ([]UserConnection, error) {
	rows, err := q.ListUserConnections(ctx, userID)
	if err != nil {
		return nil, err
	}
	last, err := q.LastDeliveries(ctx, userID)
	if err != nil {
		return nil, err
	}
	byConnection := make(map[uuid.UUID]*LastDelivery, len(last))
	for _, l := range last {
		byConnection[l.ConnectionID] = &LastDelivery{Status: l.Status, Reason: l.Reason, At: l.At.UTC()}
	}
	tests, err := q.LastConnectionTests(ctx, userID)
	if err != nil {
		return nil, err
	}
	for _, l := range tests {
		if known := byConnection[l.ConnectionID]; known == nil || l.At.After(known.At) {
			byConnection[l.ConnectionID] = &LastDelivery{Status: l.Status, Reason: l.Reason, At: l.At.UTC(), Test: true}
		}
	}
	out := make([]UserConnection, 0, len(rows))
	for _, row := range rows {
		view := viewUserConnection(row)
		view.LastDelivery = byConnection[row.ID]
		out = append(out, view)
	}
	return out, nil
}

func reachOf(ctx context.Context, q *sqlc.Queries, id *uuid.UUID) (map[uuid.UUID]int, error) {
	rows, err := q.ConnectionReach(ctx, id)
	if err != nil {
		return nil, err
	}
	out := make(map[uuid.UUID]int, len(rows))
	for _, row := range rows {
		out[row.ID] = int(row.Reach)
	}
	return out, nil
}

func RegisterConnections(root, user, admin fiber.Router, d *core.Deps) {
	root.Get("/connection/users/lookup", requireConnection(), lookupUser(d))

	user.Get("/connections", listUserConnections(d))
	user.Put("/connections/:id", setGrant(d))

	admin.Get("/connections", listConnections(d))
	admin.Post("/connections", createConnection(d))
	admin.Patch("/connections/:id", updateConnection(d))
	admin.Delete("/connections/:id", removeConnection(d))
	admin.Post("/connections/:id/token", rotateToken(d))
}

func requireConnection() fiber.Handler {
	return func(c fiber.Ctx) error {
		p, ok := auth.From(c)
		if !ok {
			return api.Unauthorized()
		}
		if p.Kind != auth.KindConnection {
			return api.Forbidden()
		}
		return c.Next()
	}
}

func lookupUser(d *core.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		externalID := c.Query("external_id")
		if externalID == "" {
			return api.Field("external_id", "Is required.")
		}
		p, _ := auth.From(c)
		user, err := d.Q().GetGrantedUserByIdentity(c.Context(), sqlc.GetGrantedUserByIdentityParams{
			ConnectionID: p.CredentialID,
			ExternalID:   externalID,
		})
		if store.IsNotFound(err) {
			return api.NotFound()
		}
		if err != nil {
			return err
		}
		return c.JSON(fiber.Map{"id": user.ID, "display_name": user.DisplayName, "timezone": user.Timezone})
	}
}

func listUserConnections(d *core.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		connections, err := userConnections(c.Context(), d.Q(), auth.Target(c))
		if err != nil {
			return err
		}
		return c.JSON(connections)
	}
}

func setGrant(d *core.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		id, err := api.ParamUUID(c, "id")
		if err != nil {
			return err
		}
		var in struct {
			Enabled *bool `json:"enabled"`
			Remind  *bool `json:"remind"`
		}
		if err := api.Body(c, &in); err != nil {
			return err
		}
		if in.Enabled == nil && in.Remind == nil {
			return api.Field("enabled", `Send "enabled", "remind" or both.`)
		}
		userID := auth.Target(c)
		var out UserConnection
		err = store.UserTx(c.Context(), d.Pool, userID, func(q *sqlc.Queries) error {
			conn, err := q.GetConnection(c.Context(), id)
			if store.IsNotFound(err) || (err == nil && !conn.Enabled) {
				return api.NotFound()
			}
			if err != nil {
				return err
			}
			if in.Enabled != nil {
				err = q.SetGrantEnabled(c.Context(), sqlc.SetGrantEnabledParams{UserID: userID, ConnectionID: id, Enabled: in.Enabled})
				if err != nil {
					return goneOr(err)
				}
			}
			if in.Remind != nil {
				err = q.SetGrantRemind(c.Context(), sqlc.SetGrantRemindParams{UserID: userID, ConnectionID: id, Remind: in.Remind})
				if err != nil {
					return goneOr(err)
				}
			}
			now := d.Now()
			if err := delivery.SyncConnection(c.Context(), q, now, id, userID); err != nil {
				return err
			}
			all, err := userConnections(c.Context(), q, userID)
			if err != nil {
				return err
			}
			found := false
			for _, one := range all {
				if one.ID == id {
					out = one
					found = true
				}
			}
			// An admin can disable the connection between the check above and this listing.
			if !found {
				return api.NotFound()
			}
			if err := events.Append(c.Context(), q, now, userID, auth.Actor(c), "connection.updated", out); err != nil {
				return err
			}
			return events.SendSignal(c.Context(), q, events.Signal{Type: events.SignalGrant, UserID: &userID, ConnectionID: &id})
		})
		if err != nil {
			return err
		}
		return c.JSON(out)
	}
}

// goneOr turns the failure of a grant whose connection was deleted meanwhile into "not found".
func goneOr(err error) error {
	if store.IsForeignKeyViolation(err) {
		return api.NotFound()
	}
	return err
}

func listConnections(d *core.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		rows, err := d.Q().ListConnections(c.Context())
		if err != nil {
			return err
		}
		reach, err := reachOf(c.Context(), d.Q(), nil)
		if err != nil {
			return err
		}
		connections := make([]Connection, 0, len(rows))
		for _, row := range rows {
			connections = append(connections, viewConnection(row, reach[row.ID]))
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

func checkInstallURL(fields map[string]string, raw string) {
	if raw == "" {
		return
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || utf8.RuneCountInString(raw) > 512 {
		fields["install_url"] = "Must be empty or an https address of at most 512 characters."
	}
}

func checkIntents(fields map[string]string, in []string) []string {
	intents, ok := auth.NormalizeIntents(in)
	if !ok {
		fields["allowed_intents"] = "Must be a non-empty list of: " + strings.Join(auth.Intents, ", ") + "; history needs items."
	}
	return intents
}

func createConnection(d *core.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		var in struct {
			Name           string    `json:"name"`
			Description    string    `json:"description"`
			Provider       string    `json:"provider"`
			AllowedIntents *[]string `json:"allowed_intents"`
			InstallURL     string    `json:"install_url"`
		}
		if err := api.Body(c, &in); err != nil {
			return err
		}
		in.Name = strings.TrimSpace(in.Name)
		in.Description = strings.TrimSpace(in.Description)
		in.InstallURL = strings.TrimSpace(in.InstallURL)
		fields := map[string]string{}
		checkText(fields, "name", in.Name, 1, 64)
		checkText(fields, "description", in.Description, 0, 500)
		checkInstallURL(fields, in.InstallURL)
		if !providerPattern.MatchString(in.Provider) {
			fields["provider"] = "Must be 1 to 32 characters: a-z, 0-9, underscore or dash."
		}
		intents := auth.Intents
		if in.AllowedIntents != nil {
			intents = checkIntents(fields, *in.AllowedIntents)
		}
		if len(fields) > 0 {
			return api.Validation(fields)
		}
		plain, hash := core.NewToken(core.PrefixConnection)
		var out Connection
		// The token is only ever shown here, so nothing is kept unless the whole answer can be made.
		err := store.Tx(c.Context(), d.Pool, func(q *sqlc.Queries) error {
			row, err := q.CreateConnection(c.Context(), sqlc.CreateConnectionParams{
				ID:             uuid.Must(uuid.NewV7()),
				Name:           in.Name,
				Description:    in.Description,
				Provider:       in.Provider,
				AllowedIntents: intents,
				InstallUrl:     in.InstallURL,
				TokenHash:      hash,
				CreatedAt:      d.Now(),
			})
			if err != nil {
				return err
			}
			reach, err := reachOf(c.Context(), q, &row.ID)
			if err != nil {
				return err
			}
			out = viewConnection(row, reach[row.ID])
			return nil
		})
		if err != nil {
			return err
		}
		out.Token = plain
		return c.Status(http.StatusCreated).JSON(out)
	}
}

func updateConnection(d *core.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		id, err := api.ParamUUID(c, "id")
		if err != nil {
			return err
		}
		var in struct {
			Name           *string   `json:"name"`
			Description    *string   `json:"description"`
			Enabled        *bool     `json:"enabled"`
			AllowedIntents *[]string `json:"allowed_intents"`
			InstallURL     *string   `json:"install_url"`
		}
		if err := api.Body(c, &in); err != nil {
			return err
		}
		var updated sqlc.Connection
		err = store.Tx(c.Context(), d.Pool, func(q *sqlc.Queries) error {
			current, err := q.GetConnection(c.Context(), id)
			if store.IsNotFound(err) {
				return api.NotFound()
			}
			if err != nil {
				return err
			}
			params := sqlc.UpdateConnectionParams{
				ID: id, Name: current.Name, Description: current.Description,
				Enabled: current.Enabled, AllowedIntents: current.AllowedIntents,
				InstallUrl: current.InstallUrl,
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
			if in.InstallURL != nil {
				params.InstallUrl = strings.TrimSpace(*in.InstallURL)
				checkInstallURL(fields, params.InstallUrl)
			}
			if len(fields) > 0 {
				return api.Validation(fields)
			}
			updated, err = q.UpdateConnection(c.Context(), params)
			if err != nil {
				return err
			}
			if !updated.Enabled || !slices.Contains(updated.AllowedIntents, "deliveries") {
				if err := delivery.CancelForConnection(c.Context(), q, d.Now(), id, nil); err != nil {
					return err
				}
			}
			return events.NotifyConnection(c.Context(), q, id)
		})
		if err != nil {
			return err
		}
		reach, err := reachOf(c.Context(), d.Q(), &id)
		if err != nil {
			return err
		}
		return c.JSON(viewConnection(updated, reach[id]))
	}
}

func removeConnection(d *core.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		id, err := api.ParamUUID(c, "id")
		if err != nil {
			return err
		}
		err = store.Tx(c.Context(), d.Pool, func(q *sqlc.Queries) error {
			if err := delivery.CancelForConnection(c.Context(), q, d.Now(), id, nil); err != nil {
				return err
			}
			n, err := q.DeleteConnection(c.Context(), id)
			if err != nil {
				return err
			}
			if n == 0 {
				return api.NotFound()
			}
			return events.NotifyConnection(c.Context(), q, id)
		})
		if err != nil {
			return err
		}
		return c.SendStatus(http.StatusNoContent)
	}
}

func rotateToken(d *core.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		id, err := api.ParamUUID(c, "id")
		if err != nil {
			return err
		}
		plain, hash := core.NewToken(core.PrefixConnection)
		var out Connection
		err = store.Tx(c.Context(), d.Pool, func(q *sqlc.Queries) error {
			row, err := q.SetConnectionToken(c.Context(), sqlc.SetConnectionTokenParams{ID: id, TokenHash: hash})
			if store.IsNotFound(err) {
				return api.NotFound()
			}
			if err != nil {
				return err
			}
			reach, err := reachOf(c.Context(), q, &id)
			if err != nil {
				return err
			}
			out = viewConnection(row, reach[id])
			return events.NotifyConnection(c.Context(), q, id)
		})
		if err != nil {
			return err
		}
		out.Token = plain
		return c.JSON(out)
	}
}
