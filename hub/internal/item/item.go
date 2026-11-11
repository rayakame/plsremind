// Package item holds todos and deadlines. A deadline is an item with a due time.
package item

import (
	"encoding/json"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/rayakame/plsremind/hub/internal/db/sqlc"
)

const dateLayout = "2006-01-02"

// Override replaces the reminder plan of the item's priority for one item.
type Override struct {
	Mode   string    `json:"mode"`
	Before *[]string `json:"before,omitempty"`
}

type Item struct {
	ID               uuid.UUID  `json:"id"`
	ListID           uuid.UUID  `json:"list_id"`
	Title            string     `json:"title"`
	Note             string     `json:"note"`
	Priority         string     `json:"priority"`
	DueAt            *time.Time `json:"due_at"`
	DueDate          *string    `json:"due_date"`
	AllDay           bool       `json:"all_day"`
	Status           string     `json:"status"`
	CompletedAt      *time.Time `json:"completed_at"`
	ReminderOverride *Override  `json:"reminder_override"`
	Subitems         []Subitem  `json:"subitems"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

type Subitem struct {
	ID        uuid.UUID `json:"id"`
	ItemID    uuid.UUID `json:"item_id"`
	Title     string    `json:"title"`
	Done      bool      `json:"done"`
	Position  int32     `json:"position"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// EffectiveDue is the instant an item is due. An all-day item is due at the
// last second of its day in loc.
func EffectiveDue(dueAt *time.Time, dueDate pgtype.Date, loc *time.Location) *time.Time {
	if dueAt != nil {
		due := dueAt.UTC()
		return &due
	}
	if !dueDate.Valid {
		return nil
	}
	y, m, d := dueDate.Time.Date()
	due := time.Date(y, m, d+1, 0, 0, 0, 0, loc).Add(-time.Second).UTC()
	return &due
}

func view(row sqlc.Item, loc *time.Location) Item {
	out := Item{
		ID:        row.ID,
		ListID:    row.ListID,
		Title:     row.Title,
		Note:      row.Note,
		Priority:  row.Priority,
		DueAt:     EffectiveDue(row.DueAt, row.DueDate, loc),
		AllDay:    row.DueDate.Valid,
		Status:    row.Status,
		Subitems:  []Subitem{},
		CreatedAt: row.CreatedAt.UTC(),
		UpdatedAt: row.UpdatedAt.UTC(),
	}
	if row.DueDate.Valid {
		date := row.DueDate.Time.Format(dateLayout)
		out.DueDate = &date
	}
	if row.CompletedAt != nil {
		completed := row.CompletedAt.UTC()
		out.CompletedAt = &completed
	}
	if row.ReminderOverride != nil {
		var override Override
		if json.Unmarshal(row.ReminderOverride, &override) == nil {
			out.ReminderOverride = &override
		}
	}
	return out
}

var priorities = []string{"low", "normal", "high"}

func rank(priority string) int {
	return slices.Index(priorities, priority) + 1
}
