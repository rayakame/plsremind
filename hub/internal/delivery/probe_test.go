package delivery_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/rayakame/plsremind/hub/internal/testkit"
)

func startTest(t *testing.T, u *testkit.Client, app string) *testkit.Resp {
	t.Helper()
	return u.Do(t, "POST", "/users/@me/connections/"+app+"/tests", nil)
}

func testState(t *testing.T, u *testkit.Client, app, id string) gjson.Result {
	t.Helper()
	r := u.Do(t, "GET", "/users/@me/connections/"+app+"/tests/"+id, nil)
	require.Equal(t, 200, r.Status, string(r.Body))
	return r.Get("@this")
}

func lastDelivery(t *testing.T, u *testkit.Client, app string) gjson.Result {
	t.Helper()
	return u.Do(t, "GET", "/users/@me/connections", nil).Get(`#(id=="` + app + `").last_delivery`)
}

func TestATestReachesOnlyTheAppAndAnswersBack(t *testing.T) {
	e := testkit.New(t)
	first, second := e.Connection(t, "discord"), e.Connection(t, "discord")
	mia := testkit.Linked(t, e, "mia", "111")
	a, b := e.Gateway(t, first.Token, "deliveries"), e.Gateway(t, second.Token, "deliveries")

	started := startTest(t, mia, first.ID)
	require.Equal(t, 201, started.Status, string(started.Body))
	require.Equal(t, "pending", started.Get("status").String())
	require.Equal(t, first.ID, started.Get("connection_id").String())
	id := started.Get("id").String()

	frame := a.Expect(t, "delivery.test")
	require.Equal(t, id, frame.Get("data.id").String())
	require.Equal(t, mia.UserID, frame.Get("data.user.id").String())
	require.Equal(t, []string{"111"}, testkit.Strings(&testkit.Resp{Body: []byte(frame.Get("data").Raw)}, "user.external_ids"))
	b.ExpectQuiet(t)

	answered := first.Do(t, "PUT", "/deliveries/tests/"+id, J{"status": "delivered"})
	require.Equal(t, 200, answered.Status, string(answered.Body))
	state := testState(t, mia, first.ID, id)
	require.Equal(t, "delivered", state.Get("status").String())
	require.True(t, state.Get("closed_at").Exists())
	last := lastDelivery(t, mia, first.ID)
	require.Equal(t, "delivered", last.Get("status").String())
	require.True(t, last.Get("test").Bool())
	require.Equal(t, "null", lastDelivery(t, mia, second.ID).Raw)
}

func TestAFailedTestCarriesTheAppsReason(t *testing.T) {
	e := testkit.New(t)
	bot := e.Connection(t, "discord")
	mia := testkit.Linked(t, e, "mia", "111")
	gw := e.Gateway(t, bot.Token, "deliveries")
	id := startTest(t, mia, bot.ID).Get("id").String()
	gw.Expect(t, "delivery.test")

	require.Equal(t, 422, bot.Do(t, "PUT", "/deliveries/tests/"+id, J{"status": "failed"}).Status)
	require.Equal(t, 200, bot.Do(t, "PUT", "/deliveries/tests/"+id, J{"status": "failed", "reason": "DMs are closed"}).Status)
	state := testState(t, mia, bot.ID, id)
	require.Equal(t, "failed", state.Get("status").String())
	require.Equal(t, "DMs are closed", state.Get("reason").String())
	require.Equal(t, "DMs are closed", lastDelivery(t, mia, bot.ID).Get("reason").String())
}

func TestATestWithoutAConnectedAppEndsAtOnce(t *testing.T) {
	e := testkit.New(t)
	bot := e.Connection(t, "discord")
	mia := testkit.Linked(t, e, "mia", "111")
	id := startTest(t, mia, bot.ID).Get("id").String()

	require.Eventually(t, func() bool {
		return testState(t, mia, bot.ID, id).Get("status").String() == "unanswered"
	}, 5*time.Second, 50*time.Millisecond)
	require.Equal(t, "not_connected", testState(t, mia, bot.ID, id).Get("reason").String())
	require.Equal(t, "null", lastDelivery(t, mia, bot.ID).Raw, "a test nobody answered says nothing about the app")
}

func TestATestNobodyAnswersEndsUnanswered(t *testing.T) {
	e := testkit.New(t)
	bot := e.Connection(t, "discord")
	mia := testkit.Linked(t, e, "mia", "111")
	gw := e.Gateway(t, bot.Token, "deliveries")
	id := startTest(t, mia, bot.ID).Get("id").String()
	gw.Expect(t, "delivery.test")

	e.Clock.Advance(59 * time.Second)
	e.Fire(t)
	require.Equal(t, "pending", testState(t, mia, bot.ID, id).Get("status").String())
	e.Clock.Advance(2 * time.Second)
	e.Fire(t)
	state := testState(t, mia, bot.ID, id)
	require.Equal(t, "unanswered", state.Get("status").String())
	require.Equal(t, "no_answer", state.Get("reason").String())
	require.Equal(t, 409, bot.Do(t, "PUT", "/deliveries/tests/"+id, J{"status": "delivered"}).Status)
}

