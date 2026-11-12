package todo

import (
	"net/http"
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

const maxRemindersPerItem = 20

// announceItem logs the item with its new set of reminders, for clients that keep items in memory.
func announceItem(c fiber.Ctx, q *sqlc.Queries, now time.Time, userID, itemID uuid.UUID, actor events.Actor) error {
	whole, err := LoadItem(c.Context(), q, userID, itemID)
	if err != nil {
		return err
	}
	return events.Append(c.Context(), q, now, userID, actor, "item.updated", whole)
}

func registerReminders(user fiber.Router, d *core.Deps) {
	reminders := user.Group("/items/:id/reminders")
	reminders.Get("", listReminders(d))
	reminders.Post("", createReminder(d))
	reminders.Delete("/:rid", removeReminder(d))
}

func listReminders(d *core.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		itemID, err := api.ParamUUID(c, "id")
		if err != nil {
			return err
		}
		q := d.Q()
		if _, err := LoadItem(c.Context(), q, auth.Target(c), itemID); err != nil {
			return err
		}
		rows, err := q.ListManualReminders(c.Context(), itemID)
		if err != nil {
			return err
		}
		reminders := make([]Reminder, 0, len(rows))
		for _, row := range rows {
			reminders = append(reminders, ViewReminder(row))
		}
		return c.JSON(reminders)
	}
}

// remindAt resolves the two ways of naming a time: an absolute "at" or a relative "in".
func remindAt(now time.Time, at, in string) (time.Time, error) {
	switch {
	case (at == "") == (in == ""):
		return time.Time{}, api.Field("at", `Send exactly one of "at" and "in".`)
	case in != "":
		offset, err := api.ParseDuration(in)
		if err != nil {
			return time.Time{}, api.Field("in", `Must be a duration such as "2h" or "1d12h".`)
		}
		return now.Add(offset), nil
	}
	when, err := time.Parse(time.RFC3339, at)
	if err != nil {
		return time.Time{}, api.Field("at", "Must be an RFC 3339 timestamp.")
	}
	if !when.After(now) {
		return time.Time{}, api.Field("at", "Must be in the future.")
	}
	return when, nil
}

func createReminder(d *core.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		itemID, err := api.ParamUUID(c, "id")
		if err != nil {
			return err
		}
		var in struct {
			At string `json:"at"`
			In string `json:"in"`
		}
		if err := api.Body(c, &in); err != nil {
			return err
		}
		now := d.Now()
		when, err := remindAt(now, in.At, in.In)
		if err != nil {
			return err
		}
		userID := auth.Target(c)
		p, _ := auth.From(c)
		var created Reminder
		err = store.UserTx(c.Context(), d.Pool, userID, func(q *sqlc.Queries) error {
			row, err := LockItem(c.Context(), q, userID, itemID)
			if err != nil {
				return err
			}
			if row.Status == "done" {
				return api.Conflict("item_done", "The item is already done.")
			}
			existing, err := q.ListManualReminders(c.Context(), itemID)
			if err != nil {
				return err
			}
			if len(existing) >= maxRemindersPerItem {
				return api.Conflict("reminder_limit", "An item can have at most 20 manual reminders.")
			}
			added, err := q.CreateManualReminder(c.Context(), sqlc.CreateManualReminderParams{
				ID:        uuid.Must(uuid.NewV7()),
				ItemID:    itemID,
				RemindAt:  when,
				CreatedAt: now,
			})
			if err != nil {
				return err
			}
			if err := reminder.SyncItem(c.Context(), q, now, itemID); err != nil {
				return err
			}
			created = ViewReminder(added)
			if err := events.Append(c.Context(), q, now, userID, auth.ActorFrom(p), "reminder.created", created); err != nil {
				return err
			}
			return announceItem(c, q, now, userID, itemID, auth.ActorFrom(p))
		})
		if err != nil {
			return err
		}
		return c.Status(http.StatusCreated).JSON(created)
	}
}

func removeReminder(d *core.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		itemID, err := api.ParamUUID(c, "id")
		if err != nil {
			return err
		}
		id, err := api.ParamUUID(c, "rid")
		if err != nil {
			return err
		}
		userID := auth.Target(c)
		p, _ := auth.From(c)
		err = store.UserTx(c.Context(), d.Pool, userID, func(q *sqlc.Queries) error {
			if _, err := LockItem(c.Context(), q, userID, itemID); err != nil {
				return err
			}
			n, err := q.DeleteManualReminder(c.Context(), sqlc.DeleteManualReminderParams{ID: id, ItemID: itemID})
			if err != nil {
				return err
			}
			if n == 0 {
				return api.NotFound()
			}
			if err := reminder.SyncItem(c.Context(), q, d.Now(), itemID); err != nil {
				return err
			}
			now := d.Now()
			if err := events.Append(c.Context(), q, now, userID, auth.ActorFrom(p), "reminder.deleted", events.Deleted{ID: id, ItemID: &itemID}); err != nil {
				return err
			}
			return announceItem(c, q, now, userID, itemID, auth.ActorFrom(p))
		})
		if err != nil {
			return err
		}
		return c.SendStatus(http.StatusNoContent)
	}
}
