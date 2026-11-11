package item

import (
	"time"

	"github.com/google/uuid"

	"github.com/rayakame/plsremind/hub/internal/httpx"
)

// cursor marks the last item of a page. It records the sort it was made for,
// because the position means nothing under a different ordering.
type cursor struct {
	Sort  string     `json:"s"`
	Order string     `json:"o"`
	ID    uuid.UUID  `json:"id"`
	Due   *time.Time `json:"due,omitempty"`
	Rank  *int       `json:"rank,omitempty"`
}

func (c cursor) encode() string {
	return httpx.EncodeCursor(c)
}

func decodeCursor(raw, sort, order string) (cursor, error) {
	var c cursor
	if err := httpx.DecodeCursor(raw, &c); err != nil {
		return cursor{}, err
	}
	if c.Sort != sort || c.Order != order || c.ID == uuid.Nil {
		return cursor{}, httpx.ErrCursor()
	}
	return c, nil
}
