package todo

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"github.com/rayakame/plsremind/hub/internal/api"
	"github.com/rayakame/plsremind/hub/internal/auth"
	"github.com/rayakame/plsremind/hub/internal/core"
	"github.com/rayakame/plsremind/hub/internal/events"
	"github.com/rayakame/plsremind/hub/internal/reminder"
	"github.com/rayakame/plsremind/hub/internal/store"
	"github.com/rayakame/plsremind/hub/internal/store/sqlc"
)

func registerItems(user fiber.Router, d *core.Deps) {
	items := user.Group("/items")
	items.Get("", listItems(d))
	items.Post("", createItem(d))
	items.Get("/:id", showItem(d))
	items.Patch("/:id", updateItem(d))
	items.Delete("/:id", removeItem(d))
}

func actor(c fiber.Ctx) events.Actor {
	p, _ := auth.From(c)
	return auth.ActorFrom(p)
}

func createItem(d *core.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		var in itemInput
		if err := api.Body(c, &in); err != nil {
			return err
		}
		userID := auth.Target(c)
		var created Item
		err := store.UserTx(c.Context(), d.Pool, userID, func(q *sqlc.Queries) error {
			inbox, err := q.GetInbox(c.Context(), userID)
			if err != nil {
				return err
			}
			list, listValid, err := in.holdList(c.Context(), q, userID)
			if err != nil {
				return err
			}
			now := d.Now()
			row := sqlc.Item{ID: uuid.Must(uuid.NewV7()), UserID: userID, ListID: inbox.ID, Priority: "normal", Status: "open"}
			if err := in.apply(now, &row, list, listValid); err != nil {
				return err
			}
			row, err = q.CreateItem(c.Context(), sqlc.CreateItemParams{
				ID:               row.ID,
				UserID:           row.UserID,
				ListID:           row.ListID,
				Title:            row.Title,
				Note:             row.Note,
				Priority:         row.Priority,
				DueAt:            row.DueAt,
				DueDate:          row.DueDate,
				Status:           row.Status,
				CompletedAt:      row.CompletedAt,
				ReminderOverride: row.ReminderOverride,
				Now:              now,
			})
			if err != nil {
				return err
			}
			for position, title := range in.Subitems {
				_, err := q.CreateSubitem(c.Context(), sqlc.CreateSubitemParams{
					ID:       uuid.Must(uuid.NewV7()),
					ItemID:   row.ID,
					Title:    title,
					Position: int32(position),
					Now:      now,
				})
				if err != nil {
					return err
				}
			}
			if err := reminder.SyncItem(c.Context(), q, now, row.ID); err != nil {
				return err
			}
			created, err = LoadItem(c.Context(), q, userID, row.ID)
			if err != nil {
				return err
			}
			return events.Append(c.Context(), q, now, userID, actor(c), "item.created", created)
		})
		if err != nil {
			return err
		}
		return c.Status(http.StatusCreated).JSON(created)
	}
}

func showItem(d *core.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		id, err := api.ParamUUID(c, "id")
		if err != nil {
			return err
		}
		item, err := LoadItem(c.Context(), d.Q(), auth.Target(c), id)
		if err != nil {
			return err
		}
		return c.JSON(item)
	}
}

func updateItem(d *core.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		id, err := api.ParamUUID(c, "id")
		if err != nil {
			return err
		}
		var in itemInput
		if err := api.Body(c, &in); err != nil {
			return err
		}
		if in.Title.Null {
			return api.Field("title", "Must be 1 to 500 characters.")
		}
		userID := auth.Target(c)
		var updated Item
		err = store.UserTx(c.Context(), d.Pool, userID, func(q *sqlc.Queries) error {
			list, listValid, err := in.holdList(c.Context(), q, userID)
			if err != nil {
				return err
			}
			row, err := LockItem(c.Context(), q, userID, id)
			if err != nil {
				return err
			}
			now := d.Now()
			if err := in.apply(now, &row, list, listValid); err != nil {
				return err
			}
			_, err = q.UpdateItem(c.Context(), sqlc.UpdateItemParams{
				ID:               row.ID,
				ListID:           row.ListID,
				Title:            row.Title,
				Note:             row.Note,
				Priority:         row.Priority,
				DueAt:            row.DueAt,
				DueDate:          row.DueDate,
				Status:           row.Status,
				CompletedAt:      row.CompletedAt,
				ReminderOverride: row.ReminderOverride,
				Now:              now,
			})
			if err != nil {
				return err
			}
			if err := reminder.SyncItem(c.Context(), q, now, id); err != nil {
				return err
			}
			if row.Status == "done" {
				if err := reminder.CancelDeliveries(c.Context(), q, now, id); err != nil {
					return err
				}
			}
			updated, err = LoadItem(c.Context(), q, userID, id)
			if err != nil {
				return err
			}
			return events.Append(c.Context(), q, now, userID, actor(c), "item.updated", updated)
		})
		if err != nil {
			return err
		}
		return c.JSON(updated)
	}
}

