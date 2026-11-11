package httpapi_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/rayakame/plsremind/hub/internal/testkit"
)

func TestRemindIn2h(t *testing.T) {
	e := testkit.New(t)
	u := e.User(t, "mia")
	id := u.Do(t, "POST", "/users/@me/items", J{"title": "Wäsche"}).Get("id").String()
	r := u.Do(t, "POST", "/users/@me/items/"+id+"/reminders", J{"in": "2h"})
	require.Equal(t, 201, r.Status)
	require.Equal(t, e.Clock.Now().Add(2*time.Hour).UTC().Format(time.RFC3339), r.Get("remind_at").String())
	require.Equal(t, id, r.Get("item_id").String())

	at := u.Do(t, "POST", "/users/@me/items/"+id+"/reminders", J{"at": "2026-11-11T13:30:00+01:00"})
	require.Equal(t, 201, at.Status)
	require.Equal(t, "2026-11-11T12:30:00Z", at.Get("remind_at").String())

	list := u.Do(t, "GET", "/users/@me/items/"+id+"/reminders", nil)
	require.Equal(t, 200, list.Status)
	require.Equal(t, []string{at.Get("id").String(), r.Get("id").String()}, ids(list, "#.id"), "sorted by time")
}

func TestReminderValidation(t *testing.T) {
	e := testkit.New(t)
	u := e.User(t, "mia")
	id := u.Do(t, "POST", "/users/@me/items", J{"title": "x"}).Get("id").String()
	path := "/users/@me/items/" + id + "/reminders"
	for _, body := range []J{
		{"at": "2026-11-11T11:00:00Z"},
		{"at": "2026-11-12T11:00:00Z", "in": "2h"},
		{},
		{"in": "soon"},
		{"at": "tomorrow"},
	} {
		require.Equal(t, 422, u.Do(t, "POST", path, body).Status, body)
	}
	u.Do(t, "PATCH", "/users/@me/items/"+id, J{"status": "done"})
	done := u.Do(t, "POST", path, J{"in": "2h"})
	require.Equal(t, 409, done.Status)
	require.Equal(t, "item_done", done.Get("error.code").String())
}

func TestReminderLimit(t *testing.T) {
	u := testkit.New(t).User(t, "mia")
	id := u.Do(t, "POST", "/users/@me/items", J{"title": "x"}).Get("id").String()
	for i := 0; i < 20; i++ {
		require.Equal(t, 201, u.Do(t, "POST", "/users/@me/items/"+id+"/reminders", J{"in": "2h"}).Status)
	}
	r := u.Do(t, "POST", "/users/@me/items/"+id+"/reminders", J{"in": "2h"})
	require.Equal(t, 409, r.Status)
	require.Equal(t, "reminder_limit", r.Get("error.code").String())
}

func TestReminderEventsAndDelete(t *testing.T) {
	e := testkit.New(t)
	mia, ben := e.User(t, "mia"), e.User(t, "ben")
	id := mia.Do(t, "POST", "/users/@me/items", J{"title": "x"}).Get("id").String()
	rem := mia.Do(t, "POST", "/users/@me/items/"+id+"/reminders", J{"in": "2h"}).Get("id").String()
	keep := mia.Do(t, "POST", "/users/@me/items/"+id+"/reminders", J{"in": "3h"}).Get("id").String()

	require.Equal(t, 404, ben.Do(t, "DELETE", "/users/@me/items/"+id+"/reminders/"+rem, nil).Status)
	require.Equal(t, 404, ben.Do(t, "GET", "/users/@me/items/"+id+"/reminders", nil).Status)
	require.Equal(t, 204, mia.Do(t, "DELETE", "/users/@me/items/"+id+"/reminders/"+rem, nil).Status)
	require.Equal(t, []string{keep}, ids(mia.Do(t, "GET", "/users/@me/items/"+id+"/reminders", nil), "#.id"))

	ev := e.Events(t, mia.UserID)
	require.Equal(t, []string{"item.created", "reminder.created", "reminder.created", "reminder.deleted"},
		[]string{ev[0].Type, ev[1].Type, ev[2].Type, ev[3].Type})
	require.Equal(t, rem, ev[1].Payload.Get("id").String())
	require.Equal(t, rem, ev[3].Payload.Get("id").String())

	mia.Do(t, "DELETE", "/users/@me/items/"+id, nil)
	var left int
	require.NoError(t, e.Pool.QueryRow(context.Background(), "SELECT count(*) FROM manual_reminders").Scan(&left))
	require.Zero(t, left)
}
