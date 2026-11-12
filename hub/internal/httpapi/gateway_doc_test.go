package httpapi_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/rayakame/plsremind/hub/internal/gateway"
	"github.com/rayakame/plsremind/hub/internal/testkit"
)

func TestGatewayDocListsEverything(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(testkit.SpecPath()), "..", "gateway.md"))
	require.NoError(t, err)
	doc := string(raw)
	for _, event := range gateway.Events {
		require.Contains(t, doc, "`"+event+"`", "event %s is not documented", event)
	}
	for code, name := range gateway.Opcodes {
		require.Contains(t, doc, fmt.Sprintf("| %d | %s |", code, name), "opcode %d is not documented", code)
	}
	for _, code := range gateway.CloseCodes {
		require.Contains(t, doc, fmt.Sprintf("| %d |", code), "close code %d is not documented", code)
	}
}

func TestOnlyDocumentedEventsAreSent(t *testing.T) {
	e := testkit.New(t)
	bot := e.Connection(t, "discord")
	mia := linked(t, e, "mia", "111")
	own := e.Gateway(t, personalToken(t, mia), "users", "lists", "items", "plans", "deliveries")
	gw := e.Gateway(t, bot.Token, "users", "lists", "items", "plans", "deliveries")

	list := mia.Do(t, "POST", "/users/@me/lists", J{"name": "Uni"}).Get("id").String()
	item := dueSoon(t, e, mia)
	sub := mia.Do(t, "POST", "/users/@me/items/"+item+"/subitems", J{"title": "a"}).Get("id").String()
	rem := mia.Do(t, "POST", "/users/@me/items/"+item+"/reminders", J{"in": "2h"}).Get("id").String()
	mia.Do(t, "PATCH", "/users/@me/items/"+item+"/subitems/"+sub, J{"done": true})
	mia.Do(t, "DELETE", "/users/@me/items/"+item+"/subitems/"+sub, nil)
	mia.Do(t, "DELETE", "/users/@me/items/"+item+"/reminders/"+rem, nil)
	mia.Do(t, "PATCH", "/users/@me/lists/"+list, J{"name": "Studium"})
	mia.Do(t, "DELETE", "/users/@me/lists/"+list, nil)
	mia.Do(t, "PATCH", "/users/@me", J{"display_name": "Mia"})
	mia.Do(t, "PUT", "/users/@me/reminder-plans/low", J{"before": []string{}, "follow_up": "never"})
	mia.Do(t, "PATCH", "/users/@me/items/"+item, J{"status": "done"})
	mia.Do(t, "DELETE", "/users/@me/items/"+item, nil)
	mia.Do(t, "PUT", "/users/@me/connections/"+bot.ID, J{"enabled": false})
	own.Drain(t)
	gw.Drain(t)

	seen := map[string]bool{}
	for _, f := range append(own.Seen, gw.Seen...) {
		require.Contains(t, gateway.Events, f.T)
		seen[f.T] = true
	}
	for _, event := range gateway.Events {
		if event != gateway.EventResumed {
			require.True(t, seen[event], "this walk through the API never produced %s", event)
		}
	}
}

func TestServerTimeoutsSpareTheGateway(t *testing.T) {
	e := testkit.NewWith(t, func(d *testkit.Deps) { d.ReadTimeout, d.IdleTimeout = 150*time.Millisecond, 150*time.Millisecond })
	gw := e.Gateway(t, personalToken(t, e.User(t, "mia")), "deliveries")
	time.Sleep(800 * time.Millisecond)
	gw.StopHeartbeat()
	gw.Send(t, gateway.OpHeartbeat, nil)
	require.Equal(t, gateway.OpHeartbeatACK, gw.Next(t).Op, "the connection outlived the server's request timeouts")
}
