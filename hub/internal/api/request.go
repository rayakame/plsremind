package api

import (
	"encoding/json"
	"net/http"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
)

// Body decodes the JSON request body into v. An empty body counts as {}.
func Body(c fiber.Ctx, v any) error {
	raw := c.Body()
	if len(raw) == 0 {
		raw = []byte("{}")
	}
	if hasEscapedNUL(raw) {
		return Field("body", "Text must not contain NUL characters.")
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return NewError(http.StatusBadRequest, "invalid_json", "The request body is not valid JSON for this endpoint.")
	}
	return nil
}

// ParamUUID reads a UUID path parameter. Anything else is treated as an unknown resource.
func ParamUUID(c fiber.Ctx, name string) (uuid.UUID, error) {
	id, err := uuid.Parse(c.Params(name))
	if err != nil {
		return uuid.Nil, NotFound()
	}
	return id, nil
}

// hasEscapedNUL finds a \u0000 escape in raw JSON. PostgreSQL cannot store NUL in text,
// and a raw NUL byte is already invalid JSON.
func hasEscapedNUL(raw []byte) bool {
	for i := 0; i < len(raw)-1; i++ {
		if raw[i] != '\\' {
			continue
		}
		if raw[i+1] == 'u' && i+6 <= len(raw) && string(raw[i+2:i+6]) == "0000" {
			return true
		}
		i++
	}
	return false
}
