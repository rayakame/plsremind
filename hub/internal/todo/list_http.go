package todo

import (
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"github.com/rayakame/plsremind/hub/internal/api"
	"github.com/rayakame/plsremind/hub/internal/auth"
	"github.com/rayakame/plsremind/hub/internal/core"
	"github.com/rayakame/plsremind/hub/internal/events"
	"github.com/rayakame/plsremind/hub/internal/store"
	"github.com/rayakame/plsremind/hub/internal/store/sqlc"
)

func registerLists(user fiber.Router, d *core.Deps) {
	lists := user.Group("/lists")
	lists.Get("", listLists(d))
	lists.Post("", createList(d))
	lists.Put("/order", reorderLists(d))
	lists.Patch("/:id", renameList(d))
	lists.Delete("/:id", removeList(d))
}

func readListName(c fiber.Ctx) (string, error) {
	var in struct {
		Name string `json:"name"`
	}
	if err := api.Body(c, &in); err != nil {
		return "", err
	}
	name := strings.TrimSpace(in.Name)
	if n := utf8.RuneCountInString(name); n < 1 || n > maxListNameLen {
		return "", api.Field("name", "Must be 1 to 100 characters.")
	}
	return name, nil
}

func listLists(d *core.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		rows, err := d.Q().ListLists(c.Context(), auth.Target(c))
		if err != nil {
			return err
		}
		lists := make([]List, 0, len(rows))
		for _, row := range rows {
			lists = append(lists, ViewList(row))
		}
		return c.JSON(lists)
	}
}

func createList(d *core.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		name, err := readListName(c)
		if err != nil {
			return err
		}
		userID := auth.Target(c)
		var created List
		err = store.UserTx(c.Context(), d.Pool, userID, func(q *sqlc.Queries) error {
			// The row lock makes position assignment and the limit check safe under concurrency.
			if _, err := q.LockUser(c.Context(), userID); err != nil {
				return err
			}
			existing, err := q.ListLists(c.Context(), userID)
			if err != nil {
				return err
			}
			if len(existing) >= maxLists {
				return api.Conflict("list_limit", "You have reached the maximum number of lists.")
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
			created = ViewList(row)
			return events.Append(c.Context(), q, now, userID, actor(c), "list.created", created)
		})
		if err != nil {
			return err
		}
		return c.Status(http.StatusCreated).JSON(created)
	}
}

func renameList(d *core.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		id, err := api.ParamUUID(c, "id")
		if err != nil {
			return err
		}
		name, err := readListName(c)
		if err != nil {
			return err
		}
		userID := auth.Target(c)
		var updated List
		err = store.UserTx(c.Context(), d.Pool, userID, func(q *sqlc.Queries) error {
			current, err := GetList(c.Context(), q, userID, id)
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
			updated = ViewList(row)
			return events.Append(c.Context(), q, now, userID, actor(c), "list.updated", updated)
		})
		if err != nil {
			return err
		}
		return c.JSON(updated)
	}
}

func removeList(d *core.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		id, err := api.ParamUUID(c, "id")
		if err != nil {
			return err
		}
		userID := auth.Target(c)
		err = store.UserTx(c.Context(), d.Pool, userID, func(q *sqlc.Queries) error {
			current, err := GetList(c.Context(), q, userID, id)
			if err != nil {
				return err
			}
			if current.IsInbox {
				return errInbox()
			}
			now := d.Now()
			inbox, err := Inbox(c.Context(), q, userID)
			if err != nil {
				return err
			}
			moved, err := MoveItems(c.Context(), q, now, id, inbox.ID)
			if err != nil {
				return err
			}
			for _, it := range moved {
				if err := events.Append(c.Context(), q, now, userID, actor(c), "item.updated", it); err != nil {
					return err
				}
			}
			if err := q.DeleteList(c.Context(), id); err != nil {
				return err
			}
			return events.Append(c.Context(), q, now, userID, actor(c), "list.deleted", events.Deleted{ID: id})
		})
		if err != nil {
			return err
		}
		return c.SendStatus(http.StatusNoContent)
	}
}

func reorderLists(d *core.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		var in struct {
			IDs []uuid.UUID `json:"ids"`
		}
		if err := api.Body(c, &in); err != nil {
			return err
		}
		userID := auth.Target(c)
		var lists []List
		err := store.UserTx(c.Context(), d.Pool, userID, func(q *sqlc.Queries) error {
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
					return api.Field("ids", "Must contain each of your lists exactly once.")
				}
				seen[id] = true
			}
			if len(in.IDs) != len(existing) {
				return api.Field("ids", "Must contain each of your lists exactly once.")
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
					if err := events.Append(c.Context(), q, now, userID, actor(c), "list.updated", ViewList(row)); err != nil {
						return err
					}
				}
				lists = append(lists, ViewList(row))
			}
			return nil
		})
		if err != nil {
			return err
		}
		return c.JSON(lists)
	}
}
