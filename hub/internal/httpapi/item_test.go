package httpapi_test

import (
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/rayakame/plsremind/hub/internal/testkit"
)

func ids(r *testkit.Resp, path string) []string {
	var out []string
	for _, v := range r.Get(path).Array() {
		out = append(out, v.String())
	}
	return out
}

func TestCreateTodoWithOneRequest(t *testing.T) {
	e := testkit.New(t)
	u := e.User(t, "mia")
	r := u.Do(t, "POST", "/users/@me/items", J{"title": "  Müll rausbringen "})
	require.Equal(t, 201, r.Status)
	require.Equal(t, "Müll rausbringen", r.Get("title").String())
	require.Equal(t, "normal", r.Get("priority").String())
	require.Equal(t, "open", r.Get("status").String())
	require.Equal(t, "", r.Get("note").String())
	require.False(t, r.Get("all_day").Bool())
	for _, field := range []string{"due_at", "due_date", "completed_at", "reminder_override"} {
		require.Equal(t, gjson.Null, r.Get(field).Type, field)
	}
	require.JSONEq(t, `[]`, r.Get("subitems").Raw)
	inbox := u.Do(t, "GET", "/users/@me/lists", nil).Get("0.id").String()
	require.Equal(t, inbox, r.Get("list_id").String())

	got := u.Do(t, "GET", "/users/@me/items/"+r.Get("id").String(), nil)
	require.Equal(t, 200, got.Status)
	require.JSONEq(t, string(r.Body), string(got.Body))

	ev := e.Events(t, u.UserID)
	require.Len(t, ev, 1)
	require.Equal(t, "item.created", ev[0].Type)
	require.Equal(t, "Müll rausbringen", ev[0].Payload.Get("title").String())
}

func TestCreateDeadlineWithTime(t *testing.T) {
	u := testkit.New(t).User(t, "mia")
	r := u.Do(t, "POST", "/users/@me/items", J{"title": "Abgabe", "due_at": "2026-11-13T18:00:00+01:00", "priority": "high", "note": "PDF hochladen"})
	require.Equal(t, 201, r.Status)
	require.Equal(t, "2026-11-13T17:00:00Z", r.Get("due_at").String())
	require.False(t, r.Get("all_day").Bool())
	require.Equal(t, gjson.Null, r.Get("due_date").Type)
	require.Equal(t, "high", r.Get("priority").String())
	require.Equal(t, "PDF hochladen", r.Get("note").String())
}

func TestAllDayDueFollowsTimezone(t *testing.T) {
	e := testkit.New(t)
	u := e.User(t, "mia")
	u.Do(t, "PATCH", "/users/@me", J{"timezone": "Europe/Berlin"})
	created := u.Do(t, "POST", "/users/@me/items", J{"title": "Abgabe", "due_date": "2024-11-13"})
	require.Equal(t, 201, created.Status)
	require.True(t, created.Get("all_day").Bool())
	require.Equal(t, "2024-11-13", created.Get("due_date").String())
	id := created.Get("id").String()
	get := func() string { return u.Do(t, "GET", "/users/@me/items/"+id, nil).Get("due_at").String() }
	require.Equal(t, "2024-11-13T22:59:59Z", get())

	dst := u.Do(t, "POST", "/users/@me/items", J{"title": "Sommerzeit", "due_date": "2025-03-30"})
	require.Equal(t, "2025-03-30T21:59:59Z", dst.Get("due_at").String())

	u.Do(t, "PATCH", "/users/@me", J{"timezone": "America/New_York"})
	require.Equal(t, "2024-11-14T04:59:59Z", get())
	var types []string
	events := e.Events(t, u.UserID)
	for _, ev := range events {
		types = append(types, ev.Type)
	}
	require.Equal(t, []string{"user.updated", "item.created", "item.created", "user.updated", "item.updated", "item.updated"}, types,
		"all-day items are announced again when the zone moves their due time")
	require.Equal(t, "2024-11-14T04:59:59Z", events[4].Payload.Get("due_at").String())

	before := u.Do(t, "GET", "/users/@me/items?due_before=2024-11-14T04:00:00Z", nil)
	require.Empty(t, ids(before, "data.#.id"), "filters use the due time in the current timezone")
	after := u.Do(t, "GET", "/users/@me/items?due_before=2024-11-14T05:00:00Z", nil)
	require.Equal(t, []string{id}, ids(after, "data.#.id"))
}

