// Package eventlog records every change to a user's data so the gateway can replay it.
package eventlog

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/rayakame/plsremind/hub/internal/auth"
	"github.com/rayakame/plsremind/hub/internal/db/sqlc"
)

type Actor struct {
	Kind string
	ID   uuid.UUID
}

func ActorFrom(p auth.Principal) Actor {
	kind := "session"
	switch p.Kind {
	case auth.KindPersonalToken:
		kind = "personal_token"
	case auth.KindConnection:
		kind = "connection"
	}
	return Actor{Kind: kind, ID: p.CredentialID}
}

// Deleted is the payload of every *.deleted event.
type Deleted struct {
	ID uuid.UUID `json:"id"`
}

// Append writes one event with the user's next sequence number. It must run in the
// transaction that makes the change, so the log never disagrees with the data.
func Append(ctx context.Context, q *sqlc.Queries, now time.Time, userID uuid.UUID, a Actor, typ string, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	seq, err := q.NextEventSeq(ctx, userID)
	if err != nil {
		return err
	}
	return q.InsertEvent(ctx, sqlc.InsertEventParams{
		UserID:    userID,
		Seq:       seq,
		Type:      typ,
		Payload:   raw,
		ActorKind: a.Kind,
		ActorID:   a.ID,
		CreatedAt: now,
	})
}
