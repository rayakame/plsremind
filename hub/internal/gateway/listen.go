package gateway

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/rayakame/plsremind/hub/internal/auth"
	"github.com/rayakame/plsremind/hub/internal/delivery"
	"github.com/rayakame/plsremind/hub/internal/events"
	"github.com/rayakame/plsremind/hub/internal/store"
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
	if _, err := conn.Exec(ctx, "LISTEN "+events.NotifyChannel); err != nil {
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
		var s events.Signal
		if err := json.Unmarshal([]byte(n.Payload), &s); err != nil {
			slog.Error("gateway got a malformed signal", "payload", n.Payload)
			continue
		}
		h.handle(s)
	}
}

// everything makes all sessions catch up, after signals may have been missed:
// new events, changed grants, withdrawn access and deliveries nobody was told about.
func (h *Hub) everything() {
	h.each(func(*Session) bool { return true }, func(s *Session) {
		s.box.post(func(m *mailbox) { m.all, m.validate, m.unsent, m.reconcile = true, true, true, true })
	})
}

func (h *Hub) handle(s events.Signal) {
	switch s.Type {
	case events.SignalEvent:
		if s.UserID != nil {
			h.eachCovering(*s.UserID, func(sess *Session) {
				sess.box.post(func(m *mailbox) { m.dirty[*s.UserID] = struct{}{} })
			})
		}
	case events.SignalGrant:
		if s.UserID != nil && s.ConnectionID != nil {
			h.each(func(sess *Session) bool { return sess.principal.CredentialID == *s.ConnectionID }, func(sess *Session) {
				sess.box.post(func(m *mailbox) { m.recheck[*s.UserID] = struct{}{} })
			})
		}
	case events.SignalUser:
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
	case events.SignalDelivery:
		if s.DeliveryID != nil {
			h.enqueue(outgoing{id: *s.DeliveryID})
		}
	case events.SignalClosed:
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
		// Through the same queue as new deliveries, so that "closed" cannot arrive before "created".
		h.enqueue(outgoing{id: *s.DeliveryID, closed: true, channel: *channel, userID: *s.UserID, status: s.Status})
	case events.SignalConnection:
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

// outgoing is a delivery to announce: a new one, or one that was closed.
type outgoing struct {
	id      uuid.UUID
	closed  bool
	channel uuid.UUID
	userID  uuid.UUID
	status  string
}

// enqueue hands a delivery to the sender. Rendering one takes a few queries,
// which must not hold up the other signals.
func (h *Hub) enqueue(o outgoing) {
	select {
	case h.outbox <- o:
	default:
		// The queue is full. Nothing is lost for good: every session asks the
		// database for what it was never sent.
		slog.Warn("gateway delivery queue is full, sessions will ask for what they missed")
		h.each(func(sess *Session) bool { return sess.wants("deliveries") }, func(sess *Session) {
			sess.box.post(func(m *mailbox) { m.unsent = true })
		})
	}
}

// deliver announces deliveries to the sessions of their channel, one at a time and in order.
func (h *Hub) deliver(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case o := <-h.outbox:
			if o.closed {
				data := envelope{UserID: o.userID, Data: map[string]any{"id": o.id, "status": o.status}}
				h.each(func(sess *Session) bool { return sess.principal.CredentialID == o.channel && sess.wants("deliveries") }, func(sess *Session) {
					sess.box.post(func(m *mailbox) { m.frames = append(m.frames, loose{event: "delivery.closed", data: data}) })
				})
				continue
			}
			if err := h.deliverOne(ctx, o.id); err != nil && ctx.Err() == nil {
				slog.Error("gateway could not send a delivery", "delivery", o.id, "err", err)
			}
		}
	}
}

func (h *Hub) deliverOne(ctx context.Context, id uuid.UUID) error {
	q := h.d.Q()
	row, err := q.GetDeliveryChannel(ctx, id)
	if store.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	channel := row.TokenID
	if row.ConnectionID != nil {
		channel = row.ConnectionID
	}
	type target struct {
		sess  *Session
		epoch int
	}
	var targets []target
	h.each(func(sess *Session) bool {
		return channel != nil && sess.principal.CredentialID == *channel && sess.wants("deliveries") && sess.attached()
	}, func(sess *Session) {
		// A session that got it with its login does not get it again.
		if epoch, ok := sess.claim(id); ok {
			targets = append(targets, target{sess, epoch})
		}
	})
	// Nobody is listening: the delivery waits, and the attempt is not counted.
	if len(targets) == 0 {
		return nil
	}
	view, _, sent, err := delivery.Send(ctx, h.d, id)
	if err != nil || !sent {
		// It did not go out after all, for one because quiet hours hold it back.
		for _, t := range targets {
			t.sess.unclaim(id, t.epoch)
		}
		return err
	}
	data := envelope{UserID: view.User.ID, Data: view}
	for _, t := range targets {
		t.sess.box.post(func(m *mailbox) { m.frames = append(m.frames, loose{"delivery.created", data, t.epoch}) })
	}
	return nil
}
