package gateway_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"

	"github.com/rayakame/plsremind/hub/internal/gateway"
	"github.com/rayakame/plsremind/hub/internal/testkit"
)

func scrape(t *testing.T, e *testkit.Env) string {
	t.Helper()
	srv := httptest.NewServer(e.Deps.Metrics.Handler())
	defer srv.Close()
	res, err := http.Get(srv.URL)
	require.NoError(t, err)
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	return string(body)
}

func TestSessionsAreCountedByTheKindOfCredential(t *testing.T) {
	e := testkit.New(t)
	bot := e.Connection(t, "discord")
	mine := e.Gateway(t, personalToken(t, e.User(t, "mia")), "deliveries")
	e.Gateway(t, bot.Token, "deliveries")
	body := scrape(t, e)
	require.Contains(t, body, `plsremind_gateway_sessions{kind="personal_token"} 1`)
	require.Contains(t, body, `plsremind_gateway_sessions{kind="connection"} 1`)
	require.Contains(t, body, `plsremind_gateway_sessions{kind="session"} 0`)

	mine.Close()
	require.Eventually(t, func() bool {
		return strings.Contains(scrape(t, e), `plsremind_gateway_sessions{kind="personal_token"} 0`)
	}, 3*time.Second, 50*time.Millisecond)
}

func TestClosesAreCountedByCode(t *testing.T) {
	e := testkit.New(t)
	bad := e.GatewayRaw(t)
	bad.Next(t)
	bad.Send(t, gateway.OpIdentify, J{"token": "plsr_pat_nope", "intents": []string{"items"}})
	bad.ExpectClose(t, gateway.CloseAuthFailed)
	require.Eventually(t, func() bool {
		return testutil.ToFloat64(e.Deps.Metrics.GatewayCloses.WithLabelValues("4004")) == 1
	}, 2*time.Second, 20*time.Millisecond)
}

func TestDispatchesAreCountedByEvent(t *testing.T) {
	e := testkit.New(t)
	mia := e.User(t, "mia")
	gw := e.Gateway(t, personalToken(t, mia), "items")
	gw.Expect(t, "user.sync")
	mia.Do(t, "POST", "/users/@me/items", J{"title": "x"})
	gw.Expect(t, "item.created")
	m := e.Deps.Metrics
	require.Equal(t, 1.0, testutil.ToFloat64(m.GatewayDispatches.WithLabelValues("READY")))
	require.Equal(t, 1.0, testutil.ToFloat64(m.GatewayDispatches.WithLabelValues("item.created")))
}
