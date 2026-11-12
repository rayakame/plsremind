package httpapi_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/rayakame/plsremind/hub/internal/testkit"
)

func TestConnectionFindsLinkedUserAndActs(t *testing.T) {
	e := testkit.New(t)
	mia := e.User(t, "mia")
	bot := e.Connection(t, "discord")
	e.GiveIdentity(t, mia.UserID, "discord", "1234")

	r := bot.Do(t, "GET", "/connection/users/lookup?external_id=1234", nil)
	require.Equal(t, 200, r.Status)
	require.Equal(t, mia.UserID, r.Get("id").String())
	require.Equal(t, "mia", r.Get("display_name").String())
	require.Equal(t, "UTC", r.Get("timezone").String())

	base := "/users/" + mia.UserID
	it := bot.Do(t, "POST", base+"/items", J{"title": "vom Bot", "subitems": []string{"a"}})
	require.Equal(t, 201, it.Status)
	id := it.Get("id").String()
	require.Equal(t, 200, bot.Do(t, "PATCH", base+"/items/"+id, J{"status": "done"}).Status)
	require.Equal(t, 200, bot.Do(t, "GET", base+"/items?status=all", nil).Status)
	require.Equal(t, 200, bot.Do(t, "GET", base+"/lists", nil).Status)
	require.Equal(t, 201, bot.Do(t, "POST", base+"/lists", J{"name": "Bot"}).Status)
	require.Equal(t, 200, bot.Do(t, "GET", base, nil).Status)
	require.Equal(t, 200, bot.Do(t, "GET", base+"/reminder-plans", nil).Status)

	ev := e.Events(t, mia.UserID)
	require.Equal(t, "connection", ev[len(ev)-1].ActorKind)
	require.Equal(t, bot.ID, ev[len(ev)-1].ActorID)
	require.Equal(t, "vom Bot", mia.Do(t, "GET", "/users/@me/items/"+id, nil).Get("title").String())
}

func TestConnectionCannotReachUnlinkedUser(t *testing.T) {
	e := testkit.New(t)
	ben := e.User(t, "ben")
	bot := e.Connection(t, "discord")
	require.Equal(t, 404, bot.Do(t, "GET", "/connection/users/lookup?external_id=1234", nil).Status)
	require.Equal(t, 404, bot.Do(t, "GET", "/users/"+ben.UserID+"/items", nil).Status)
	require.Equal(t, 404, bot.Do(t, "POST", "/users/"+ben.UserID+"/items", J{"title": "x"}).Status)
	require.Equal(t, 422, bot.Do(t, "GET", "/connection/users/lookup", nil, testkit.NoContract()).Status)
}

func TestUserCanRevokeConnection(t *testing.T) {
	e := testkit.New(t)
	mia := e.User(t, "mia")
	bot := e.Connection(t, "discord")
	e.GiveIdentity(t, mia.UserID, "discord", "1234")

	list := mia.Do(t, "GET", "/users/@me/connections", nil)
	require.Equal(t, 200, list.Status)
	require.Equal(t, bot.ID, list.Get("0.id").String())
	require.True(t, list.Get("0.linked").Bool())
	require.True(t, list.Get("0.enabled").Bool())

	off := mia.Do(t, "PUT", "/users/@me/connections/"+bot.ID, J{"enabled": false})
	require.Equal(t, 200, off.Status)
	require.True(t, off.Get("linked").Bool())
	require.False(t, off.Get("enabled").Bool())
	require.Equal(t, 404, bot.Do(t, "GET", "/connection/users/lookup?external_id=1234", nil).Status)
	require.Equal(t, 404, bot.Do(t, "GET", "/users/"+mia.UserID+"/items", nil).Status)

	mia.Do(t, "PUT", "/users/@me/connections/"+bot.ID, J{"enabled": true})
	require.Equal(t, 200, bot.Do(t, "GET", "/connection/users/lookup?external_id=1234", nil).Status)
	require.Equal(t, 404, mia.Do(t, "PUT", "/users/@me/connections/"+mia.UserID, J{"enabled": true}).Status)
}

