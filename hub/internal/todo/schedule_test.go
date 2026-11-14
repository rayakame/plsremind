package todo_test

import (
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/rayakame/plsremind/hub/internal/testkit"
)

func TestItemScheduleListsWhatWillFire(t *testing.T) {
	e := testkit.New(t)
	mia := e.User(t, "mia")
	id := mia.Do(t, "POST", "/users/@me/items", J{"title": "Abgabe", "priority": "low", "due_at": "2026-11-13T18:00:00Z"}).Get("id").String()
	rem := mia.Do(t, "POST", "/users/@me/items/"+id+"/reminders", J{"in": "2h"}).Get("id").String()

	s := mia.Do(t, "GET", "/users/@me/items/"+id+"/schedule", nil)
	require.Equal(t, 200, s.Status, string(s.Body))
	require.Equal(t, []string{"manual", "before", "due"}, testkit.Strings(s, "#.kind"))
	require.Equal(t, rem, s.Get("0.reminder_id").String())
	require.Equal(t, gjson.Null, s.Get("0.offset").Type)
	require.Equal(t, "1d", s.Get("1.offset").String())
	require.Equal(t, gjson.Null, s.Get("1.reminder_id").Type)
	require.Equal(t, "2026-11-12T18:00:00Z", s.Get("1.scheduled_for").String())
	require.Equal(t, "2026-11-12T18:00:00Z", s.Get("1.fire_at").String())
	require.Equal(t, gjson.Null, s.Get("2.offset").Type)
	require.Equal(t, "2026-11-13T18:00:00Z", s.Get("2.fire_at").String())
}

func TestItemScheduleShowsQuietHoursShift(t *testing.T) {
	e := testkit.New(t)
	mia := e.User(t, "mia")
	require.Equal(t, 200, mia.Do(t, "PATCH", "/users/@me", J{"quiet_hours": J{"start": "22:00", "end": "08:00"}}).Status)
	id := mia.Do(t, "POST", "/users/@me/items", J{"title": "früh", "priority": "low", "due_at": "2026-11-13T12:00:00Z"}).Get("id").String()
	mia.Do(t, "PATCH", "/users/@me/items/"+id, J{"reminder_override": J{"mode": "custom", "before": []string{"12h"}}})

	s := mia.Do(t, "GET", "/users/@me/items/"+id+"/schedule", nil)
	require.Equal(t, "before", s.Get("0.kind").String())
	require.Equal(t, "2026-11-13T00:00:00Z", s.Get("0.scheduled_for").String())
	require.Equal(t, "2026-11-13T08:00:00Z", s.Get("0.fire_at").String(), "held until quiet hours end")
}

func TestItemScheduleOfDoneItemIsEmpty(t *testing.T) {
	e := testkit.New(t)
	mia := e.User(t, "mia")
	id := mia.Do(t, "POST", "/users/@me/items", J{"title": "x", "due_at": "2026-11-13T18:00:00Z"}).Get("id").String()
	mia.Do(t, "PATCH", "/users/@me/items/"+id, J{"status": "done"})
	s := mia.Do(t, "GET", "/users/@me/items/"+id+"/schedule", nil)
	require.Equal(t, 200, s.Status)
	require.JSONEq(t, `[]`, string(s.Body))
}

func TestItemScheduleMatchesTheTicker(t *testing.T) {
	e := testkit.New(t)
	mia := e.User(t, "mia")
	id := mia.Do(t, "POST", "/users/@me/items", J{"title": "x", "priority": "high", "due_at": "2026-11-20T18:00:00Z"}).Get("id").String()
	planned := e.Schedule(t, id)
	s := mia.Do(t, "GET", "/users/@me/items/"+id+"/schedule", nil)
	require.Equal(t, int64(len(planned)), s.Get("#").Int())
	for i, p := range planned {
		row := s.Get(strconv.Itoa(i))
		require.Equal(t, p.Kind, row.Get("kind").String())
		require.Equal(t, p.Offset, row.Get("offset").String())
		require.Equal(t, p.FireAt.UTC().Format(time.RFC3339), row.Get("fire_at").String())
		require.Equal(t, p.For.UTC().Format(time.RFC3339), row.Get("scheduled_for").String())
	}
}

func TestItemScheduleIsPrivate(t *testing.T) {
	e := testkit.New(t)
	mia := testkit.Linked(t, e, "mia", "111")
	ben := e.User(t, "ben")
	id := mia.Do(t, "POST", "/users/@me/items", J{"title": "x", "due_at": "2026-11-13T18:00:00Z"}).Get("id").String()
	require.Equal(t, 404, ben.Do(t, "GET", "/users/@me/items/"+id+"/schedule", nil).Status)
	require.Equal(t, 404, mia.Do(t, "GET", "/users/@me/items/not-an-id/schedule", nil, testkit.NoContract()).Status)

	bot := e.Connection(t, "discord")
	require.Equal(t, 200, bot.Do(t, "GET", "/users/"+mia.UserID+"/items/"+id+"/schedule", nil).Status)
	require.Equal(t, 200, e.Admin(t).Do(t, "PATCH", "/admin/connections/"+bot.ID, J{"allowed_intents": []string{"users"}}).Status)
	denied := bot.Do(t, "GET", "/users/"+mia.UserID+"/items/"+id+"/schedule", nil)
	require.Equal(t, bot.Do(t, "GET", "/users/"+mia.UserID+"/items/"+id, nil).Status, denied.Status)
	require.GreaterOrEqual(t, denied.Status, 400)
}
