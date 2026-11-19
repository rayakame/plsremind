from __future__ import annotations

import asyncio
import collections.abc
import logging
import pathlib
import signal
import sys
import typing

import aiohttp
import msgspec
import pytest

import plsremind
from plsremind import events
from plsremind.enums import ClientStatus
from plsremind.enums import Intents
from plsremind.internal import gateway
from tests.support import fakes
from tests.support import fixtures

TOKEN = "plsr_pat_secret"
ME = fixtures.USER_ID
ACK, RESUME, IDENTIFY, INVALID_SESSION, HEARTBEAT = 11, 6, 2, 9, 1
STATE = Intents.USERS | Intents.LISTS | Intents.ITEMS | Intents.PLANS
Document = dict[str, typing.Any]
type Make = collections.abc.Callable[..., plsremind.Client]
until = fakes.until


@pytest.fixture
async def make(hub: fakes.FakeHub) -> collections.abc.AsyncIterator[Make]:
    made: list[plsremind.Client] = []

    def build(intents: Intents = STATE | Intents.DELIVERIES, token: str = TOKEN) -> plsremind.Client:
        client = plsremind.Client(hub.url, token=token, intents=intents)
        made.append(client)
        return client

    try:
        yield build
    finally:
        for client in made:
            await client.close()


def item(number: int, **changes: object) -> Document:
    return {**fixtures.PLAIN_ITEM, "id": f"01930000-0000-7000-8000-{number:012x}", "title": f"item {number}", **changes}


def change(data: object, user: str = ME) -> Document:
    return {"user_id": user, "actor": fixtures.ACTOR, "nonce": None, "data": data}


def sync(user: str = ME, index: int = 0, count: int = 1, **parts: object) -> Document:
    first: Document = {"user": fixtures.USER, "lists": [fixtures.LIST], "plans": []} if index == 0 else {}
    return {"user_id": user, "chunk": {"index": index, "count": count}, **first, **parts}


async def handshake(hub: fakes.FakeHub, users: collections.abc.Sequence[str] = (ME,)) -> fakes.FakeConnection:
    connection = await hub.gateway.connection()
    await connection.hello()
    assert (await connection.receive())["op"] == IDENTIFY
    await connection.ready(users=users, principal=fixtures.PRINCIPAL, intents=["users", "lists", "items", "plans"])
    return connection


async def started(
    hub: fakes.FakeHub, client: plsremind.Client, items: collections.abc.Sequence[Document] = ()
) -> fakes.FakeConnection:
    starting = asyncio.create_task(client.start())
    connection = await handshake(hub)
    await connection.dispatch("user.sync", sync(items=list(items)))
    await asyncio.wait_for(starting, 2)
    return connection


def collect[E: events.Event](client: plsremind.Client, kind: type[E]) -> list[E]:
    seen: list[E] = []

    async def listener(event: E) -> None:
        seen.append(event)

    client.subscribe(kind, listener)
    return seen


async def test_start_returns_when_every_user_is_synced(hub: fakes.FakeHub, make: Make) -> None:
    client = make()
    ready = collect(client, plsremind.ReadyEvent)
    user_ready = collect(client, plsremind.UserReadyEvent)
    assert client.status is ClientStatus.IDLE
    assert client.principal is None
    starting = asyncio.create_task(client.start())
    connection = await handshake(hub, users=[ME, fixtures.OTHER_USER_ID])
    await connection.dispatch("user.sync", sync(count=2, items=[item(1)]))
    await connection.dispatch("user.sync", sync(fixtures.OTHER_USER_ID, items=[item(3)]))
    await asyncio.sleep(0.05)
    assert not starting.done()
    assert client.cache.get_users() == (fixtures.OTHER_USER_ID,)
    await connection.dispatch("user.sync", sync(index=1, count=2, items=[item(2)]))
    await asyncio.wait_for(starting, 2)
    assert [entry.title for entry in client.cache.get_items()] == ["item 1", "item 2"]
    assert client.status is ClientStatus.CONNECTED
    assert client.principal is not None
    assert client.principal.user_id == ME
    assert client.intents == STATE
    await until(lambda: ready)
    assert ready[0].users == (ME, fixtures.OTHER_USER_ID)
    assert ready[0].intents == STATE
    assert [event.user_id for event in user_ready] == [fixtures.OTHER_USER_ID, ME]


async def test_start_with_no_users_to_wait_for(hub: fakes.FakeHub, make: Make) -> None:
    client = make(Intents.DELIVERIES)
    starting = asyncio.create_task(client.start())
    await handshake(hub, users=[])
    await asyncio.wait_for(starting, 2)
    assert client.cache.get_users() == ()


async def test_a_user_gone_before_their_sync_does_not_hold_up_the_start(hub: fakes.FakeHub, make: Make) -> None:
    client = make()
    starting = asyncio.create_task(client.start())
    connection = await handshake(hub, users=[ME, fixtures.OTHER_USER_ID])
    await connection.dispatch("user.gone", {"user_id": fixtures.OTHER_USER_ID})
    await connection.dispatch("user.sync", sync())
    await asyncio.wait_for(starting, 2)
    assert client.cache.get_users() == (ME,)


async def test_start_without_intents_is_refused(make: Make) -> None:
    client = make(Intents.NONE)
    with pytest.raises(RuntimeError, match="intents"):
        await client.start()


async def test_start_twice_is_refused(hub: fakes.FakeHub, make: Make) -> None:
    client = make()
    await started(hub, client)
    with pytest.raises(RuntimeError, match="already"):
        await client.start()


