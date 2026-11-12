package gateway_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/rayakame/plsremind/hub/internal/api"
	"github.com/rayakame/plsremind/hub/internal/gateway"
	"github.com/rayakame/plsremind/hub/internal/testkit"
)

func personalToken(t *testing.T, u *testkit.Client) string {
	t.Helper()
	r := u.Do(t, "POST", "/users/@me/tokens", J{"name": "gateway"})
	require.Equal(t, 201, r.Status)
	return r.Get("token").String()
}

func TestGatewayHelloIdentifyReady(t *testing.T) {
	e := testkit.New(t)
	mia := e.User(t, "mia")
	gw := e.GatewayRaw(t)
	hello := gw.Next(t)
	require.Equal(t, gateway.OpHello, hello.Op)
	require.Equal(t, int64(200), hello.Get("heartbeat_interval").Int(), "milliseconds")

	gw.Send(t, gateway.OpIdentify, J{"token": personalToken(t, mia), "intents": []string{"deliveries", "items"}})
	ready := gw.Next(t)
	require.Equal(t, gateway.OpDispatch, ready.Op)
	require.Equal(t, "READY", ready.T)
	require.Equal(t, int64(1), ready.S)
	require.NotEmpty(t, ready.Get("session_id").String())
	require.JSONEq(t, `["items","deliveries"]`, ready.Get("intents").Raw)
	require.Equal(t, "personal_token", ready.Get("principal.kind").String())
	require.Equal(t, mia.UserID, ready.Get("principal.user_id").String())
}

func TestGatewayReadyForConnection(t *testing.T) {
	e := testkit.New(t)
	bot := e.Connection(t, "discord")
	gw := e.Gateway(t, bot.Token, "deliveries")
	require.Equal(t, "connection", gw.Ready.Get("principal.kind").String())
	require.Equal(t, bot.ID, gw.Ready.Get("principal.connection.id").String())
	require.Equal(t, "discord", gw.Ready.Get("principal.connection.provider").String())
	require.False(t, gw.Ready.Get("principal.user_id").Exists())
}

func TestGatewayHeartbeat(t *testing.T) {
	e := testkit.New(t)
	gw := e.Gateway(t, personalToken(t, e.User(t, "mia")), "deliveries")
	gw.StopHeartbeat()
	for i := 0; i < 3; i++ {
		gw.Send(t, gateway.OpHeartbeat, nil)
		require.Equal(t, gateway.OpHeartbeatACK, gw.Next(t).Op)
		time.Sleep(100 * time.Millisecond)
	}
	gw.ExpectClose(t, gateway.CloseTimeout)
}

func TestGatewayIdentifyTimeout(t *testing.T) {
	e := testkit.New(t)
	gw := e.GatewayRaw(t)
	require.Equal(t, gateway.OpHello, gw.Next(t).Op)
	gw.ExpectClose(t, gateway.CloseTimeout)
}

func TestGatewayRejectsBadInput(t *testing.T) {
	e := testkit.New(t)
	token := personalToken(t, e.User(t, "mia"))

	garbage := e.GatewayRaw(t)
	garbage.Next(t)
	garbage.SendRaw(t, "{")
	garbage.ExpectClose(t, gateway.CloseDecode)

	unknown := e.GatewayRaw(t)
	unknown.Next(t)
	unknown.Send(t, 99, nil)
	unknown.ExpectClose(t, gateway.CloseUnknownOp)

	serverOnly := e.GatewayRaw(t)
	serverOnly.Next(t)
	serverOnly.Send(t, gateway.OpDispatch, J{})
	serverOnly.ExpectClose(t, gateway.CloseUnknownOp)

	early := e.GatewayRaw(t)
	early.Next(t)
	early.Send(t, gateway.OpHeartbeat, nil)
	require.Equal(t, gateway.OpHeartbeatACK, early.Next(t).Op, "a heartbeat before identifying is fine")

	twice := e.Gateway(t, token, "items")
	twice.Send(t, gateway.OpIdentify, J{"token": token, "intents": []string{"items"}})
	twice.ExpectClose(t, gateway.CloseAlreadyAuthenticated)
}

