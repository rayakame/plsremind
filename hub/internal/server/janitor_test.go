package server_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/rayakame/plsremind/hub/internal/server"
	"github.com/rayakame/plsremind/hub/internal/testkit"
)

func TestSweepRemovesExpiredRows(t *testing.T) {
	e := testkit.New(t)
	testkit.EnableProvider(t, e, "discord")
	admin := e.Admin(t)
	ctx := context.Background()
	count := func(table string) int {
		var n int
		require.NoError(t, e.Pool.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&n))
		return n
	}

	used := admin.Do(t, "POST", "/admin/invites", J{}).Get("code").String()
	e.Do(t, "POST", "/auth/register", J{"username": "mia", "password": testkit.Password, "invite_code": used})
	admin.Do(t, "POST", "/admin/invites", J{"expires_in": "1d"})
	e.OAuthStart(t, "discord", "intent=login")
	require.Equal(t, []int{2, 2, 1}, []int{count("sessions"), count("invites"), count("oauth_states")})

	require.NoError(t, server.Sweep(ctx, e.Pool, e.Clock.Now()))
	require.Equal(t, []int{2, 2, 1}, []int{count("sessions"), count("invites"), count("oauth_states")}, "nothing has expired yet")

	e.Clock.Advance(31 * 24 * time.Hour)
	fresh := admin.Do(t, "POST", "/admin/invites", J{})
	require.Equal(t, 401, fresh.Status, "the admin session has expired too")
	e.Do(t, "POST", "/auth/login", J{"username": "root", "password": testkit.Password})

	require.NoError(t, server.Sweep(ctx, e.Pool, e.Clock.Now()))
	require.Equal(t, 1, count("sessions"), "only the fresh login is left")
	require.Equal(t, 0, count("oauth_states"))
	require.Equal(t, 1, count("invites"), "used invites are kept as a record")
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