async def test_start_raises_on_a_fatal_close(hub: fakes.FakeHub, make: Make) -> None:
    client = make()
    closed = collect(client, plsremind.ClosedEvent)
    starting = asyncio.create_task(client.start())
    connection = await hub.gateway.connection()
    await connection.hello()
    await connection.receive()
    await connection.close(4004, "Authentication failed.")
    with pytest.raises(plsremind.GatewayError, match="Authentication failed") as caught:
        await asyncio.wait_for(starting, 2)
    assert caught.value.code == 4004
    assert client.status is ClientStatus.CLOSED
    await until(lambda: closed)
    assert closed[0].error is caught.value


async def test_a_given_gateway_address_is_used_and_info_is_never_asked(hub: fakes.FakeHub) -> None:
    hub.answer("GET", "/info", fakes.error(503, "down"))
    address = hub.url.replace("http://", "ws://") + fakes.PREFIX + "/gateway"
    client = plsremind.Client(hub.url, token=TOKEN, intents=STATE, gateway_url=address)
    try:
        await started(hub, client)
        assert hub.requests == []
    finally:
        await client.close()


async def test_without_a_gateway_address_info_is_asked(hub: fakes.FakeHub, make: Make) -> None:
    await started(hub, make())
    assert [request.path for request in hub.requests] == ["/info"]


async def test_start_raises_when_the_hub_is_unreachable(hub: fakes.FakeHub, make: Make) -> None:
    hub.answer("GET", "/info", fakes.error(503, "unavailable", "Down."))
    client = make()
    with pytest.raises(plsremind.GatewayError, match="could not be reached"):
        await asyncio.wait_for(client.start(), 2)


async def test_a_cancelled_start_cleans_up(hub: fakes.FakeHub, make: Make) -> None:
    before = asyncio.all_tasks()
    client = make()
    starting = asyncio.create_task(client.start())
    connection = await hub.gateway.connection()
    starting.cancel()
    with pytest.raises(asyncio.CancelledError):
        await starting
    await connection.closed()
    await asyncio.sleep(0.05)
    assert asyncio.all_tasks() - before == set()
    assert client.status is ClientStatus.IDLE


async def test_listener_by_annotation(hub: fakes.FakeHub, make: Make) -> None:
    client = make()
    seen: list[plsremind.ItemCreateEvent] = []

    @client.listen()
    async def on_item(event: plsremind.ItemCreateEvent) -> None:
        seen.append(event)

    connection = await started(hub, client)
    await connection.dispatch("item.created", change(item(1)))
    await until(lambda: seen)
    assert seen[0].item.title == "item 1"
    assert on_item.__name__ == "on_item"


async def test_listener_by_argument(hub: fakes.FakeHub, make: Make) -> None:
    client = make()
    seen: list[object] = []

    @client.listen(plsremind.ItemCreateEvent)
    async def on_item(event: plsremind.Event) -> None:
        seen.append(event)

    connection = await started(hub, client)
    await connection.dispatch("item.created", change(item(1)))
    await until(lambda: seen)
    assert isinstance(seen[0], plsremind.ItemCreateEvent)
    assert on_item is not None


async def test_listener_on_a_base_class_gets_subclasses(hub: fakes.FakeHub, make: Make) -> None:
    client = make()
    items = collect(client, plsremind.ItemEvent)
    everything = collect(client, plsremind.Event)
    connection = await started(hub, client)
    await connection.dispatch("item.created", change(item(1)))
    await connection.dispatch("item.updated", change(item(1, title="renamed")))
    await connection.dispatch("item.deleted", change({"id": item(1)["id"]}))
    await connection.dispatch("list.created", change({**fixtures.LIST, "id": "l2", "position": 1}))
    await until(lambda: len(items) == 3)
    assert [type(event) for event in items] == [
        plsremind.ItemCreateEvent,
        plsremind.ItemUpdateEvent,
        plsremind.ItemDeleteEvent,
    ]
    await until(lambda: plsremind.ListCreateEvent in {type(event) for event in everything})
    assert {plsremind.UserSyncEvent, plsremind.UserReadyEvent, plsremind.ReadyEvent} <= {
        type(event) for event in everything
    }


async def test_unsubscribe(hub: fakes.FakeHub, make: Make) -> None:
    client = make()
    seen: list[plsremind.ItemCreateEvent] = []
    other = collect(client, plsremind.ItemCreateEvent)

    async def listener(event: plsremind.ItemCreateEvent) -> None:
        seen.append(event)

    client.subscribe(plsremind.ItemCreateEvent, listener)
    connection = await started(hub, client)
    await connection.dispatch("item.created", change(item(1)))
    await until(lambda: seen)
    client.unsubscribe(plsremind.ItemCreateEvent, listener)
    client.unsubscribe(plsremind.ItemCreateEvent, listener)
    await connection.dispatch("item.created", change(item(2)))
    await until(lambda: len(other) == 2)
    assert len(seen) == 1


async def test_bad_listeners_are_refused(make: Make) -> None:
    client = make()

    def not_async(event: plsremind.ItemCreateEvent) -> None:
        del event

    async def two(event: plsremind.ItemCreateEvent, more: int) -> None:
        del event, more

    async def none() -> None:
        await asyncio.sleep(0)

    async def not_an_event(event: int) -> None:
        del event

    async def bare(event) -> None:  # ruff: ignore[missing-type-function-argument]  # pyright: ignore[reportUnknownParameterType, reportMissingParameterType]
        del event

    async def fine(event: plsremind.ItemCreateEvent) -> None:
        del event

    with pytest.raises(TypeError, match=r"not_async.*async def"):
        client.listen()(not_async)  # pyright: ignore[reportArgumentType]
    with pytest.raises(TypeError, match=r"two.*exactly one parameter"):
        client.listen()(two)  # pyright: ignore[reportArgumentType]
    with pytest.raises(TypeError, match=r"none.*exactly one parameter"):
        client.listen()(none)  # pyright: ignore[reportArgumentType]
    with pytest.raises(TypeError, match=r"not_an_event.*event class"):
        client.listen()(not_an_event)
    with pytest.raises(TypeError, match=r"bare.*annotate"):
        client.listen()(bare)
    with pytest.raises(TypeError, match="event class"):
        client.subscribe(int, fine)  # pyright: ignore[reportArgumentType]


