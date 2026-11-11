// Package web serves the browser frontend. Until the dashboard exists this is a placeholder page.
package web

import (
	_ "embed"

	"github.com/gofiber/fiber/v3"
)

//go:embed index.html
var index []byte

// Handler answers every non-API path with the page, so client-side routes work on reload.
func Handler() fiber.Handler {
	return func(c fiber.Ctx) error {
		c.Set(fiber.HeaderContentType, fiber.MIMETextHTMLCharsetUTF8)
		return c.Send(index)
	}
}
