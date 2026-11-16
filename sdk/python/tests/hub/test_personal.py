from __future__ import annotations

import asyncio
import datetime

import pytest

import plsremind
from plsremind import Intents
from tests.hub import helpers

pytestmark = pytest.mark.hub
UTC = datetime.UTC
until = helpers.until


def ids(items: tuple[plsremind.Item, ...]) -> list[str]:
    return [item.id for item in items]


def soon(seconds: float = 2.0) -> datetime.datetime:
    return datetime.datetime.now(UTC) + datetime.timedelta(seconds=seconds)


async def test_the_cache_after_start_equals_what_rest_says(kit: helpers.Kit) -> None:
    mia = await kit.account("mia")
    rest = mia.rest
    uni = await rest.create_list(name="Uni")
    override = plsremind.ReminderOverride(
        mode=plsremind.OverrideMode.CUSTOM, before=(plsremind.Duration(days=1), plsremind.Duration(hours=1))
    )
    report = await rest.create_item(
        title="Hand in the report",
        list_id=uni.id,
        due_at=datetime.datetime(2030, 1, 1, 10, 0, tzinfo=UTC),
        subitems=["print", "sign"],
        reminder_override=override,
    )
    await rest.create_reminder(report.id, at=datetime.datetime(2029, 12, 30, 8, 0, tzinfo=UTC))
    await rest.create_item(title="Buy milk", due_date=datetime.date(2030, 2, 1), priority=plsremind.Priority.HIGH)
    finished = await rest.create_item(title="Call home")
    await rest.update_item(finished.id, status=plsremind.ItemStatus.DONE)
    await rest.set_reminder_plan(
        plsremind.Priority.HIGH, before=[datetime.timedelta(hours=2)], follow_up=plsremind.FollowUp.DAILY
    )

    client = kit.client(mia.token).client
    await client.start()
    cache = client.cache

    assert cache.get_users() == (mia.id,)
    assert cache.get_lists() == await rest.list_lists()
    assert sorted(cache.get_plans(), key=lambda plan: plan.priority.value) == sorted(
        await rest.list_reminder_plans(), key=lambda plan: plan.priority.value
    )
    listed = await rest.list_items(status=plsremind.ItemStatus.OPEN)
    assert sorted(cache.get_items(), key=lambda item: item.id) == sorted(listed.data, key=lambda item: item.id)
    done = await rest.list_items(
        status=plsremind.ItemStatus.DONE, sort=plsremind.ItemSort.COMPLETED, order=plsremind.SortOrder.DESCENDING
    )
    assert cache.get_history() == done.data
    assert cache.get_history_cursor() is None
    assert cache.get_profile() == await rest.get_user()
    cached = cache.get_item(report.id)
    assert cached is not None
    assert cached.reminder_override == override
    assert cache.get_item(finished.id) == done.data[0]
    assert client.principal is not None
    assert client.principal.kind is plsremind.CredentialKind.PERSONAL_TOKEN
    assert client.principal.user_id == mia.id
    assert client.principal.id == mia.token_id
    assert client.intents == Intents.ALL


async def test_changes_arrive_as_events_with_actor_nonce_and_old_object(kit: helpers.Kit) -> None:
    mia = await kit.account("mia")
    client = kit.client(mia.token).client
    await client.start()
    seen: list[str] = []
    other_id, other_token = await kit.personal_token(mia.browser)
    other = kit.rest(other_token)

    @client.listen()
    async def on_create(event: plsremind.ItemCreateEvent) -> None:
        assert event.actor is not None
        seen.append(f"created {event.nonce} {event.actor.kind.value} {event.actor.id == other_id}")

    @client.listen()
    async def on_update(event: plsremind.ItemUpdateEvent) -> None:
        assert event.old_item is not None
        assert client.principal is not None
        assert event.actor is not None
        own = event.actor.id == client.principal.id
        seen.append(f"updated {event.nonce} {event.old_item.title} -> {event.item.title} own={own}")

    item = await other.create_item(title="first", nonce="n-create")
    await client.rest.update_item(item.id, title="second", nonce="n-update")
    await until(lambda: len(seen) == 2)
    assert seen == ["created n-create personal_token True", "updated n-update first -> second own=True"]
    assert client.cache.get_item(item.id) == await mia.rest.get_item(item.id)
    assert on_create is not on_update


