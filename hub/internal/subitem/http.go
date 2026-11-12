// Package subitem manages the one-level checklist of an item.
package subitem

import (
	"net/http"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"github.com/rayakame/plsremind/hub/internal/apierr"
	"github.com/rayakame/plsremind/hub/internal/auth"
	"github.com/rayakame/plsremind/hub/internal/db"
	"github.com/rayakame/plsremind/hub/internal/db/sqlc"
	"github.com/rayakame/plsremind/hub/internal/deps"
	"github.com/rayakame/plsremind/hub/internal/eventlog"
	"github.com/rayakame/plsremind/hub/internal/httpx"
	"github.com/rayakame/plsremind/hub/internal/item"
)

func Register(user fiber.Router, d *deps.Deps) {
	subitems := user.Group("/items/:id/subitems")
	subitems.Post("", create(d))
	subitems.Put("/order", reorder(d))
	subitems.Patch("/:sid", update(d))
	subitems.Delete("/:sid", remove(d))
}

func titleError() error {
	return apierr.Field("title", "Must be 1 to 200 characters.")
}

// change runs fn on the locked item and records the resulting event.
func change(c fiber.Ctx, d *deps.Deps, fn func(q *sqlc.Queries, itemID uuid.UUID, log func(typ string, payload any) error) error) error {
	itemID, err := httpx.ParamUUID(c, "id")
	if err != nil {
		return err
	}
	userID := auth.Target(c)
	p, _ := auth.From(c)
	return db.Tx(c.Context(), d.Pool, func(q *sqlc.Queries) error {
		if _, err := item.Lock(c.Context(), q, userID, itemID); err != nil {
			return err
		}
		now := d.Now()
		if err := item.Touch(c.Context(), q, now, itemID); err != nil {
			return err
		}
		return fn(q, itemID, func(typ string, payload any) error {
			return eventlog.Append(c.Context(), q, now, userID, eventlog.ActorFrom(p), typ, payload)
		})
	})
}

func find(c fiber.Ctx, q *sqlc.Queries, itemID uuid.UUID) (sqlc.Subitem, error) {
	id, err := httpx.ParamUUID(c, "sid")
	if err != nil {
		return sqlc.Subitem{}, err
	}
	row, err := q.GetSubitem(c.Context(), sqlc.GetSubitemParams{ID: id, ItemID: itemID})
	if db.IsNotFound(err) {
		return sqlc.Subitem{}, apierr.NotFound()
	}
	return row, err
}

func create(d *deps.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		var in struct {
			Title string `json:"title"`
		}
		if err := httpx.Body(c, &in); err != nil {
			return err
		}
		title, ok := item.CleanSubitemTitle(in.Title)
		if !ok {
			return titleError()
		}
		var created item.Subitem
		err := change(c, d, func(q *sqlc.Queries, itemID uuid.UUID, log func(string, any) error) error {
			existing, err := q.ListSubitems(c.Context(), []uuid.UUID{itemID})
			if err != nil {
				return err
			}
			if len(existing) >= item.MaxSubitems {
				return apierr.Conflict("subitem_limit", "An item can have at most 50 subitems.")
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
			created = item.ViewSubitem(row)
			return log("subitem.created", created)
		})
		if err != nil {
			return err
		}
		return c.Status(http.StatusCreated).JSON(created)
	}
}

func update(d *deps.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		var in struct {
			Title *string `json:"title"`
			Done  *bool   `json:"done"`
		}
		if err := httpx.Body(c, &in); err != nil {
			return err
		}
		var updated item.Subitem
		err := change(c, d, func(q *sqlc.Queries, itemID uuid.UUID, log func(string, any) error) error {
			row, err := find(c, q, itemID)
			if err != nil {
				return err
			}
			params := sqlc.UpdateSubitemParams{ID: row.ID, Title: row.Title, Done: row.Done, Now: d.Now()}
			if in.Title != nil {
				title, ok := item.CleanSubitemTitle(*in.Title)
				if !ok {
					return titleError()
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
			updated = item.ViewSubitem(row)
			return log("subitem.updated", updated)
		})
		if err != nil {
			return err
		}
		return c.JSON(updated)
	}
}

func remove(d *deps.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		err := change(c, d, func(q *sqlc.Queries, itemID uuid.UUID, log func(string, any) error) error {
			row, err := find(c, q, itemID)
			if err != nil {
				return err
			}
			if err := q.DeleteSubitem(c.Context(), row.ID); err != nil {
				return err
			}
			return log("subitem.deleted", eventlog.Deleted{ID: row.ID, ItemID: &itemID})
		})
		if err != nil {
			return err
		}
		return c.SendStatus(http.StatusNoContent)
	}
}

func reorder(d *deps.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		var in struct {
			IDs []uuid.UUID `json:"ids"`
		}
		if err := httpx.Body(c, &in); err != nil {
			return err
		}
		invalid := apierr.Field("ids", "Must contain each subitem of the item exactly once.")
		var subitems []item.Subitem
		err := change(c, d, func(q *sqlc.Queries, itemID uuid.UUID, log func(string, any) error) error {
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
			subitems = make([]item.Subitem, 0, len(in.IDs))
			for position, id := range in.IDs {
				row := byID[id]
				if row.Position != int32(position) {
					row, err = q.SetSubitemPosition(c.Context(), sqlc.SetSubitemPositionParams{ID: id, Position: int32(position), Now: d.Now()})
					if err != nil {
						return err
					}
					if err := log("subitem.updated", item.ViewSubitem(row)); err != nil {
						return err
					}
				}
				subitems = append(subitems, item.ViewSubitem(row))
			}
			return nil
		})
		if err != nil {
			return err
		}
		return c.JSON(subitems)
	}
}
