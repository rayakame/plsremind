package server

import (
	"strings"

	"github.com/gofiber/fiber/v3"

	"github.com/rayakame/plsremind/hub/internal/core"
)

func info(d *core.Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		settings, err := d.Q().GetSettings(c.Context())
		if err != nil {
			return err
		}
		q := d.Q()
		users, err := q.CountUsers(c.Context())
		if err != nil {
			return err
		}
		methods, err := q.ListLoginMethods(c.Context())
		if err != nil {
			return err
		}
		loginMethods := make([]fiber.Map, 0, len(methods))
		for _, m := range methods {
			loginMethods = append(loginMethods, fiber.Map{"key": m.Key, "type": m.Type, "name": m.Name})
		}
		return c.JSON(fiber.Map{
			"name":           settings.Name,
			"version":        Version,
			"setup_required": users == 0,
			"registration":   settings.Registration,
			"login_methods":  loginMethods,
			// http becomes ws and https becomes wss.
			"gateway_url": "ws" + strings.TrimPrefix(d.Cfg.PublicURL, "http") + "/api/v1/gateway",
		})
	}
}
