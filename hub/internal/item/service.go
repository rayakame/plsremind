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

func get(ctx context.Context, q *sqlc.Queries, userID, id uuid.UUID) (sqlc.Item, error) {
	row, err := q.GetItem(ctx, sqlc.GetItemParams{ID: id, UserID: userID})
	if db.IsNotFound(err) {
		return sqlc.Item{}, apierr.NotFound()
	}
	return row, err
}

// Lock returns one of the user's items and holds its row until the transaction
// ends, so that changes to its subitems and reminders happen one at a time.
func Lock(ctx context.Context, q *sqlc.Queries, userID, id uuid.UUID) (sqlc.Item, error) {
	row, err := q.LockItem(ctx, sqlc.LockItemParams{ID: id, UserID: userID})
	if db.IsNotFound(err) {
		return sqlc.Item{}, apierr.NotFound()
	}
	return row, err
}

// Views turns rows into API items.
func Views(ctx context.Context, q *sqlc.Queries, rows []sqlc.Item) ([]Item, error) {
	ids := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	dues, err := q.ListAllDayDue(ctx, ids)
	if err != nil {
		return nil, err
	}
	allDayDue := make(map[uuid.UUID]*time.Time, len(dues))
	for _, due := range dues {
		allDayDue[due.ID] = &due.Due
	}
	subitems, err := q.ListSubitems(ctx, ids)
	if err != nil {
		return nil, err
	}
	byItem := make(map[uuid.UUID][]Subitem, len(rows))
	for _, sub := range subitems {
		byItem[sub.ItemID] = append(byItem[sub.ItemID], ViewSubitem(sub))
	}
	manual, err := q.ListManualRemindersForItems(ctx, ids)
	if err != nil {
		return nil, err
	}
	reminders := make(map[uuid.UUID][]Reminder, len(rows))
	for _, m := range manual {
		reminders[m.ItemID] = append(reminders[m.ItemID], ViewReminder(m))
	}
	items := make([]Item, 0, len(rows))
	for _, row := range rows {
		item := view(row, allDayDue[row.ID])
		if subs := byItem[row.ID]; subs != nil {
			item.Subitems = subs
		}
		if rs := reminders[row.ID]; rs != nil {
			item.Reminders = rs
		}
		items = append(items, item)
	}
	return items, nil
}

// Load returns one of the user's items, or not_found.
func Load(ctx context.Context, q *sqlc.Queries, userID, id uuid.UUID) (Item, error) {
	row, err := get(ctx, q, userID, id)
	if err != nil {
		return Item{}, err
	}
	items, err := Views(ctx, q, []sqlc.Item{row})
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
func MoveAll(ctx context.Context, q *sqlc.Queries, now time.Time, from, to uuid.UUID) ([]Item, error) {
	rows, err := q.MoveItemsToList(ctx, sqlc.MoveItemsToListParams{ListID: from, TargetListID: to, Now: now})
	if err != nil {
		return nil, err
	}
	slices.SortFunc(rows, func(a, b sqlc.Item) int { return slices.Compare(a.ID[:], b.ID[:]) })
	return Views(ctx, q, rows)
}

// OpenAllDay returns the user's open all-day items. Their due time depends on
// the user's time zone, so they change when the zone does.
func OpenAllDay(ctx context.Context, q *sqlc.Queries, userID uuid.UUID) ([]Item, error) {
	rows, err := q.ListOpenAllDayItems(ctx, userID)
	if err != nil {
		return nil, err
	}
	return Views(ctx, q, rows)
}

// Open returns all open items of a user with their subitems and reminders, oldest first.
func Open(ctx context.Context, q *sqlc.Queries, userID uuid.UUID) ([]Item, error) {
	rows, err := q.ListOpenItems(ctx, userID)
	if err != nil {
		return nil, err
	}
	return Views(ctx, q, rows)
}
