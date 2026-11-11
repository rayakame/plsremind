package httpx

import (
	"encoding/json"
	"net/http"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"github.com/rayakame/plsremind/hub/internal/apierr"
)

// Body decodes the JSON request body into v. An empty body counts as {}.
func Body(c fiber.Ctx, v any) error {
	raw := c.Body()
	if len(raw) == 0 {
		raw = []byte("{}")
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return apierr.New(http.StatusBadRequest, "invalid_json", "The request body is not valid JSON for this endpoint.")
	}
	return nil
}

// ParamUUID reads a UUID path parameter. Anything else is treated as an unknown resource.
func ParamUUID(c fiber.Ctx, name string) (uuid.UUID, error) {
	id, err := uuid.Parse(c.Params(name))
	if err != nil {
		return uuid.Nil, apierr.NotFound()
	}
	return id, nil
}
