package testkit

import (
	"testing"

	"github.com/stretchr/testify/require"
)

const Password = "correct horse battery"

// Client sends requests as one authenticated caller.
type Client struct {
	env     *Env
	opts    []ReqOpt
	UserID  string
	Session string
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
