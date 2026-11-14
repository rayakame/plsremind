package todo

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/rayakame/plsremind/hub/internal/store/sqlc"
)

// HistoryPos is a place in a user's done items, ordered by when they were completed.
type HistoryPos struct {
	CompletedAt time.Time
	ID          uuid.UUID
}

// History returns the user's done items after the given place, most recently completed first.
func History(ctx context.Context, q *sqlc.Queries, userID uuid.UUID, after *HistoryPos, size int) ([]Item, error) {
	params := sqlc.ListItemsByCompletedParams{UserID: userID, Descending: true, PageSize: int32(size)}
	if after != nil {
		params.CursorID, params.CursorCompleted = &after.ID, &after.CompletedAt
	}
	rows, err := q.ListItemsByCompleted(ctx, params)
	if err != nil {
		return nil, err
	}
	return ViewItems(ctx, q, rows)
}

// HistoryCursor is the cursor with which listing done items by completion,
// newest first, continues after the given item.
func HistoryCursor(last Item) string {
	return itemCursor{Sort: "completed", Order: "desc", ID: last.ID, Completed: last.CompletedAt}.encode()
}
