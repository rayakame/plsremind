package todo

import (
	"time"

	"github.com/google/uuid"

	"github.com/rayakame/plsremind/hub/internal/api"
)

// cursor marks the last item of a page. It records the sort it was made for,
// because the position means nothing under a different ordering.
type itemCursor struct {
	Sort  string     `json:"s"`
	Order string     `json:"o"`
	ID    uuid.UUID  `json:"id"`
	Due   *time.Time `json:"due,omitempty"`
	Rank  *int       `json:"rank,omitempty"`
}

func (c itemCursor) encode() string {
	return api.EncodeCursor(c)
}

func decodeItemCursor(raw, sort, order string) (itemCursor, error) {
	var c itemCursor
	if err := api.DecodeCursor(raw, &c); err != nil {
		return itemCursor{}, err
	}
	if c.Sort != sort || c.Order != order || c.ID == uuid.Nil {
		return itemCursor{}, api.ErrCursor()
	}
	return c, nil
}