func TestGatewayAuth(t *testing.T) {
	e := testkit.New(t)
	admin := e.Admin(t)
	mia := e.User(t, "mia")
	token := personalToken(t, mia)

	bad := e.GatewayRaw(t)
	bad.Next(t)
	bad.Send(t, gateway.OpIdentify, J{"token": "plsr_pat_nope", "intents": []string{"items"}})
	bad.ExpectClose(t, gateway.CloseAuthFailed)

	session := e.GatewayRaw(t, testkit.Cookie("plsr_session", mia.Session), testkit.Header("Origin", testkit.PublicURL))
	session.Next(t)
	session.Send(t, gateway.OpIdentify, J{"intents": []string{"items"}})
	ready := session.Next(t)
	require.Equal(t, "session", ready.Get("principal.kind").String())
	require.Equal(t, mia.UserID, ready.Get("principal.user_id").String())

	foreign := e.GatewayRaw(t, testkit.Cookie("plsr_session", mia.Session), testkit.Header("Origin", "https://evil.example"))
	foreign.Next(t)
	foreign.Send(t, gateway.OpIdentify, J{"intents": []string{"items"}})
	foreign.ExpectClose(t, gateway.CloseAuthFailed)

	anonymous := e.GatewayRaw(t)
	anonymous.Next(t)
	anonymous.Send(t, gateway.OpIdentify, J{"intents": []string{"items"}})
	anonymous.ExpectClose(t, gateway.CloseAuthFailed)

	admin.Do(t, "PATCH", "/admin/users/"+mia.UserID, J{"disabled": true})
	disabled := e.GatewayRaw(t)
	disabled.Next(t)
	disabled.Send(t, gateway.OpIdentify, J{"token": token, "intents": []string{"items"}})
	disabled.ExpectClose(t, gateway.CloseAuthFailed)
}

func TestGatewayIntents(t *testing.T) {
	e := testkit.New(t)
	admin := e.Admin(t)
	token := personalToken(t, e.User(t, "mia"))
	bot := e.Connection(t, "discord")
	admin.Do(t, "PATCH", "/admin/connections/"+bot.ID, J{"allowed_intents": []string{"deliveries"}})

	for _, tc := range []struct {
		token   string
		intents any
		code    int
	}{
		{token, []string{}, gateway.CloseInvalidIntents},
		{token, nil, gateway.CloseInvalidIntents},
		{token, []string{"nope"}, gateway.CloseInvalidIntents},
		{bot.Token, []string{"deliveries", "items"}, gateway.CloseDisallowedIntents},
	} {
		gw := e.GatewayRaw(t)
		gw.Next(t)
		body := J{"token": tc.token}
		if tc.intents != nil {
			body["intents"] = tc.intents
		}
		gw.Send(t, gateway.OpIdentify, body)
		gw.ExpectClose(t, tc.code)
	}
	ok := e.Gateway(t, bot.Token, "deliveries")
	require.JSONEq(t, `["deliveries"]`, ok.Ready.Get("intents").Raw)
}

func TestGatewaySessionLimit(t *testing.T) {
	e := testkit.New(t)
	e.Deps.Gateway.MaxSessions = 2
	token := personalToken(t, e.User(t, "mia"))
	e.Gateway(t, token, "items")
	e.Gateway(t, token, "items")
	third := e.GatewayRaw(t)
	third.Next(t)
	third.Send(t, gateway.OpIdentify, J{"token": token, "intents": []string{"items"}})
	third.ExpectClose(t, gateway.CloseRateLimited)

	other := personalToken(t, e.User(t, "ben"))
	e.Gateway(t, other, "items")
}

func TestGatewayClientRateLimit(t *testing.T) {
	e := testkit.New(t)
	e.Deps.Gateway.MaxClientMessages = 5
	gw := e.Gateway(t, personalToken(t, e.User(t, "mia")), "deliveries") // identify is the first message
	gw.StopHeartbeat()
	for i := 0; i < 4; i++ {
		gw.Send(t, gateway.OpHeartbeat, nil)
		require.Equal(t, gateway.OpHeartbeatACK, gw.Next(t).Op)
	}
	gw.Send(t, gateway.OpHeartbeat, nil)
	gw.ExpectClose(t, gateway.CloseRateLimited)
}

