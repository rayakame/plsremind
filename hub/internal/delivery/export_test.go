package delivery

import (
	"context"

	"github.com/google/uuid"

	"github.com/rayakame/plsremind/hub/internal/core"
)

// DropSpent lets a test drop the reminders of one item the way the sweep does.
func DropSpent(ctx context.Context, d *core.Deps, userID, itemID uuid.UUID, reminders ...uuid.UUID) (int, error) {
	return dropSpent(ctx, d, &spentItem{userID: userID, itemID: itemID, reminders: reminders})
}
