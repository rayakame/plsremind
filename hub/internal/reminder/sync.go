package reminder

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/rayakame/plsremind/hub/internal/api"
	"github.com/rayakame/plsremind/hub/internal/store"
	"github.com/rayakame/plsremind/hub/internal/store/sqlc"
)

func sinceMidnight(t pgtype.Time) time.Duration {
	return time.Duration(t.Microseconds) * time.Microsecond
}

// QuietOf returns the user's quiet window, or nil when quiet hours are off.
func QuietOf(u sqlc.User) *Quiet {
	if !u.QuietStart.Valid || !u.QuietEnd.Valid {
		return nil
	}
	return &Quiet{Start: sinceMidnight(u.QuietStart), End: sinceMidnight(u.QuietEnd)}
}

// LocationOf returns the user's time zone. The name was validated when it was set.
func LocationOf(u sqlc.User) *time.Location {
	loc, err := time.LoadLocation(u.Timezone)
	if err != nil {
		return time.UTC
	}
	return loc
}

// input gathers everything that decides an item's reminder times.
func scheduleInput(ctx context.Context, q *sqlc.Queries, row sqlc.Item) (ScheduleInput, error) {
	user, err := q.GetUser(ctx, row.UserID)
	if err != nil {
		return ScheduleInput{}, err
	}
	in := ScheduleInput{Open: true, Due: row.DueAt, Quiet: QuietOf(user), Loc: LocationOf(user)}
	if row.DueDate.Valid {
		dues, err := q.ListAllDayDue(ctx, []uuid.UUID{row.ID})
		if err != nil {
			return ScheduleInput{}, err
		}
		if len(dues) > 0 {
			in.Due = &dues[0].Due
		}
	}

	chosen := DefaultPlans[row.Priority]
	plans, err := q.ListReminderPlans(ctx, row.UserID)
	if err != nil {
		return ScheduleInput{}, err
	}
	for _, p := range plans {
		if p.Priority != row.Priority {
			continue
		}
		chosen = Plan{FollowUp: p.FollowUp}
		for _, seconds := range p.BeforeSeconds {
			chosen.Before = append(chosen.Before, time.Duration(seconds)*time.Second)
		}
	}
	in.Before, in.FollowUp = chosen.Before, chosen.FollowUp

	if row.ReminderOverride != nil {
		var override struct {
			Mode   string   `json:"mode"`
			Before []string `json:"before"`
		}
		if err := json.Unmarshal(row.ReminderOverride, &override); err != nil {
			return ScheduleInput{}, err
		}
		switch override.Mode {
		case "off":
			in.Off = true
		case "custom":
			in.Before = nil
			for _, raw := range override.Before {
				offset, err := api.ParseDuration(raw)
				if err != nil {
					return ScheduleInput{}, err
				}
				in.Before = append(in.Before, offset)
			}
		}
	}

	manual, err := q.ListManualReminders(ctx, row.ID)
	if err != nil {
		return ScheduleInput{}, err
	}
	for _, m := range manual {
		in.Manual = append(in.Manual, Manual{ID: m.ID, At: m.RemindAt})
	}
	return in, nil
}

func scheduleKey(kind string, scheduledFor time.Time) string {
	return kind + " " + scheduledFor.UTC().Format(time.RFC3339Nano)
}

// SyncItem brings the schedule of one item in line with what the item, the
// user's plan and their quiet hours now say. It runs in the transaction that
// changed any of those.
//
// A reminder is scheduled if the moment it is meant for is still ahead, or if
// it was already scheduled and has not gone out: quiet hours can hold a
// reminder past its moment, and it must survive an edit in that time. What
// went out before is never scheduled again, and a moment that had passed
// before the reminder ever existed is not made up.
func SyncItem(ctx context.Context, q *sqlc.Queries, now time.Time, itemID uuid.UUID) error {
	row, err := q.GetItemByID(ctx, itemID)
	if store.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if row.Status != "open" {
		return q.DeleteItemSchedule(ctx, itemID)
	}
	in, err := scheduleInput(ctx, q, row)
	if err != nil {
		return err
	}
	planned := ComputeAll(in)
	wanted := make(map[string]bool, len(planned))
	for _, p := range planned {
		wanted[scheduleKey(string(p.Kind), p.For)] = true
	}

	existing, err := q.ListItemSchedule(ctx, itemID)
	if err != nil {
		return err
	}
	// Rows whose time has come belong to the ticker and stay, unless the item no
	// longer has that reminder at all, as after its deadline was moved.
	scheduled := make(map[string]bool, len(existing))
	waiting := map[string]bool{}
	var obsolete []uuid.UUID
	for _, e := range existing {
		k := scheduleKey(e.Kind, e.ScheduledFor)
		scheduled[k] = true
		switch {
		case e.FireAt.After(now):
		case wanted[k]:
			waiting[k] = true
		default:
			obsolete = append(obsolete, e.ID)
		}
	}
	if err := q.DeleteScheduledReminders(ctx, obsolete); err != nil {
		return err
	}
	if err := q.DeleteFutureSchedule(ctx, sqlc.DeleteFutureScheduleParams{ItemID: itemID, Now: now}); err != nil {
		return err
	}
	handled, err := q.ListHandledReminders(ctx, itemID)
	if err != nil {
		return err
	}
	done := make(map[string]bool, len(handled))
	for _, h := range handled {
		done[scheduleKey(h.Kind, h.ScheduledFor)] = true
	}

	for _, p := range planned {
		k := scheduleKey(string(p.Kind), p.For)
		if done[k] || waiting[k] {
			continue
		}
		if !p.For.After(now) && !scheduled[k] {
			continue
		}
		params := sqlc.InsertScheduledReminderParams{
			ID:           uuid.Must(uuid.NewV7()),
			ItemID:       itemID,
			UserID:       row.UserID,
			Kind:         string(p.Kind),
			FireAt:       p.At,
			ScheduledFor: p.For,
		}
		switch p.Kind {
		case KindBefore:
			seconds := int32(p.Offset / time.Second)
			params.OffsetSeconds = &seconds
		case KindManual:
			params.ManualID = &p.ManualID
		}
		if err := q.InsertScheduledReminder(ctx, params); err != nil {
			return err
		}
	}
	return nil
}

// SyncUser rewrites the upcoming reminders of all open items of a user, after
// a change to their plans, time zone or quiet hours.
func SyncUser(ctx context.Context, q *sqlc.Queries, now time.Time, userID uuid.UUID) error {
	ids, err := q.ListOpenItemIDs(ctx, userID)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := SyncItem(ctx, q, now, id); err != nil {
			return err
		}
	}
	return nil
}
