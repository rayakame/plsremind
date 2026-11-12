package events

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"github.com/rayakame/plsremind/hub/internal/store/sqlc"
)

const NotifyChannel = "plsremind"

const (
	// Event: a new entry in the user's event log.
	SignalEvent = "event"
	// Grant: a user changed whether a connection may act for them or remind them.
	SignalGrant = "grant"
	// User: something about a user's access or identity changed: disabled, deleted,
	// an account linked, a token or session gone. Their gateway sessions are checked again.
	SignalUser = "user"
	// Connection: a connection was changed, removed or got a new token.
	SignalConnection = "connection"
	// Delivery: a new delivery is waiting to be sent.
	SignalDelivery = "delivery"
	// Closed: an open delivery was closed without being acknowledged.
	SignalClosed = "closed"
)

// Signal tells the gateway that something changed. It travels as a PostgreSQL
// notification, which only arrives when the transaction that sent it commits.
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

// SendSignal queues the signal in q's transaction.
func SendSignal(ctx context.Context, q *sqlc.Queries, s Signal) error {
	payload, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return q.Notify(ctx, sqlc.NotifyParams{Channel: NotifyChannel, Payload: string(payload)})
}

func NotifyUser(ctx context.Context, q *sqlc.Queries, typ string, userID uuid.UUID) error {
	return SendSignal(ctx, q, Signal{Type: typ, UserID: &userID})
}

func NotifyConnection(ctx context.Context, q *sqlc.Queries, connectionID uuid.UUID) error {
	return SendSignal(ctx, q, Signal{Type: SignalConnection, ConnectionID: &connectionID})
}

// DeliveriesClosed tells the gateway which deliveries were closed without being confirmed.
func DeliveriesClosed(ctx context.Context, q *sqlc.Queries, rows []sqlc.Delivery) error {
	for _, row := range rows {
		// Nobody was ever handed this one, so nobody needs to take it back.
		if row.Attempts == 0 {
			continue
		}
		err := SendSignal(ctx, q, Signal{
			Type:         SignalClosed,
			DeliveryID:   &row.ID,
			UserID:       &row.UserID,
			ConnectionID: row.ConnectionID,
			TokenID:      row.TokenID,
			Status:       row.Status,
		})
		if err != nil {
			return err
		}
	}
	return nil
}
