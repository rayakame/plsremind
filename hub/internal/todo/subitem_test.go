package todo_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/rayakame/plsremind/hub/internal/testkit"
)

func TestCreateItemWithSubitems(t *testing.T) {
	e := testkit.New(t)
	u := e.User(t, "mia")
	r := u.Do(t, "POST", "/users/@me/items", J{"title": "Projektabgabe", "subitems": []string{"Code fertig", " Doku "}})
	require.Equal(t, 201, r.Status)
	require.Equal(t, int64(2), r.Get("subitems.#").Int())
	require.Equal(t, "Code fertig", r.Get("subitems.0.title").String())
	require.Equal(t, "Doku", r.Get("subitems.1.title").String())
	require.Equal(t, int64(0), r.Get("subitems.0.position").Int())
	require.Equal(t, int64(1), r.Get("subitems.1.position").Int())
	require.False(t, r.Get("subitems.0.done").Bool())
	require.Equal(t, r.Get("id").String(), r.Get("subitems.0.item_id").String())

	ev := e.Events(t, u.UserID)
	require.Len(t, ev, 1)
	require.Equal(t, int64(2), ev[0].Payload.Get("subitems.#").Int())

	require.Equal(t, 422, u.Do(t, "POST", "/users/@me/items", J{"title": "x", "subitems": []string{""}}).Status)
}

func TestSubitemCRUDAndOrder(t *testing.T) {
	e := testkit.New(t)
	u := e.User(t, "mia")
	item := u.Do(t, "POST", "/users/@me/items", J{"title": "Projektabgabe"}).Get("id").String()
	base := "/users/@me/items/" + item + "/subitems"

	a := u.Do(t, "POST", base, J{"title": "a"})
	require.Equal(t, 201, a.Status)
	b := u.Do(t, "POST", base, J{"title": "b"})
	require.Equal(t, int64(1), b.Get("position").Int())
	aID, bID := a.Get("id").String(), b.Get("id").String()

	done := u.Do(t, "PATCH", base+"/"+aID, J{"done": true, "title": "a!"})
	require.Equal(t, 200, done.Status)
	require.True(t, done.Get("done").Bool())
	require.Equal(t, "a!", done.Get("title").String())

	order := u.Do(t, "PUT", base+"/order", J{"ids": []string{bID, aID}})
	require.Equal(t, 200, order.Status)
	got := u.Do(t, "GET", "/users/@me/items/"+item, nil)
	require.Equal(t, []string{bID, aID}, testkit.Strings(got, "subitems.#.id"))
	require.Equal(t, 422, u.Do(t, "PUT", base+"/order", J{"ids": []string{bID}}).Status)

	require.Equal(t, 204, u.Do(t, "DELETE", base+"/"+bID, nil).Status)
	require.Equal(t, []string{aID}, testkit.Strings(u.Do(t, "GET", "/users/@me/items/"+item, nil), "subitems.#.id"))
	require.Equal(t, 404, u.Do(t, "DELETE", base+"/"+bID, nil).Status)
	require.Equal(t, 422, u.Do(t, "POST", base, J{"title": " "}).Status)

	var types []string
	for _, ev := range e.Events(t, u.UserID) {
		types = append(types, ev.Type)
	}
	// Every change to the checklist is followed by the item in its new state.
	require.Equal(t, []string{
		"item.created",
		"subitem.created", "item.updated",
		"subitem.created", "item.updated",
		"subitem.updated", "item.updated",
		"subitem.updated", "subitem.updated", "item.updated",
		"subitem.deleted", "item.updated",
	}, types)
}

func TestSubitemsDoNotCompleteItem(t *testing.T) {
	u := testkit.New(t).User(t, "mia")
	r := u.Do(t, "POST", "/users/@me/items", J{"title": "x", "subitems": []string{"a", "b"}})
	item := r.Get("id").String()
	for _, sub := range testkit.Strings(r, "subitems.#.id") {
		u.Do(t, "PATCH", "/users/@me/items/"+item+"/subitems/"+sub, J{"done": true})
	}
	require.Equal(t, "open", u.Do(t, "GET", "/users/@me/items/"+item, nil).Get("status").String())

	other := u.Do(t, "POST", "/users/@me/items", J{"title": "y", "subitems": []string{"a"}})
	closed := u.Do(t, "PATCH", "/users/@me/items/"+other.Get("id").String(), J{"status": "done"})
	require.False(t, closed.Get("subitems.0.done").Bool())
}

func TestSubitemChangeTouchesItem(t *testing.T) {
	e := testkit.New(t)
	u := e.User(t, "mia")
	r := u.Do(t, "POST", "/users/@me/items", J{"title": "x"})
	item := r.Get("id").String()
	e.Clock.Advance(60_000_000_000)
	u.Do(t, "POST", "/users/@me/items/"+item+"/subitems", J{"title": "a"})
	require.Equal(t, "2026-11-11T12:01:00Z", u.Do(t, "GET", "/users/@me/items/"+item, nil).Get("updated_at").String())
}

func TestSubitemLimit(t *testing.T) {
	u := testkit.New(t).User(t, "mia")
	item := u.Do(t, "POST", "/users/@me/items", J{"title": "x"}).Get("id").String()
	for i := 0; i < 50; i++ {
		require.Equal(t, 201, u.Do(t, "POST", "/users/@me/items/"+item+"/subitems", J{"title": "s"}).Status)
	}
	r := u.Do(t, "POST", "/users/@me/items/"+item+"/subitems", J{"title": "s"})
	require.Equal(t, 409, r.Status)
	require.Equal(t, "subitem_limit", r.Get("error.code").String())
}

func TestSubitemUnderForeignItemIs404(t *testing.T) {
	e := testkit.New(t)
	mia, ben := e.User(t, "mia"), e.User(t, "ben")
	r := mia.Do(t, "POST", "/users/@me/items", J{"title": "x", "subitems": []string{"a"}})
	item, sub := r.Get("id").String(), r.Get("subitems.0.id").String()
	other := mia.Do(t, "POST", "/users/@me/items", J{"title": "y"}).Get("id").String()

	require.Equal(t, 404, ben.Do(t, "POST", "/users/@me/items/"+item+"/subitems", J{"title": "b"}).Status)
	require.Equal(t, 404, ben.Do(t, "PATCH", "/users/@me/items/"+item+"/subitems/"+sub, J{"done": true}).Status)
	require.Equal(t, 404, ben.Do(t, "DELETE", "/users/@me/items/"+item+"/subitems/"+sub, nil).Status)
	require.Equal(t, 404, mia.Do(t, "PATCH", "/users/@me/items/"+other+"/subitems/"+sub, J{"done": true}).Status, "the subitem belongs to another item")
}
