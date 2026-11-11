package httpapi_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/rayakame/plsremind/hub/internal/testkit"
)

func TestDefaultPlans(t *testing.T) {
	r := testkit.New(t).User(t, "mia").Do(t, "GET", "/users/@me/reminder-plans", nil)
	require.Equal(t, 200, r.Status)
	require.Equal(t, int64(3), r.Get("#").Int())
	require.Equal(t, "low", r.Get("0.priority").String())
	require.JSONEq(t, `["1d"]`, r.Get("0.before").Raw)
	require.JSONEq(t, `["3d","1d"]`, r.Get("1.before").Raw)
	require.JSONEq(t, `["7d","3d","1d","3h"]`, r.Get("2.before").Raw)
	require.Equal(t, "twice_daily", r.Get("2.follow_up").String())
	require.Equal(t, "never", r.Get("0.follow_up").String())
	require.True(t, r.Get("1.is_default").Bool())
}

func TestCustomPlanAndReset(t *testing.T) {
	e := testkit.New(t)
	u := e.User(t, "mia")
	r := u.Do(t, "PUT", "/users/@me/reminder-plans/normal", J{"before": []string{"1h", "2d", "1h"}, "follow_up": "never"})
	require.Equal(t, 200, r.Status)
	require.JSONEq(t, `["2d","1h"]`, r.Get("before").Raw)
	require.Equal(t, "never", r.Get("follow_up").String())
	require.False(t, r.Get("is_default").Bool())

	all := u.Do(t, "GET", "/users/@me/reminder-plans", nil)
	require.JSONEq(t, `["2d","1h"]`, all.Get("1.before").Raw)
	require.True(t, all.Get("0.is_default").Bool())

	empty := u.Do(t, "PUT", "/users/@me/reminder-plans/low", J{"before": []string{}, "follow_up": "never"})
	require.Equal(t, 200, empty.Status)
	require.JSONEq(t, `[]`, empty.Get("before").Raw)

	require.Equal(t, 204, u.Do(t, "DELETE", "/users/@me/reminder-plans/normal", nil).Status)
	reset := u.Do(t, "GET", "/users/@me/reminder-plans", nil)
	require.JSONEq(t, `["3d","1d"]`, reset.Get("1.before").Raw)
	require.True(t, reset.Get("1.is_default").Bool())
}

func TestPlanValidation(t *testing.T) {
	e := testkit.New(t)
	u := e.User(t, "mia")
	eleven := []string{"1m", "2m", "3m", "4m", "5m", "6m", "7m", "8m", "9m", "10m", "11m"}
	for _, body := range []J{
		{"before": eleven, "follow_up": "daily"},
		{"before": []string{"30s"}, "follow_up": "daily"},
		{"before": []string{"366d"}, "follow_up": "daily"},
		{"before": []string{"1d"}, "follow_up": "hourly"},
		{"before": []string{"1d"}},
	} {
		r := u.Do(t, "PUT", "/users/@me/reminder-plans/high", body)
		require.Equal(t, 422, r.Status, body)
	}
	require.Equal(t, 404, u.Do(t, "PUT", "/users/@me/reminder-plans/urgent", J{"before": []string{"1d"}, "follow_up": "daily"}).Status)
	require.Equal(t, 404, u.Do(t, "DELETE", "/users/@me/reminder-plans/urgent", nil).Status)
}

func TestPlansAreIsolated(t *testing.T) {
	e := testkit.New(t)
	mia, ben := e.User(t, "mia"), e.User(t, "ben")
	mia.Do(t, "PUT", "/users/@me/reminder-plans/high", J{"before": []string{"1h"}, "follow_up": "never"})
	require.True(t, ben.Do(t, "GET", "/users/@me/reminder-plans", nil).Get("2.is_default").Bool())
}
