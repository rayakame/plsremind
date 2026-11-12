package delivery_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/rayakame/plsremind/hub/internal/delivery"
	"github.com/rayakame/plsremind/hub/internal/testkit"
)

func statuses(ds []testkit.Delivery) []string {
	out := make([]string, 0, len(ds))
	for _, d := range ds {
		out = append(out, d.Kind+" "+d.Status)
	}
	return out
}

func TestReminderFiresOncePerChannel(t *testing.T) {
	e := testkit.New(t)
	mia, _ := testkit.Reminded(t, e, "mia")
	e.Connection(t, "discord")
	e.GiveIdentity(t, mia.UserID, "discord", "1234")
	id := mia.Do(t, "POST", "/users/@me/items", J{"title": "x", "priority": "low", "due_at": "2026-11-11T13:00:00Z"}).Get("id").String()

	e.Fire(t)
	require.Empty(t, e.Deliveries(t, mia.UserID), "nothing is due yet")

	e.Clock.Advance(time.Hour)
	e.Fire(t)
	e.Fire(t)
	ds := e.Deliveries(t, mia.UserID)
	require.Len(t, ds, 2)
	require.ElementsMatch(t, []string{"connection", "token"}, []string{ds[0].Channel, ds[1].Channel})
	require.Equal(t, []string{"due", "pending", id}, []string{ds[0].Kind, ds[0].Status, ds[0].ItemID})
	require.Empty(t, e.Schedule(t, id), "a fired reminder leaves the schedule")
	require.Equal(t, []string{"due fired"}, e.ReminderLog(t, mia.UserID))
}

func TestNoChannelStillLogs(t *testing.T) {
	e := testkit.New(t)
	mia := e.User(t, "mia")
	mia.Do(t, "POST", "/users/@me/items", J{"title": "x", "priority": "low", "due_at": "2026-11-11T13:00:00Z"})
	e.Clock.Advance(time.Hour)
	e.Fire(t)
	require.Empty(t, e.Deliveries(t, mia.UserID))
	require.Equal(t, []string{"due fired"}, e.ReminderLog(t, mia.UserID))
}

func TestWhoIsAChannel(t *testing.T) {
	e := testkit.New(t)
	admin := e.Admin(t)
	mia := e.User(t, "mia")
	mia.Do(t, "POST", "/users/@me/tokens", J{"name": "script"}) // not marked for reminders
	off := e.Connection(t, "discord")
	limited := e.Connection(t, "discord")
	disabled := e.Connection(t, "discord")
	unlinked := e.Connection(t, "telegram")
	good := e.Connection(t, "discord")
	e.GiveIdentity(t, mia.UserID, "discord", "1234")
	mia.Do(t, "PUT", "/users/@me/connections/"+off.ID, J{"remind": false})
	admin.Do(t, "PATCH", "/admin/connections/"+limited.ID, J{"allowed_intents": []string{"items"}})
	admin.Do(t, "PATCH", "/admin/connections/"+disabled.ID, J{"enabled": false})
	_ = unlinked

	mia.Do(t, "POST", "/users/@me/items", J{"title": "x", "priority": "low", "due_at": "2026-11-11T13:00:00Z"})
	e.Clock.Advance(time.Hour)
	e.Fire(t)
	ds := e.Deliveries(t, mia.UserID)
	require.Len(t, ds, 1)
	require.Equal(t, good.ID, ds[0].ChannelID)
}

func TestNewerReminderSupersedesPending(t *testing.T) {
	e := testkit.New(t)
	mia, _ := testkit.Reminded(t, e, "mia")
	mia.Do(t, "POST", "/users/@me/items", J{"title": "x", "priority": "low", "due_at": "2026-11-12T13:00:00Z"})
	e.Clock.Advance(time.Hour) // "1d before"
	e.Fire(t)
	require.Equal(t, []string{"before pending"}, statuses(e.Deliveries(t, mia.UserID)))
	e.Clock.Advance(24 * time.Hour) // due
	e.Fire(t)
	require.Equal(t, []string{"before superseded", "due pending"}, statuses(e.Deliveries(t, mia.UserID)))
}

func TestCatchUpAfterDowntime(t *testing.T) {
	// High priority, due Friday 18:00. Times: Thu 18:00 (1d), Fri 15:00 (3h), Fri 18:00 (due),
	// Sat 06:00 (follow-up). The hub was down from Wednesday until Saturday 17:00.
	e := testkit.New(t)
	mia, _ := testkit.Reminded(t, e, "mia")
	mia.Do(t, "POST", "/users/@me/items", J{"title": "x", "priority": "high", "due_at": "2026-11-13T18:00:00Z"})
	e.Clock.Advance(77 * time.Hour)
	e.Fire(t)

	// Older than 24 hours is missed; of the rest only the newest goes out.
	require.Equal(t, []string{"before missed", "before missed", "due superseded", "follow_up fired"}, e.ReminderLog(t, mia.UserID))
	require.Equal(t, []string{"follow_up pending"}, statuses(e.Deliveries(t, mia.UserID)))
}

