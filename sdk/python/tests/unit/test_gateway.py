from __future__ import annotations

import asyncio
import collections.abc
import dataclasses

import aiohttp
import msgspec
import pytest

from plsremind import errors
from plsremind.enums import ClientStatus
from plsremind.internal import gateway as gateway_module
from plsremind.internal.gateway import Gateway
from plsremind.rest import RESTClient
from tests.support import fakes

TOKEN = "plsr_pat_secret"
HEARTBEAT, IDENTIFY, RESUME, RECONNECT, INVALID_SESSION, ACK = 1, 2, 6, 7, 9, 11


@dataclasses.dataclass
class Seen:
    dispatches: list[tuple[str, bytes]] = dataclasses.field(default_factory=list[tuple[str, bytes]])
    statuses: list[ClientStatus] = dataclasses.field(default_factory=list[ClientStatus])
    disconnects: list[tuple[int | None, str]] = dataclasses.field(default_factory=list[tuple[int | None, str]])
    closed: list[errors.GatewayError] = dataclasses.field(default_factory=list[errors.GatewayError])
    sleeps: list[float] = dataclasses.field(default_factory=list[float])
    ended: asyncio.Event = dataclasses.field(default_factory=asyncio.Event)

    def names(self) -> list[str]:
        return [name for name, _ in self.dispatches]


type Make = collections.abc.Callable[..., tuple[Gateway, Seen]]


@pytest.fixture
async def make(hub: fakes.FakeHub) -> collections.abc.AsyncIterator[Make]:
    session = aiohttp.ClientSession()
    rest = RESTClient(hub.url, token=TOKEN, session=session)
    made: list[Gateway] = []

    def build(
        *,
        random: float = 1.0,
        url: str | None = None,
        after_dispatch: collections.abc.Callable[[Gateway, str], None] | None = None,
    ) -> tuple[Gateway, Seen]:
        seen = Seen()

        def dispatched(name: str, payload: msgspec.Raw) -> None:
            seen.dispatches.append((name, bytes(payload)))
            if after_dispatch is not None:
                after_dispatch(gateway, name)

        async def address() -> str:
            return url or (await rest.get_info()).gateway_url

        async def sleep(seconds: float) -> None:
            seen.sleeps.append(seconds)
            await asyncio.sleep(0)

        def closed(error: errors.GatewayError) -> None:
            seen.closed.append(error)
            seen.ended.set()

        gateway = Gateway(
            url=address,
            token=TOKEN,
            intents=["items", "deliveries"],
            session=lambda: session,
            on_dispatch=dispatched,
            on_status=seen.statuses.append,
            on_disconnected=lambda code, reason: seen.disconnects.append((code, reason)),
            on_closed=closed,
            random=lambda: random,
            sleep=sleep,
        )
        made.append(gateway)
        return gateway, seen

    try:
        yield build
    finally:
        for gateway in made:
            await gateway.stop()
        await session.close()


async def logged_in(hub: fakes.FakeHub, gateway: Gateway, interval_ms: int = 45_000) -> fakes.FakeConnection:
    gateway.start()
    connection = await hub.gateway.connection()
    await connection.hello(interval_ms)
    assert (await connection.receive())["op"] == IDENTIFY
    await connection.ready()
    return connection


until = fakes.until


async def test_identifies_after_hello(hub: fakes.FakeHub, make: Make) -> None:
    gateway, seen = make()
    gateway.start()
    connection = await hub.gateway.connection()
    assert connection.nothing_received()
    await connection.hello()
    frame = await connection.receive()
    assert frame["op"] == IDENTIFY
    assert frame["d"] == {"token": TOKEN, "intents": ["items", "deliveries"]}
    await connection.ready()
    await until(lambda: seen.names() == ["READY"])
    assert seen.statuses == [ClientStatus.CONNECTING, ClientStatus.CONNECTED]


