package httpapi_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/rayakame/plsremind/hub/internal/ratelimit"
	"github.com/rayakame/plsremind/hub/internal/testkit"
)

func TestSetupCreatesAdminOnce(t *testing.T) {
	e := testkit.New(t)
	require.True(t, e.Do(t, "GET", "/info", nil).Get("setup_required").Bool())
	r := e.Do(t, "POST", "/setup", J{"username": "Root", "password": "correct horse battery"})
	require.Equal(t, 201, r.Status)
	require.Equal(t, "admin", r.Get("role").String())
	require.Equal(t, "root", r.Get("display_name").String())
	require.NotEmpty(t, r.Cookie("plsr_session"))
	require.False(t, e.Do(t, "GET", "/info", nil).Get("setup_required").Bool())
	again := e.Do(t, "POST", "/setup", J{"username": "x2x", "password": "correct horse battery"})
	require.Equal(t, 409, again.Status)
	require.Equal(t, "already_set_up", again.Get("error.code").String())
}

func TestInfoListsPasswordLogin(t *testing.T) {
	r := testkit.New(t).Do(t, "GET", "/info", nil)
	require.Equal(t, "invite", r.Get("registration").String())
	require.Equal(t, "password", r.Get("login_methods.0.key").String())
	require.Equal(t, int64(1), r.Get("login_methods.#").Int())
}

func TestMalformedJSONIs400(t *testing.T) {
	r := testkit.New(t).Do(t, "POST", "/setup", testkit.Raw("{"))
	require.Equal(t, 400, r.Status)
	require.Equal(t, "invalid_json", r.Get("error.code").String())
}

func TestSetupValidatesInput(t *testing.T) {
	r := testkit.New(t).Do(t, "POST", "/setup", J{"username": "a b", "password": "short"})
	require.Equal(t, 422, r.Status)
	require.Equal(t, "validation_failed", r.Get("error.code").String())
	require.True(t, r.Get("error.fields.username").Exists())
	require.True(t, r.Get("error.fields.password").Exists())
}

func TestLoginAndMe(t *testing.T) {
	e := testkit.New(t)
	admin := e.Admin(t)
	login := e.Do(t, "POST", "/auth/login", J{"username": "ROOT", "password": testkit.Password})
	require.Equal(t, 200, login.Status)
	cookie := login.Cookie("plsr_session")
	require.NotEmpty(t, cookie)

	me := e.Do(t, "GET", "/users/@me", nil, testkit.Cookie("plsr_session", cookie))
	require.Equal(t, 200, me.Status)
	require.Equal(t, admin.UserID, me.Get("id").String())
	require.Equal(t, "UTC", me.Get("timezone").String())
	require.Equal(t, "22:00", me.Get("quiet_hours.start").String())
	require.Equal(t, "08:00", me.Get("quiet_hours.end").String())

	require.Equal(t, 401, e.Do(t, "GET", "/users/@me", nil).Status)
}

func TestWrongPasswordIs401(t *testing.T) {
	e := testkit.New(t)
	e.Admin(t)
	wrong := e.Do(t, "POST", "/auth/login", J{"username": "root", "password": "not the password"})
	unknown := e.Do(t, "POST", "/auth/login", J{"username": "nobody", "password": "not the password"})
	require.Equal(t, 401, wrong.Status)
	require.Equal(t, "invalid_credentials", wrong.Get("error.code").String())
	require.Equal(t, wrong.Status, unknown.Status)
	require.JSONEq(t, string(wrong.Body), string(unknown.Body))
	require.Empty(t, wrong.Cookie("plsr_session"))
}

func TestLogoutEndsSession(t *testing.T) {
	e := testkit.New(t)
	admin := e.Admin(t)
	require.Equal(t, 204, admin.Do(t, "POST", "/auth/logout", nil).Status)
	require.Equal(t, 401, admin.Do(t, "GET", "/users/@me", nil).Status)
}

func TestSessionExpires(t *testing.T) {
	e := testkit.New(t)
	admin := e.Admin(t)
	e.Clock.Advance(29 * 24 * time.Hour)
	require.Equal(t, 200, admin.Do(t, "GET", "/users/@me", nil).Status)
	e.Clock.Advance(29 * 24 * time.Hour)
	require.Equal(t, 200, admin.Do(t, "GET", "/users/@me", nil).Status, "use extends the session")
	e.Clock.Advance(31 * 24 * time.Hour)
	require.Equal(t, 401, admin.Do(t, "GET", "/users/@me", nil).Status)
}

