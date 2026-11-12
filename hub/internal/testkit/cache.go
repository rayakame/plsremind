package testkit

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// UserState is what a client knows about one user from the gateway alone.
type UserState struct {
	User  map[string]any
	Lists map[string]any
	Items map[string]any
	Plans map[string]any
}

// Cache is the state a client would hold after applying every dispatch it received,
// the way an SDK cache does.
type Cache struct {
	Users map[string]*UserState
}

func decode(raw string) map[string]any {
	var out map[string]any
	_ = json.Unmarshal([]byte(raw), &out)
	return out
}

func (c *Cache) apply(f Frame) {
	userID := f.Get("user_id").String()
	state := c.Users[userID]
	data := f.Get("data")
	switch f.T {
	case "user.sync":
		if f.Get("chunk.index").Int() == 0 {
			state = &UserState{}
			c.Users[userID] = state
		}
		if user := f.Get("user"); user.Exists() {
			state.User = decode(user.Raw)
		}
		if lists := f.Get("lists"); lists.Exists() {
			state.Lists = map[string]any{}
			lists.ForEach(func(_, l gjson.Result) bool { state.Lists[l.Get("id").String()] = decode(l.Raw); return true })
		}
		if plans := f.Get("plans"); plans.Exists() {
			state.Plans = map[string]any{}
			plans.ForEach(func(_, p gjson.Result) bool { state.Plans[p.Get("priority").String()] = decode(p.Raw); return true })
		}
		if items := f.Get("items"); items.Exists() {
			if state.Items == nil {
				state.Items = map[string]any{}
			}
			items.ForEach(func(_, it gjson.Result) bool { state.Items[it.Get("id").String()] = decode(it.Raw); return true })
		}
		return
	case "user.gone":
		delete(c.Users, userID)
		return
	}
	if state == nil {
		return
	}
	id := data.Get("id").String()
	switch f.T {
	case "user.updated":
		state.User = decode(data.Raw)
	case "list.created", "list.updated":
		state.Lists[id] = decode(data.Raw)
	case "list.deleted":
		delete(state.Lists, id)
	case "item.created", "item.updated":
		// Only open items are kept, as in user.sync.
		if data.Get("status").String() == "done" {
			delete(state.Items, id)
		} else {
			state.Items[id] = decode(data.Raw)
		}
	case "item.deleted":
		delete(state.Items, id)
	case "plan.updated":
		state.Plans[data.Get("priority").String()] = decode(data.Raw)
	}
}

// Drain reads until the gateway has been silent for a moment.
func (g *GatewayClient) Drain(t *testing.T) {
	t.Helper()
	for {
		if _, ok := g.next(400*time.Millisecond, true); !ok {
			return
		}
	}
}

// Cache waits for the gateway to fall silent and returns the state built from everything received.
func (g *GatewayClient) Cache(t *testing.T) *Cache {
	t.Helper()
	g.Drain(t)
	c := &Cache{Users: map[string]*UserState{}}
	for _, f := range g.Seen {
		c.apply(f)
	}
	return c
}

func byKey(arr gjson.Result, key string) map[string]any {
	out := map[string]any{}
	arr.ForEach(func(_, v gjson.Result) bool { out[v.Get(key).String()] = decode(v.Raw); return true })
	return out
}

func requireSameJSON(t *testing.T, want, got any, what string) {
	t.Helper()
	w, _ := json.Marshal(want)
	g, _ := json.Marshal(got)
	require.JSONEq(t, string(w), string(g), "the cache's %s differ from what the API returns", what)
}

// RequireMatchesREST compares the cached state of a user with what the REST API
// returns under base, for every part the cache holds.
func (c *Cache) RequireMatchesREST(t *testing.T, client *Client, base, userID string) {
	t.Helper()
	state := c.Users[userID]
	require.NotNil(t, state, "the cache has no state for user %s", userID)
	if state.Lists != nil {
		r := client.Do(t, "GET", base+"/lists", nil)
		require.Equal(t, 200, r.Status)
		requireSameJSON(t, byKey(gjson.ParseBytes(r.Body), "id"), state.Lists, "lists")
	}
	if state.Items != nil {
		r := client.Do(t, "GET", base+"/items?status=open&limit=100", nil)
		require.Equal(t, 200, r.Status)
		require.False(t, r.Get("next_cursor").Exists() && r.Get("next_cursor").String() != "", "too many items for one page")
		requireSameJSON(t, byKey(r.Get("data"), "id"), state.Items, "items")
	}
	if state.Plans != nil {
		r := client.Do(t, "GET", base+"/reminder-plans", nil)
		require.Equal(t, 200, r.Status)
		requireSameJSON(t, byKey(gjson.ParseBytes(r.Body), "priority"), state.Plans, "plans")
	}
	if state.User != nil {
		r := client.Do(t, "GET", base, nil)
		require.Equal(t, 200, r.Status)
		// A connection's view carries extra fields, so only what the API returns is compared.
		for key, want := range decode(string(r.Body)) {
			requireSameJSON(t, want, state.User[key], "user."+key)
		}
	}
}
