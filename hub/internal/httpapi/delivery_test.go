package httpapi_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/rayakame/plsremind/hub/internal/testkit"
)

// dueSoon creates a low-priority item that is due in one hour and lets that hour pass.
func dueSoon(t *testing.T, e *testkit.Env, u *testkit.Client) string {
	t.Helper()
	due := e.Clock.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	r := u.Do(t, "POST", "/users/@me/items", J{"title": "Abgabe", "priority": "low", "due_at": due})
	require.Equal(t, 201, r.Status)
	e.Clock.Advance(time.Hour)
	e.Fire(t)
	return r.Get("id").String()
}

func TestFetchAndAck(t *testing.T) {
	e := testkit.New(t)
	mia := e.User(t, "mia")
	bot := e.Connection(t, "discord")
	e.GiveIdentity(t, mia.UserID, "discord", "1234")
	item := dueSoon(t, e, mia)

	list := bot.Do(t, "GET", "/deliveries", nil)
	require.Equal(t, 200, list.Status)
	require.Equal(t, int64(1), list.Get("#").Int())
	d := list.Get("0")
	require.Equal(t, int64(1), d.Get("attempt").Int())
	require.False(t, d.Get("late").Bool())
	require.Equal(t, "due", d.Get("reminder.kind").String())
	require.Equal(t, "2026-11-11T13:00:00Z", d.Get("reminder.scheduled_for").String())
	require.Equal(t, item, d.Get("item.id").String())
	require.Equal(t, "Abgabe", d.Get("item.title").String())
	require.Equal(t, mia.UserID, d.Get("user.id").String())
	require.Equal(t, "UTC", d.Get("user.timezone").String())
	require.JSONEq(t, `["1234"]`, d.Get("user.external_ids").Raw)
	id := d.Get("id").String()

	ack := bot.Do(t, "PUT", "/deliveries/"+id, J{"status": "delivered"})
	require.Equal(t, 200, ack.Status)
	require.Equal(t, "delivered", ack.Get("status").String())
	require.Equal(t, int64(0), bot.Do(t, "GET", "/deliveries", nil).Get("#").Int())

	require.Equal(t, 200, bot.Do(t, "PUT", "/deliveries/"+id, J{"status": "delivered"}).Status, "confirming twice is fine")
	conflict := bot.Do(t, "PUT", "/deliveries/"+id, J{"status": "failed", "reason": "changed my mind"})
	require.Equal(t, 409, conflict.Status)
	require.Equal(t, "delivery_closed", conflict.Get("error.code").String())
}

func TestBeforeReminderNamesItsOffset(t *testing.T) {
	e := testkit.New(t)
	mia, token := reminded(t, e, "mia")
	mia.Do(t, "POST", "/users/@me/items", J{"title": "x", "priority": "low", "due_at": "2026-11-12T13:00:00Z"})
	e.Clock.Advance(time.Hour)
	e.Fire(t)
	d := e.Do(t, "GET", "/deliveries", nil, token).Get("0")
	require.Equal(t, "before", d.Get("reminder.kind").String())
	require.Equal(t, "1d", d.Get("reminder.offset").String())
	require.False(t, d.Get("user.external_ids").Exists(), "a personal token has no external account")
}

func TestFailedNeedsReason(t *testing.T) {
	e := testkit.New(t)
	mia, token := reminded(t, e, "mia")
	dueSoon(t, e, mia)
	id := e.Do(t, "GET", "/deliveries", nil, token).Get("0.id").String()

	require.Equal(t, 422, e.Do(t, "PUT", "/deliveries/"+id, J{"status": "failed"}, token).Status)
	require.Equal(t, 422, e.Do(t, "PUT", "/deliveries/"+id, J{"status": "maybe"}, token).Status)
	failed := e.Do(t, "PUT", "/deliveries/"+id, J{"status": "failed", "reason": "direct messages are closed"}, token)
	require.Equal(t, 200, failed.Status)

	log := mia.Do(t, "GET", "/users/@me/reminder-log", nil)
	require.Equal(t, "failed", log.Get("data.0.deliveries.0.status").String())
	require.Equal(t, "direct messages are closed", log.Get("data.0.deliveries.0.reason").String())
	require.Equal(t, int64(0), e.Do(t, "GET", "/deliveries", nil, token).Get("#").Int(), "a failed delivery is not tried again")
}

func TestDeliveriesAreIsolated(t *testing.T) {
	e := testkit.New(t)
	mia := e.User(t, "mia")
	bot := e.Connection(t, "discord")
	other := e.Connection(t, "discord")
	e.GiveIdentity(t, mia.UserID, "discord", "1234")
	mia.Do(t, "PUT", "/users/@me/connections/"+other.ID, J{"remind": false})
	plain := mia.Do(t, "POST", "/users/@me/tokens", J{"name": "script"}).Get("token").String()
	ben, benToken := reminded(t, e, "ben")
	_ = ben
	dueSoon(t, e, mia)
	id := bot.Do(t, "GET", "/deliveries", nil).Get("0.id").String()
	require.NotEmpty(t, id)

	require.Equal(t, int64(0), other.Do(t, "GET", "/deliveries", nil).Get("#").Int())
	require.Equal(t, 404, other.Do(t, "PUT", "/deliveries/"+id, J{"status": "delivered"}).Status)
	require.Equal(t, 404, e.Do(t, "PUT", "/deliveries/"+id, J{"status": "delivered"}, benToken).Status)
	require.Equal(t, 403, e.Do(t, "GET", "/deliveries", nil, testkit.Bearer(plain)).Status, "a token that is not a reminder channel")
	require.Equal(t, 403, mia.Do(t, "GET", "/deliveries", nil).Status, "a dashboard session is not a channel")
	require.Equal(t, 401, e.Do(t, "GET", "/deliveries", nil).Status)
	require.Equal(t, 404, bot.Do(t, "PUT", "/deliveries/not-a-uuid", J{"status": "delivered"}).Status)
	require.Equal(t, 200, bot.Do(t, "PUT", "/deliveries/"+id, J{"status": "delivered"}).Status)
}