async def test_a_method_can_listen(hub: fakes.FakeHub, make: Make) -> None:
    class Bot:
        def __init__(self) -> None:
            self.seen: list[plsremind.ItemCreateEvent] = []

        async def on_item(self, event: plsremind.ItemCreateEvent) -> None:
            await asyncio.sleep(0)
            self.seen.append(event)

    client, bot = make(), Bot()
    client.listen()(bot.on_item)
    connection = await started(hub, client)
    await connection.dispatch("item.created", change(item(1)))
    await until(lambda: bot.seen)


async def test_update_event_carries_the_old_item_and_the_cache_is_ahead(hub: fakes.FakeHub, make: Make) -> None:
    client = make()
    seen: list[tuple[plsremind.ItemUpdateEvent, plsremind.Item | None]] = []

    @client.listen()
    async def on_update(event: plsremind.ItemUpdateEvent) -> None:
        seen.append((event, client.cache.get_item(event.item.id)))

    connection = await started(hub, client, items=[item(1)])
    before = client.cache.get_item(plsremind.ID(item(1)["id"]))
    await connection.dispatch("item.updated", change(item(1, title="renamed")))
    await until(lambda: seen)
    event, cached = seen[0]
    assert event.old_item is before
    assert cached is event.item
    assert on_update is not None


async def test_events_reach_a_listener_in_order(hub: fakes.FakeHub, make: Make) -> None:
    client = make()
    created = collect(client, plsremind.ItemCreateEvent)
    connection = await started(hub, client)
    for number in range(50):
        await connection.dispatch("item.created", change(item(number)))
    await until(lambda: len(created) == 50)
    assert [event.item.title for event in created] == [f"item {number}" for number in range(50)]


async def test_a_payload_nobody_wants_is_not_decoded(
    hub: fakes.FakeHub, make: Make, caplog: pytest.LogCaptureFixture
) -> None:
    client = make()
    lists = collect(client, plsremind.ListCreateEvent)
    connection = await started(hub, client)
    with caplog.at_level(logging.WARNING, logger="plsremind"):
        await connection.dispatch("delivery.created", {"broken": True}, sequenced=False)
        await connection.dispatch("subitem.created", {"broken": True})
        await connection.dispatch("item.exploded", {"new": "event"})
        await connection.dispatch("list.created", change({**fixtures.LIST, "id": "l2"}))
        await until(lambda: lists)
    assert caplog.records == []


async def test_a_payload_that_cannot_be_read_is_logged_and_skipped(
    hub: fakes.FakeHub, make: Make, caplog: pytest.LogCaptureFixture
) -> None:
    client = make()
    deliveries = collect(client, plsremind.DeliveryCreateEvent)
    connection = await started(hub, client)
    with caplog.at_level(logging.ERROR, logger="plsremind"):
        await connection.dispatch("delivery.created", {"broken": True}, sequenced=False)
        await connection.dispatch("delivery.created", change(fixtures.DELIVERY), sequenced=False)
        await until(lambda: deliveries)
    assert len(deliveries) == 1
    assert ["delivery.created" in record.getMessage() for record in caplog.records] == [True]


async def test_delivery_reaches_a_client_that_keeps_no_state(hub: fakes.FakeHub, make: Make) -> None:
    client = make(Intents.DELIVERIES)
    deliveries = collect(client, plsremind.DeliveryCreateEvent)
    closed = collect(client, plsremind.DeliveryCloseEvent)
    starting = asyncio.create_task(client.start())
    connection = await handshake(hub, users=[])
    await asyncio.wait_for(starting, 2)
    await connection.dispatch("delivery.created", change(fixtures.DELIVERY), sequenced=False)
    await connection.dispatch(
        "delivery.closed", change({"id": fixtures.DELIVERY_ID, "status": "cancelled"}), sequenced=False
    )
    await until(lambda: deliveries and closed)
    assert deliveries[0].delivery.user.external_ids == ("1234",)
    assert closed[0].status is plsremind.DeliveryStatus.CANCELLED
    assert client.cache.get_users() == ()
    assert client.cache.get_item(deliveries[0].delivery.item.id) is None


async def test_delivery_is_reported_again_after_a_resume_and_never_acknowledged(hub: fakes.FakeHub, make: Make) -> None:
    client = make()
    deliveries = collect(client, plsremind.DeliveryCreateEvent)
    resumed = collect(client, plsremind.ResumedEvent)
    disconnected = collect(client, plsremind.DisconnectedEvent)
    connection = await started(hub, client)
    await connection.dispatch("delivery.created", change(fixtures.DELIVERY), sequenced=False)
    await until(lambda: deliveries)
    await connection.send(7)
    again = await hub.gateway.connection()
    await again.hello()
    assert (await again.receive_op(RESUME))["d"]["session_id"] == "s1"
    await again.dispatch("RESUMED", {}, sequenced=False)
    await again.dispatch("delivery.created", change({**fixtures.DELIVERY, "attempt": 2}), sequenced=False)
    await until(lambda: len(deliveries) == 2)
    assert [event.delivery.attempt for event in deliveries] == [1, 2]
    assert [event.delivery.id for event in deliveries] == [fixtures.DELIVERY_ID] * 2
    await until(lambda: resumed and disconnected)
    assert disconnected[0].reason == "The hub asked to reconnect."
    assert [request.path for request in hub.requests] == ["/info"]


