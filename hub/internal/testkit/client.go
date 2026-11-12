package testkit

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

const Password = "correct horse battery"

// Client sends requests as one authenticated caller.
type Client struct {
	env     *Env
	opts    []ReqOpt
	UserID  string
	Session string
	// ID and Token are the connection's, for clients made by Connection.
	ID    string
	Token string
}

func (c *Client) Do(t *testing.T, method, path string, body any, opts ...ReqOpt) *Resp {
	t.Helper()
	return c.env.Do(t, method, path, body, append(append([]ReqOpt{}, c.opts...), opts...)...)
}

func (e *Env) sessionClient(r *Resp) *Client {
	session := r.Cookie("plsr_session")
	return &Client{
		env:     e,
		opts:    []ReqOpt{Cookie("plsr_session", session)},
		UserID:  r.Get("id").String(),
		Session: session,
	}
}

// Admin sets the instance up and returns the first user, an admin named root.
func (e *Env) Admin(t *testing.T) *Client {
	t.Helper()
	if e.admin == nil {
		r := e.Do(t, "POST", "/setup", J{"username": "root", "password": Password})
		require.Equal(t, 201, r.Status, string(r.Body))
		e.admin = e.sessionClient(r)
	}
	return e.admin
}

// User registers a regular user through an invite.
func (e *Env) User(t *testing.T, username string) *Client {
	t.Helper()
	invite := e.Admin(t).Do(t, "POST", "/admin/invites", J{})
	require.Equal(t, 201, invite.Status, string(invite.Body))
	r := e.Do(t, "POST", "/auth/register", J{
		"username":    username,
		"password":    Password,
		"invite_code": invite.Get("code").String(),
	})
	require.Equal(t, 201, r.Status, string(r.Body))
	return e.sessionClient(r)
}

// Connection installs a connection for the given provider and returns a client using its token.
func (e *Env) Connection(t *testing.T, provider string) *Client {
	t.Helper()
	r := e.Admin(t).Do(t, "POST", "/admin/connections", J{"name": provider + " bot", "provider": provider})
	require.Equal(t, 201, r.Status, string(r.Body))
	token := r.Get("token").String()
	return &Client{env: e, opts: []ReqOpt{Bearer(token)}, ID: r.Get("id").String(), Token: token}
}

// GiveIdentity links an external account to a user without going through OAuth.
func (e *Env) GiveIdentity(t *testing.T, userID, provider, externalID string) {
	t.Helper()
	_, err := e.Pool.Exec(context.Background(),
		`INSERT INTO identities (id, user_id, provider, external_id, display_name, created_at)
		 VALUES ($1, $2, $3, $4, $4, $5)`,
		uuid.Must(uuid.NewV7()), userID, provider, externalID, e.Clock.Now())
	require.NoError(t, err)
}

// Strings collects the values a gjson path matches in a response.
func Strings(r *Resp, path string) []string {
	var out []string
	for _, v := range r.Get(path).Array() {
		out = append(out, v.String())
	}
	return out
}
