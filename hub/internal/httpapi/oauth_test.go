package httpapi_test

import (
	"context"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/rayakame/plsremind/hub/internal/deps"
	"github.com/rayakame/plsremind/hub/internal/testkit"
)

// enableProvider configures GitHub or Discord against a fake and opens registration.
func enableProvider(t *testing.T, e *testkit.Env, kind string) *testkit.FakeOAuth {
	t.Helper()
	fake := testkit.NewFakeOAuth(t, kind)
	if e.Deps.OAuthEndpoints == nil {
		e.Deps.OAuthEndpoints = map[string]deps.OAuthEndpoints{}
	}
	e.Deps.OAuthEndpoints[kind] = fake.Endpoints
	admin := e.Admin(t)
	r := admin.Do(t, "PUT", "/admin/providers/"+kind, J{"client_id": "cid", "client_secret": "sec", "login_enabled": true, "link_enabled": true})
	require.Equal(t, 200, r.Status, string(r.Body))
	require.Equal(t, 200, admin.Do(t, "PATCH", "/admin/settings", J{"registration": "open"}).Status)
	return fake
}

func countUsers(t *testing.T, e *testkit.Env) int {
	t.Helper()
	var n int
	require.NoError(t, e.Pool.QueryRow(context.Background(), "SELECT count(*) FROM users").Scan(&n))
	return n
}

func sessionOf(r *testkit.Resp) testkit.ReqOpt {
	return testkit.Cookie("plsr_session", r.Cookie("plsr_session"))
}

func TestDiscordLoginCreatesAccountAndBotFindsIt(t *testing.T) {
	e := testkit.New(t)
	fake := enableProvider(t, e, "discord")
	fake.User = testkit.OAuthUser{ID: "1234", Name: "Mia"}

	r := e.OAuthLogin(t, "discord")
	require.Equal(t, 302, r.Status)
	require.Equal(t, testkit.PublicURL+"/", r.Header.Get("Location"))
	me := e.Do(t, "GET", "/users/@me", nil, sessionOf(r))
	require.Equal(t, 200, me.Status)
	require.Equal(t, "Mia", me.Get("display_name").String())
	require.Equal(t, "user", me.Get("role").String())

	identities := e.Do(t, "GET", "/users/@me/identities", nil, sessionOf(r))
	require.Equal(t, "discord", identities.Get("0.provider").String())
	require.Equal(t, "1234", identities.Get("0.external_id").String())

	bot := e.Connection(t, "discord")
	found := bot.Do(t, "GET", "/connection/users/lookup?external_id=1234", nil)
	require.Equal(t, me.Get("id").String(), found.Get("id").String())

	var token []byte
	require.NoError(t, e.Pool.QueryRow(context.Background(), "SELECT provider_token FROM identities WHERE provider = 'discord'").Scan(&token))
	require.NotEmpty(t, token)
	require.NotContains(t, string(token), "refresh-1")
}

func TestSecondLoginReusesAccount(t *testing.T) {
	e := testkit.New(t)
	enableProvider(t, e, "discord")
	first := e.OAuthLogin(t, "discord")
	second := e.OAuthLogin(t, "discord")
	a := e.Do(t, "GET", "/users/@me", nil, sessionOf(first)).Get("id").String()
	b := e.Do(t, "GET", "/users/@me", nil, sessionOf(second)).Get("id").String()
	require.Equal(t, a, b)
	require.Equal(t, 2, countUsers(t, e))
}

func TestOAuthRegistrationRespectsInvite(t *testing.T) {
	e := testkit.New(t)
	enableProvider(t, e, "discord")
	admin := e.Admin(t)
	admin.Do(t, "PATCH", "/admin/settings", J{"registration": "invite"})

	closed := e.OAuthLogin(t, "discord")
	require.Equal(t, 302, closed.Status)
	require.Equal(t, "registration_closed", closed.AuthError())
	require.Empty(t, closed.Cookie("plsr_session"))
	require.Equal(t, 1, countUsers(t, e))

	bad := e.OAuthFlow(t, "discord", "intent=login&invite_code=plsr_inv_nope")
	require.Equal(t, "invalid_invite", bad.AuthError())

	code := admin.Do(t, "POST", "/admin/invites", J{}).Get("code").String()
	ok := e.OAuthFlow(t, "discord", "intent=login&invite_code="+code)
	require.Empty(t, ok.AuthError())
	require.Equal(t, 2, countUsers(t, e))
	require.NotEmpty(t, admin.Do(t, "GET", "/admin/invites", nil).Get("0.used_at").String())
}

