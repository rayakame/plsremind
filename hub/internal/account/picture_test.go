package account_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/rayakame/plsremind/hub/internal/testkit"
)

func identityOf(t *testing.T, c *testkit.Client, provider string) gjson.Result {
	t.Helper()
	all := c.Do(t, "GET", "/users/@me/identities", nil)
	require.Equal(t, 200, all.Status, string(all.Body))
	one := all.Get(`#(provider=="` + provider + `")`)
	require.True(t, one.Exists(), string(all.Body))
	return one
}

func pictureOf(t *testing.T, c *testkit.Client, identity gjson.Result, opts ...testkit.ReqOpt) *testkit.Resp {
	t.Helper()
	return c.Do(t, "GET", "/users/@me/identities/"+identity.Get("id").String()+"/avatar", nil, opts...)
}

func linkingOIDC(t *testing.T, e *testkit.Env) *testkit.FakeOAuth {
	t.Helper()
	fake := testkit.NewFakeOAuth(t, "oidc")
	put := e.Admin(t).Do(t, "PUT", "/admin/providers/authentik", J{
		"type": "oidc", "name": "Authentik", "issuer_url": fake.IssuerURL,
		"client_id": "cid", "client_secret": "sec", "login_enabled": true, "link_enabled": true,
	})
	require.Equal(t, 200, put.Status, string(put.Body))
	return fake
}

func TestLinkingDiscordStoresHandleAndPicture(t *testing.T) {
	e := testkit.New(t)
	fake := testkit.EnableProvider(t, e, "discord")
	fake.User = testkit.OAuthUser{ID: "1234", Name: "Alex", Username: "alexb", Avatar: "abc"}
	mia := e.User(t, "mia")

	require.Empty(t, mia.OAuthLink(t, "discord").AuthError())
	discord := identityOf(t, mia, "discord")
	require.Equal(t, "alexb", discord.Get("username").String())
	require.True(t, discord.Get("has_avatar").Bool())
	require.Equal(t, []string{"/avatars/1234/abc.png"}, fake.ImagePaths())

	picture := pictureOf(t, mia, discord)
	require.Equal(t, 200, picture.Status)
	require.Equal(t, "image/png", picture.Header.Get("Content-Type"))
	require.Equal(t, testkit.TinyPNG, picture.Body)
	require.Contains(t, picture.Header.Get("Cache-Control"), "private")
	tag := picture.Header.Get("ETag")
	require.NotEmpty(t, tag)
	require.Equal(t, 304, pictureOf(t, mia, discord, testkit.Header("If-None-Match", tag)).Status)
}

func TestADiscordUserWithoutPictureHasNone(t *testing.T) {
	e := testkit.New(t)
	fake := testkit.EnableProvider(t, e, "discord")
	fake.User = testkit.OAuthUser{ID: "1234", Name: "Alex", Username: "alexb"}
	mia := e.User(t, "mia")

	require.Empty(t, mia.OAuthLink(t, "discord").AuthError())
	discord := identityOf(t, mia, "discord")
	require.False(t, discord.Get("has_avatar").Bool())
	require.Empty(t, fake.ImagePaths())
	require.Equal(t, 404, pictureOf(t, mia, discord).Status)
}

func TestGitHubHandleAndPicture(t *testing.T) {
	e := testkit.New(t)
	fake := testkit.EnableProvider(t, e, "github")
	fake.User = testkit.OAuthUser{ID: "77", Name: "Octo Mia", Username: "octomia", Avatar: "u/77"}
	mia := e.User(t, "mia")

	require.Empty(t, mia.OAuthLink(t, "github").AuthError())
	github := identityOf(t, mia, "github")
	require.Equal(t, "octomia", github.Get("username").String())
	require.True(t, github.Get("has_avatar").Bool())
	require.Equal(t, []string{"/avatars/u/77"}, fake.ImagePaths())
}

func TestOIDCPreferredUsernameAndPicture(t *testing.T) {
	e := testkit.New(t)
	fake := linkingOIDC(t, e)
	fake.User = testkit.OAuthUser{ID: "sub-42", Name: "Mia Musterfrau", Username: "mia", Avatar: "mia.png"}
	mia := e.User(t, "mia")

	require.Empty(t, mia.OAuthLink(t, "authentik").AuthError())
	oidc := identityOf(t, mia, "authentik")
	require.Equal(t, "mia", oidc.Get("username").String())
	require.True(t, oidc.Get("has_avatar").Bool())
	require.Equal(t, testkit.TinyPNG, pictureOf(t, mia, oidc).Body)
}

