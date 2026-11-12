package httpapi_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/rayakame/plsremind/hub/internal/gateway"
	"github.com/rayakame/plsremind/hub/internal/ratelimit"
	"github.com/rayakame/plsremind/hub/internal/testkit"
)

func TestResumeReplaysMissedEvents(t *testing.T) {
	e := testkit.New(t)
	mia := e.User(t, "mia")
	token := personalToken(t, mia)
	gw := e.Gateway(t, token, "items")
	gw.Expect(t, "user.sync")
	mia.Do(t, "POST", "/users/@me/items", J{"title": "seen"})
	gw.Expect(t, "item.created")
	gw.Close()

	for i := 0; i < 5; i++ {
		mia.Do(t, "POST", "/users/@me/items", J{"title": fmt.Sprintf("missed %d", i)})
	}
	back := e.ResumeGateway(t, token, gw.SessionID, gw.LastSeq)
	for i := 0; i < 5; i++ {
		ev := back.Expect(t, "item.created")
		require.Equal(t, gw.LastSeq+int64(i)+1, ev.S)
		require.Equal(t, fmt.Sprintf("missed %d", i), ev.Get("data.title").String())
	}
	resumed := back.Expect(t, gateway.EventResumed)
	require.Equal(t, gw.LastSeq+6, resumed.S)

	mia.Do(t, "POST", "/users/@me/items", J{"title": "live again"})
	require.Equal(t, "live again", back.Expect(t, "item.created").Get("data.title").String())

	// Everything both connections saw adds up to the current state.
	back.Seen = append(gw.Seen, back.Seen...)
	back.Cache(t).RequireMatchesREST(t, mia, "/users/@me", mia.UserID)
}

func TestResumeWithNothingMissed(t *testing.T) {
	e := testkit.New(t)
	token := personalToken(t, e.User(t, "mia"))
	gw := e.Gateway(t, token, "items")
	gw.Expect(t, "user.sync")
	gw.Close()
	back := e.ResumeGateway(t, token, gw.SessionID, gw.LastSeq)
	back.Expect(t, gateway.EventResumed)
	back.ExpectQuiet(t)
}

func TestResumeTakesOverFromALingeringConnection(t *testing.T) {
	e := testkit.New(t)
	token := personalToken(t, e.User(t, "mia"))
	gw := e.Gateway(t, token, "items")
	gw.Expect(t, "user.sync")
	back := e.ResumeGateway(t, token, gw.SessionID, gw.LastSeq)
	back.Expect(t, gateway.EventResumed)
	gw.ExpectEnded(t)
}

func TestResumeAfterTTLIsInvalid(t *testing.T) {
	e := testkit.New(t)
	mia := e.User(t, "mia")
	token := personalToken(t, mia)
	gw := e.Gateway(t, token, "items")
	gw.Expect(t, "user.sync")
	gw.Close()
	time.Sleep(e.Deps.Gateway.SessionTTL + 300*time.Millisecond)

	back := e.ResumeGateway(t, token, gw.SessionID, gw.LastSeq)
	invalid := back.NextOp(t)
	require.Equal(t, gateway.OpInvalidSession, invalid.Op)
	require.Equal(t, "false", invalid.D.Raw)

	// The connection stays open for a fresh start.
	back.Send(t, gateway.OpIdentify, J{"token": token, "intents": []string{"items"}})
	back.Expect(t, gateway.EventReady)
	back.Expect(t, "user.sync")
}

func TestResumeAfterBufferOverflowIsInvalid(t *testing.T) {
	e := testkit.New(t)
	e.Deps.Gateway.BufferSize = 3
	mia := e.User(t, "mia")
	token := personalToken(t, mia)
	gw := e.Gateway(t, token, "items")
	gw.Expect(t, "user.sync")
	gw.Close()
	for i := 0; i < 5; i++ {
		mia.Do(t, "POST", "/users/@me/items", J{"title": "x"})
	}
	time.Sleep(100 * time.Millisecond)
	back := e.ResumeGateway(t, token, gw.SessionID, gw.LastSeq)
	require.Equal(t, gateway.OpInvalidSession, back.NextOp(t).Op)
}

func TestResumeAfterRestartIsInvalid(t *testing.T) {
	e := testkit.New(t)
	token := personalToken(t, e.User(t, "mia"))
	gw := e.Gateway(t, token, "items")
	gw.Expect(t, "user.sync")
	e.RestartGateway(t)
	require.Equal(t, gateway.OpReconnect, gw.NextOp(t).Op)

	back := e.ResumeGateway(t, token, gw.SessionID, gw.LastSeq)
	require.Equal(t, gateway.OpInvalidSession, back.NextOp(t).Op)
	back.Send(t, gateway.OpIdentify, J{"token": token, "intents": []string{"items"}})
	back.Expect(t, gateway.EventReady)
}

func TestResumeChecksOwnership(t *testing.T) {
	e := testkit.New(t)
	mia, ben := e.User(t, "mia"), e.User(t, "ben")
	token := personalToken(t, mia)
	gw := e.Gateway(t, token, "items")
	gw.Expect(t, "user.sync")
	gw.Close()

	stolen := e.ResumeGateway(t, personalToken(t, ben), gw.SessionID, gw.LastSeq)
	require.Equal(t, gateway.OpInvalidSession, stolen.NextOp(t).Op)

	bad := e.ResumeGateway(t, "plsr_pat_nope", gw.SessionID, gw.LastSeq)
	bad.ExpectClose(t, gateway.CloseAuthFailed)

	ahead := e.ResumeGateway(t, token, gw.SessionID, gw.LastSeq+10)
	ahead.ExpectClose(t, gateway.CloseInvalidSeq)

	unknown := e.ResumeGateway(t, token, "no-such-session", 1)
	require.Equal(t, gateway.OpInvalidSession, unknown.NextOp(t).Op)
}

