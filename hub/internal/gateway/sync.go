package gateway

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/rayakame/plsremind/hub/internal/account"
	"github.com/rayakame/plsremind/hub/internal/auth"
	"github.com/rayakame/plsremind/hub/internal/db/sqlc"
	"github.com/rayakame/plsremind/hub/internal/item"
	"github.com/rayakame/plsremind/hub/internal/list"
	"github.com/rayakame/plsremind/hub/internal/plan"
)

// stateIntents are the intents that make a session keep state about users.
var stateIntents = []string{"users", "lists", "items", "plans"}

func (s *Session) keepsState() bool {
	for _, intent := range stateIntents {
		if s.wants(intent) {
			return true
		}
	}
	return false
}

type chunk struct {
	Index int `json:"index"`
	Count int `json:"count"`
}

type userSync struct {
	UserID uuid.UUID   `json:"user_id"`
	Chunk  chunk       `json:"chunk"`
	User   any         `json:"user,omitempty"`
	Lists  []list.List `json:"lists,omitempty"`
	Items  []item.Item `json:"items,omitempty"`
	Plans  any         `json:"plans,omitempty"`
}

// envelope wraps every change event.
type envelope struct {
	UserID uuid.UUID `json:"user_id"`
	Actor  *actor    `json:"actor"`
	Data   any       `json:"data"`
}

type actor struct {
	Kind string    `json:"kind"`
	ID   uuid.UUID `json:"id"`
}

// userView renders a user the way this session may see them.
func (s *Session) userView(ctx context.Context, q *sqlc.Queries, user sqlc.User) (any, error) {
	return account.View(user), nil
}

// syncUser sends the user's state and from then on follows their event log.
//
// The state and the log position are read in one snapshot. Whatever commits
// after that snapshot has a higher number and is picked up by pump, so nothing
// is lost in between and nothing arrives that the state already contains.
func (s *Session) syncUser(ctx context.Context, userID uuid.UUID) error {
	tx, err := s.hub.d.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	q := sqlc.New(tx)

	seq, err := q.GetEventSeq(ctx, userID)
	if err != nil {
		return err
	}
	first := userSync{UserID: userID}
	if s.wants("users") {
		user, err := q.GetUser(ctx, userID)
		if err != nil {
			return err
		}
		if first.User, err = s.userView(ctx, q, user); err != nil {
			return err
		}
	}
	if s.wants("lists") {
		rows, err := q.ListLists(ctx, userID)
		if err != nil {
			return err
		}
		first.Lists = make([]list.List, 0, len(rows))
		for _, row := range rows {
			first.Lists = append(first.Lists, list.View(row))
		}
	}
	if s.wants("plans") {
		if first.Plans, err = plan.Views(ctx, q, userID); err != nil {
			return err
		}
	}
	var items []item.Item
	if s.wants("items") {
		if items, err = item.Open(ctx, q, userID); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}

	size := s.hub.d.Gateway.SyncChunk
	count := max(1, (len(items)+size-1)/size)
	for index := 0; index < count; index++ {
		part := userSync{UserID: userID}
		if index == 0 {
			part = first
		}
		part.Chunk = chunk{Index: index, Count: count}
		if s.wants("items") {
			part.Items = items[min(index*size, len(items)):min((index+1)*size, len(items))]
			if part.Items == nil {
				part.Items = []item.Item{}
			}
		}
		s.emit("user.sync", true, part)
	}
	s.cursors[userID] = seq
	s.hub.cover(s, userID)
	return s.pump(ctx, userID)
}

// intentOf maps an event type to the intent it belongs to.
func intentOf(eventType string) string {
	kind, _, _ := strings.Cut(eventType, ".")
	switch kind {
	case "user", "connection":
		return "users"
	case "list":
		return "lists"
	case "item", "subitem", "reminder":
		return "items"
	case "plan":
		return "plans"
	}
	return ""
}

const pumpBatch = 500

// pump sends every event of the user that the session has not seen yet.
func (s *Session) pump(ctx context.Context, userID uuid.UUID) error {
	cursor, covered := s.cursors[userID]
	if !covered {
		return nil
	}
	q := s.hub.d.Q()
	for {
		events, err := q.ListEventsAfter(ctx, sqlc.ListEventsAfterParams{UserID: userID, After: cursor, Batch: pumpBatch})
		if err != nil {
			return err
		}
		for _, ev := range events {
			if err := s.dispatch(ctx, q, ev); err != nil {
				return err
			}
			cursor = ev.Seq
			s.cursors[userID] = cursor
		}
		if len(events) < pumpBatch {
			return nil
		}
	}
}

func (s *Session) dispatch(ctx context.Context, q *sqlc.Queries, ev sqlc.Event) error {
	if !s.wants(intentOf(ev.Type)) {
		return nil
	}
	// What a user allows other connections is nobody's business but their own.
	if ev.Type == "connection.updated" && s.principal.Kind == auth.KindConnection {
		return nil
	}
	var data any = json.RawMessage(ev.Payload)
	if ev.Type == "user.updated" {
		// Rendered for this session rather than taken from the log, because a
		// connection sees less of a profile than the user does.
		user, err := q.GetUser(ctx, ev.UserID)
		if err != nil {
			return err
		}
		if data, err = s.userView(ctx, q, user); err != nil {
			return err
		}
	}
	s.emit(ev.Type, true, envelope{UserID: ev.UserID, Actor: &actor{Kind: ev.ActorKind, ID: ev.ActorID}, Data: data})
	return nil
}
