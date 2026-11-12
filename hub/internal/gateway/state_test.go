package gateway_test

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/rayakame/plsremind/hub/internal/api"
	"github.com/rayakame/plsremind/hub/internal/testkit"
)

func TestUserSyncContainsState(t *testing.T) {
	e := testkit.New(t)
	mia := e.User(t, "mia")
	uni := mia.Do(t, "POST", "/users/@me/lists", J{"name": "Uni"}).Get("id").String()
	open := mia.Do(t, "POST", "/users/@me/items", J{"title": "Abgabe", "list_id": uni, "subitems": []string{"Code", "Doku"}}).Get("id").String()
	mia.Do(t, "POST", "/users/@me/items/"+open+"/reminders", J{"in": "2h"})
	mia.Do(t, "POST", "/users/@me/items", J{"title": "Wäsche"})
	done := mia.Do(t, "POST", "/users/@me/items", J{"title": "erledigt"}).Get("id").String()
	mia.Do(t, "PATCH", "/users/@me/items/"+done, J{"status": "done"})
	mia.Do(t, "PUT", "/users/@me/reminder-plans/high", J{"before": []string{"1h"}, "follow_up": "never"})

	gw := e.Gateway(t, personalToken(t, mia), "users", "lists", "items", "plans")
	require.JSONEq(t, `["`+mia.UserID+`"]`, gw.Ready.Get("users").Raw)

	sync := gw.Expect(t, "user.sync")
	require.Equal(t, int64(2), sync.S)
	require.Equal(t, mia.UserID, sync.Get("user_id").String())
	require.JSONEq(t, `{"index":0,"count":1}`, sync.Get("chunk").Raw)
	require.Equal(t, "mia", sync.Get("user.display_name").String())
	require.Equal(t, []string{"Inbox", "Uni"}, testkit.Strings(&testkit.Resp{Body: []byte(sync.D.Raw)}, "lists.#.name"))
	require.Equal(t, []string{"Abgabe", "Wäsche"}, testkit.Strings(&testkit.Resp{Body: []byte(sync.D.Raw)}, "items.#.title"), "done items are not part of the state")
	require.Equal(t, int64(2), sync.Get("items.0.subitems.#").Int())
	require.Equal(t, int64(1), sync.Get("items.0.reminders.#").Int())
	require.Equal(t, int64(3), sync.Get("plans.#").Int())
	require.JSONEq(t, `["1h"]`, sync.Get("plans.2.before").Raw)

	gw.Cache(t).RequireMatchesREST(t, mia, "/users/@me", mia.UserID)
}

func TestUserSyncRespectsIntents(t *testing.T) {
	e := testkit.New(t)
	mia := e.User(t, "mia")
	mia.Do(t, "POST", "/users/@me/items", J{"title": "x"})
	token := personalToken(t, mia)
	parts := map[string]string{"users": "user", "lists": "lists", "items": "items", "plans": "plans"}
	for intent, part := range parts {
		sync := e.Gateway(t, token, intent).Expect(t, "user.sync")
		for _, other := range parts {
			require.Equal(t, other == part, sync.Get(other).Exists(), "intent %s, part %s", intent, other)
		}
	}
	only := e.Gateway(t, token, "deliveries")
	require.JSONEq(t, `[]`, only.Ready.Get("users").Raw)
	only.ExpectQuiet(t)
}

func TestUserSyncChunks(t *testing.T) {
	e := testkit.New(t)
	e.Deps.Gateway.SyncChunk = 2
	mia := e.User(t, "mia")
	for i := 0; i < 5; i++ {
		mia.Do(t, "POST", "/users/@me/items", J{"title": fmt.Sprintf("item %d", i)})
	}
	gw := e.Gateway(t, personalToken(t, mia), "lists", "items")
	var titles []string
	for index := 0; index < 3; index++ {
		chunk := gw.Expect(t, "user.sync")
		require.JSONEq(t, fmt.Sprintf(`{"index":%d,"count":3}`, index), chunk.Get("chunk").Raw)
		require.Equal(t, index == 0, chunk.Get("lists").Exists(), "lists only come in the first part")
		titles = append(titles, testkit.Strings(&testkit.Resp{Body: []byte(chunk.D.Raw)}, "items.#.title")...)
	}
	require.Equal(t, []string{"item 0", "item 1", "item 2", "item 3", "item 4"}, titles)
	gw.Cache(t).RequireMatchesREST(t, mia, "/users/@me", mia.UserID)
}

func TestLiveEventsArriveInOrder(t *testing.T) {
	e := testkit.New(t)
	mia := e.User(t, "mia")
	gw := e.Gateway(t, personalToken(t, mia), "items")
	gw.Expect(t, "user.sync")
	last := gw.LastSeq
	for i := 0; i < 30; i++ {
		mia.Do(t, "POST", "/users/@me/items", J{"title": fmt.Sprintf("item %d", i)})
	}
	for i := 0; i < 30; i++ {
		ev := gw.Expect(t, "item.created")
		require.Equal(t, last+int64(i)+1, ev.S)
		require.Equal(t, fmt.Sprintf("item %d", i), ev.Get("data.title").String())
		require.Equal(t, mia.UserID, ev.Get("user_id").String())
		require.Equal(t, "session", ev.Get("actor.kind").String())
		require.NotEmpty(t, ev.Get("actor.id").String())
	}
}

