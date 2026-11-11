package httpapi_test

import (
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/rayakame/plsremind/hub/internal/testkit"
)

func TestNewUserHasInbox(t *testing.T) {
	e := testkit.New(t)
	r := e.User(t, "mia").Do(t, "GET", "/users/@me/lists", nil)
	require.Equal(t, 200, r.Status)
	require.Equal(t, int64(1), r.Get("#").Int())
	require.True(t, r.Get("0.is_inbox").Bool())
	require.Equal(t, "Inbox", r.Get("0.name").String())
}

func TestListCRUD(t *testing.T) {
	e := testkit.New(t)
	u := e.User(t, "mia")
	created := u.Do(t, "POST", "/users/@me/lists", J{"name": "  Uni "})
	require.Equal(t, 201, created.Status)
	require.Equal(t, "Uni", created.Get("name").String())
	require.Equal(t, int64(1), created.Get("position").Int())
	require.False(t, created.Get("is_inbox").Bool())
	id := created.Get("id").String()

	renamed := u.Do(t, "PATCH", "/users/@me/lists/"+id, J{"name": "Studium"})
	require.Equal(t, 200, renamed.Status)
	require.Equal(t, "Studium", renamed.Get("name").String())

	require.Equal(t, 422, u.Do(t, "POST", "/users/@me/lists", J{"name": ""}).Status)
	require.Equal(t, 204, u.Do(t, "DELETE", "/users/@me/lists/"+id, nil).Status)
	require.Equal(t, int64(1), u.Do(t, "GET", "/users/@me/lists", nil).Get("#").Int())
}

func TestInboxIsProtected(t *testing.T) {
	e := testkit.New(t)
	u := e.User(t, "mia")
	inbox := u.Do(t, "GET", "/users/@me/lists", nil).Get("0.id").String()
	for _, r := range []*testkit.Resp{
		u.Do(t, "PATCH", "/users/@me/lists/"+inbox, J{"name": "Other"}),
		u.Do(t, "DELETE", "/users/@me/lists/"+inbox, nil),
	} {
		require.Equal(t, 409, r.Status)
		require.Equal(t, "inbox_protected", r.Get("error.code").String())
	}
}

func TestReorderLists(t *testing.T) {
	e := testkit.New(t)
	u := e.User(t, "mia")
	inbox := u.Do(t, "GET", "/users/@me/lists", nil).Get("0.id").String()
	a := u.Do(t, "POST", "/users/@me/lists", J{"name": "A"}).Get("id").String()
	b := u.Do(t, "POST", "/users/@me/lists", J{"name": "B"}).Get("id").String()

	r := u.Do(t, "PUT", "/users/@me/lists/order", J{"ids": []string{b, a, inbox}})
	require.Equal(t, 200, r.Status)
	got := u.Do(t, "GET", "/users/@me/lists", nil)
	require.Equal(t, []string{b, a, inbox}, []string{got.Get("0.id").String(), got.Get("1.id").String(), got.Get("2.id").String()})
	require.Equal(t, int64(0), got.Get("0.position").Int())

	require.Equal(t, 422, u.Do(t, "PUT", "/users/@me/lists/order", J{"ids": []string{b, a}}).Status)
	require.Equal(t, 422, u.Do(t, "PUT", "/users/@me/lists/order", J{"ids": []string{b, a, a}}).Status)
}

func TestListsAreIsolated(t *testing.T) {
	e := testkit.New(t)
	mia, ben := e.User(t, "mia"), e.User(t, "ben")
	id := ben.Do(t, "POST", "/users/@me/lists", J{"name": "Bens"}).Get("id").String()
	require.Equal(t, 404, mia.Do(t, "PATCH", "/users/@me/lists/"+id, J{"name": "x"}).Status)
	require.Equal(t, 404, mia.Do(t, "DELETE", "/users/@me/lists/"+id, nil).Status)
	require.Equal(t, 404, mia.Do(t, "PATCH", "/users/@me/lists/not-a-uuid", J{"name": "x"}).Status)
	require.Equal(t, int64(1), mia.Do(t, "GET", "/users/@me/lists", nil).Get("#").Int())
}

func TestListMutationsAreLogged(t *testing.T) {
	e := testkit.New(t)
	u := e.User(t, "mia")
	id := u.Do(t, "POST", "/users/@me/lists", J{"name": "Uni"}).Get("id").String()
	u.Do(t, "DELETE", "/users/@me/lists/"+id, nil)
	ev := e.Events(t, u.UserID)
	require.Len(t, ev, 2)
	require.Equal(t, []any{int64(1), "list.created", "Uni", "session"}, []any{ev[0].Seq, ev[0].Type, ev[0].Payload.Get("name").String(), ev[0].ActorKind})
	require.Equal(t, []any{int64(2), "list.deleted", id}, []any{ev[1].Seq, ev[1].Type, ev[1].Payload.Get("id").String()})
}

func TestTokenActorIsLogged(t *testing.T) {
	e := testkit.New(t)
	u := e.User(t, "mia")
	tok := u.Do(t, "POST", "/users/@me/tokens", J{"name": "cli"})
	e.Do(t, "POST", "/users/@me/lists", J{"name": "Uni"}, testkit.Bearer(tok.Get("token").String()))
	ev := e.Events(t, u.UserID)
	require.Len(t, ev, 1)
	require.Equal(t, "personal_token", ev[0].ActorKind)
	require.Equal(t, tok.Get("id").String(), ev[0].ActorID)
}

func TestEventSeqIsGaplessUnderConcurrency(t *testing.T) {
	e := testkit.New(t)
	u := e.User(t, "mia")
	const n = 40
	statuses := make([]int, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			statuses[i] = u.Do(t, "POST", "/users/@me/lists", J{"name": fmt.Sprintf("list %d", i)}, testkit.NoContract()).Status
		}()
	}
	wg.Wait()
	for _, s := range statuses {
		require.Equal(t, 201, s)
	}
	ev := e.Events(t, u.UserID)
	require.Len(t, ev, n)
	for i, event := range ev {
		require.Equal(t, int64(i+1), event.Seq)
	}
	lists := u.Do(t, "GET", "/users/@me/lists", nil)
	seen := map[int64]bool{}
	for _, l := range lists.Get("#.position").Array() {
		require.False(t, seen[l.Int()], "positions must be unique")
		seen[l.Int()] = true
	}
}
