package httpapi_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/rayakame/plsremind/hub/internal/testkit"
)

func TestQuietHoursAreOffByDefault(t *testing.T) {
	u := testkit.New(t).User(t, "mia")
	require.Equal(t, gjson.Null, u.Do(t, "GET", "/users/@me", nil).Get("quiet_hours").Type)

	on := u.Do(t, "PATCH", "/users/@me", J{"quiet_hours": J{"start": "23:00", "end": "07:00"}})
	require.Equal(t, 200, on.Status)
	require.Equal(t, "23:00", on.Get("quiet_hours.start").String())
	require.Equal(t, "07:00", on.Get("quiet_hours.end").String())

	other := u.Do(t, "PATCH", "/users/@me", J{"display_name": "Mia"})
	require.Equal(t, "23:00", other.Get("quiet_hours.start").String(), "unrelated changes keep them")

	off := u.Do(t, "PATCH", "/users/@me", J{"quiet_hours": nil})
	require.Equal(t, 200, off.Status)
	require.Equal(t, gjson.Null, off.Get("quiet_hours").Type)

	require.Equal(t, 422, u.Do(t, "PATCH", "/users/@me", J{"quiet_hours": J{"start": "23:00"}}).Status)
	require.Equal(t, 422, u.Do(t, "PATCH", "/users/@me", J{"quiet_hours": J{"start": "23:00", "end": "23:00"}}).Status)
}

func TestConnectionRemindFlag(t *testing.T) {
	e := testkit.New(t)
	mia := e.User(t, "mia")
	bot := e.Connection(t, "discord")
	e.GiveIdentity(t, mia.UserID, "discord", "1234")

	list := mia.Do(t, "GET", "/users/@me/connections", nil)
	require.True(t, list.Get("0.remind").Bool())
	require.True(t, list.Get("0.enabled").Bool())

	off := mia.Do(t, "PUT", "/users/@me/connections/"+bot.ID, J{"remind": false})
	require.Equal(t, 200, off.Status)
	require.False(t, off.Get("remind").Bool())
	require.True(t, off.Get("enabled").Bool(), "the bot keeps its access")
	require.Equal(t, 200, bot.Do(t, "GET", "/users/"+mia.UserID+"/items", nil).Status)

	require.Equal(t, 422, mia.Do(t, "PUT", "/users/@me/connections/"+bot.ID, J{}).Status)
}

func TestRemindChoiceSurvivesGrantToggle(t *testing.T) {
	e := testkit.New(t)
	mia := e.User(t, "mia")
	bot := e.Connection(t, "discord")
	e.GiveIdentity(t, mia.UserID, "discord", "1234")
	path := "/users/@me/connections/" + bot.ID
	mia.Do(t, "PUT", path, J{"remind": false})
	mia.Do(t, "PUT", path, J{"enabled": false})
	back := mia.Do(t, "PUT", path, J{"enabled": true})
	require.True(t, back.Get("enabled").Bool())
	require.False(t, back.Get("remind").Bool())
}

func TestTokenRemindFlag(t *testing.T) {
	e := testkit.New(t)
	mia := e.User(t, "mia")
	plain := mia.Do(t, "POST", "/users/@me/tokens", J{"name": "script"})
	require.False(t, plain.Get("remind").Bool())
	channel := mia.Do(t, "POST", "/users/@me/tokens", J{"name": "homelab", "remind": true})
	require.Equal(t, 201, channel.Status)
	require.True(t, channel.Get("remind").Bool())
	id := channel.Get("id").String()

	patched := mia.Do(t, "PATCH", "/users/@me/tokens/"+id, J{"remind": false, "name": "homelab pi"})
	require.Equal(t, 200, patched.Status)
	require.False(t, patched.Get("remind").Bool())
	require.Equal(t, "homelab pi", patched.Get("name").String())
	require.False(t, patched.Get("token").Exists())

	require.Equal(t, 422, mia.Do(t, "PATCH", "/users/@me/tokens/"+id, J{}).Status)
	bearer := testkit.Bearer(channel.Get("token").String())
	require.Equal(t, 403, e.Do(t, "PATCH", "/users/@me/tokens/"+id, J{"remind": true}, bearer).Status)
	ben := e.User(t, "ben")
	require.Equal(t, 404, ben.Do(t, "PATCH", "/users/@me/tokens/"+id, J{"remind": true}).Status)
}

func TestAdminLimitsConnectionIntents(t *testing.T) {
	e := testkit.New(t)
	admin := e.Admin(t)
	mia := e.User(t, "mia")
	bot := e.Connection(t, "discord")
	e.GiveIdentity(t, mia.UserID, "discord", "1234")
	require.JSONEq(t, `["users","lists","items","plans","deliveries"]`, admin.Do(t, "GET", "/admin/connections", nil).Get("0.allowed_intents").Raw)

	r := admin.Do(t, "PATCH", "/admin/connections/"+bot.ID, J{"allowed_intents": []string{"deliveries", "users"}})
	require.Equal(t, 200, r.Status)
	require.JSONEq(t, `["users","deliveries"]`, r.Get("allowed_intents").Raw)

	base := "/users/" + mia.UserID
	require.Equal(t, 200, bot.Do(t, "GET", base, nil).Status)
	require.Equal(t, 200, bot.Do(t, "GET", "/connection/users/lookup?external_id=1234", nil).Status)
	for _, path := range []string{base + "/items", base + "/lists", base + "/reminder-plans"} {
		require.Equal(t, 403, bot.Do(t, "GET", path, nil).Status, path)
	}
	require.Equal(t, 403, bot.Do(t, "POST", base+"/items", J{"title": "x"}).Status)
	require.JSONEq(t, `["users","deliveries"]`, mia.Do(t, "GET", "/users/@me/connections", nil).Get("0.intents").Raw)

	admin.Do(t, "PATCH", "/admin/connections/"+bot.ID, J{"allowed_intents": []string{"items"}})
	require.Equal(t, 403, bot.Do(t, "GET", base, nil).Status)
	require.Equal(t, 200, bot.Do(t, "GET", base+"/items", nil).Status)
}

func TestAllowedIntentsValidation(t *testing.T) {
	e := testkit.New(t)
	admin := e.Admin(t)
	for _, bad := range [][]string{{}, {"everything"}} {
		r := admin.Do(t, "POST", "/admin/connections", J{"name": "x", "provider": "discord", "allowed_intents": bad})
		require.Equal(t, 422, r.Status, bad)
	}
	r := admin.Do(t, "POST", "/admin/connections", J{"name": "x", "provider": "discord", "allowed_intents": []string{"items", "items", "lists"}})
	require.Equal(t, 201, r.Status)
	require.JSONEq(t, `["lists","items"]`, r.Get("allowed_intents").Raw)
}
