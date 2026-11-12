package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/rayakame/plsremind/hub/internal/api"
	"github.com/rayakame/plsremind/hub/internal/auth"
	"github.com/rayakame/plsremind/hub/internal/core"
)

// Hub keeps every gateway session of this process.
type Hub struct {
	d *core.Deps

	mu       sync.Mutex
	sessions map[string]*Session
	// byUser finds the sessions that follow a user.
	byUser map[uuid.UUID]map[*Session]struct{}
	// listening is closed each time the signal connection is (re)established.
	listening chan struct{}
	// outbox holds new deliveries waiting to be rendered and sent.
	outbox chan outgoing

	cancel context.CancelFunc
	wg     sync.WaitGroup
}

func NewHub(d *core.Deps) *Hub {
	return &Hub{d: d, sessions: map[string]*Session{}, byUser: map[uuid.UUID]map[*Session]struct{}{}, listening: make(chan struct{}), outbox: make(chan outgoing, 4096)}
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
			c.finish(CloseRestart, "restarting")
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
		// Checked and removed under one lock, so a session cannot be resumed
		// in the moment it is being forgotten.
		now := time.Now()
		h.mu.Lock()
		var gone []*Session
		for _, s := range h.sessions {
			if s.expired(now) {
				h.forget(s)
				gone = append(gone, s)
			}
		}
		h.mu.Unlock()
		for _, s := range gone {
			s.halt()
		}
	}
}

// forget removes a session from the hub's books. The caller holds h.mu.
func (h *Hub) forget(s *Session) {
	delete(h.sessions, s.id)
	for userID, sessions := range h.byUser {
		delete(sessions, s)
		if len(sessions) == 0 {
			delete(h.byUser, userID)
		}
	}
}

// drop forgets a session for good. With a code, its connection is closed with it.
func (h *Hub) drop(s *Session, code int, reason string) {
	h.mu.Lock()
	h.forget(s)
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

// admit makes room for a new session of a credential, or refuses it. Only
// connected sessions count against the limit: a client that reconnects with
// Identify leaves a detached session behind each time, and those give way,
// oldest first.
func (h *Hub) admit(credential uuid.UUID) bool {
	h.mu.Lock()
	var attached int
	var detached []*Session
	for _, s := range h.sessions {
		if s.principal.CredentialID != credential {
			continue
		}
		if s.attached() {
			attached++
		} else {
			detached = append(detached, s)
		}
	}
	limit := h.d.Gateway.MaxSessions
	if attached >= limit {
		h.mu.Unlock()
		return false
	}
	slices.SortFunc(detached, func(a, b *Session) int { return a.createdAt.Compare(b.createdAt) })
	var evicted []*Session
	for attached+len(detached) >= limit && len(detached) > 0 {
		h.forget(detached[0])
		evicted = append(evicted, detached[0])
		detached = detached[1:]
	}
	h.mu.Unlock()
	for _, s := range evicted {
		s.halt()
	}
	return true
}

type verdict int

const (
	allowed verdict = iota
	// denied: these credentials do not work, and will not work on a retry.
	denied
	// trouble: the hub could not find out, usually because the database is away.
	trouble
)

// judge tells a refusal from a failure to check. Only a refusal may ever be
// reported to the client as "your credentials are no good".
func judge(found bool, err error) verdict {
	var refusal *api.Error
	switch {
	case errors.As(err, &refusal):
		return denied
	case err != nil:
		return trouble
	case !found:
		return denied
	}
	return allowed
}

// authenticate finds out who is on the other end: by token, or by the session
// cookie of the upgrade request.
func (h *Hub) authenticate(ctx context.Context, token, cookie string, sameOrigin bool) (auth.Principal, string, verdict) {
	switch {
	case token != "":
		p, err := auth.Authenticate(ctx, h.d, token)
		return p, "", judge(true, err)
	case sameOrigin:
		// A browser cannot put a token into the handshake, so the dashboard comes with
		// its session cookie. Only pages of this hub may use it that way.
		p, found, err := auth.SessionPrincipal(ctx, h.d, cookie)
		return p, cookie, judge(found, err)
	}
	return auth.Principal{}, "", denied
}

func refusal(v verdict) int {
	if v == trouble {
		return CloseInternal
	}
	return CloseAuthFailed
}

// resume attaches a connection to a session that lost its own. invalid means
// the session cannot be continued and the client has to identify afresh.
func (h *Hub) resume(ctx context.Context, c *conn, raw json.RawMessage, cookie string, sameOrigin bool) (s *Session, code int, invalid bool) {
	var in resume
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, CloseDecode, false
	}
	p, _, v := h.authenticate(ctx, in.Token, cookie, sameOrigin)
	if v != allowed {
		return nil, refusal(v), false
	}
	h.mu.Lock()
	s = h.sessions[in.SessionID]
	h.mu.Unlock()
	// Someone else's session is as good as none.
	if s == nil || s.principal.CredentialID != p.CredentialID {
		return nil, 0, true
	}

	// Holding the send lock keeps new events behind the replay.
	s.sendMu.Lock()
	defer s.sendMu.Unlock()

	// The session is looked up again and attached under the hub's lock, so it
	// cannot be reaped in between and left attached but forgotten.
	h.mu.Lock()
	if h.sessions[in.SessionID] != s {
		h.mu.Unlock()
		return nil, 0, true
	}
	s.mu.Lock()
	if in.Seq > s.seq {
		s.mu.Unlock()
		h.mu.Unlock()
		return nil, CloseInvalidSeq, false
	}
	// Everything after in.Seq must still be in the buffer.
	oldest := s.seq + 1
	if len(s.buffer) > 0 {
		oldest = s.buffer[0].seq
	}
	if in.Seq < oldest-1 {
		s.mu.Unlock()
		h.forget(s)
		h.mu.Unlock()
		s.halt()
		return nil, 0, true
	}
	previous := s.conn
	s.conn = c
	var missed [][]byte
	for _, frame := range s.buffer {
		if frame.seq > in.Seq {
			missed = append(missed, frame.frame)
		}
	}
	s.mu.Unlock()
	h.mu.Unlock()

	if previous != nil {
		previous.close(CloseAlreadyAuthenticated, "resumed elsewhere")
	}
	for _, frame := range missed {
		c.send(frame)
	}
	s.box.post(func(m *mailbox) { m.resumed, m.validate = true, true })
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
	p, cookie, v := h.authenticate(ctx, in.Token, cookie, sameOrigin)
	if v != allowed {
		return nil, refusal(v)
	}
	for _, intent := range intents {
		if !auth.HasIntent(p, intent) {
			return nil, CloseDisallowedIntents
		}
	}
	if !h.admit(p.CredentialID) {
		return nil, CloseRateLimited
	}

	id, _ := core.NewToken("")
	s := &Session{
		id: id, hub: h, principal: p, intents: intents, token: in.Token, cookie: cookie, conn: c, createdAt: time.Now(),
		box: newMailbox(), stop: make(chan struct{}), cursors: map[uuid.UUID]int64{},
	}
	// Registered before the list of users is read: a grant that changes from
	// here on finds the session and is looked at again.
	h.mu.Lock()
	h.sessions[s.id] = s
	h.mu.Unlock()
	welcome, err := h.ready(ctx, s)
	if err != nil {
		h.drop(s, 0, "")
		return nil, CloseInternal
	}
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
