package httpapi_test

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
	require.ElementsMatch(t, []string{"password", "github", "discord"}, ids(list, "#.key"))

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
	enableProvider(t, e, "discord")
	admin := e.Admin(t)
	require.Empty(t, admin.OAuthLink(t, "discord").AuthError())
	require.Equal(t, 200, admin.Do(t, "PUT", "/admin/providers/password", J{"login_enabled": false}).Status)
	login := e.Do(t, "POST", "/auth/login", J{"username": "root", "password": testkit.Password})
	require.Equal(t, 403, login.Status)
	require.Equal(t, "login_disabled", login.Get("error.code").String())
	require.Equal(t, 403, e.Do(t, "POST", "/auth/register", J{"username": "mia", "password": testkit.Password}).Status)
}
