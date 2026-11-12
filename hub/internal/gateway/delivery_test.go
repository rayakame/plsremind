package gateway_test

import (
	"context"
	"math/rand"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/rayakame/plsremind/hub/internal/api"
	"github.com/rayakame/plsremind/hub/internal/events"
	"github.com/rayakame/plsremind/hub/internal/gateway"
	"github.com/rayakame/plsremind/hub/internal/testkit"
)

func dueIn(e *testkit.Env, d time.Duration) string {
	return e.Clock.Now().Add(d).UTC().Format(time.RFC3339)
}

func TestDeliveryArrivesAndIsAcked(t *testing.T) {
	e := testkit.New(t)
	bot := e.Connection(t, "discord")
	mia := testkit.Linked(t, e, "mia", "111")
	gw := e.Gateway(t, bot.Token, "deliveries")
	item := testkit.DueSoon(t, e, mia)

	d := gw.Expect(t, "delivery.created")
	require.Equal(t, gjson.Null, gjson.Parse(d.D.Raw).Get("actor").Type)
	require.Equal(t, int64(0), d.S, "deliveries are not part of the numbered sequence")
	require.Equal(t, mia.UserID, d.Get("user_id").String())
	require.Equal(t, int64(1), d.Get("data.attempt").Int())
	require.False(t, d.Get("data.late").Bool())
	require.Equal(t, "due", d.Get("data.reminder.kind").String())
	require.Equal(t, item, d.Get("data.item.id").String())
	require.JSONEq(t, `["111"]`, d.Get("data.user.external_ids").Raw)

	ack := bot.Do(t, "PUT", "/deliveries/"+d.Get("data.id").String(), J{"status": "delivered"})
	require.Equal(t, 200, ack.Status)
	gw.Close()
	again := e.Gateway(t, bot.Token, "deliveries")
	again.ExpectQuiet(t)
}

func TestOfflineChannelGetsNewestOnly(t *testing.T) {
	// The bot is away while "1 day before" and "3 hours before" both come due.
	e := testkit.New(t)
	bot := e.Connection(t, "discord")
	mia := testkit.Linked(t, e, "mia", "111")
	mia.Do(t, "POST", "/users/@me/items", J{"title": "Abgabe", "priority": "high", "due_at": dueIn(e, 26*time.Hour)})
	done := mia.Do(t, "POST", "/users/@me/items", J{"title": "schon erledigt", "priority": "high", "due_at": dueIn(e, 26*time.Hour)}).Get("id").String()

	e.Clock.Advance(2 * time.Hour) // 1d before
	e.Fire(t)
	mia.Do(t, "PATCH", "/users/@me/items/"+done, J{"status": "done"})
	e.Clock.Advance(21 * time.Hour) // 3h before
	e.Fire(t)
	e.Clock.Advance(time.Hour)

	gw := e.Gateway(t, bot.Token, "deliveries")
	d := gw.Expect(t, "delivery.created")
	require.Equal(t, "Abgabe", d.Get("data.item.title").String())
	require.Equal(t, []string{"before", "3h"}, []string{d.Get("data.reminder.kind").String(), d.Get("data.reminder.offset").String()})
	require.True(t, d.Get("data.late").Bool())
	require.Equal(t, int64(1), d.Get("data.attempt").Int())
	gw.ExpectQuiet(t)
}

func TestUnackedIsRedeliveredOnlyOnLogin(t *testing.T) {
	e := testkit.New(t)
	bot := e.Connection(t, "discord")
	mia := testkit.Linked(t, e, "mia", "111")
	gw := e.Gateway(t, bot.Token, "deliveries")
	testkit.DueSoon(t, e, mia)
	require.Equal(t, int64(1), gw.Expect(t, "delivery.created").Get("data.attempt").Int())
	for i := 0; i < 3; i++ {
		e.Clock.Advance(10 * time.Minute)
		e.Fire(t)
	}
	gw.ExpectQuiet(t)
	gw.Close()

	again := e.Gateway(t, bot.Token, "deliveries")
	second := again.Expect(t, "delivery.created")
	require.Equal(t, int64(2), second.Get("data.attempt").Int())
	require.True(t, second.Get("data.late").Bool())
}