func TestConnectionWithoutDeliveriesIntentCannotFetch(t *testing.T) {
	e := testkit.New(t)
	admin := e.Admin(t)
	bot := e.Connection(t, "discord")
	admin.Do(t, "PATCH", "/admin/connections/"+bot.ID, J{"allowed_intents": []string{"items"}})
	require.Equal(t, 403, bot.Do(t, "GET", "/deliveries", nil).Status)
}

func TestLateAndAttempt(t *testing.T) {
	e := testkit.New(t)
	mia, token := reminded(t, e, "mia")
	dueSoon(t, e, mia)
	first := e.Do(t, "GET", "/deliveries", nil, token).Get("0")
	require.Equal(t, []any{int64(1), false}, []any{first.Get("attempt").Int(), first.Get("late").Bool()})
	e.Clock.Advance(2 * time.Minute)
	second := e.Do(t, "GET", "/deliveries", nil, token).Get("0")
	require.Equal(t, []any{int64(2), true}, []any{second.Get("attempt").Int(), second.Get("late").Bool()})
}

func TestQuietHoursHoldRedelivery(t *testing.T) {
	e := testkit.New(t)
	mia, token := reminded(t, e, "mia")
	mia.Do(t, "PATCH", "/users/@me", J{"quiet_hours": J{"start": "12:30", "end": "14:00"}})
	// Overdue since yesterday: the daily follow-up is due at 13:00 and waits until 14:00.
	mia.Do(t, "POST", "/users/@me/items", J{"title": "overdue", "due_at": "2026-11-10T13:00:00Z"})
	e.Clock.Advance(2 * time.Hour)
	e.Fire(t)
	require.Equal(t, "follow_up", e.Do(t, "GET", "/deliveries", nil, token).Get("0.reminder.kind").String())

	// The next day, inside the quiet window, with yesterday's follow-up still unconfirmed.
	e.Clock.Advance(23 * time.Hour)
	require.Equal(t, int64(0), e.Do(t, "GET", "/deliveries", nil, token).Get("#").Int(), "a follow-up waits for the window to end")

	dueNow := e.Clock.Now().Add(time.Minute).UTC().Format(time.RFC3339)
	mia.Do(t, "POST", "/users/@me/items", J{"title": "due in the window", "priority": "low", "due_at": dueNow, "reminder_override": J{"mode": "custom", "before": []string{}}})
	e.Clock.Advance(time.Minute)
	e.Fire(t)
	inWindow := e.Do(t, "GET", "/deliveries", nil, token)
	require.Equal(t, int64(1), inWindow.Get("#").Int())
	require.Equal(t, "due", inWindow.Get("0.reminder.kind").String(), "the due reminder is never held back")

	e.Clock.Advance(59 * time.Minute)
	require.Equal(t, int64(2), e.Do(t, "GET", "/deliveries", nil, token).Get("#").Int())
}

func TestReminderLog(t *testing.T) {
	e := testkit.New(t)
	mia, _ := reminded(t, e, "mia")
	ben := e.User(t, "ben")
	first := dueSoon(t, e, mia)
	second := dueSoon(t, e, mia)

	log := mia.Do(t, "GET", "/users/@me/reminder-log", nil)
	require.Equal(t, 200, log.Status)
	require.Equal(t, []string{second, first}, ids(log, "data.#.item_id"), "newest first")
	entry := log.Get("data.0")
	require.Equal(t, []string{"due", "fired"}, []string{entry.Get("kind").String(), entry.Get("outcome").String()})
	require.Equal(t, "pending", entry.Get("deliveries.0.status").String())
	require.Equal(t, "token", entry.Get("deliveries.0.channel.kind").String())
	require.Equal(t, "homelab", entry.Get("deliveries.0.channel.name").String())

	require.Equal(t, []string{first}, ids(mia.Do(t, "GET", "/users/@me/reminder-log?item_id="+first, nil), "data.#.item_id"))
	page := mia.Do(t, "GET", "/users/@me/reminder-log?limit=1", nil)
	require.Equal(t, []string{second}, ids(page, "data.#.item_id"))
	next := mia.Do(t, "GET", "/users/@me/reminder-log?limit=1&cursor="+page.Get("next_cursor").String(), nil)
	require.Equal(t, []string{first}, ids(next, "data.#.item_id"))
	require.Empty(t, ids(ben.Do(t, "GET", "/users/@me/reminder-log", nil), "data.#.id"))
	require.Equal(t, 422, mia.Do(t, "GET", "/users/@me/reminder-log?item_id=nope", nil, testkit.NoContract()).Status)
}

func TestSweepPrunesOldLog(t *testing.T) {
	e := testkit.New(t)
	mia, _ := reminded(t, e, "mia")
	dueSoon(t, e, mia)
	e.Clock.Advance(29 * 24 * time.Hour)
	e.Sweep(t)
	require.Len(t, e.ReminderLog(t, mia.UserID), 1)
	e.Clock.Advance(2 * 24 * time.Hour)
	e.Sweep(t)
	require.Empty(t, e.ReminderLog(t, mia.UserID))
	require.Empty(t, e.Deliveries(t, mia.UserID))
}
