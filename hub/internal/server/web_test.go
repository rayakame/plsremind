package server_test

import (
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/require"

	"github.com/rayakame/plsremind/hub/internal/api"
	"github.com/rayakame/plsremind/hub/internal/server"
	"github.com/rayakame/plsremind/hub/internal/testkit"
)

func TestRootServesPlaceholder(t *testing.T) {
	e := testkit.New(t)
	for _, path := range []string{"/", "/lists/42", "/settings"} {
		req, _ := http.NewRequest("GET", path, nil)
		res, err := e.App.Test(req)
		require.NoError(t, err)
		require.Equal(t, 200, res.StatusCode, path)
		require.True(t, strings.HasPrefix(res.Header.Get("Content-Type"), "text/html"), path)
	}
	api := e.Do(t, "GET", "/nope", nil, testkit.NoContract())
	require.Equal(t, 404, api.Status)
	require.Equal(t, "not_found", api.Get("error.code").String())

	req, _ := http.NewRequest("GET", "/api", nil)
	res, err := e.App.Test(req)
	require.NoError(t, err)
	require.Equal(t, 404, res.StatusCode)
	require.True(t, strings.HasPrefix(res.Header.Get("Content-Type"), "application/json"))
}

func TestProxyHeaderSeparatesAnonymousClients(t *testing.T) {
	e := testkit.New(t)
	e.Admin(t)
	d := *e.Deps
	d.Cfg.ProxyHeader = "X-Forwarded-For"
	d.RateAnon = api.NewLimiter(1, time.Minute, e.Clock.Now)
	app := server.New(&d).App
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = app.Listener(ln, fiber.ListenConfig{DisableStartupMessage: true}) }()
	t.Cleanup(func() { _ = app.Shutdown() })

	get := func(client string) int {
		req, _ := http.NewRequest("GET", "http://"+ln.Addr().String()+"/api/v1/info", nil)
		req.Header.Set("X-Forwarded-For", client)
		res, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		res.Body.Close()
		return res.StatusCode
	}
	require.Equal(t, 200, get("203.0.113.7"))
	require.Equal(t, 429, get("203.0.113.7"))
	require.Equal(t, 200, get("203.0.113.8"), "another client behind the same proxy has its own budget")
}