func TestClosedIsAnnounced(t *testing.T) {
	e := testkit.New(t)
	bot := e.Connection(t, "discord")
	mia := testkit.Linked(t, e, "mia", "111")
	gw := e.Gateway(t, bot.Token, "deliveries")

	first := testkit.DueSoon(t, e, mia)
	created := gw.Expect(t, "delivery.created")
	mia.Do(t, "PATCH", "/users/@me/items/"+first, J{"status": "done"})
	closed := gw.Expect(t, "delivery.closed")
	require.Equal(t, created.Get("data.id").String(), closed.Get("data.id").String())
	require.Equal(t, "cancelled", closed.Get("data.status").String())
	require.Equal(t, mia.UserID, closed.Get("user_id").String())

	mia.Do(t, "POST", "/users/@me/items", J{"title": "x", "priority": "low", "due_at": dueIn(e, 25*time.Hour)})
	e.Clock.Advance(time.Hour)
	e.Fire(t)
	before := gw.Expect(t, "delivery.created")
	e.Clock.Advance(24 * time.Hour)
	e.Fire(t)
	types := map[string]testkit.Frame{}
	for i := 0; i < 2; i++ {
		f := gw.NextEvent(t)
		types[f.T] = f
	}
	require.Equal(t, before.Get("data.id").String(), types["delivery.closed"].Get("data.id").String())
	require.Equal(t, "superseded", types["delivery.closed"].Get("data.status").String())
	require.Equal(t, "due", types["delivery.created"].Get("data.reminder.kind").String())

	second := types["delivery.created"].Get("data.item.id").String()
	mia.Do(t, "DELETE", "/users/@me/items/"+second, nil)
	require.Equal(t, "cancelled", gw.Expect(t, "delivery.closed").Get("data.status").String())
}

func TestExpiredIsAnnounced(t *testing.T) {
	e := testkit.New(t)
	bot := e.Connection(t, "discord")
	mia := testkit.Linked(t, e, "mia", "111")
	gw := e.Gateway(t, bot.Token, "deliveries")
	testkit.DueSoon(t, e, mia)
	gw.Expect(t, "delivery.created")
	e.Clock.Advance(24*time.Hour + time.Second)
	e.Fire(t)
	require.Equal(t, "expired", gw.Expect(t, "delivery.closed").Get("data.status").String())
}

func TestQuietHoldReleases(t *testing.T) {
	e := testkit.New(t)
	bot := e.Connection(t, "discord")
	mia := testkit.Linked(t, e, "mia", "111")
	mia.Do(t, "PATCH", "/users/@me", J{"quiet_hours": J{"start": "12:30", "end": "14:00"}})
	mia.Do(t, "POST", "/users/@me/items", J{"title": "overdue", "due_at": "2026-11-10T13:00:00Z"})
	e.Clock.Advance(2 * time.Hour)
	e.Fire(t) // the follow-up goes out at 14:00 and stays unconfirmed
	e.Clock.Advance(23 * time.Hour)

	gw := e.Gateway(t, bot.Token, "deliveries") // 13:00 the next day, inside the quiet window
	gw.ExpectQuiet(t)
	e.Clock.Advance(59 * time.Minute)
	e.Fire(t)
	gw.ExpectQuiet(t)
	e.Clock.Advance(time.Minute) // 14:00, the window is over
	e.Fire(t)
	// Yesterday's follow-up gives way to today's, which was scheduled for the end of
	// the window. The bot never got yesterday's, so it is not told that it is gone.
	require.Equal(t, "follow_up", gw.Expect(t, "delivery.created").Get("data.reminder.kind").String())
	gw.ExpectQuiet(t)
}

func TestHeldDeliveryIsReleasedWhenTheWindowEnds(t *testing.T) {
	e := testkit.New(t)
	bot := e.Connection(t, "discord")
	mia := testkit.Linked(t, e, "mia", "111")
	// One follow-up per day at 13:00; quiet hours are switched on only after it fired.
	mia.Do(t, "POST", "/users/@me/items", J{"title": "overdue", "due_at": "2026-11-10T13:00:00Z"})
	e.Clock.Advance(time.Hour)
	e.Fire(t)
	mia.Do(t, "PATCH", "/users/@me", J{"quiet_hours": J{"start": "12:30", "end": "13:30"}})

	gw := e.Gateway(t, bot.Token, "deliveries") // 13:00, now inside the window
	gw.ExpectQuiet(t)
	e.Clock.Advance(30 * time.Minute)
	e.Fire(t)
	released := gw.Expect(t, "delivery.created")
	require.Equal(t, "follow_up", released.Get("data.reminder.kind").String())
	require.Equal(t, int64(1), released.Get("data.attempt").Int())
}

func TestDeliveriesNeedTheIntent(t *testing.T) {
	e := testkit.New(t)
	bot := e.Connection(t, "discord")
	mia := testkit.Linked(t, e, "mia", "111")
	gw := e.Gateway(t, bot.Token, "items")
	gw.Expect(t, "user.sync")
	testkit.DueSoon(t, e, mia)
	gw.Expect(t, "item.created")
	gw.ExpectQuiet(t)
	ds := e.Deliveries(t, mia.UserID)
	require.Equal(t, []any{"pending", 0}, []any{ds[0].Status, ds[0].Attempts})
}

