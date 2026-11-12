package testkit

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/rayakame/plsremind/hub/internal/events"
	"github.com/rayakame/plsremind/hub/internal/server"
)

// SignalTap listens to the signals the hub sends to its gateway.
type SignalTap struct {
	conn *pgx.Conn
}

func (e *Env) Signals(t *testing.T) *SignalTap {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, e.dbURL)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close(ctx) })
	_, err = conn.Exec(ctx, "LISTEN "+events.NotifyChannel)
	require.NoError(t, err)
	return &SignalTap{conn: conn}
}

func (s *SignalTap) wait(timeout time.Duration) (events.Signal, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	n, err := s.conn.WaitForNotification(ctx)
	if err != nil {
		return events.Signal{}, false
	}
	var out events.Signal
	_ = json.Unmarshal([]byte(n.Payload), &out)
	return out, true
}

func (s *SignalTap) Next(t *testing.T) events.Signal {
	t.Helper()
	out, ok := s.wait(3 * time.Second)
	require.True(t, ok, "no signal arrived")
	return out
}

// NextOf skips signals of other types.
func (s *SignalTap) NextOf(t *testing.T, typ string) events.Signal {
	t.Helper()
	for {
		out := s.Next(t)
		if out.Type == typ {
			return out
		}
	}
}

func (s *SignalTap) ExpectNone(t *testing.T) {
	t.Helper()
	out, ok := s.wait(150 * time.Millisecond)
	require.False(t, ok, "unexpected signal %+v", out)
}

// Sweep runs the hub's periodic cleanup once.
func (e *Env) Sweep(t *testing.T) {
	t.Helper()
	require.NoError(t, server.Sweep(context.Background(), e.Pool, e.Clock.Now()))
}