func TestPicturesThatAreRefusedLeaveTheLinkWorking(t *testing.T) {
	other := testkit.NewFakeOAuth(t, "oidc")
	cases := map[string]func(fake *testkit.FakeOAuth){
		"another origin over http": func(fake *testkit.FakeOAuth) { fake.User.Picture = other.IssuerURL + "/avatars/x.png" },
		"no picture at all":        func(fake *testkit.FakeOAuth) { fake.User.Avatar = "" },
		"a page, not a picture":    func(fake *testkit.FakeOAuth) { fake.ImageType = "text/html" },
		"too large":                func(fake *testkit.FakeOAuth) { fake.Image = make([]byte, 1<<20+1) },
		"an error":                 func(fake *testkit.FakeOAuth) { fake.ImageStatus = http.StatusInternalServerError },
		"a redirect":               func(fake *testkit.FakeOAuth) { fake.ImageStatus = http.StatusFound },
	}
	for name, spoil := range cases {
		t.Run(name, func(t *testing.T) {
			e := testkit.New(t)
			fake := linkingOIDC(t, e)
			fake.User = testkit.OAuthUser{ID: "sub-42", Name: "Mia", Avatar: "mia.png"}
			spoil(fake)
			mia := e.User(t, "mia")

			require.Empty(t, mia.OAuthLink(t, "authentik").AuthError())
			oidc := identityOf(t, mia, "authentik")
			require.False(t, oidc.Get("has_avatar").Bool())
			require.Equal(t, 404, pictureOf(t, mia, oidc).Status)
		})
	}
}

func TestRefreshKeepsAPictureWhenFetchingFails(t *testing.T) {
	e := testkit.New(t)
	fake := testkit.EnableProvider(t, e, "discord")
	fake.User = testkit.OAuthUser{ID: "1234", Name: "Alex", Username: "alexb", Avatar: "abc"}
	mia := e.User(t, "mia")
	require.Empty(t, mia.OAuthLink(t, "discord").AuthError())

	fake.ImageStatus = http.StatusServiceUnavailable
	fake.User.Username = "alexb2"
	require.Empty(t, mia.OAuthLink(t, "discord").AuthError())
	discord := identityOf(t, mia, "discord")
	require.Equal(t, "alexb2", discord.Get("username").String())
	require.True(t, discord.Get("has_avatar").Bool())
	require.Equal(t, testkit.TinyPNG, pictureOf(t, mia, discord).Body)
}

func TestRefreshClearsAPictureTheProviderNoLongerHas(t *testing.T) {
	e := testkit.New(t)
	fake := testkit.EnableProvider(t, e, "discord")
	fake.User = testkit.OAuthUser{ID: "1234", Name: "Alex", Username: "alexb", Avatar: "abc"}
	require.Empty(t, e.OAuthLogin(t, "discord").AuthError())

	fake.User.Avatar = ""
	login := e.OAuthLogin(t, "discord")
	require.Empty(t, login.AuthError())
	ids := e.Do(t, "GET", "/users/@me/identities", nil, sessionOf(login))
	require.False(t, ids.Get(`#(provider=="discord").has_avatar`).Bool())
}

func TestAPictureIsOnlyItsOwnersAndGoesWithTheAccount(t *testing.T) {
	e := testkit.New(t)
	fake := testkit.EnableProvider(t, e, "github")
	fake.User = testkit.OAuthUser{ID: "77", Name: "Octo Mia", Username: "octomia", Avatar: "u/77"}
	mia := e.User(t, "mia")
	require.Empty(t, mia.OAuthLink(t, "github").AuthError())
	github := identityOf(t, mia, "github")

	tom := e.User(t, "tom")
	require.Equal(t, 404, pictureOf(t, tom, github).Status)

	require.Equal(t, 204, mia.Do(t, "DELETE", "/users/@me/identities/"+github.Get("id").String(), nil).Status)
	require.Equal(t, 404, pictureOf(t, mia, github).Status)
}

func TestAnOldIdentityHasNoHandleAndNoPicture(t *testing.T) {
	e := testkit.New(t)
	mia := testkit.Linked(t, e, "mia", "1234")
	discord := identityOf(t, mia, "discord")
	require.Equal(t, "", discord.Get("username").String())
	require.True(t, discord.Get("has_avatar").Exists())
	require.False(t, discord.Get("has_avatar").Bool())
}