func removeItem(d *core.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		id, err := api.ParamUUID(c, "id")
		if err != nil {
			return err
		}
		userID := auth.Target(c)
		err = store.UserTx(c.Context(), d.Pool, userID, func(q *sqlc.Queries) error {
			if _, err := LockItem(c.Context(), q, userID, id); err != nil {
				return err
			}
			// Schedule first, deliveries second: the same order the ticker takes them in.
			if err := q.DeleteItemSchedule(c.Context(), id); err != nil {
				return err
			}
			if err := reminder.CancelDeliveries(c.Context(), q, d.Now(), id); err != nil {
				return err
			}
			if err := q.DeleteItem(c.Context(), id); err != nil {
				return err
			}
			return events.Append(c.Context(), q, d.Now(), userID, actor(c), "item.deleted", events.Deleted{ID: id})
		})
		if err != nil {
			return err
		}
		return c.SendStatus(http.StatusNoContent)
	}
}

type itemFilter struct {
	listID    *uuid.UUID
	status    *string
	hasDue    *bool
	dueBefore *time.Time
	dueAfter  *time.Time
	sort      string
	desc      bool
}

func parseItemFilter(c fiber.Ctx) (itemFilter, error) {
	f := itemFilter{sort: "created"}
	fields := map[string]string{}

	if raw := c.Query("list_id"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			fields["list_id"] = "Must be a list ID."
		}
		f.listID = &id
	}
	switch status := c.Query("status", "open"); status {
	case "open", "done":
		f.status = &status
	case "all":
	default:
		fields["status"] = `Must be "open", "done" or "all".`
	}
	if raw := c.Query("has_due"); raw != "" {
		hasDue, err := strconv.ParseBool(raw)
		if err != nil {
			fields["has_due"] = `Must be "true" or "false".`
		}
		f.hasDue = &hasDue
	}
	for name, target := range map[string]**time.Time{"due_before": &f.dueBefore, "due_after": &f.dueAfter} {
		if raw := c.Query(name); raw != "" {
			at, err := time.Parse(time.RFC3339, raw)
			if err != nil {
				fields[name] = "Must be an RFC 3339 timestamp."
			}
			*target = &at
		}
	}
	switch sort := c.Query("sort", "created"); sort {
	case "created", "due", "priority":
		f.sort = sort
	default:
		fields["sort"] = `Must be "created", "due" or "priority".`
	}
	switch c.Query("order", "asc") {
	case "asc":
	case "desc":
		f.desc = true
	default:
		fields["order"] = `Must be "asc" or "desc".`
	}
	if len(fields) > 0 {
		return itemFilter{}, api.Validation(fields)
	}
	return f, nil
}

func (f itemFilter) order() string {
	if f.desc {
		return "desc"
	}
	return "asc"
}

func listItems(d *core.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		f, err := parseItemFilter(c)
		if err != nil {
			return err
		}
		page, err := api.ParsePage(c)
		if err != nil {
			return err
		}
		var after itemCursor
		var cursorID *uuid.UUID
		if page.Cursor != "" {
			after, err = decodeItemCursor(page.Cursor, f.sort, f.order())
			if err != nil {
				return err
			}
			cursorID = &after.ID
		}

		userID := auth.Target(c)
		q := d.Q()
		size := int32(page.Limit + 1)
		var rows []sqlc.Item
		switch f.sort {
		case "due":
			rows, err = q.ListItemsByDue(c.Context(), sqlc.ListItemsByDueParams{
				UserID: userID, ListID: f.listID, Status: f.status, HasDue: f.hasDue, DueBefore: f.dueBefore, DueAfter: f.dueAfter,
				CursorID: cursorID, CursorDue: after.Due, Descending: f.desc, PageSize: size,
			})
		case "priority":
			var cursorRank *int32
			if after.Rank != nil {
				r := int32(*after.Rank)
				cursorRank = &r
			} else if cursorID != nil {
				return api.ErrCursor()
			}
			rows, err = q.ListItemsByPriority(c.Context(), sqlc.ListItemsByPriorityParams{
				UserID: userID, ListID: f.listID, Status: f.status, HasDue: f.hasDue, DueBefore: f.dueBefore, DueAfter: f.dueAfter,
				CursorID: cursorID, CursorRank: cursorRank, Descending: f.desc, PageSize: size,
			})
		default:
			rows, err = q.ListItemsByCreated(c.Context(), sqlc.ListItemsByCreatedParams{
				UserID: userID, ListID: f.listID, Status: f.status, HasDue: f.hasDue, DueBefore: f.dueBefore, DueAfter: f.dueAfter,
				CursorID: cursorID, Descending: f.desc, PageSize: size,
			})
		}
		if err != nil {
			return err
		}
		items, err := ViewItems(c.Context(), q, rows)
		if err != nil {
			return err
		}
		return c.JSON(api.NewList(items, page.Limit, func(last Item) any {
			next := itemCursor{Sort: f.sort, Order: f.order(), ID: last.ID}
			switch f.sort {
			case "due":
				next.Due = last.DueAt
			case "priority":
				r := priorityRank(last.Priority)
				next.Rank = &r
			}
			return next
		}))
	}
}
