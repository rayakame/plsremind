package gateway

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/rayakame/plsremind/hub/internal/auth"
	"github.com/rayakame/plsremind/hub/internal/deps"
	"github.com/rayakame/plsremind/hub/internal/secret"
)

// Hub keeps every gateway session of this process.
type Hub struct {
	d *deps.Deps

	mu       sync.Mutex
	sessions map[string]*Session

	cancel context.CancelFunc
	wg     sync.WaitGroup
}

func NewHub(d *deps.Deps) *Hub {
	return &Hub{d: d, sessions: map[string]*Session{}}
}

// Start runs the hub's background work until Shutdown.
func (h *Hub) Start(ctx context.Context) {
	ctx, h.cancel = context.WithCancel(ctx)
	h.wg.Add(1)
	go func() {
		defer h.wg.Done()
		h.reap(ctx)
	}()
}

// Shutdown asks every client to reconnect and forgets all sessions.
func (h *Hub) Shutdown() {
	if h.cancel != nil {
		h.cancel()
		h.wg.Wait()
		h.cancel = nil
	}
	h.mu.Lock()
	sessions := h.sessions
	h.sessions = map[string]*Session{}
	h.mu.Unlock()
	for _, s := range sessions {
		s.mu.Lock()
		c := s.conn
		s.mu.Unlock()
		if c != nil {
			c.send(encode(OpReconnect, nil, nil, nil))
		}
		s.end(1012, "restarting")
	}
}

// reap forgets sessions whose client did not come back in time.
func (h *Hub) reap(ctx context.Context) {
	for {
		interval := h.d.Gateway.SessionTTL / 4
		select {
		case <-ctx.Done():
			return
		case <-time.After(max(interval, 10*time.Millisecond)):
		}
		now := time.Now()
		h.mu.Lock()
		for id, s := range h.sessions {
			if s.expired(now) {
				delete(h.sessions, id)
			}
		}
		h.mu.Unlock()
	}
}

func (h *Hub) remove(s *Session) {
	h.mu.Lock()
	delete(h.sessions, s.id)
	h.mu.Unlock()
}

func (h *Hub) countFor(credential uuid.UUID) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, s := range h.sessions {
		if s.principal.CredentialID == credential {
			n++
		}
	}
	return n
}

// identify turns a connection into a session. It returns a close code on refusal.
func (h *Hub) identify(ctx context.Context, c *conn, raw json.RawMessage, cookie string, sameOrigin bool) (*Session, int) {
	var in identify
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, CloseDecode
	}
	intents, ok := auth.NormalizeIntents(in.Intents)
	if !ok {
		return nil, CloseInvalidIntents
	}

	var p auth.Principal
	switch {
	case in.Token != "":
		var err error
		if p, err = auth.Authenticate(ctx, h.d, in.Token); err != nil {
			return nil, CloseAuthFailed
		}
		cookie = ""
	case sameOrigin:
		// A browser cannot put a token into the handshake, so the dashboard comes with
		// its session cookie. Only pages of this hub may use it that way.
		var found bool
		var err error
		if p, found, err = auth.SessionPrincipal(ctx, h.d, cookie); err != nil || !found {
			return nil, CloseAuthFailed
		}
	default:
		return nil, CloseAuthFailed
	}
	for _, intent := range intents {
		if !auth.HasIntent(p, intent) {
			return nil, CloseDisallowedIntents
		}
	}
	if h.countFor(p.CredentialID) >= h.d.Gateway.MaxSessions {
		return nil, CloseRateLimited
	}

	id, _ := secret.NewToken("")
	s := &Session{id: id, hub: h, principal: p, intents: intents, token: in.Token, cookie: cookie, conn: c}
	welcome, err := h.ready(ctx, s)
	if err != nil {
		return nil, CloseAuthFailed
	}
	h.mu.Lock()
	h.sessions[s.id] = s
	h.mu.Unlock()
	s.emit(EventReady, true, welcome)
	return s, 0
}

func (h *Hub) ready(ctx context.Context, s *Session) (ready, error) {
	out := ready{SessionID: s.id, Intents: s.intents, Users: []string{}}
	switch s.principal.Kind {
	case auth.KindConnection:
		conn, err := h.d.Q().GetConnection(ctx, s.principal.CredentialID)
		if err != nil {
			return ready{}, err
		}
		out.Principal = readyPrincipal{Kind: "connection", Connection: &readyConnection{ID: conn.ID.String(), Name: conn.Name, Provider: conn.Provider}}
	default:
		kind := "session"
		if s.principal.Kind == auth.KindPersonalToken {
			kind = "personal_token"
		}
		userID := s.principal.UserID.String()
		out.Principal = readyPrincipal{Kind: kind, UserID: &userID}
	}
	return out, nil
}
