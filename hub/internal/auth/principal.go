// Package auth finds out who makes a request and what they may reach.
package auth

import (
	"slices"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"github.com/rayakame/plsremind/hub/internal/events"
)

type Kind int

const (
	KindSession Kind = iota + 1
	KindPersonalToken
	KindConnection
)

// Principal is whoever authenticated the current request.
type Principal struct {
	Kind               Kind
	UserID             uuid.UUID
	Role               string
	CredentialID       uuid.UUID
	ConnectionProvider string
	// Intents is what the admin allows a connection to have. Empty for other kinds.
	Intents []string
}

// Intents are the kinds of data a client can ask for, in canonical order.
var Intents = []string{"users", "lists", "items", "plans", "deliveries"}

// HasIntent reports whether the caller may have the given kind of data. Sessions
// and personal tokens belong to the user and may have everything.
func HasIntent(p Principal, intent string) bool {
	return p.Kind != KindConnection || slices.Contains(p.Intents, intent)
}

// NormalizeIntents validates a list of intents and returns it without duplicates in canonical order.
func NormalizeIntents(in []string) ([]string, bool) {
	out := make([]string, 0, len(Intents))
	for _, known := range Intents {
		if slices.Contains(in, known) {
			out = append(out, known)
		}
	}
	for _, intent := range in {
		if !slices.Contains(Intents, intent) {
			return nil, false
		}
	}
	return out, len(out) > 0
}

type localKey int

const (
	principalKey localKey = iota
	targetKey
	failureKey
)

func From(c fiber.Ctx) (Principal, bool) {
	p, ok := c.Locals(principalKey).(Principal)
	return p, ok
}

// Target is the user that a /users/:user route operates on.
func Target(c fiber.Ctx) uuid.UUID {
	id, _ := c.Locals(targetKey).(uuid.UUID)
	return id
}

// ActorFrom names the credential behind a change for the event log.
func ActorFrom(p Principal) events.Actor {
	kind := "session"
	switch p.Kind {
	case KindPersonalToken:
		kind = "personal_token"
	case KindConnection:
		kind = "connection"
	}
	return events.Actor{Kind: kind, ID: p.CredentialID}
}
