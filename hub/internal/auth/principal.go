package auth

import (
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
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
