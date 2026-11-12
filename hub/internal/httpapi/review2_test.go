package httpapi_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/rayakame/plsremind/hub/internal/gateway"
	"github.com/rayakame/plsremind/hub/internal/ratelimit"
	"github.com/rayakame/plsremind/hub/internal/testkit"
)

func TestListenerOutageIsMadeUp(t *testing.T) {
	e := testkit.New(t)
	bot := e.Connection(t, "discord")
	mia, ben := linked(t, e, "mia", "111"), linked(t, e, "ben", "222")
	gw := e.Gateway(t, bot.Token, "users", "deliveries")
	gw.Expect(t, "user.sync")
	gw.Expect(t, "user.sync")

	// The hub's signal connection dies, as it would when PostgreSQL restarts. What
	// happens before it is back must not be lost.
	e.KillListener(t)
	dueSoon(t, e, mia)
	ben.Do(t, "PUT", "/users/@me/connections/"+bot.ID, J{"enabled": false})

	got := map[string]testkit.Frame{}
	for len(got) < 2 {
		f := gw.NextEvent(t)
		if f.T == "delivery.created" || f.T == "user.gone" {
			got[f.T] = f
		}
	}
	require.Equal(t, mia.UserID, got["delivery.created"].Get("user_id").String())
	require.Equal(t, int64(1), got["delivery.created"].Get("data.attempt").Int())
	require.Equal(t, ben.UserID, got["user.gone"].Get("user_id").String())
	gw.ExpectQuiet(t)
}

func TestDatabaseTroubleDoesNotLookLikeRevokedAccess(t *testing.T) {
	e := testkit.New(t)
	gw := e.Gateway(t, personalToken(t, e.User(t, "mia")), "deliveries")
	ctx := context.Background()
	_, err := e.Pool.Exec(ctx, "ALTER TABLE personal_tokens RENAME TO personal_tokens_away")
	require.NoError(t, err)
	// Every heartbeat now fails to check the token. 4004 tells an adapter to give
	// up for good; a hiccup of the database must not say that.
	time.Sleep(4 * e.Deps.Gateway.HeartbeatInterval)
	_, err = e.Pool.Exec(ctx, "ALTER TABLE personal_tokens_away RENAME TO personal_tokens")
	require.NoError(t, err)
	gw.ExpectQuiet(t)
	gw.Send(t, gateway.OpHeartbeat, nil)
	require.Equal(t, gateway.OpHeartbeatACK, gw.Next(t).Op, "the session is still there")
}

func TestStuckClientDoesNotStallOtherUsers(t *testing.T) {
	e := testkit.New(t)
	e.Deps.RateAuth = ratelimit.New(100000, time.Minute, e.Clock.Now)
	e.Deps.Gateway.WriteTimeout = 4 * time.Second
	mia, ben := e.User(t, "mia"), e.User(t, "ben")
	stuck := e.Gateway(t, personalToken(t, mia), "items")
	stuck.Expect(t, "user.sync")
	other := e.Gateway(t, personalToken(t, ben), "items")
	other.Expect(t, "user.sync")

	// Mia's client keeps its heartbeat going but stops reading, and gets far more
	// than its socket can hold.
	stuck.PauseReading()
	note := strings.Repeat("x", 9000)
	for i := 0; i < 600; i++ {
		mia.Do(t, "POST", "/users/@me/items", J{"title": "x", "note": note}, testkit.NoContract())
	}

	// Ben's events, new logins and resumes must not wait for her.
	var worst time.Duration
	for i := 0; i < 15; i++ {
		started := time.Now()
		ben.Do(t, "POST", "/users/@me/items", J{"title": fmt.Sprintf("ben %d", i)})
		require.Equal(t, fmt.Sprintf("ben %d", i), other.Expect(t, "item.created").Get("data.title").String())
		worst = max(worst, time.Since(started))
		fresh := e.Gateway(t, personalToken(t, ben), "deliveries")
		fresh.Close()
		time.Sleep(100 * time.Millisecond)
	}
	require.Less(t, worst, time.Second, "another user's event took %s", worst)
}

