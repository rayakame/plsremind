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
	r := e.Do(t, "POST", "/setup", J{"username": "root", "password": Password})
	require.Equal(t, 201, r.Status, string(r.Body))
	return e.sessionClient(r)
}
