package server

import (
	"bytes"
	"compress/gzip"
	"embed"
	"io/fs"
	"mime"
	"path"
	"strings"
	"sync"

	"github.com/gofiber/fiber/v3"
)

// Filled by "make web". Without it the binary serves the placeholder page.
//
//go:embed all:webdist
var webDist embed.FS

//go:embed index.html
var webPlaceholder []byte

const (
	pagePolicy = "default-src 'self'; connect-src 'self' %s; img-src 'self' data:; " +
		"style-src 'self'; object-src 'none'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'"
	placeholderPolicy = "default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'none'"

	// Below this a response fits one packet either way.
	compressFrom = 1024
)

var compressible = map[string]bool{
	".html": true, ".js": true, ".css": true, ".svg": true, ".json": true, ".txt": true, ".map": true,
}

func embeddedWeb() fs.FS {
	files, err := fs.Sub(webDist, "webdist")
	if err != nil {
		panic(err)
	}
	return files
}

// packed keeps the compressed form of the files, each made on first use.
type packed struct {
	mu    sync.Mutex
	files map[string][]byte
}

func (p *packed) of(name string, plain []byte) []byte {
	p.mu.Lock()
	defer p.mu.Unlock()
	if data, ok := p.files[name]; ok {
		return data
	}
	var buf bytes.Buffer
	w, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	_, _ = w.Write(plain)
	_ = w.Close()
	p.files[name] = buf.Bytes()
	return p.files[name]
}

func acceptsGzip(header string) bool {
	for _, part := range strings.Split(header, ",") {
		coding, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		if !strings.EqualFold(strings.TrimSpace(coding), "gzip") {
			continue
		}
		q := strings.TrimSpace(params)
		return q != "q=0" && q != "q=0.0" && q != "q=0.00" && q != "q=0.000"
	}
	return false
}

// The browser's own WebSocket address of the hub. Older browsers do not count it under 'self'.
func socketOrigin(publicURL string) string {
	if rest, ok := strings.CutPrefix(publicURL, "https://"); ok {
		return "wss://" + rest
	}
	if rest, ok := strings.CutPrefix(publicURL, "http://"); ok {
		return "ws://" + rest
	}
	return ""
}

// WebHandler serves the dashboard's files and answers every other path with its page, so
// client-side routes work on reload.
func WebHandler(files fs.FS, publicURL string) fiber.Handler {
	index, err := fs.ReadFile(files, "index.html")
	policy := strings.Replace(pagePolicy, "%s", socketOrigin(publicURL), 1)
	if err != nil {
		index, policy = webPlaceholder, placeholderPolicy
	}
	zipped := &packed{files: map[string][]byte{}}
	send := func(c fiber.Ctx, name string, data []byte) error {
		c.Set(fiber.HeaderVary, fiber.HeaderAcceptEncoding)
		if len(data) >= compressFrom && compressible[path.Ext(name)] && acceptsGzip(c.Get(fiber.HeaderAcceptEncoding)) {
			c.Set(fiber.HeaderContentEncoding, "gzip")
			return c.Send(zipped.of(name, data))
		}
		return c.Send(data)
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
				return send(c, name, data)
			}
		}
		if strings.HasPrefix(name, "assets/") {
			return fiber.ErrNotFound
		}
		c.Set(fiber.HeaderContentType, fiber.MIMETextHTMLCharsetUTF8)
		c.Set(fiber.HeaderCacheControl, "no-cache")
		c.Set(fiber.HeaderContentSecurityPolicy, policy)
		c.Set(fiber.HeaderReferrerPolicy, "same-origin")
		return send(c, "index.html", index)
	}
}