func TestTokenChannelOverGateway(t *testing.T) {
	e := testkit.New(t)
	mia := e.User(t, "mia")
	tok := mia.Do(t, "POST", "/users/@me/tokens", J{"name": "homelab", "remind": true}).Get("token").String()
	plain := personalToken(t, mia)
	gw := e.Gateway(t, tok, "deliveries")
	other := e.Gateway(t, plain, "deliveries")
	testkit.DueSoon(t, e, mia)
	d := gw.Expect(t, "delivery.created")
	require.False(t, d.Get("data.user.external_ids").Exists())
	other.ExpectQuiet(t)
}

func TestAllSessionsOfAChannelGetIt(t *testing.T) {
	e := testkit.New(t)
	bot := e.Connection(t, "discord")
	mia := testkit.Linked(t, e, "mia", "111")
	a, b := e.Gateway(t, bot.Token, "deliveries"), e.Gateway(t, bot.Token, "deliveries")
	testkit.DueSoon(t, e, mia)
	first, second := a.Expect(t, "delivery.created"), b.Expect(t, "delivery.created")
	require.Equal(t, first.Get("data.id").String(), second.Get("data.id").String())
	require.Equal(t, int64(1), first.Get("data.attempt").Int())
	require.Equal(t, 1, e.Deliveries(t, mia.UserID)[0].Attempts)
}

func TestClosedIsNotAnnouncedForWhatWasNeverSent(t *testing.T) {
	e := testkit.New(t)
	bot := e.Connection(t, "discord")
	mia := testkit.Linked(t, e, "mia", "111")
	item := mia.Do(t, "POST", "/users/@me/items", J{"title": "overdue", "due_at": "2026-11-10T13:00:00Z"}).Get("id").String()
	e.Clock.Advance(time.Hour)
	e.Fire(t)
	mia.Do(t, "PATCH", "/users/@me", J{"quiet_hours": J{"start": "12:30", "end": "18:00"}})
	gw := e.Gateway(t, bot.Token, "deliveries")
	gw.ExpectQuiet(t)

	// The delivery is held back by quiet hours, so the bot has never heard of it.
	mia.Do(t, "PATCH", "/users/@me/items/"+item, J{"status": "done"})
	// Closed and new deliveries are announced in order, so the next one tells.
	mia.Do(t, "POST", "/users/@me/items", J{"title": "now", "due_at": dueIn(e, time.Minute)})
	e.Clock.Advance(time.Minute)
	e.Fire(t)
	require.Equal(t, "now", gw.Expect(t, "delivery.created").Get("data.item.title").String())
}

func TestSignalArrivingAfterLoginDoesNotRepeatTheDelivery(t *testing.T) {
	e := testkit.New(t)
	bot := e.Connection(t, "discord")
	mia := testkit.Linked(t, e, "mia", "111")
	testkit.DueSoon(t, e, mia)

	gw := e.Gateway(t, bot.Token, "deliveries")
	d := gw.Expect(t, "delivery.created")
	require.Equal(t, int64(1), d.Get("data.attempt").Int())

	// The hub hears of the new delivery only now, after the login already handed it out.
	id := uuid.MustParse(d.Get("data.id").String())
	require.NoError(t, events.SendSignal(context.Background(), e.Deps.Q(), events.Signal{Type: events.SignalDelivery, DeliveryID: &id}))
	// Deliveries go out in the order of their signals, so the next one tells.
	next := testkit.DueSoon(t, e, mia)
	require.Equal(t, next, gw.Expect(t, "delivery.created").Get("data.item.id").String())
	gw.ExpectQuiet(t)

	gw.Close()
	again := e.ResumeGateway(t, bot.Token, gw.SessionID, gw.LastSeq)
	again.Expect(t, gateway.EventResumed)
	require.Equal(t, int64(2), again.Expect(t, "delivery.created").Get("data.attempt").Int())
}