func TestDueAtAndDueDateExclusive(t *testing.T) {
	u := testkit.New(t).User(t, "mia")
	both := u.Do(t, "POST", "/users/@me/items", J{"title": "x", "due_at": "2026-11-13T18:00:00Z", "due_date": "2026-11-13"})
	require.Equal(t, 422, both.Status)
	require.Equal(t, 422, u.Do(t, "POST", "/users/@me/items", J{"title": "x", "due_date": "13.11.2026"}).Status)
	require.Equal(t, 422, u.Do(t, "POST", "/users/@me/items", J{"title": "x", "due_at": "tomorrow"}).Status)

	id := u.Do(t, "POST", "/users/@me/items", J{"title": "x", "due_date": "2026-11-13"}).Get("id").String()
	switched := u.Do(t, "PATCH", "/users/@me/items/"+id, J{"due_at": "2026-11-13T18:00:00Z"})
	require.Equal(t, "2026-11-13T18:00:00Z", switched.Get("due_at").String())
	require.False(t, switched.Get("all_day").Bool())
	require.Equal(t, gjson.Null, switched.Get("due_date").Type)

	cleared := u.Do(t, "PATCH", "/users/@me/items/"+id, J{"due_date": nil})
	require.Equal(t, 200, cleared.Status)
	require.Equal(t, gjson.Null, cleared.Get("due_at").Type)
	require.False(t, cleared.Get("all_day").Bool())
}

func TestPatchChangesOnlySentFields(t *testing.T) {
	u := testkit.New(t).User(t, "mia")
	id := u.Do(t, "POST", "/users/@me/items", J{"title": "Abgabe", "note": "wichtig", "priority": "high", "due_date": "2026-11-13"}).Get("id").String()
	r := u.Do(t, "PATCH", "/users/@me/items/"+id, J{"title": "Abgabe Mathe"})
	require.Equal(t, 200, r.Status)
	require.Equal(t, []string{"Abgabe Mathe", "wichtig", "high", "2026-11-13"},
		[]string{r.Get("title").String(), r.Get("note").String(), r.Get("priority").String(), r.Get("due_date").String()})
	require.Equal(t, 422, u.Do(t, "PATCH", "/users/@me/items/"+id, J{"priority": "urgent"}).Status)
	require.Equal(t, 422, u.Do(t, "PATCH", "/users/@me/items/"+id, J{"title": nil}).Status)
}

func TestCompleteAndReopen(t *testing.T) {
	e := testkit.New(t)
	u := e.User(t, "mia")
	id := u.Do(t, "POST", "/users/@me/items", J{"title": "x"}).Get("id").String()
	done := u.Do(t, "PATCH", "/users/@me/items/"+id, J{"status": "done"})
	require.Equal(t, 200, done.Status)
	require.Equal(t, "done", done.Get("status").String())
	require.Equal(t, "2026-11-11T12:00:00Z", done.Get("completed_at").String())

	open := u.Do(t, "PATCH", "/users/@me/items/"+id, J{"status": "open"})
	require.Equal(t, "open", open.Get("status").String())
	require.Equal(t, gjson.Null, open.Get("completed_at").Type)
	require.Equal(t, 422, u.Do(t, "PATCH", "/users/@me/items/"+id, J{"status": "archived"}).Status)

	ev := e.Events(t, u.UserID)
	require.Equal(t, []string{"item.created", "item.updated", "item.updated"}, []string{ev[0].Type, ev[1].Type, ev[2].Type})
	require.Equal(t, "done", ev[1].Payload.Get("status").String())
}

func TestDeleteItem(t *testing.T) {
	e := testkit.New(t)
	u := e.User(t, "mia")
	id := u.Do(t, "POST", "/users/@me/items", J{"title": "x"}).Get("id").String()
	require.Equal(t, 204, u.Do(t, "DELETE", "/users/@me/items/"+id, nil).Status)
	require.Equal(t, 404, u.Do(t, "GET", "/users/@me/items/"+id, nil).Status)
	ev := e.Events(t, u.UserID)
	require.Equal(t, "item.deleted", ev[1].Type)
	require.Equal(t, id, ev[1].Payload.Get("id").String())
}

