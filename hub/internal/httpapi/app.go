package httpapi

import (
	"github.com/gofiber/fiber/v3"

	"github.com/rayakame/plsremind/hub/internal/account"
	"github.com/rayakame/plsremind/hub/internal/apierr"
	"github.com/rayakame/plsremind/hub/internal/auth"
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

	api := app.Group("/api/v1", auth.Middleware(d))
	api.Get("/info", info(d))
	user := api.Group("/users/:user", auth.ResolveUser(d))

	account.Register(api, user, d)

	app.Use("/api", func(fiber.Ctx) error { return fiber.ErrNotFound })
	return app
}
