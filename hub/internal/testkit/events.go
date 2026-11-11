package testkit

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type Event struct {
	Seq       int64
	Type      string
	Payload   gjson.Result
	ActorKind string
	ActorID   string
}

// Events returns the user's event log in order.
func (e *Env) Events(t *testing.T, userID string) []Event {
	t.Helper()
	rows, err := e.Pool.Query(context.Background(),
		"SELECT seq, type, payload::text, actor_kind, actor_id::text FROM events WHERE user_id = $1 ORDER BY seq", userID)
	require.NoError(t, err)
	defer rows.Close()
	var events []Event
	for rows.Next() {
		var ev Event
		var payload string
		require.NoError(t, rows.Scan(&ev.Seq, &ev.Type, &payload, &ev.ActorKind, &ev.ActorID))
		ev.Payload = gjson.Parse(payload)
		events = append(events, ev)
	}
	require.NoError(t, rows.Err())
	return events
}