func TestPatchProfile(t *testing.T) {
	e := testkit.New(t)
	admin := e.Admin(t)
	r := admin.Do(t, "PATCH", "/users/@me", J{
		"display_name": "Mia",
		"timezone":     "Europe/Berlin",
		"quiet_hours":  J{"start": "23:00", "end": "07:00"},
	})
	require.Equal(t, 200, r.Status)
	require.Equal(t, "Mia", r.Get("display_name").String())
	require.Equal(t, "Europe/Berlin", r.Get("timezone").String())
	require.Equal(t, "23:00", r.Get("quiet_hours.start").String())
	require.Equal(t, "07:00", r.Get("quiet_hours.end").String())

	partial := admin.Do(t, "PATCH", "/users/@me", J{"display_name": "Mia B"})
	require.Equal(t, "Europe/Berlin", partial.Get("timezone").String())

	bad := admin.Do(t, "PATCH", "/users/@me", J{"timezone": "Mars/Base"})
	require.Equal(t, 422, bad.Status)
	require.True(t, bad.Get("error.fields.timezone").Exists())
	badHours := admin.Do(t, "PATCH", "/users/@me", J{"quiet_hours": J{"start": "25:00", "end": "07:00"}})
	require.Equal(t, 422, badHours.Status)
}

func TestCSRFOriginRequired(t *testing.T) {
	e := testkit.New(t)
	admin := e.Admin(t)
	r := admin.Do(t, "PATCH", "/users/@me", J{"display_name": "X"}, testkit.Header("Origin", "https://evil.example"))
	require.Equal(t, 403, r.Status)
	require.Equal(t, "csrf_failed", r.Get("error.code").String())
	require.Equal(t, "root", admin.Do(t, "GET", "/users/@me", nil).Get("display_name").String())
}

func TestForeignUserPathIs404(t *testing.T) {
	e := testkit.New(t)
	admin := e.Admin(t)
	require.Equal(t, 404, admin.Do(t, "GET", "/users/"+uuid.NewString(), nil).Status)
	require.Equal(t, 404, admin.Do(t, "GET", "/users/"+admin.UserID, nil).Status)
}

func TestNoPlaintextSecretsInDB(t *testing.T) {
	e := testkit.New(t)
	admin := e.Admin(t)
	ctx := context.Background()

	var hash string
	require.NoError(t, e.Pool.QueryRow(ctx, "SELECT password_hash FROM identities WHERE provider = 'password'").Scan(&hash))
	require.True(t, strings.HasPrefix(hash, "$argon2id$"))

	var tokenHash []byte
	require.NoError(t, e.Pool.QueryRow(ctx, "SELECT token_hash FROM sessions").Scan(&tokenHash))
	require.NotContains(t, string(tokenHash), admin.Session)
	require.Len(t, tokenHash, 32)
}

func TestLoginRejectsCrossSiteRequests(t *testing.T) {
	e := testkit.New(t)
	e.Admin(t)
	body := J{"username": "root", "password": testkit.Password}
	r := e.Do(t, "POST", "/auth/login", body, testkit.Header("Origin", "https://evil.example"))
	require.Equal(t, 403, r.Status)
	require.Equal(t, "csrf_failed", r.Get("error.code").String())
	require.Empty(t, r.Cookie("plsr_session"))
	for _, path := range []string{"/auth/register", "/setup", "/auth/logout"} {
		require.Equal(t, 403, e.Do(t, "POST", path, body, testkit.Header("Origin", "https://evil.example")).Status, path)
	}
	noOrigin := e.Do(t, "POST", "/auth/login", body, testkit.Header("Origin", ""))
	require.Equal(t, 200, noOrigin.Status, "tools that are not browsers send no Origin")
}

func TestLoginThrottleIgnoresCredentials(t *testing.T) {
	e := testkit.New(t)
	e.Admin(t)
	mia := e.User(t, "mia")
	tok := mia.Do(t, "POST", "/users/@me/tokens", J{"name": "cli"}).Get("token").String()
	e.Deps.RateAnon = ratelimit.New(2, time.Minute, e.Clock.Now)
	wrong := J{"username": "root", "password": "wrong wrong wrong"}
	require.Equal(t, 401, mia.Do(t, "POST", "/auth/login", wrong).Status)
	require.Equal(t, 401, e.Do(t, "POST", "/auth/login", wrong, testkit.Bearer(tok)).Status)
	require.Equal(t, 429, mia.Do(t, "POST", "/auth/login", wrong).Status)
	require.Equal(t, 429, e.Do(t, "POST", "/auth/register", J{"username": "x1x", "password": testkit.Password}, testkit.Bearer(tok)).Status)
	require.Equal(t, 200, mia.Do(t, "GET", "/users/@me", nil).Status, "other endpoints keep the credential's own budget")
}

func TestChangingPasswordEndsOtherSessions(t *testing.T) {
	e := testkit.New(t)
	mia := e.User(t, "mia")
	other := e.Do(t, "POST", "/auth/login", J{"username": "mia", "password": testkit.Password})
	otherSession := testkit.Cookie("plsr_session", other.Cookie("plsr_session"))
	require.Equal(t, 200, e.Do(t, "GET", "/users/@me", nil, otherSession).Status)

	changed := mia.Do(t, "PUT", "/users/@me/password", J{"current_password": testkit.Password, "new_password": "a brand new password"})
	require.Equal(t, 204, changed.Status)
	require.Equal(t, 401, e.Do(t, "GET", "/users/@me", nil, otherSession).Status)
	require.Equal(t, 200, mia.Do(t, "GET", "/users/@me", nil).Status, "the session that made the change stays")
}
