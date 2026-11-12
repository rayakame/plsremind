package httpapi_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/rayakame/plsremind/hub/internal/testkit"
)

func fireTimes(rows []testkit.Scheduled) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		label := r.Kind
		if r.Offset != "" {
			label += " " + r.Offset
		}
		out = append(out, label+" "+r.FireAt.UTC().Format(time.RFC3339))
	}
	return out
}

func TestScheduleFollowsItem(t *testing.T) {
	e := testkit.New(t)
	u := e.User(t, "mia")
	id := u.Do(t, "POST", "/users/@me/items", J{"title": "Abgabe", "priority": "high", "due_at": "2026-11-13T18:00:00Z"}).Get("id").String()
	s := e.Schedule(t, id)
	require.Len(t, s, 17, "1d, 3h, due and 14 follow-ups; 7d and 3d have passed")
	require.Equal(t, "before 1d 2026-11-12T18:00:00Z", fireTimes(s)[0])

	u.Do(t, "PATCH", "/users/@me/items/"+id, J{"priority": "low"})
	require.Equal(t, []string{"before 1d 2026-11-12T18:00:00Z", "due 2026-11-13T18:00:00Z"}, fireTimes(e.Schedule(t, id)))

	u.Do(t, "PATCH", "/users/@me/items/"+id, J{"status": "done"})
	require.Empty(t, e.Schedule(t, id))
	u.Do(t, "PATCH", "/users/@me/items/"+id, J{"status": "open"})
	require.Len(t, e.Schedule(t, id), 2)
}

func TestScheduleFollowsPlanAndProfile(t *testing.T) {
	e := testkit.New(t)
	u := e.User(t, "mia")
	id := u.Do(t, "POST", "/users/@me/items", J{"title": "x", "due_at": "2026-11-13T18:00:00Z"}).Get("id").String()
	require.Len(t, e.Schedule(t, id), 9, "1d, due and 7 daily follow-ups")

	u.Do(t, "PUT", "/users/@me/reminder-plans/normal", J{"before": []string{"2h"}, "follow_up": "never"})
	require.Equal(t, []string{"before 2h 2026-11-13T16:00:00Z", "due 2026-11-13T18:00:00Z"}, fireTimes(e.Schedule(t, id)))

	u.Do(t, "PATCH", "/users/@me", J{"quiet_hours": J{"start": "15:00", "end": "17:00"}})
	s := e.Schedule(t, id)
	require.Equal(t, "before 2h 2026-11-13T17:00:00Z", fireTimes(s)[0])
	require.Equal(t, "2026-11-13T16:00:00Z", s[0].For.UTC().Format(time.RFC3339))

	u.Do(t, "DELETE", "/users/@me/reminder-plans/normal", nil)
	require.Len(t, e.Schedule(t, id), 9)

	allDay := u.Do(t, "POST", "/users/@me/items", J{"title": "y", "due_date": "2026-11-13", "priority": "low"}).Get("id").String()
	require.Equal(t, "due 2026-11-13T23:59:59Z", fireTimes(e.Schedule(t, allDay))[1])
	u.Do(t, "PATCH", "/users/@me", J{"timezone": "Europe/Berlin"})
	require.Equal(t, "due 2026-11-13T22:59:59Z", fireTimes(e.Schedule(t, allDay))[1])
}

func TestScheduleFollowsOverrideAndDueChange(t *testing.T) {
	e := testkit.New(t)
	u := e.User(t, "mia")
	id := u.Do(t, "POST", "/users/@me/items", J{"title": "x", "priority": "low", "due_at": "2026-11-13T18:00:00Z"}).Get("id").String()
	path := "/users/@me/items/" + id

	u.Do(t, "PATCH", path, J{"reminder_override": J{"mode": "custom", "before": []string{"30m"}}})
	require.Equal(t, []string{"before 30m 2026-11-13T17:30:00Z", "due 2026-11-13T18:00:00Z"}, fireTimes(e.Schedule(t, id)))
	u.Do(t, "PATCH", path, J{"reminder_override": J{"mode": "off"}})
	require.Empty(t, e.Schedule(t, id))
	u.Do(t, "PATCH", path, J{"reminder_override": nil})
	require.Len(t, e.Schedule(t, id), 2)

	u.Do(t, "PATCH", path, J{"due_at": "2026-11-20T18:00:00Z"})
	require.Equal(t, []string{"before 1d 2026-11-19T18:00:00Z", "due 2026-11-20T18:00:00Z"}, fireTimes(e.Schedule(t, id)))
	u.Do(t, "PATCH", path, J{"due_at": nil})
	require.Empty(t, e.Schedule(t, id))
}

func TestManualReminderIsScheduled(t *testing.T) {
	e := testkit.New(t)
	u := e.User(t, "mia")
	id := u.Do(t, "POST", "/users/@me/items", J{"title": "Wäsche"}).Get("id").String()
	rem := u.Do(t, "POST", "/users/@me/items/"+id+"/reminders", J{"in": "2h"}).Get("id").String()
	require.Equal(t, []string{"manual 2026-11-11T14:00:00Z"}, fireTimes(e.Schedule(t, id)))

	u.Do(t, "PATCH", "/users/@me/items/"+id, J{"status": "done"})
	require.Empty(t, e.Schedule(t, id), "a done item does not remind, not even by hand")
	u.Do(t, "PATCH", "/users/@me/items/"+id, J{"status": "open"})
	require.Len(t, e.Schedule(t, id), 1)

	u.Do(t, "DELETE", "/users/@me/items/"+id+"/reminders/"+rem, nil)
	require.Empty(t, e.Schedule(t, id))
}

func TestDueRowSurvivesAnEdit(t *testing.T) {
	// The reminder is due, the ticker has not come by yet, and the user renames the item.
	e := testkit.New(t)
	u := e.User(t, "mia")
	id := u.Do(t, "POST", "/users/@me/items", J{"title": "x", "priority": "low", "due_at": "2026-11-11T13:00:00Z"}).Get("id").String()
	e.Clock.Advance(time.Hour + time.Second)
	u.Do(t, "PATCH", "/users/@me/items/"+id, J{"title": "y"})
	s := e.Schedule(t, id)
	require.Len(t, s, 1)
	require.Equal(t, "due", s[0].Kind)
	require.True(t, s[0].FireAt.Before(e.Clock.Now()))
}

func TestDeletingItemClearsSchedule(t *testing.T) {
	e := testkit.New(t)
	u := e.User(t, "mia")
	id := u.Do(t, "POST", "/users/@me/items", J{"title": "x", "due_at": "2026-11-13T18:00:00Z"}).Get("id").String()
	require.NotEmpty(t, e.Schedule(t, id))
	u.Do(t, "DELETE", "/users/@me/items/"+id, nil)
	require.Empty(t, e.Schedule(t, id))
}
