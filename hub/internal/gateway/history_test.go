package gateway_test

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/rayakame/plsremind/hub/internal/api"
	"github.com/rayakame/plsremind/hub/internal/gateway"
	"github.com/rayakame/plsremind/hub/internal/testkit"
)

// syncParts reads one user's whole user.sync.
func syncParts(t *testing.T, gw *testkit.GatewayClient) []testkit.Frame {
	t.Helper()
	var parts []testkit.Frame
	for {
		part := gw.Expect(t, "user.sync")
		parts = append(parts, part)
		if part.Get("chunk.index").Int() == part.Get("chunk.count").Int()-1 {
			return parts
		}
	}
}

func titles(f testkit.Frame, path string) []string {
	return testkit.Strings(&testkit.Resp{Body: []byte(f.D.Raw)}, path)
}

// finish creates done items named after their order of completion, a minute apart.
func finish(t *testing.T, e *testkit.Env, u *testkit.Client, count int) {
	t.Helper()
	for i := 0; i < count; i++ {
		id := u.Do(t, "POST", "/users/@me/items", J{"title": fmt.Sprintf("done %d", i)}).Get("id").String()
		require.Equal(t, 200, u.Do(t, "PATCH", "/users/@me/items/"+id, J{"status": "done"}).Status)
		e.Clock.Advance(time.Minute)
	}
}

func TestSyncCarriesHistory(t *testing.T) {
	e := testkit.New(t)
	e.Deps.Gateway.SyncChunk, e.Deps.Gateway.SyncHistory = 2, 5
	mia := e.User(t, "mia")
	for i := 0; i < 3; i++ {
		mia.Do(t, "POST", "/users/@me/items", J{"title": fmt.Sprintf("open %d", i)})
	}
	finish(t, e, mia, 7)

	gw := e.Gateway(t, personalToken(t, mia), "items", "history")
	parts := syncParts(t, gw)
	require.Len(t, parts, 5, "two parts of open items, three of history")
	var open, history []string
	for index, part := range parts {
		require.JSONEq(t, fmt.Sprintf(`{"index":%d,"count":5}`, index), part.Get("chunk").Raw)
		require.Equal(t, index < 2, part.Get("items").Exists(), "part %d", index)
		require.Equal(t, index >= 2, part.Get("history").Exists(), "part %d", index)
		require.Equal(t, index == 4, part.Get("history_cursor").Exists(), "the cursor comes with the last part of history")
		open = append(open, titles(part, "items.#.title")...)
		history = append(history, titles(part, "history.#.title")...)
	}
	require.Equal(t, []string{"open 0", "open 1", "open 2"}, open)
	require.Equal(t, []string{"done 6", "done 5", "done 4", "done 3", "done 2"}, history)
	require.Equal(t, "done", parts[2].Get("history.0.status").String())
	require.True(t, parts[2].Get("history.0.subitems").Exists(), "history holds whole items")

	cursor := parts[4].Get("history_cursor").String()
	require.NotEmpty(t, cursor)
	older := mia.Do(t, "GET", "/users/@me/items?status=done&sort=completed&order=desc&cursor="+cursor, nil)
	require.Equal(t, 200, older.Status, string(older.Body))
	require.Equal(t, []string{"done 1", "done 0"}, testkit.Strings(older, "data.#.title"))
	require.Equal(t, gjson.Null, older.Get("next_cursor").Type)
}

func TestHistoryCursorIsNullWhenNothingOlderExists(t *testing.T) {
	e := testkit.New(t)
	e.Deps.Gateway.SyncHistory = 5
	mia := e.User(t, "mia")
	finish(t, e, mia, 5)
	parts := syncParts(t, e.Gateway(t, personalToken(t, mia), "items", "history"))
	last := parts[len(parts)-1]
	require.Equal(t, int64(5), last.Get("history.#").Int())
	require.True(t, last.Get("history_cursor").Exists())
	require.Equal(t, gjson.Null, last.Get("history_cursor").Type)
}

func TestSyncWithoutDoneItemsCarriesEmptyHistory(t *testing.T) {
	e := testkit.New(t)
	mia := e.User(t, "mia")
	mia.Do(t, "POST", "/users/@me/items", J{"title": "open"})
	parts := syncParts(t, e.Gateway(t, personalToken(t, mia), "items", "history"))
	require.Len(t, parts, 1)
	require.JSONEq(t, `[]`, parts[0].Get("history").Raw)
	require.True(t, parts[0].Get("history_cursor").Exists())
	require.Equal(t, gjson.Null, parts[0].Get("history_cursor").Type)
	require.Equal(t, int64(1), parts[0].Get("items.#").Int())
}

func TestSyncWithoutHistoryIntentIsUnchanged(t *testing.T) {
	e := testkit.New(t)
	e.Deps.Gateway.SyncChunk = 2
	mia := e.User(t, "mia")
	for i := 0; i < 3; i++ {
		mia.Do(t, "POST", "/users/@me/items", J{"title": "open"})
	}
	finish(t, e, mia, 3)
	parts := syncParts(t, e.Gateway(t, personalToken(t, mia), "users", "lists", "items", "plans"))
	require.Len(t, parts, 2)
	for _, part := range parts {
		require.False(t, part.Get("history").Exists())
		require.False(t, part.Get("history_cursor").Exists())
	}
}

