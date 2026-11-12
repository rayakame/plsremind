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
	// byUser finds the sessions that follow a user.
	byUser map[uuid.UUID]map[*Session]struct{}
	// listening is closed each time the signal connection is (re)established.
	listening chan struct{}

	cancel context.CancelFunc
	wg     sync.WaitGroup
}

func NewHub(d *deps.Deps) *Hub {
	return &Hub{d: d, sessions: map[string]*Session{}, byUser: map[uuid.UUID]map[*Session]struct{}{}, listening: make(chan struct{})}
}

// Start runs the hub's background work until Shutdown.
func (h *Hub) Start(ctx context.Context) {
	ctx, h.cancel = context.WithCancel(ctx)
	ready := h.listening
	h.wg.Add(2)
	go func() {
		defer h.wg.Done()
		h.reap(ctx)
	}()
	go func() {
		defer h.wg.Done()
		h.listen(ctx)
	}()
	// Return only once signals are being received, so nothing sent after Start is missed.
	select {
	case <-ready:
	case <-time.After(5 * time.Second):
	case <-ctx.Done():
	}
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
	h.byUser = map[uuid.UUID]map[*Session]struct{}{}
	h.mu.Unlock()
	for _, s := range sessions {
		s.halt()
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
		var gone []*Session
		for _, s := range h.sessions {
			if s.expired(now) {
				gone = append(gone, s)
			}
		}
		h.mu.Unlock()
		for _, s := range gone {
			h.drop(s, 0, "")
		}
	}
}

// drop forgets a session for good. With a code, its connection is closed with it.
func (h *Hub) drop(s *Session, code int, reason string) {
	h.mu.Lock()
	delete(h.sessions, s.id)
	for userID, sessions := range h.byUser {
		delete(sessions, s)
		if len(sessions) == 0 {
			delete(h.byUser, userID)
		}
	}
	h.mu.Unlock()
	s.halt()
	if code != 0 {
		s.end(code, reason)
	}
}

// cover records that a session follows a user.
func (h *Hub) cover(s *Session, userID uuid.UUID) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, alive := h.sessions[s.id]; !alive {
		return
	}
	if h.byUser[userID] == nil {
		h.byUser[userID] = map[*Session]struct{}{}
	}
	h.byUser[userID][s] = struct{}{}
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
	s := &Session{
		id: id, hub: h, principal: p, intents: intents, token: in.Token, cookie: cookie, conn: c,
		box: newMailbox(), stop: make(chan struct{}), cursors: map[uuid.UUID]int64{},
	}
	welcome, err := h.ready(ctx, s)
	if err != nil {
		return nil, CloseAuthFailed
	}
	h.mu.Lock()
	h.sessions[s.id] = s
	h.mu.Unlock()
	s.emit(EventReady, true, welcome)
	go s.run()
	s.box.post(func(m *mailbox) { m.start = true })
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
		if s.keepsState() {
			s.initial = []uuid.UUID{s.principal.UserID}
		}
	}
	for _, userID := range s.initial {
		out.Users = append(out.Users, userID.String())
	}
	return out, nil
}
