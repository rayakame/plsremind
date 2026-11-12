package httpapi_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/rayakame/plsremind/hub/internal/testkit"
)

func TestMutationsSignalAfterCommit(t *testing.T) {
	e := testkit.New(t)
	u := e.User(t, "mia")
	tap := e.Signals(t)

	require.Equal(t, 422, u.Do(t, "POST", "/users/@me/items", J{"title": ""}).Status)
	tap.ExpectNone(t)

	require.Equal(t, 201, u.Do(t, "POST", "/users/@me/items", J{"title": "x"}).Status)
	s := tap.Next(t)
	require.Equal(t, "event", s.Type)
	require.Equal(t, u.UserID, s.UserID.String())
	tap.ExpectNone(t)
}

func TestNewEventTypes(t *testing.T) {
	e := testkit.New(t)
	mia := e.User(t, "mia")
	bot := e.Connection(t, "discord")
	e.GiveIdentity(t, mia.UserID, "discord", "1234")

	mia.Do(t, "PATCH", "/users/@me", J{"display_name": "Mia B"})
	mia.Do(t, "PUT", "/users/@me/reminder-plans/high", J{"before": []string{"1h"}, "follow_up": "never"})
	mia.Do(t, "DELETE", "/users/@me/reminder-plans/high", nil)
	mia.Do(t, "PUT", "/users/@me/connections/"+bot.ID, J{"remind": false})

	ev := e.Events(t, mia.UserID)
	require.Len(t, ev, 4)
	require.Equal(t, []string{"user.updated", "plan.updated", "plan.updated", "connection.updated"},
		[]string{ev[0].Type, ev[1].Type, ev[2].Type, ev[3].Type})
	require.Equal(t, "Mia B", ev[0].Payload.Get("display_name").String())
	require.JSONEq(t, `["1h"]`, ev[1].Payload.Get("before").Raw)
	require.Equal(t, "high", ev[1].Payload.Get("priority").String())
	require.True(t, ev[2].Payload.Get("is_default").Bool())
	require.JSONEq(t, `["7d","3d","1d","3h"]`, ev[2].Payload.Get("before").Raw)
	require.False(t, ev[3].Payload.Get("remind").Bool())
	require.Equal(t, bot.ID, ev[3].Payload.Get("id").String())
}

func TestChildDeletesNameTheirItem(t *testing.T) {
	e := testkit.New(t)
	u := e.User(t, "mia")
	item := u.Do(t, "POST", "/users/@me/items", J{"title": "x", "subitems": []string{"a"}})
	id := item.Get("id").String()
	sub := item.Get("subitems.0.id").String()
	rem := u.Do(t, "POST", "/users/@me/items/"+id+"/reminders", J{"in": "2h"}).Get("id").String()
	u.Do(t, "DELETE", "/users/@me/items/"+id+"/subitems/"+sub, nil)
	u.Do(t, "DELETE", "/users/@me/items/"+id+"/reminders/"+rem, nil)

	ev := e.Events(t, u.UserID)
	subDeleted, remDeleted := ev[len(ev)-4], ev[len(ev)-2]
	require.Equal(t, []string{"subitem.deleted", sub, id}, []string{subDeleted.Type, subDeleted.Payload.Get("id").String(), subDeleted.Payload.Get("item_id").String()})
	require.Equal(t, []string{"reminder.deleted", rem, id}, []string{remDeleted.Type, remDeleted.Payload.Get("id").String(), remDeleted.Payload.Get("item_id").String()})
}

func TestAccessChangesSignal(t *testing.T) {
	e := testkit.New(t)
	admin := e.Admin(t)
	mia := e.User(t, "mia")
	bot := e.Connection(t, "discord")
	e.GiveIdentity(t, mia.UserID, "discord", "1234")
	tok := mia.Do(t, "POST", "/users/@me/tokens", J{"name": "cli"}).Get("id").String()
	tap := e.Signals(t)

	mia.Do(t, "PUT", "/users/@me/connections/"+bot.ID, J{"enabled": false})
	grant := tap.NextOf(t, "grant")
	require.Equal(t, []string{mia.UserID, bot.ID}, []string{grant.UserID.String(), grant.ConnectionID.String()})

	mia.Do(t, "DELETE", "/users/@me/tokens/"+tok, nil)
	require.Equal(t, mia.UserID, tap.NextOf(t, "user").UserID.String())

	admin.Do(t, "PATCH", "/admin/connections/"+bot.ID, J{"allowed_intents": []string{"deliveries"}})
	require.Equal(t, bot.ID, tap.NextOf(t, "connection").ConnectionID.String())
	admin.Do(t, "POST", "/admin/connections/"+bot.ID+"/token", nil)
	require.Equal(t, bot.ID, tap.NextOf(t, "connection").ConnectionID.String())

	admin.Do(t, "PATCH", "/admin/users/"+mia.UserID, J{"disabled": true})
	require.Equal(t, mia.UserID, tap.NextOf(t, "user").UserID.String())
	admin.Do(t, "PATCH", "/admin/users/"+mia.UserID, J{"disabled": false})
	require.Equal(t, mia.UserID, tap.NextOf(t, "user").UserID.String())

	fresh := e.Do(t, "POST", "/auth/login", J{"username": "mia", "password": testkit.Password})
	e.Do(t, "POST", "/auth/logout", nil, testkit.Cookie("plsr_session", fresh.Cookie("plsr_session")))
	require.Equal(t, mia.UserID, tap.NextOf(t, "user").UserID.String())

	admin.Do(t, "PUT", "/admin/users/"+mia.UserID+"/password", J{"new_password": "reset by the admin"})
	require.Equal(t, mia.UserID, tap.NextOf(t, "user").UserID.String())

	admin.Do(t, "DELETE", "/admin/connections/"+bot.ID, nil)
	require.Equal(t, bot.ID, tap.NextOf(t, "connection").ConnectionID.String())
	admin.Do(t, "DELETE", "/admin/users/"+mia.UserID, nil)
	require.Equal(t, mia.UserID, tap.NextOf(t, "user").UserID.String())
}

func TestSweepPrunesOldEvents(t *testing.T) {
	e := testkit.New(t)
	u := e.User(t, "mia")
	u.Do(t, "POST", "/users/@me/items", J{"title": "old"})
	e.Clock.Advance(25 * time.Hour)
	u.Do(t, "POST", "/users/@me/items", J{"title": "new"})
	e.Sweep(t)
	ev := e.Events(t, u.UserID)
	require.Len(t, ev, 1)
	require.Equal(t, int64(2), ev[0].Seq, "numbers are never reused")
	u.Do(t, "POST", "/users/@me/items", J{"title": "newer"})
	require.Equal(t, int64(3), e.Events(t, u.UserID)[1].Seq)
}