async def test_dispatches_arrive_in_order_as_raw(hub: fakes.FakeHub, make: Make) -> None:
    gateway, seen = make()
    connection = await logged_in(hub, gateway)
    await connection.dispatch("item.created", {"user_id": "u", "data": {"id": "a"}})
    await connection.dispatch("delivery.created", {"n": 1}, sequenced=False)
    await connection.dispatch("item.deleted", {"n": 2})
    await until(lambda: len(seen.dispatches) == 4)
    assert seen.names() == ["READY", "item.created", "delivery.created", "item.deleted"]
    assert msgspec.json.decode(seen.dispatches[1][1]) == {"user_id": "u", "data": {"id": "a"}}


async def test_heartbeats_carry_the_last_sequence(hub: fakes.FakeHub, make: Make) -> None:
    gateway, _ = make(random=0.5)
    connection = await logged_in(hub, gateway, interval_ms=40)
    await connection.dispatch("item.created", {})
    await connection.dispatch("delivery.created", {}, sequenced=False)
    first = await connection.receive_op(HEARTBEAT)
    await connection.send(ACK)
    second = await connection.receive_op(HEARTBEAT)
    await connection.send(ACK)
    assert first["d"] in {1, 2}
    assert second["d"] == 2


async def test_first_heartbeat_waits_a_random_part_of_the_interval(hub: fakes.FakeHub, make: Make) -> None:
    gateway, _ = make(random=0.0)
    gateway.start()
    connection = await hub.gateway.connection()
    await connection.hello(60_000)
    frames = [await connection.receive(), await connection.receive()]
    assert sorted(frame["op"] for frame in frames) == [HEARTBEAT, IDENTIFY]


async def test_missing_ack_reconnects_and_resumes(hub: fakes.FakeHub, make: Make) -> None:
    gateway, seen = make(random=0.5)
    connection = await logged_in(hub, gateway, interval_ms=30)
    await connection.receive_op(HEARTBEAT)
    assert await connection.closed() == 4000
    again = await hub.gateway.connection()
    await again.hello()
    frame = await again.receive()
    assert frame["op"] == RESUME
    assert frame["d"] == {"token": TOKEN, "session_id": "s1", "seq": 1}
    assert seen.disconnects == [(None, "The hub stopped answering heartbeats.")]
    assert seen.statuses[-1] is ClientStatus.RECONNECTING
    await again.dispatch("RESUMED", {}, sequenced=False)
    await until(lambda: seen.statuses[-1] is ClientStatus.CONNECTED)
    assert seen.names() == ["READY", "RESUMED"]


async def test_resume_after_a_drop(hub: fakes.FakeHub, make: Make) -> None:
    gateway, seen = make()
    connection = await logged_in(hub, gateway)
    await connection.dispatch("item.created", {})
    await connection.dispatch("item.created", {})
    await until(lambda: len(seen.dispatches) == 3)
    connection.drop()
    again = await hub.gateway.connection()
    await again.hello()
    frame = await again.receive()
    assert (frame["op"], frame["d"]["seq"], frame["d"]["session_id"]) == (RESUME, 3, "s1")
    assert len(seen.disconnects) == 1
    assert seen.sleeps == [1.0]


async def test_reconnect_request_is_followed_at_once(hub: fakes.FakeHub, make: Make) -> None:
    gateway, seen = make()
    connection = await logged_in(hub, gateway)
    await connection.send(RECONNECT)
    again = await hub.gateway.connection()
    assert await connection.closed() == 4000
    await again.hello()
    assert (await again.receive())["op"] == RESUME
    assert seen.sleeps == []
    assert seen.disconnects == [(None, "The hub asked to reconnect.")]


async def test_invalid_session_identifies_again_on_the_same_socket(hub: fakes.FakeHub, make: Make) -> None:
    gateway, seen = make()
    connection = await logged_in(hub, gateway)
    connection.drop()
    again = await hub.gateway.connection()
    await again.hello()
    assert (await again.receive())["op"] == RESUME
    await again.send(INVALID_SESSION, d=False)
    frame = await again.receive()
    assert frame["op"] == IDENTIFY
    assert frame["d"] == {"token": TOKEN, "intents": ["items", "deliveries"]}
    await again.ready(session_id="s2")
    await until(lambda: seen.names() == ["READY", "READY"])
    assert hub.gateway.opened == 2
    again.drop()
    third = await hub.gateway.connection()
    await third.hello()
    assert (await third.receive())["d"]["session_id"] == "s2"


