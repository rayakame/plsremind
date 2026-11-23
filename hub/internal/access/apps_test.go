package access_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/rayakame/plsremind/hub/internal/testkit"
)

func appOf(t *testing.T, u *testkit.Client, id string) gjson.Result {
	t.Helper()
	all := u.Do(t, "GET", "/users/@me/connections", nil)
	require.Equal(t, 200, all.Status, string(all.Body))
	app := all.Get(`#(id=="` + id + `")`)
	require.True(t, app.Exists(), string(all.Body))
	return app
}

func adminApp(t *testing.T, e *testkit.Env, id string) gjson.Result {
	t.Helper()
	all := e.Admin(t).Do(t, "GET", "/admin/connections", nil)
	require.Equal(t, 200, all.Status, string(all.Body))
	return all.Get(`#(id=="` + id + `")`)
}

const discordInstall = "https://discord.com/oauth2/authorize?client_id=123&integration_type=1&scope=applications.commands"

func TestAnAppKeepsItsInstallLink(t *testing.T) {
	e := testkit.New(t)
	admin := e.Admin(t)
	created := admin.Do(t, "POST", "/admin/connections", J{"name": "Bot", "provider": "discord", "install_url": discordInstall})
	require.Equal(t, 201, created.Status, string(created.Body))
	require.Equal(t, discordInstall, created.Get("install_url").String())
	id := created.Get("id").String()

	mia := testkit.Linked(t, e, "mia", "111")
	require.Equal(t, discordInstall, appOf(t, mia, id).Get("install_url").String())

	cleared := admin.Do(t, "PATCH", "/admin/connections/"+id, J{"install_url": ""})
	require.Equal(t, 200, cleared.Status, string(cleared.Body))
	require.Equal(t, "", cleared.Get("install_url").String())
	require.Equal(t, "", appOf(t, mia, id).Get("install_url").String())
}

func TestAnInstallLinkMustBeHttps(t *testing.T) {
	e := testkit.New(t)
	admin := e.Admin(t)
	for _, bad := range []string{"http://discord.com/x", "javascript:alert(1)", "https://", "discord.com/x", "https://x.example/" + strings.Repeat("a", 500)} {
		r := admin.Do(t, "POST", "/admin/connections", J{"name": "Bot", "provider": "discord", "install_url": bad})
		require.Equal(t, 422, r.Status, bad)
		require.True(t, r.Get("error.fields.install_url").Exists(), string(r.Body))
	}
	id := admin.Do(t, "POST", "/admin/connections", J{"name": "Bot", "provider": "discord"}).Get("id").String()
	r := admin.Do(t, "PATCH", "/admin/connections/"+id, J{"install_url": "ftp://x"})
	require.Equal(t, 422, r.Status)
}

func TestReachCountsWhoAnAppWouldRemind(t *testing.T) {
	e := testkit.New(t)
	bot := e.Connection(t, "discord")
	quiet := e.Admin(t).Do(t, "POST", "/admin/connections", J{"name": "Quiet", "provider": "discord", "allowed_intents": []string{"users", "items"}})
	require.Equal(t, 201, quiet.Status)

	testkit.Linked(t, e, "mia", "1")
	off := testkit.Linked(t, e, "off", "2")
	require.Equal(t, 200, off.Do(t, "PUT", "/users/@me/connections/"+bot.ID, J{"enabled": false}).Status)
	silent := testkit.Linked(t, e, "silent", "3")
	require.Equal(t, 200, silent.Do(t, "PUT", "/users/@me/connections/"+bot.ID, J{"remind": false}).Status)
	gone := testkit.Linked(t, e, "gone", "4")
	require.Equal(t, 200, e.Admin(t).Do(t, "PATCH", "/admin/users/"+gone.UserID, J{"disabled": true}).Status)
	e.User(t, "unlinked")
	optedIn := e.User(t, "opted")
	require.Equal(t, 200, optedIn.Do(t, "PUT", "/users/@me/connections/"+bot.ID, J{"enabled": true}).Status)

	require.Equal(t, int64(1), adminApp(t, e, bot.ID).Get("reach").Int(), "only users with an account there count")
	require.Equal(t, int64(0), adminApp(t, e, quiet.Get("id").String()).Get("reach").Int())
	require.True(t, quiet.Get("reach").Exists())
}

func TestLastDeliveryIsTheNewestThatCounts(t *testing.T) {
	e := testkit.New(t)
	bot := e.Connection(t, "discord")
	mia := testkit.Linked(t, e, "mia", "111")
	tom := testkit.Linked(t, e, "tom", "222")
	require.Equal(t, "null", appOf(t, mia, bot.ID).Get("last_delivery").Raw)

	testkit.DueSoon(t, e, mia)
	first := e.Deliveries(t, mia.UserID)[0]
	require.Equal(t, "pending", appOf(t, mia, bot.ID).Get("last_delivery.status").String())
	require.Equal(t, 200, bot.Do(t, "PUT", "/deliveries/"+first.ID, J{"status": "delivered"}).Status)
	last := appOf(t, mia, bot.ID).Get("last_delivery")
	require.Equal(t, "delivered", last.Get("status").String())
	require.False(t, last.Get("test").Bool())
	require.Equal(t, e.Clock.Now().UTC().Format(time.RFC3339), last.Get("at").Time().UTC().Format(time.RFC3339))

	testkit.DueSoon(t, e, mia)
	second := e.Deliveries(t, mia.UserID)[1]
	require.Equal(t, 200, bot.Do(t, "PUT", "/deliveries/"+second.ID, J{"status": "failed", "reason": "DMs are closed"}).Status)
	last = appOf(t, mia, bot.ID).Get("last_delivery")
	require.Equal(t, "failed", last.Get("status").String())
	require.Equal(t, "DMs are closed", last.Get("reason").String())

	item := testkit.DueSoon(t, e, mia)
	require.Equal(t, 200, mia.Do(t, "PATCH", "/users/@me/items/"+item, J{"status": "done"}).Status)
	require.Equal(t, "failed", appOf(t, mia, bot.ID).Get("last_delivery.status").String(), "a cancelled delivery does not count")

	testkit.DueSoon(t, e, mia)
	e.Clock.Advance(25 * time.Hour)
	e.Fire(t)
	require.Equal(t, "expired", appOf(t, mia, bot.ID).Get("last_delivery.status").String())
	require.Equal(t, "null", appOf(t, tom, bot.ID).Get("last_delivery").Raw, "another user's deliveries do not show")
}
