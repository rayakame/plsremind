package server

import (
	"context"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Ready reports whether the database answers within two seconds.
func Ready(ctx context.Context, pool *pgxpool.Pool) bool {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return pool.Ping(ctx) == nil
}

func registerHealth(app *fiber.App, pool *pgxpool.Pool) {
	app.Get("/healthz", func(c fiber.Ctx) error {
		return c.SendString("ok")
	})
	app.Get("/readyz", func(c fiber.Ctx) error {
		if !Ready(c.Context(), pool) {
			return c.Status(fiber.StatusServiceUnavailable).SendString("unavailable")
		}
		return c.SendString("ok")
	})
}
