package auth_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/rayakame/plsremind/hub/internal/api"
	"github.com/rayakame/plsremind/hub/internal/testkit"
)

func TestBadTokensCountAgainstTheAddress(t *testing.T) {
	e := testkit.New(t)
	e.Admin(t)
	e.Deps.RateAnon = api.NewLimiter(2, time.Minute, e.Clock.Now)
	bad := testkit.Bearer("plsr_pat_doesnotexist")
	first := e.Do(t, "GET", "/users/@me", nil, bad)
	require.Equal(t, 401, first.Status)
	require.Equal(t, "2", first.Header.Get("X-RateLimit-Limit"))
	require.Equal(t, "1", first.Header.Get("X-RateLimit-Remaining"))
	require.Equal(t, 401, e.Do(t, "GET", "/users/@me", nil, bad).Status)
	third := e.Do(t, "GET", "/users/@me", nil, bad)
	require.Equal(t, 429, third.Status)
	require.Equal(t, "rate_limited", third.Get("error.code").String())
}
