package gateway

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/rayakame/plsremind/hub/internal/auth"
	"github.com/rayakame/plsremind/hub/internal/db"
	"github.com/rayakame/plsremind/hub/internal/delivery"
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
	case signal.Grant:
		if s.UserID != nil && s.ConnectionID != nil {
			h.each(func(sess *Session) bool { return sess.principal.CredentialID == *s.ConnectionID }, func(sess *Session) {
				sess.box.post(func(m *mailbox) { m.recheck[*s.UserID] = struct{}{} })
			})
		}
	case signal.User:
		if s.UserID == nil {
			return
		}
		// The user's own sessions may have lost their access; connections may have
		// gained or lost the user.
		h.each(func(*Session) bool { return true }, func(sess *Session) {
			switch {
			case sess.principal.Kind == auth.KindConnection:
				sess.box.post(func(m *mailbox) { m.recheck[*s.UserID] = struct{}{} })
			case sess.principal.UserID == *s.UserID:
				sess.box.post(func(m *mailbox) { m.validate = true })
			}
		})
	case signal.Delivery:
		if s.DeliveryID != nil {
			// Rendering a delivery takes a few queries; they must not hold up the other signals.
			select {
			case h.outbox <- *s.DeliveryID:
			default:
				slog.Warn("gateway delivery queue is full, the delivery waits for the next login")
			}
		}
	case signal.Closed:
		// Acknowledged deliveries need no announcement: whoever confirmed them knows.
		if s.DeliveryID == nil || s.UserID == nil || (s.Status != "superseded" && s.Status != "cancelled" && s.Status != "expired") {
			return
		}
		channel := s.ConnectionID
		if channel == nil {
			channel = s.TokenID
		}
		if channel == nil {
			return
		}
		data := envelope{UserID: *s.UserID, Data: map[string]any{"id": *s.DeliveryID, "status": s.Status}}
		h.each(func(sess *Session) bool { return sess.principal.CredentialID == *channel && sess.wants("deliveries") }, func(sess *Session) {
			sess.box.post(func(m *mailbox) { m.frames = append(m.frames, loose{"delivery.closed", data}) })
		})
	case signal.Connection:
		if s.ConnectionID != nil {
			h.each(func(sess *Session) bool { return sess.principal.CredentialID == *s.ConnectionID }, func(sess *Session) {
				sess.box.post(func(m *mailbox) { m.validate = true })
			})
		}
	}
}

// each calls fn for every session that matches, outside the hub's lock.
func (h *Hub) each(match func(*Session) bool, fn func(*Session)) {
	h.mu.Lock()
	sessions := make([]*Session, 0, len(h.sessions))
	for _, s := range h.sessions {
		if match(s) {
			sessions = append(sessions, s)
		}
	}
	h.mu.Unlock()
	for _, s := range sessions {
		fn(s)
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

// deliver sends new deliveries to the sessions of their channel, one at a time.
func (h *Hub) deliver(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case id := <-h.outbox:
			if err := h.deliverOne(ctx, id); err != nil && ctx.Err() == nil {
				slog.Error("gateway could not send a delivery", "delivery", id, "err", err)
			}
		}
	}
}

func (h *Hub) deliverOne(ctx context.Context, id uuid.UUID) error {
	q := h.d.Q()
	row, err := q.GetDeliveryChannel(ctx, id)
	if db.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	channel := row.TokenID
	if row.ConnectionID != nil {
		channel = row.ConnectionID
	}
	var targets []*Session
	h.each(func(sess *Session) bool {
		return channel != nil && sess.principal.CredentialID == *channel && sess.wants("deliveries") && sess.attached()
	}, func(sess *Session) { targets = append(targets, sess) })
	// Nobody is listening: the delivery waits, and the attempt is not counted.
	if len(targets) == 0 {
		return nil
	}
	view, _, sent, err := delivery.Send(ctx, h.d, id)
	if err != nil || !sent {
		return err
	}
	data := envelope{UserID: view.User.ID, Data: view}
	for _, sess := range targets {
		sess.box.post(func(m *mailbox) { m.frames = append(m.frames, loose{"delivery.created", data}) })
	}
	return nil
}
