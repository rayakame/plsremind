package delivery

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/rayakame/plsremind/hub/internal/core"
	"github.com/rayakame/plsremind/hub/internal/events"
	"github.com/rayakame/plsremind/hub/internal/store"
	"github.com/rayakame/plsremind/hub/internal/store/sqlc"
)

// batchSize is how many items one transaction handles.
const batchSize = 200

// RunOnce fires every reminder whose time has come and expires deliveries that
// waited too long. The ticker calls it every few seconds.
func RunOnce(ctx context.Context, d *core.Deps) error {
	for {
		fired, err := fireBatch(ctx, d)
		if err != nil {
			return err
		}
		if fired == 0 {
			break
		}
	}
	return store.Tx(ctx, d.Pool, func(q *sqlc.Queries) error {
		now := d.Now()
		expired, err := q.ExpireDeliveries(ctx, &now)
		if err != nil {
			return err
		}
		if err := events.DeliveriesClosed(ctx, q, expired); err != nil {
			return err
		}
		// Deliveries that waited for quiet hours to end go out now.
		released, err := q.ReleaseHeldDeliveries(ctx, &now)
		if err != nil {
			return err
		}
		return announceNew(ctx, q, released)
	})
}

// fireBatch handles one batch of due rows in one transaction. The row locks
// make sure a reminder fires once even if two tickers run at the same time,
// and a crash before the commit leaves the rows for the next run.
func fireBatch(ctx context.Context, d *core.Deps) (int, error) {
	count := 0
	err := store.Tx(ctx, d.Pool, func(q *sqlc.Queries) error {
		now := d.Now()
		due, err := q.LockDueReminders(ctx, sqlc.LockDueRemindersParams{Now: now, Batch: batchSize})
		if err != nil {
			return err
		}
		count = len(due)

		// After the hub was away several reminders of one item can be due at once.
		// Only the newest still says something; older than a day says nothing.
		newest := map[uuid.UUID]sqlc.ScheduledReminder{}
		for _, row := range due {
			if now.Sub(row.FireAt) <= Lifetime {
				newest[row.ItemID] = row
			}
		}
		ids := make([]uuid.UUID, 0, len(due))
		for _, row := range due {
			ids = append(ids, row.ID)
			outcome := "superseded"
			switch {
			case now.Sub(row.FireAt) > Lifetime:
				outcome = "missed"
			case newest[row.ItemID].ID == row.ID:
				outcome = "fired"
			}
			reminderID := uuid.Must(uuid.NewV7())
			err := q.InsertReminderLog(ctx, sqlc.InsertReminderLogParams{
				ID:            reminderID,
				UserID:        row.UserID,
				ItemID:        row.ItemID,
				Kind:          row.Kind,
				OffsetSeconds: row.OffsetSeconds,
				ScheduledFor:  row.ScheduledFor,
				FireAt:        row.FireAt,
				FiredAt:       now,
				Outcome:       outcome,
			})
			if err != nil {
				return err
			}
			if outcome != "fired" {
				continue
			}
			if err := deliver(ctx, q, now, row, reminderID); err != nil {
				return err
			}
		}
		return q.DeleteScheduledReminders(ctx, ids)
	})
	return count, err
}

// deliver creates one delivery per channel. A reminder that is still waiting
// on a channel for the same item gives way to the new one.
func deliver(ctx context.Context, q *sqlc.Queries, now time.Time, row sqlc.ScheduledReminder, reminderID uuid.UUID) error {
	channels, err := Channels(ctx, q, row.UserID)
	if err != nil {
		return err
	}
	for _, ch := range channels {
		var replaced []sqlc.Delivery
		if ch.ConnectionID != nil {
			replaced, err = q.SupersedeConnectionDeliveries(ctx, sqlc.SupersedeConnectionDeliveriesParams{ItemID: row.ItemID, ConnectionID: ch.ConnectionID, Now: &now})
		} else {
			replaced, err = q.SupersedeTokenDeliveries(ctx, sqlc.SupersedeTokenDeliveriesParams{ItemID: row.ItemID, TokenID: ch.TokenID, Now: &now})
		}
		if err != nil {
			return err
		}
		if err := events.DeliveriesClosed(ctx, q, replaced); err != nil {
			return err
		}
		id := uuid.Must(uuid.NewV7())
		err = q.InsertDelivery(ctx, sqlc.InsertDeliveryParams{
			ID:           id,
			ReminderID:   reminderID,
			UserID:       row.UserID,
			ItemID:       row.ItemID,
			ConnectionID: ch.ConnectionID,
			TokenID:      ch.TokenID,
			CreatedAt:    now,
			ExpiresAt:    now.Add(Lifetime),
		})
		if err != nil {
			return err
		}
		if err := announceNew(ctx, q, []uuid.UUID{id}); err != nil {
			return err
		}
	}
	return nil
}
