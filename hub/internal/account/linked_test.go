package account_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/rayakame/plsremind/hub/internal/testkit"
)

func TestALinkReturnsToTheConfirmationWithTheIdentity(t *testing.T) {
	e := testkit.New(t)
	testkit.EnableProvider(t, e, "discord")
	mia := e.User(t, "mia")

	r := mia.OAuthLink(t, "discord")
	require.Equal(t, 302, r.Status)
	provider, identity, code := r.Linked()
	require.Equal(t, "discord", provider)
	require.Empty(t, code)
	require.Equal(t, identityOf(t, mia, "discord").Get("id").String(), identity)
	require.Contains(t, r.Header.Get("Location"), testkit.PublicURL+"/linked/discord?identity=")
}

func TestLinkingTheSameAccountAgainIsASuccess(t *testing.T) {
	e := testkit.New(t)
	testkit.EnableProvider(t, e, "discord")
	mia := e.User(t, "mia")
	_, first, _ := mia.OAuthLink(t, "discord").Linked()
	require.NotEmpty(t, first)

	_, again, code := mia.OAuthLink(t, "discord").Linked()
	require.Empty(t, code)
	require.Equal(t, first, again)
}

func TestALinkFailureReturnsToTheConfirmationWithItsCode(t *testing.T) {
	e := testkit.New(t)
	fake := testkit.EnableProvider(t, e, "github")
	fake.User = testkit.OAuthUser{ID: "77", Name: "Ben"}
	mia, ben := e.User(t, "mia"), e.User(t, "ben")
	require.Empty(t, ben.OAuthLink(t, "github").AuthError())

	provider, identity, code := mia.OAuthLink(t, "github").Linked()
	require.Equal(t, "github", provider)
	require.Empty(t, identity)
	require.Equal(t, "identity_taken", code)

	fake.FailWith = "user"
	_, _, code = mia.OAuthLink(t, "github").Linked()
	require.Equal(t, "provider_error", code)
	fake.FailWith = ""

	require.Equal(t, 200, e.Admin(t).Do(t, "PUT", "/admin/providers/github", J{"link_enabled": false}).Status)
	_, _, code = mia.OAuthLink(t, "github").Linked()
	require.Equal(t, "link_disabled", code)
}

func TestALinkStartedByAnotherSessionReturnsToTheConfirmation(t *testing.T) {
	e := testkit.New(t)
	testkit.EnableProvider(t, e, "discord")
	mia, ben := e.User(t, "mia"), e.User(t, "ben")

	start := e.OAuthStart(t, "discord", "intent=link", mia.Opts()...)
	path, opts := e.OAuthCallback(t, start)
	r := e.Do(t, "GET", path, nil, append(opts, ben.Opts()...)...)
	provider, _, code := r.Linked()
	require.Equal(t, "discord", provider)
	require.Equal(t, "invalid_state", code)
}

func TestALoginStillReturnsHome(t *testing.T) {
	e := testkit.New(t)
	fake := testkit.EnableProvider(t, e, "discord")
	ok := e.OAuthLogin(t, "discord")
	require.Equal(t, testkit.PublicURL+"/", ok.Header.Get("Location"))

	fake.FailWith = "user"
	failed := e.OAuthLogin(t, "discord")
	require.Equal(t, testkit.PublicURL+"/?auth_error=provider_error", failed.Header.Get("Location"))
}

func TestAnUnknownStateStillReturnsHome(t *testing.T) {
	e := testkit.New(t)
	testkit.EnableProvider(t, e, "discord")
	r := e.Do(t, "GET", "/auth/oauth/discord/callback?code=x&state=nope", nil)
	require.Equal(t, testkit.PublicURL+"/?auth_error=invalid_state", r.Header.Get("Location"))
}
