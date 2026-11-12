package reminder_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/rayakame/plsremind/hub/internal/reminder"
)

func TestNormalizeBefore(t *testing.T) {
	got, err := reminder.NormalizeBefore([]string{"1h", "2d", "1h", "60m"})
	require.NoError(t, err)
	require.Equal(t, []time.Duration{48 * time.Hour, time.Hour}, got)

	got, err = reminder.NormalizeBefore(nil)
	require.NoError(t, err)
	require.Empty(t, got)

	for _, bad := range [][]string{
		{"30s"}, {"366d"}, {"soon"},
		{"1m", "2m", "3m", "4m", "5m", "6m", "7m", "8m", "9m", "10m", "11m"},
	} {
		_, err := reminder.NormalizeBefore(bad)
		require.Error(t, err, bad)
	}
	_, err = reminder.NormalizeBefore([]string{"1m", "365d"})
	require.NoError(t, err)
}

func TestDefaults(t *testing.T) {
	require.Equal(t, []time.Duration{7 * 24 * time.Hour, 3 * 24 * time.Hour, 24 * time.Hour, 3 * time.Hour}, reminder.DefaultPlans["high"].Before)
	require.Equal(t, "twice_daily", reminder.DefaultPlans["high"].FollowUp)
	require.Equal(t, "daily", reminder.DefaultPlans["normal"].FollowUp)
	require.Equal(t, "never", reminder.DefaultPlans["low"].FollowUp)
	require.Equal(t, 7*24*time.Hour, reminder.FollowUpWindow)
}