func TestOptInWithoutIdentity(t *testing.T) {
	e := testkit.New(t)
	ben := e.User(t, "ben")
	bot := e.Connection(t, "discord")
	list := ben.Do(t, "GET", "/users/@me/connections", nil)
	require.False(t, list.Get("0.linked").Bool())
	require.False(t, list.Get("0.enabled").Bool())

	ben.Do(t, "PUT", "/users/@me/connections/"+bot.ID, J{"enabled": true})
	require.Equal(t, 200, bot.Do(t, "GET", "/users/"+ben.UserID+"/items", nil).Status)
	require.Equal(t, 404, bot.Do(t, "GET", "/connection/users/lookup?external_id=1234", nil).Status)
}

func TestConnectionOnlyLooksUpOwnProvider(t *testing.T) {
	e := testkit.New(t)
	mia := e.User(t, "mia")
	bot := e.Connection(t, "discord")
	e.GiveIdentity(t, mia.UserID, "github", "1234")
	require.Equal(t, 404, bot.Do(t, "GET", "/connection/users/lookup?external_id=1234", nil).Status)
	require.Equal(t, 404, bot.Do(t, "GET", "/users/"+mia.UserID+"/items", nil).Status)
}

func TestConnectionCannotTouchAccount(t *testing.T) {
	e := testkit.New(t)
	admin := e.Admin(t)
	mia := e.User(t, "mia")
	bot := e.Connection(t, "discord")
	e.GiveIdentity(t, mia.UserID, "discord", "1234")
	base := "/users/" + mia.UserID
	for _, tc := range []struct {
		method, path string
		body         any
	}{
		{"PATCH", base, J{"display_name": "hacked"}},
		{"DELETE", base, nil},
		{"GET", base + "/identities", nil},
		{"GET", base + "/tokens", nil},
		{"POST", base + "/tokens", J{"name": "x"}},
		{"PUT", base + "/password", J{"current_password": testkit.Password, "new_password": "something else"}},
		{"GET", base + "/connections", nil},
		{"PUT", base + "/connections/" + bot.ID, J{"enabled": true}},
		{"PUT", base + "/reminder-plans/high", J{"before": []string{"1h"}, "follow_up": "never"}},
		{"DELETE", base + "/reminder-plans/high", nil},
		{"GET", "/admin/users", nil},
		{"POST", "/admin/connections", J{"name": "x", "provider": "x"}},
	} {
		r := bot.Do(t, tc.method, tc.path, tc.body)
		require.Equal(t, 403, r.Status, tc.method+" "+tc.path)
		require.Equal(t, "forbidden", r.Get("error.code").String())
	}
	require.Equal(t, 404, bot.Do(t, "GET", "/users/@me", nil).Status)
	require.Equal(t, 404, bot.Do(t, "GET", "/users/@me/items", nil).Status)
	require.Equal(t, 404, bot.Do(t, "GET", "/users/"+admin.UserID+"/items", nil).Status)
	require.Equal(t, "mia", mia.Do(t, "GET", "/users/@me", nil).Get("display_name").String())

	require.Equal(t, 403, mia.Do(t, "GET", "/connection/users/lookup?external_id=1234", nil).Status)
	require.Equal(t, 401, e.Do(t, "GET", "/connection/users/lookup?external_id=1234", nil).Status)
}

func TestConnectionAdmin(t *testing.T) {
	e := testkit.New(t)
	admin := e.Admin(t)
	created := admin.Do(t, "POST", "/admin/connections", J{"name": "Discord Bot", "description": "Slash commands", "provider": "discord"})
	require.Equal(t, 201, created.Status)
	require.Contains(t, created.Get("token").String(), "plsr_con_")
	id := created.Get("id").String()

	list := admin.Do(t, "GET", "/admin/connections", nil)
	require.Equal(t, "Discord Bot", list.Get("0.name").String())
	require.False(t, list.Get("0.token").Exists())

	patched := admin.Do(t, "PATCH", "/admin/connections/"+id, J{"name": "Bot"})
	require.Equal(t, "Bot", patched.Get("name").String())
	require.Equal(t, "Slash commands", patched.Get("description").String())

	for _, body := range []J{{"name": "", "provider": "discord"}, {"name": "x", "provider": "Dis cord"}, {"name": "x"}} {
		require.Equal(t, 422, admin.Do(t, "POST", "/admin/connections", body).Status, body)
	}
	var stored []byte
	require.NoError(t, e.Pool.QueryRow(context.Background(), "SELECT token_hash FROM connections").Scan(&stored))
	require.NotContains(t, string(stored), created.Get("token").String())

	require.Equal(t, 204, admin.Do(t, "DELETE", "/admin/connections/"+id, nil).Status)
	require.Equal(t, 401, e.Do(t, "GET", "/connection/users/lookup?external_id=1", nil, testkit.Bearer(created.Get("token").String())).Status)
}