async def test_bad_sequence_close_forgets_the_session(hub: fakes.FakeHub, make: Make) -> None:
    gateway, _ = make()
    connection = await logged_in(hub, gateway)
    await connection.close(4007, "unknown sequence")
    again = await hub.gateway.connection()
    await again.hello()
    assert (await again.receive())["op"] == IDENTIFY


async def test_wait_grows_while_the_hub_is_away_and_resets(hub: fakes.FakeHub, make: Make) -> None:
    gateway, seen = make(random=0.0)
    connection = await logged_in(hub, gateway)
    for _ in range(3):
        await connection.close(1012, "restarting")
        connection = await hub.gateway.connection()
    assert seen.sleeps == [0.5, 1.0, 2.0]
    await connection.hello()
    await connection.receive_op(RESUME)
    await connection.dispatch("RESUMED", {}, sequenced=False)
    await until(lambda: seen.statuses[-1] is ClientStatus.CONNECTED)
    await connection.close(1012, "restarting")
    await hub.gateway.connection()
    assert seen.sleeps == [0.5, 1.0, 2.0, 0.5]
    assert seen.disconnects == [(1012, "restarting"), (1012, "restarting")]


async def test_wait_is_capped(hub: fakes.FakeHub, make: Make) -> None:
    gateway, seen = make(random=1.0)
    connection = await logged_in(hub, gateway)
    for _ in range(8):
        await connection.close(1012)
        connection = await hub.gateway.connection()
    assert seen.sleeps == [1.0, 2.0, 4.0, 8.0, 16.0, 32.0, 60.0, 60.0]


async def test_too_many_waits_at_least_five_seconds(hub: fakes.FakeHub, make: Make) -> None:
    gateway, seen = make(random=0.0)
    connection = await logged_in(hub, gateway)
    await connection.close(4008, "too many sessions")
    await hub.gateway.connection()
    assert seen.sleeps == [5.0]


@pytest.mark.parametrize("code", [4004, 4013, 4014])
async def test_fatal_close_ends_the_gateway(hub: fakes.FakeHub, make: Make, code: int) -> None:
    gateway, seen = make()
    connection = await logged_in(hub, gateway)
    await connection.close(code, "no")
    await asyncio.wait_for(seen.ended.wait(), 2)
    assert [(error.code, error.reason) for error in seen.closed] == [(code, "no")]
    assert seen.sleeps == []
    await asyncio.sleep(0.05)
    assert hub.gateway.none_waiting()


async def test_fatal_close_during_the_handshake(hub: fakes.FakeHub, make: Make) -> None:
    gateway, seen = make()
    gateway.start()
    connection = await hub.gateway.connection()
    await connection.hello()
    await connection.receive()
    await connection.close(4004, "Authentication failed.")
    await asyncio.wait_for(seen.ended.wait(), 2)
    assert seen.closed[0].code == 4004
    assert seen.closed[0].reason == "Authentication failed."
    assert seen.disconnects == []


async def test_failure_before_the_first_ready_ends_the_gateway(hub: fakes.FakeHub, make: Make) -> None:
    gateway, seen = make()
    gateway.start()
    connection = await hub.gateway.connection()
    connection.drop()
    await asyncio.wait_for(seen.ended.wait(), 2)
    assert seen.closed[0].code is None
    assert seen.sleeps == []


async def test_unreachable_gateway_ends_the_gateway(make: Make) -> None:
    gateway, seen = make(url="ws://127.0.0.1:9/api/v1/gateway")
    gateway.start()
    await asyncio.wait_for(seen.ended.wait(), 5)
    assert "could not be reached" in seen.closed[0].reason


