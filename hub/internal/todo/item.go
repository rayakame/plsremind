package todo

import (
	"encoding/json"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/rayakame/plsremind/hub/internal/store/sqlc"
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
	// Reminders are the ones the user set by hand, earliest first.
	Reminders []Reminder `json:"reminders"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

// Reminder is a reminder the user set on an item by hand.
type Reminder struct {
	ID        uuid.UUID `json:"id"`
	ItemID    uuid.UUID `json:"item_id"`
	RemindAt  time.Time `json:"remind_at"`
	CreatedAt time.Time `json:"created_at"`
}

func ViewReminder(r sqlc.ManualReminder) Reminder {
	return Reminder{ID: r.ID, ItemID: r.ItemID, RemindAt: r.RemindAt.UTC(), CreatedAt: r.CreatedAt.UTC()}
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

const maxSubitemTitleLen = 200

func ViewSubitem(row sqlc.Subitem) Subitem {
	return Subitem{
		ID:        row.ID,
		ItemID:    row.ItemID,
		Title:     row.Title,
		Done:      row.Done,
		Position:  row.Position,
		CreatedAt: row.CreatedAt.UTC(),
		UpdatedAt: row.UpdatedAt.UTC(),
	}
}

// CleanSubitemTitle trims a subitem title and reports whether it is acceptable.
func CleanSubitemTitle(title string) (string, bool) {
	title = strings.TrimSpace(title)
	n := utf8.RuneCountInString(title)
	return title, n >= 1 && n <= maxSubitemTitleLen
}

// viewItem builds the API item. allDayDue is the due instant of an all-day item as the
// database computed it; the database is the only place that does this calculation.
func viewItem(row sqlc.Item, allDayDue *time.Time) Item {
	due := row.DueAt
	if row.DueDate.Valid {
		due = allDayDue
	}
	if due != nil {
		utc := due.UTC()
		due = &utc
	}
	out := Item{
		ID:        row.ID,
		ListID:    row.ListID,
		Title:     row.Title,
		Note:      row.Note,
		Priority:  row.Priority,
		DueAt:     due,
		AllDay:    row.DueDate.Valid,
		Status:    row.Status,
		Subitems:  []Subitem{},
		Reminders: []Reminder{},
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

func priorityRank(priority string) int {
	return slices.Index(priorities, priority) + 1
}