func TestMoveItemBetweenLists(t *testing.T) {
	e := testkit.New(t)
	mia, ben := e.User(t, "mia"), e.User(t, "ben")
	uni := mia.Do(t, "POST", "/users/@me/lists", J{"name": "Uni"}).Get("id").String()
	bens := ben.Do(t, "POST", "/users/@me/lists", J{"name": "Bens"}).Get("id").String()
	id := mia.Do(t, "POST", "/users/@me/items", J{"title": "x"}).Get("id").String()

	moved := mia.Do(t, "PATCH", "/users/@me/items/"+id, J{"list_id": uni})
	require.Equal(t, 200, moved.Status)
	require.Equal(t, uni, moved.Get("list_id").String())

	foreign := mia.Do(t, "PATCH", "/users/@me/items/"+id, J{"list_id": bens})
	require.Equal(t, 422, foreign.Status)
	require.True(t, foreign.Get("error.fields.list_id").Exists())
	require.Equal(t, 422, mia.Do(t, "POST", "/users/@me/items", J{"title": "y", "list_id": bens}).Status)

	direct := mia.Do(t, "POST", "/users/@me/items", J{"title": "y", "list_id": uni})
	require.Equal(t, uni, direct.Get("list_id").String())
}

func TestReminderOverride(t *testing.T) {
	u := testkit.New(t).User(t, "mia")
	created := u.Do(t, "POST", "/users/@me/items", J{"title": "x", "due_date": "2026-11-13",
		"reminder_override": J{"mode": "custom", "before": []string{"1h", "2d"}}})
	require.Equal(t, 201, created.Status)
	require.JSONEq(t, `{"mode":"custom","before":["2d","1h"]}`, created.Get("reminder_override").Raw)
	id := created.Get("id").String()

	off := u.Do(t, "PATCH", "/users/@me/items/"+id, J{"reminder_override": J{"mode": "off"}})
	require.JSONEq(t, `{"mode":"off"}`, off.Get("reminder_override").Raw)

	none := u.Do(t, "PATCH", "/users/@me/items/"+id, J{"reminder_override": nil})
	require.Equal(t, gjson.Null, none.Get("reminder_override").Type)

	for _, bad := range []J{
		{"mode": "custom", "before": []string{"10s"}},
		{"mode": "sometimes"},
		{"mode": "custom"},
	} {
		require.Equal(t, 422, u.Do(t, "PATCH", "/users/@me/items/"+id, J{"reminder_override": bad}).Status, bad)
	}
}

func TestItemFiltersAndSort(t *testing.T) {
	u := testkit.New(t).User(t, "mia")
	uni := u.Do(t, "POST", "/users/@me/lists", J{"name": "Uni"}).Get("id").String()
	mk := func(body J) string {
		r := u.Do(t, "POST", "/users/@me/items", body)
		require.Equal(t, 201, r.Status, string(r.Body))
		return r.Get("id").String()
	}
	a := mk(J{"title": "a", "priority": "low"})
	b := mk(J{"title": "b", "priority": "high", "due_at": "2026-11-12T10:00:00Z"})
	c := mk(J{"title": "c", "due_date": "2026-11-20", "list_id": uni})
	d := mk(J{"title": "d", "priority": "high"})
	e := mk(J{"title": "e", "priority": "low", "due_at": "2026-11-15T10:00:00Z"})
	u.Do(t, "PATCH", "/users/@me/items/"+d, J{"status": "done"})

	list := func(query string) []string {
		r := u.Do(t, "GET", "/users/@me/items"+query, nil)
		require.Equal(t, 200, r.Status, string(r.Body))
		return ids(r, "data.#.id")
	}
	require.Equal(t, []string{a, b, c, e}, list(""))
	require.Equal(t, []string{d}, list("?status=done"))
	require.Equal(t, []string{a, b, c, d, e}, list("?status=all"))
	require.Equal(t, []string{b, c, e}, list("?has_due=true"))
	require.Equal(t, []string{a}, list("?has_due=false"))
	require.Equal(t, []string{b}, list("?due_before=2026-11-14T00:00:00Z"))
	require.Equal(t, []string{c, e}, list("?due_after=2026-11-14T00:00:00Z"))
	require.Equal(t, []string{c}, list("?list_id="+uni))
	require.Equal(t, []string{e, c, b, a}, list("?order=desc"))
	require.Equal(t, []string{b, e, c, a}, list("?sort=due"))
	require.Equal(t, []string{c, e, b, a}, list("?sort=due&order=desc"))
	require.Equal(t, []string{d, b, c, e, a}, list("?sort=priority&order=desc&status=all"))
	require.Equal(t, []string{a, e, c, b, d}, list("?sort=priority&status=all"))

	for _, bad := range []string{"?sort=title", "?order=up", "?status=archived", "?due_before=soon", "?has_due=maybe", "?list_id=nope"} {
		require.Equal(t, 422, u.Do(t, "GET", "/users/@me/items"+bad, nil, testkit.NoContract()).Status, bad)
	}
}