func TestLinkAndUnlink(t *testing.T) {
	e := testkit.New(t)
	fake := enableProvider(t, e, "github")
	fake.User = testkit.OAuthUser{ID: "77", Name: "Octo Mia"}
	mia := e.User(t, "mia")

	linked := mia.OAuthLink(t, "github")
	require.Equal(t, 302, linked.Status)
	require.Empty(t, linked.AuthError())
	identities := mia.Do(t, "GET", "/users/@me/identities", nil)
	require.Equal(t, int64(2), identities.Get("#").Int())
	require.Equal(t, "77", identities.Get("1.external_id").String())
	require.Equal(t, "Octo Mia", identities.Get("1.display_name").String())
	require.Equal(t, 2, countUsers(t, e), "linking must not create an account")

	again := mia.OAuthLink(t, "github")
	require.Empty(t, again.AuthError(), "linking the same account twice is fine")
	require.Equal(t, int64(2), mia.Do(t, "GET", "/users/@me/identities", nil).Get("#").Int())

	byGitHub := e.OAuthLogin(t, "github")
	require.Equal(t, mia.UserID, e.Do(t, "GET", "/users/@me", nil, sessionOf(byGitHub)).Get("id").String())

	unlink := mia.Do(t, "DELETE", "/users/@me/identities/"+identities.Get("1.id").String(), nil)
	require.Equal(t, 204, unlink.Status)
	require.NotEmpty(t, fake.Revoked())
	require.True(t, strings.HasPrefix(fake.Revoked()[0], "access-"))

	last := mia.Do(t, "DELETE", "/users/@me/identities/"+identities.Get("0.id").String(), nil)
	require.Equal(t, 409, last.Status)
	require.Equal(t, "last_identity", last.Get("error.code").String())
}

func TestUnlinkIsSessionOnlyAndIsolated(t *testing.T) {
	e := testkit.New(t)
	enableProvider(t, e, "github")
	mia, ben := e.User(t, "mia"), e.User(t, "ben")
	mia.OAuthLink(t, "github")
	id := mia.Do(t, "GET", "/users/@me/identities", nil).Get("1.id").String()
	require.Equal(t, 404, ben.Do(t, "DELETE", "/users/@me/identities/"+id, nil).Status)
	tok := mia.Do(t, "POST", "/users/@me/tokens", J{"name": "cli"}).Get("token").String()
	require.Equal(t, 403, e.Do(t, "DELETE", "/users/@me/identities/"+id, nil, testkit.Bearer(tok)).Status)
}

func TestLinkTakenIdentity(t *testing.T) {
	e := testkit.New(t)
	fake := enableProvider(t, e, "github")
	fake.User = testkit.OAuthUser{ID: "77", Name: "Ben"}
	mia, ben := e.User(t, "mia"), e.User(t, "ben")
	require.Empty(t, ben.OAuthLink(t, "github").AuthError())

	taken := mia.OAuthLink(t, "github")
	require.Equal(t, "identity_taken", taken.AuthError())
	require.Equal(t, int64(1), mia.Do(t, "GET", "/users/@me/identities", nil).Get("#").Int())
}

func TestLinkNeedsSession(t *testing.T) {
	e := testkit.New(t)
	enableProvider(t, e, "github")
	require.Equal(t, 401, e.OAuthStart(t, "github", "intent=link").Status)
	require.Equal(t, 422, e.OAuthStart(t, "github", "intent=steal", testkit.NoContract()).Status)
	require.Equal(t, 404, e.OAuthStart(t, "nope", "intent=login").Status)
	require.Equal(t, 404, e.OAuthStart(t, "password", "intent=login").Status)
	require.Equal(t, 404, e.OAuthStart(t, "discord", "intent=login").Status, "not configured")
}