async def test_raising_listener_becomes_an_exception_event(hub: fakes.FakeHub, make: Make) -> None:
    client = make()
    failures = collect(client, plsremind.ExceptionEvent)
    others = collect(client, plsremind.ItemCreateEvent)

    @client.listen()
    async def broken(event: plsremind.ItemCreateEvent) -> None:
        msg = f"cannot handle {event.item.title}"
        raise ValueError(msg)

    connection = await started(hub, client)
    await connection.dispatch("item.created", change(item(1)))
    await connection.dispatch("item.created", change(item(2)))
    await until(lambda: len(failures) == 2 and len(others) == 2)
    assert str(failures[0].exception) == "cannot handle item 1"
    assert isinstance(failures[0].failed_event, plsremind.ItemCreateEvent)
    assert failures[0].failed_listener is broken
    assert client.status is ClientStatus.CONNECTED


async def test_raising_listener_is_logged_without_an_exception_listener(
    hub: fakes.FakeHub, make: Make, caplog: pytest.LogCaptureFixture
) -> None:
    client = make()

    @client.listen()
    async def broken(event: plsremind.ItemCreateEvent) -> None:
        del event
        msg = "boom"
        raise ValueError(msg)

    connection = await started(hub, client)
    with caplog.at_level(logging.ERROR, logger="plsremind"):
        await connection.dispatch("item.created", change(item(1)))
        await until(lambda: caplog.records)
    record = caplog.records[0]
    assert "broken" in record.getMessage()
    assert "ItemCreateEvent" in record.getMessage()
    assert record.exc_info is not None
    assert isinstance(record.exc_info[1], ValueError)


async def test_raising_exception_listener_is_only_logged(
    hub: fakes.FakeHub, make: Make, caplog: pytest.LogCaptureFixture
) -> None:
    client = make()
    calls: list[plsremind.ExceptionEvent] = []

    @client.listen()
    async def broken(event: plsremind.ItemCreateEvent) -> None:
        del event
        msg = "first"
        raise ValueError(msg)

    @client.listen()
    async def also_broken(event: plsremind.ExceptionEvent) -> None:
        calls.append(event)
        msg = "second"
        raise RuntimeError(msg)

    connection = await started(hub, client)
    with caplog.at_level(logging.ERROR, logger="plsremind"):
        await connection.dispatch("item.created", change(item(1)))
        await until(lambda: caplog.records)
        await asyncio.sleep(0.05)
    assert len(calls) == 1
    assert len(caplog.records) == 1
    assert "also_broken" in caplog.records[0].getMessage()
    assert broken is not also_broken


async def test_slow_listener_blocks_neither_heartbeats_nor_other_listeners(hub: fakes.FakeHub, make: Make) -> None:
    client = make()
    release = asyncio.Event()
    entered: list[str] = []
    quick = collect(client, plsremind.ItemCreateEvent)

    @client.listen()
    async def slow(event: plsremind.ItemCreateEvent) -> None:
        entered.append(event.item.title)
        await release.wait()

    starting = asyncio.create_task(client.start())
    connection = await hub.gateway.connection()
    await connection.hello(40)
    await connection.receive_op(IDENTIFY)
    await connection.ready(users=[], principal=fixtures.PRINCIPAL)
    await asyncio.wait_for(starting, 2)
    await connection.dispatch("item.created", change(item(1)))
    await connection.dispatch("item.created", change(item(2)))
    for _ in range(3):
        await connection.receive_op(1)
        await connection.send(ACK)
    assert entered == ["item 1", "item 2"]
    assert len(quick) == 2
    release.set()
    assert slow is not None


async def test_wait_for_with_predicate_and_timeout(hub: fakes.FakeHub, make: Make) -> None:
    client = make()
    connection = await started(hub, client)
    waiting = asyncio.create_task(
        client.wait_for(
            plsremind.ItemEvent, timeout=2, predicate=lambda event: isinstance(event, events.ItemDeleteEvent)
        )
    )
    await asyncio.sleep(0)
    await connection.dispatch("item.created", change(item(1)))
    await connection.dispatch("item.deleted", change({"id": item(1)["id"]}))
    await connection.dispatch("item.deleted", change({"id": item(2)["id"]}))
    event = await waiting
    assert isinstance(event, plsremind.ItemDeleteEvent)
    assert event.item_id == item(1)["id"]
    with pytest.raises(TimeoutError):
        await client.wait_for(plsremind.ListDeleteEvent, timeout=0.05)
    unlimited = asyncio.create_task(client.wait_for(plsremind.ListCreateEvent, timeout=None))
    await asyncio.sleep(0)
    await connection.dispatch("list.created", change({**fixtures.LIST, "id": "l3"}))
    assert (await asyncio.wait_for(unlimited, 2)).list.id == "l3"


async def test_a_raising_predicate_reaches_the_waiter(hub: fakes.FakeHub, make: Make) -> None:
    client = make()
    connection = await started(hub, client)

    def predicate(event: plsremind.ItemCreateEvent) -> bool:
        raise LookupError(event.item.title)

    waiting = asyncio.create_task(client.wait_for(plsremind.ItemCreateEvent, timeout=2, predicate=predicate))
    await asyncio.sleep(0)
    await connection.dispatch("item.created", change(item(1)))
    with pytest.raises(LookupError, match="item 1"):
        await waiting


async def test_ready_event_again_after_an_invalid_session(hub: fakes.FakeHub, make: Make) -> None:
    client = make()
    ready = collect(client, plsremind.ReadyEvent)
    connection = await started(hub, client, items=[item(1)])
    connection.drop()
    again = await hub.gateway.connection()
    await again.hello()
    await again.receive_op(RESUME)
    await again.send(INVALID_SESSION, d=False)
    await again.receive_op(IDENTIFY)
    await again.ready(users=[ME], principal=fixtures.PRINCIPAL, intents=["users", "lists", "items", "plans"])
    await again.dispatch("user.sync", sync(count=2, items=[item(2)]))
    await asyncio.sleep(0.05)
    assert [entry.title for entry in client.cache.get_items()] == ["item 1"]
    assert len(ready) == 1
    await again.dispatch("user.sync", sync(index=1, count=2, items=[item(3)]))
    await until(lambda: len(ready) == 2)
    assert [entry.title for entry in client.cache.get_items()] == ["item 2", "item 3"]


