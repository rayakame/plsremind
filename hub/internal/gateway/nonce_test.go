package gateway_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/rayakame/plsremind/hub/internal/gateway"
	"github.com/rayakame/plsremind/hub/internal/testkit"
)

func nonce(value string) testkit.ReqOpt {
	return testkit.Header("X-Request-Nonce", value)
}

func TestNonceComesBackInEveryEventOfTheRequest(t *testing.T) {
	e := testkit.New(t)
	mia := e.User(t, "mia")
	list := mia.Do(t, "POST", "/users/@me/lists", J{"name": "Uni"}).Get("id").String()
	mia.Do(t, "POST", "/users/@me/items", J{"title": "a", "list_id": list})
	mia.Do(t, "POST", "/users/@me/items", J{"title": "b", "list_id": list})
	gw := e.Gateway(t, personalToken(t, mia), "lists", "items")
	gw.Expect(t, "user.sync")

	mia.Do(t, "POST", "/users/@me/items", J{"title": "c"}, nonce("n-1"))
	require.Equal(t, "n-1", gw.Expect(t, "item.created").Get("nonce").String())

	// Deleting the list moves its two items: three events, one nonce.
	mia.Do(t, "DELETE", "/users/@me/lists/"+list, nil, nonce("n-2"))
	frames := gw.Collect(300 * time.Millisecond)
	require.Len(t, frames, 3)
	for _, f := range frames {
		require.Equal(t, "n-2", f.Get("nonce").String(), f.T)
	}

	mia.Do(t, "POST", "/users/@me/items", J{"title": "d"})
	created := gw.Expect(t, "item.created")
	require.True(t, created.Get("nonce").Exists())
	require.Equal(t, gjson.Null, created.Get("nonce").Type)
}

func TestNonceReachesOtherSessionsToo(t *testing.T) {
	e := testkit.New(t)
	bot := e.Connection(t, "discord")
	mia := testkit.Linked(t, e, "mia", "111")
	own := e.Gateway(t, personalToken(t, mia), "items")
	own.Expect(t, "user.sync")
	gw := e.Gateway(t, bot.Token, "items")
	gw.Expect(t, "user.sync")

	mia.Do(t, "POST", "/users/@me/items", J{"title": "x"}, nonce("shared"))
	require.Equal(t, "shared", own.Expect(t, "item.created").Get("nonce").String())
	require.Equal(t, "shared", gw.Expect(t, "item.created").Get("nonce").String())
}

func TestNonceSurvivesResume(t *testing.T) {
	e := testkit.New(t)
	mia := e.User(t, "mia")
	token := personalToken(t, mia)
	gw := e.Gateway(t, token, "items")
	gw.Expect(t, "user.sync")
	gw.Close()

	mia.Do(t, "POST", "/users/@me/items", J{"title": "missed"}, nonce("while-away"))
	back := e.ResumeGateway(t, token, gw.SessionID, gw.LastSeq)
	require.Equal(t, "while-away", back.Expect(t, "item.created").Get("nonce").String())
	back.Expect(t, gateway.EventResumed)
}

func TestProfileChangeCarriesNonceOnMovedItems(t *testing.T) {
	e := testkit.New(t)
	mia := e.User(t, "mia")
	mia.Do(t, "POST", "/users/@me/items", J{"title": "all day", "due_date": "2026-11-20"})
	gw := e.Gateway(t, personalToken(t, mia), "users", "items")
	gw.Expect(t, "user.sync")

	mia.Do(t, "PATCH", "/users/@me", J{"timezone": "Europe/Berlin"}, nonce("tz"))
	require.Equal(t, "tz", gw.Expect(t, "user.updated").Get("nonce").String())
	require.Equal(t, "tz", gw.Expect(t, "item.updated").Get("nonce").String())
}

func TestInvalidNonceIsRefused(t *testing.T) {
	e := testkit.New(t)
	mia := e.User(t, "mia")
	for _, bad := range []string{strings.Repeat("a", 65), "two words", "schön"} {
		r := mia.Do(t, "POST", "/users/@me/items", J{"title": "x"}, nonce(bad), testkit.NoContract())
		require.Equal(t, 400, r.Status, bad)
		require.Equal(t, "invalid_nonce", r.Get("error.code").String())
	}
	require.Equal(t, int64(0), mia.Do(t, "GET", "/users/@me/items", nil).Get("data.#").Int(), "nothing was created")

	r := mia.Do(t, "POST", "/users/@me/items", J{"title": "x"}, nonce(strings.Repeat("a", 64)))
	require.Equal(t, 201, r.Status)
}