async def test_unreachable_hub_ends_the_gateway(hub: fakes.FakeHub, make: Make) -> None:
    hub.answer("GET", "/info", fakes.error(503, "unavailable", "Down."))
    gateway, seen = make()
    gateway.start()
    await asyncio.wait_for(seen.ended.wait(), 2)
    assert "could not be reached" in seen.closed[0].reason


async def test_silent_handshake_counts_as_lost(hub: fakes.FakeHub, make: Make, monkeypatch: pytest.MonkeyPatch) -> None:
    gateway, seen = make()
    connection = await logged_in(hub, gateway)
    monkeypatch.setattr(gateway_module, "HANDSHAKE_SILENCE", 0.05)
    connection.drop()
    silent = await hub.gateway.connection()
    assert await silent.closed() == 4000
    again = await hub.gateway.connection()
    await again.hello()
    assert (await again.receive())["op"] == RESUME
    assert seen.sleeps == [1.0, 2.0]


async def test_garbage_frames_are_ignored(hub: fakes.FakeHub, make: Make) -> None:
    gateway, seen = make()
    connection = await logged_in(hub, gateway)
    await connection.send_text("not json")
    await connection.send_text('{"op": "zero"}')
    await connection.send_text("[1, 2]")
    await connection.send(99, {"new": "opcode"})
    await connection.send(0, {"no": "name"}, 5, None)
    await connection.dispatch("item.created", {})
    await until(lambda: seen.names() == ["READY", "item.created"])
    assert seen.disconnects == []


async def test_stop_closes_with_1000_and_leaves_no_task(hub: fakes.FakeHub, make: Make) -> None:
    before = asyncio.all_tasks()
    gateway, seen = make()
    connection = await logged_in(hub, gateway, interval_ms=20)
    await gateway.stop()
    assert await connection.closed() == 1000
    await asyncio.sleep(0.05)
    assert asyncio.all_tasks() - before == set()
    assert seen.closed == []
    assert seen.disconnects == []
    await gateway.stop()


async def test_stop_during_a_wait(hub: fakes.FakeHub) -> None:
    session = aiohttp.ClientSession()
    release = asyncio.Event()
    waiting = asyncio.Event()

    async def sleep(seconds: float) -> None:
        del seconds
        waiting.set()
        await release.wait()

    async def address() -> str:
        await asyncio.sleep(0)
        return hub.url.replace("http", "ws") + "/api/v1/gateway"

    def ignore(*_: object) -> None:
        return None

    gateway = Gateway(
        url=address,
        token=TOKEN,
        intents=["items"],
        session=lambda: session,
        on_dispatch=ignore,
        on_status=ignore,
        on_disconnected=ignore,
        on_closed=ignore,
        sleep=sleep,
    )
    try:
        connection = await logged_in(hub, gateway)
        connection.drop()
        await asyncio.wait_for(waiting.wait(), 2)
        before = asyncio.all_tasks()
        await gateway.stop()
        assert [task for task in before - asyncio.all_tasks() if task.get_name() == "plsremind-gateway"]
        release.set()
        await asyncio.sleep(0.05)
        assert hub.gateway.none_waiting()
        assert not [task for task in asyncio.all_tasks() if task.get_name() == "plsremind-gateway"]
    finally:
        await gateway.stop()
        await session.close()


async def test_start_twice_and_restart_after_stop(hub: fakes.FakeHub, make: Make) -> None:
    gateway, seen = make()
    connection = await logged_in(hub, gateway)
    gateway.start()
    await asyncio.sleep(0.05)
    assert hub.gateway.opened == 1
    await gateway.stop()
    await connection.closed()
    gateway.start()
    again = await hub.gateway.connection()
    await again.hello()
    assert (await again.receive())["op"] == IDENTIFY
    assert seen.statuses.count(ClientStatus.CONNECTING) == 2


