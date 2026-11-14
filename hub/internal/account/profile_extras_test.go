package account_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/rayakame/plsremind/hub/internal/testkit"
)

func TestProfileCarriesLocaleAndPreferences(t *testing.T) {
	e := testkit.New(t)
	mia := e.User(t, "mia")
	me := mia.Do(t, "GET", "/users/@me", nil)
	require.Equal(t, gjson.Null, me.Get("locale").Type)
	require.JSONEq(t, `{}`, me.Get("preferences").Raw)

	got := mia.Do(t, "PATCH", "/users/@me", J{"locale": "de", "preferences": J{"theme": "dark", "keys": J{"done": "x"}}})
	require.Equal(t, 200, got.Status, string(got.Body))
	require.Equal(t, "de", got.Get("locale").String())
	require.JSONEq(t, `{"theme":"dark","keys":{"done":"x"}}`, got.Get("preferences").Raw)

	got = mia.Do(t, "PATCH", "/users/@me", J{"preferences": J{"theme": "light"}})
	require.JSONEq(t, `{"theme":"light"}`, got.Get("preferences").Raw, "preferences are replaced as a whole")
	require.Equal(t, "de", got.Get("locale").String())

	got = mia.Do(t, "PATCH", "/users/@me", J{"display_name": "Mia"})
	require.JSONEq(t, `{"theme":"light"}`, got.Get("preferences").Raw)
	require.Equal(t, "de", got.Get("locale").String())

	require.Equal(t, gjson.Null, mia.Do(t, "PATCH", "/users/@me", J{"locale": nil}).Get("locale").Type)
	require.Equal(t, "pt-BR", mia.Do(t, "PATCH", "/users/@me", J{"locale": "pt-BR"}).Get("locale").String())
	require.Equal(t, "pt-BR", mia.Do(t, "GET", "/users/@me", nil).Get("locale").String())
}

func TestProfileRejectsBadLocaleAndPreferences(t *testing.T) {
	e := testkit.New(t)
	mia := e.User(t, "mia")
	for _, bad := range []string{"", "d", "deutsch!!", "de_DE", "en-" + strings.Repeat("abcdefgh-", 4) + "x"} {
		r := mia.Do(t, "PATCH", "/users/@me", J{"locale": bad})
		require.Equal(t, 422, r.Status, bad)
		require.NotEmpty(t, r.Get("error.fields.locale").String(), bad)
	}
	for _, bad := range []string{`[]`, `1`, `"x"`, `null`} {
		r := mia.Do(t, "PATCH", "/users/@me", testkit.Raw(`{"preferences":`+bad+`}`))
		require.Equal(t, 422, r.Status, bad)
		require.NotEmpty(t, r.Get("error.fields.preferences").String(), bad)
	}

	sized := func(n int) string {
		// {"k":"aaa"} with the padding chosen so the object is exactly n bytes long.
		return `{"k":"` + strings.Repeat("a", n-len(`{"k":""}`)) + `"}`
	}
	fits := sized(16384)
	require.Len(t, fits, 16384)
	r := mia.Do(t, "PATCH", "/users/@me", testkit.Raw(`{"preferences":`+fits+`}`))
	require.Equal(t, 200, r.Status, string(r.Body))
	var kept map[string]string
	require.NoError(t, json.Unmarshal([]byte(r.Get("preferences").Raw), &kept))
	require.Len(t, kept["k"], 16384-len(`{"k":""}`))

	r = mia.Do(t, "PATCH", "/users/@me", testkit.Raw(`{"preferences":`+sized(16385)+`}`))
	require.Equal(t, 422, r.Status)
	require.NotEmpty(t, r.Get("error.fields.preferences").String())
}

