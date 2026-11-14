package todo_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/rayakame/plsremind/hub/internal/testkit"
	"github.com/rayakame/plsremind/hub/internal/todo"
)

// completedInOrder creates items a to e and completes c, then a minute later a and e at the same instant.
func completedInOrder(t *testing.T, e *testkit.Env, u *testkit.Client) map[string]string {
	t.Helper()
	ids := map[string]string{}
	for _, title := range []string{"a", "b", "c", "d", "e"} {
		ids[title] = u.Do(t, "POST", "/users/@me/items", J{"title": title}).Get("id").String()
	}
	done := func(title string) {
		require.Equal(t, 200, u.Do(t, "PATCH", "/users/@me/items/"+ids[title], J{"status": "done"}).Status)
	}
	done("c")
	e.Clock.Advance(time.Minute)
	done("a")
	done("e")
	return ids
}

func TestItemsSortByCompletion(t *testing.T) {
	e := testkit.New(t)
	mia := e.User(t, "mia")
	completedInOrder(t, e, mia)

	page := mia.Do(t, "GET", "/users/@me/items?status=done&sort=completed&order=desc&limit=2", nil)
	require.Equal(t, 200, page.Status, string(page.Body))
	require.Equal(t, []string{"e", "a"}, testkit.Strings(page, "data.#.title"), "equal times fall back to the id")
	next := mia.Do(t, "GET", "/users/@me/items?status=done&sort=completed&order=desc&limit=2&cursor="+page.Get("next_cursor").String(), nil)
	require.Equal(t, []string{"c"}, testkit.Strings(next, "data.#.title"))
	require.Equal(t, gjson.Null, next.Get("next_cursor").Type)

	asc := mia.Do(t, "GET", "/users/@me/items?status=done&sort=completed", nil)
	require.Equal(t, []string{"c", "a", "e"}, testkit.Strings(asc, "data.#.title"))
	first := mia.Do(t, "GET", "/users/@me/items?status=done&sort=completed&limit=1", nil)
	rest := mia.Do(t, "GET", "/users/@me/items?status=done&sort=completed&limit=5&cursor="+first.Get("next_cursor").String(), nil)
	require.Equal(t, []string{"a", "e"}, testkit.Strings(rest, "data.#.title"))
}

func TestSortCompletedNeedsStatusDone(t *testing.T) {
	e := testkit.New(t)
	mia := e.User(t, "mia")
	for _, query := range []string{"?sort=completed", "?sort=completed&status=open", "?sort=completed&status=all"} {
		r := mia.Do(t, "GET", "/users/@me/items"+query, nil)
		require.Equal(t, 422, r.Status, query)
		require.NotEmpty(t, r.Get("error.fields.sort").String(), query)
	}
}

func TestCompletedCursorIsBoundToItsSort(t *testing.T) {
	e := testkit.New(t)
	mia := e.User(t, "mia")
	completedInOrder(t, e, mia)
	completed := mia.Do(t, "GET", "/users/@me/items?status=done&sort=completed&limit=1", nil).Get("next_cursor").String()
	created := mia.Do(t, "GET", "/users/@me/items?status=done&limit=1", nil).Get("next_cursor").String()
	require.NotEmpty(t, completed)
	require.NotEmpty(t, created)
	for _, query := range []string{
		"?status=done&cursor=" + completed,
		"?status=done&sort=completed&order=desc&cursor=" + completed,
		"?status=done&sort=completed&cursor=" + created,
	} {
		r := mia.Do(t, "GET", "/users/@me/items"+query, nil)
		require.Equal(t, 400, r.Status, query)
		require.Equal(t, "invalid_cursor", r.Get("error.code").String(), query)
	}
}

func TestSortCompletedHonoursListFilter(t *testing.T) {
	e := testkit.New(t)
	mia := e.User(t, "mia")
	uni := mia.Do(t, "POST", "/users/@me/lists", J{"name": "Uni"}).Get("id").String()
	for _, it := range []J{{"title": "inbox"}, {"title": "uni", "list_id": uni}} {
		id := mia.Do(t, "POST", "/users/@me/items", it).Get("id").String()
		mia.Do(t, "PATCH", "/users/@me/items/"+id, J{"status": "done"})
	}
	r := mia.Do(t, "GET", "/users/@me/items?status=done&sort=completed&order=desc&list_id="+uni, nil)
	require.Equal(t, []string{"uni"}, testkit.Strings(r, "data.#.title"))
}

func TestHistoryCursorContinuesAfterAnItem(t *testing.T) {
	e := testkit.New(t)
	mia := e.User(t, "mia")
	completedInOrder(t, e, mia)
	ctx := context.Background()
	userID := uuid.MustParse(mia.UserID)

	newest, err := todo.History(ctx, e.Deps.Q(), userID, nil, 2)
	require.NoError(t, err)
	require.Len(t, newest, 2)
	require.Equal(t, []string{"e", "a"}, []string{newest[0].Title, newest[1].Title})

	older, err := todo.History(ctx, e.Deps.Q(), userID, &todo.HistoryPos{CompletedAt: *newest[1].CompletedAt, ID: newest[1].ID}, 2)
	require.NoError(t, err)
	require.Len(t, older, 1)
	require.Equal(t, "c", older[0].Title)

	rest := mia.Do(t, "GET", "/users/@me/items?status=done&sort=completed&order=desc&cursor="+todo.HistoryCursor(newest[1]), nil)
	require.Equal(t, 200, rest.Status, string(rest.Body))
	require.Equal(t, []string{"c"}, testkit.Strings(rest, "data.#.title"))
}
