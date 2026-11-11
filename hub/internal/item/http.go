package item

import (
	"net/http"
	"strconv"
	"time"

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
	items := user.Group("/items")
	items.Get("", index(d))
	items.Post("", create(d))
	items.Get("/:id", show(d))
	items.Patch("/:id", update(d))
	items.Delete("/:id", remove(d))
}

func actor(c fiber.Ctx) eventlog.Actor {
	p, _ := auth.From(c)
	return eventlog.ActorFrom(p)
}

func create(d *deps.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		var in input
		if err := httpx.Body(c, &in); err != nil {
			return err
		}
		userID := auth.Target(c)
		var created Item
		err := db.Tx(c.Context(), d.Pool, func(q *sqlc.Queries) error {
			inbox, err := q.GetInbox(c.Context(), userID)
			if err != nil {
				return err
			}
			now := d.Now()
			row := sqlc.Item{ID: uuid.Must(uuid.NewV7()), UserID: userID, ListID: inbox.ID, Priority: "normal", Status: "open"}
			if err := in.apply(c.Context(), q, now, &row); err != nil {
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
			created, err = Load(c.Context(), q, userID, row.ID)
			if err != nil {
				return err
			}
			return eventlog.Append(c.Context(), q, now, userID, actor(c), "item.created", created)
		})
		if err != nil {
			return err
		}
		return c.Status(http.StatusCreated).JSON(created)
	}
}

func show(d *deps.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		id, err := httpx.ParamUUID(c, "id")
		if err != nil {
			return err
		}
		item, err := Load(c.Context(), d.Q(), auth.Target(c), id)
		if err != nil {
			return err
		}
		return c.JSON(item)
	}
}

func update(d *deps.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		id, err := httpx.ParamUUID(c, "id")
		if err != nil {
			return err
		}
		var in input
		if err := httpx.Body(c, &in); err != nil {
			return err
		}
		if in.Title.Null {
			return apierr.Field("title", "Must be 1 to 500 characters.")
		}
		userID := auth.Target(c)
		var updated Item
		err = db.Tx(c.Context(), d.Pool, func(q *sqlc.Queries) error {
			row, err := get(c.Context(), q, userID, id)
			if err != nil {
				return err
			}
			now := d.Now()
			if err := in.apply(c.Context(), q, now, &row); err != nil {
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
			updated, err = Load(c.Context(), q, userID, id)
			if err != nil {
				return err
			}
			return eventlog.Append(c.Context(), q, now, userID, actor(c), "item.updated", updated)
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
			if _, err := get(c.Context(), q, userID, id); err != nil {
				return err
			}
			if err := q.DeleteItem(c.Context(), id); err != nil {
				return err
			}
			return eventlog.Append(c.Context(), q, d.Now(), userID, actor(c), "item.deleted", eventlog.Deleted{ID: id})
		})
		if err != nil {
			return err
		}
		return c.SendStatus(http.StatusNoContent)
	}
}

type filter struct {
	listID    *uuid.UUID
	status    *string
	hasDue    *bool
	dueBefore *time.Time
	dueAfter  *time.Time
	sort      string
	desc      bool
}

func parseFilter(c fiber.Ctx) (filter, error) {
	f := filter{sort: "created"}
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
		return filter{}, apierr.Validation(fields)
	}
	return f, nil
}

func (f filter) order() string {
	if f.desc {
		return "desc"
	}
	return "asc"
}

func index(d *deps.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		f, err := parseFilter(c)
		if err != nil {
			return err
		}
		page, err := httpx.ParsePage(c)
		if err != nil {
			return err
		}
		var after cursor
		var cursorID *uuid.UUID
		if page.Cursor != "" {
			after, err = decodeCursor(page.Cursor, f.sort, f.order())
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
				return httpx.ErrCursor()
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
		items, err := Views(c.Context(), q, userID, rows)
		if err != nil {
			return err
		}
		return c.JSON(httpx.NewList(items, page.Limit, func(last Item) any {
			next := cursor{Sort: f.sort, Order: f.order(), ID: last.ID}
			switch f.sort {
			case "due":
				next.Due = last.DueAt
			case "priority":
				r := rank(last.Priority)
				next.Rank = &r
			}
			return next
		}))
	}
}
