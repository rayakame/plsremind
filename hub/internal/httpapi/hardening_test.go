package httpapi_test

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/rayakame/plsremind/hub/internal/ratelimit"
	"github.com/rayakame/plsremind/hub/internal/testkit"
)

func TestLastAdminSurvivesConcurrentChanges(t *testing.T) {
	e := testkit.New(t)
	root := e.Admin(t)
	mia, ben := e.User(t, "mia"), e.User(t, "ben")
	ctx := context.Background()
	for round := 0; round < 40; round++ {
		_, err := e.Pool.Exec(ctx, "UPDATE users SET role = CASE WHEN id = $1 THEN 'user' ELSE 'admin' END, disabled = false", ben.UserID)
		require.NoError(t, err)
		var wg sync.WaitGroup
		for _, change := range []struct {
			user string
			body J
		}{
			{ben.UserID, J{"role": "admin"}},
			{ben.UserID, J{"role": "user"}},
			{root.UserID, J{"role": "user"}},
			{mia.UserID, J{"role": "user"}},
			{ben.UserID, J{"disabled": true}},
		} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				root.Do(t, "PATCH", "/admin/users/"+change.user, change.body, testkit.NoContract())
			}()
		}
		wg.Wait()
		var admins int
		require.NoError(t, e.Pool.QueryRow(ctx, "SELECT count(*) FROM users WHERE role = 'admin' AND NOT disabled").Scan(&admins))
		require.GreaterOrEqual(t, admins, 1, "round %d left the instance without an admin", round)
	}
}

func TestOnlyOnePasswordPerAccount(t *testing.T) {
	e := testkit.New(t)
	fake := enableProvider(t, e, "discord")
	for round := 0; round < 6; round++ {
		fake.User = testkit.OAuthUser{ID: fmt.Sprintf("90%d", round), Name: "Mia"}
		session := sessionOf(e.OAuthLogin(t, "discord"))
		statuses := make([]int, 2)
		var wg sync.WaitGroup
		for i := range statuses {
			wg.Add(1)
			go func() {
				defer wg.Done()
				body := J{"username": fmt.Sprintf("mia%d_%d", round, i), "new_password": testkit.Password}
				statuses[i] = e.Do(t, "PUT", "/users/@me/password", body, session, testkit.NoContract()).Status
			}()
		}
		wg.Wait()
		// Whoever comes second finds a password already set: asked for the current one,
		// or stopped by the database if both got past that check.
		slices.Sort(statuses)
		require.Equal(t, 204, statuses[0], "round %d", round)
		require.Contains(t, []int{403, 409}, statuses[1], "round %d", round)
		require.Equal(t, int64(2), e.Do(t, "GET", "/users/@me/identities", nil, session).Get("#").Int())
	}
}

func TestSecondPasswordIsRefused(t *testing.T) {
	e := testkit.New(t)
	mia := e.User(t, "mia")
	_, err := e.Pool.Exec(context.Background(),
		`INSERT INTO identities (id, user_id, provider, external_id, display_name, password_hash, created_at)
		 VALUES (gen_random_uuid(), $1, 'password', 'mia-two', 'mia-two', 'x', now())`, mia.UserID)
	require.Error(t, err, "the database itself must refuse a second password for one account")
}

