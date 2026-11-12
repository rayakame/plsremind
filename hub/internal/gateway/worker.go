package gateway

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
)

// mailbox collects what a session still has to do. Work for the same user is
// merged, so a burst of changes turns into one look at the event log.
type mailbox struct {
	mu      sync.Mutex
	start   bool
	dirty   map[uuid.UUID]struct{}
	recheck map[uuid.UUID]struct{}
	all     bool
	wake    chan struct{}
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
	start   bool
	all     bool
	dirty   []uuid.UUID
	recheck []uuid.UUID
}

func (m *mailbox) take() work {
	m.mu.Lock()
	defer m.mu.Unlock()
	w := work{start: m.start, all: m.all}
	for id := range m.dirty {
		w.dirty = append(w.dirty, id)
	}
	for id := range m.recheck {
		w.recheck = append(w.recheck, id)
	}
	m.start, m.all = false, false
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
			s.hub.drop(s, CloseTimeout, "internal error")
			cancel()
			return
		}
		cancel()
	}
}

func (s *Session) do(ctx context.Context, w work) error {
	if w.start {
		for _, userID := range s.initial {
			if err := s.syncUser(ctx, userID); err != nil {
				return err
			}
		}
		s.initial = nil
	}
	if w.all {
		for userID := range s.cursors {
			w.dirty = append(w.dirty, userID)
		}
	}
	for _, userID := range w.dirty {
		if err := s.pump(ctx, userID); err != nil {
			return err
		}
	}
	return nil
}
