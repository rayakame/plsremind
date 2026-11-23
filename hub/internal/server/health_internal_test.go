package server

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestReadinessAsksTheDatabaseAtMostOnceASecond(t *testing.T) {
	var pings atomic.Int32
	now := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	r := &readiness{
		ping: func(context.Context) error { pings.Add(1); return nil },
		now:  func() time.Time { return now },
	}
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() { require.True(t, r.ready(context.Background())) })
	}
	wg.Wait()
	require.Equal(t, int32(1), pings.Load())
	now = now.Add(time.Second)
	require.True(t, r.ready(context.Background()))
	require.Equal(t, int32(2), pings.Load())
}

func TestReadinessGivesUpOnADatabaseThatHangs(t *testing.T) {
	r := &readiness{
		ping: func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() },
		now:  time.Now,
	}
	start := time.Now()
	require.False(t, r.ready(context.Background()))
	require.Less(t, time.Since(start), 3*time.Second)
}

func TestReadinessRemembersAFailureForASecondToo(t *testing.T) {
	var pings atomic.Int32
	r := &readiness{
		ping: func(context.Context) error { pings.Add(1); return errors.New("away") },
		now:  func() time.Time { return time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC) },
	}
	require.False(t, r.ready(context.Background()))
	require.False(t, r.ready(context.Background()))
	require.Equal(t, int32(1), pings.Load())
}
