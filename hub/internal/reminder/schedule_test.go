package reminder_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/rayakame/plsremind/hub/internal/reminder"
)

const (
	hour = time.Hour
	day  = 24 * time.Hour
)

var (
	now    = ts("2026-11-11T12:00:00Z") // a Wednesday
	due    = ts("2026-11-13T18:00:00Z") // the Friday after
	berlin = mustLoad("Europe/Berlin")
	night  = &reminder.Quiet{Start: 22 * hour, End: 8 * hour}
)

func ts(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func mustLoad(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return loc
}

func at(planned []reminder.Planned) []string {
	out := make([]string, 0, len(planned))
	for _, p := range planned {
		out = append(out, string(p.Kind)+" "+p.At.UTC().Format(time.RFC3339))
	}
	return out
}

func TestHighPriorityFromWednesday(t *testing.T) {
	got := reminder.Compute(reminder.ScheduleInput{
		Open: true, Due: &due, Loc: time.UTC,
		Before: []time.Duration{7 * day, 3 * day, day, 3 * hour}, FollowUp: "twice_daily",
	}, now)
	require.Len(t, got, 17)
	require.Equal(t, []string{
		"before 2026-11-12T18:00:00Z",
		"before 2026-11-13T15:00:00Z",
		"due 2026-11-13T18:00:00Z",
		"follow_up 2026-11-14T06:00:00Z",
	}, at(got)[:4])
	require.Equal(t, "follow_up 2026-11-20T18:00:00Z", at(got)[16])
	require.Equal(t, day, got[0].Offset)
	require.Equal(t, got[0].At, got[0].For)
}

func TestLowPriority(t *testing.T) {
	got := reminder.Compute(reminder.ScheduleInput{Open: true, Due: &due, Loc: time.UTC, Before: []time.Duration{day}, FollowUp: "never"}, now)
	require.Equal(t, []string{"before 2026-11-12T18:00:00Z", "due 2026-11-13T18:00:00Z"}, at(got))
}

func TestDailyFollowUpsRunForAWeek(t *testing.T) {
	got := reminder.Compute(reminder.ScheduleInput{Open: true, Due: &due, Loc: time.UTC, FollowUp: "daily"}, now)
	require.Len(t, got, 8)
	require.Equal(t, "follow_up 2026-11-14T18:00:00Z", at(got)[1])
	require.Equal(t, "follow_up 2026-11-20T18:00:00Z", at(got)[7])
}

func TestOverrideOff(t *testing.T) {
	manual := reminder.Manual{ID: uuid.New(), At: now.Add(2 * hour)}
	got := reminder.Compute(reminder.ScheduleInput{
		Open: true, Due: &due, Loc: time.UTC, Off: true,
		Before: []time.Duration{day}, FollowUp: "daily", Manual: []reminder.Manual{manual},
	}, now)
	require.Equal(t, []string{"manual 2026-11-11T14:00:00Z"}, at(got))
	require.Equal(t, manual.ID, got[0].ManualID)
}

func TestTodoWithoutDueOnlyManual(t *testing.T) {
	got := reminder.Compute(reminder.ScheduleInput{
		Open: true, Loc: time.UTC, Before: []time.Duration{day}, FollowUp: "daily",
		Manual: []reminder.Manual{{ID: uuid.New(), At: now.Add(2 * hour)}, {ID: uuid.New(), At: now.Add(-hour)}},
	}, now)
	require.Equal(t, []string{"manual 2026-11-11T14:00:00Z"}, at(got))
}

func TestDoneItemHasNothing(t *testing.T) {
	got := reminder.Compute(reminder.ScheduleInput{
		Open: false, Due: &due, Loc: time.UTC, Before: []time.Duration{day}, FollowUp: "daily",
		Manual: []reminder.Manual{{ID: uuid.New(), At: now.Add(2 * hour)}},
	}, now)
	require.Empty(t, got)
}

func TestPastTimesAreSkipped(t *testing.T) {
	later := due.Add(3 * day)
	got := reminder.Compute(reminder.ScheduleInput{
		Open: true, Due: &due, Loc: time.UTC, Before: []time.Duration{day}, FollowUp: "twice_daily",
	}, later)
	require.Len(t, got, 8)
	require.Equal(t, "follow_up 2026-11-17T06:00:00Z", at(got)[0], "a time equal to now is already past")
}

func TestQuietHoursShiftBeforeAndFollowUp(t *testing.T) {
	noon := ts("2026-11-13T11:00:00Z")    // 12:00 in Berlin
	lateEve := ts("2026-11-13T22:30:00Z") // 23:30 in Berlin
	before := reminder.Compute(reminder.ScheduleInput{
		Open: true, Due: &noon, Loc: berlin, Quiet: night, Before: []time.Duration{6 * hour}, FollowUp: "never",
	}, now)
	require.Equal(t, []string{"before 2026-11-13T07:00:00Z", "due 2026-11-13T11:00:00Z"}, at(before))
	require.Equal(t, ts("2026-11-13T05:00:00Z"), before[0].For.UTC(), "For keeps the time the reminder was meant for")

	follow := reminder.Compute(reminder.ScheduleInput{Open: true, Due: &lateEve, Loc: berlin, Quiet: night, FollowUp: "daily"}, now)
	require.Equal(t, "due 2026-11-13T22:30:00Z", at(follow)[0])
	require.Equal(t, "follow_up 2026-11-15T07:00:00Z", at(follow)[1], "23:30 on Saturday waits until 08:00 on Sunday")
}

func TestQuietHoursLeaveDueAndManual(t *testing.T) {
	early := ts("2026-11-13T02:00:00Z") // 03:00 in Berlin
	got := reminder.Compute(reminder.ScheduleInput{
		Open: true, Due: &early, Loc: berlin, Quiet: night, FollowUp: "never",
		Manual: []reminder.Manual{{ID: uuid.New(), At: ts("2026-11-12T03:00:00Z")}},
	}, now)
	require.Equal(t, []string{"manual 2026-11-12T03:00:00Z", "due 2026-11-13T02:00:00Z"}, at(got))
}

func TestBeforeShiftedPastDueIsDropped(t *testing.T) {
	seven := ts("2026-11-13T06:00:00Z") // 07:00 in Berlin
	got := reminder.Compute(reminder.ScheduleInput{
		Open: true, Due: &seven, Loc: berlin, Quiet: night, Before: []time.Duration{3 * hour, day}, FollowUp: "never",
	}, now)
	require.Equal(t, []string{"before 2026-11-12T07:00:00Z", "due 2026-11-13T06:00:00Z"}, at(got))
	require.Equal(t, day, got[0].Offset)
}

func TestCollisionKeepsTheClosestBefore(t *testing.T) {
	noon := ts("2026-11-13T11:00:00Z") // 12:00 in Berlin
	got := reminder.Compute(reminder.ScheduleInput{
		Open: true, Due: &noon, Loc: berlin, Quiet: night,
		Before: []time.Duration{6 * hour, 5 * hour, 4 * hour}, FollowUp: "never",
	}, now)
	// 06:00 and 07:00 both wait until 08:00, where the unshifted "4h before" already is.
	require.Equal(t, []string{"before 2026-11-13T07:00:00Z", "due 2026-11-13T11:00:00Z"}, at(got))
	require.Equal(t, 4*hour, got[0].Offset)
}

func TestQuietWindowAcrossMidnightAndDST(t *testing.T) {
	// Berlin switches to summer time in this night: 02:00 becomes 03:00.
	morning := ts("2025-03-30T08:00:00Z") // 10:00 CEST
	got := reminder.Compute(reminder.ScheduleInput{
		Open: true, Due: &morning, Loc: berlin, Quiet: night, Before: []time.Duration{6*hour + 30*time.Minute}, FollowUp: "never",
	}, ts("2025-03-29T12:00:00Z"))
	require.Equal(t, []string{"before 2025-03-30T06:00:00Z", "due 2025-03-30T08:00:00Z"}, at(got), "08:00 CEST is 06:00 UTC")
}

func TestQuietWindowNotCrossingMidnight(t *testing.T) {
	siesta := &reminder.Quiet{Start: 13 * hour, End: 15 * hour}
	evening := ts("2026-11-13T17:00:00Z") // 18:00 in Berlin
	got := reminder.Compute(reminder.ScheduleInput{
		Open: true, Due: &evening, Loc: berlin, Quiet: siesta, Before: []time.Duration{4 * hour, 2 * hour}, FollowUp: "never",
	}, now)
	// 14:00 waits until 15:00, 16:00 is outside the window.
	require.Equal(t, []string{"before 2026-11-13T14:00:00Z", "before 2026-11-13T15:00:00Z", "due 2026-11-13T17:00:00Z"}, at(got))
}