func TestCatchUpPicksTheNewestAcrossBatches(t *testing.T) {
	e := testkit.New(t)
	mia, _ := reminded(t, e, "mia")
	e.Deps.RateAuth = ratelimit.New(100000, time.Minute, e.Clock.Now)
	target := mia.Do(t, "POST", "/users/@me/items", J{"title": "target"}).Get("id").String()
	mia.Do(t, "POST", "/users/@me/items/"+target+"/reminders", J{"in": "1h"})
	for i := 0; i < 210; i++ {
		id := mia.Do(t, "POST", "/users/@me/items", J{"title": "filler"}, testkit.NoContract()).Get("id").String()
		mia.Do(t, "POST", "/users/@me/items/"+id+"/reminders", J{"in": "2h"}, testkit.NoContract())
	}
	mia.Do(t, "POST", "/users/@me/items/"+target+"/reminders", J{"in": "3h"})
	e.Clock.Advance(4 * time.Hour)
	e.Fire(t)

	var outcomes []string
	rows, err := e.Pool.Query(context.Background(),
		"SELECT outcome FROM reminder_log WHERE item_id = $1 ORDER BY scheduled_for", target)
	require.NoError(t, err)
	for rows.Next() {
		var o string
		require.NoError(t, rows.Scan(&o))
		outcomes = append(outcomes, o)
	}
	require.Equal(t, []string{"superseded", "fired"}, outcomes, "only the newest reminder of an item goes out, however many are due")
	n := 0
	for _, d := range e.Deliveries(t, mia.UserID) {
		if d.ItemID == target {
			n++
		}
	}
	require.Equal(t, 1, n)
}

func TestDetachedSessionsDoNotCountAgainstTheLimit(t *testing.T) {
	e := testkit.New(t)
	e.Deps.Gateway.MaxSessions = 2
	token := personalToken(t, e.User(t, "mia"))
	// A client that reconnects with Identify instead of Resume, as the dashboard does on every page load.
	for i := 0; i < 6; i++ {
		gw := e.Gateway(t, token, "deliveries")
		gw.Close()
	}
	e.Gateway(t, token, "deliveries")
	e.Gateway(t, token, "deliveries")
	third := e.GatewayRaw(t)
	third.Next(t)
	third.Send(t, gateway.OpIdentify, J{"token": token, "intents": []string{"deliveries"}})
	third.ExpectClose(t, gateway.CloseRateLimited)
}

func TestNothingIsHandedOutOnAChannelThatNoLongerReminds(t *testing.T) {
	e := testkit.New(t)
	admin := e.Admin(t)
	bot := e.Connection(t, "discord")
	mia := linked(t, e, "mia", "111")
	dueSoon(t, e, mia)
	require.Equal(t, []string{"due pending"}, statuses(e.Deliveries(t, mia.UserID)))

	admin.Do(t, "PATCH", "/admin/users/"+mia.UserID, J{"disabled": true})
	require.Equal(t, int64(0), bot.Do(t, "GET", "/deliveries", nil).Get("#").Int())
	require.Equal(t, []string{"due cancelled"}, statuses(e.Deliveries(t, mia.UserID)))
}

func TestRoleChangeIsAnnounced(t *testing.T) {
	e := testkit.New(t)
	admin := e.Admin(t)
	mia := e.User(t, "mia")
	gw := e.Gateway(t, personalToken(t, mia), "users")
	gw.Expect(t, "user.sync")
	admin.Do(t, "PATCH", "/admin/users/"+mia.UserID, J{"role": "admin"})
	updated := gw.Expect(t, "user.updated")
	require.Equal(t, "admin", updated.Get("data.role").String())
	gw.Cache(t).RequireMatchesREST(t, mia, "/users/@me", mia.UserID)
}

func TestReminderMovedByQuietHoursIsNotLate(t *testing.T) {
	e := testkit.New(t)
	mia, token := reminded(t, e, "mia")
	mia.Do(t, "PATCH", "/users/@me", J{"quiet_hours": J{"start": "12:30", "end": "14:00"}})
	// "3h before" is meant for 13:00 and goes out when the window ends at 14:00.
	mia.Do(t, "POST", "/users/@me/items", J{"title": "x", "priority": "low", "due_at": "2026-11-11T16:00:00Z",
		"reminder_override": J{"mode": "custom", "before": []string{"3h"}}})
	e.Clock.Advance(2 * time.Hour)
	e.Fire(t)
	d := e.Do(t, "GET", "/deliveries", nil, token).Get("0")
	require.Equal(t, "2026-11-11T13:00:00Z", d.Get("reminder.scheduled_for").String())
	require.False(t, d.Get("late").Bool(), "it went out exactly when it was supposed to")
}

