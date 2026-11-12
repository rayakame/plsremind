package delivery_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/rayakame/plsremind/hub/internal/store/sqlc"
	"github.com/rayakame/plsremind/hub/internal/testkit"
)

// Both tests run the ticker's statements by hand, in the order fireBatch runs
// them, to stop it between two of them.

func TestPasswordChangeDoesNotDeadlockWithTheTicker(t *testing.T) {
	e := testkit.New(t)
	mia, _ := testkit.Reminded(t, e, "mia")
	item := testkit.DueSoon(t, e, mia)
	require.Equal(t, 201, mia.Do(t, "POST", "/users/@me/items/"+item+"/reminders", J{"in": "1h"}).Status)
	e.Clock.Advance(time.Hour)
	pending := e.Deliveries(t, mia.UserID)
	require.Len(t, pending, 1)
	tokenID := uuid.MustParse(pending[0].ChannelID)

	ctx := context.Background()
	tx, err := e.Pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck
	q := sqlc.New(tx)
	now := e.Clock.Now()
	due, err := q.LockDueReminders(ctx, sqlc.LockDueRemindersParams{Now: now, Batch: 200})
	require.NoError(t, err)
	require.NotEmpty(t, due)
	row := due[len(due)-1]
	require.NoError(t, q.LockItemsDeliveries(ctx, []uuid.UUID{row.ItemID}))

	status := make(chan int, 1)
	go func() {
		r := mia.Do(t, "PUT", "/users/@me/password", J{"current_password": testkit.Password, "new_password": "another-password-1"})
		status <- r.Status
	}()
	time.Sleep(500 * time.Millisecond)

	reminderID := uuid.Must(uuid.NewV7())
	err = q.InsertReminderLog(ctx, sqlc.InsertReminderLogParams{
		ID: reminderID, UserID: row.UserID, ItemID: row.ItemID, Kind: row.Kind, OffsetSeconds: row.OffsetSeconds,
		ScheduledFor: row.ScheduledFor, FireAt: row.FireAt, FiredAt: now, Outcome: "fired",
	})
	if err == nil {
		err = q.InsertDelivery(ctx, sqlc.InsertDeliveryParams{
			ID: uuid.Must(uuid.NewV7()), ReminderID: reminderID, UserID: row.UserID, ItemID: row.ItemID,
			TokenID: &tokenID, CreatedAt: now, ExpiresAt: now.Add(24 * time.Hour),
		})
	}
	if err == nil {
		err = tx.Commit(ctx)
	} else {
		_ = tx.Rollback(ctx)
	}
	got := <-status
	require.NoError(t, err)
	require.Less(t, got, 500, "the two deadlocked")
}

func TestQuietHoursChangeDoesNotDeadlockWithTheTicker(t *testing.T) {
	e := testkit.New(t)
	bot := e.Connection(t, "discord")
	mia := testkit.Linked(t, e, "mia", "111")
	item := mia.Do(t, "POST", "/users/@me/items", J{"title": "overdue", "due_at": "2026-11-10T13:00:00Z"}).Get("id").String()
	e.Clock.Advance(time.Hour)
	e.Fire(t)
	mia.Do(t, "PATCH", "/users/@me", J{"quiet_hours": J{"start": "12:30", "end": "18:00"}})
	// Fetching during quiet hours puts the follow-up on hold.
	require.Equal(t, 200, bot.Do(t, "GET", "/deliveries", nil).Status)
	var held int
	require.NoError(t, e.Pool.QueryRow(context.Background(), "SELECT count(*) FROM deliveries WHERE held_until IS NOT NULL AND status = 'pending'").Scan(&held))
	require.Equal(t, 1, held, "setup: one delivery is held; now=%s", e.Clock.Now())
	require.Equal(t, 201, mia.Do(t, "POST", "/users/@me/items/"+item+"/reminders", J{"in": "30m"}).Status)
	e.Clock.Advance(30 * time.Minute)

	ctx := context.Background()
	tx, err := e.Pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck
	q := sqlc.New(tx)
	due, err := q.LockDueReminders(ctx, sqlc.LockDueRemindersParams{Now: e.Clock.Now(), Batch: 200})
	require.NoError(t, err)
	require.NotEmpty(t, due)

	status := make(chan int, 1)
	go func() {
		status <- mia.Do(t, "PATCH", "/users/@me", J{"quiet_hours": nil}).Status
	}()
	time.Sleep(500 * time.Millisecond)

	err = q.LockItemsDeliveries(ctx, []uuid.UUID{due[0].ItemID})
	if err == nil {
		err = tx.Commit(ctx)
	} else {
		_ = tx.Rollback(ctx)
	}
	got := <-status
	require.NoError(t, err)
	require.Less(t, got, 500, "the two deadlocked")
}