func TestBadTokensCountAgainstTheAddress(t *testing.T) {
	e := testkit.New(t)
	e.Admin(t)
	e.Deps.RateAnon = ratelimit.New(2, time.Minute, e.Clock.Now)
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

func oidcProvider(t *testing.T, e *testkit.Env, key string) *testkit.FakeOAuth {
	t.Helper()
	admin := e.Admin(t)
	admin.Do(t, "PATCH", "/admin/settings", J{"registration": "open"})
	fake := testkit.NewFakeOAuth(t, "oidc")
	r := admin.Do(t, "PUT", "/admin/providers/"+key, J{
		"type": "oidc", "name": "Login", "issuer_url": fake.IssuerURL,
		"client_id": "cid", "client_secret": "sec", "login_enabled": true, "link_enabled": true,
	})
	require.Equal(t, 200, r.Status, string(r.Body))
	return fake
}

func TestOIDCDiscoveryIsCached(t *testing.T) {
	e := testkit.New(t)
	admin := e.Admin(t)
	fake := oidcProvider(t, e, "authentik")

	require.Empty(t, e.OAuthLogin(t, "authentik").AuthError())
	require.Empty(t, e.OAuthLogin(t, "authentik").AuthError())
	require.Equal(t, 1, fake.Discoveries(), "two logins share one discovery")

	e.Clock.Advance(11 * time.Minute)
	require.Empty(t, e.OAuthLogin(t, "authentik").AuthError())
	require.Equal(t, 2, fake.Discoveries(), "the cached configuration expires")

	require.Equal(t, 200, admin.Do(t, "PUT", "/admin/providers/authentik", J{"client_secret": "rotated"}).Status)
	require.Empty(t, e.OAuthLogin(t, "authentik").AuthError())
	require.Equal(t, 3, fake.Discoveries(), "changed credentials are not served from the cache")
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

func TestCannotUnlinkLastUsableLogin(t *testing.T) {
	e := testkit.New(t)
	enableProvider(t, e, "discord")
	admin := e.Admin(t)
	mia := e.User(t, "mia")
	require.Empty(t, mia.OAuthLink(t, "discord").AuthError())
	identities := mia.Do(t, "GET", "/users/@me/identities", nil)
	password, discord := identities.Get("0.id").String(), identities.Get("1.id").String()

	require.Equal(t, 200, admin.Do(t, "PUT", "/admin/providers/discord", J{"login_enabled": false}).Status)
	stuck := mia.Do(t, "DELETE", "/users/@me/identities/"+password, nil)
	require.Equal(t, 409, stuck.Status, "Discord is linked but cannot be used to log in")
	require.Equal(t, "last_identity", stuck.Get("error.code").String())

	require.Equal(t, 204, mia.Do(t, "DELETE", "/users/@me/identities/"+discord, nil).Status)
	require.Equal(t, 409, mia.Do(t, "DELETE", "/users/@me/identities/"+password, nil).Status)
}

func TestAdminCannotDisableTheirOnlyLogin(t *testing.T) {
	e := testkit.New(t)
	enableProvider(t, e, "discord")
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

func TestPasswordChangeRevokesPersonalTokens(t *testing.T) {
	e := testkit.New(t)
	admin := e.Admin(t)
	mia := e.User(t, "mia")
	mint := func() testkit.ReqOpt {
		return testkit.Bearer(mia.Do(t, "POST", "/users/@me/tokens", J{"name": "cli"}).Get("token").String())
	}

	stolen := mint()
	require.Equal(t, 204, mia.Do(t, "PUT", "/users/@me/password", J{"current_password": testkit.Password, "new_password": "a brand new password"}).Status)
	require.Equal(t, 401, e.Do(t, "GET", "/users/@me", nil, stolen).Status, "a token made before the change must stop working")
	require.Equal(t, int64(0), mia.Do(t, "GET", "/users/@me/tokens", nil).Get("#").Int())

	again := mint()
	require.Equal(t, 204, admin.Do(t, "PUT", "/admin/users/"+mia.UserID+"/password", J{"new_password": "reset by the admin"}).Status)
	require.Equal(t, 401, e.Do(t, "GET", "/users/@me", nil, again).Status)
}

func TestMovingAnItemIntoAListBeingDeletedNeverFails(t *testing.T) {
	u := testkit.New(t).User(t, "mia")
	for round := 0; round < 25; round++ {
		list := u.Do(t, "POST", "/users/@me/lists", J{"name": "tmp"}).Get("id").String()
		item := u.Do(t, "POST", "/users/@me/items", J{"title": "x", "list_id": list}).Get("id").String()
		var patch, del int
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			patch = u.Do(t, "PATCH", "/users/@me/items/"+item, J{"list_id": list, "title": "y"}, testkit.NoContract()).Status
		}()
		go func() {
			defer wg.Done()
			del = u.Do(t, "DELETE", "/users/@me/lists/"+list, nil, testkit.NoContract()).Status
		}()
		wg.Wait()
		require.Equal(t, 204, del, "round %d", round)
		require.Contains(t, []int{200, 422}, patch, "round %d", round)
	}
}

func TestIdentitiesNeverOutliveTheirProvider(t *testing.T) {
	e := testkit.New(t)
	admin := e.Admin(t)
	e.Deps.RateAnon = ratelimit.New(1000, time.Minute, e.Clock.Now)
	ctx := context.Background()
	for round := 0; round < 12; round++ {
		key := fmt.Sprintf("idp%d", round)
		oidcProvider(t, e, key)
		path, opts := e.OAuthCallback(t, e.OAuthStart(t, key, "intent=login"))
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			e.Do(t, "GET", path, nil, append(opts, testkit.NoContract())...)
		}()
		go func() {
			defer wg.Done()
			admin.Do(t, "DELETE", "/admin/providers/"+key, nil, testkit.NoContract())
		}()
		wg.Wait()
		var orphans int
		require.NoError(t, e.Pool.QueryRow(ctx,
			"SELECT count(*) FROM identities i WHERE NOT EXISTS (SELECT 1 FROM login_providers p WHERE p.key = i.provider)").Scan(&orphans))
		require.Zero(t, orphans, "round %d left an identity for a deleted provider", round)
	}
}
