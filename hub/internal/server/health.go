package server

import (
	"context"
	"sync"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5/pgxpool"
)

// /readyz has no login and no rate limit, so a flood of probes must not reach the pool.
type readiness struct {
	mu      sync.Mutex
	ping    func(context.Context) error
	now     func() time.Time
	checked time.Time
	ok      bool
}

func (r *readiness) ready(ctx context.Context) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.checked.IsZero() && r.now().Sub(r.checked) < time.Second {
		return r.ok
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	r.ok = r.ping(ctx) == nil
	r.checked = r.now()
	return r.ok
}

func registerHealth(app *fiber.App, pool *pgxpool.Pool) {
	r := &readiness{ping: pool.Ping, now: time.Now}
	app.Get("/healthz", func(c fiber.Ctx) error {
		return c.SendString("ok")
	})
	app.Get("/readyz", func(c fiber.Ctx) error {
		if !r.ready(c.Context()) {
			return c.Status(fiber.StatusServiceUnavailable).SendString("unavailable")
		}
		return c.SendString("ok")
	})
}
