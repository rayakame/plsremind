package httpapi_test

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/rayakame/plsremind/hub/internal/gateway"
	"github.com/rayakame/plsremind/hub/internal/ratelimit"
	"github.com/rayakame/plsremind/hub/internal/testkit"
)

// linked returns a user with an account at the bot's provider.
func linked(t *testing.T, e *testkit.Env, name, externalID string) *testkit.Client {
	t.Helper()
	u := e.User(t, name)
	e.GiveIdentity(t, u.UserID, "discord", externalID)
	return u
}

func TestConnectionSyncsAllGrantedUsers(t *testing.T) {
	e := testkit.New(t)
	bot := e.Connection(t, "discord")
	mia, ben := linked(t, e, "mia", "111"), linked(t, e, "ben", "222")
	cara := e.User(t, "cara")
	mia.Do(t, "POST", "/users/@me/items", J{"title": "mias"})

	gw := e.Gateway(t, bot.Token, "users", "lists", "items", "plans")
	require.ElementsMatch(t, []string{mia.UserID, ben.UserID}, ids(&testkit.Resp{Body: []byte(gw.Ready.D.Raw)}, "users"))
	seen := map[string]testkit.Frame{}
	for i := 0; i < 2; i++ {
		sync := gw.Expect(t, "user.sync")
		seen[sync.Get("user_id").String()] = sync
	}
	require.NotContains(t, seen, cara.UserID)
	miaSync := seen[mia.UserID]
	require.JSONEq(t, `["111"]`, miaSync.Get("user.external_ids").Raw)
	require.True(t, miaSync.Get("user.remind").Bool())
	require.Equal(t, "mia", miaSync.Get("user.display_name").String())
	require.False(t, miaSync.Get("user.role").Exists(), "a connection sees the reduced profile")
	require.Equal(t, "mias", miaSync.Get("items.0.title").String())
	require.JSONEq(t, `["222"]`, seen[ben.UserID].Get("user.external_ids").Raw)
	gw.ExpectQuiet(t)
}

func TestConnectionSeesChangesOfItsUsers(t *testing.T) {
	e := testkit.New(t)
	bot := e.Connection(t, "discord")
	mia := linked(t, e, "mia", "111")
	cara := e.User(t, "cara")
	gw := e.Gateway(t, bot.Token, "items")
	gw.Expect(t, "user.sync")

	mia.Do(t, "POST", "/users/@me/items", J{"title": "neu"})
	created := gw.Expect(t, "item.created")
	require.Equal(t, mia.UserID, created.Get("user_id").String())
	require.Equal(t, "neu", created.Get("data.title").String())

	cara.Do(t, "POST", "/users/@me/items", J{"title": "geheim"})
	gw.ExpectQuiet(t)

	own := bot.Do(t, "POST", "/users/"+mia.UserID+"/items", J{"title": "vom Bot"})
	require.Equal(t, 201, own.Status)
	echo := gw.Expect(t, "item.created")
	require.Equal(t, "connection", echo.Get("actor.kind").String())
	require.Equal(t, bot.ID, echo.Get("actor.id").String(), "a client can tell its own changes apart")
}

func TestGrantAndRevokeWhileConnected(t *testing.T) {
	e := testkit.New(t)
	bot := e.Connection(t, "discord")
	mia := linked(t, e, "mia", "111")
	cara := e.User(t, "cara")
	gw := e.Gateway(t, bot.Token, "users", "items")
	gw.Expect(t, "user.sync")

	cara.Do(t, "PUT", "/users/@me/connections/"+bot.ID, J{"enabled": true})
	joined := gw.Expect(t, "user.sync")
	require.Equal(t, cara.UserID, joined.Get("user_id").String())

	mia.Do(t, "PUT", "/users/@me/connections/"+bot.ID, J{"enabled": false})
	gone := gw.Expect(t, "user.gone")
	require.Equal(t, mia.UserID, gone.Get("user_id").String())
	mia.Do(t, "POST", "/users/@me/items", J{"title": "nicht mehr für den Bot"})
	gw.ExpectQuiet(t)

	mia.Do(t, "PUT", "/users/@me/connections/"+bot.ID, J{"enabled": true})
	back := gw.Expect(t, "user.sync")
	require.Equal(t, mia.UserID, back.Get("user_id").String())
	require.Equal(t, "nicht mehr für den Bot", back.Get("items.0.title").String())
	gw.Cache(t).RequireMatchesREST(t, bot, "/users/"+mia.UserID, mia.UserID)
}

