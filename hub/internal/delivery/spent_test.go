package delivery_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/rayakame/plsremind/hub/internal/delivery"
	"github.com/rayakame/plsremind/hub/internal/testkit"
)

func newItem(t *testing.T, u *testkit.Client) string {
	t.Helper()
	r := u.Do(t, "POST", "/users/@me/items", J{"title": "x"})
	require.Equal(t, 201, r.Status)
	return r.Get("id").String()
}

func remindIn(t *testing.T, u *testkit.Client, item, in string) string {
	t.Helper()
	r := u.Do(t, "POST", "/users/@me/items/"+item+"/reminders", J{"in": in})
	require.Equal(t, 201, r.Status, string(r.Body))
	return r.Get("id").String()
}

func remindersOf(t *testing.T, u *testkit.Client, item string) []string {
	t.Helper()
	r := u.Do(t, "GET", "/users/@me/items/"+item+"/reminders", nil)
	require.Equal(t, 200, r.Status)
	return testkit.Strings(r, "#.id")
}

func typesOf(events []testkit.Event) []string {
	out := make([]string, 0, len(events))
	for _, event := range events {
		out = append(out, event.Type)
	}
	return out
}

func TestASpentManualReminderLeavesItsItem(t *testing.T) {
	e := testkit.New(t)
	mia, _ := testkit.Reminded(t, e, "mia")
	id := newItem(t, mia)
	rem := remindIn(t, mia, id, "1h")

	e.Clock.Advance(time.Hour)
	e.Fire(t)

	require.Empty(t, remindersOf(t, mia, id))
	require.Equal(t, []string{"manual fired"}, e.ReminderLog(t, mia.UserID))
	require.Equal(t, []string{"manual pending"}, statuses(e.Deliveries(t, mia.UserID)), "the reminder itself still goes out")
	ev := e.Events(t, mia.UserID)
	gone, whole := ev[len(ev)-2], ev[len(ev)-1]
	require.Equal(t, []string{"reminder.deleted", "item.updated"}, []string{gone.Type, whole.Type})
	require.Equal(t, rem, gone.Payload.Get("id").String())
	require.Equal(t, id, gone.Payload.Get("item_id").String())
	require.Zero(t, whole.Payload.Get("reminders.#").Int())
	require.Empty(t, gone.ActorKind, "the hub did this, nobody else")
	require.Empty(t, whole.ActorKind)
}

func TestTheLimitCountsOnlyRemindersStillAhead(t *testing.T) {
	e := testkit.New(t)
	mia := e.User(t, "mia")
	id := newItem(t, mia)
	for i := 0; i < 20; i++ {
		remindIn(t, mia, id, "1h")
	}
	full := mia.Do(t, "POST", "/users/@me/items/"+id+"/reminders", J{"in": "1h"})
	require.Equal(t, 409, full.Status)
	require.Equal(t, "reminder_limit", full.Get("error.code").String())

	e.Clock.Advance(2 * time.Hour)
	e.Fire(t)

	remindIn(t, mia, id, "1h")
	require.Len(t, remindersOf(t, mia, id), 1)
}

func TestAReminderWhoseTimePassedWhileTheItemWasDoneIsRemoved(t *testing.T) {
	e := testkit.New(t)
	mia, _ := testkit.Reminded(t, e, "mia")
	id := newItem(t, mia)
	remindIn(t, mia, id, "1h")
	require.Equal(t, 200, mia.Do(t, "PATCH", "/users/@me/items/"+id, J{"status": "done"}).Status)

	e.Clock.Advance(2 * time.Hour)
	e.Fire(t)
	require.Equal(t, 200, mia.Do(t, "PATCH", "/users/@me/items/"+id, J{"status": "open"}).Status)

	require.Empty(t, remindersOf(t, mia, id))
	require.Empty(t, e.Deliveries(t, mia.UserID), "a reminder of a done item never goes out")
}

