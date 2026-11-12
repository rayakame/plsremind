package todo

import (
	"net/http"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"github.com/rayakame/plsremind/hub/internal/api"
	"github.com/rayakame/plsremind/hub/internal/auth"
	"github.com/rayakame/plsremind/hub/internal/core"
	"github.com/rayakame/plsremind/hub/internal/events"
	"github.com/rayakame/plsremind/hub/internal/store"
	"github.com/rayakame/plsremind/hub/internal/store/sqlc"
)

func registerSubitems(user fiber.Router, d *core.Deps) {
	subitems := user.Group("/items/:id/subitems")
	subitems.Post("", createSubitem(d))
	subitems.Put("/order", reorderSubitems(d))
	subitems.Patch("/:sid", updateSubitem(d))
	subitems.Delete("/:sid", removeSubitem(d))
}

func subitemTitleError() error {
	return api.Field("title", "Must be 1 to 200 characters.")
}

// change runs fn on the locked item and records the resulting event.
func changeSubitems(c fiber.Ctx, d *core.Deps, fn func(q *sqlc.Queries, itemID uuid.UUID, log func(typ string, payload any) error) error) error {
	itemID, err := api.ParamUUID(c, "id")
	if err != nil {
		return err
	}
	userID := auth.Target(c)
	p, _ := auth.From(c)
	return store.UserTx(c.Context(), d.Pool, userID, func(q *sqlc.Queries) error {
		if _, err := LockItem(c.Context(), q, userID, itemID); err != nil {
			return err
		}
		now := d.Now()
		if err := TouchItem(c.Context(), q, now, itemID); err != nil {
			return err
		}
		actor := auth.ActorFrom(p)
		err := fn(q, itemID, func(typ string, payload any) error {
			return events.Append(c.Context(), q, now, userID, actor, typ, payload)
		})
		if err != nil {
			return err
		}
		// The item changed too: its checklist and its updated_at. Sending it whole
		// lets a client that keeps items in memory simply replace its copy.
		whole, err := LoadItem(c.Context(), q, userID, itemID)
		if err != nil {
			return err
		}
		return events.Append(c.Context(), q, now, userID, actor, "item.updated", whole)
	})
}

func findSubitem(c fiber.Ctx, q *sqlc.Queries, itemID uuid.UUID) (sqlc.Subitem, error) {
	id, err := api.ParamUUID(c, "sid")
	if err != nil {
		return sqlc.Subitem{}, err
	}
	row, err := q.GetSubitem(c.Context(), sqlc.GetSubitemParams{ID: id, ItemID: itemID})
	if store.IsNotFound(err) {
		return sqlc.Subitem{}, api.NotFound()
	}
	return row, err
}

func createSubitem(d *core.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		var in struct {
			Title string `json:"title"`
		}
		if err := api.Body(c, &in); err != nil {
			return err
		}
		title, ok := CleanSubitemTitle(in.Title)
		if !ok {
			return subitemTitleError()
		}
		var created Subitem
		err := changeSubitems(c, d, func(q *sqlc.Queries, itemID uuid.UUID, log func(string, any) error) error {
			existing, err := q.ListSubitems(c.Context(), []uuid.UUID{itemID})
			if err != nil {
				return err
			}
			if len(existing) >= MaxSubitems {
				return api.Conflict("subitem_limit", "An item can have at most 50 subitems.")
			}
			position := int32(0)
			for _, s := range existing {
				position = max(position, s.Position+1)
			}
			row, err := q.CreateSubitem(c.Context(), sqlc.CreateSubitemParams{
				ID:       uuid.Must(uuid.NewV7()),
				ItemID:   itemID,
				Title:    title,
				Position: position,
				Now:      d.Now(),
			})
			if err != nil {
				return err
			}
			created = ViewSubitem(row)
			return log("subitem.created", created)
		})
		if err != nil {
			return err
		}
		return c.Status(http.StatusCreated).JSON(created)
	}
}

func updateSubitem(d *core.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		var in struct {
			Title *string `json:"title"`
			Done  *bool   `json:"done"`
		}
		if err := api.Body(c, &in); err != nil {
			return err
		}
		var updated Subitem
		err := changeSubitems(c, d, func(q *sqlc.Queries, itemID uuid.UUID, log func(string, any) error) error {
			row, err := findSubitem(c, q, itemID)
			if err != nil {
				return err
			}
			params := sqlc.UpdateSubitemParams{ID: row.ID, Title: row.Title, Done: row.Done, Now: d.Now()}
			if in.Title != nil {
				title, ok := CleanSubitemTitle(*in.Title)
				if !ok {
					return subitemTitleError()
				}
				params.Title = title
			}
			if in.Done != nil {
				params.Done = *in.Done
			}
			row, err = q.UpdateSubitem(c.Context(), params)
			if err != nil {
				return err
			}
			updated = ViewSubitem(row)
			return log("subitem.updated", updated)
		})
		if err != nil {
			return err
		}
		return c.JSON(updated)
	}
}

func removeSubitem(d *core.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		err := changeSubitems(c, d, func(q *sqlc.Queries, itemID uuid.UUID, log func(string, any) error) error {
			row, err := findSubitem(c, q, itemID)
			if err != nil {
				return err
			}
			if err := q.DeleteSubitem(c.Context(), row.ID); err != nil {
				return err
			}
			return log("subitem.deleted", events.Deleted{ID: row.ID, ItemID: &itemID})
		})
		if err != nil {
			return err
		}
		return c.SendStatus(http.StatusNoContent)
	}
}

func reorderSubitems(d *core.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		var in struct {
			IDs []uuid.UUID `json:"ids"`
		}
		if err := api.Body(c, &in); err != nil {
			return err
		}
		invalid := api.Field("ids", "Must contain each subitem of the item exactly once.")
		var subitems []Subitem
		err := changeSubitems(c, d, func(q *sqlc.Queries, itemID uuid.UUID, log func(string, any) error) error {
			existing, err := q.ListSubitems(c.Context(), []uuid.UUID{itemID})
			if err != nil {
				return err
			}
			byID := make(map[uuid.UUID]sqlc.Subitem, len(existing))
			for _, s := range existing {
				byID[s.ID] = s
			}
			seen := make(map[uuid.UUID]bool, len(in.IDs))
			for _, id := range in.IDs {
				if _, ok := byID[id]; !ok || seen[id] {
					return invalid
				}
				seen[id] = true
			}
			if len(in.IDs) != len(existing) {
				return invalid
			}
			subitems = make([]Subitem, 0, len(in.IDs))
			for position, id := range in.IDs {
				row := byID[id]
				if row.Position != int32(position) {
					row, err = q.SetSubitemPosition(c.Context(), sqlc.SetSubitemPositionParams{ID: id, Position: int32(position), Now: d.Now()})
					if err != nil {
						return err
					}
					if err := log("subitem.updated", ViewSubitem(row)); err != nil {
						return err
					}
				}
				subitems = append(subitems, ViewSubitem(row))
			}
			return nil
		})
		if err != nil {
			return err
		}
		return c.JSON(subitems)
	}
}
