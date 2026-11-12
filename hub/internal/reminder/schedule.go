// Package reminder decides when the hub reminds about an item, following the plan
// the user keeps for each priority.
package reminder

import (
	"slices"
	"time"

	"github.com/google/uuid"
)

type Kind string

const (
	KindBefore   Kind = "before"
	KindDue      Kind = "due"
	KindFollowUp Kind = "follow_up"
	KindManual   Kind = "manual"
)

// Planned is one reminder that will fire.
type Planned struct {
	Kind     Kind
	Offset   time.Duration // before only: how long before the due time
	ManualID uuid.UUID     // manual only
	// At is when the reminder fires. For is the moment it was meant for, which
	// differs when quiet hours moved it.
	At  time.Time
	For time.Time
}

// Quiet is the user's daily quiet window, as offsets from local midnight.
type Quiet struct {
	Start, End time.Duration
}

type Manual struct {
	ID uuid.UUID
	At time.Time
}

type ScheduleInput struct {
	Open bool
	Due  *time.Time
	// Before is already resolved: the item's override, else the user's plan, else the default.
	Before   []time.Duration
	FollowUp string
	// Off is the item override that silences everything the hub would send on its own.
	Off    bool
	Manual []Manual
	Quiet  *Quiet
	Loc    *time.Location
}

var followUpEvery = map[string]time.Duration{
	"daily":       24 * time.Hour,
	"twice_daily": 12 * time.Hour,
}

// kindRank orders reminders that land on the same moment: the one that matters most first.
func kindRank(k Kind) int {
	return slices.Index([]Kind{KindDue, KindFollowUp, KindBefore}, k)
}

// Compute returns every reminder of an item that is still ahead of now, earliest first.
func Compute(in ScheduleInput, now time.Time) []Planned {
	return slices.DeleteFunc(ComputeAll(in), func(p Planned) bool { return !p.At.After(now) })
}

// ComputeAll returns every reminder an item has, past ones included, earliest first.
func ComputeAll(in ScheduleInput) []Planned {
	if !in.Open {
		return nil
	}
	var planned []Planned
	if in.Due != nil && !in.Off {
		due := *in.Due
		for _, offset := range in.Before {
			p := Planned{Kind: KindBefore, Offset: offset, For: due.Add(-offset)}
			p.At = in.afterQuiet(p.For)
			// Moved to the deadline or past it, "3 hours before" no longer means anything.
			if p.At.Before(due) {
				planned = append(planned, p)
			}
		}
		planned = append(planned, Planned{Kind: KindDue, At: due, For: due})
		if every, ok := followUpEvery[in.FollowUp]; ok {
			for t := due.Add(every); !t.After(due.Add(FollowUpWindow)); t = t.Add(every) {
				planned = append(planned, Planned{Kind: KindFollowUp, At: in.afterQuiet(t), For: t})
			}
		}
		planned = collapse(planned)
	}
	for _, m := range in.Manual {
		planned = append(planned, Planned{Kind: KindManual, ManualID: m.ID, At: m.At, For: m.At})
	}

	slices.SortStableFunc(planned, func(a, b Planned) int { return a.At.Compare(b.At) })
	return planned
}

// collapse keeps one reminder per moment, so that quiet hours ending do not
// release several reminders about the same item at once.
func collapse(in []Planned) []Planned {
	slices.SortStableFunc(in, func(a, b Planned) int {
		if c := a.At.Compare(b.At); c != 0 {
			return c
		}
		if c := kindRank(a.Kind) - kindRank(b.Kind); c != 0 {
			return c
		}
		return int(a.Offset - b.Offset)
	})
	return slices.CompactFunc(in, func(a, b Planned) bool { return a.At.Equal(b.At) })
}

// afterQuiet returns t, or the end of the quiet window that t falls into.
func (in ScheduleInput) afterQuiet(t time.Time) time.Time {
	if in.Quiet == nil {
		return t
	}
	local := t.In(in.Loc)
	sinceMidnight := time.Duration(local.Hour())*time.Hour + time.Duration(local.Minute())*time.Minute +
		time.Duration(local.Second())*time.Second + time.Duration(local.Nanosecond())
	start, end := in.Quiet.Start, in.Quiet.End

	endsNextDay := false
	switch {
	case start < end:
		if sinceMidnight < start || sinceMidnight >= end {
			return t
		}
	case sinceMidnight >= start:
		endsNextDay = true
	case sinceMidnight >= end:
		return t
	}
	y, m, d := local.Date()
	if endsNextDay {
		d++
	}
	// Built from the wall clock, so the window ends at the same local time on days the clocks change.
	return time.Date(y, m, d, int(end/time.Hour), int(end%time.Hour/time.Minute), 0, 0, in.Loc)
}

// QuietUntil reports whether t falls into the quiet window and, if so, when the window ends.
func QuietUntil(quiet *Quiet, loc *time.Location, t time.Time) (time.Time, bool) {
	end := ScheduleInput{Quiet: quiet, Loc: loc}.afterQuiet(t)
	return end, !end.Equal(t)
}