async def test_status_follows_the_connection(hub: fakes.FakeHub, make: Make) -> None:
    client = make()
    connection = await started(hub, client)
    assert client.status is ClientStatus.CONNECTED
    await connection.close(1012, "restarting")
    await until(lambda: client.status is ClientStatus.RECONNECTING)
    await client.close()
    assert client.status is ClientStatus.IDLE


async def test_closed_event_on_a_fatal_close_after_start(hub: fakes.FakeHub, make: Make) -> None:
    client = make()
    closed = collect(client, plsremind.ClosedEvent)
    connection = await started(hub, client, items=[item(1)])
    joining = asyncio.create_task(client.join())
    await connection.close(4004, "The token was deleted.")
    await until(lambda: closed)
    await asyncio.wait_for(joining, 2)
    assert closed[0].error.code == 4004
    assert client.status is ClientStatus.CLOSED
    assert len(client.cache.get_items()) == 1
    await asyncio.sleep(0.05)
    assert hub.gateway.none_waiting()


async def test_close_is_idempotent_and_leaves_no_tasks(hub: fakes.FakeHub, make: Make) -> None:
    before = asyncio.all_tasks()
    client = make()
    connection = await started(hub, client, items=[item(1)])
    await asyncio.gather(client.close(), client.close())
    await client.close()
    assert await connection.closed() == 1000
    await asyncio.sleep(0.05)
    assert asyncio.all_tasks() - before == set()
    assert client.status is ClientStatus.IDLE
    assert len(client.cache.get_items()) == 1


async def test_close_before_start(make: Make) -> None:
    client = make()
    await client.close()
    await client.join()


async def test_close_waits_for_running_listeners_and_cancels_stuck_ones(
    hub: fakes.FakeHub, make: Make, monkeypatch: pytest.MonkeyPatch
) -> None:
    client = make()
    finished: list[str] = []
    cancelled: list[str] = []

    brief_entered, stuck_entered = asyncio.Event(), asyncio.Event()

    @client.listen()
    async def brief(event: plsremind.ItemCreateEvent) -> None:
        brief_entered.set()
        await asyncio.sleep(0.05)
        finished.append(event.item.title)

    @client.listen()
    async def stuck(event: plsremind.ItemCreateEvent) -> None:
        stuck_entered.set()
        try:
            await asyncio.sleep(60)
        except asyncio.CancelledError:
            cancelled.append(event.item.title)
            raise

    monkeypatch.setattr(plsremind.client, "LISTENER_GRACE", 0.2)
    connection = await started(hub, client)
    await connection.dispatch("item.created", change(item(1)))
    await asyncio.wait_for(asyncio.gather(brief_entered.wait(), stuck_entered.wait()), 2)
    await client.close()
    assert finished == ["item 1"]
    assert cancelled == ["item 1"]
    assert brief is not stuck


async def test_close_from_inside_a_listener(hub: fakes.FakeHub, make: Make) -> None:
    client = make()
    done = asyncio.Event()

    @client.listen()
    async def on_item(event: plsremind.ItemCreateEvent) -> None:
        del event
        await client.close()
        done.set()

    connection = await started(hub, client)
    await connection.dispatch("item.created", change(item(1)))
    await asyncio.wait_for(done.wait(), 2)
    assert await connection.closed() == 1000
    assert client.status is ClientStatus.IDLE
    assert on_item is not None


async def test_close_during_a_reconnect_wait(hub: fakes.FakeHub, make: Make) -> None:
    client = make()
    connection = await started(hub, client)
    connection.drop()
    await until(lambda: client.status is ClientStatus.RECONNECTING)
    await client.close()
    assert not [task for task in asyncio.all_tasks() if task.get_name().startswith("plsremind")]
    await asyncio.sleep(1.2)
    assert hub.gateway.none_waiting()


async def test_restart_after_close(hub: fakes.FakeHub, make: Make) -> None:
    client = make()
    await started(hub, client, items=[item(1)])
    await client.close()
    await started(hub, client, items=[item(2)])
    assert [entry.title for entry in client.cache.get_items()] == ["item 2"]
    assert client.status is ClientStatus.CONNECTED


async def test_async_with_starts_and_closes(hub: fakes.FakeHub, make: Make) -> None:
    client = make(Intents.DELIVERIES)

    async def serve() -> fakes.FakeConnection:
        return await handshake(hub, users=[])

    serving = asyncio.create_task(serve())
    async with client as entered:
        assert entered is client
        assert client.status is ClientStatus.CONNECTED
    connection = await serving
    assert await connection.closed() == 1000
    assert client.status is ClientStatus.IDLE


async def test_join_returns_after_close(hub: fakes.FakeHub, make: Make) -> None:
    client = make()
    await started(hub, client)
    joining = asyncio.create_task(client.join())
    await asyncio.sleep(0.02)
    assert not joining.done()
    await client.close()
    await asyncio.wait_for(joining, 2)


async def test_rest_works_without_the_gateway(hub: fakes.FakeHub, make: Make) -> None:
    client = make(Intents.NONE)
    hub.answer("GET", "/users/@me/lists", fakes.Reply(json=[fixtures.LIST]))
    assert (await client.rest.list_lists())[0].name == "Inbox"
    assert hub.requests[0].headers["Authorization"] == f"Bearer {TOKEN}"