func TestExpiry(t *testing.T) {
	e := testkit.New(t)
	mia, _ := testkit.Reminded(t, e, "mia")
	mia.Do(t, "POST", "/users/@me/items", J{"title": "x", "priority": "low", "due_at": "2026-11-11T13:00:00Z"})
	e.Clock.Advance(time.Hour)
	e.Fire(t)
	e.Clock.Advance(24 * time.Hour)
	e.Fire(t)
	require.Equal(t, []string{"due pending"}, statuses(e.Deliveries(t, mia.UserID)), "24 hours are not over until they are over")
	e.Clock.Advance(time.Second)
	e.Fire(t)
	require.Equal(t, []string{"due expired"}, statuses(e.Deliveries(t, mia.UserID)))
}

func TestDoneAndDeleteCancel(t *testing.T) {
	e := testkit.New(t)
	mia, token := testkit.Reminded(t, e, "mia")
	first := mia.Do(t, "POST", "/users/@me/items", J{"title": "x", "priority": "low", "due_at": "2026-11-11T13:00:00Z"}).Get("id").String()
	second := mia.Do(t, "POST", "/users/@me/items", J{"title": "y", "priority": "low", "due_at": "2026-11-11T13:00:00Z"}).Get("id").String()
	e.Clock.Advance(time.Hour)
	e.Fire(t)
	require.Equal(t, int64(2), e.Do(t, "GET", "/deliveries", nil, token).Get("#").Int())
	tap := e.Signals(t)

	mia.Do(t, "PATCH", "/users/@me/items/"+first, J{"status": "done"})
	require.NotNil(t, tap.NextOf(t, "closed").DeliveryID)
	mia.Do(t, "DELETE", "/users/@me/items/"+second, nil)
	require.NotNil(t, tap.NextOf(t, "closed").DeliveryID)
	require.Equal(t, []string{"due cancelled"}, statuses(e.Deliveries(t, mia.UserID)), "the deleted item took its delivery with it")
}

func TestTurningChannelOffCancels(t *testing.T) {
	e := testkit.New(t)
	admin := e.Admin(t)
	mia := e.User(t, "mia")
	tok := mia.Do(t, "POST", "/users/@me/tokens", J{"name": "homelab", "remind": true}).Get("id").String()
	patched := mia.Do(t, "POST", "/users/@me/tokens", J{"name": "pi", "remind": true}).Get("id").String()
	userOff := e.Connection(t, "discord")
	adminOff := e.Connection(t, "discord")
	narrowed := e.Connection(t, "discord")
	e.GiveIdentity(t, mia.UserID, "discord", "1234")
	mia.Do(t, "POST", "/users/@me/items", J{"title": "x", "priority": "low", "due_at": "2026-11-11T13:00:00Z"})
	e.Clock.Advance(time.Hour)
	e.Fire(t)
	require.Len(t, e.Deliveries(t, mia.UserID), 5)

	mia.Do(t, "PUT", "/users/@me/connections/"+userOff.ID, J{"remind": false})
	admin.Do(t, "PATCH", "/admin/connections/"+adminOff.ID, J{"enabled": false})
	admin.Do(t, "PATCH", "/admin/connections/"+narrowed.ID, J{"allowed_intents": []string{"items"}})
	mia.Do(t, "PATCH", "/users/@me/tokens/"+patched, J{"remind": false})
	mia.Do(t, "DELETE", "/users/@me/tokens/"+tok, nil)

	ds := e.Deliveries(t, mia.UserID)
	require.Len(t, ds, 4, "the deleted token took its delivery with it")
	for _, d := range ds {
		require.Equal(t, "cancelled", d.Status, d.Channel+" "+d.ChannelID)
	}
}

func TestTwoTickersFireOnce(t *testing.T) {
	e := testkit.New(t)
	mia, _ := testkit.Reminded(t, e, "mia")
	for i := 0; i < 20; i++ {
		mia.Do(t, "POST", "/users/@me/items", J{"title": fmt.Sprintf("item %d", i), "priority": "low", "due_at": "2026-11-11T13:00:00Z"})
	}
	e.Clock.Advance(time.Hour)
	errs := make([]error, 8)
	var wg sync.WaitGroup
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = delivery.RunOnce(context.Background(), e.Deps)
		}()
	}
	wg.Wait()
	for _, err := range errs {
		require.NoError(t, err)
	}
	e.Fire(t)
	require.Len(t, e.Deliveries(t, mia.UserID), 20)
	require.Len(t, e.ReminderLog(t, mia.UserID), 20)
}

func TestReminderMovedByQuietHoursIsNotLate(t *testing.T) {
	e := testkit.New(t)
	mia, token := testkit.Reminded(t, e, "mia")
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

func TestLiftingQuietHoursReleasesHeldDeliveries(t *testing.T) {
	e := testkit.New(t)
	bot := e.Connection(t, "discord")
	mia := testkit.Linked(t, e, "mia", "111")
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
