package reminder

import (
	"context"
	"net/http"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"github.com/rayakame/plsremind/hub/internal/api"
	"github.com/rayakame/plsremind/hub/internal/auth"
	"github.com/rayakame/plsremind/hub/internal/core"
	"github.com/rayakame/plsremind/hub/internal/events"
	"github.com/rayakame/plsremind/hub/internal/store"
	"github.com/rayakame/plsremind/hub/internal/store/sqlc"
)

type planView struct {
	Priority  string   `json:"priority"`
	Before    []string `json:"before"`
	FollowUp  string   `json:"follow_up"`
	IsDefault bool     `json:"is_default"`
}

func Register(user fiber.Router, d *core.Deps) {
	plans := user.Group("/reminder-plans")
	plans.Get("", listPlans(d))
	plans.Put("/:priority", putPlan(d))
	plans.Delete("/:priority", resetPlan(d))
}

// views returns the user's three plans in the order low, normal, high.
func planViews(ctx context.Context, q *sqlc.Queries, userID uuid.UUID) ([]planView, error) {
	rows, err := q.ListReminderPlans(ctx, userID)
	if err != nil {
		return nil, err
	}
	custom := make(map[string]sqlc.ReminderPlan, len(rows))
	for _, row := range rows {
		custom[row.Priority] = row
	}
	plans := make([]planView, 0, len(Priorities))
	for _, priority := range Priorities {
		if row, ok := custom[priority]; ok {
			before := make([]time.Duration, 0, len(row.BeforeSeconds))
			for _, s := range row.BeforeSeconds {
				before = append(before, time.Duration(s)*time.Second)
			}
			plans = append(plans, planView{Priority: priority, Before: FormatBefore(before), FollowUp: row.FollowUp})
			continue
		}
		def := DefaultPlans[priority]
		plans = append(plans, planView{Priority: priority, Before: FormatBefore(def.Before), FollowUp: def.FollowUp, IsDefault: true})
	}
	return plans, nil
}

// Views returns the user's plans as the API shows them.
func PlanViews(ctx context.Context, q *sqlc.Queries, userID uuid.UUID) (any, error) {
	return planViews(ctx, q, userID)
}

func listPlans(d *core.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		plans, err := planViews(c.Context(), d.Q(), auth.Target(c))
		if err != nil {
			return err
		}
		return c.JSON(plans)
	}
}

// changedPlan records the new state of one plan and lets the schedule follow.
func changedPlan(c fiber.Ctx, d *core.Deps, q *sqlc.Queries, userID uuid.UUID, priority string) (planView, error) {
	plans, err := planViews(c.Context(), q, userID)
	if err != nil {
		return planView{}, err
	}
	var current planView
	for _, p := range plans {
		if p.Priority == priority {
			current = p
		}
	}
	now := d.Now()
	principal, _ := auth.From(c)
	if err := events.Append(c.Context(), q, now, userID, auth.ActorFrom(principal), "plan.updated", current); err != nil {
		return planView{}, err
	}
	return current, SyncUser(c.Context(), q, now, userID)
}

func priorityParam(c fiber.Ctx) (string, error) {
	priority := c.Params("priority")
	if !ValidPriority(priority) {
		return "", api.NotFound()
	}
	return priority, nil
}

func putPlan(d *core.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		priority, err := priorityParam(c)
		if err != nil {
			return err
		}
		var in struct {
			Before   *[]string `json:"before"`
			FollowUp string    `json:"follow_up"`
		}
		if err := api.Body(c, &in); err != nil {
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
			return api.Validation(fields)
		}
		seconds := make([]int32, 0, len(before))
		for _, b := range before {
			seconds = append(seconds, int32(b/time.Second))
		}
		userID := auth.Target(c)
		var saved planView
		err = store.UserTx(c.Context(), d.Pool, userID, func(q *sqlc.Queries) error {
			err := q.UpsertReminderPlan(c.Context(), sqlc.UpsertReminderPlanParams{
				UserID:        userID,
				Priority:      priority,
				BeforeSeconds: seconds,
				FollowUp:      in.FollowUp,
			})
			if err != nil {
				return err
			}
			saved, err = changedPlan(c, d, q, userID, priority)
			return err
		})
		if err != nil {
			return err
		}
		return c.JSON(saved)
	}
}

func resetPlan(d *core.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		priority, err := priorityParam(c)
		if err != nil {
			return err
		}
		userID := auth.Target(c)
		err = store.UserTx(c.Context(), d.Pool, userID, func(q *sqlc.Queries) error {
			if err := q.DeleteReminderPlan(c.Context(), sqlc.DeleteReminderPlanParams{UserID: userID, Priority: priority}); err != nil {
				return err
			}
			_, err := changedPlan(c, d, q, userID, priority)
			return err
		})
		if err != nil {
			return err
		}
		return c.SendStatus(http.StatusNoContent)
	}
}
