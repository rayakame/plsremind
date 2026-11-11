package duration_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/rayakame/plsremind/hub/internal/duration"
)

func TestParse(t *testing.T) {
	for in, want := range map[string]time.Duration{
		"3d": 72 * time.Hour, "1d12h": 36 * time.Hour, "90m": 90 * time.Minute, "2h": 2 * time.Hour,
	} {
		got, err := duration.Parse(in)
		require.NoError(t, err, in)
		require.Equal(t, want, got, in)
	}
	for _, bad := range []string{"", "0m", "3", "h", "1h1d", "1.5h", "2 h", "-1h", "1w", "1h1h", "99999999999999999999d"} {
		_, err := duration.Parse(bad)
		require.Error(t, err, bad)
	}
}

func TestFormat(t *testing.T) {
	for d, want := range map[time.Duration]string{
		72 * time.Hour:     "3d",
		36 * time.Hour:     "1d12h",
		90 * time.Minute:   "1h30m",
		7 * 24 * time.Hour: "7d",
	} {
		require.Equal(t, want, duration.Format(d))
	}
}

func TestParseRejectsOverflow(t *testing.T) {
	_, err := duration.Parse("213504d")
	require.Error(t, err)
	_, err = duration.Parse("106752d")
	require.Error(t, err)
	_, err = duration.Parse("106751d2147483647m")
	require.Error(t, err)
}
