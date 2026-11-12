// Package delivery fires reminders and tracks each one on each way of reaching
// the user until that way confirms it.
package delivery

import (
	"context"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/rayakame/plsremind/hub/internal/db/sqlc"
	"github.com/rayakame/plsremind/hub/internal/signal"
)

// Lifetime is how long a delivery waits to be confirmed.
const Lifetime = 24 * time.Hour

// Channel is one way of reaching a user: a connection or a personal token.
type Channel struct {
	ConnectionID *uuid.UUID
	TokenID      *uuid.UUID
}

// Channels returns the ways the user is reminded through right now.
func Channels(ctx context.Context, q *sqlc.Queries, userID uuid.UUID) ([]Channel, error) {
	connections, err := q.ListReminderConnections(ctx, userID)
	if err != nil {
		return nil, err
	}
	tokens, err := q.ListReminderTokens(ctx, userID)
	if err != nil {
		return nil, err
	}
	channels := make([]Channel, 0, len(connections)+len(tokens))
	for _, id := range connections {
		channels = append(channels, Channel{ConnectionID: &id})
	}
	for _, id := range tokens {
		channels = append(channels, Channel{TokenID: &id})
	}
	return channels, nil
}

func announceNew(ctx context.Context, q *sqlc.Queries, ids []uuid.UUID) error {
	for _, id := range ids {
		if err := signal.Send(ctx, q, signal.Signal{Type: signal.Delivery, DeliveryID: &id}); err != nil {
			return err
		}
	}
	return nil
}

// announceClosed tells the gateway which deliveries were closed without being confirmed.
func announceClosed(ctx context.Context, q *sqlc.Queries, rows []sqlc.Delivery) error {
	for _, row := range rows {
		err := signal.Send(ctx, q, signal.Signal{
			Type:         signal.Closed,
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

// CancelForItem closes the open deliveries of an item that was completed or is about to be deleted.
func CancelForItem(ctx context.Context, q *sqlc.Queries, now time.Time, itemID uuid.UUID) error {
	rows, err := q.CancelItemDeliveries(ctx, sqlc.CancelItemDeliveriesParams{ItemID: itemID, Now: &now})
	if err != nil {
		return err
	}
	return announceClosed(ctx, q, rows)
}

// CancelForConnection closes the open deliveries of a connection, for one user or for all (userID nil).
func CancelForConnection(ctx context.Context, q *sqlc.Queries, now time.Time, connectionID uuid.UUID, userID *uuid.UUID) error {
	rows, err := q.CancelConnectionDeliveries(ctx, sqlc.CancelConnectionDeliveriesParams{ConnectionID: &connectionID, UserID: userID, Now: &now})
	if err != nil {
		return err
	}
	return announceClosed(ctx, q, rows)
}

// CancelForToken closes the open deliveries of a personal token.
func CancelForToken(ctx context.Context, q *sqlc.Queries, now time.Time, tokenID uuid.UUID) error {
	rows, err := q.CancelTokenDeliveries(ctx, sqlc.CancelTokenDeliveriesParams{TokenID: &tokenID, Now: &now})
	if err != nil {
		return err
	}
	return announceClosed(ctx, q, rows)
}

// SyncConnection cancels what is open for a user on a connection that no longer reminds them.
func SyncConnection(ctx context.Context, q *sqlc.Queries, now time.Time, connectionID, userID uuid.UUID) error {
	reminding, err := q.ListReminderConnections(ctx, userID)
	if err != nil {
		return err
	}
	if slices.Contains(reminding, connectionID) {
		return nil
	}
	return CancelForConnection(ctx, q, now, connectionID, &userID)
}