func TestAReminderStillAheadOfADoneItemStays(t *testing.T) {
	e := testkit.New(t)
	mia, _ := testkit.Reminded(t, e, "mia")
	id := newItem(t, mia)
	rem := remindIn(t, mia, id, "3h")
	require.Equal(t, 200, mia.Do(t, "PATCH", "/users/@me/items/"+id, J{"status": "done"}).Status)

	e.Clock.Advance(time.Hour)
	e.Fire(t)
	require.Equal(t, 200, mia.Do(t, "PATCH", "/users/@me/items/"+id, J{"status": "open"}).Status)
	require.Equal(t, []string{rem}, remindersOf(t, mia, id))

	e.Clock.Advance(2 * time.Hour)
	e.Fire(t)
	require.Equal(t, []string{"manual pending"}, statuses(e.Deliveries(t, mia.UserID)))
	require.Empty(t, remindersOf(t, mia, id))
}

func TestManySpentRemindersAreRemovedInOneRun(t *testing.T) {
	e := testkit.New(t)
	ctx := context.Background()
	users := []*testkit.Client{e.User(t, "mia"), e.User(t, "ben"), e.User(t, "kim")}
	past := e.Clock.Now().Add(-time.Hour)
	for _, u := range users {
		_, err := e.Pool.Exec(ctx,
			`INSERT INTO manual_reminders (id, item_id, remind_at, created_at)
			 SELECT gen_random_uuid(), $1, $2, $2 FROM generate_series(1, 150)`, newItem(t, u), past)
		require.NoError(t, err)
	}

	e.Fire(t)

	var left int
	require.NoError(t, e.Pool.QueryRow(ctx, "SELECT count(*) FROM manual_reminders").Scan(&left))
	require.Zero(t, left)
	for _, u := range users {
		gone := 0
		for _, event := range e.Events(t, u.UserID) {
			if event.Type == "reminder.deleted" {
				gone++
			}
		}
		require.Equal(t, 150, gone)
	}
}

func TestASecondRunAnnouncesNothingMore(t *testing.T) {
	e := testkit.New(t)
	mia := e.User(t, "mia")
	id := newItem(t, mia)
	byHand := remindIn(t, mia, id, "1h")
	remindIn(t, mia, id, "1h")
	e.Clock.Advance(time.Hour)
	require.Equal(t, 204, mia.Do(t, "DELETE", "/users/@me/items/"+id+"/reminders/"+byHand, nil).Status)

	e.Fire(t)
	first := e.Events(t, mia.UserID)
	e.Fire(t)

	require.Equal(t, typesOf(first), typesOf(e.Events(t, mia.UserID)))
	var byHub, byUser int
	for _, event := range first {
		switch {
		case event.Type != "reminder.deleted":
		case event.ActorKind == "":
			byHub++
		default:
			byUser++
		}
	}
	require.Equal(t, []int{1, 1}, []int{byHub, byUser}, "each removal is announced once, by whoever made it")
}

func TestAHugeBacklogIsWorkedOffOverSeveralRuns(t *testing.T) {
	e := testkit.New(t)
	ctx := context.Background()
	mia := e.User(t, "mia")
	_, err := e.Pool.Exec(ctx,
		`INSERT INTO manual_reminders (id, item_id, remind_at, created_at)
		 SELECT gen_random_uuid(), $1, $2, $2 FROM generate_series(1, 1100)`, newItem(t, mia), e.Clock.Now().Add(-time.Hour))
	require.NoError(t, err)
	left := func() (n int) {
		require.NoError(t, e.Pool.QueryRow(ctx, "SELECT count(*) FROM manual_reminders").Scan(&n))
		return n
	}

	e.Fire(t)
	require.Equal(t, 100, left(), "one run takes a thousand at most, so that firing is not held up")
	e.Fire(t)
	require.Zero(t, left())
}

func TestTroubleWhileSweepingDoesNotKeepDeliveriesFromExpiring(t *testing.T) {
	e := testkit.New(t)
	ctx := context.Background()
	mia, _ := testkit.Reminded(t, e, "mia")
	testkit.DueSoon(t, e, mia)
	require.Equal(t, []string{"due pending"}, statuses(e.Deliveries(t, mia.UserID)))
	e.Clock.Advance(25 * time.Hour)
	_, err := e.Pool.Exec(ctx, "ALTER TABLE manual_reminders RENAME TO manual_reminders_away")
	require.NoError(t, err)

	require.Error(t, delivery.RunOnce(ctx, e.Deps))

	require.Equal(t, []string{"due expired"}, statuses(e.Deliveries(t, mia.UserID)))
}

