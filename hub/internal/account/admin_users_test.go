package account_test

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/rayakame/plsremind/hub/internal/testkit"
)

func TestUserCannotReachAdmin(t *testing.T) {
	e := testkit.New(t)
	mia := e.User(t, "mia")
	for _, path := range []string{"/admin/users", "/admin/settings", "/admin/invites"} {
		r := mia.Do(t, "GET", path, nil)
		require.Equal(t, 403, r.Status, path)
		require.Equal(t, "forbidden", r.Get("error.code").String())
		require.Equal(t, 401, e.Do(t, "GET", path, nil).Status, path)
	}
	require.Equal(t, 403, mia.Do(t, "PATCH", "/admin/users/"+mia.UserID, J{"role": "admin"}).Status)
}

func TestAdminListsUsers(t *testing.T) {
	e := testkit.New(t)
	admin := e.Admin(t)
	e.User(t, "mia")
	e.User(t, "ben")
	first := admin.Do(t, "GET", "/admin/users?limit=2", nil)
	require.Equal(t, 200, first.Status)
	require.Equal(t, int64(2), first.Get("data.#").Int())
	require.Equal(t, "root", first.Get("data.0.display_name").String())
	require.False(t, first.Get("data.0.disabled").Bool())
	second := admin.Do(t, "GET", "/admin/users?limit=2&cursor="+first.Get("next_cursor").String(), nil)
	require.Equal(t, int64(1), second.Get("data.#").Int())
	require.Equal(t, "ben", second.Get("data.0.display_name").String())
	require.False(t, second.Get("next_cursor").Exists() && second.Get("next_cursor").String() != "")
	require.Equal(t, 400, admin.Do(t, "GET", "/admin/users?cursor=%%%", nil, testkit.NoContract()).Status)
	require.Equal(t, 422, admin.Do(t, "GET", "/admin/users?limit=0", nil, testkit.NoContract()).Status)
}

func TestLastAdminIsProtected(t *testing.T) {
	e := testkit.New(t)
	admin := e.Admin(t)
	for _, body := range []J{{"role": "user"}, {"disabled": true}} {
		r := admin.Do(t, "PATCH", "/admin/users/"+admin.UserID, body)
		require.Equal(t, 409, r.Status)
		require.Equal(t, "last_admin", r.Get("error.code").String())
	}
	require.Equal(t, 409, admin.Do(t, "DELETE", "/admin/users/"+admin.UserID, nil).Status)
	require.Equal(t, 409, admin.Do(t, "DELETE", "/users/@me", nil).Status)

	mia := e.User(t, "mia")
	promoted := admin.Do(t, "PATCH", "/admin/users/"+mia.UserID, J{"role": "admin"})
	require.Equal(t, 200, promoted.Status)
	require.Equal(t, "admin", promoted.Get("role").String())
	require.Equal(t, 200, admin.Do(t, "PATCH", "/admin/users/"+admin.UserID, J{"role": "user"}).Status)
}

func TestDisabledUserIsLockedOut(t *testing.T) {
	e := testkit.New(t)
	admin := e.Admin(t)
	mia := e.User(t, "mia")
	r := admin.Do(t, "PATCH", "/admin/users/"+mia.UserID, J{"disabled": true})
	require.Equal(t, 200, r.Status)
	require.True(t, r.Get("disabled").Bool())
	require.Equal(t, 401, mia.Do(t, "GET", "/users/@me", nil).Status)
	login := e.Do(t, "POST", "/auth/login", J{"username": "mia", "password": testkit.Password})
	require.Equal(t, 403, login.Status)
	require.Equal(t, "account_disabled", login.Get("error.code").String())

	admin.Do(t, "PATCH", "/admin/users/"+mia.UserID, J{"disabled": false})
	require.Equal(t, 200, e.Do(t, "POST", "/auth/login", J{"username": "mia", "password": testkit.Password}).Status)
}

func TestAdminResetsPassword(t *testing.T) {
	e := testkit.New(t)
	admin := e.Admin(t)
	mia := e.User(t, "mia")
	r := admin.Do(t, "PUT", "/admin/users/"+mia.UserID+"/password", J{"new_password": "reset by the admin"})
	require.Equal(t, 204, r.Status)
	require.Equal(t, 200, e.Do(t, "POST", "/auth/login", J{"username": "mia", "password": "reset by the admin"}).Status)
	require.Equal(t, 401, e.Do(t, "POST", "/auth/login", J{"username": "mia", "password": testkit.Password}).Status)
	require.Equal(t, 401, mia.Do(t, "GET", "/users/@me", nil).Status, "a reset ends existing sessions")
}

func TestAdminDeletesUser(t *testing.T) {
	e := testkit.New(t)
	admin := e.Admin(t)
	mia := e.User(t, "mia")
	require.Equal(t, 204, admin.Do(t, "DELETE", "/admin/users/"+mia.UserID, nil).Status)
	require.Equal(t, 404, admin.Do(t, "DELETE", "/admin/users/"+mia.UserID, nil).Status)
	require.Equal(t, 404, admin.Do(t, "PATCH", "/admin/users/not-a-uuid", J{"role": "admin"}).Status)
}

func TestLastAdminSurvivesConcurrentChanges(t *testing.T) {
	e := testkit.New(t)
	root := e.Admin(t)
	mia, ben := e.User(t, "mia"), e.User(t, "ben")
	ctx := context.Background()
	for round := 0; round < 40; round++ {
		_, err := e.Pool.Exec(ctx, "UPDATE users SET role = CASE WHEN id = $1 THEN 'user' ELSE 'admin' END, disabled = false", ben.UserID)
		require.NoError(t, err)
		var wg sync.WaitGroup
		for _, change := range []struct {
			user string
			body J
		}{
			{ben.UserID, J{"role": "admin"}},
			{ben.UserID, J{"role": "user"}},
			{root.UserID, J{"role": "user"}},
			{mia.UserID, J{"role": "user"}},
			{ben.UserID, J{"disabled": true}},
		} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				root.Do(t, "PATCH", "/admin/users/"+change.user, change.body, testkit.NoContract())
			}()
		}
		wg.Wait()
		var admins int
		require.NoError(t, e.Pool.QueryRow(ctx, "SELECT count(*) FROM users WHERE role = 'admin' AND NOT disabled").Scan(&admins))
		require.GreaterOrEqual(t, admins, 1, "round %d left the instance without an admin", round)
	}
}
