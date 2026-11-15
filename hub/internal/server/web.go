package server

import (
	"embed"
	"io/fs"
	"mime"
	"path"
	"strings"

	"github.com/gofiber/fiber/v3"
)

// Filled by "make web". Without it the binary serves the placeholder page.
//
//go:embed all:webdist
var webDist embed.FS

//go:embed index.html
var webPlaceholder []byte

const (
	pagePolicy = "default-src 'self'; connect-src 'self' ws: wss:; img-src 'self' data:; " +
		"style-src 'self'; object-src 'none'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'"
	placeholderPolicy = "default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'none'"
)

func embeddedWeb() fs.FS {
	files, err := fs.Sub(webDist, "webdist")
	if err != nil {
		panic(err)
	}
	return files
}

// WebHandler serves the dashboard's files and answers every other path with its page, so
// client-side routes work on reload.
func WebHandler(files fs.FS) fiber.Handler {
	index, err := fs.ReadFile(files, "index.html")
	policy := pagePolicy
	if err != nil {
		index, policy = webPlaceholder, placeholderPolicy
	}
	return func(c fiber.Ctx) error {
		c.Set(fiber.HeaderXContentTypeOptions, "nosniff")
		name := strings.TrimPrefix(path.Clean("/"+c.Path()), "/")
		if name != "" && name != "index.html" && fs.ValidPath(name) {
			if data, err := fs.ReadFile(files, name); err == nil {
				kind := mime.TypeByExtension(path.Ext(name))
				if kind == "" {
					kind = fiber.MIMEOctetStream
				}
				c.Set(fiber.HeaderContentType, kind)
				if strings.HasPrefix(name, "assets/") {
					c.Set(fiber.HeaderCacheControl, "public, max-age=31536000, immutable")
				} else {
					c.Set(fiber.HeaderCacheControl, "no-cache")
				}
				return c.Send(data)
			}
		}
		if strings.HasPrefix(name, "assets/") {
			return fiber.ErrNotFound
		}
		c.Set(fiber.HeaderContentType, fiber.MIMETextHTMLCharsetUTF8)
		c.Set(fiber.HeaderCacheControl, "no-cache")
		c.Set(fiber.HeaderContentSecurityPolicy, policy)
		c.Set(fiber.HeaderReferrerPolicy, "same-origin")
		return c.Send(index)
	}
}
