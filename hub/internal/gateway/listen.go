package gateway

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/rayakame/plsremind/hub/internal/signal"
)

// listen receives the hub's signals from PostgreSQL for as long as ctx lives.
// If the connection breaks it reconnects, and since signals sent in between
// are gone, every session then looks at everything again.
func (h *Hub) listen(ctx context.Context) {
	for ctx.Err() == nil {
		err := h.listenOnce(ctx)
		if ctx.Err() != nil {
			return
		}
		slog.Error("gateway lost its signal connection, reconnecting", "err", err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
		}
	}
}

func (h *Hub) listenOnce(ctx context.Context) error {
	conn, err := h.d.Pool.Acquire(ctx)
	if err != nil {
		return err
	}
	// The connection may be mid-wait when we leave; closing it is cleaner than handing it back.
	defer func() { _ = conn.Conn().Close(context.Background()); conn.Release() }()
	if _, err := conn.Exec(ctx, "LISTEN "+signal.Channel); err != nil {
		return err
	}
	h.everything()
	close(h.listening)
	h.listening = make(chan struct{})
	for {
		n, err := conn.Conn().WaitForNotification(ctx)
		if err != nil {
			return err
		}
		var s signal.Signal
		if err := json.Unmarshal([]byte(n.Payload), &s); err != nil {
			slog.Error("gateway got a malformed signal", "payload", n.Payload)
			continue
		}
		h.handle(s)
	}
}

// everything makes all sessions catch up, after signals may have been missed.
func (h *Hub) everything() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, s := range h.sessions {
		s.box.post(func(m *mailbox) { m.all = true })
	}
}

func (h *Hub) handle(s signal.Signal) {
	switch s.Type {
	case signal.Event:
		if s.UserID != nil {
			h.eachCovering(*s.UserID, func(sess *Session) {
				sess.box.post(func(m *mailbox) { m.dirty[*s.UserID] = struct{}{} })
			})
		}
	}
}

func (h *Hub) eachCovering(userID uuid.UUID, fn func(*Session)) {
	h.mu.Lock()
	sessions := make([]*Session, 0, len(h.byUser[userID]))
	for s := range h.byUser[userID] {
		sessions = append(sessions, s)
	}
	h.mu.Unlock()
	for _, s := range sessions {
		fn(s)
	}
}
