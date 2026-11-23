package delivery_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"

	"github.com/rayakame/plsremind/hub/internal/delivery"
	"github.com/rayakame/plsremind/hub/internal/testkit"
)

func lateness(t *testing.T, e *testkit.Env) (uint64, float64) {
	t.Helper()
	families, err := e.Deps.Metrics.Registry().Gather()
	require.NoError(t, err)
	for _, family := range families {
		if family.GetName() == "plsremind_reminder_lateness_seconds" {
			h := family.GetMetric()[0].GetHistogram()
			return h.GetSampleCount(), h.GetSampleSum()
		}
	}
	return 0, 0
}

func TestAFiredReminderIsCountedWithHowLateItWent(t *testing.T) {
	e := testkit.New(t)
	mia, _ := testkit.Reminded(t, e, "mia")
	mia.Do(t, "POST", "/users/@me/items", J{"title": "x", "priority": "low", "due_at": "2026-11-11T13:00:00Z"})
	e.Clock.Advance(time.Hour + 90*time.Second)
	e.Fire(t)
	require.Equal(t, 1.0, testutil.ToFloat64(e.Deps.Metrics.Reminders.WithLabelValues("due", "fired")))
	count, sum := lateness(t, e)
	require.Equal(t, uint64(1), count)
	require.InDelta(t, 90, sum, 0.001)
}

func TestAReminderOlderThanADayIsCountedAsMissed(t *testing.T) {
	e := testkit.New(t)
	mia, _ := testkit.Reminded(t, e, "mia")
	mia.Do(t, "POST", "/users/@me/items", J{"title": "x", "priority": "low", "due_at": "2026-11-11T13:00:00Z"})
	e.Clock.Advance(26 * time.Hour)
	e.Fire(t)
	require.Equal(t, 1.0, testutil.ToFloat64(e.Deps.Metrics.Reminders.WithLabelValues("due", "missed")))
	count, _ := lateness(t, e)
	require.Zero(t, count, "a reminder that did not go out has no lateness")
}

func TestExpiredDeliveriesAreCounted(t *testing.T) {
	e := testkit.New(t)
	mia, _ := testkit.Reminded(t, e, "mia")
	mia.Do(t, "POST", "/users/@me/items", J{"title": "x", "priority": "low", "due_at": "2026-11-11T13:00:00Z"})
	e.Clock.Advance(time.Hour)
	e.Fire(t)
	require.Contains(t, scrape(t, e), "plsremind_deliveries_open 1")
	e.Clock.Advance(24*time.Hour + time.Second)
	e.Fire(t)
	require.Equal(t, 1.0, testutil.ToFloat64(e.Deps.Metrics.Expired.WithLabelValues("personal_token")))
	require.Zero(t, testutil.ToFloat64(e.Deps.Metrics.Expired.WithLabelValues("connection")))
	require.Contains(t, scrape(t, e), "plsremind_deliveries_open 0")
}

func TestAcknowledgementsAreCountedOncePerDelivery(t *testing.T) {
	e := testkit.New(t)
	mia := e.User(t, "mia")
	bot := e.Connection(t, "discord")
	e.GiveIdentity(t, mia.UserID, "discord", "1234")
	testkit.DueSoon(t, e, mia)
	testkit.DueSoon(t, e, mia)
	open := bot.Do(t, "GET", "/deliveries", nil)
	first, second := open.Get("0.id").String(), open.Get("1.id").String()
	bot.Do(t, "PUT", "/deliveries/"+first, J{"status": "delivered"})
	bot.Do(t, "PUT", "/deliveries/"+first, J{"status": "delivered"})
	bot.Do(t, "PUT", "/deliveries/"+second, J{"status": "failed", "reason": "closed DMs"})
	m := e.Deps.Metrics
	require.Equal(t, 1.0, testutil.ToFloat64(m.Acknowledged.WithLabelValues("delivered")), "confirming again is not counted")
	require.Equal(t, 1.0, testutil.ToFloat64(m.Acknowledged.WithLabelValues("failed")))
}

func scrape(t *testing.T, e *testkit.Env) string {
	t.Helper()
	srv := httptest.NewServer(e.Deps.Metrics.Handler())
	defer srv.Close()
	res, err := http.Get(srv.URL)
	require.NoError(t, err)
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	return string(body)
}

func TestAReminderMovedByQuietHoursIsNotCountedAsLate(t *testing.T) {
	e := testkit.New(t)
	mia, _ := testkit.Reminded(t, e, "mia")
	mia.Do(t, "PATCH", "/users/@me", J{"quiet_hours": J{"start": "12:30", "end": "14:00"}})
	mia.Do(t, "POST", "/users/@me/items", J{"title": "x", "priority": "low", "due_at": "2026-11-11T16:00:00Z",
		"reminder_override": J{"mode": "custom", "before": []string{"3h"}}})
	e.Clock.Advance(2 * time.Hour)
	e.Fire(t)
	count, sum := lateness(t, e)
	require.Equal(t, uint64(1), count)
	require.Zero(t, sum, "an hour after 13:00, but at the end of the quiet hours it was meant for")
}

func TestABatchThatRollsBackIsNotCounted(t *testing.T) {
	e := testkit.New(t)
	mia, _ := testkit.Reminded(t, e, "mia")
	mia.Do(t, "POST", "/users/@me/items", J{"title": "x", "priority": "low", "due_at": "2026-11-11T13:00:00Z"})
	e.Clock.Advance(time.Hour)
	ctx := context.Background()
	_, err := e.Pool.Exec(ctx, `CREATE FUNCTION refuse() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'refused'; END $$;
		CREATE TRIGGER refuse BEFORE DELETE ON scheduled_reminders FOR EACH ROW EXECUTE FUNCTION refuse();`)
	require.NoError(t, err)
	require.Error(t, delivery.RunOnce(ctx, e.Deps))
	require.Zero(t, testutil.ToFloat64(e.Deps.Metrics.Reminders.WithLabelValues("due", "fired")), "the batch did not happen")

	_, err = e.Pool.Exec(ctx, `DROP TRIGGER refuse ON scheduled_reminders`)
	require.NoError(t, err)
	e.Fire(t)
	require.Equal(t, 1.0, testutil.ToFloat64(e.Deps.Metrics.Reminders.WithLabelValues("due", "fired")))
}

func TestExpiredDeliveriesToAConnectionAreCountedAsSuch(t *testing.T) {
	e := testkit.New(t)
	mia := e.User(t, "mia")
	e.Connection(t, "discord")
	e.GiveIdentity(t, mia.UserID, "discord", "1234")
	testkit.DueSoon(t, e, mia)
	e.Clock.Advance(24*time.Hour + time.Second)
	e.Fire(t)
	require.Equal(t, 1.0, testutil.ToFloat64(e.Deps.Metrics.Expired.WithLabelValues("connection")))
	require.Zero(t, testutil.ToFloat64(e.Deps.Metrics.Expired.WithLabelValues("personal_token")))
}
