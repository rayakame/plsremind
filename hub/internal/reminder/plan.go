package reminder

import (
	"errors"
	"slices"
	"time"

	"github.com/rayakame/plsremind/hub/internal/api"
)

const (
	maxBefore = 10
	minOffset = time.Minute
	maxOffset = 365 * 24 * time.Hour

	// FollowUpWindow is how long the hub keeps nagging about an overdue deadline.
	FollowUpWindow = 7 * 24 * time.Hour
)

var Priorities = []string{"low", "normal", "high"}

var followUps = []string{"never", "daily", "twice_daily"}

type Plan struct {
	Before   []time.Duration
	FollowUp string
}

const day = 24 * time.Hour

var DefaultPlans = map[string]Plan{
	"low":    {Before: []time.Duration{day}, FollowUp: "never"},
	"normal": {Before: []time.Duration{3 * day, day}, FollowUp: "daily"},
	"high":   {Before: []time.Duration{7 * day, 3 * day, day, 3 * time.Hour}, FollowUp: "twice_daily"},
}

func ValidPriority(s string) bool {
	return slices.Contains(Priorities, s)
}

func ValidFollowUp(s string) bool {
	return slices.Contains(followUps, s)
}

var ErrBefore = errors.New(`Must be at most 10 durations between 1m and 365d, for example ["3d", "1d"].`)

// NormalizeBefore parses reminder offsets, drops duplicates and sorts them longest first.
func NormalizeBefore(in []string) ([]time.Duration, error) {
	if len(in) > maxBefore {
		return nil, ErrBefore
	}
	out := make([]time.Duration, 0, len(in))
	for _, s := range in {
		d, err := api.ParseDuration(s)
		if err != nil || d < minOffset || d > maxOffset {
			return nil, ErrBefore
		}
		if !slices.Contains(out, d) {
			out = append(out, d)
		}
	}
	slices.SortFunc(out, func(a, b time.Duration) int { return int(b - a) })
	return out, nil
}

// FormatBefore renders offsets the way the API returns them.
func FormatBefore(in []time.Duration) []string {
	out := make([]string, 0, len(in))
	for _, d := range in {
		out = append(out, api.FormatDuration(d))
	}
	return out
}
