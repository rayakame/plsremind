package gateway_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/rayakame/plsremind/hub/internal/events"
	"github.com/rayakame/plsremind/hub/internal/store"
	"github.com/rayakame/plsremind/hub/internal/store/sqlc"
	"github.com/rayakame/plsremind/hub/internal/testkit"
	"github.com/rayakame/plsremind/hub/internal/todo"
)

func TestAnEventOfTheHubItselfCarriesNoActor(t *testing.T) {
	e := testkit.New(t)
	mia := e.User(t, "mia")
	id := mia.Do(t, "POST", "/users/@me/items", J{"title": "x"}).Get("id").String()
	gw := e.Gateway(t, personalToken(t, mia), "items")
	gw.Expect(t, "user.sync")

	userID, itemID := uuid.MustParse(mia.UserID), uuid.MustParse(id)
	ctx := context.Background()
	require.NoError(t, store.UserTx(ctx, e.Pool, userID, func(q *sqlc.Queries) error {
		item, err := todo.LoadItem(ctx, q, userID, itemID)
		if err != nil {
			return err
		}
		return events.Append(ctx, q, e.Clock.Now(), userID, events.Actor{}, "item.updated", item)
	}))

	frame := gw.Expect(t, "item.updated")
	require.Equal(t, gjson.Null, gjson.Parse(frame.D.Raw).Get("actor").Type)
	require.Equal(t, gjson.Null, gjson.Parse(frame.D.Raw).Get("nonce").Type)
	log := e.Events(t, mia.UserID)
	require.Empty(t, log[len(log)-1].ActorKind)
	require.Empty(t, log[len(log)-1].ActorID)
}

func TestAGatewayClientSeesASpentReminderGo(t *testing.T) {
	e := testkit.New(t)
	mia := e.User(t, "mia")
	id := mia.Do(t, "POST", "/users/@me/items", J{"title": "x"}).Get("id").String()
	gw := e.Gateway(t, personalToken(t, mia), "items")
	gw.Expect(t, "user.sync")
	rem := mia.Do(t, "POST", "/users/@me/items/"+id+"/reminders", J{"in": "1h"}).Get("id").String()
	gw.Expect(t, "reminder.created")
	gw.Expect(t, "item.updated")

	e.Clock.Advance(time.Hour)
	e.Fire(t)

	gone := gw.Expect(t, "reminder.deleted")
	require.Equal(t, rem, gone.Get("data.id").String())
	require.Equal(t, gjson.Null, gjson.Parse(gone.D.Raw).Get("actor").Type)
	whole := gw.Expect(t, "item.updated")
	require.Zero(t, whole.Get("data.reminders.#").Int())
	require.Equal(t, gjson.Null, gjson.Parse(whole.D.Raw).Get("actor").Type)
	gw.Cache(t).RequireMatchesREST(t, mia, "/users/@me", mia.UserID)
}
