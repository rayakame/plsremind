package server_test

import (
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/require"

	"github.com/rayakame/plsremind/hub/internal/api"
	"github.com/rayakame/plsremind/hub/internal/server"
	"github.com/rayakame/plsremind/hub/internal/testkit"
)

func TestRootServesAPageOnEveryPath(t *testing.T) {
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

func TestWebHandlerServesTheBuiltDashboard(t *testing.T) {
	files := fstest.MapFS{
		"index.html":         {Data: []byte("<!doctype html><title>dashboard</title>")},
		"theme.js":           {Data: []byte("// theme")},
		"assets/app-abc.js":  {Data: []byte("console.log(1)")},
		"assets/app-abc.css": {Data: []byte("body{}")},
	}
	app := fiber.New()
	app.Get("/*", server.WebHandler(files))
	get := func(path string) (*http.Response, string) {
		req, _ := http.NewRequest("GET", path, nil)
		res, err := app.Test(req)
		require.NoError(t, err)
		body, _ := io.ReadAll(res.Body)
		return res, string(body)
	}

	for _, path := range []string{"/", "/lists/42", "/settings/personal", "/index.html", "/assets", "/assets/"} {
		res, body := get(path)
		require.Equal(t, 200, res.StatusCode, path)
		require.Contains(t, body, "dashboard", path)
		require.True(t, strings.HasPrefix(res.Header.Get("Content-Type"), "text/html"), path)
		require.Equal(t, "no-cache", res.Header.Get("Cache-Control"), path)
		csp := res.Header.Get("Content-Security-Policy")
		require.Contains(t, csp, "default-src 'self'", path)
		require.Contains(t, csp, "frame-ancestors 'none'", path)
		require.NotContains(t, csp, "unsafe", path)
		require.Equal(t, "nosniff", res.Header.Get("X-Content-Type-Options"), path)
		require.Equal(t, "same-origin", res.Header.Get("Referrer-Policy"), path)
	}

	res, body := get("/assets/app-abc.js")
	require.Equal(t, 200, res.StatusCode)
	require.Equal(t, "console.log(1)", body)
	require.True(t, strings.HasPrefix(res.Header.Get("Content-Type"), "text/javascript"))
	require.Equal(t, "public, max-age=31536000, immutable", res.Header.Get("Cache-Control"))
	require.Equal(t, "nosniff", res.Header.Get("X-Content-Type-Options"))

	res, _ = get("/assets/app-abc.css")
	require.True(t, strings.HasPrefix(res.Header.Get("Content-Type"), "text/css"))

	res, body = get("/theme.js")
	require.Equal(t, "// theme", body)
	require.Equal(t, "no-cache", res.Header.Get("Cache-Control"))

	res, _ = get("/assets/missing.js")
	require.Equal(t, 404, res.StatusCode)

	res, body = get("/..%2f..%2fetc/passwd")
	require.Equal(t, 200, res.StatusCode)
	require.Contains(t, body, "dashboard")
}

func TestWebHandlerFallsBackToThePlaceholder(t *testing.T) {
	app := fiber.New()
	app.Get("/*", server.WebHandler(fstest.MapFS{".gitkeep": {}}))
	req, _ := http.NewRequest("GET", "/lists/42", nil)
	res, err := app.Test(req)
	require.NoError(t, err)
	body, _ := io.ReadAll(res.Body)
	require.Equal(t, 200, res.StatusCode)
	require.Contains(t, string(body), "plsremind")
	require.Equal(t, "no-cache", res.Header.Get("Cache-Control"))
	require.NotEmpty(t, res.Header.Get("Content-Security-Policy"))
}