async def test_every_kind_of_change_arrives(kit: helpers.Kit) -> None:
    mia = await kit.account("mia")
    rest = mia.rest
    client = kit.client(mia.token).client
    await client.start()
    kinds: list[type[plsremind.Event]] = []

    @client.listen()
    async def on_change(event: plsremind.ChangeEvent) -> None:
        kinds.append(type(event))

    work = await rest.create_list(name="Work")
    await rest.rename_list(work.id, name="Job")
    item = await rest.create_item(title="Report", list_id=work.id)
    sub = await rest.create_subitem(item.id, title="print")
    await rest.update_subitem(item.id, sub.id, done=True)
    await rest.delete_subitem(item.id, sub.id)
    reminder = await rest.create_reminder(item.id, after=datetime.timedelta(days=1))
    await rest.delete_reminder(item.id, reminder.id)
    await rest.set_reminder_plan(
        plsremind.Priority.LOW, before=[datetime.timedelta(days=2)], follow_up=plsremind.FollowUp.NEVER
    )
    await rest.update_user(display_name="Mia B.")
    provider = helpers.unique("chat")
    bot_id, _ = await kit.connection(provider)
    await rest.set_connection_grant(bot_id, enabled=False)
    await rest.delete_item(item.id)
    await rest.delete_list(work.id)

    expected = {
        plsremind.ListCreateEvent,
        plsremind.ListUpdateEvent,
        plsremind.ListDeleteEvent,
        plsremind.ItemCreateEvent,
        plsremind.ItemUpdateEvent,
        plsremind.ItemDeleteEvent,
        plsremind.SubitemCreateEvent,
        plsremind.SubitemUpdateEvent,
        plsremind.SubitemDeleteEvent,
        plsremind.ReminderCreateEvent,
        plsremind.ReminderDeleteEvent,
        plsremind.PlanUpdateEvent,
        plsremind.UserUpdateEvent,
        plsremind.ConnectionUpdateEvent,
    }
    await until(lambda: expected <= set(kinds), timeout=10)
    profile = client.cache.get_profile()
    assert profile is not None
    assert profile.display_name == "Mia B."
    assert client.cache.get_list(work.id) is None
    assert client.cache.get_items() == ()
    low = client.cache.get_plan(plsremind.Priority.LOW)
    assert low is not None
    assert low.before == (datetime.timedelta(days=2),)
    assert on_change is not None


async def test_done_and_reopened_item_moves_between_items_and_history(kit: helpers.Kit) -> None:
    mia = await kit.account("mia")
    client = kit.client(mia.token).client
    await client.start()
    item = await mia.rest.create_item(title="Call home")
    await until(lambda: client.cache.get_items())
    await mia.rest.update_item(item.id, status=plsremind.ItemStatus.DONE)
    await until(lambda: client.cache.get_history())
    assert client.cache.get_items() == ()
    assert ids(client.cache.get_history()) == [item.id]
    await mia.rest.update_item(item.id, status=plsremind.ItemStatus.OPEN)
    await until(lambda: client.cache.get_items())
    assert client.cache.get_history() == ()

    plain = kit.client(mia.token, Intents.ITEMS).client
    await plain.start()
    assert ids(plain.cache.get_items()) == [item.id]
    await mia.rest.update_item(item.id, status=plsremind.ItemStatus.DONE)
    await until(lambda: not plain.cache.get_items())
    assert plain.cache.get_history() == ()
    assert plain.cache.get_item(item.id) is None