func TestEventsRespectIntents(t *testing.T) {
	e := testkit.New(t)
	mia := e.User(t, "mia")
	gw := e.Gateway(t, personalToken(t, mia), "lists")
	gw.Expect(t, "user.sync")
	mia.Do(t, "POST", "/users/@me/items", J{"title": "x"})
	mia.Do(t, "PATCH", "/users/@me", J{"display_name": "Mia"})
	mia.Do(t, "PUT", "/users/@me/reminder-plans/low", J{"before": []string{}, "follow_up": "never"})
	mia.Do(t, "POST", "/users/@me/lists", J{"name": "Uni"})
	require.Equal(t, "Uni", gw.Expect(t, "list.created").Get("data.name").String())
	gw.ExpectQuiet(t)
}

func TestChildChangesAlsoAnnounceTheItem(t *testing.T) {
	e := testkit.New(t)
	mia := e.User(t, "mia")
	id := mia.Do(t, "POST", "/users/@me/items", J{"title": "x"}).Get("id").String()
	gw := e.Gateway(t, personalToken(t, mia), "items")
	gw.Expect(t, "user.sync")

	sub := mia.Do(t, "POST", "/users/@me/items/"+id+"/subitems", J{"title": "a"}).Get("id").String()
	require.Equal(t, sub, gw.Expect(t, "subitem.created").Get("data.id").String())
	whole := gw.Expect(t, "item.updated")
	require.Equal(t, sub, whole.Get("data.subitems.0.id").String(), "the item follows with its new state")

	mia.Do(t, "POST", "/users/@me/items/"+id+"/reminders", J{"in": "2h"})
	gw.Expect(t, "reminder.created")
	require.Equal(t, int64(1), gw.Expect(t, "item.updated").Get("data.reminders.#").Int())
	gw.Cache(t).RequireMatchesREST(t, mia, "/users/@me", mia.UserID)
}

func TestCacheMatchesRESTUnderLoad(t *testing.T) {
	e := testkit.New(t)
	mia := e.User(t, "mia")
	token := personalToken(t, mia)
	e.Deps.RateAuth = api.NewLimiter(100000, time.Minute, e.Clock.Now)
	seed := mia.Do(t, "POST", "/users/@me/items", J{"title": "seed", "subitems": []string{"a"}}).Get("id").String()

	var wg sync.WaitGroup
	write := func(fn func(i int)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 15; i++ {
				fn(i)
			}
		}()
	}
	nc := testkit.NoContract()
	var failed atomic.Int32
	do := func(method, path string, body any) *testkit.Resp {
		r := mia.Do(t, method, path, body, nc)
		if r.Status >= 500 {
			failed.Add(1)
		}
		return r
	}
	write(func(i int) {
		id := do("POST", "/users/@me/items", J{"title": fmt.Sprintf("a%d", i), "due_date": "2026-12-01"}).Get("id").String()
		do("PATCH", "/users/@me/items/"+id, J{"title": fmt.Sprintf("a%d!", i), "priority": "high"})
		if i%3 == 0 {
			do("PATCH", "/users/@me/items/"+id, J{"status": "done"})
		}
		if i%5 == 0 {
			do("DELETE", "/users/@me/items/"+id, nil)
		}
	})
	write(func(i int) {
		list := do("POST", "/users/@me/lists", J{"name": fmt.Sprintf("l%d", i)}).Get("id").String()
		do("POST", "/users/@me/items", J{"title": "in list", "list_id": list})
		if i%2 == 0 {
			do("DELETE", "/users/@me/lists/"+list, nil)
		}
	})
	write(func(i int) {
		sub := do("POST", "/users/@me/items/"+seed+"/subitems", J{"title": fmt.Sprintf("s%d", i)}).Get("id").String()
		do("PATCH", "/users/@me/items/"+seed+"/subitems/"+sub, J{"done": true})
		rem := do("POST", "/users/@me/items/"+seed+"/reminders", J{"in": fmt.Sprintf("%dh", i+1)}).Get("id").String()
		if i%2 == 0 {
			do("DELETE", "/users/@me/items/"+seed+"/subitems/"+sub, nil)
			do("DELETE", "/users/@me/items/"+seed+"/reminders/"+rem, nil)
		}
	})
	write(func(i int) {
		zones := []string{"Europe/Berlin", "UTC", "America/New_York"}
		do("PATCH", "/users/@me", J{"display_name": fmt.Sprintf("Mia %d", i), "timezone": zones[i%3]})
		do("PUT", "/users/@me/reminder-plans/normal", J{"before": []string{fmt.Sprintf("%dh", i+1)}, "follow_up": "daily"})
	})

	// The client connects while all of this is going on.
	gw := e.Gateway(t, token, "users", "lists", "items", "plans")
	wg.Wait()
	require.Zero(t, failed.Load(), "concurrent changes to one user must not fail each other")
	gw.Cache(t).RequireMatchesREST(t, mia, "/users/@me", mia.UserID)
}

func TestOtherUsersEventsNeverLeak(t *testing.T) {
	e := testkit.New(t)
	mia, ben := e.User(t, "mia"), e.User(t, "ben")
	gw := e.Gateway(t, personalToken(t, mia), "users", "lists", "items", "plans")
	gw.Expect(t, "user.sync")
	ben.Do(t, "POST", "/users/@me/items", J{"title": "geheim"})
	ben.Do(t, "PATCH", "/users/@me", J{"display_name": "Ben"})
	gw.ExpectQuiet(t)
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
