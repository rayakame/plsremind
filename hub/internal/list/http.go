package list

import (
	"net/http"
	"strings"
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
)

func Register(user fiber.Router, d *deps.Deps) {
	lists := user.Group("/lists")
	lists.Get("", index(d))
	lists.Post("", create(d))
	lists.Put("/order", reorder(d))
	lists.Patch("/:id", rename(d))
	lists.Delete("/:id", remove(d))
}

func actor(c fiber.Ctx) eventlog.Actor {
	p, _ := auth.From(c)
	return eventlog.ActorFrom(p)
}

func readName(c fiber.Ctx) (string, error) {
	var in struct {
		Name string `json:"name"`
	}
	if err := httpx.Body(c, &in); err != nil {
		return "", err
	}
	name := strings.TrimSpace(in.Name)
	if n := utf8.RuneCountInString(name); n < 1 || n > maxNameLen {
		return "", apierr.Field("name", "Must be 1 to 100 characters.")
	}
	return name, nil
}

func index(d *deps.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		rows, err := d.Q().ListLists(c.Context(), auth.Target(c))
		if err != nil {
			return err
		}
		lists := make([]List, 0, len(rows))
		for _, row := range rows {
			lists = append(lists, View(row))
		}
		return c.JSON(lists)
	}
}

func create(d *deps.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		name, err := readName(c)
		if err != nil {
			return err
		}
		userID := auth.Target(c)
		var created List
		err = db.Tx(c.Context(), d.Pool, func(q *sqlc.Queries) error {
			// The row lock makes position assignment and the limit check safe under concurrency.
			if _, err := q.LockUser(c.Context(), userID); err != nil {
				return err
			}
			existing, err := q.ListLists(c.Context(), userID)
			if err != nil {
				return err
			}
			if len(existing) >= maxLists {
				return apierr.Conflict("list_limit", "You have reached the maximum number of lists.")
			}
			position := int32(0)
			for _, l := range existing {
				position = max(position, l.Position+1)
			}
			now := d.Now()
			row, err := q.CreateList(c.Context(), sqlc.CreateListParams{
				ID:       uuid.Must(uuid.NewV7()),
				UserID:   userID,
				Name:     name,
				Position: position,
				Now:      now,
			})
			if err != nil {
				return err
			}
			created = View(row)
			return eventlog.Append(c.Context(), q, now, userID, actor(c), "list.created", created)
		})
		if err != nil {
			return err
		}
		return c.Status(http.StatusCreated).JSON(created)
	}
}

func rename(d *deps.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		id, err := httpx.ParamUUID(c, "id")
		if err != nil {
			return err
		}
		name, err := readName(c)
		if err != nil {
			return err
		}
		userID := auth.Target(c)
		var updated List
		err = db.Tx(c.Context(), d.Pool, func(q *sqlc.Queries) error {
			current, err := Get(c.Context(), q, userID, id)
			if err != nil {
				return err
			}
			if current.IsInbox {
				return errInbox()
			}
			now := d.Now()
			row, err := q.RenameList(c.Context(), sqlc.RenameListParams{ID: id, Name: name, Now: now})
			if err != nil {
				return err
			}
			updated = View(row)
			return eventlog.Append(c.Context(), q, now, userID, actor(c), "list.updated", updated)
		})
		if err != nil {
			return err
		}
		return c.JSON(updated)
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
			current, err := Get(c.Context(), q, userID, id)
			if err != nil {
				return err
			}
			if current.IsInbox {
				return errInbox()
			}
			now := d.Now()
			if err := q.DeleteList(c.Context(), id); err != nil {
				return err
			}
			return eventlog.Append(c.Context(), q, now, userID, actor(c), "list.deleted", eventlog.Deleted{ID: id})
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
		userID := auth.Target(c)
		var lists []List
		err := db.Tx(c.Context(), d.Pool, func(q *sqlc.Queries) error {
			if _, err := q.LockUser(c.Context(), userID); err != nil {
				return err
			}
			existing, err := q.ListLists(c.Context(), userID)
			if err != nil {
				return err
			}
			byID := make(map[uuid.UUID]sqlc.List, len(existing))
			for _, l := range existing {
				byID[l.ID] = l
			}
			seen := make(map[uuid.UUID]bool, len(in.IDs))
			for _, id := range in.IDs {
				if _, ok := byID[id]; !ok || seen[id] {
					return apierr.Field("ids", "Must contain each of your lists exactly once.")
				}
				seen[id] = true
			}
			if len(in.IDs) != len(existing) {
				return apierr.Field("ids", "Must contain each of your lists exactly once.")
			}
			now := d.Now()
			lists = make([]List, 0, len(in.IDs))
			for position, id := range in.IDs {
				row := byID[id]
				if row.Position != int32(position) {
					row, err = q.SetListPosition(c.Context(), sqlc.SetListPositionParams{ID: id, Position: int32(position), Now: now})
					if err != nil {
						return err
					}
					if err := eventlog.Append(c.Context(), q, now, userID, actor(c), "list.updated", View(row)); err != nil {
						return err
					}
				}
				lists = append(lists, View(row))
			}
			return nil
		})
		if err != nil {
			return err
		}
		return c.JSON(lists)
	}
}