async def test_history_continues_over_rest_exactly_where_the_sync_ends(kit: helpers.Kit) -> None:
    mia = await kit.account("mia")
    await kit.complete_in_the_past(mia.id, 1030)
    client = kit.client(mia.token).client
    await client.start()
    history = client.cache.get_history()
    cursor = client.cache.get_history_cursor()
    assert len(history) == 1000
    assert cursor is not None
    completions = [item.completed_at for item in history if item.completed_at is not None]
    assert completions == sorted(completions, reverse=True)

    older = [
        item
        async for item in plsremind.paginate(
            client.rest.list_items,
            status=plsremind.ItemStatus.DONE,
            sort=plsremind.ItemSort.COMPLETED,
            order=plsremind.SortOrder.DESCENDING,
            limit=100,
            cursor=cursor,
        )
    ]
    assert len(older) == 30
    assert len({*ids(history), *ids(tuple(older))}) == 1030
    assert older[0].completed_at is not None
    assert history[-1].completed_at is not None
    assert older[0].completed_at <= history[-1].completed_at

    finished = await mia.rest.create_item(title="just now")
    await mia.rest.update_item(finished.id, status=plsremind.ItemStatus.DONE)
    await until(lambda: client.cache.get_history()[0].id == finished.id)
    assert len(client.cache.get_history()) == 1001


async def test_a_dropped_connection_is_resumed_and_misses_nothing(kit: helpers.Kit) -> None:
    mia = await kit.account("mia")
    tapped = kit.client(mia.token)
    client = tapped.client
    await client.start()
    seen: list[str] = []

    @client.listen()
    async def on_event(event: plsremind.Event) -> None:
        if isinstance(event, plsremind.DisconnectedEvent | plsremind.ResumedEvent | plsremind.ReadyEvent):
            seen.append(type(event).__name__)

    tapped.tap.drop_current()
    await until(lambda: "DisconnectedEvent" in seen)
    item = await mia.rest.create_item(title="while away")
    await until(lambda: "ResumedEvent" in seen and client.cache.get_item(item.id) is not None, timeout=15)
    assert seen == ["DisconnectedEvent", "ResumedEvent"]
    assert len(tapped.tap.sockets) == 2
    assert client.status is plsremind.ClientStatus.CONNECTED
    assert on_event is not None


async def test_after_a_hub_restart_the_client_identifies_again_and_has_the_full_state(kit: helpers.Kit) -> None:
    mia = await kit.account("mia")
    client = kit.client(mia.token).client
    before = await mia.rest.create_item(title="before the restart")
    await client.start()
    ready: list[plsremind.ReadyEvent] = []

    @client.listen()
    async def on_ready(event: plsremind.ReadyEvent) -> None:
        ready.append(event)

    await asyncio.to_thread(kit.hub.restart)
    during = await mia.rest.create_item(title="during the restart")
    await until(lambda: ready and client.cache.get_item(during.id) is not None, timeout=40)
    assert sorted(ids(client.cache.get_items())) == sorted([before.id, during.id])
    assert client.status is plsremind.ClientStatus.CONNECTED
    assert len(ready) == 1
    assert on_ready is not None


async def test_a_reminder_arrives_is_acknowledged_and_shows_as_delivered(kit: helpers.Kit) -> None:
    mia = await kit.account("mia", remind=True)
    client = kit.client(mia.token, Intents.DELIVERIES).client
    await client.start()
    assert client.cache.get_users() == ()
    waiting = asyncio.create_task(client.wait_for(plsremind.DeliveryCreateEvent, timeout=30))
    item = await mia.rest.create_item(title="remind me")
    await mia.rest.create_reminder(item.id, at=soon())
    event = await waiting
    delivery = event.delivery
    assert delivery.attempt == 1
    assert delivery.reminder.kind is plsremind.ReminderKind.MANUAL
    assert delivery.reminder.offset is None
    assert (delivery.item.id, delivery.item.title) == (item.id, "remind me")
    assert delivery.user.id == mia.id
    assert delivery.user.external_ids == ()
    assert event.user_id == mia.id

    result = await client.rest.acknowledge_delivery(delivery.id, status=plsremind.DeliveryStatus.DELIVERED)
    assert result.status is plsremind.DeliveryStatus.DELIVERED
    log = await mia.rest.list_reminder_log(item_id=item.id)
    assert log.data[0].deliveries[0].status is plsremind.DeliveryStatus.DELIVERED
    assert log.data[0].deliveries[0].channel.kind is plsremind.ChannelKind.TOKEN
    assert log.data[0].outcome is plsremind.ReminderOutcome.FIRED
    assert log.data[0].kind is plsremind.ReminderKind.MANUAL