func TestCallbackRejectsReplayedState(t *testing.T) {
	e := testkit.New(t)
	enableProvider(t, e, "discord")
	enableProvider(t, e, "github")

	path, opts := e.OAuthCallback(t, e.OAuthStart(t, "discord", "intent=login"))
	first := e.Do(t, "GET", path, nil, opts...)
	require.Empty(t, first.AuthError())
	replay := e.Do(t, "GET", path, nil, opts...)
	require.Equal(t, 302, replay.Status)
	require.Equal(t, "invalid_state", replay.AuthError())
	require.Empty(t, replay.Cookie("plsr_session"))

	path, opts = e.OAuthCallback(t, e.OAuthStart(t, "discord", "intent=login"))
	e.Clock.Advance(11 * time.Minute)
	require.Equal(t, "invalid_state", e.Do(t, "GET", path, nil, opts...).AuthError())

	path, opts = e.OAuthCallback(t, e.OAuthStart(t, "discord", "intent=login"))
	crossed := strings.Replace(path, "/discord/", "/github/", 1)
	require.Equal(t, "invalid_state", e.Do(t, "GET", crossed, nil, opts...).AuthError())

	path, _ = e.OAuthCallback(t, e.OAuthStart(t, "discord", "intent=login"))
	require.Equal(t, "invalid_state", e.Do(t, "GET", path, nil).AuthError(), "the state must belong to this browser")
	require.Equal(t, "invalid_state", e.Do(t, "GET", "/auth/oauth/discord/callback?code=x", nil).AuthError())
	require.Equal(t, 2, countUsers(t, e))
}

func TestCallbackProviderError(t *testing.T) {
	e := testkit.New(t)
	fake := enableProvider(t, e, "discord")

	start := e.OAuthStart(t, "discord", "intent=login")
	location, err := url.Parse(start.Header.Get("Location"))
	require.NoError(t, err)
	state := location.Query().Get("state")
	denied := e.Do(t, "GET", "/auth/oauth/discord/callback?error=access_denied&state="+state, nil,
		testkit.Cookie("plsr_oauth_state", start.Cookie("plsr_oauth_state")))
	require.Equal(t, "provider_error", denied.AuthError())

	fake.FailWith = "token"
	require.Equal(t, "provider_error", e.OAuthLogin(t, "discord").AuthError())
	fake.FailWith = "user"
	failed := e.OAuthLogin(t, "discord")
	require.Equal(t, "provider_error", failed.AuthError())
	require.Empty(t, failed.Cookie("plsr_session"))
	require.Equal(t, 1, countUsers(t, e))
}

func TestLoginDisabledLinkStillWorks(t *testing.T) {
	e := testkit.New(t)
	enableProvider(t, e, "discord")
	admin := e.Admin(t)
	mia := e.User(t, "mia")
	require.Equal(t, 200, admin.Do(t, "PUT", "/admin/providers/discord", J{"login_enabled": false}).Status)

	require.Equal(t, "login_disabled", e.OAuthLogin(t, "discord").AuthError())
	require.Empty(t, mia.OAuthLink(t, "discord").AuthError())
	bot := e.Connection(t, "discord")
	require.Equal(t, mia.UserID, bot.Do(t, "GET", "/connection/users/lookup?external_id=1234", nil).Get("id").String())
	require.Equal(t, "login_disabled", e.OAuthLogin(t, "discord").AuthError(), "a linked account cannot log in either")

	admin.Do(t, "PUT", "/admin/providers/discord", J{"login_enabled": true, "link_enabled": false})
	ben := e.User(t, "ben")
	require.Equal(t, "link_disabled", ben.OAuthLink(t, "discord").AuthError())
}

