package item

import (
	"context"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/rayakame/plsremind/hub/internal/apierr"
	"github.com/rayakame/plsremind/hub/internal/db"
	"github.com/rayakame/plsremind/hub/internal/db/sqlc"
)

func location(ctx context.Context, q *sqlc.Queries, userID uuid.UUID) (*time.Location, error) {
	user, err := q.GetUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	loc, err := time.LoadLocation(user.Timezone)
	if err != nil {
		return time.UTC, nil
	}
	return loc, nil
}

func get(ctx context.Context, q *sqlc.Queries, userID, id uuid.UUID) (sqlc.Item, error) {
	row, err := q.GetItem(ctx, sqlc.GetItemParams{ID: id, UserID: userID})
	if db.IsNotFound(err) {
		return sqlc.Item{}, apierr.NotFound()
	}
	return row, err
}

// Views turns rows of one user into API items.
func Views(ctx context.Context, q *sqlc.Queries, userID uuid.UUID, rows []sqlc.Item) ([]Item, error) {
	loc, err := location(ctx, q, userID)
	if err != nil {
		return nil, err
	}
	items := make([]Item, 0, len(rows))
	for _, row := range rows {
		items = append(items, view(row, loc))
	}
	return items, nil
}

// Load returns one of the user's items, or not_found.
func Load(ctx context.Context, q *sqlc.Queries, userID, id uuid.UUID) (Item, error) {
	row, err := get(ctx, q, userID, id)
	if err != nil {
		return Item{}, err
	}
	items, err := Views(ctx, q, userID, []sqlc.Item{row})
	if err != nil {
		return Item{}, err
	}
	return items[0], nil
}

// Touch marks the item as changed, for edits to things that hang off it.
func Touch(ctx context.Context, q *sqlc.Queries, now time.Time, itemID uuid.UUID) error {
	return q.TouchItem(ctx, sqlc.TouchItemParams{ID: itemID, Now: now})
}

// MoveAll moves every item of a list to another list and returns them as they are now.
func MoveAll(ctx context.Context, q *sqlc.Queries, now time.Time, userID, from, to uuid.UUID) ([]Item, error) {
	rows, err := q.MoveItemsToList(ctx, sqlc.MoveItemsToListParams{ListID: from, TargetListID: to, Now: now})
	if err != nil {
		return nil, err
	}
	slices.SortFunc(rows, func(a, b sqlc.Item) int { return slices.Compare(a.ID[:], b.ID[:]) })
	return Views(ctx, q, userID, rows)
}