func TestPaginationIsStable(t *testing.T) {
	for _, query := range []string{"", "&sort=due", "&sort=due&order=desc", "&sort=priority&order=desc"} {
		t.Run(query, func(t *testing.T) {
			u := testkit.New(t).User(t, "mia")
			want := map[string]bool{}
			for i, body := range []J{
				{"title": "1", "priority": "high"},
				{"title": "2", "due_at": "2026-11-12T10:00:00Z"},
				{"title": "3", "due_at": "2026-11-12T10:00:00Z", "priority": "low"},
				{"title": "4"},
				{"title": "5", "due_date": "2026-11-12", "priority": "high"},
				{"title": "6", "priority": "low"},
				{"title": "7", "due_at": "2026-11-20T10:00:00Z"},
			} {
				r := u.Do(t, "POST", "/users/@me/items", body)
				require.Equal(t, 201, r.Status, i)
				want[r.Get("id").String()] = true
			}

			page := u.Do(t, "GET", "/users/@me/items?limit=3"+query, nil)
			seen := ids(page, "data.#.id")
			require.Len(t, seen, 3)
			require.Equal(t, 204, u.Do(t, "DELETE", "/users/@me/items/"+seen[0], nil).Status)
			require.Equal(t, 204, u.Do(t, "DELETE", "/users/@me/items/"+seen[2], nil).Status)

			for pages := 0; page.Get("next_cursor").String() != ""; pages++ {
				require.Less(t, pages, 5)
				page = u.Do(t, "GET", "/users/@me/items?limit=3"+query+"&cursor="+page.Get("next_cursor").String(), nil)
				require.Equal(t, 200, page.Status)
				seen = append(seen, ids(page, "data.#.id")...)
			}
			require.Equal(t, gjson.Null, page.Get("next_cursor").Type)
			require.Len(t, seen, 7)
			for _, id := range seen {
				require.True(t, want[id], "unknown or repeated id")
				delete(want, id)
			}
		})
	}
}

func TestGarbageCursorIs400(t *testing.T) {
	u := testkit.New(t).User(t, "mia")
	for i := 0; i < 3; i++ {
		u.Do(t, "POST", "/users/@me/items", J{"title": "x", "due_at": "2026-11-12T10:00:00Z"})
	}
	bad := u.Do(t, "GET", "/users/@me/items?cursor=abc", nil)
	require.Equal(t, 400, bad.Status)
	require.Equal(t, "invalid_cursor", bad.Get("error.code").String())

	dueCursor := u.Do(t, "GET", "/users/@me/items?limit=1&sort=due", nil).Get("next_cursor").String()
	require.NotEmpty(t, dueCursor)
	for _, query := range []string{"?cursor=", "?sort=due&order=desc&cursor=", "?sort=priority&cursor="} {
		r := u.Do(t, "GET", "/users/@me/items"+query+dueCursor, nil)
		require.Equal(t, 400, r.Status, query)
		require.Equal(t, "invalid_cursor", r.Get("error.code").String())
	}
}

func TestTitleLimitsCountCharacters(t *testing.T) {
	u := testkit.New(t).User(t, "mia")
	ok := u.Do(t, "POST", "/users/@me/items", J{"title": strings.Repeat("語", 500)})
	require.Equal(t, 201, ok.Status)
	require.Equal(t, 422, u.Do(t, "POST", "/users/@me/items", J{"title": strings.Repeat("語", 501)}).Status)
	require.Equal(t, 422, u.Do(t, "POST", "/users/@me/items", J{"title": "   "}).Status)
	require.Equal(t, 422, u.Do(t, "POST", "/users/@me/items", J{}).Status)
	require.Equal(t, 201, u.Do(t, "POST", "/users/@me/items", J{"title": "x", "note": strings.Repeat("ä", 10000)}).Status)
	require.Equal(t, 422, u.Do(t, "POST", "/users/@me/items", J{"title": "x", "note": strings.Repeat("ä", 10001)}).Status)
}