func TestTestsNeedAccessAndAnAccount(t *testing.T) {
	e := testkit.New(t)
	admin := e.Admin(t)
	bot := e.Connection(t, "discord")
	quiet := admin.Do(t, "POST", "/admin/connections", J{"name": "Quiet", "provider": "discord", "allowed_intents": []string{"users", "items"}}).Get("id").String()
	off := admin.Do(t, "POST", "/admin/connections", J{"name": "Off", "provider": "discord"}).Get("id").String()
	require.Equal(t, 200, admin.Do(t, "PATCH", "/admin/connections/"+off, J{"enabled": false}).Status)
	mia := testkit.Linked(t, e, "mia", "111")
	tom := e.User(t, "tom")
	ben := testkit.Linked(t, e, "ben", "222")
	require.Equal(t, 200, ben.Do(t, "PUT", "/users/@me/connections/"+bot.ID, J{"enabled": false}).Status)

	for name, try := range map[string]*testkit.Resp{
		"without deliveries": startTest(t, mia, quiet),
		"a disabled app":     startTest(t, mia, off),
		"without an account": startTest(t, tom, bot.ID),
		"the app turned off": startTest(t, ben, bot.ID),
	} {
		require.Equal(t, 409, try.Status, name)
		require.Equal(t, "test_unavailable", try.Get("error.code").String(), name)
	}
	require.Equal(t, 404, startTest(t, mia, "00000000-0000-7000-8000-000000000000").Status)

	tok := mia.Do(t, "POST", "/users/@me/tokens", J{"name": "script"}).Get("token").String()
	require.Equal(t, 403, e.Do(t, "POST", "/users/@me/connections/"+bot.ID+"/tests", nil, testkit.Bearer(tok)).Status)
}

func TestTestsAreLimited(t *testing.T) {
	e := testkit.New(t)
	bot := e.Connection(t, "discord")
	mia := testkit.Linked(t, e, "mia", "111")
	require.Equal(t, 201, startTest(t, mia, bot.ID).Status)

	again := startTest(t, mia, bot.ID)
	require.Equal(t, 429, again.Status)
	require.Equal(t, "rate_limited", again.Get("error.code").String())
	require.NotEmpty(t, again.Header.Get("Retry-After"))

	other := e.Connection(t, "discord")
	require.Equal(t, 201, startTest(t, mia, other.ID).Status, "another app is counted on its own")
	e.Clock.Advance(31 * time.Second)
	require.Equal(t, 201, startTest(t, mia, bot.ID).Status)
}

func TestOnlyTheAppMayAnswerItsTest(t *testing.T) {
	e := testkit.New(t)
	bot, other := e.Connection(t, "discord"), e.Connection(t, "discord")
	mia := testkit.Linked(t, e, "mia", "111")
	gw := e.Gateway(t, bot.Token, "deliveries")
	id := startTest(t, mia, bot.ID).Get("id").String()
	gw.Expect(t, "delivery.test")

	require.Equal(t, 404, other.Do(t, "PUT", "/deliveries/tests/"+id, J{"status": "delivered"}).Status)
	require.Equal(t, 200, bot.Do(t, "PUT", "/deliveries/tests/"+id, J{"status": "delivered"}).Status)
	require.Equal(t, 200, bot.Do(t, "PUT", "/deliveries/tests/"+id, J{"status": "delivered"}).Status, "the same outcome again is fine")
	changed := bot.Do(t, "PUT", "/deliveries/tests/"+id, J{"status": "failed", "reason": "nope"})
	require.Equal(t, 409, changed.Status)
	require.Equal(t, "test_closed", changed.Get("error.code").String())
	require.Equal(t, 404, mia.Do(t, "GET", "/users/@me/connections/"+other.ID+"/tests/"+id, nil).Status)
}

func TestTheTestEventIsNotSentAgainOnLogin(t *testing.T) {
	e := testkit.New(t)
	bot := e.Connection(t, "discord")
	mia := testkit.Linked(t, e, "mia", "111")
	first := e.Gateway(t, bot.Token, "deliveries")
	startTest(t, mia, bot.ID)
	first.Expect(t, "delivery.test")

	later := e.Gateway(t, bot.Token, "deliveries")
	later.ExpectQuiet(t)
}

func TestOldTestsAreSwept(t *testing.T) {
	e := testkit.New(t)
	bot := e.Connection(t, "discord")
	mia := testkit.Linked(t, e, "mia", "111")
	id := startTest(t, mia, bot.ID).Get("id").String()

	e.Clock.Advance(29 * 24 * time.Hour)
	e.Sweep(t)
	require.Equal(t, 200, mia.Do(t, "GET", "/users/@me/connections/"+bot.ID+"/tests/"+id, nil).Status)
	e.Clock.Advance(2 * 24 * time.Hour)
	e.Sweep(t)
	require.Equal(t, 404, mia.Do(t, "GET", "/users/@me/connections/"+bot.ID+"/tests/"+id, nil).Status)
}
