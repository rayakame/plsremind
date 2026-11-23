package server

import (
	"strconv"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/rayakame/plsremind/hub/internal/metrics"
)

var knownMethods = map[string]bool{
	fiber.MethodGet: true, fiber.MethodHead: true, fiber.MethodPost: true, fiber.MethodPut: true,
	fiber.MethodPatch: true, fiber.MethodDelete: true, fiber.MethodOptions: true,
}

func measure(m *metrics.Metrics) fiber.Handler {
	return func(c fiber.Ctx) error {
		start := time.Now()
		if err := c.Next(); err != nil {
			// Handled here, so that the status counted is the one the client gets.
			if err := c.App().ErrorHandler(c, err); err != nil {
				_ = c.SendStatus(fiber.StatusInternalServerError)
			}
		}
		method := c.Method()
		if !knownMethods[method] {
			method = "other"
		}
		route := routeOf(c)
		m.Requests.WithLabelValues(method, route, strconv.Itoa(c.Response().StatusCode())).Inc()
		m.Durations.WithLabelValues(method, route).Observe(time.Since(start).Seconds())
		return nil
	}
}

func routeOf(c fiber.Ctx) string {
	switch path := c.Route().Path; path {
	case "/*":
		return "web"
	case "", "/", "/api":
		return "unmatched"
	default:
		return path
	}
}