async def test_an_unexpected_failure_ends_the_gateway_instead_of_killing_it_silently(hub: fakes.FakeHub) -> None:
    session = aiohttp.ClientSession()
    await session.close()
    closed: list[errors.GatewayError] = []
    ended = asyncio.Event()

    async def address() -> str:
        await asyncio.sleep(0)
        return hub.url.replace("http", "ws") + "/api/v1/gateway"

    def on_closed(error: errors.GatewayError) -> None:
        closed.append(error)
        ended.set()

    def ignore(*_: object) -> None:
        return None

    gateway = Gateway(
        url=address,
        token=TOKEN,
        intents=["items"],
        session=lambda: session,
        on_dispatch=ignore,
        on_status=ignore,
        on_disconnected=ignore,
        on_closed=on_closed,
    )
    gateway.start()
    try:
        await asyncio.wait_for(ended.wait(), 2)
    finally:
        await gateway.stop()
    assert isinstance(closed[0].__cause__, RuntimeError)
    assert "Session is closed" in closed[0].reason


async def test_a_hub_that_never_answers_the_upgrade_counts_as_lost(make: Make, monkeypatch: pytest.MonkeyPatch) -> None:
    held: list[asyncio.StreamWriter] = []

    async def swallow(reader: asyncio.StreamReader, writer: asyncio.StreamWriter) -> None:
        held.append(writer)
        await reader.read()

    server = await asyncio.start_server(swallow, "127.0.0.1", 0)
    port = server.sockets[0].getsockname()[1]
    monkeypatch.setattr(gateway_module, "HANDSHAKE_SILENCE", 0.1)
    gateway, seen = make(url=f"ws://127.0.0.1:{port}/api/v1/gateway")
    try:
        gateway.start()
        await asyncio.wait_for(seen.ended.wait(), 2)
    finally:
        for writer in held:
            writer.close()
        server.close()
        await server.wait_closed()
    assert seen.closed[0].reason == "The hub did not finish the handshake."


async def test_a_reconnect_request_before_the_first_ready_is_followed(hub: fakes.FakeHub, make: Make) -> None:
    gateway, seen = make()
    gateway.start()
    connection = await hub.gateway.connection()
    await connection.hello()
    await connection.receive()
    await connection.send(RECONNECT)
    again = await hub.gateway.connection()
    await again.hello()
    assert (await again.receive())["op"] == IDENTIFY
    await again.ready()
    await until(lambda: seen.names() == ["READY"])
    assert seen.closed == []
    assert seen.sleeps == [1.0]


async def test_the_session_can_be_started_over_from_a_dispatch(hub: fakes.FakeHub, make: Make) -> None:
    def restart(gateway: Gateway, name: str) -> None:
        if name == "item.created":
            gateway.restart_session()

    gateway, seen = make(after_dispatch=restart)
    connection = await logged_in(hub, gateway)
    await connection.dispatch("item.created", {})
    assert await connection.closed() == 4000
    again = await hub.gateway.connection()
    await again.hello()
    assert (await again.receive())["op"] == IDENTIFY
    assert seen.sleeps == []
    assert seen.disconnects == [(None, "The session is started over.")]


async def test_a_dispatch_can_end_the_gateway(hub: fakes.FakeHub, make: Make) -> None:
    def refuse(gateway: Gateway, name: str) -> None:
        del name
        gateway.abort(errors.GatewayError("The hub's READY could not be read."))

    gateway, seen = make(after_dispatch=refuse)
    gateway.start()
    connection = await hub.gateway.connection()
    await connection.hello()
    await connection.receive()
    await connection.ready()
    await asyncio.wait_for(seen.ended.wait(), 2)
    assert seen.closed[0].reason == "The hub's READY could not be read."
    assert await connection.closed() == 1000
    assert ClientStatus.CONNECTED not in seen.statuses


async def test_a_hub_that_only_ever_asks_to_reconnect_is_given_up_on(hub: fakes.FakeHub, make: Make) -> None:
    gateway, seen = make()
    gateway.start()
    for _ in range(5):
        connection = await hub.gateway.connection()
        await connection.hello()
        await connection.receive()
        await connection.send(RECONNECT)
    await asyncio.wait_for(seen.ended.wait(), 2)
    assert seen.closed[0].reason == "The hub kept asking to reconnect before it let the client in."
    assert seen.sleeps == [1.0, 2.0, 4.0, 8.0]
