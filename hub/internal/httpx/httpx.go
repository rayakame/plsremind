package httpx

import (
	"encoding/json"
	"net/http"

	"github.com/gofiber/fiber/v3"

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
