package api

import (
	"errors"
	"math"
	"strconv"
	"strings"
	"time"
)

var durationUnits = []struct {
	suffix byte
	size   time.Duration
}{
	{'d', 24 * time.Hour},
	{'h', time.Hour},
	{'m', time.Minute},
}

var ErrDuration = errors.New(`invalid duration, expected a form like "3d", "1d12h" or "90m"`)

// ParseDuration accepts parts in descending unit order, each unit at most once.
func ParseDuration(s string) (time.Duration, error) {
	var total time.Duration
	rest := s
	next := 0
	for rest != "" {
		digits := 0
		for digits < len(rest) && rest[digits] >= '0' && rest[digits] <= '9' {
			digits++
		}
		if digits == 0 || digits == len(rest) {
			return 0, ErrDuration
		}
		n, err := strconv.ParseInt(rest[:digits], 10, 32)
		if err != nil {
			return 0, ErrDuration
		}
		unit := -1
		for i := next; i < len(durationUnits); i++ {
			if durationUnits[i].suffix == rest[digits] {
				unit = i
				break
			}
		}
		if unit < 0 {
			return 0, ErrDuration
		}
		// A count this large would wrap around and come out as a short, valid-looking duration.
		if n > int64((math.MaxInt64-total)/durationUnits[unit].size) {
			return 0, ErrDuration
		}
		total += time.Duration(n) * durationUnits[unit].size
		next = unit + 1
		rest = rest[digits+1:]
	}
	if total <= 0 {
		return 0, ErrDuration
	}
	return total, nil
}

// FormatDuration renders d with the largest units first. Seconds are dropped.
func FormatDuration(d time.Duration) string {
	var b strings.Builder
	for _, u := range durationUnits {
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
