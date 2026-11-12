package delivery

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/rayakame/plsremind/hub/internal/auth"
	"github.com/rayakame/plsremind/hub/internal/db"
	"github.com/rayakame/plsremind/hub/internal/db/sqlc"
	"github.com/rayakame/plsremind/hub/internal/deps"
	"github.com/rayakame/plsremind/hub/internal/duration"
	"github.com/rayakame/plsremind/hub/internal/item"
	"github.com/rayakame/plsremind/hub/internal/schedule"
)

// lateAfter is how long past its time a reminder may arrive before it is marked late.
const lateAfter = time.Minute

type Reminder struct {
	ID           uuid.UUID `json:"id"`
	Kind         string    `json:"kind"`
	Offset       string    `json:"offset,omitempty"`
	ScheduledFor time.Time `json:"scheduled_for"`
}

type User struct {
	ID          uuid.UUID `json:"id"`
	DisplayName string    `json:"display_name"`
	Timezone    string    `json:"timezone"`
	// ExternalIDs are the user's accounts at the connection's provider, so the
	// connection knows whom to address. Absent for personal tokens.
	ExternalIDs []string `json:"external_ids,omitempty"`
}

// View is a delivery as a channel receives it: everything needed to remind
// without asking the hub anything else.
type View struct {
	ID       uuid.UUID `json:"id"`
	Attempt  int32     `json:"attempt"`
	Late     bool      `json:"late"`
	Reminder Reminder  `json:"reminder"`
	Item     item.Item `json:"item"`
	User     User      `json:"user"`
}

func viewReminder(r sqlc.ReminderLog) Reminder {
	out := Reminder{ID: r.ID, Kind: r.Kind, ScheduledFor: r.ScheduledFor.UTC()}
	if r.OffsetSeconds != nil {
		out.Offset = duration.Format(time.Duration(*r.OffsetSeconds) * time.Second)
	}
	return out
}

// ChannelOf returns the channel a caller speaks for, if it is one: a connection
// that may have deliveries, or a personal token marked for reminders.
func ChannelOf(ctx context.Context, q *sqlc.Queries, p auth.Principal) (Channel, bool, error) {
	switch p.Kind {
	case auth.KindConnection:
		return Channel{ConnectionID: &p.CredentialID}, auth.HasIntent(p, "deliveries"), nil
	case auth.KindPersonalToken:
		token, err := q.GetPersonalTokenByID(ctx, p.CredentialID)
		if db.IsNotFound(err) {
			return Channel{}, false, nil
		}
		return Channel{TokenID: &p.CredentialID}, err == nil && token.Remind, err
	}
	return Channel{}, false, nil
}

// build renders one delivery and counts the send.
func build(ctx context.Context, q *sqlc.Queries, now time.Time, d sqlc.Delivery, r sqlc.ReminderLog, user sqlc.User, provider string) (View, error) {
	attempt, err := q.SendDelivery(ctx, d.ID)
	if err != nil {
		return View{}, err
	}
	it, err := item.Load(ctx, q, d.UserID, d.ItemID)
	if err != nil {
		return View{}, err
	}
	view := View{
		ID:      d.ID,
		Attempt: attempt,
		// Measured from when it should have gone out. A reminder that quiet hours
		// moved and that then left on time is not late.
		Late:     now.Sub(r.FireAt) > lateAfter,
		Reminder: viewReminder(r),
		Item:     it,
		User:     User{ID: user.ID, DisplayName: user.DisplayName, Timezone: user.Timezone},
	}
	if provider != "" {
		view.User.ExternalIDs, err = q.ListExternalIDs(ctx, sqlc.ListExternalIDsParams{UserID: user.ID, Provider: provider})
		if err != nil {
			return View{}, err
		}
		if view.User.ExternalIDs == nil {
			view.User.ExternalIDs = []string{}
		}
	}
	return view, nil
}

type pendingRow struct {
	delivery sqlc.Delivery
	reminder sqlc.ReminderLog
}

// held reports whether a delivery has to wait for the user's quiet hours to end.
// The reminder at the due time and reminders set by hand always go out.
func held(now time.Time, r sqlc.ReminderLog, user sqlc.User) (time.Time, bool) {
	if r.Kind != string(schedule.KindBefore) && r.Kind != string(schedule.KindFollowUp) {
		return time.Time{}, false
	}
	return schedule.QuietUntil(schedule.QuietOf(user), schedule.LocationOf(user), now)
}

