package ratelimit_test

import (
	"testing"
	"time"
	"unsafe"

	"github.com/stretchr/testify/require"

	"github.com/rayakame/plsremind/hub/internal/ratelimit"
)

func TestLimiterWindow(t *testing.T) {
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	l := ratelimit.New(2, time.Minute, func() time.Time { return now })

	remaining, reset, ok := l.Allow("a")
	require.True(t, ok)
	require.Equal(t, 1, remaining)
	require.Equal(t, now.Add(time.Minute), reset)

	_, _, ok = l.Allow("a")
	require.True(t, ok)
	remaining, reset, ok = l.Allow("a")
	require.False(t, ok)
	require.Equal(t, 0, remaining)
	require.Equal(t, now.Add(time.Minute), reset)

	_, _, ok = l.Allow("b")
	require.True(t, ok, "keys are independent")

	now = now.Add(61 * time.Second)
	remaining, _, ok = l.Allow("a")
	require.True(t, ok)
	require.Equal(t, 1, remaining)
}

func TestLimiterKeepsItsOwnCopyOfTheKey(t *testing.T) {
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	l := ratelimit.New(2, time.Minute, func() time.Time { return now })

	// Request strings can point into a buffer that the server reuses for the next request.
	buf := []byte("203.0.113.7")
	l.Allow(unsafe.String(&buf[0], len(buf)))
	copy(buf, "198.51.100.9")

	remaining, _, ok := l.Allow("203.0.113.7")
	require.True(t, ok)
	require.Equal(t, 0, remaining, "the first request must still be counted for its address")
}
