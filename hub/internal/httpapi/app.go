package httpapi

import (
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/rayakame/plsremind/hub/internal/account"
	"github.com/rayakame/plsremind/hub/internal/apierr"
	"github.com/rayakame/plsremind/hub/internal/auth"
	"github.com/rayakame/plsremind/hub/internal/connection"
	"github.com/rayakame/plsremind/hub/internal/deps"
	"github.com/rayakame/plsremind/hub/internal/item"
	"github.com/rayakame/plsremind/hub/internal/list"
	"github.com/rayakame/plsremind/hub/internal/plan"
	"github.com/rayakame/plsremind/hub/internal/ratelimit"
	"github.com/rayakame/plsremind/hub/internal/reminder"
	"github.com/rayakame/plsremind/hub/internal/subitem"
	"github.com/rayakame/plsremind/hub/internal/token"
)

var Version = "dev"

const bodyLimit = 1 << 20

func New(d *deps.Deps) *fiber.App {
	app := fiber.New(fiber.Config{
		AppName:      "plsremind",
		BodyLimit:    bodyLimit,
		ErrorHandler: apierr.Handler,
	})

	if d.RateAuth == nil {
		d.RateAuth = ratelimit.New(120, time.Minute, d.Now)
	}
	if d.RateConn == nil {
		// A connection serves every user of the instance through one token.
		d.RateConn = ratelimit.New(1200, time.Minute, d.Now)
	}
	if d.RateAnon == nil {
		d.RateAnon = ratelimit.New(20, time.Minute, d.Now)
	}

	api := app.Group("/api/v1", auth.Middleware(d), ratelimit.Middleware(d.Now, func(c fiber.Ctx) (*ratelimit.Limiter, string) {
		if p, ok := auth.From(c); ok {
			if p.Kind == auth.KindConnection {
				return d.RateConn, p.CredentialID.String()
			}
			return d.RateAuth, p.CredentialID.String()
		}
		return d.RateAnon, c.IP()
	}))
	api.Get("/info", info(d))
	user := api.Group("/users/:user", auth.ResolveUser(d))

	admin := api.Group("/admin", auth.RequireAdmin())

	account.Register(api, user, admin, d)
	token.Register(user, d)
	list.Register(user, d)
	plan.Register(user, d)
	item.Register(user, d)
	subitem.Register(user, d)
	reminder.Register(user, d)
	connection.Register(api, user, admin, d)

	app.Use("/api", func(fiber.Ctx) error { return fiber.ErrNotFound })
	return app
}
