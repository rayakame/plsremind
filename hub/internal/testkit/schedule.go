package testkit

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/rayakame/plsremind/hub/internal/api"
)

type Scheduled struct {
	Kind   string
	Offset string
	FireAt time.Time
	For    time.Time
}

// Schedule returns the reminders the hub has planned for an item, earliest first.
func (e *Env) Schedule(t *testing.T, itemID string) []Scheduled {
	t.Helper()
	rows, err := e.Pool.Query(context.Background(),
		`SELECT kind, offset_seconds, fire_at, scheduled_for FROM scheduled_reminders
		 WHERE item_id = $1 ORDER BY fire_at, kind`, itemID)
	require.NoError(t, err)
	defer rows.Close()
	var out []Scheduled
	for rows.Next() {
		var s Scheduled
		var offset *int32
		require.NoError(t, rows.Scan(&s.Kind, &offset, &s.FireAt, &s.For))
		if offset != nil {
			s.Offset = api.FormatDuration(time.Duration(*offset) * time.Second)
		}
		out = append(out, s)
	}
	require.NoError(t, rows.Err())
	return out
}
