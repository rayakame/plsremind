package httpapi

import (
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/rayakame/plsremind/hub/internal/account"
	"github.com/rayakame/plsremind/hub/internal/apierr"
	"github.com/rayakame/plsremind/hub/internal/auth"
	"github.com/rayakame/plsremind/hub/internal/connection"
	"github.com/rayakame/plsremind/hub/internal/delivery"
	"github.com/rayakame/plsremind/hub/internal/deps"
	"github.com/rayakame/plsremind/hub/internal/item"
	"github.com/rayakame/plsremind/hub/internal/list"
	"github.com/rayakame/plsremind/hub/internal/oauth"
	"github.com/rayakame/plsremind/hub/internal/plan"
	"github.com/rayakame/plsremind/hub/internal/ratelimit"
	"github.com/rayakame/plsremind/hub/internal/reminder"
	"github.com/rayakame/plsremind/hub/internal/schedule"
	"github.com/rayakame/plsremind/hub/internal/subitem"
	"github.com/rayakame/plsremind/hub/internal/token"
	"github.com/rayakame/plsremind/hub/internal/web"
)

var Version = "dev"

// rejectNUL keeps NUL bytes out of path and query values, which PostgreSQL cannot store in text.
func rejectNUL(c fiber.Ctx) error {
	if strings.Contains(c.OriginalURL(), "%00") {
		return fiber.NewError(fiber.StatusBadRequest, "The URL must not contain NUL characters.")
	}
	return c.Next()
}

const bodyLimit = 1 << 20

func New(d *deps.Deps) *fiber.App {
	cfg := fiber.Config{
		AppName:   "plsremind",
		BodyLimit: bodyLimit,
		// Access checks compare paths as written, so routing must not be more lenient than they are.
		CaseSensitive: true,
		// A client that stops sending must not hold a connection open forever.
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  2 * time.Minute,
		ErrorHandler: apierr.Handler,
	}
	if d.Cfg.ProxyHeader != "" {
		// Behind a reverse proxy every request comes from the proxy's address. The
		// header is only believed when the direct peer is on a local or private network.
		cfg.ProxyHeader = d.Cfg.ProxyHeader
		cfg.TrustProxy = true
		cfg.TrustProxyConfig = fiber.TrustProxyConfig{Loopback: true, Private: true}
		cfg.EnableIPValidation = true
	}
	app := fiber.New(cfg)

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

	api := app.Group("/api/v1", rejectNUL, auth.Middleware(d), ratelimit.Middleware(d.Now, func(c fiber.Ctx) (*ratelimit.Limiter, string) {
		// Logging in and signing up are always counted per address. Otherwise anyone
		// with an account could guess passwords on their own credential's larger budget.
		if path := c.Path(); path == "/api/v1/setup" || strings.HasPrefix(path, "/api/v1/auth/") {
			return d.RateAnon, c.IP()
		}
		if p, ok := auth.From(c); ok && !auth.Failed(c) {
			if p.Kind == auth.KindConnection {
				return d.RateConn, p.CredentialID.String()
			}
			return d.RateAuth, p.CredentialID.String()
		}
		return d.RateAnon, c.IP()
	}), auth.Reject())
	api.Get("/info", info(d))
	user := api.Group("/users/:user", auth.ResolveUser(d))

	admin := api.Group("/admin", auth.RequireAdmin())

	providers := oauth.NewService(d)
	account.Register(api, user, admin, d, providers)
	oauth.Register(api, user, admin, d, providers)
	token.Register(user, d)
	list.Register(user, d)
	plan.Register(user, d, schedule.SyncUser)
	item.Register(user, d, delivery.CancelForItem)
	subitem.Register(user, d)
	reminder.Register(user, d)
	connection.Register(api, user, admin, d)

	app.Use("/api", func(fiber.Ctx) error { return fiber.ErrNotFound })
	app.Get("/*", web.Handler())
	return app
}
