package auth

import (
	"net/http"

	"github.com/gofiber/fiber/v3"

	"github.com/rayakame/plsremind/hub/internal/api"
	"github.com/rayakame/plsremind/hub/internal/events"
)

// NonceHeader carries a value of the client's choosing that comes back in every
// gateway event the request causes.
const NonceHeader = "X-Request-Nonce"

const maxNonceLen = 64

func validNonce(s string) bool {
	if len(s) == 0 || len(s) > maxNonceLen {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < 0x21 || s[i] > 0x7e {
			return false
		}
	}
	return true
}

// Nonce checks the nonce of a request, if it has one, and keeps it for Actor.
func Nonce() fiber.Handler {
	return func(c fiber.Ctx) error {
		if _, sent := c.GetReqHeaders()[NonceHeader]; sent {
			// Copied: header values live in a buffer that the next request reuses.
			nonce := string(c.Request().Header.Peek(NonceHeader))
			if !validNonce(nonce) {
				return api.NewError(http.StatusBadRequest, "invalid_nonce", "The "+NonceHeader+" header must be 1 to 64 printable ASCII characters.")
			}
			c.Locals(nonceKey, nonce)
		}
		return c.Next()
	}
}

// Actor names the credential behind the request and the nonce it sent, for the event log.
func Actor(c fiber.Ctx) events.Actor {
	p, _ := From(c)
	a := ActorFrom(p)
	if nonce, ok := c.Locals(nonceKey).(string); ok {
		a.Nonce = &nonce
	}
	return a
}