func TestItemsAreIsolated(t *testing.T) {
	e := testkit.New(t)
	mia, ben := e.User(t, "mia"), e.User(t, "ben")
	id := mia.Do(t, "POST", "/users/@me/items", J{"title": "geheim"}).Get("id").String()
	require.Equal(t, 404, ben.Do(t, "GET", "/users/@me/items/"+id, nil).Status)
	require.Equal(t, 404, ben.Do(t, "PATCH", "/users/@me/items/"+id, J{"title": "x"}).Status)
	require.Equal(t, 404, ben.Do(t, "DELETE", "/users/@me/items/"+id, nil).Status)
	require.Equal(t, 404, ben.Do(t, "GET", "/users/@me/items/not-a-uuid", nil).Status)
	require.Empty(t, ids(ben.Do(t, "GET", "/users/@me/items?status=all", nil), "data.#.id"))
	require.Equal(t, "geheim", mia.Do(t, "GET", "/users/@me/items/"+id, nil).Get("title").String())
}

func TestDeletingListMovesItemsToInbox(t *testing.T) {
	e := testkit.New(t)
	u := e.User(t, "mia")
	inbox := u.Do(t, "GET", "/users/@me/lists", nil).Get("0.id").String()
	uni := u.Do(t, "POST", "/users/@me/lists", J{"name": "Uni"}).Get("id").String()
	first := u.Do(t, "POST", "/users/@me/items", J{"title": "1", "list_id": uni}).Get("id").String()
	second := u.Do(t, "POST", "/users/@me/items", J{"title": "2", "list_id": uni}).Get("id").String()

	require.Equal(t, 204, u.Do(t, "DELETE", "/users/@me/lists/"+uni, nil).Status)
	for _, id := range []string{first, second} {
		require.Equal(t, inbox, u.Do(t, "GET", "/users/@me/items/"+id, nil).Get("list_id").String())
	}
	ev := e.Events(t, u.UserID)
	tail := ev[len(ev)-3:]
	require.Equal(t, []string{"item.updated", "item.updated", "list.deleted"}, []string{tail[0].Type, tail[1].Type, tail[2].Type})
	require.Equal(t, inbox, tail[0].Payload.Get("list_id").String())
}

func TestAllDayDueWhenMidnightIsSkippedOrRepeated(t *testing.T) {
	u := testkit.New(t).User(t, "mia")
	require.Equal(t, 200, u.Do(t, "PATCH", "/users/@me", J{"timezone": "America/Havana"}).Status)
	// Cuba moves its clocks at midnight: 2024-03-10 had no 00:00, 2024-11-03 had it twice.
	// The dates are in the past because time zone rules for the future keep changing.
	for date, want := range map[string]string{
		"2024-03-09": "2024-03-10T04:59:59Z",
		"2024-11-02": "2024-11-03T03:59:59Z",
		"2024-06-15": "2024-06-16T03:59:59Z",
		"2024-12-15": "2024-12-16T04:59:59Z",
	} {
		r := u.Do(t, "POST", "/users/@me/items", J{"title": date, "due_date": date})
		require.Equal(t, 201, r.Status)
		require.Equal(t, want, r.Get("due_at").String(), date)
	}
	before := u.Do(t, "GET", "/users/@me/items?due_before=2024-03-10T05:00:00Z", nil)
	require.Len(t, ids(before, "data.#.id"), 1, "filters agree with the due_at that responses show")
	none := u.Do(t, "GET", "/users/@me/items?due_before=2024-03-10T04:59:59Z", nil)
	require.Empty(t, ids(none, "data.#.id"))
}

func TestDuePagingTerminatesInMidnightTransitionZone(t *testing.T) {
	u := testkit.New(t).User(t, "mia")
	u.Do(t, "PATCH", "/users/@me", J{"timezone": "America/Havana"})
	for _, date := range []string{"2024-03-09", "2024-03-09", "2024-03-09", "2024-11-02", "2024-11-02"} {
		u.Do(t, "POST", "/users/@me/items", J{"title": "x", "due_date": date})
	}
	for _, order := range []string{"asc", "desc"} {
		seen := map[string]bool{}
		page := u.Do(t, "GET", "/users/@me/items?sort=due&limit=1&order="+order, nil)
		for pages := 0; ; pages++ {
			require.Less(t, pages, 8, "paging must end")
			for _, id := range ids(page, "data.#.id") {
				require.False(t, seen[id], "an item came twice")
				seen[id] = true
			}
			next := page.Get("next_cursor").String()
			if next == "" {
				break
			}
			page = u.Do(t, "GET", "/users/@me/items?sort=due&limit=1&order="+order+"&cursor="+next, nil)
		}
		require.Len(t, seen, 5, order)
	}
}