async def test_run_closes_on_sigterm() -> None:
    script = pathlib.Path(__file__).parents[1] / "support" / "run_until_signal.py"
    process = await asyncio.create_subprocess_exec(
        sys.executable,
        "-m",
        f"tests.support.{script.stem}",
        stdout=asyncio.subprocess.PIPE,
        stderr=asyncio.subprocess.STDOUT,
        cwd=script.parents[2],
    )
    assert process.stdout is not None
    try:
        assert (await asyncio.wait_for(process.stdout.readline(), 15)).decode().strip() == "ready 0"
        process.send_signal(signal.SIGTERM)
        rest, _ = await asyncio.wait_for(process.communicate(), 15)
    finally:
        if process.returncode is None:
            process.kill()
    lines = rest.decode().split()
    assert process.returncode == 0, rest.decode()
    assert "stopped idle" in rest.decode()
    assert "1000" in lines


async def test_a_given_session_serves_rest_and_gateway_and_stays_open(hub: fakes.FakeHub) -> None:
    hub.answer("GET", "/users/@me/lists", fakes.Reply(json=[]))
    async with aiohttp.ClientSession(headers={"X-Forwarded-For": "10.1.2.3"}) as session:
        client = plsremind.Client(hub.url, token=TOKEN, intents=Intents.DELIVERIES, session=session)
        starting = asyncio.create_task(client.start())
        connection = await handshake(hub, users=[])
        await asyncio.wait_for(starting, 2)
        await client.rest.list_lists()
        assert connection.headers["X-Forwarded-For"] == "10.1.2.3"
        assert [request.headers["X-Forwarded-For"] for request in hub.requests] == ["10.1.2.3"] * 2
        await client.close()
        assert not session.closed


async def test_the_reason_the_hub_could_not_be_reached_is_kept(hub: fakes.FakeHub, make: Make) -> None:
    hub.answer("GET", "/info", fakes.error(401, "unauthorized", "The token is not valid."))
    client = make()
    with pytest.raises(plsremind.GatewayError) as caught:
        await asyncio.wait_for(client.start(), 2)
    assert isinstance(caught.value.__cause__, plsremind.UnauthorizedError)
    assert client.status is ClientStatus.CLOSED


async def test_an_unreadable_ready_ends_the_client(hub: fakes.FakeHub, make: Make) -> None:
    client = make()
    starting = asyncio.create_task(client.start())
    connection = await hub.gateway.connection()
    await connection.hello()
    await connection.receive()
    await connection.dispatch("READY", {"session_id": "s1", "intents": ["items"], "users": []})
    with pytest.raises(plsremind.GatewayError, match="READY could not be read") as caught:
        await asyncio.wait_for(starting, 2)
    assert isinstance(caught.value.__cause__, msgspec.MsgspecError)
    assert client.status is ClientStatus.CLOSED


async def test_an_unreadable_sync_ends_the_client(hub: fakes.FakeHub, make: Make) -> None:
    client = make()
    starting = asyncio.create_task(client.start())
    connection = await handshake(hub)
    await connection.dispatch("user.sync", sync(items=[{"id": 5}]))
    with pytest.raises(plsremind.GatewayError, match="state of a user could not be read"):
        await asyncio.wait_for(starting, 2)
    assert client.status is ClientStatus.CLOSED


async def test_an_unreadable_change_starts_the_session_over_so_the_cache_cannot_drift(
    hub: fakes.FakeHub, make: Make, caplog: pytest.LogCaptureFixture
) -> None:
    client = make()
    ready = collect(client, plsremind.ReadyEvent)
    connection = await started(hub, client, items=[item(1)])
    with caplog.at_level(logging.ERROR, logger="plsremind"):
        await connection.dispatch("item.updated", change({"id": item(1)["id"], "title": 5}))
        again = await hub.gateway.connection()
    assert "item.updated" in caplog.records[0].getMessage()
    await again.hello()
    assert (await again.receive())["op"] == IDENTIFY
    await again.ready(users=[ME], principal=fixtures.PRINCIPAL, intents=["users", "lists", "items", "plans"])
    await again.dispatch("user.sync", sync(items=[item(1, title="as the hub has it")]))
    await until(lambda: len(ready) == 2)
    assert [entry.title for entry in client.cache.get_items()] == ["as the hub has it"]


async def test_exception_events_reach_base_class_listeners_and_waiters(hub: fakes.FakeHub, make: Make) -> None:
    client = make()
    everything = collect(client, plsremind.Event)

    @client.listen()
    async def broken(event: plsremind.ItemCreateEvent) -> None:
        raise ValueError(event.item.title)

    connection = await started(hub, client)
    waiting = asyncio.create_task(client.wait_for(plsremind.ExceptionEvent, timeout=2))
    await asyncio.sleep(0)
    await connection.dispatch("item.created", change(item(1)))
    failure = await waiting
    assert str(failure.exception) == "item 1"
    await until(lambda: plsremind.ExceptionEvent in {type(event) for event in everything})
    assert broken is not None


async def test_close_during_the_first_connect_fails_the_start(hub: fakes.FakeHub, make: Make) -> None:
    before = asyncio.all_tasks()
    client = make()
    starting = asyncio.create_task(client.start())
    connection = await hub.gateway.connection()
    await connection.hello()
    await connection.receive()
    await client.close()
    with pytest.raises(plsremind.GatewayError, match="The client was closed"):
        await asyncio.wait_for(starting, 2)
    assert await connection.closed() == 1000
    assert client.status is ClientStatus.IDLE
    await asyncio.sleep(0.05)
    assert asyncio.all_tasks() - before == set()


async def test_start_after_the_hub_turned_the_client_away_leaks_no_session(
    hub: fakes.FakeHub, make: Make, monkeypatch: pytest.MonkeyPatch
) -> None:
    opened: list[aiohttp.ClientSession] = []
    original = aiohttp.ClientSession

    def recording() -> aiohttp.ClientSession:
        opened.append(original())
        return opened[-1]

    monkeypatch.setattr(plsremind.client.aiohttp, "ClientSession", recording)
    client = make()
    connection = await started(hub, client)
    await connection.close(4004, "no")
    await until(lambda: client.status is ClientStatus.CLOSED)
    await started(hub, client)
    assert client.status is ClientStatus.CONNECTED
    # One session for REST and one for the gateway; the socket session of the first start is gone.
    assert sum(not session.closed for session in opened) == 2
    await client.close()
    assert all(session.closed for session in opened)


