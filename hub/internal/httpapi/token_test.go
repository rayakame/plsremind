package httpapi_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/rayakame/plsremind/hub/internal/ratelimit"
	"github.com/rayakame/plsremind/hub/internal/testkit"
)

func TestPersonalTokenLifecycle(t *testing.T) {
	e := testkit.New(t)
	u := e.User(t, "mia")
	r := u.Do(t, "POST", "/users/@me/tokens", J{"name": "homelab"})
	require.Equal(t, 201, r.Status)
	tok := r.Get("token").String()
	require.True(t, strings.HasPrefix(tok, "plsr_pat_"))

	me := e.Do(t, "GET", "/users/@me", nil, testkit.Bearer(tok))
	require.Equal(t, 200, me.Status)
	require.Equal(t, u.UserID, me.Get("id").String())

	list := u.Do(t, "GET", "/users/@me/tokens", nil)
	require.Equal(t, "homelab", list.Get("0.name").String())
	require.False(t, list.Get("0.token").Exists())
	require.NotEmpty(t, list.Get("0.last_used_at").String())

	var stored []byte
	require.NoError(t, e.Pool.QueryRow(context.Background(), "SELECT token_hash FROM personal_tokens").Scan(&stored))
	require.NotContains(t, string(stored), tok)

	require.Equal(t, 204, u.Do(t, "DELETE", "/users/@me/tokens/"+r.Get("id").String(), nil).Status)
	require.Equal(t, 401, e.Do(t, "GET", "/users/@me", nil, testkit.Bearer(tok)).Status)
}

func TestUnknownBearerIs401(t *testing.T) {
	e := testkit.New(t)
	u := e.User(t, "mia")
	r := u.Do(t, "GET", "/users/@me", nil, testkit.Bearer("plsr_pat_doesnotexist"))
	require.Equal(t, 401, r.Status, "a bad token must not fall back to the session cookie")
	require.Equal(t, 401, e.Do(t, "GET", "/users/@me", nil, testkit.Header("Authorization", "Basic abc")).Status)
}

func TestPersonalTokenIsScoped(t *testing.T) {
	e := testkit.New(t)
	admin := e.Admin(t)
	tok := admin.Do(t, "POST", "/users/@me/tokens", J{"name": "cli"}).Get("token").String()
	bearer := testkit.Bearer(tok)
	for _, tc := range []struct {
		method, path string
		body         any
	}{
		{"POST", "/users/@me/tokens", J{"name": "more"}},
		{"GET", "/users/@me/tokens", nil},
		{"PUT", "/users/@me/password", J{"current_password": testkit.Password, "new_password": "something else"}},
		{"DELETE", "/users/@me", nil},
		{"GET", "/admin/users", nil},
	} {
		r := e.Do(t, tc.method, tc.path, tc.body, bearer)
		require.Equal(t, 403, r.Status, tc.method+" "+tc.path)
		require.Equal(t, "forbidden", r.Get("error.code").String())
	}
	require.Equal(t, 200, e.Do(t, "GET", "/users/@me/identities", nil, bearer).Status)
}

func TestTokenValidationAndLimit(t *testing.T) {
	e := testkit.New(t)
	u := e.User(t, "mia")
	require.Equal(t, 422, u.Do(t, "POST", "/users/@me/tokens", J{"name": "  "}).Status)
	for i := 0; i < 25; i++ {
		require.Equal(t, 201, u.Do(t, "POST", "/users/@me/tokens", J{"name": "t"}).Status)
	}
	r := u.Do(t, "POST", "/users/@me/tokens", J{"name": "one too many"})
	require.Equal(t, 409, r.Status)
	require.Equal(t, "token_limit", r.Get("error.code").String())
}

func TestTokensAreIsolated(t *testing.T) {
	e := testkit.New(t)
	mia, ben := e.User(t, "mia"), e.User(t, "ben")
	id := mia.Do(t, "POST", "/users/@me/tokens", J{"name": "x"}).Get("id").String()
	require.Equal(t, 404, ben.Do(t, "DELETE", "/users/@me/tokens/"+id, nil).Status)
	require.Equal(t, int64(0), ben.Do(t, "GET", "/users/@me/tokens", nil).Get("#").Int())
}

func TestBearerNeedsNoOrigin(t *testing.T) {
	e := testkit.New(t)
	u := e.User(t, "mia")
	tok := u.Do(t, "POST", "/users/@me/tokens", J{"name": "cli"}).Get("token").String()
	r := e.Do(t, "PATCH", "/users/@me", J{"display_name": "Mia"}, testkit.Bearer(tok), testkit.Header("Origin", "https://elsewhere.example"))
	require.Equal(t, 200, r.Status)
}

func TestRateLimitHeadersAnd429(t *testing.T) {
	e := testkit.New(t)
	u := e.User(t, "mia")
	e.Deps.RateAuth = ratelimit.New(3, time.Minute, e.Clock.Now)
	for _, remaining := range []string{"2", "1", "0"} {
		r := u.Do(t, "GET", "/users/@me", nil)
		require.Equal(t, 200, r.Status)
		require.Equal(t, "3", r.Header.Get("X-RateLimit-Limit"))
		require.Equal(t, remaining, r.Header.Get("X-RateLimit-Remaining"))
		require.NotEmpty(t, r.Header.Get("X-RateLimit-Reset"))
	}
	r := u.Do(t, "GET", "/users/@me", nil)
	require.Equal(t, 429, r.Status)
	require.Equal(t, "rate_limited", r.Get("error.code").String())
	require.Equal(t, "60", r.Header.Get("Retry-After"))

	e.Clock.Advance(61 * time.Second)
	require.Equal(t, 200, u.Do(t, "GET", "/users/@me", nil).Status)
}

func TestAnonymousRateLimit(t *testing.T) {
	e := testkit.New(t)
	e.Admin(t)
	e.Deps.RateAnon = ratelimit.New(2, time.Minute, e.Clock.Now)
	body := J{"username": "root", "password": "wrong wrong wrong"}
	require.Equal(t, 401, e.Do(t, "POST", "/auth/login", body).Status)
	require.Equal(t, 401, e.Do(t, "POST", "/auth/login", body).Status)
	require.Equal(t, 429, e.Do(t, "POST", "/auth/login", body).Status)
}