func TestQuietHoursDoNotBringBackAPassedMoment(t *testing.T) {
	e := testkit.New(t)
	u := e.User(t, "mia")
	u.Do(t, "PATCH", "/users/@me", J{"quiet_hours": J{"start": "11:00", "end": "14:00"}})
	// Created at 12:00. "5h before" was meant for 11:00, which has passed; that it
	// would have waited until 14:00 does not make it new.
	id := u.Do(t, "POST", "/users/@me/items", J{"title": "x", "priority": "low", "due_at": "2026-11-11T16:00:00Z",
		"reminder_override": J{"mode": "custom", "before": []string{"5h"}}}).Get("id").String()
	require.Equal(t, []string{"due 2026-11-11T16:00:00Z"}, fireTimes(e.Schedule(t, id)))
}

func TestLiftingQuietHoursFiresWhatWasWaiting(t *testing.T) {
	e := testkit.New(t)
	u, token := reminded(t, e, "mia")
	u.Do(t, "PATCH", "/users/@me", J{"quiet_hours": J{"start": "12:30", "end": "14:00"}})
	id := u.Do(t, "POST", "/users/@me/items", J{"title": "x", "priority": "low", "due_at": "2026-11-11T16:00:00Z",
		"reminder_override": J{"mode": "custom", "before": []string{"3h"}}}).Get("id").String()
	require.Equal(t, "before 3h 2026-11-11T14:00:00Z", fireTimes(e.Schedule(t, id))[0])

	e.Clock.Advance(90 * time.Minute) // 13:30, its moment has passed and it is waiting
	u.Do(t, "PATCH", "/users/@me", J{"quiet_hours": nil})
	e.Fire(t)
	require.Equal(t, []string{"before fired"}, e.ReminderLog(t, u.UserID), "with quiet hours gone there is nothing to wait for")
	require.Equal(t, "before", e.Do(t, "GET", "/deliveries", nil, token).Get("0.reminder.kind").String())
}

func TestLiftingQuietHoursReleasesHeldDeliveries(t *testing.T) {
	e := testkit.New(t)
	bot := e.Connection(t, "discord")
	mia := linked(t, e, "mia", "111")
	mia.Do(t, "POST", "/users/@me/items", J{"title": "overdue", "due_at": "2026-11-10T13:00:00Z"})
	e.Clock.Advance(time.Hour)
	e.Fire(t)
	mia.Do(t, "PATCH", "/users/@me", J{"quiet_hours": J{"start": "12:30", "end": "18:00"}})
	gw := e.Gateway(t, bot.Token, "deliveries")
	gw.ExpectQuiet(t)

	mia.Do(t, "PATCH", "/users/@me", J{"quiet_hours": nil})
	e.Fire(t)
	require.Equal(t, "follow_up", gw.Expect(t, "delivery.created").Get("data.reminder.kind").String())
}

func TestMovingADeadlineDropsItsDueReminder(t *testing.T) {
	// The reminder is due, the ticker has not come by yet, and the user moves the deadline.
	e := testkit.New(t)
	u, _ := reminded(t, e, "mia")
	id := u.Do(t, "POST", "/users/@me/items", J{"title": "x", "priority": "low", "due_at": "2026-11-11T13:00:00Z"}).Get("id").String()
	e.Clock.Advance(time.Hour + time.Second)
	u.Do(t, "PATCH", "/users/@me/items/"+id, J{"due_at": "2026-11-20T13:00:00Z"})
	e.Fire(t)
	require.Empty(t, e.ReminderLog(t, u.UserID), "the old deadline no longer exists")
	require.Equal(t, []string{"before 1d 2026-11-19T13:00:00Z", "due 2026-11-20T13:00:00Z"}, fireTimes(e.Schedule(t, id)))
}
