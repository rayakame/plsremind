// Package signal tells the gateway that something changed. Signals travel as
// PostgreSQL notifications, which are only delivered when the transaction that
// sent them commits, so the gateway never hears about a change that was rolled back.
package signal

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"github.com/rayakame/plsremind/hub/internal/db/sqlc"
)

const Channel = "plsremind"

const (
	// Event: a new entry in the user's event log.
	Event = "event"
	// Grant: a user changed whether a connection may act for them or remind them.
	Grant = "grant"
	// User: something about a user's access or identity changed: disabled, deleted,
	// an account linked, a token or session gone. Their gateway sessions are checked again.
	User = "user"
	// Connection: a connection was changed, removed or got a new token.
	Connection = "connection"
	// Delivery: a new delivery is waiting to be sent.
	Delivery = "delivery"
	// Closed: an open delivery was closed without being acknowledged.
	Closed = "closed"
)

type Signal struct {
	Type         string     `json:"type"`
	UserID       *uuid.UUID `json:"user_id,omitempty"`
	ConnectionID *uuid.UUID `json:"connection_id,omitempty"`
	DeliveryID   *uuid.UUID `json:"delivery_id,omitempty"`
	// TokenID and Status accompany Closed, together with the user and the connection:
	// the delivery itself may already be gone when the signal arrives.
	TokenID *uuid.UUID `json:"token_id,omitempty"`
	Status  string     `json:"status,omitempty"`
}

// Send queues the signal in q's transaction.
func Send(ctx context.Context, q *sqlc.Queries, s Signal) error {
	payload, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return q.Notify(ctx, sqlc.NotifyParams{Channel: Channel, Payload: string(payload)})
}

func ForUser(ctx context.Context, q *sqlc.Queries, typ string, userID uuid.UUID) error {
	return Send(ctx, q, Signal{Type: typ, UserID: &userID})
}

func ForConnection(ctx context.Context, q *sqlc.Queries, connectionID uuid.UUID) error {
	return Send(ctx, q, Signal{Type: Connection, ConnectionID: &connectionID})
}