func TestDroppingForAnItemOrAUserThatIsGoneIsNoError(t *testing.T) {
	e := testkit.New(t)
	ctx := context.Background()
	mia := e.User(t, "mia")
	userID := uuid.MustParse(mia.UserID)

	n, err := delivery.DropSpent(ctx, e.Deps, userID, uuid.New(), uuid.New())
	require.NoError(t, err)
	require.Zero(t, n)
	n, err = delivery.DropSpent(ctx, e.Deps, uuid.New(), uuid.New(), uuid.New())
	require.NoError(t, err)
	require.Zero(t, n)
}

func TestOneItemThatCannotBeSweptDoesNotHoldUpTheOthers(t *testing.T) {
	e := testkit.New(t)
	ctx := context.Background()
	mia, ben := e.User(t, "mia"), e.User(t, "ben")
	stuck, freed := newItem(t, mia), newItem(t, ben)
	for id, at := range map[string]time.Time{stuck: e.Clock.Now().Add(-2 * time.Hour), freed: e.Clock.Now().Add(-time.Hour)} {
		_, err := e.Pool.Exec(ctx,
			`INSERT INTO manual_reminders (id, item_id, remind_at, created_at) VALUES (gen_random_uuid(), $1, $2, $2)`, id, at)
		require.NoError(t, err)
	}
	_, err := e.Pool.Exec(ctx, `CREATE FUNCTION refuse() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'refused'; END $$`)
	require.NoError(t, err)
	_, err = e.Pool.Exec(ctx, `CREATE TRIGGER refuse BEFORE DELETE ON manual_reminders FOR EACH ROW
		WHEN (OLD.item_id = '`+stuck+`') EXECUTE FUNCTION refuse()`)
	require.NoError(t, err)

	require.Error(t, delivery.RunOnce(ctx, e.Deps))

	require.Len(t, remindersOf(t, mia, stuck), 1)
	require.Empty(t, remindersOf(t, ben, freed), "the item after the one that failed is swept all the same")
}

func TestAWholeBatchThatCannotBeSweptDoesNotHoldUpWhatComesAfterIt(t *testing.T) {
	e := testkit.New(t)
	ctx := context.Background()
	mia, ben := e.User(t, "mia"), e.User(t, "ben")
	stuck, freed := newItem(t, mia), newItem(t, ben)
	_, err := e.Pool.Exec(ctx,
		`INSERT INTO manual_reminders (id, item_id, remind_at, created_at)
		 SELECT gen_random_uuid(), $1, $2, $2 FROM generate_series(1, 201)`, stuck, e.Clock.Now().Add(-2*time.Hour))
	require.NoError(t, err)
	_, err = e.Pool.Exec(ctx,
		`INSERT INTO manual_reminders (id, item_id, remind_at, created_at) VALUES (gen_random_uuid(), $1, $2, $2)`,
		freed, e.Clock.Now().Add(-time.Hour))
	require.NoError(t, err)
	_, err = e.Pool.Exec(ctx, `CREATE FUNCTION refuse() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'refused'; END $$`)
	require.NoError(t, err)
	_, err = e.Pool.Exec(ctx, `CREATE TRIGGER refuse BEFORE DELETE ON manual_reminders FOR EACH ROW
		WHEN (OLD.item_id = '`+stuck+`') EXECUTE FUNCTION refuse()`)
	require.NoError(t, err)

	require.Error(t, delivery.RunOnce(ctx, e.Deps))

	require.Empty(t, remindersOf(t, ben, freed), "a batch in which nothing could be swept is stepped over")
}

func TestRemindersWhoseTimeHasPassedDoNotCountTowardsTheLimitBeforeTheSweep(t *testing.T) {
	e := testkit.New(t)
	ctx := context.Background()
	mia := e.User(t, "mia")
	id := newItem(t, mia)
	_, err := e.Pool.Exec(ctx,
		`INSERT INTO manual_reminders (id, item_id, remind_at, created_at)
		 SELECT gen_random_uuid(), $1, $2, $2 FROM generate_series(1, 20)`, id, e.Clock.Now().Add(-time.Minute))
	require.NoError(t, err)

	r := mia.Do(t, "POST", "/users/@me/items/"+id+"/reminders", J{"in": "1h"})
	require.Equal(t, 201, r.Status, string(r.Body))
}