func TestTimezoneMustBeARealZoneName(t *testing.T) {
	u := testkit.New(t).User(t, "mia")
	r := u.Do(t, "PATCH", "/users/@me", J{"timezone": "Local"})
	require.Equal(t, 422, r.Status)
	ok := u.Do(t, "PATCH", "/users/@me", J{"timezone": "Asia/Kolkata"})
	require.Equal(t, 200, ok.Status)
	require.Equal(t, 200, u.Do(t, "GET", "/users/@me/items", nil).Status)
}

func TestConcurrentPatchesKeepBothChanges(t *testing.T) {
	u := testkit.New(t).User(t, "mia")
	for round := 0; round < 12; round++ {
		id := u.Do(t, "POST", "/users/@me/items", J{"title": "before"}).Get("id").String()
		var wg sync.WaitGroup
		for _, body := range []J{{"title": "renamed"}, {"status": "done"}} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				u.Do(t, "PATCH", "/users/@me/items/"+id, body, testkit.NoContract())
			}()
		}
		wg.Wait()
		got := u.Do(t, "GET", "/users/@me/items/"+id, nil)
		require.Equal(t, []string{"renamed", "done"}, []string{got.Get("title").String(), got.Get("status").String()}, round)
	}
}

func TestConcurrentDeletesLogOneEvent(t *testing.T) {
	e := testkit.New(t)
	u := e.User(t, "mia")
	for round := 0; round < 8; round++ {
		id := u.Do(t, "POST", "/users/@me/items", J{"title": "x"}).Get("id").String()
		var wg sync.WaitGroup
		for i := 0; i < 4; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				u.Do(t, "DELETE", "/users/@me/items/"+id, nil, testkit.NoContract())
			}()
		}
		wg.Wait()
	}
	deleted := 0
	for _, ev := range e.Events(t, u.UserID) {
		if ev.Type == "item.deleted" {
			deleted++
		}
	}
	require.Equal(t, 8, deleted)
}

func TestCreatingIntoAListBeingDeletedNeverFails(t *testing.T) {
	u := testkit.New(t).User(t, "mia")
	for round := 0; round < 10; round++ {
		list := u.Do(t, "POST", "/users/@me/lists", J{"name": "tmp"}).Get("id").String()
		statuses := make([]int, 4)
		var wg sync.WaitGroup
		for i := range statuses {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if i == 0 {
					statuses[i] = u.Do(t, "DELETE", "/users/@me/lists/"+list, nil, testkit.NoContract()).Status
					return
				}
				statuses[i] = u.Do(t, "POST", "/users/@me/items", J{"title": "x", "list_id": list}, testkit.NoContract()).Status
			}()
		}
		wg.Wait()
		require.Equal(t, 204, statuses[0])
		for _, s := range statuses[1:] {
			require.Contains(t, []int{201, 422}, s)
		}
	}
	inbox := u.Do(t, "GET", "/users/@me/lists", nil).Get("0.id").String()
	for _, listID := range ids(u.Do(t, "GET", "/users/@me/items?limit=100", nil), "data.#.list_id") {
		require.Equal(t, inbox, listID)
	}
}

func TestNULInTextIsRejected(t *testing.T) {
	u := testkit.New(t).User(t, "mia")
	for _, tc := range []struct {
		method, path string
		body         testkit.Raw
	}{
		{"POST", "/users/@me/items", `{"title":"a\u0000b"}`},
		{"POST", "/users/@me/items", `{"title":"ok","note":"a\u0000b"}`},
		{"POST", "/users/@me/lists", `{"name":"a\u0000b"}`},
		{"PATCH", "/users/@me", `{"display_name":"a\u0000b"}`},
	} {
		r := u.Do(t, tc.method, tc.path, tc.body)
		require.Equal(t, 422, r.Status, string(tc.body))
	}
	ok := u.Do(t, "POST", "/users/@me/items", testkit.Raw(`{"title":"C:\\u0000 is just text"}`))
	require.Equal(t, 201, ok.Status)
	require.Equal(t, `C:\u0000 is just text`, ok.Get("title").String())
}