// render turns pending rows into views, leaving out the ones quiet hours hold back.
func render(ctx context.Context, q *sqlc.Queries, now time.Time, rows []pendingRow, provider string) ([]View, error) {
	views := make([]View, 0, len(rows))
	users := map[uuid.UUID]sqlc.User{}
	channels := map[uuid.UUID][]Channel{}
	for _, row := range rows {
		user, ok := users[row.delivery.UserID]
		if !ok {
			var err error
			if user, err = q.GetUser(ctx, row.delivery.UserID); err != nil {
				return nil, err
			}
			users[user.ID] = user
		}
		reminds, err := stillReminds(ctx, q, channels, row.delivery)
		if err != nil {
			return nil, err
		}
		if !reminds {
			// The channel was switched off, or the user disabled, in the moment
			// the reminder fired. It is withdrawn instead of handed out.
			closed, err := q.CloseDelivery(ctx, sqlc.CloseDeliveryParams{ID: row.delivery.ID, Status: "cancelled", Now: &now})
			if err != nil && !db.IsNotFound(err) {
				return nil, err
			}
			if err == nil {
				if err := announceClosed(ctx, q, []sqlc.Delivery{closed}); err != nil {
					return nil, err
				}
			}
			continue
		}
		if until, wait := held(now, row.reminder, user); wait {
			if err := q.HoldDelivery(ctx, sqlc.HoldDeliveryParams{ID: row.delivery.ID, HeldUntil: &until}); err != nil {
				return nil, err
			}
			continue
		}
		view, err := build(ctx, q, now, row.delivery, row.reminder, user, provider)
		if err != nil {
			return nil, err
		}
		views = append(views, view)
	}
	return views, nil
}

// stillReminds reports whether the delivery's channel is still a way the user is reminded through.
func stillReminds(ctx context.Context, q *sqlc.Queries, cache map[uuid.UUID][]Channel, row sqlc.Delivery) (bool, error) {
	channels, ok := cache[row.UserID]
	if !ok {
		var err error
		if channels, err = Channels(ctx, q, row.UserID); err != nil {
			return false, err
		}
		cache[row.UserID] = channels
	}
	for _, ch := range channels {
		if ch.owns(row) {
			return true, nil
		}
	}
	return false, nil
}

// ReleaseHeldFor lets go of what was held back for a user's quiet hours, after
// those hours changed. The ticker sends it on its next round.
func ReleaseHeldFor(ctx context.Context, q *sqlc.Queries, now time.Time, userID uuid.UUID) error {
	return q.ReleaseUserHeldDeliveries(ctx, sqlc.ReleaseUserHeldDeliveriesParams{UserID: userID, Now: &now})
}

// Pending hands a channel everything that is waiting for it. Each delivery
// handed out counts as one attempt. With onlyUnsent it is limited to
// deliveries that were never handed out.
func Pending(ctx context.Context, d *deps.Deps, ch Channel, onlyUnsent bool) ([]View, error) {
	var views []View
	err := db.Tx(ctx, d.Pool, func(q *sqlc.Queries) error {
		var rows []pendingRow
		provider := ""
		if ch.ConnectionID != nil {
			conn, err := q.GetConnection(ctx, *ch.ConnectionID)
			if err != nil {
				return err
			}
			provider = conn.Provider
			found, err := q.ListPendingConnectionDeliveries(ctx, sqlc.ListPendingConnectionDeliveriesParams{ConnectionID: ch.ConnectionID, OnlyUnsent: onlyUnsent})
			if err != nil {
				return err
			}
			for _, f := range found {
				rows = append(rows, pendingRow{f.Delivery, f.ReminderLog})
			}
		} else {
			found, err := q.ListPendingTokenDeliveries(ctx, sqlc.ListPendingTokenDeliveriesParams{TokenID: ch.TokenID, OnlyUnsent: onlyUnsent})
			if err != nil {
				return err
			}
			for _, f := range found {
				rows = append(rows, pendingRow{f.Delivery, f.ReminderLog})
			}
		}
		var err error
		views, err = render(ctx, q, d.Now(), rows, provider)
		return err
	})
	return views, err
}

// Send hands out one pending delivery, counting the attempt. It reports false
// when there is nothing to send: the delivery is closed, or quiet hours hold it back.
func Send(ctx context.Context, d *deps.Deps, id uuid.UUID) (View, Channel, bool, error) {
	var view View
	var ch Channel
	sent := false
	err := db.Tx(ctx, d.Pool, func(q *sqlc.Queries) error {
		found, err := q.GetPendingDelivery(ctx, id)
		if db.IsNotFound(err) {
			return nil
		}
		if err != nil {
			return err
		}
		ch = Channel{ConnectionID: found.Delivery.ConnectionID, TokenID: found.Delivery.TokenID}
		provider := ""
		if ch.ConnectionID != nil {
			conn, err := q.GetConnection(ctx, *ch.ConnectionID)
			if err != nil {
				return err
			}
			provider = conn.Provider
		}
		views, err := render(ctx, q, d.Now(), []pendingRow{{found.Delivery, found.ReminderLog}}, provider)
		if err != nil || len(views) == 0 {
			return err
		}
		view, sent = views[0], true
		return nil
	})
	return view, ch, sent, err
}

// ID returns the credential the channel belongs to: the connection or the token.
func (ch Channel) ID() uuid.UUID {
	if ch.ConnectionID != nil {
		return *ch.ConnectionID
	}
	if ch.TokenID != nil {
		return *ch.TokenID
	}
	return uuid.Nil
}
