package item

import (
	"context"
	"encoding/json"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/rayakame/plsremind/hub/internal/apierr"
	"github.com/rayakame/plsremind/hub/internal/db"
	"github.com/rayakame/plsremind/hub/internal/db/sqlc"
	"github.com/rayakame/plsremind/hub/internal/httpx"
	"github.com/rayakame/plsremind/hub/internal/plan"
)

const (
	MaxSubitems = 50

	maxTitleLen = 500
	maxNoteLen  = 10000
)

type input struct {
	Title            httpx.Opt[string]   `json:"title"`
	Note             httpx.Opt[string]   `json:"note"`
	ListID           httpx.Opt[string]   `json:"list_id"`
	Priority         httpx.Opt[string]   `json:"priority"`
	DueAt            httpx.Opt[string]   `json:"due_at"`
	DueDate          httpx.Opt[string]   `json:"due_date"`
	Status           httpx.Opt[string]   `json:"status"`
	ReminderOverride httpx.Opt[Override] `json:"reminder_override"`
	// Subitems is only read when creating an item.
	Subitems []string `json:"subitems"`
}

// apply validates the sent fields and writes them onto row, which holds the
// item's current state or the defaults of a new one.
func (in input) apply(ctx context.Context, q *sqlc.Queries, now time.Time, row *sqlc.Item) error {
	fields := map[string]string{}

	if in.Title.Set {
		row.Title = strings.TrimSpace(in.Title.Val)
	}
	if n := utf8.RuneCountInString(row.Title); n < 1 || n > maxTitleLen {
		fields["title"] = "Must be 1 to 500 characters."
	}

	if in.Note.Set {
		row.Note = in.Note.Val
		if utf8.RuneCountInString(row.Note) > maxNoteLen {
			fields["note"] = "Must be at most 10000 characters."
		}
	}

	if in.ListID.HasValue() {
		id, err := uuid.Parse(in.ListID.Val)
		if err == nil {
			_, err = q.GetList(ctx, sqlc.GetListParams{ID: id, UserID: row.UserID})
			if err != nil && !db.IsNotFound(err) {
				return err
			}
		}
		if err != nil {
			fields["list_id"] = "Must be one of your lists."
		} else {
			row.ListID = id
		}
	}

	if in.Priority.Set {
		row.Priority = in.Priority.Val
		if !plan.ValidPriority(row.Priority) {
			fields["priority"] = `Must be "low", "normal" or "high".`
		}
	}

	in.applyDue(row, fields)

	if in.Status.Set {
		switch in.Status.Val {
		case "done":
			if row.Status != "done" {
				row.CompletedAt = &now
			}
			row.Status = "done"
		case "open":
			row.Status = "open"
			row.CompletedAt = nil
		default:
			fields["status"] = `Must be "open" or "done".`
		}
	}

	if in.ReminderOverride.Set {
		raw, msg := normalizeOverride(in.ReminderOverride)
		if msg != "" {
			fields["reminder_override"] = msg
		}
		row.ReminderOverride = raw
	}

	if len(in.Subitems) > MaxSubitems {
		fields["subitems"] = "An item can have at most 50 subitems."
	}
	for i, title := range in.Subitems {
		clean, ok := CleanSubitemTitle(title)
		if !ok {
			fields["subitems"] = "Every subitem title must be 1 to 200 characters."
		}
		in.Subitems[i] = clean
	}

	if len(fields) > 0 {
		return apierr.Validation(fields)
	}
	return nil
}

func (in input) applyDue(row *sqlc.Item, fields map[string]string) {
	switch {
	case in.DueAt.HasValue() && in.DueDate.HasValue():
		fields["due_at"] = "Send either due_at or due_date, not both."
	case in.DueAt.HasValue():
		at, err := time.Parse(time.RFC3339, in.DueAt.Val)
		if err != nil {
			fields["due_at"] = "Must be an RFC 3339 timestamp."
			return
		}
		row.DueAt, row.DueDate = &at, pgtype.Date{}
	case in.DueDate.HasValue():
		date, err := time.Parse(dateLayout, in.DueDate.Val)
		if err != nil {
			fields["due_date"] = "Must be a date in YYYY-MM-DD format."
			return
		}
		row.DueAt, row.DueDate = nil, pgtype.Date{Time: date, Valid: true}
	case in.DueAt.Set || in.DueDate.Set:
		row.DueAt, row.DueDate = nil, pgtype.Date{}
	}
}

func normalizeOverride(in httpx.Opt[Override]) (raw []byte, msg string) {
	if in.Null {
		return nil, ""
	}
	override := Override{Mode: in.Val.Mode}
	switch in.Val.Mode {
	case "off":
	case "custom":
		if in.Val.Before == nil {
			return nil, `"custom" needs a "before" list.`
		}
		before, err := plan.NormalizeBefore(*in.Val.Before)
		if err != nil {
			return nil, err.Error()
		}
		formatted := plan.FormatBefore(before)
		override.Before = &formatted
	default:
		return nil, `"mode" must be "off" or "custom".`
	}
	raw, _ = json.Marshal(override)
	return raw, ""
}
