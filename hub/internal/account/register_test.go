package account_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/rayakame/plsremind/hub/internal/testkit"
)

func TestRegisterNeedsInviteByDefault(t *testing.T) {
	e := testkit.New(t)
	admin := e.Admin(t)
	r := e.Do(t, "POST", "/auth/register", J{"username": "mia", "password": testkit.Password})
	require.Equal(t, 403, r.Status)
	require.Equal(t, "registration_closed", r.Get("error.code").String())

	invite := admin.Do(t, "POST", "/admin/invites", J{})
	require.Equal(t, 201, invite.Status)
	code := invite.Get("code").String()
	require.True(t, strings.HasPrefix(code, "plsr_inv_"))

	ok := e.Do(t, "POST", "/auth/register", J{"username": "mia", "password": testkit.Password, "invite_code": code})
	require.Equal(t, 201, ok.Status)
	require.Equal(t, "user", ok.Get("role").String())
	require.NotEmpty(t, ok.Cookie("plsr_session"))

	reuse := e.Do(t, "POST", "/auth/register", J{"username": "ben", "password": testkit.Password, "invite_code": code})
	require.Equal(t, 403, reuse.Status)
	require.Equal(t, "invalid_invite", reuse.Get("error.code").String())

	list := admin.Do(t, "GET", "/admin/invites", nil)
	require.Equal(t, ok.Get("id").String(), list.Get("0.used_by").String())
	require.False(t, list.Get("0.code").Exists())
}

func TestOpenRegistration(t *testing.T) {
	e := testkit.New(t)
	admin := e.Admin(t)
	s := admin.Do(t, "PATCH", "/admin/settings", J{"registration": "open", "name": "Home"})
	require.Equal(t, 200, s.Status)
	require.Equal(t, "Home", e.Do(t, "GET", "/info", nil).Get("name").String())
	r := e.Do(t, "POST", "/auth/register", J{"username": "mia", "password": testkit.Password})
	require.Equal(t, 201, r.Status)
	require.Equal(t, 422, admin.Do(t, "PATCH", "/admin/settings", J{"registration": "maybe"}).Status)
}

func TestExpiredInviteRejected(t *testing.T) {
	e := testkit.New(t)
	admin := e.Admin(t)
	code := admin.Do(t, "POST", "/admin/invites", J{"expires_in": "1h"}).Get("code").String()
	e.Clock.Advance(2 * time.Hour)
	r := e.Do(t, "POST", "/auth/register", J{"username": "mia", "password": testkit.Password, "invite_code": code})
	require.Equal(t, 403, r.Status)
	require.Equal(t, "invalid_invite", r.Get("error.code").String())
	require.Equal(t, 422, admin.Do(t, "POST", "/admin/invites", J{"expires_in": "31d"}).Status)
}

func TestRevokedInviteRejected(t *testing.T) {
	e := testkit.New(t)
	admin := e.Admin(t)
	invite := admin.Do(t, "POST", "/admin/invites", J{})
	require.Equal(t, 204, admin.Do(t, "DELETE", "/admin/invites/"+invite.Get("id").String(), nil).Status)
	r := e.Do(t, "POST", "/auth/register", J{"username": "mia", "password": testkit.Password, "invite_code": invite.Get("code").String()})
	require.Equal(t, "invalid_invite", r.Get("error.code").String())
}

func TestDuplicateUsernameIs409(t *testing.T) {
	e := testkit.New(t)
	e.User(t, "mia")
	admin := e.Admin(t)
	code := admin.Do(t, "POST", "/admin/invites", J{}).Get("code").String()
	r := e.Do(t, "POST", "/auth/register", J{"username": "MIA", "password": testkit.Password, "invite_code": code})
	require.Equal(t, 409, r.Status)
	require.Equal(t, "username_taken", r.Get("error.code").String())

	retry := e.Do(t, "POST", "/auth/register", J{"username": "mia2", "password": testkit.Password, "invite_code": code})
	require.Equal(t, 201, retry.Status, "a failed registration must not use up the invite")
}

func TestListOwnIdentities(t *testing.T) {
	e := testkit.New(t)
	mia := e.User(t, "mia")
	r := mia.Do(t, "GET", "/users/@me/identities", nil)
	require.Equal(t, 200, r.Status)
	require.Equal(t, int64(1), r.Get("#").Int())
	require.Equal(t, "password", r.Get("0.provider").String())
	require.Equal(t, "mia", r.Get("0.external_id").String())
	require.False(t, r.Get("0.password_hash").Exists())
}

func TestChangeOwnPassword(t *testing.T) {
	e := testkit.New(t)
	mia := e.User(t, "mia")
	wrong := mia.Do(t, "PUT", "/users/@me/password", J{"current_password": "nope nope nope", "new_password": "a brand new password"})
	require.Equal(t, 403, wrong.Status)
	require.Equal(t, "invalid_credentials", wrong.Get("error.code").String())
	short := mia.Do(t, "PUT", "/users/@me/password", J{"current_password": testkit.Password, "new_password": "short"})
	require.Equal(t, 422, short.Status)

	ok := mia.Do(t, "PUT", "/users/@me/password", J{"current_password": testkit.Password, "new_password": "a brand new password"})
	require.Equal(t, 204, ok.Status)
	require.Equal(t, 401, e.Do(t, "POST", "/auth/login", J{"username": "mia", "password": testkit.Password}).Status)
	require.Equal(t, 200, e.Do(t, "POST", "/auth/login", J{"username": "mia", "password": "a brand new password"}).Status)
}

func TestDeleteOwnAccount(t *testing.T) {
	e := testkit.New(t)
	mia := e.User(t, "mia")
	require.Equal(t, 204, mia.Do(t, "DELETE", "/users/@me", nil).Status)
	require.Equal(t, 401, e.Do(t, "POST", "/auth/login", J{"username": "mia", "password": testkit.Password}).Status)
	require.Equal(t, 401, mia.Do(t, "GET", "/users/@me", nil).Status)

	var users, identities, lists int
	ctx := context.Background()
	require.NoError(t, e.Pool.QueryRow(ctx, "SELECT count(*) FROM users WHERE id = $1", mia.UserID).Scan(&users))
	require.NoError(t, e.Pool.QueryRow(ctx, "SELECT count(*) FROM identities WHERE user_id = $1", mia.UserID).Scan(&identities))
	require.NoError(t, e.Pool.QueryRow(ctx, "SELECT count(*) FROM lists WHERE user_id = $1", mia.UserID).Scan(&lists))
	require.Equal(t, []int{0, 0, 0}, []int{users, identities, lists})
}