func TestOAuthLoginOfDisabledUser(t *testing.T) {
	e := testkit.New(t)
	enableProvider(t, e, "discord")
	admin := e.Admin(t)
	first := e.OAuthLogin(t, "discord")
	id := e.Do(t, "GET", "/users/@me", nil, sessionOf(first)).Get("id").String()
	admin.Do(t, "PATCH", "/admin/users/"+id, J{"disabled": true})
	r := e.OAuthLogin(t, "discord")
	require.Equal(t, "account_disabled", r.AuthError())
	require.Empty(t, r.Cookie("plsr_session"))
}

func TestOIDCProvider(t *testing.T) {
	e := testkit.New(t)
	admin := e.Admin(t)
	admin.Do(t, "PATCH", "/admin/settings", J{"registration": "open"})
	fake := testkit.NewFakeOAuth(t, "oidc")
	fake.User = testkit.OAuthUser{ID: "sub-42", Name: "Mia Musterfrau"}
	put := admin.Do(t, "PUT", "/admin/providers/authentik", J{
		"type": "oidc", "name": "Authentik", "issuer_url": fake.IssuerURL,
		"client_id": "cid", "client_secret": "sec", "login_enabled": true, "link_enabled": true,
	})
	require.Equal(t, 200, put.Status, string(put.Body))

	r := e.OAuthLogin(t, "authentik")
	require.Empty(t, r.AuthError())
	identities := e.Do(t, "GET", "/users/@me/identities", nil, sessionOf(r))
	require.Equal(t, "authentik", identities.Get("0.provider").String())
	require.Equal(t, "sub-42", identities.Get("0.external_id").String())
	require.Equal(t, "Mia Musterfrau", identities.Get("0.display_name").String())

	fake.BadSignature = true
	fake.User.ID = "sub-43"
	require.Equal(t, "provider_error", e.OAuthLogin(t, "authentik").AuthError())
	require.Equal(t, 2, countUsers(t, e))

	fake.BadSignature = false
	require.Equal(t, 204, e.Do(t, "DELETE", "/users/@me", nil, sessionOf(r)).Status)
	require.Equal(t, []string{"refresh-1"}, fake.Revoked())
}

func TestAddPasswordToOAuthAccount(t *testing.T) {
	e := testkit.New(t)
	enableProvider(t, e, "discord")
	r := e.OAuthLogin(t, "discord")
	session := sessionOf(r)
	missing := e.Do(t, "PUT", "/users/@me/password", J{"new_password": testkit.Password}, session)
	require.Equal(t, 422, missing.Status)
	ok := e.Do(t, "PUT", "/users/@me/password", J{"username": "mia", "new_password": testkit.Password}, session)
	require.Equal(t, 204, ok.Status)
	require.Equal(t, 200, e.Do(t, "POST", "/auth/login", J{"username": "mia", "password": testkit.Password}).Status)
	require.Equal(t, int64(2), e.Do(t, "GET", "/users/@me/identities", nil, session).Get("#").Int())
}

func TestDeleteAccountRevokesProviderTokens(t *testing.T) {
	e := testkit.New(t)
	fake := enableProvider(t, e, "discord")
	r := e.OAuthLogin(t, "discord")
	require.Equal(t, 204, e.Do(t, "DELETE", "/users/@me", nil, sessionOf(r)).Status)
	require.Equal(t, []string{"refresh-1"}, fake.Revoked())

	again := e.OAuthLogin(t, "discord")
	id := e.Do(t, "GET", "/users/@me", nil, sessionOf(again)).Get("id").String()
	require.Equal(t, 204, e.Admin(t).Do(t, "DELETE", "/admin/users/"+id, nil).Status)
	require.Len(t, fake.Revoked(), 2)
}

func TestInfoListsEnabledLoginMethods(t *testing.T) {
	e := testkit.New(t)
	before := e.Do(t, "GET", "/info", nil)
	require.Equal(t, int64(1), before.Get("login_methods.#").Int())
	enableProvider(t, e, "discord")
	after := e.Do(t, "GET", "/info", nil)
	require.Equal(t, []string{"password", "discord"}, ids(after, "login_methods.#.key"))
	require.Equal(t, "Discord", after.Get("login_methods.1.name").String())
}
