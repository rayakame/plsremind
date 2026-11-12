package testkit

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/rayakame/plsremind/hub/internal/delivery"
)

// Fire runs the reminder ticker once.
func (e *Env) Fire(t *testing.T) {
	t.Helper()
	require.NoError(t, delivery.RunOnce(context.Background(), e.Deps))
}

type Delivery struct {
	ID        string
	ItemID    string
	Kind      string
	Status    string
	Channel   string // "connection" or "token"
	ChannelID string
	Attempts  int
}

// Deliveries returns the user's deliveries in the order they were created.
func (e *Env) Deliveries(t *testing.T, userID string) []Delivery {
	t.Helper()
	rows, err := e.Pool.Query(context.Background(),
		`SELECT d.id::text, d.item_id::text, r.kind, d.status,
		        CASE WHEN d.connection_id IS NULL THEN 'token' ELSE 'connection' END,
		        COALESCE(d.connection_id, d.token_id)::text, d.attempts
		 FROM deliveries d JOIN reminder_log r ON r.id = d.reminder_id
		 WHERE d.user_id = $1 ORDER BY d.id`, userID)
	require.NoError(t, err)
	defer rows.Close()
	var out []Delivery
	for rows.Next() {
		var d Delivery
		require.NoError(t, rows.Scan(&d.ID, &d.ItemID, &d.Kind, &d.Status, &d.Channel, &d.ChannelID, &d.Attempts))
		out = append(out, d)
	}
	require.NoError(t, rows.Err())
	return out
}

// ReminderLog returns "kind outcome" for every reminder the hub handled, by scheduled time.
func (e *Env) ReminderLog(t *testing.T, userID string) []string {
	t.Helper()
	rows, err := e.Pool.Query(context.Background(),
		`SELECT kind || ' ' || outcome FROM reminder_log WHERE user_id = $1 ORDER BY scheduled_for, id`, userID)
	require.NoError(t, err)
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		require.NoError(t, rows.Scan(&s))
		out = append(out, s)
	}
	require.NoError(t, rows.Err())
	return out
}
