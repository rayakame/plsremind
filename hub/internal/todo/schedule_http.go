package todo

import (
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"github.com/rayakame/plsremind/hub/internal/api"
	"github.com/rayakame/plsremind/hub/internal/auth"
	"github.com/rayakame/plsremind/hub/internal/core"
)

// Scheduled is one reminder the hub will send for an item.
type Scheduled struct {
	Kind   string  `json:"kind"`
	Offset *string `json:"offset"`
	// ReminderID names the manual reminder behind a reminder of kind manual.
	ReminderID *uuid.UUID `json:"reminder_id"`
	// ScheduledFor is the moment the reminder is meant for, FireAt when it goes
	// out. Quiet hours can put the second after the first.
	ScheduledFor time.Time `json:"scheduled_for"`
	FireAt       time.Time `json:"fire_at"`
}

// showSchedule lists what the ticker has in store for an item. It reads the
// stored schedule rather than working it out again, so it cannot disagree with
// what the hub goes on to do.
func showSchedule(d *core.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		itemID, err := api.ParamUUID(c, "id")
		if err != nil {
			return err
		}
		q := d.Q()
		if _, err := LoadItem(c.Context(), q, auth.Target(c), itemID); err != nil {
			return err
		}
		rows, err := q.ListItemScheduleByTime(c.Context(), itemID)
		if err != nil {
			return err
		}
		out := make([]Scheduled, 0, len(rows))
		for _, row := range rows {
			s := Scheduled{Kind: row.Kind, ReminderID: row.ManualID, ScheduledFor: row.ScheduledFor.UTC(), FireAt: row.FireAt.UTC()}
			if row.OffsetSeconds != nil {
				offset := api.FormatDuration(time.Duration(*row.OffsetSeconds) * time.Second)
				s.Offset = &offset
			}
			out = append(out, s)
		}
		return c.JSON(out)
	}
}
