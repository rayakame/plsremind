package api

import (
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gofiber/fiber/v3"
)

type window struct {
	count int
	reset time.Time
}

// Limiter counts requests per key in fixed windows, in memory.
type Limiter struct {
	limit  int
	window time.Duration
	now    func() time.Time

	mu      sync.Mutex
	windows map[string]*window
	sweepAt time.Time
}

func NewLimiter(limit int, every time.Duration, now func() time.Time) *Limiter {
	return &Limiter{limit: limit, window: every, now: now, windows: map[string]*window{}}
}

func (l *Limiter) Limit() int { return l.limit }

// Allow counts one request for key and reports what is left of its window.
func (l *Limiter) Allow(key string) (remaining int, reset time.Time, ok bool) {
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()

	if !now.Before(l.sweepAt) {
		for k, w := range l.windows {
			if !now.Before(w.reset) {
				delete(l.windows, k)
			}
		}
		l.sweepAt = now.Add(l.window)
	}

	w := l.windows[key]
	if w == nil || !now.Before(w.reset) {
		w = &window{reset: now.Add(l.window)}
		// The key may point into a request buffer that is reused once the request ends.
		l.windows[strings.Clone(key)] = w
	}
	if w.count >= l.limit {
		return 0, w.reset, false
	}
	w.count++
	return l.limit - w.count, w.reset, true
}

// Middleware applies pick's limiter and key to every request and reports the state in headers.
func RateLimit(now func() time.Time, pick func(c fiber.Ctx) (*Limiter, string)) fiber.Handler {
	return func(c fiber.Ctx) error {
		limiter, key := pick(c)
		if limiter == nil {
			return c.Next()
		}
		remaining, reset, ok := limiter.Allow(key)
		c.Set("X-RateLimit-Limit", strconv.Itoa(limiter.limit))
		c.Set("X-RateLimit-Remaining", strconv.Itoa(remaining))
		c.Set("X-RateLimit-Reset", strconv.FormatInt(reset.Unix(), 10))
		if !ok {
			wait := int(reset.Sub(now()).Round(time.Second) / time.Second)
			c.Set(fiber.HeaderRetryAfter, strconv.Itoa(max(wait, 1)))
			return NewError(http.StatusTooManyRequests, "rate_limited", "Too many requests. Try again later.")
		}
		return c.Next()
	}
}
