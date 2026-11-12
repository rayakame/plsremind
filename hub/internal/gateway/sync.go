package gateway

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/rayakame/plsremind/hub/internal/account"
	"github.com/rayakame/plsremind/hub/internal/auth"
	"github.com/rayakame/plsremind/hub/internal/db"
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

// userSync leaves out the parts the session did not ask for. The fields are
// untyped so that an empty list is still sent: "no items" is state too.
type userSync struct {
	UserID uuid.UUID `json:"user_id"`
	Chunk  chunk     `json:"chunk"`
	User   any       `json:"user,omitempty"`
	Lists  any       `json:"lists,omitempty"`
	Items  any       `json:"items,omitempty"`
	Plans  any       `json:"plans,omitempty"`
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

// connectionUser is a user as a connection sees them: enough to address them
// at the right time, and whom to address at the connection's provider.
type connectionUser struct {
	ID          uuid.UUID           `json:"id"`
	DisplayName string              `json:"display_name"`
	Timezone    string              `json:"timezone"`
	QuietHours  *account.QuietHours `json:"quiet_hours"`
	ExternalIDs []string            `json:"external_ids"`
	Remind      bool                `json:"remind"`
}

// userView renders a user the way this session may see them.
func (s *Session) userView(ctx context.Context, q *sqlc.Queries, user sqlc.User) (any, error) {
	if s.principal.Kind != auth.KindConnection {
		return account.View(user), nil
	}
	externalIDs, err := q.ListExternalIDs(ctx, sqlc.ListExternalIDsParams{UserID: user.ID, Provider: s.principal.ConnectionProvider})
	if err != nil {
		return nil, err
	}
	if externalIDs == nil {
		externalIDs = []string{}
	}
	remind, err := q.GetGrantRemind(ctx, sqlc.GetGrantRemindParams{UserID: user.ID, ConnectionID: s.principal.CredentialID})
	if err != nil {
		return nil, err
	}
	return connectionUser{
		ID:          user.ID,
		DisplayName: user.DisplayName,
		Timezone:    user.Timezone,
		QuietHours:  account.ViewQuietHours(user),
		ExternalIDs: externalIDs,
		Remind:      remind,
	}, nil
}

// recheck looks at whether the session still follows a user after their access
// or their grant changed, and tells the client what that means for it.
func (s *Session) recheck(ctx context.Context, userID uuid.UUID) error {
	if s.principal.Kind != auth.KindConnection || !s.keepsState() {
		return nil
	}
	q := s.hub.d.Q()
	user, err := q.GetGrantedUser(ctx, sqlc.GetGrantedUserParams{ConnectionID: s.principal.CredentialID, UserID: userID})
	covered := err == nil
	if err != nil && !db.IsNotFound(err) {
		return err
	}
	_, following := s.cursors[userID]
	switch {
	case covered && !following:
		return s.syncUser(ctx, userID)
	case !covered && following:
		delete(s.cursors, userID)
		s.hub.uncover(s, userID)
		s.emit("user.gone", true, map[string]uuid.UUID{"user_id": userID})
	case covered && s.wants("users"):
		view, err := s.userView(ctx, q, user)
		if err != nil {
			return err
		}
		s.emit("user.updated", true, envelope{UserID: userID, Data: view})
	}
	return nil
}

// revalidate checks that whoever identified may still be here. It returns a
// close code only when access is definitely gone. If the check itself fails,
// the session stays and is looked at again at the next heartbeat: a database
// that is briefly away must not tell clients that their token is dead.
func (s *Session) revalidate(ctx context.Context) (code int) {
	p, _, v := s.hub.authenticate(ctx, s.token, s.cookie, true)
	switch {
	case v == trouble:
		slog.Warn("gateway could not check a session's access, keeping it for now", "session", s.id)
		return 0
	case v == denied || p.CredentialID != s.principal.CredentialID:
		return CloseAuthFailed
	}
	for _, intent := range s.intents {
		if !auth.HasIntent(p, intent) {
			return CloseDisallowedIntents
		}
	}
	return 0
}

// reconcile compares the users a connection session follows with the users it
// may follow, after grant changes may have gone unheard.
func (s *Session) reconcile(ctx context.Context) error {
	if s.principal.Kind != auth.KindConnection || !s.keepsState() {
		return nil
	}
	granted, err := s.hub.d.Q().ListGrantedUsers(ctx, s.principal.CredentialID)
	if err != nil {
		return err
	}
	seen := make(map[uuid.UUID]bool, len(granted))
	for _, userID := range granted {
		seen[userID] = true
	}
	for userID := range s.cursors {
		if !seen[userID] {
			granted = append(granted, userID)
		}
	}
	for _, userID := range granted {
		if err := s.recheck(ctx, userID); err != nil {
			return err
		}
	}
	return nil
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

	if s.principal.Kind == auth.KindConnection {
		// Checked inside the snapshot: a grant withdrawn a moment ago must not be
		// answered with the user's whole state.
		_, err := q.GetGrantedUser(ctx, sqlc.GetGrantedUserParams{ConnectionID: s.principal.CredentialID, UserID: userID})
		if db.IsNotFound(err) {
			return nil
		}
		if err != nil {
			return err
		}
	}
	seq, err := q.GetEventSeq(ctx, userID)
	if db.IsNotFound(err) {
		return nil
	}
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
		lists := make([]list.List, 0, len(rows))
		for _, row := range rows {
			lists = append(lists, list.View(row))
		}
		first.Lists = lists
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
			slice := items[min(index*size, len(items)):min((index+1)*size, len(items))]
			if slice == nil {
				slice = []item.Item{}
			}
			part.Items = slice
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
