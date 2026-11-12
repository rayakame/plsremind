// Package reminder manages the reminders a user sets by hand: "remind me again in 2h".
package reminder

import (
	"net/http"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"github.com/rayakame/plsremind/hub/internal/apierr"
	"github.com/rayakame/plsremind/hub/internal/auth"
	"github.com/rayakame/plsremind/hub/internal/db"
	"github.com/rayakame/plsremind/hub/internal/db/sqlc"
	"github.com/rayakame/plsremind/hub/internal/deps"
	"github.com/rayakame/plsremind/hub/internal/duration"
	"github.com/rayakame/plsremind/hub/internal/eventlog"
	"github.com/rayakame/plsremind/hub/internal/httpx"
	"github.com/rayakame/plsremind/hub/internal/item"
	"github.com/rayakame/plsremind/hub/internal/schedule"
)

const maxPerItem = 20

type Reminder = item.Reminder

func view(r sqlc.ManualReminder) Reminder {
	return item.ViewReminder(r)
}

// announceItem logs the item with its new set of reminders, for clients that keep items in memory.
func announceItem(c fiber.Ctx, q *sqlc.Queries, now time.Time, userID, itemID uuid.UUID, actor eventlog.Actor) error {
	whole, err := item.Load(c.Context(), q, userID, itemID)
	if err != nil {
		return err
	}
	return eventlog.Append(c.Context(), q, now, userID, actor, "item.updated", whole)
}

func Register(user fiber.Router, d *deps.Deps) {
	reminders := user.Group("/items/:id/reminders")
	reminders.Get("", index(d))
	reminders.Post("", create(d))
	reminders.Delete("/:rid", remove(d))
}

func index(d *deps.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		itemID, err := httpx.ParamUUID(c, "id")
		if err != nil {
			return err
		}
		q := d.Q()
		if _, err := item.Load(c.Context(), q, auth.Target(c), itemID); err != nil {
			return err
		}
		rows, err := q.ListManualReminders(c.Context(), itemID)
		if err != nil {
			return err
		}
		reminders := make([]Reminder, 0, len(rows))
		for _, row := range rows {
			reminders = append(reminders, view(row))
		}
		return c.JSON(reminders)
	}
}

// remindAt resolves the two ways of naming a time: an absolute "at" or a relative "in".
func remindAt(now time.Time, at, in string) (time.Time, error) {
	switch {
	case (at == "") == (in == ""):
		return time.Time{}, apierr.Field("at", `Send exactly one of "at" and "in".`)
	case in != "":
		offset, err := duration.Parse(in)
		if err != nil {
			return time.Time{}, apierr.Field("in", `Must be a duration such as "2h" or "1d12h".`)
		}
		return now.Add(offset), nil
	}
	when, err := time.Parse(time.RFC3339, at)
	if err != nil {
		return time.Time{}, apierr.Field("at", "Must be an RFC 3339 timestamp.")
	}
	if !when.After(now) {
		return time.Time{}, apierr.Field("at", "Must be in the future.")
	}
	return when, nil
}

func create(d *deps.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		itemID, err := httpx.ParamUUID(c, "id")
		if err != nil {
			return err
		}
		var in struct {
			At string `json:"at"`
			In string `json:"in"`
		}
		if err := httpx.Body(c, &in); err != nil {
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
		err = db.UserTx(c.Context(), d.Pool, userID, func(q *sqlc.Queries) error {
			row, err := item.Lock(c.Context(), q, userID, itemID)
			if err != nil {
				return err
			}
			if row.Status == "done" {
				return apierr.Conflict("item_done", "The item is already done.")
			}
			existing, err := q.ListManualReminders(c.Context(), itemID)
			if err != nil {
				return err
			}
			if len(existing) >= maxPerItem {
				return apierr.Conflict("reminder_limit", "An item can have at most 20 manual reminders.")
			}
			reminder, err := q.CreateManualReminder(c.Context(), sqlc.CreateManualReminderParams{
				ID:        uuid.Must(uuid.NewV7()),
				ItemID:    itemID,
				RemindAt:  when,
				CreatedAt: now,
			})
			if err != nil {
				return err
			}
			if err := schedule.SyncItem(c.Context(), q, now, itemID); err != nil {
				return err
			}
			created = view(reminder)
			if err := eventlog.Append(c.Context(), q, now, userID, eventlog.ActorFrom(p), "reminder.created", created); err != nil {
				return err
			}
			return announceItem(c, q, now, userID, itemID, eventlog.ActorFrom(p))
		})
		if err != nil {
			return err
		}
		return c.Status(http.StatusCreated).JSON(created)
	}
}

func remove(d *deps.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		itemID, err := httpx.ParamUUID(c, "id")
		if err != nil {
			return err
		}
		id, err := httpx.ParamUUID(c, "rid")
		if err != nil {
			return err
		}
		userID := auth.Target(c)
		p, _ := auth.From(c)
		err = db.UserTx(c.Context(), d.Pool, userID, func(q *sqlc.Queries) error {
			if _, err := item.Lock(c.Context(), q, userID, itemID); err != nil {
				return err
			}
			n, err := q.DeleteManualReminder(c.Context(), sqlc.DeleteManualReminderParams{ID: id, ItemID: itemID})
			if err != nil {
				return err
			}
			if n == 0 {
				return apierr.NotFound()
			}
			if err := schedule.SyncItem(c.Context(), q, d.Now(), itemID); err != nil {
				return err
			}
			now := d.Now()
			if err := eventlog.Append(c.Context(), q, now, userID, eventlog.ActorFrom(p), "reminder.deleted", eventlog.Deleted{ID: id, ItemID: &itemID}); err != nil {
				return err
			}
			return announceItem(c, q, now, userID, itemID, eventlog.ActorFrom(p))
		})
		if err != nil {
			return err
		}
		return c.SendStatus(http.StatusNoContent)
	}
}
