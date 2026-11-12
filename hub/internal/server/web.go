package server

import (
	_ "embed"

	"github.com/gofiber/fiber/v3"
)

// Until the dashboard exists this is a placeholder page.
//
//go:embed index.html
var webIndex []byte

// Handler answers every non-API path with the page, so client-side routes work on reload.
func webHandler() fiber.Handler {
	return func(c fiber.Ctx) error {
		c.Set(fiber.HeaderContentType, fiber.MIMETextHTMLCharsetUTF8)
		return c.Send(webIndex)
	}
}
