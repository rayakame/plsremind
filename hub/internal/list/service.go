package list

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/rayakame/plsremind/hub/internal/apierr"
	"github.com/rayakame/plsremind/hub/internal/db"
	"github.com/rayakame/plsremind/hub/internal/db/sqlc"
)

const (
	maxLists   = 200
	maxNameLen = 100
)

type List struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Position  int32     `json:"position"`
	IsInbox   bool      `json:"is_inbox"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func View(l sqlc.List) List {
	return List{
		ID:        l.ID,
		Name:      l.Name,
		Position:  l.Position,
		IsInbox:   l.IsInbox,
		CreatedAt: l.CreatedAt.UTC(),
		UpdatedAt: l.UpdatedAt.UTC(),
	}
}

func Inbox(ctx context.Context, q *sqlc.Queries, userID uuid.UUID) (sqlc.List, error) {
	return q.GetInbox(ctx, userID)
}

// Get returns one of the user's lists, or not_found, and holds its row until the
// transaction ends so that renames, deletions and moves into it do not interleave.
func Get(ctx context.Context, q *sqlc.Queries, userID, id uuid.UUID) (sqlc.List, error) {
	l, err := q.LockList(ctx, sqlc.LockListParams{ID: id, UserID: userID})
	if db.IsNotFound(err) {
		return sqlc.List{}, apierr.NotFound()
	}
	return l, err
}

func errInbox() error {
	return apierr.Conflict("inbox_protected", "The inbox cannot be renamed or deleted.")
}
