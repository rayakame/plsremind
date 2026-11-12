package delivery

import (
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"github.com/rayakame/plsremind/hub/internal/apierr"
	"github.com/rayakame/plsremind/hub/internal/auth"
	"github.com/rayakame/plsremind/hub/internal/db"
	"github.com/rayakame/plsremind/hub/internal/db/sqlc"
	"github.com/rayakame/plsremind/hub/internal/deps"
	"github.com/rayakame/plsremind/hub/internal/duration"
	"github.com/rayakame/plsremind/hub/internal/httpx"
)

const maxReasonLen = 500

func Register(api, user fiber.Router, d *deps.Deps) {
	api.Get("/deliveries", fetch(d))
	api.Put("/deliveries/:id", acknowledge(d))
	user.Get("/reminder-log", history(d))
}

// channel resolves the caller to the channel it speaks for.
func channel(c fiber.Ctx, d *deps.Deps) (Channel, error) {
	p, ok := auth.From(c)
	if !ok {
		return Channel{}, apierr.Unauthorized()
	}
	ch, ok, err := ChannelOf(c.Context(), d.Q(), p)
	if err != nil {
		return Channel{}, err
	}
	if !ok {
		return Channel{}, apierr.Forbidden()
	}
	return ch, nil
}

func fetch(d *deps.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		ch, err := channel(c, d)
		if err != nil {
			return err
		}
		views, err := Pending(c.Context(), d, ch, false)
		if err != nil {
			return err
		}
		return c.JSON(views)
	}
}

func (ch Channel) owns(row sqlc.Delivery) bool {
	if ch.ConnectionID != nil {
		return row.ConnectionID != nil && *row.ConnectionID == *ch.ConnectionID
	}
	return row.TokenID != nil && ch.TokenID != nil && *row.TokenID == *ch.TokenID
}

func acknowledge(d *deps.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		ch, err := channel(c, d)
		if err != nil {
			return err
		}
		id, err := httpx.ParamUUID(c, "id")
		if err != nil {
			return err
		}
		var in struct {
			Status string `json:"status"`
			Reason string `json:"reason"`
		}
		if err := httpx.Body(c, &in); err != nil {
			return err
		}
		in.Reason = strings.TrimSpace(in.Reason)
		switch in.Status {
		case "delivered":
			in.Reason = ""
		case "failed":
			if n := utf8.RuneCountInString(in.Reason); n < 1 || n > maxReasonLen {
				return apierr.Field("reason", "A failed delivery needs a reason of at most 500 characters.")
			}
		default:
			return apierr.Field("status", `Must be "delivered" or "failed".`)
		}

		var out sqlc.Delivery
		err = db.Tx(c.Context(), d.Pool, func(q *sqlc.Queries) error {
			row, err := q.LockDelivery(c.Context(), id)
			// A delivery of another channel does not exist as far as this one is concerned.
			if db.IsNotFound(err) || (err == nil && !ch.owns(row)) {
				return apierr.NotFound()
			}
			if err != nil {
				return err
			}
			switch row.Status {
			case "pending":
				now := d.Now()
				out, err = q.CloseDelivery(c.Context(), sqlc.CloseDeliveryParams{ID: id, Status: in.Status, Reason: in.Reason, Now: &now})
				return err
			case in.Status:
				out = row
				return nil
			}
			return apierr.Conflict("delivery_closed", "The delivery is already "+row.Status+".")
		})
		if err != nil {
			return err
		}
		return c.JSON(fiber.Map{"id": out.ID, "status": out.Status, "reason": out.Reason})
	}
}

type logChannel struct {
	Kind string    `json:"kind"`
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

type logDelivery struct {
	ID       uuid.UUID  `json:"id"`
	Channel  logChannel `json:"channel"`
	Status   string     `json:"status"`
	Reason   string     `json:"reason"`
	Attempts int32      `json:"attempts"`
}

type logEntry struct {
	ID           uuid.UUID     `json:"id"`
	ItemID       uuid.UUID     `json:"item_id"`
	Kind         string        `json:"kind"`
	Offset       string        `json:"offset,omitempty"`
	ScheduledFor time.Time     `json:"scheduled_for"`
	FiredAt      time.Time     `json:"fired_at"`
	Outcome      string        `json:"outcome"`
	Deliveries   []logDelivery `json:"deliveries"`
}

func history(d *deps.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		page, err := httpx.ParsePage(c)
		if err != nil {
			return err
		}
		params := sqlc.ListReminderLogParams{UserID: auth.Target(c), PageSize: int32(page.Limit + 1)}
		if raw := c.Query("item_id"); raw != "" {
			itemID, err := uuid.Parse(raw)
			if err != nil {
				return apierr.Field("item_id", "Must be an item ID.")
			}
			params.ItemID = &itemID
		}
		if page.Cursor != "" {
			var before uuid.UUID
			if err := httpx.DecodeCursor(page.Cursor, &before); err != nil {
				return err
			}
			params.BeforeID = &before
		}
		q := d.Q()
		rows, err := q.ListReminderLog(c.Context(), params)
		if err != nil {
			return err
		}
		ids := make([]uuid.UUID, 0, len(rows))
		for _, row := range rows {
			ids = append(ids, row.ID)
		}
		deliveries, err := q.ListLogDeliveries(c.Context(), ids)
		if err != nil {
			return err
		}
		byReminder := map[uuid.UUID][]logDelivery{}
		for _, row := range deliveries {
			ch := logChannel{Kind: "token", Name: row.ChannelName}
			if row.ConnectionID != nil {
				ch.Kind, ch.ID = "connection", *row.ConnectionID
			} else if row.TokenID != nil {
				ch.ID = *row.TokenID
			}
			byReminder[row.ReminderID] = append(byReminder[row.ReminderID], logDelivery{
				ID: row.ID, Channel: ch, Status: row.Status, Reason: row.Reason, Attempts: row.Attempts,
			})
		}
		entries := make([]logEntry, 0, len(rows))
		for _, row := range rows {
			entry := logEntry{
				ID:           row.ID,
				ItemID:       row.ItemID,
				Kind:         row.Kind,
				ScheduledFor: row.ScheduledFor.UTC(),
				FiredAt:      row.FiredAt.UTC(),
				Outcome:      row.Outcome,
				Deliveries:   byReminder[row.ID],
			}
			if row.OffsetSeconds != nil {
				entry.Offset = duration.Format(time.Duration(*row.OffsetSeconds) * time.Second)
			}
			if entry.Deliveries == nil {
				entry.Deliveries = []logDelivery{}
			}
			entries = append(entries, entry)
		}
		return c.JSON(httpx.NewList(entries, page.Limit, func(last logEntry) any { return last.ID }))
	}
}
