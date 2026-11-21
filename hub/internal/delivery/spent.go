package delivery

import (
	"context"
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/rayakame/plsremind/hub/internal/api"
	"github.com/rayakame/plsremind/hub/internal/core"
	"github.com/rayakame/plsremind/hub/internal/events"
	"github.com/rayakame/plsremind/hub/internal/store"
	"github.com/rayakame/plsremind/hub/internal/store/sqlc"
	"github.com/rayakame/plsremind/hub/internal/todo"
)

type spentItem struct {
	userID    uuid.UUID
	itemID    uuid.UUID
	reminders []uuid.UUID
}

// sweepRounds caps one run, so that a large backlog does not hold up the reminders that are due.
const sweepRounds = 5

// Not in fireBatch: that holds schedule rows before the user's row, the reverse of a request.
func sweepSpent(ctx context.Context, d *core.Deps) error {
	var failed []error
	params := sqlc.ListSpentManualRemindersParams{Now: d.Now(), Batch: batchSize}
	for range sweepRounds {
		var spent []sqlc.ListSpentManualRemindersRow
		err := store.Tx(ctx, d.Pool, func(q *sqlc.Queries) error {
			var err error
			spent, err = q.ListSpentManualReminders(ctx, params)
			return err
		})
		if err != nil {
			return errors.Join(append(failed, err)...)
		}
		if len(spent) == 0 {
			break
		}
		// The next round starts after this batch, so that reminders that could not be swept do not block the rest.
		last := spent[len(spent)-1]
		params.AfterAt, params.AfterID = last.RemindAt, last.ID
		for _, item := range bySpentItem(spent) {
			if _, err := dropSpent(ctx, d, item); err != nil {
				// Reported once the others are done.
				failed = append(failed, err)
			}
		}
	}
	return errors.Join(failed...)
}

func bySpentItem(rows []sqlc.ListSpentManualRemindersRow) []*spentItem {
	var items []*spentItem
	index := map[uuid.UUID]*spentItem{}
	for _, row := range rows {
		item, ok := index[row.ItemID]
		if !ok {
			item = &spentItem{userID: row.UserID, itemID: row.ItemID}
			index[row.ItemID] = item
			items = append(items, item)
		}
		item.reminders = append(item.reminders, row.ID)
	}
	return items
}

func dropSpent(ctx context.Context, d *core.Deps, item *spentItem) (int, error) {
	removed := 0
	err := store.UserTx(ctx, d.Pool, item.userID, func(q *sqlc.Queries) error {
		removed = 0
		if _, err := todo.LockItem(ctx, q, item.userID, item.itemID); err != nil {
			return err
		}
		now := d.Now()
		for _, id := range item.reminders {
			n, err := q.DeleteSpentManualReminder(ctx, sqlc.DeleteSpentManualReminderParams{ID: id, Now: now})
			if err != nil {
				return err
			}
			if n == 0 {
				continue
			}
			removed++
			gone := events.Deleted{ID: id, ItemID: &item.itemID}
			if err := events.Append(ctx, q, now, item.userID, events.Actor{}, "reminder.deleted", gone); err != nil {
				return err
			}
		}
		if removed == 0 {
			return nil
		}
		return todo.AnnounceItem(ctx, q, now, item.userID, item.itemID, events.Actor{})
	})
	var gone *api.Error
	if errors.As(err, &gone) && gone.Status == http.StatusNotFound {
		// The user or the item was deleted meanwhile, and its reminders with it.
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return removed, nil
}