func TestRotatedAndDisabledConnectionTokens(t *testing.T) {
	e := testkit.New(t)
	admin := e.Admin(t)
	mia := e.User(t, "mia")
	bot := e.Connection(t, "discord")
	e.GiveIdentity(t, mia.UserID, "discord", "1234")
	lookup := "/connection/users/lookup?external_id=1234"
	require.Equal(t, 200, bot.Do(t, "GET", lookup, nil).Status)

	rotated := admin.Do(t, "POST", "/admin/connections/"+bot.ID+"/token", nil)
	require.Equal(t, 200, rotated.Status)
	fresh := testkit.Bearer(rotated.Get("token").String())
	require.Equal(t, 401, bot.Do(t, "GET", lookup, nil).Status)
	require.Equal(t, 200, e.Do(t, "GET", lookup, nil, fresh).Status)

	admin.Do(t, "PATCH", "/admin/connections/"+bot.ID, J{"enabled": false})
	require.Equal(t, 401, e.Do(t, "GET", lookup, nil, fresh).Status)
	require.Equal(t, int64(0), mia.Do(t, "GET", "/users/@me/connections", nil).Get("#").Int())
}

func TestConnectionCannotReachDisabledUser(t *testing.T) {
	e := testkit.New(t)
	admin := e.Admin(t)
	mia := e.User(t, "mia")
	bot := e.Connection(t, "discord")
	e.GiveIdentity(t, mia.UserID, "discord", "1234")
	admin.Do(t, "PATCH", "/admin/users/"+mia.UserID, J{"disabled": true})
	require.Equal(t, 404, bot.Do(t, "GET", "/connection/users/lookup?external_id=1234", nil).Status)
	require.Equal(t, 404, bot.Do(t, "GET", "/users/"+mia.UserID+"/items", nil).Status)
}

func TestConnectionAllowlistIgnoresPathCase(t *testing.T) {
	e := testkit.New(t)
	mia := e.User(t, "mia")
	bot := e.Connection(t, "discord")
	e.GiveIdentity(t, mia.UserID, "discord", "1234")
	for _, path := range []string{
		"/USERS/" + mia.UserID + "/identities",
		"/Users/" + mia.UserID + "/connections",
		"/users/" + mia.UserID + "/IDENTITIES",
		"/users/" + mia.UserID + "/Tokens",
	} {
		r := bot.Do(t, "GET", path, nil, testkit.NoContract())
		require.Contains(t, []int{403, 404}, r.Status, path)
		require.NotContains(t, string(r.Body), "external_id", path)
	}
}

func TestConnectionSeesOnlyWhatItNeedsOfTheProfile(t *testing.T) {
	e := testkit.New(t)
	mia := e.User(t, "mia")
	bot := e.Connection(t, "discord")
	e.GiveIdentity(t, mia.UserID, "discord", "1234")
	r := bot.Do(t, "GET", "/users/"+mia.UserID, nil)
	require.Equal(t, 200, r.Status)
	require.Equal(t, "mia", r.Get("display_name").String())
	require.Equal(t, "UTC", r.Get("timezone").String())
	require.True(t, r.Get("quiet_hours").Exists())
	require.False(t, r.Get("role").Exists())
	require.False(t, r.Get("created_at").Exists())
}

func TestLookupRejectsNUL(t *testing.T) {
	e := testkit.New(t)
	bot := e.Connection(t, "discord")
	r := bot.Do(t, "GET", "/connection/users/lookup?external_id=a%00b", nil, testkit.NoContract())
	require.Equal(t, 400, r.Status)
}
