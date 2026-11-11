package httpapi

import (
	"github.com/gofiber/fiber/v3"

	"github.com/rayakame/plsremind/hub/internal/deps"
)

func info(d *deps.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		settings, err := d.Q().GetSettings(c.Context())
		if err != nil {
			return err
		}
		return c.JSON(fiber.Map{
			"name":    settings.Name,
			"version": Version,
		})
	}
}
