// Package server puts the hub together: the HTTP API, the gateway and the browser frontend.
package server

import (
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/rayakame/plsremind/hub/internal/access"
	"github.com/rayakame/plsremind/hub/internal/account"
	"github.com/rayakame/plsremind/hub/internal/api"
	"github.com/rayakame/plsremind/hub/internal/auth"
	"github.com/rayakame/plsremind/hub/internal/core"
	"github.com/rayakame/plsremind/hub/internal/delivery"
	"github.com/rayakame/plsremind/hub/internal/gateway"
	"github.com/rayakame/plsremind/hub/internal/reminder"
	"github.com/rayakame/plsremind/hub/internal/todo"
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

// Server is the HTTP application together with the gateway it serves.
type Server struct {
	App     *fiber.App
	Gateway *gateway.Hub
}

func New(d *core.Deps) *Server {
	d.Gateway = d.Gateway.WithDefaults()
	if d.ReadTimeout == 0 {
		d.ReadTimeout = 30 * time.Second
	}
	if d.IdleTimeout == 0 {
		d.IdleTimeout = 2 * time.Minute
	}
	cfg := fiber.Config{
		AppName:   "plsremind",
		BodyLimit: bodyLimit,
		// Access checks compare paths as written, so routing must not be more lenient than they are.
		CaseSensitive: true,
		// A client that stops sending must not hold a connection open forever.
		ReadTimeout:  d.ReadTimeout,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  d.IdleTimeout,
		ErrorHandler: api.ErrorHandler,
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
		d.RateAuth = api.NewLimiter(120, time.Minute, d.Now)
	}
	if d.RateConn == nil {
		// A connection serves every user of the instance through one token.
		d.RateConn = api.NewLimiter(1200, time.Minute, d.Now)
	}
	if d.RateAnon == nil {
		d.RateAnon = api.NewLimiter(20, time.Minute, d.Now)
	}

	root := app.Group("/api/v1", rejectNUL, auth.Middleware(d), api.RateLimit(d.Now, func(c fiber.Ctx) (*api.Limiter, string) {
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
	root.Get("/info", info(d))
	user := root.Group("/users/:user", auth.ResolveUser(d))

	admin := root.Group("/admin", auth.RequireAdmin())

	account.Register(root, user, admin, d)
	access.RegisterTokens(user, d)
	reminder.Register(user, d)
	todo.Register(user, d)
	access.RegisterConnections(root, user, admin, d)
	delivery.Register(root, user, d)
	hub := gateway.NewHub(d)
	hub.Register(root)

	app.Use("/api", func(fiber.Ctx) error { return fiber.ErrNotFound })
	app.Get("/*", webHandler())
	return &Server{App: app, Gateway: hub}
}