func TestPendingDeliveriesComeAfterResume(t *testing.T) {
	e := testkit.New(t)
	bot := e.Connection(t, "discord")
	mia := linked(t, e, "mia", "111")
	gw := e.Gateway(t, bot.Token, "deliveries")
	gw.Close()
	dueSoon(t, e, mia)

	back := e.ResumeGateway(t, bot.Token, gw.SessionID, gw.LastSeq)
	back.Expect(t, gateway.EventResumed)
	d := back.Expect(t, "delivery.created")
	require.Equal(t, int64(1), d.Get("data.attempt").Int())
}

func TestRevokedCredentialsDisconnect(t *testing.T) {
	e := testkit.New(t)
	admin := e.Admin(t)
	mia := e.User(t, "mia")

	// A deleted token.
	tok := mia.Do(t, "POST", "/users/@me/tokens", J{"name": "cli"})
	byToken := e.Gateway(t, tok.Get("token").String(), "deliveries")
	mia.Do(t, "DELETE", "/users/@me/tokens/"+tok.Get("id").String(), nil)
	byToken.ExpectClose(t, gateway.CloseAuthFailed)

	// A session that logs out.
	login := e.Do(t, "POST", "/auth/login", J{"username": "mia", "password": testkit.Password})
	cookie := testkit.Cookie("plsr_session", login.Cookie("plsr_session"))
	bySession := e.GatewayRaw(t, cookie, testkit.Header("Origin", testkit.PublicURL))
	bySession.Next(t)
	bySession.Send(t, gateway.OpIdentify, J{"intents": []string{"deliveries"}})
	bySession.Expect(t, gateway.EventReady)
	e.Do(t, "POST", "/auth/logout", nil, cookie)
	bySession.ExpectClose(t, gateway.CloseAuthFailed)

	// A password change revokes personal tokens.
	other := e.Gateway(t, personalToken(t, mia), "deliveries")
	mia.Do(t, "PUT", "/users/@me/password", J{"current_password": testkit.Password, "new_password": "a brand new password"})
	other.ExpectClose(t, gateway.CloseAuthFailed)

	// A connection that loses an intent, and one that is switched off.
	narrowed, disabled := e.Connection(t, "discord"), e.Connection(t, "discord")
	n := e.Gateway(t, narrowed.Token, "items", "deliveries")
	d := e.Gateway(t, disabled.Token, "deliveries")
	admin.Do(t, "PATCH", "/admin/connections/"+narrowed.ID, J{"allowed_intents": []string{"deliveries"}})
	n.ExpectClose(t, gateway.CloseDisallowedIntents)
	admin.Do(t, "PATCH", "/admin/connections/"+disabled.ID, J{"enabled": false})
	d.ExpectClose(t, gateway.CloseAuthFailed)

	// Unrelated changes leave a session alone.
	kept := e.Connection(t, "discord")
	k := e.Gateway(t, kept.Token, "deliveries")
	admin.Do(t, "PATCH", "/admin/connections/"+kept.ID, J{"name": "renamed"})
	k.ExpectQuiet(t)
	k.Send(t, gateway.OpHeartbeat, nil)
	require.Equal(t, gateway.OpHeartbeatACK, k.Next(t).Op)
}

func TestExpiredSessionCookieDisconnectsOnHeartbeat(t *testing.T) {
	e := testkit.New(t)
	mia := e.User(t, "mia")
	gw := e.GatewayRaw(t, testkit.Cookie("plsr_session", mia.Session), testkit.Header("Origin", testkit.PublicURL))
	gw.Next(t)
	gw.Send(t, gateway.OpIdentify, J{"intents": []string{"deliveries"}})
	gw.Expect(t, gateway.EventReady)
	// No signal announces that a session ran out. The next heartbeat notices.
	e.Clock.Advance(31 * 24 * time.Hour)
	gw.Send(t, gateway.OpHeartbeat, nil)
	gw.ExpectClose(t, gateway.CloseAuthFailed)
}

func TestSlowClientDoesNotBlockOthers(t *testing.T) {
	e := testkit.New(t)
	e.Deps.RateAuth = ratelimit.New(100000, time.Minute, e.Clock.Now)
	e.Deps.Gateway.WriteTimeout = 500 * time.Millisecond
	mia := e.User(t, "mia")
	token := personalToken(t, mia)
	stuck := e.Gateway(t, token, "items")
	stuck.Expect(t, "user.sync")
	healthy := e.Gateway(t, token, "items")
	healthy.Expect(t, "user.sync")

	stuck.Pause() // stops reading, like a client that hangs
	note := strings.Repeat("x", 9000)
	const changes = 400
	started := time.Now()
	for i := 0; i < changes; i++ {
		r := mia.Do(t, "POST", "/users/@me/items", J{"title": fmt.Sprintf("item %d", i), "note": note}, testkit.NoContract())
		require.Equal(t, 201, r.Status)
	}
	for i := 0; i < changes; i++ {
		require.Equal(t, fmt.Sprintf("item %d", i), healthy.Expect(t, "item.created").Get("data.title").String())
	}
	require.Less(t, time.Since(started), 20*time.Second, "a stuck client must not slow the hub down")
	stuck.ExpectEnded(t)
}

func TestShutdownAsksToReconnect(t *testing.T) {
	e := testkit.New(t)
	gw := e.Gateway(t, personalToken(t, e.User(t, "mia")), "deliveries")
	e.Hub.Shutdown()
	require.Equal(t, gateway.OpReconnect, gw.NextOp(t).Op)
	gw.ExpectEnded(t)
}