func TestResumeWhileADeliveryIsOnItsWayDoesNotRepeatIt(t *testing.T) {
	e := testkit.New(t)
	e.Deps.RateConn = api.NewLimiter(1000000, time.Minute, e.Clock.Now)
	bot := e.Connection(t, "discord")
	mia := testkit.Linked(t, e, "mia", "111")
	gw := e.Gateway(t, bot.Token, "deliveries")
	for round := 0; round < 30; round++ {
		due := dueIn(e, time.Hour)
		require.Equal(t, 201, mia.Do(t, "POST", "/users/@me/items", J{"title": "x", "priority": "low", "due_at": due}).Status)
		e.Clock.Advance(time.Hour)
		fired := make(chan struct{})
		go func() {
			time.Sleep(time.Duration(rand.Intn(6000)) * time.Microsecond)
			e.Fire(t)
			close(fired)
		}()
		time.Sleep(time.Duration(rand.Intn(12000)) * time.Microsecond)
		// The client lost its connection without the hub noticing, and comes back.
		back := e.ResumeGateway(t, bot.Token, gw.SessionID, gw.LastSeq)
		<-fired
		seen := map[string]int{}
		for _, f := range back.Collect(200 * time.Millisecond) {
			if f.T == "delivery.created" {
				seen[f.Get("data.id").String()]++
			}
		}
		for id, times := range seen {
			require.Equal(t, 1, times, "round %d: one connection got the same delivery %d times", round, times)
			require.Equal(t, 200, bot.Do(t, "PUT", "/deliveries/"+id, J{"status": "delivered"}).Status)
		}
		gw = back
	}
}

func TestOtherAccountActivityIsNotAnnouncedToConnections(t *testing.T) {
	e := testkit.New(t)
	bot := e.Connection(t, "discord")
	mia := testkit.Linked(t, e, "mia", "111")
	gw := e.Gateway(t, bot.Token, "users")
	gw.Expect(t, "user.sync")

	// Signals that her access changed, but nothing the bot can see did.
	created := mia.Do(t, "POST", "/users/@me/tokens", J{"name": "homelab"})
	require.Equal(t, 201, created.Status)
	require.Equal(t, 204, mia.Do(t, "DELETE", "/users/@me/tokens/"+created.Get("id").String(), nil).Status)
	gw.ExpectQuiet(t)

	e.GiveIdentity(t, mia.UserID, "discord", "222")
	require.NoError(t, events.NotifyUser(context.Background(), e.Deps.Q(), events.SignalUser, uuid.MustParse(mia.UserID)))
	require.JSONEq(t, `["111","222"]`, gw.Expect(t, "user.updated").Get("data.external_ids").Raw)
}

func TestTwoQuickResumesDoNotRepeatADelivery(t *testing.T) {
	e := testkit.New(t)
	e.Deps.RateConn = api.NewLimiter(1000000, time.Minute, e.Clock.Now)
	bot := e.Connection(t, "discord")
	mia := testkit.Linked(t, e, "mia", "111")
	for i := 0; i < 6; i++ {
		testkit.DueSoon(t, e, mia)
	}
	gw := e.Gateway(t, bot.Token, "deliveries")
	gw.Collect(200 * time.Millisecond)
	for round := 0; round < 15; round++ {
		e.ResumeGateway(t, bot.Token, gw.SessionID, gw.LastSeq)
		time.Sleep(time.Duration(rand.Intn(10000)) * time.Microsecond)
		second := e.ResumeGateway(t, bot.Token, gw.SessionID, gw.LastSeq)
		seen := map[string]int{}
		for len(seen) < 6 {
			f := second.NextEvent(t)
			if f.T == "delivery.created" {
				seen[f.Get("data.id").String()]++
			}
		}
		for _, f := range second.Collect(200 * time.Millisecond) {
			if f.T == "delivery.created" {
				seen[f.Get("data.id").String()]++
			}
		}
		for id, times := range seen {
			require.Equal(t, 1, times, "round %d: delivery %s arrived %d times on one connection", round, id, times)
		}
		gw = second
	}
}

func TestDetachedSessionDoesNotSwallowWhatTheHubMissed(t *testing.T) {
	for round := 0; round < 6; round++ {
		e := testkit.New(t)
		bot := e.Connection(t, "discord")
		mia := testkit.Linked(t, e, "mia", "111")
		// The adapter reconnected with Identify, so its old session lingers.
		old := e.Gateway(t, bot.Token, "deliveries")
		old.Close()
		time.Sleep(100 * time.Millisecond)
		first, second := e.Gateway(t, bot.Token, "deliveries"), e.Gateway(t, bot.Token, "deliveries")

		e.KillListener(t)
		testkit.DueSoon(t, e, mia)
		// Every connected session of the channel gets it, as with any delivery.
		require.Equal(t, int64(1), first.Expect(t, "delivery.created").Get("data.attempt").Int(), "round %d", round)
		require.Equal(t, int64(1), second.Expect(t, "delivery.created").Get("data.attempt").Int(), "round %d", round)
	}
}