async def test_an_unacknowledged_reminder_comes_again_and_a_closed_one_is_a_conflict(kit: helpers.Kit) -> None:
    mia = await kit.account("mia", remind=True)
    tapped = kit.client(mia.token, Intents.DELIVERIES)
    client = tapped.client
    await client.start()
    attempts: list[int] = []
    delivery_ids: set[plsremind.ID] = set()
    closed: list[plsremind.DeliveryStatus] = []

    @client.listen()
    async def on_delivery(event: plsremind.DeliveryCreateEvent) -> None:
        delivery_ids.add(event.delivery.id)
        attempts.append(event.delivery.attempt)

    @client.listen()
    async def on_close(event: plsremind.DeliveryCloseEvent) -> None:
        assert event.delivery_id in delivery_ids
        closed.append(event.status)

    item = await mia.rest.create_item(title="remind me twice")
    await mia.rest.create_reminder(item.id, at=soon())
    await until(lambda: attempts == [1], timeout=30)
    tapped.tap.drop_current()
    await until(lambda: len(attempts) == 2, timeout=15)
    assert attempts == [1, 2]
    assert len(delivery_ids) == 1
    (delivery_id,) = delivery_ids
    assert [delivery.id for delivery in await client.rest.list_deliveries()] == [delivery_id]

    await mia.rest.update_item(item.id, status=plsremind.ItemStatus.DONE)
    await until(lambda: closed)
    assert closed == [plsremind.DeliveryStatus.CANCELLED]
    with pytest.raises(plsremind.ConflictError) as caught:
        await client.rest.acknowledge_delivery(delivery_id, status=plsremind.DeliveryStatus.DELIVERED)
    assert caught.value.code == "delivery_closed"
    assert on_delivery is not on_close


async def test_a_failed_delivery_shows_its_reason_in_the_log(kit: helpers.Kit) -> None:
    mia = await kit.account("mia", remind=True)
    client = kit.client(mia.token, Intents.DELIVERIES).client
    await client.start()
    waiting = asyncio.create_task(client.wait_for(plsremind.DeliveryCreateEvent, timeout=30))
    item = await mia.rest.create_item(title="unreachable")
    await mia.rest.create_reminder(item.id, at=soon())
    event = await waiting
    result = await client.rest.acknowledge_delivery(
        event.delivery.id, status=plsremind.DeliveryStatus.FAILED, reason="direct messages are closed"
    )
    assert (result.status, result.reason) == (plsremind.DeliveryStatus.FAILED, "direct messages are closed")
    log = await mia.rest.list_reminder_log(item_id=item.id)
    assert log.data[0].deliveries[0].reason == "direct messages are closed"


async def test_a_deleted_token_ends_the_client_with_4004(kit: helpers.Kit) -> None:
    mia = await kit.account("mia")
    token_id, token = await kit.personal_token(mia.browser)
    client = kit.client(token).client
    await client.start()
    waiting = asyncio.create_task(client.wait_for(plsremind.ClosedEvent, timeout=15))
    await mia.browser.call("DELETE", f"/users/@me/tokens/{token_id}", expect=204)
    closed = await waiting
    assert closed.error.code == 4004
    assert client.status is plsremind.ClientStatus.CLOSED
    await client.join()


async def test_a_token_that_does_not_exist_is_refused(kit: helpers.Kit) -> None:
    client = kit.client("plsr_pat_this-token-does-not-exist").client
    with pytest.raises(plsremind.GatewayError) as caught:
        await client.start()
    assert isinstance(caught.value.__cause__, plsremind.UnauthorizedError)
    assert client.status is plsremind.ClientStatus.CLOSED


async def test_history_without_items_is_refused_with_4013(kit: helpers.Kit) -> None:
    mia = await kit.account("mia")
    client = kit.client(mia.token, Intents.HISTORY).client
    with pytest.raises(plsremind.GatewayError) as caught:
        await client.start()
    assert caught.value.code == 4013