async def test_a_listener_calling_close_while_the_client_closes_does_not_stall(hub: fakes.FakeHub, make: Make) -> None:
    client = make()
    entered = asyncio.Event()
    release = asyncio.Event()

    @client.listen()
    async def closes(event: plsremind.ItemCreateEvent) -> None:
        del event
        entered.set()
        await release.wait()
        await client.close()

    connection = await started(hub, client)
    await connection.dispatch("item.created", change(item(1)))
    await asyncio.wait_for(entered.wait(), 2)
    closing = asyncio.create_task(client.close())
    await asyncio.sleep(0.01)
    release.set()
    await asyncio.wait_for(closing, 1)
    assert closes is not None


async def test_no_listener_task_outlives_close(hub: fakes.FakeHub, make: Make, monkeypatch: pytest.MonkeyPatch) -> None:
    client = make()
    entered = asyncio.Event()

    @client.listen()
    async def fails_slowly(event: plsremind.ItemCreateEvent) -> None:
        entered.set()
        await asyncio.sleep(0.05)
        raise ValueError(event.item.title)

    @client.listen()
    async def stuck(event: plsremind.ExceptionEvent) -> None:
        del event
        await asyncio.sleep(60)

    monkeypatch.setattr(plsremind.client, "LISTENER_GRACE", 0.2)
    connection = await started(hub, client)
    await connection.dispatch("item.created", change(item(1)))
    await asyncio.wait_for(entered.wait(), 2)
    await client.close()
    assert not [task for task in asyncio.all_tasks() if task.get_name().startswith("plsremind-listener")]
    assert fails_slowly is not stuck


async def test_a_connection_must_name_the_user_even_before_it_is_connected(hub: fakes.FakeHub, make: Make) -> None:
    client = make(token="plsr_con_secret")
    with pytest.raises(TypeError, match="which user"):
        client.cache.get_items()
    assert client.cache.get_items(plsremind.ID(ME)) == ()
    assert hub.requests == []


async def test_waiters_fail_when_the_client_closes(hub: fakes.FakeHub, make: Make) -> None:
    client = make()
    await started(hub, client)
    waiting = asyncio.create_task(client.wait_for(plsremind.ItemCreateEvent, timeout=None))
    await asyncio.sleep(0)
    await client.close()
    with pytest.raises(plsremind.GatewayError, match="The client was closed"):
        await asyncio.wait_for(waiting, 1)


async def test_waiters_fail_when_the_hub_turns_the_client_away(hub: fakes.FakeHub, make: Make) -> None:
    client = make()
    connection = await started(hub, client)
    waiting = asyncio.create_task(client.wait_for(plsremind.ItemCreateEvent, timeout=None))
    closed = asyncio.create_task(client.wait_for(plsremind.ClosedEvent, timeout=2))
    await asyncio.sleep(0)
    await connection.close(4004, "The token was deleted.")
    assert (await closed).error.code == 4004
    with pytest.raises(plsremind.GatewayError, match="The token was deleted"):
        await asyncio.wait_for(waiting, 1)


async def test_waiting_after_the_hub_turned_the_client_away_fails_at_once(hub: fakes.FakeHub, make: Make) -> None:
    client = make()
    connection = await started(hub, client)
    closed = asyncio.create_task(client.wait_for(plsremind.ClosedEvent, timeout=2))
    await asyncio.sleep(0)
    await connection.close(4004, "The token was deleted.")
    await closed
    with pytest.raises(plsremind.GatewayError, match="The token was deleted"):
        await asyncio.wait_for(client.wait_for(plsremind.ItemCreateEvent, timeout=None), 1)


async def test_run_stops_on_a_signal_while_still_connecting() -> None:
    script = pathlib.Path(__file__).parents[1] / "support" / "run_against_silence.py"
    process = await asyncio.create_subprocess_exec(
        sys.executable,
        "-m",
        f"tests.support.{script.stem}",
        stdout=asyncio.subprocess.PIPE,
        stderr=asyncio.subprocess.STDOUT,
        cwd=script.parents[2],
    )
    assert process.stdout is not None
    try:
        assert (await asyncio.wait_for(process.stdout.readline(), 15)).decode().strip() == "connecting"
        await asyncio.sleep(0.3)
        process.send_signal(signal.SIGINT)
        rest, _ = await asyncio.wait_for(process.communicate(), 5)
    finally:
        if process.returncode is None:
            process.kill()
            await process.wait()
    assert process.returncode == 0, rest.decode()
    assert "stopped idle" in rest.decode()


async def test_two_starts_at_once_after_the_hub_turned_the_client_away(hub: fakes.FakeHub, make: Make) -> None:
    before = asyncio.all_tasks()
    client = make()
    connection = await started(hub, client)
    await connection.close(4004, "no")
    await until(lambda: client.status is ClientStatus.CLOSED)
    first = asyncio.create_task(client.start())
    second = asyncio.create_task(client.start())
    connection = await handshake(hub)
    await connection.dispatch("user.sync", sync())
    done, _ = await asyncio.wait({first, second}, timeout=2)
    assert len(done) == 2
    outcomes = sorted(type(task.exception()).__name__ for task in done)
    assert outcomes == ["NoneType", "RuntimeError"]
    assert hub.gateway.none_waiting()
    await client.close()
    await asyncio.sleep(0.05)
    assert asyncio.all_tasks() - before == set()


