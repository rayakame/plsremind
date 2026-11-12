package testkit

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/rayakame/plsremind/hub/internal/core"
)

// DueSoon creates a low-priority item that is due in one hour and lets that hour pass.
func DueSoon(t *testing.T, e *Env, u *Client) string {
	t.Helper()
	due := e.Clock.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	r := u.Do(t, "POST", "/users/@me/items", J{"title": "Abgabe", "priority": "low", "due_at": due})
	require.Equal(t, 201, r.Status)
	e.Clock.Advance(time.Hour)
	e.Fire(t)
	return r.Get("id").String()
}

// Reminded returns a user who is reminded through one personal token.
func Reminded(t *testing.T, e *Env, name string) (*Client, ReqOpt) {
	t.Helper()
	u := e.User(t, name)
	tok := u.Do(t, "POST", "/users/@me/tokens", J{"name": "homelab", "remind": true})
	require.Equal(t, 201, tok.Status)
	return u, Bearer(tok.Get("token").String())
}

// EnableProvider configures GitHub or Discord against a fake and opens registration.
func EnableProvider(t *testing.T, e *Env, kind string) *FakeOAuth {
	t.Helper()
	fake := NewFakeOAuth(t, kind)
	if e.Deps.OAuthEndpoints == nil {
		e.Deps.OAuthEndpoints = map[string]core.OAuthEndpoints{}
	}
	e.Deps.OAuthEndpoints[kind] = fake.Endpoints
	admin := e.Admin(t)
	r := admin.Do(t, "PUT", "/admin/providers/"+kind, J{"client_id": "cid", "client_secret": "sec", "login_enabled": true, "link_enabled": true})
	require.Equal(t, 200, r.Status, string(r.Body))
	require.Equal(t, 200, admin.Do(t, "PATCH", "/admin/settings", J{"registration": "open"}).Status)
	return fake
}

// Linked returns a user with an account at the Discord bot's provider.
func Linked(t *testing.T, e *Env, name, externalID string) *Client {
	t.Helper()
	u := e.User(t, name)
	e.GiveIdentity(t, u.UserID, "discord", externalID)
	return u
}
