package server_test

import (
	"context"
	"io"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/rayakame/plsremind/hub/internal/api"
	"github.com/rayakame/plsremind/hub/internal/testkit"
)

func get(t *testing.T, e *testkit.Env, path string) (int, string) {
	t.Helper()
	res, err := e.App.Test(httptest.NewRequest("GET", path, nil))
	require.NoError(t, err)
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	return res.StatusCode, string(body)
}

func TestHealthAndReadinessAnswerOk(t *testing.T) {
	e := testkit.New(t)
	for _, path := range []string{"/healthz", "/readyz"} {
		code, body := get(t, e, path)
		require.Equal(t, 200, code, path)
		require.Equal(t, "ok", body, path)
	}
}

func TestReadinessIsUnavailableWithoutTheDatabase(t *testing.T) {
	away, err := pgxpool.New(context.Background(), "postgres://plsremind@127.0.0.1:1/plsremind?sslmode=disable&connect_timeout=1")
	require.NoError(t, err)
	t.Cleanup(away.Close)
	e := testkit.NewWith(t, func(d *testkit.Deps) { d.Pool = away })
	code, body := get(t, e, "/readyz")
	require.Equal(t, 503, code)
	require.Equal(t, "unavailable", body)
	code, _ = get(t, e, "/healthz")
	require.Equal(t, 200, code, "the process itself still serves")
}

func TestHealthNeedsNoLoginAndIgnoresTheRateLimit(t *testing.T) {
	e := testkit.NewWith(t, func(d *testkit.Deps) { d.RateAnon = api.NewLimiter(1, time.Minute, d.Now) })
	get(t, e, "/api/v1/info")
	code, _ := get(t, e, "/api/v1/info")
	require.Equal(t, 429, code, "the anonymous budget is used up")
	for _, path := range []string{"/healthz", "/readyz"} {
		code, body := get(t, e, path)
		require.Equal(t, 200, code, path)
		require.Equal(t, "ok", body, path)
	}
}
