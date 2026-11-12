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
	// outbox holds new deliveries waiting to be rendered and sent.
	outbox chan uuid.UUID

	cancel context.CancelFunc
	wg     sync.WaitGroup
}

func NewHub(d *deps.Deps) *Hub {
	return &Hub{d: d, sessions: map[string]*Session{}, byUser: map[uuid.UUID]map[*Session]struct{}{}, listening: make(chan struct{}), outbox: make(chan uuid.UUID, 4096)}
}

// Start runs the hub's background work until Shutdown.
func (h *Hub) Start(ctx context.Context) {
	ctx, h.cancel = context.WithCancel(ctx)
	ready := h.listening
	h.wg.Add(3)
	go func() {
		defer h.wg.Done()
		h.reap(ctx)
	}()
	go func() {
		defer h.wg.Done()
		h.deliver(ctx)
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
		s.conn = nil
		s.mu.Unlock()
		if c != nil {
			c.send(encode(OpReconnect, nil, nil, nil))
			c.finish(1012, "restarting")
		}
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

// uncover records that a session no longer follows a user.
func (h *Hub) uncover(s *Session, userID uuid.UUID) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.byUser[userID], s)
	if len(h.byUser[userID]) == 0 {
		delete(h.byUser, userID)
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

// authenticate finds out who is on the other end: by token, or by the session
// cookie of the upgrade request.
func (h *Hub) authenticate(ctx context.Context, token, cookie string, sameOrigin bool) (auth.Principal, string, bool) {
	switch {
	case token != "":
		p, err := auth.Authenticate(ctx, h.d, token)
		return p, "", err == nil
	case sameOrigin:
		// A browser cannot put a token into the handshake, so the dashboard comes with
		// its session cookie. Only pages of this hub may use it that way.
		p, found, err := auth.SessionPrincipal(ctx, h.d, cookie)
		return p, cookie, err == nil && found
	}
	return auth.Principal{}, "", false
}

// resume attaches a connection to a session that lost its own. invalid means
// the session cannot be continued and the client has to identify afresh.
func (h *Hub) resume(ctx context.Context, c *conn, raw json.RawMessage, cookie string, sameOrigin bool) (s *Session, code int, invalid bool) {
	var in resume
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, CloseDecode, false
	}
	p, _, ok := h.authenticate(ctx, in.Token, cookie, sameOrigin)
	if !ok {
		return nil, CloseAuthFailed, false
	}
	h.mu.Lock()
	s = h.sessions[in.SessionID]
	h.mu.Unlock()
	// Someone else's session is as good as none.
	if s == nil || s.principal.CredentialID != p.CredentialID {
		return nil, 0, true
	}

	s.mu.Lock()
	if in.Seq > s.seq {
		s.mu.Unlock()
		return nil, CloseInvalidSeq, false
	}
	// Everything after in.Seq must still be in the buffer.
	oldest := s.seq + 1
	if len(s.buffer) > 0 {
		oldest = s.buffer[0].seq
	}
	if in.Seq < oldest-1 {
		s.mu.Unlock()
		h.drop(s, 0, "")
		return nil, 0, true
	}
	previous := s.conn
	s.conn = c
	for _, missed := range s.buffer {
		if missed.seq > in.Seq {
			c.send(missed.frame)
		}
	}
	s.mu.Unlock()
	if previous != nil {
		previous.close(CloseAlreadyAuthenticated, "resumed elsewhere")
	}
	s.emit(EventResumed, true, struct{}{})
	s.box.post(func(m *mailbox) { m.pending, m.validate = true, true })
	return s, 0, false
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
	p, cookie, ok := h.authenticate(ctx, in.Token, cookie, sameOrigin)
	if !ok {
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
		if s.keepsState() {
			if s.initial, err = h.d.Q().ListGrantedUsers(ctx, conn.ID); err != nil {
				return ready{}, err
			}
		}
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
