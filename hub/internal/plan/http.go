package plan

import (
	"context"
	"net/http"
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

type view struct {
	Priority  string   `json:"priority"`
	Before    []string `json:"before"`
	FollowUp  string   `json:"follow_up"`
	IsDefault bool     `json:"is_default"`
}

// Changed is called in the transaction that changed a user's plan.
type Changed func(ctx context.Context, q *sqlc.Queries, now time.Time, userID uuid.UUID) error

func Register(user fiber.Router, d *deps.Deps, changed Changed) {
	plans := user.Group("/reminder-plans")
	plans.Get("", index(d))
	plans.Put("/:priority", put(d, changed))
	plans.Delete("/:priority", reset(d, changed))
}

// views returns the user's three plans in the order low, normal, high.
func views(ctx context.Context, q *sqlc.Queries, userID uuid.UUID) ([]view, error) {
	rows, err := q.ListReminderPlans(ctx, userID)
	if err != nil {
		return nil, err
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
	return plans, nil
}

// Views returns the user's plans as the API shows them.
func Views(ctx context.Context, q *sqlc.Queries, userID uuid.UUID) (any, error) {
	return views(ctx, q, userID)
}

func index(d *deps.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		plans, err := views(c.Context(), d.Q(), auth.Target(c))
		if err != nil {
			return err
		}
		return c.JSON(plans)
	}
}

// changedPlan records the new state of one plan and lets the schedule follow.
func changedPlan(c fiber.Ctx, d *deps.Deps, q *sqlc.Queries, changed Changed, userID uuid.UUID, priority string) (view, error) {
	plans, err := views(c.Context(), q, userID)
	if err != nil {
		return view{}, err
	}
	var current view
	for _, p := range plans {
		if p.Priority == priority {
			current = p
		}
	}
	now := d.Now()
	principal, _ := auth.From(c)
	if err := eventlog.Append(c.Context(), q, now, userID, eventlog.ActorFrom(principal), "plan.updated", current); err != nil {
		return view{}, err
	}
	return current, changed(c.Context(), q, now, userID)
}

func priorityParam(c fiber.Ctx) (string, error) {
	priority := c.Params("priority")
	if !ValidPriority(priority) {
		return "", apierr.NotFound()
	}
	return priority, nil
}

func put(d *deps.Deps, changed Changed) fiber.Handler {
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
		userID := auth.Target(c)
		var saved view
		err = db.Tx(c.Context(), d.Pool, func(q *sqlc.Queries) error {
			err := q.UpsertReminderPlan(c.Context(), sqlc.UpsertReminderPlanParams{
				UserID:        userID,
				Priority:      priority,
				BeforeSeconds: seconds,
				FollowUp:      in.FollowUp,
			})
			if err != nil {
				return err
			}
			saved, err = changedPlan(c, d, q, changed, userID, priority)
			return err
		})
		if err != nil {
			return err
		}
		return c.JSON(saved)
	}
}

func reset(d *deps.Deps, changed Changed) fiber.Handler {
	return func(c fiber.Ctx) error {
		priority, err := priorityParam(c)
		if err != nil {
			return err
		}
		userID := auth.Target(c)
		err = db.Tx(c.Context(), d.Pool, func(q *sqlc.Queries) error {
			if err := q.DeleteReminderPlan(c.Context(), sqlc.DeleteReminderPlanParams{UserID: userID, Priority: priority}); err != nil {
				return err
			}
			_, err := changedPlan(c, d, q, changed, userID, priority)
			return err
		})
		if err != nil {
			return err
		}
		return c.SendStatus(http.StatusNoContent)
	}
}
