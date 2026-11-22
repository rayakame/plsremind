// Package events records every change to a user's data so the gateway can replay
// it, and tells the gateway that something changed.
package events

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/rayakame/plsremind/hub/internal/store/sqlc"
)

// Actor is who made a change. The zero Actor stands for the hub itself.
type Actor struct {
	Kind string
	ID   uuid.UUID
	// Nonce is what the client sent with the request, to recognise its own change by.
	Nonce *string
}

// Deleted is the payload of every *.deleted event. ItemID names the parent of a
// subitem or manual reminder, so a client can find what to drop without searching.
type Deleted struct {
	ID     uuid.UUID  `json:"id"`
	ItemID *uuid.UUID `json:"item_id,omitempty"`
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
	params := sqlc.InsertEventParams{
		UserID:    userID,
		Seq:       seq,
		Type:      typ,
		Payload:   raw,
		Nonce:     a.Nonce,
		CreatedAt: now,
	}
	if a.Kind != "" {
		params.ActorKind = &a.Kind
		params.ActorID = &a.ID
	}
	if err := q.InsertEvent(ctx, params); err != nil {
		return err
	}
	return NotifyUser(ctx, q, SignalEvent, userID)
}