func TestHistoryBoundaryWithEqualCompletionTimes(t *testing.T) {
	e := testkit.New(t)
	e.Deps.Gateway.SyncHistory = 2
	mia := e.User(t, "mia")
	all := map[string]bool{}
	for i := 0; i < 4; i++ {
		id := mia.Do(t, "POST", "/users/@me/items", J{"title": fmt.Sprintf("same %d", i)}).Get("id").String()
		mia.Do(t, "PATCH", "/users/@me/items/"+id, J{"status": "done"})
		all[id] = true
	}
	parts := syncParts(t, e.Gateway(t, personalToken(t, mia), "items", "history"))
	last := parts[len(parts)-1]
	seen := map[string]int{}
	for _, part := range parts {
		for _, id := range titles(part, "history.#.id") {
			seen[id]++
		}
	}
	require.Len(t, seen, 2)
	older := mia.Do(t, "GET", "/users/@me/items?status=done&sort=completed&order=desc&cursor="+last.Get("history_cursor").String(), nil)
	for _, id := range testkit.Strings(older, "data.#.id") {
		seen[id]++
	}
	require.Len(t, seen, 4, "sync and REST together hold every done item")
	for id, n := range seen {
		require.True(t, all[id])
		require.Equal(t, 1, n, "item %s came twice", id)
	}
}

func TestHistoryNeedsItems(t *testing.T) {
	e := testkit.New(t)
	token := personalToken(t, e.User(t, "mia"))
	alone := e.GatewayRaw(t)
	alone.Next(t)
	alone.Send(t, gateway.OpIdentify, J{"token": token, "intents": []string{"history"}})
	alone.ExpectClose(t, gateway.CloseInvalidIntents)

	require.JSONEq(t, `["items","history"]`, e.Gateway(t, token, "history", "items").Ready.Get("intents").Raw)

	r := e.Admin(t).Do(t, "POST", "/admin/connections", J{"name": "x", "provider": "discord", "allowed_intents": []string{"users", "history"}})
	require.Equal(t, 422, r.Status)
	require.Contains(t, r.Get("error.fields.allowed_intents").String(), "history needs items")
}

func TestConnectionNeedsHistoryAllowed(t *testing.T) {
	e := testkit.New(t)
	bot := e.Connection(t, "discord")
	require.Contains(t, testkit.Strings(e.Admin(t).Do(t, "GET", "/admin/connections", nil), "0.allowed_intents"), "history", "a new connection may have everything")
	e.Gateway(t, bot.Token, "items", "history")

	r := e.Admin(t).Do(t, "PATCH", "/admin/connections/"+bot.ID, J{"allowed_intents": []string{"items"}})
	require.Equal(t, 200, r.Status)
	gw := e.GatewayRaw(t)
	gw.Next(t)
	gw.Send(t, gateway.OpIdentify, J{"token": bot.Token, "intents": []string{"items", "history"}})
	gw.ExpectClose(t, gateway.CloseDisallowedIntents)
}

func TestCacheWithHistoryMatchesRESTUnderWrites(t *testing.T) {
	for name, limit := range map[string]int{"everything fits": 1000, "history is cut off": 4} {
		t.Run(name, func(t *testing.T) {
			e := testkit.New(t)
			e.Deps.Gateway.SyncChunk, e.Deps.Gateway.SyncHistory = 3, limit
			e.Deps.RateAuth = api.NewLimiter(100000, time.Minute, e.Clock.Now)
			mia := e.User(t, "mia")
			finish(t, e, mia, 8)

			nc := testkit.NoContract()
			var failed atomic.Int32
			do := func(method, path string, body any) *testkit.Resp {
				r := mia.Do(t, method, path, body, nc)
				if r.Status >= 500 {
					failed.Add(1)
				}
				return r
			}
			var wg sync.WaitGroup
			for worker := 0; worker < 3; worker++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for i := 0; i < 12; i++ {
						id := do("POST", "/users/@me/items", J{"title": fmt.Sprintf("w%d.%d", worker, i)}).Get("id").String()
						if i%2 == 0 {
							do("PATCH", "/users/@me/items/"+id, J{"status": "done"})
						}
						if i%4 == 0 {
							do("PATCH", "/users/@me/items/"+id, J{"status": "open"})
						}
						if i%6 == 2 {
							do("DELETE", "/users/@me/items/"+id, nil)
						}
						if i%6 == 4 {
							do("PATCH", "/users/@me/items/"+id, J{"title": "renamed while done"})
						}
					}
				}()
			}
			// The client connects while all of this is going on.
			gw := e.Gateway(t, personalToken(t, mia), "items", "history")
			wg.Wait()
			require.Zero(t, failed.Load())
			gw.Cache(t).RequireMatchesREST(t, mia, "/users/@me", mia.UserID)
		})
	}
}

func TestTimezoneChangeReachesDoneAllDayItems(t *testing.T) {
	e := testkit.New(t)
	mia := e.User(t, "mia")
	id := mia.Do(t, "POST", "/users/@me/items", J{"title": "all day", "due_date": "2026-11-20"}).Get("id").String()
	require.Equal(t, 200, mia.Do(t, "PATCH", "/users/@me/items/"+id, J{"status": "done"}).Status)
	gw := e.Gateway(t, personalToken(t, mia), "items", "history")
	syncParts(t, gw)

	require.Equal(t, 200, mia.Do(t, "PATCH", "/users/@me", J{"timezone": "Asia/Tokyo"}).Status)
	moved := gw.Expect(t, "item.updated")
	require.Equal(t, id, moved.Get("data.id").String())
	require.Equal(t, "done", moved.Get("data.status").String())
	require.Equal(t, "2026-11-20T14:59:59Z", moved.Get("data.due_at").String())
	gw.Cache(t).RequireMatchesREST(t, mia, "/users/@me", mia.UserID)
}
