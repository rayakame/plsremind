package gateway

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/rayakame/plsremind/hub/internal/delivery"
)

// mailbox collects what a session still has to do. Work for the same user is
// merged, so a burst of changes turns into one look at the event log.
type mailbox struct {
	mu       sync.Mutex
	start    bool
	validate bool
	dirty    map[uuid.UUID]struct{}
	recheck  map[uuid.UUID]struct{}
	all      bool
	// resumed: the client came back; RESUMED and what waits for it are due.
	resumed bool
	// reconcile: grants may have changed without the session hearing of it.
	reconcile bool
	frames    []loose
	wake      chan struct{}
}

// loose is a dispatch outside the numbered sequence, such as a delivery.
type loose struct {
	event string
	data  any
	// delivery and epoch are set for a new delivery: which one, and the
	// session's epoch it was made for.
	delivery uuid.UUID
	epoch    int
}

func newMailbox() *mailbox {
	return &mailbox{dirty: map[uuid.UUID]struct{}{}, recheck: map[uuid.UUID]struct{}{}, wake: make(chan struct{}, 1)}
}

func (m *mailbox) post(fn func(m *mailbox)) {
	m.mu.Lock()
	fn(m)
	m.mu.Unlock()
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

type work struct {
	start     bool
	validate  bool
	all       bool
	resumed   bool
	reconcile bool
	frames    []loose
	dirty     []uuid.UUID
	recheck   []uuid.UUID
}

func (m *mailbox) take() work {
	m.mu.Lock()
	defer m.mu.Unlock()
	w := work{
		start: m.start, all: m.all, validate: m.validate, frames: m.frames,
		resumed: m.resumed, reconcile: m.reconcile,
	}
	for id := range m.dirty {
		w.dirty = append(w.dirty, id)
	}
	for id := range m.recheck {
		w.recheck = append(w.recheck, id)
	}
	m.start, m.all, m.validate, m.frames = false, false, false, nil
	m.resumed, m.reconcile = false, false
	m.dirty, m.recheck = map[uuid.UUID]struct{}{}, map[uuid.UUID]struct{}{}
	return w
}

const workTimeout = 30 * time.Second

// run is the session's own goroutine. Everything that produces numbered events
// for the session happens here, one thing after the other, which is what keeps
// them in order.
func (s *Session) run() {
	for {
		select {
		case <-s.stop:
			return
		case <-s.box.wake:
		}
		w := s.box.take()
		ctx, cancel := context.WithTimeout(context.Background(), workTimeout)
		if err := s.do(ctx, w); err != nil {
			slog.Error("gateway session work failed", "session", s.id, "err", err)
			// The client's picture may now be incomplete. Ending the session makes it start over.
			s.hub.drop(s, CloseInternal, "internal error")
			cancel()
			return
		}
		cancel()
	}
}

func (s *Session) do(ctx context.Context, w work) error {
	if w.validate {
		if code := s.revalidate(ctx); code != 0 {
			s.hub.drop(s, code, "access withdrawn")
			return nil
		}
	}
	if w.start {
		for _, userID := range s.initial {
			if err := s.syncUser(ctx, userID); err != nil {
				return err
			}
			// READY named the user, so the client waits for word about them.
			if _, following := s.cursors[userID]; !following {
				s.emit("user.gone", true, map[string]uuid.UUID{"user_id": userID})
			}
		}
		s.initial = nil
	}
	if w.reconcile {
		if err := s.reconcile(ctx); err != nil {
			return err
		}
	}
	if w.all {
		for userID := range s.cursors {
			w.dirty = append(w.dirty, userID)
		}
	}
	for _, userID := range w.recheck {
		if err := s.recheck(ctx, userID); err != nil {
			return err
		}
	}
	for _, userID := range w.dirty {
		if err := s.pump(ctx, userID); err != nil {
			return err
		}
	}
	if w.resumed {
		s.emit(EventResumed, true, struct{}{})
	}
	for _, f := range w.frames {
		if f.epoch != 0 {
			s.deliver(f.epoch, f.delivery, f.data)
			continue
		}
		s.emit(f.event, false, f.data)
	}
	if w.start || w.resumed {
		return s.sendPending(ctx)
	}
	return nil
}

// sendPending hands the session's channel everything that is still waiting for
// it. This is how a reminder reaches an adapter that was away when it fired.
func (s *Session) sendPending(ctx context.Context) error {
	if !s.wants("deliveries") || !s.attached() {
		return nil
	}
	epoch := s.currentEpoch()
	ch, ok, err := delivery.ChannelOf(ctx, s.hub.d.Q(), s.principal)
	if err != nil || !ok {
		return err
	}
	views, err := delivery.Pending(ctx, s.hub.d, ch, func(id uuid.UUID) bool {
		claimed, ok := s.claim(id)
		if ok && claimed != epoch {
			// The client resumed meanwhile; that login hands it out.
			s.unclaim(id, claimed)
			return false
		}
		return ok
	})
	if err != nil {
		return err
	}
	for _, view := range views {
		s.deliver(epoch, view.ID, envelope{UserID: view.User.ID, Data: view})
	}
	return nil
}
