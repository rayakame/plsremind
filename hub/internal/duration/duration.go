// Package duration reads and writes the API's duration format: "3d", "1d12h", "90m".
package duration

import (
	"errors"
	"math"
	"strconv"
	"strings"
	"time"
)

var units = []struct {
	suffix byte
	size   time.Duration
}{
	{'d', 24 * time.Hour},
	{'h', time.Hour},
	{'m', time.Minute},
}

var ErrInvalid = errors.New(`invalid duration, expected a form like "3d", "1d12h" or "90m"`)

// Parse accepts parts in descending unit order, each unit at most once.
func Parse(s string) (time.Duration, error) {
	var total time.Duration
	rest := s
	next := 0
	for rest != "" {
		digits := 0
		for digits < len(rest) && rest[digits] >= '0' && rest[digits] <= '9' {
			digits++
		}
		if digits == 0 || digits == len(rest) {
			return 0, ErrInvalid
		}
		n, err := strconv.ParseInt(rest[:digits], 10, 32)
		if err != nil {
			return 0, ErrInvalid
		}
		unit := -1
		for i := next; i < len(units); i++ {
			if units[i].suffix == rest[digits] {
				unit = i
				break
			}
		}
		if unit < 0 {
			return 0, ErrInvalid
		}
		// A count this large would wrap around and come out as a short, valid-looking duration.
		if n > int64((math.MaxInt64-total)/units[unit].size) {
			return 0, ErrInvalid
		}
		total += time.Duration(n) * units[unit].size
		next = unit + 1
		rest = rest[digits+1:]
	}
	if total <= 0 {
		return 0, ErrInvalid
	}
	return total, nil
}

// Format renders d with the largest units first. Seconds are dropped.
func Format(d time.Duration) string {
	var b strings.Builder
	for _, u := range units {
		if n := d / u.size; n > 0 {
			b.WriteString(strconv.FormatInt(int64(n), 10))
			b.WriteByte(u.suffix)
			d -= n * u.size
		}
	}
	if b.Len() == 0 {
		return "0m"
	}
	return b.String()
}