func TestRegisterAndSetupTakeLocale(t *testing.T) {
	root := testkit.New(t).Do(t, "POST", "/setup", J{"username": "root", "password": testkit.Password, "locale": "en-GB"})
	require.Equal(t, 201, root.Status, string(root.Body))
	require.Equal(t, "en-GB", root.Get("locale").String())

	e := testkit.New(t)
	register := func(name string, locale any) *testkit.Resp {
		code := e.Admin(t).Do(t, "POST", "/admin/invites", J{}).Get("code").String()
		body := J{"username": name, "password": testkit.Password, "invite_code": code}
		if locale != nil {
			body["locale"] = locale
		}
		return e.Do(t, "POST", "/auth/register", body)
	}
	mia := register("mia", "de")
	require.Equal(t, 201, mia.Status, string(mia.Body))
	require.Equal(t, "de", mia.Get("locale").String())

	bad := register("ben", "not a locale")
	require.Equal(t, 422, bad.Status)
	require.NotEmpty(t, bad.Get("error.fields.locale").String())

	none := register("ben", nil)
	require.Equal(t, 201, none.Status, string(none.Body))
	require.Equal(t, gjson.Null, none.Get("locale").Type)
}

func TestPreferencesWithNumbersNoClientCanReadAreRefused(t *testing.T) {
	e := testkit.New(t)
	mia := e.User(t, "mia")
	for _, bad := range []string{`{"a":1e999999}`, `{"a":[{"b":-1e400}]}`} {
		r := mia.Do(t, "PATCH", "/users/@me", testkit.Raw(`{"preferences":`+bad+`}`))
		require.Equal(t, 422, r.Status, bad)
		require.NotEmpty(t, r.Get("error.fields.preferences").String(), bad)
	}
	require.JSONEq(t, `{}`, mia.Do(t, "GET", "/users/@me", nil).Get("preferences").Raw)
}

func TestAdminsDoNotSeePreferences(t *testing.T) {
	e := testkit.New(t)
	mia := e.User(t, "mia")
	require.Equal(t, 200, mia.Do(t, "PATCH", "/users/@me", J{"locale": "de", "preferences": J{"secret": "mine"}}).Status)

	list := e.Admin(t).Do(t, "GET", "/admin/users", nil)
	require.Equal(t, 200, list.Status)
	require.NotContains(t, string(list.Body), "mine")
	require.NotContains(t, string(list.Body), "preferences")
	require.Contains(t, string(list.Body), `"locale":"de"`)

	changed := e.Admin(t).Do(t, "PATCH", "/admin/users/"+mia.UserID, J{"role": "admin"})
	require.Equal(t, 200, changed.Status)
	require.False(t, changed.Get("preferences").Exists())
}

func TestLocaleAcceptsExtensionSubtags(t *testing.T) {
	e := testkit.New(t)
	mia := e.User(t, "mia")
	for _, tag := range []string{"en-u-ca-gregory", "de-x-private", "zh-Hant-TW", "de-DE-1996"} {
		require.Equal(t, tag, mia.Do(t, "PATCH", "/users/@me", J{"locale": tag}).Get("locale").String())
	}
	for _, bad := range []string{"-de", "de-", "de--DE", "abcdefghi", "de-abcdefghi"} {
		require.Equal(t, 422, mia.Do(t, "PATCH", "/users/@me", J{"locale": bad}).Status, bad)
	}
}

func TestPreferencesComeBackNoLargerThanTheyWereSent(t *testing.T) {
	e := testkit.New(t)
	mia := e.User(t, "mia")
	// Numbers that a database could write out digit by digit, three hundred each.
	sent := `{"a":[` + strings.TrimSuffix(strings.Repeat("1e300,", 2000), ",") + `],"b":"\ud800"}`
	r := mia.Do(t, "PATCH", "/users/@me", testkit.Raw(`{"preferences":`+sent+`}`))
	require.Equal(t, 200, r.Status)
	require.Less(t, len(r.Body), 2*len(sent))
	require.JSONEq(t, sent, r.Get("preferences").Raw)

	got := mia.Do(t, "GET", "/users/@me", nil)
	require.Less(t, len(got.Body), 2*len(sent))
	var size int
	require.NoError(t, e.Pool.QueryRow(context.Background(),
		`SELECT coalesce(max(pg_column_size(payload)), 0) FROM events WHERE user_id = $1`, mia.UserID).Scan(&size))
	require.Less(t, size, 4096, "the event log does not keep a copy of the preferences")
}
