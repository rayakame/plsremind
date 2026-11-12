package reminder

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/rayakame/plsremind/hub/internal/events"
	"github.com/rayakame/plsremind/hub/internal/store/sqlc"
)

// CancelDeliveries closes the open deliveries of an item that was completed or is about to be deleted.
func CancelDeliveries(ctx context.Context, q *sqlc.Queries, now time.Time, itemID uuid.UUID) error {
	rows, err := q.CancelItemDeliveries(ctx, sqlc.CancelItemDeliveriesParams{ItemID: itemID, Now: &now})
	if err != nil {
		return err
	}
	return events.DeliveriesClosed(ctx, q, rows)
}
