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
	for i := 0; i < 3; i++ {
		f := gw.NextEvent(t)
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

func TestNonceComesBackFromEveryKindOfWrite(t *testing.T) {
	e := testkit.New(t)
	bot := e.Connection(t, "discord")
	mia := testkit.Linked(t, e, "mia", "111")
	item := mia.Do(t, "POST", "/users/@me/items", J{"title": "x"}).Get("id").String()
	sub := mia.Do(t, "POST", "/users/@me/items/"+item+"/subitems", J{"title": "a"}).Get("id").String()
	other := mia.Do(t, "POST", "/users/@me/items/"+item+"/subitems", J{"title": "b"}).Get("id").String()
	uni := mia.Do(t, "POST", "/users/@me/lists", J{"name": "Uni"}).Get("id").String()
	inbox := mia.Do(t, "GET", "/users/@me/lists", nil).Get("0.id").String()
	gw := e.Gateway(t, personalToken(t, mia), "users", "lists", "items", "plans")
	gw.Expect(t, "user.sync")

	admin := e.Admin(t)
	writes := []struct {
		name   string
		do     func(opt testkit.ReqOpt) *testkit.Resp
		events []string
	}{
		{"subitems reordered", func(o testkit.ReqOpt) *testkit.Resp {
			return mia.Do(t, "PUT", "/users/@me/items/"+item+"/subitems/order", J{"ids": []string{other, sub}}, o)
		}, nil},
		{"subitem created", func(o testkit.ReqOpt) *testkit.Resp {
			return mia.Do(t, "POST", "/users/@me/items/"+item+"/subitems", J{"title": "c"}, o)
		}, []string{"subitem.created", "item.updated"}},
		{"subitem updated", func(o testkit.ReqOpt) *testkit.Resp {
			return mia.Do(t, "PATCH", "/users/@me/items/"+item+"/subitems/"+sub, J{"done": true}, o)
		}, []string{"subitem.updated", "item.updated"}},
		{"reminder created", func(o testkit.ReqOpt) *testkit.Resp {
			return mia.Do(t, "POST", "/users/@me/items/"+item+"/reminders", J{"in": "2h"}, o)
		}, []string{"reminder.created", "item.updated"}},
		{"plan set", func(o testkit.ReqOpt) *testkit.Resp {
			return mia.Do(t, "PUT", "/users/@me/reminder-plans/low", J{"before": []string{"2h"}, "follow_up": "never"}, o)
		}, []string{"plan.updated"}},
		{"plan reset", func(o testkit.ReqOpt) *testkit.Resp {
			return mia.Do(t, "DELETE", "/users/@me/reminder-plans/low", nil, o)
		}, []string{"plan.updated"}},
		{"lists reordered", func(o testkit.ReqOpt) *testkit.Resp {
			return mia.Do(t, "PUT", "/users/@me/lists/order", J{"ids": []string{uni, inbox}}, o)
		}, nil},
		{"grant changed", func(o testkit.ReqOpt) *testkit.Resp {
			return mia.Do(t, "PUT", "/users/@me/connections/"+bot.ID, J{"remind": false}, o)
		}, []string{"connection.updated"}},
		{"role changed by an admin", func(o testkit.ReqOpt) *testkit.Resp {
			return admin.Do(t, "PATCH", "/admin/users/"+mia.UserID, J{"role": "admin"}, o)
		}, []string{"user.updated"}},
	}
	for _, w := range writes {
		r := w.do(nonce(w.name[:1] + "-nonce"))
		require.Less(t, r.Status, 300, "%s: %s", w.name, r.Body)
		var got []string
		for _, f := range gw.Collect(time.Second) {
			require.Equal(t, w.name[:1]+"-nonce", f.Get("nonce").String(), "%s: %s", w.name, f.T)
			got = append(got, f.T)
		}
		require.NotEmpty(t, got, w.name)
		if w.events != nil {
			require.Equal(t, w.events, got, w.name)
		}
	}
}