async def test_start_while_closing_waits_for_the_close(hub: fakes.FakeHub, make: Make) -> None:
    client = make()
    closed = collect(client, plsremind.ClosedEvent)
    await started(hub, client)
    closing = asyncio.create_task(client.close())
    await asyncio.sleep(0)
    starting = asyncio.create_task(client.start())
    connection = await handshake(hub)
    await connection.dispatch("user.sync", sync())
    await asyncio.wait_for(starting, 2)
    await closing
    assert client.status is ClientStatus.CONNECTED
    assert closed == []


async def test_a_failure_while_applying_a_change_starts_the_session_over(
    hub: fakes.FakeHub, make: Make, monkeypatch: pytest.MonkeyPatch, caplog: pytest.LogCaptureFixture
) -> None:
    client = make()
    connection = await started(hub, client, items=[item(1)])
    original = type(client.cache).apply  # pyright: ignore[reportAttributeAccessIssue, reportUnknownMemberType, reportUnknownVariableType]

    def failing(store: object, event: plsremind.Event) -> object:
        if isinstance(event, plsremind.ItemUpdateEvent):
            msg = "the cache tripped"
            raise KeyError(msg)
        return original(store, event)  # pyright: ignore[reportUnknownVariableType]

    monkeypatch.setattr(type(client.cache), "apply", failing)
    with caplog.at_level(logging.ERROR, logger="plsremind"):
        await connection.dispatch("item.updated", change(item(1, title="renamed")))
        again = await hub.gateway.connection()
    assert "item.updated" in caplog.records[0].getMessage()
    await again.hello()
    assert (await again.receive())["op"] == IDENTIFY


async def test_start_gives_up_when_the_state_never_arrives(
    hub: fakes.FakeHub, make: Make, monkeypatch: pytest.MonkeyPatch
) -> None:
    monkeypatch.setattr(gateway, "HANDSHAKE_SILENCE", 0.15)
    client = make()
    starting = asyncio.create_task(client.start())
    await handshake(hub)
    with pytest.raises(plsremind.GatewayError, match="did not finish the handshake"):
        await asyncio.wait_for(starting, 2)
    assert client.status is ClientStatus.CLOSED


async def test_answered_heartbeats_do_not_keep_a_stalled_start_waiting(
    hub: fakes.FakeHub, make: Make, monkeypatch: pytest.MonkeyPatch
) -> None:
    monkeypatch.setattr(gateway, "HANDSHAKE_SILENCE", 0.3)
    client = make()
    starting = asyncio.create_task(client.start())
    connection = await hub.gateway.connection()
    await connection.hello(interval_ms=50)

    async def answer_heartbeats() -> None:
        while True:
            if (await connection.receive(timeout=5))["op"] == HEARTBEAT:
                await connection.send(ACK)

    await connection.receive_op(IDENTIFY)
    await connection.ready(users=[ME], principal=fixtures.PRINCIPAL, intents=["users", "lists", "items", "plans"])
    answering = asyncio.create_task(answer_heartbeats())
    try:
        with pytest.raises(plsremind.GatewayError, match="did not finish the handshake"):
            await asyncio.wait_for(starting, 2)
    finally:
        answering.cancel()
        await asyncio.gather(answering, return_exceptions=True)


async def test_a_resync_that_stalls_is_started_again_from_scratch(
    hub: fakes.FakeHub, make: Make, monkeypatch: pytest.MonkeyPatch
) -> None:
    client = make()
    connection = await started(hub, client, items=[item(1)])
    monkeypatch.setattr(gateway, "HANDSHAKE_SILENCE", 0.15)
    connection.drop()
    again = await hub.gateway.connection()
    await again.hello()
    await again.receive_op(RESUME)
    await again.send(INVALID_SESSION, d=False)
    await again.receive_op(IDENTIFY)
    await again.ready(users=[ME], principal=fixtures.PRINCIPAL, intents=["users", "lists", "items", "plans"])
    assert client.status is ClientStatus.RECONNECTING
    third = await hub.gateway.connection(timeout=5)
    await third.hello()
    assert (await third.receive())["op"] == IDENTIFY
    assert [entry.title for entry in client.cache.get_items()] == ["item 1"]


async def test_a_personal_token_may_name_its_own_user(hub: fakes.FakeHub, make: Make) -> None:
    client = make()
    await started(hub, client)
    hub.answer("GET", "/users/@me/lists", fakes.Reply(json=[fixtures.LIST]))
    assert (await client.rest.list_lists(user=plsremind.ID(ME)))[0].name == "Inbox"
    assert hub.requests[-1].path == "/users/@me/lists"
    hub.answer("GET", f"/users/{fixtures.OTHER_USER_ID}/lists", fakes.error(404, "not_found", "No."))
    with pytest.raises(plsremind.NotFoundError):
        await client.rest.list_lists(user=plsremind.ID(fixtures.OTHER_USER_ID))


async def test_sessions_are_released_when_the_hub_turns_the_client_away(
    hub: fakes.FakeHub, make: Make, monkeypatch: pytest.MonkeyPatch
) -> None:
    opened: list[aiohttp.ClientSession] = []
    original = aiohttp.ClientSession

    def recording() -> aiohttp.ClientSession:
        opened.append(original())
        return opened[-1]

    monkeypatch.setattr(plsremind.client.aiohttp, "ClientSession", recording)
    client = make()
    connection = await started(hub, client)
    joining = asyncio.create_task(client.join())
    await connection.close(4004, "no")
    await asyncio.wait_for(joining, 2)
    assert opened
    assert all(session.closed for session in opened)


def test_star_import_brings_the_public_names_only() -> None:
    names = set(typing.cast("list[str]", plsremind.__all__))  # pyright: ignore[reportUnknownMemberType]
    assert {
        "Client",
        "RESTClient",
        "Item",
        "ItemCreateEvent",
        "Intents",
        "ID",
        "paginate",
        "to_json",
        "from_json",
    } <= names
    assert {"annotations", "client", "events", "models", "typing"} & names == set()
    assert all(hasattr(plsremind, name) for name in names)