func TestGatewayNeedsAnUpgrade(t *testing.T) {
	e := testkit.New(t)
	r := e.Do(t, "GET", "/gateway", nil)
	require.Equal(t, 426, r.Status)
	require.Equal(t, "upgrade_required", r.Get("error.code").String())
}

func TestInfoNamesGateway(t *testing.T) {
	r := testkit.New(t).Do(t, "GET", "/info", nil)
	require.Equal(t, "wss://hub.test/api/v1/gateway", r.Get("gateway_url").String())
}

func TestDatabaseTroubleDoesNotLookLikeRevokedAccess(t *testing.T) {
	e := testkit.New(t)
	gw := e.Gateway(t, personalToken(t, e.User(t, "mia")), "deliveries")
	ctx := context.Background()
	_, err := e.Pool.Exec(ctx, "ALTER TABLE personal_tokens RENAME TO personal_tokens_away")
	require.NoError(t, err)
	// Every heartbeat now fails to check the token. 4004 tells an adapter to give
	// up for good; a hiccup of the database must not say that.
	time.Sleep(4 * e.Deps.Gateway.HeartbeatInterval)
	_, err = e.Pool.Exec(ctx, "ALTER TABLE personal_tokens_away RENAME TO personal_tokens")
	require.NoError(t, err)
	gw.ExpectQuiet(t)
	gw.Send(t, gateway.OpHeartbeat, nil)
	require.Equal(t, gateway.OpHeartbeatACK, gw.Next(t).Op, "the session is still there")
}

func TestStuckClientDoesNotStallOtherUsers(t *testing.T) {
	e := testkit.New(t)
	e.Deps.RateAuth = api.NewLimiter(100000, time.Minute, e.Clock.Now)
	e.Deps.Gateway.WriteTimeout = 4 * time.Second
	mia, ben := e.User(t, "mia"), e.User(t, "ben")
	stuck := e.Gateway(t, personalToken(t, mia), "items")
	stuck.Expect(t, "user.sync")
	other := e.Gateway(t, personalToken(t, ben), "items")
	other.Expect(t, "user.sync")

	// Mia's client keeps its heartbeat going but stops reading, and gets far more
	// than its socket can hold.
	stuck.PauseReading()
	note := strings.Repeat("x", 9000)
	for i := 0; i < 600; i++ {
		mia.Do(t, "POST", "/users/@me/items", J{"title": "x", "note": note}, testkit.NoContract())
	}

	// Ben's events, new logins and resumes must not wait for her.
	var worst time.Duration
	for i := 0; i < 15; i++ {
		started := time.Now()
		ben.Do(t, "POST", "/users/@me/items", J{"title": fmt.Sprintf("ben %d", i)})
		require.Equal(t, fmt.Sprintf("ben %d", i), other.Expect(t, "item.created").Get("data.title").String())
		worst = max(worst, time.Since(started))
		fresh := e.Gateway(t, personalToken(t, ben), "deliveries")
		fresh.Close()
		time.Sleep(100 * time.Millisecond)
	}
	require.Less(t, worst, time.Second, "another user's event took %s", worst)
}

func TestDetachedSessionsDoNotCountAgainstTheLimit(t *testing.T) {
	e := testkit.New(t)
	e.Deps.Gateway.MaxSessions = 2
	token := personalToken(t, e.User(t, "mia"))
	// A client that reconnects with Identify instead of Resume, as the dashboard does on every page load.
	for i := 0; i < 6; i++ {
		gw := e.Gateway(t, token, "deliveries")
		gw.Close()
	}
	e.Gateway(t, token, "deliveries")
	e.Gateway(t, token, "deliveries")
	third := e.GatewayRaw(t)
	third.Next(t)
	third.Send(t, gateway.OpIdentify, J{"token": token, "intents": []string{"deliveries"}})
	third.ExpectClose(t, gateway.CloseRateLimited)
}
