package httpapi

import (
	"github.com/gofiber/fiber/v3"

	"github.com/rayakame/plsremind/hub/internal/apierr"
	"github.com/rayakame/plsremind/hub/internal/deps"
)

var Version = "dev"

const bodyLimit = 1 << 20

func New(d *deps.Deps) *fiber.App {
	app := fiber.New(fiber.Config{
		AppName:      "plsremind",
		BodyLimit:    bodyLimit,
		ErrorHandler: apierr.Handler,
	})

	api := app.Group("/api/v1")
	api.Get("/info", info(d))

	app.Use("/api", func(fiber.Ctx) error { return fiber.ErrNotFound })
	return app
}