func TestDisabledUserGoes(t *testing.T) {
	e := testkit.New(t)
	admin := e.Admin(t)
	bot := e.Connection(t, "discord")
	ben := linked(t, e, "ben", "222")
	own := e.Gateway(t, personalToken(t, ben), "items")
	own.Expect(t, "user.sync")
	gw := e.Gateway(t, bot.Token, "items")
	gw.Expect(t, "user.sync")

	admin.Do(t, "PATCH", "/admin/users/"+ben.UserID, J{"disabled": true})
	require.Equal(t, ben.UserID, gw.Expect(t, "user.gone").Get("user_id").String())
	own.ExpectClose(t, gateway.CloseAuthFailed)

	admin.Do(t, "PATCH", "/admin/users/"+ben.UserID, J{"disabled": false})
	require.Equal(t, ben.UserID, gw.Expect(t, "user.sync").Get("user_id").String())
	admin.Do(t, "DELETE", "/admin/users/"+ben.UserID, nil)
	require.Equal(t, ben.UserID, gw.Expect(t, "user.gone").Get("user_id").String())
}

func TestConnectionNeverSeesAccountData(t *testing.T) {
	e := testkit.New(t)
	bot := e.Connection(t, "discord")
	other := e.Connection(t, "discord")
	mia := linked(t, e, "mia", "111")
	gw := e.Gateway(t, bot.Token, "users", "lists", "items", "plans")
	gw.Expect(t, "user.sync")

	secret := mia.Do(t, "POST", "/users/@me/tokens", J{"name": "sehr geheim"}).Get("token").String()
	mia.Do(t, "PUT", "/users/@me/connections/"+other.ID, J{"remind": false})
	mia.Do(t, "PATCH", "/users/@me", J{"display_name": "Mia"})
	gw.Drain(t)

	for _, f := range gw.Seen {
		require.NotEqual(t, "connection.updated", f.T)
		require.NotContains(t, f.D.Raw, secret)
		require.NotContains(t, f.D.Raw, "sehr geheim")
		require.NotContains(t, f.D.Raw, `"role"`)
		require.NotContains(t, f.D.Raw, other.ID)
	}
	last := gw.Seen[len(gw.Seen)-1]
	require.Equal(t, "user.updated", last.T)
	require.Equal(t, "Mia", last.Get("data.display_name").String())
	require.JSONEq(t, `["111"]`, last.Get("data.external_ids").Raw)
}

func TestRemindToggleUpdatesUser(t *testing.T) {
	e := testkit.New(t)
	bot := e.Connection(t, "discord")
	mia := linked(t, e, "mia", "111")
	gw := e.Gateway(t, bot.Token, "users")
	gw.Expect(t, "user.sync")
	mia.Do(t, "PUT", "/users/@me/connections/"+bot.ID, J{"remind": false})
	updated := gw.Expect(t, "user.updated")
	require.False(t, updated.Get("data.remind").Bool())
	require.Equal(t, mia.UserID, updated.Get("user_id").String())
}

func TestConnectionCacheMatchesREST(t *testing.T) {
	e := testkit.New(t)
	e.Deps.RateAuth = ratelimit.New(100000, time.Minute, e.Clock.Now)
	bot := e.Connection(t, "discord")
	users := []*testkit.Client{linked(t, e, "mia", "111"), linked(t, e, "ben", "222")}

	var wg sync.WaitGroup
	for n, u := range users {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 12; i++ {
				nc := testkit.NoContract()
				list := u.Do(t, "POST", "/users/@me/lists", J{"name": fmt.Sprintf("l%d", i)}, nc).Get("id").String()
				id := u.Do(t, "POST", "/users/@me/items", J{"title": fmt.Sprintf("u%d i%d", n, i), "list_id": list, "subitems": []string{"a"}}, nc).Get("id").String()
				u.Do(t, "PATCH", "/users/@me/items/"+id, J{"due_date": "2026-12-01"}, nc)
				u.Do(t, "PATCH", "/users/@me", J{"display_name": fmt.Sprintf("user %d.%d", n, i)}, nc)
				if i%3 == 0 {
					u.Do(t, "PATCH", "/users/@me/items/"+id, J{"status": "done"}, nc)
				}
				if i%4 == 0 {
					u.Do(t, "DELETE", "/users/@me/lists/"+list, nil, nc)
				}
			}
		}()
	}
	gw := e.Gateway(t, bot.Token, "users", "lists", "items", "plans")
	wg.Wait()
	cache := gw.Cache(t)
	for _, u := range users {
		cache.RequireMatchesREST(t, bot, "/users/"+u.UserID, u.UserID)
	}
	for _, f := range gw.Seen {
		require.False(t, strings.Contains(f.D.Raw, `"role"`), f.T)
	}
}
