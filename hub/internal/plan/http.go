package plan

import (
	"net/http"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/rayakame/plsremind/hub/internal/apierr"
	"github.com/rayakame/plsremind/hub/internal/auth"
	"github.com/rayakame/plsremind/hub/internal/db/sqlc"
	"github.com/rayakame/plsremind/hub/internal/deps"
	"github.com/rayakame/plsremind/hub/internal/httpx"
)

type view struct {
	Priority  string   `json:"priority"`
	Before    []string `json:"before"`
	FollowUp  string   `json:"follow_up"`
	IsDefault bool     `json:"is_default"`
}

func Register(user fiber.Router, d *deps.Deps) {
	plans := user.Group("/reminder-plans")
	plans.Get("", index(d))
	plans.Put("/:priority", put(d))
	plans.Delete("/:priority", reset(d))
}

func index(d *deps.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		rows, err := d.Q().ListReminderPlans(c.Context(), auth.Target(c))
		if err != nil {
			return err
		}
		custom := make(map[string]sqlc.ReminderPlan, len(rows))
		for _, row := range rows {
			custom[row.Priority] = row
		}
		plans := make([]view, 0, len(Priorities))
		for _, priority := range Priorities {
			if row, ok := custom[priority]; ok {
				before := make([]time.Duration, 0, len(row.BeforeSeconds))
				for _, s := range row.BeforeSeconds {
					before = append(before, time.Duration(s)*time.Second)
				}
				plans = append(plans, view{Priority: priority, Before: FormatBefore(before), FollowUp: row.FollowUp})
				continue
			}
			def := Defaults[priority]
			plans = append(plans, view{Priority: priority, Before: FormatBefore(def.Before), FollowUp: def.FollowUp, IsDefault: true})
		}
		return c.JSON(plans)
	}
}

func priorityParam(c fiber.Ctx) (string, error) {
	priority := c.Params("priority")
	if !ValidPriority(priority) {
		return "", apierr.NotFound()
	}
	return priority, nil
}

func put(d *deps.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		priority, err := priorityParam(c)
		if err != nil {
			return err
		}
		var in struct {
			Before   *[]string `json:"before"`
			FollowUp string    `json:"follow_up"`
		}
		if err := httpx.Body(c, &in); err != nil {
			return err
		}
		fields := map[string]string{}
		var before []time.Duration
		if in.Before == nil {
			fields["before"] = "Is required. Send an empty list for no reminders before the deadline."
		} else if before, err = NormalizeBefore(*in.Before); err != nil {
			fields["before"] = err.Error()
		}
		if !ValidFollowUp(in.FollowUp) {
			fields["follow_up"] = `Must be "never", "daily" or "twice_daily".`
		}
		if len(fields) > 0 {
			return apierr.Validation(fields)
		}
		seconds := make([]int32, 0, len(before))
		for _, b := range before {
			seconds = append(seconds, int32(b/time.Second))
		}
		err = d.Q().UpsertReminderPlan(c.Context(), sqlc.UpsertReminderPlanParams{
			UserID:        auth.Target(c),
			Priority:      priority,
			BeforeSeconds: seconds,
			FollowUp:      in.FollowUp,
		})
		if err != nil {
			return err
		}
		return c.JSON(view{Priority: priority, Before: FormatBefore(before), FollowUp: in.FollowUp})
	}
}

func reset(d *deps.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		priority, err := priorityParam(c)
		if err != nil {
			return err
		}
		err = d.Q().DeleteReminderPlan(c.Context(), sqlc.DeleteReminderPlanParams{UserID: auth.Target(c), Priority: priority})
		if err != nil {
			return err
		}
		return c.SendStatus(http.StatusNoContent)
	}
}
