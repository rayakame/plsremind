package account_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/rayakame/plsremind/hub/internal/testkit"
)

func TestProviderAdminRules(t *testing.T) {
	e := testkit.New(t)
	admin := e.Admin(t)

	list := admin.Do(t, "GET", "/admin/providers", nil)
	require.Equal(t, 200, list.Status)
	require.ElementsMatch(t, []string{"password", "github", "discord"}, testkit.Strings(list, "#.key"))

	noCreds := admin.Do(t, "PUT", "/admin/providers/github", J{"login_enabled": true})
	require.Equal(t, 422, noCreds.Status)

	ok := admin.Do(t, "PUT", "/admin/providers/github", J{"client_id": "cid", "client_secret": "sec", "login_enabled": true})
	require.Equal(t, 200, ok.Status)
	require.True(t, ok.Get("has_client_secret").Bool())
	require.False(t, ok.Get("client_secret").Exists())
	require.Equal(t, "cid", ok.Get("client_id").String())
	require.True(t, ok.Get("login_enabled").Bool())
	require.False(t, ok.Get("link_enabled").Bool())

	var stored []byte
	require.NoError(t, e.Pool.QueryRow(context.Background(), "SELECT client_secret FROM login_providers WHERE key = 'github'").Scan(&stored))
	require.NotEmpty(t, stored)
	require.NotContains(t, string(stored), "sec")

	keepsSecret := admin.Do(t, "PUT", "/admin/providers/github", J{"name": "GitHub.com"})
	require.True(t, keepsSecret.Get("has_client_secret").Bool())
	require.Equal(t, "GitHub.com", keepsSecret.Get("name").String())

	admin.Do(t, "PUT", "/admin/providers/github", J{"login_enabled": false})
	last := admin.Do(t, "PUT", "/admin/providers/password", J{"login_enabled": false})
	require.Equal(t, 409, last.Status)
	require.Equal(t, "last_login_method", last.Get("error.code").String())

	for _, tc := range []struct {
		key  string
		body J
	}{
		{"keycloak", J{"name": "Keycloak"}},
		{"keycloak", J{"type": "github", "name": "x"}},
		{"Key.Cloak", J{"type": "oidc", "name": "x"}},
		{"keycloak", J{"type": "oidc", "name": "x", "issuer_url": "not a url", "client_id": "a", "client_secret": "b", "login_enabled": true}},
		{"github", J{"type": "oidc"}},
	} {
		require.Equal(t, 422, admin.Do(t, "PUT", "/admin/providers/"+tc.key, tc.body).Status, tc)
	}

	require.Equal(t, 422, admin.Do(t, "DELETE", "/admin/providers/github", nil).Status)
	require.Equal(t, 404, admin.Do(t, "DELETE", "/admin/providers/nope", nil).Status)
}

func TestDeleteOIDCProvider(t *testing.T) {
	e := testkit.New(t)
	admin := e.Admin(t)
	fake := testkit.NewFakeOAuth(t, "oidc")
	admin.Do(t, "PATCH", "/admin/settings", J{"registration": "open"})
	body := J{"type": "oidc", "name": "Keycloak", "issuer_url": fake.IssuerURL, "client_id": "cid", "client_secret": "sec", "login_enabled": true}
	require.Equal(t, 200, admin.Do(t, "PUT", "/admin/providers/keycloak", body).Status)
	require.Equal(t, 200, admin.Do(t, "PUT", "/admin/providers/unused", body).Status)

	require.Empty(t, e.OAuthLogin(t, "keycloak").AuthError())
	inUse := admin.Do(t, "DELETE", "/admin/providers/keycloak", nil)
	require.Equal(t, 409, inUse.Status)
	require.Equal(t, "provider_in_use", inUse.Get("error.code").String())
	require.Equal(t, 204, admin.Do(t, "DELETE", "/admin/providers/unused", nil).Status)
}

func TestPasswordLoginCanBeTurnedOff(t *testing.T) {
	e := testkit.New(t)
	testkit.EnableProvider(t, e, "discord")
	admin := e.Admin(t)
	require.Empty(t, admin.OAuthLink(t, "discord").AuthError())
	require.Equal(t, 200, admin.Do(t, "PUT", "/admin/providers/password", J{"login_enabled": false}).Status)
	login := e.Do(t, "POST", "/auth/login", J{"username": "root", "password": testkit.Password})
	require.Equal(t, 403, login.Status)
	require.Equal(t, "login_disabled", login.Get("error.code").String())
	require.Equal(t, 403, e.Do(t, "POST", "/auth/register", J{"username": "mia", "password": testkit.Password}).Status)
}

func TestIssuerCannotChangeWhileAccountsAreLinked(t *testing.T) {
	e := testkit.New(t)
	admin := e.Admin(t)
	fake := oidcProvider(t, e, "authentik")
	other := testkit.NewFakeOAuth(t, "oidc")

	typo := admin.Do(t, "PUT", "/admin/providers/authentik", J{"issuer_url": other.IssuerURL})
	require.Equal(t, 200, typo.Status, "free to change while nobody is linked")
	require.Equal(t, 200, admin.Do(t, "PUT", "/admin/providers/authentik", J{"issuer_url": fake.IssuerURL}).Status)

	require.Empty(t, e.OAuthLogin(t, "authentik").AuthError())
	moved := admin.Do(t, "PUT", "/admin/providers/authentik", J{"issuer_url": other.IssuerURL})
	require.Equal(t, 409, moved.Status)
	require.Equal(t, "provider_in_use", moved.Get("error.code").String())
	same := admin.Do(t, "PUT", "/admin/providers/authentik", J{"issuer_url": fake.IssuerURL, "name": "Authentik"})
	require.Equal(t, 200, same.Status, "sending the unchanged issuer is fine")
}

func TestAdminCannotDisableTheirOnlyLogin(t *testing.T) {
	e := testkit.New(t)
	testkit.EnableProvider(t, e, "discord")
	admin := e.Admin(t)

	locked := admin.Do(t, "PUT", "/admin/providers/password", J{"login_enabled": false})
	require.Equal(t, 409, locked.Status, "the admin only has a password")
	require.Equal(t, "last_login_method", locked.Get("error.code").String())
	require.Equal(t, 200, e.Do(t, "POST", "/auth/login", J{"username": "root", "password": testkit.Password}).Status)

	require.Empty(t, admin.OAuthLink(t, "discord").AuthError())
	require.Equal(t, 200, admin.Do(t, "PUT", "/admin/providers/password", J{"login_enabled": false}).Status)
	back := admin.Do(t, "PUT", "/admin/providers/discord", J{"login_enabled": false})
	require.Equal(t, 409, back.Status)
}

func TestIssuerIsStoredAsGiven(t *testing.T) {
	e := testkit.New(t)
	admin := e.Admin(t)
	// Some providers, Auth0 among them, publish an issuer that ends with a slash,
	// and the issuer has to match what they publish exactly.
	r := admin.Do(t, "PUT", "/admin/providers/auth0", J{"type": "oidc", "name": "Auth0", "issuer_url": " https://tenant.example.com/ "})
	require.Equal(t, 200, r.Status)
	require.Equal(t, "https://tenant.example.com/", r.Get("issuer_url").String())
}
