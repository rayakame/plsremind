"""A stand-in hub on a local port: real HTTP and WebSocket, scripted answers."""

from __future__ import annotations

import asyncio
import collections.abc
import dataclasses
import json
import typing

import aiohttp.web
import multidict

PREFIX = "/api/v1"


@dataclasses.dataclass(frozen=True)
class Recorded:
    method: str
    path: str
    raw_path: str
    query: dict[str, str]
    headers: multidict.CIMultiDictProxy[str]
    body: bytes

    @property
    def json(self) -> typing.Any:  # ruff: ignore[any-type]
        return json.loads(self.body)


@dataclasses.dataclass(frozen=True)
class Reply:
    status: int = 200
    json: typing.Any = None
    body: bytes | None = None
    headers: dict[str, str] = dataclasses.field(default_factory=dict[str, str])
    delay: float = 0.0


def error(status: int, code: str, message: str = "no", fields: dict[str, str] | None = None) -> Reply:
    body: dict[str, typing.Any] = {"code": code, "message": message}
    if fields is not None:
        body["fields"] = fields
    return Reply(status, {"error": body})


def limited(retry_after: str | None) -> Reply:
    headers = {} if retry_after is None else {"Retry-After": retry_after}
    return Reply(429, {"error": {"code": "rate_limited", "message": "Slow down."}}, headers=headers)


class FakeConnection:
    """The hub's end of one gateway connection."""

    def __init__(self, socket: aiohttp.web.WebSocketResponse, request: aiohttp.web.Request) -> None:
        self._socket = socket
        self._request = request
        self._frames: asyncio.Queue[dict[str, typing.Any]] = asyncio.Queue()
        self._closed: asyncio.Future[int | None] = asyncio.get_running_loop().create_future()
        self.sequence = 0

    async def pump(self) -> None:
        try:
            async for message in self._socket:
                if message.type is aiohttp.WSMsgType.TEXT:
                    self._frames.put_nowait(json.loads(message.data))
        finally:
            if not self._closed.done():
                self._closed.set_result(self._socket.close_code)

    async def receive(self, timeout: float = 2.0) -> dict[str, typing.Any]:  # ruff: ignore[async-function-with-timeout]
        """The next frame the client sent."""
        return await asyncio.wait_for(self._frames.get(), timeout)

    async def receive_op(self, op: int, timeout: float = 2.0) -> dict[str, typing.Any]:  # ruff: ignore[async-function-with-timeout]
        """The next frame with this opcode, skipping others such as heartbeats."""
        async with asyncio.timeout(timeout):
            while True:
                frame = await self._frames.get()
                if frame["op"] == op:
                    return frame

    def nothing_received(self) -> bool:
        return self._frames.empty()

    async def send(self, op: int, d: object = None, s: int | None = None, t: str | None = None) -> None:
        await self._socket.send_str(json.dumps({"op": op, "d": d, "s": s, "t": t}))

    async def send_text(self, text: str) -> None:
        await self._socket.send_str(text)

    async def hello(self, interval_ms: int = 45_000) -> None:
        await self.send(10, {"heartbeat_interval": interval_ms})

    async def dispatch(self, name: str, d: object, *, sequenced: bool = True) -> None:
        if sequenced:
            self.sequence += 1
        await self.send(0, d, self.sequence if sequenced else None, name)

    async def ready(
        self,
        *,
        session_id: str = "s1",
        users: collections.abc.Sequence[str] = (),
        principal: dict[str, typing.Any] | None = None,
        intents: collections.abc.Sequence[str] = ("items",),
    ) -> None:
        self.sequence = 0
        payload = {
            "session_id": session_id,
            "intents": list(intents),
            "principal": principal or {"kind": "personal_token", "id": "token-1", "user_id": "user-1"},
            "users": list(users),
        }
        await self.dispatch("READY", payload)

    async def close(self, code: int, reason: str = "") -> None:
        await self._socket.close(code=code, message=reason.encode())

    def drop(self) -> None:
        """Cut the connection the way a network would, without a close frame."""
        transport = self._request.transport
        assert transport is not None
        transport.abort()

    async def closed(self, timeout: float = 2.0) -> int | None:  # ruff: ignore[async-function-with-timeout]
        """Wait until the client is gone and return the close code it sent."""
        return await asyncio.wait_for(asyncio.shield(self._closed), timeout)


class FakeGateway:
    def __init__(self) -> None:
        self._connections: asyncio.Queue[FakeConnection] = asyncio.Queue()
        self.opened = 0

    async def connection(self, timeout: float = 2.0) -> FakeConnection:  # ruff: ignore[async-function-with-timeout]
        """The next connection a client opens."""
        return await asyncio.wait_for(self._connections.get(), timeout)

    def none_waiting(self) -> bool:
        return self._connections.empty()

    async def handle(self, request: aiohttp.web.Request) -> aiohttp.web.StreamResponse:
        socket = aiohttp.web.WebSocketResponse()
        await socket.prepare(request)
        connection = FakeConnection(socket, request)
        self.opened += 1
        self._connections.put_nowait(connection)
        await connection.pump()
        return socket


class FakeHub:
    def __init__(self) -> None:
        self.requests: list[Recorded] = []
        self.url = ""
        self.gateway = FakeGateway()
        self._answers: dict[tuple[str, str], list[Reply]] = {}
        self._app = aiohttp.web.Application()
        self._app.router.add_get(f"{PREFIX}/gateway", self.gateway.handle)
        self._app.router.add_route("*", "/{tail:.*}", self._handle)
        self._runner = aiohttp.web.AppRunner(self._app, shutdown_timeout=0.1)

    async def start(self) -> None:
        await self._runner.setup()
        site = aiohttp.web.TCPSite(self._runner, "127.0.0.1", 0)
        await site.start()
        host, port = typing.cast("tuple[str, int]", self._runner.addresses[0])
        self.url = f"http://{host}:{port}"
        info: dict[str, typing.Any] = {
            "name": "fake",
            "version": "0.0.0",
            "gateway_url": f"ws://{host}:{port}{PREFIX}/gateway",
            "setup_required": False,
            "registration": "open",
            "login_methods": [],
        }
        self.answer("GET", "/info", Reply(json=info))

    async def stop(self) -> None:
        await self._runner.cleanup()

    def answer(self, method: str, path: str, *replies: Reply) -> None:
        """Queue answers for a call. The last one repeats."""
        self._answers[method, path] = list(replies)

    async def _handle(self, request: aiohttp.web.Request) -> aiohttp.web.StreamResponse:
        path = request.path.removeprefix(PREFIX)
        self.requests.append(
            Recorded(request.method, path, request.raw_path, dict(request.query), request.headers, await request.read())
        )
        queue = self._answers.get((request.method, path))
        if not queue:
            reply = error(404, "not_found", f"nothing answers {request.method} {path}")
        elif len(queue) > 1:
            reply = queue.pop(0)
        else:
            reply = queue[0]
        if reply.delay:
            await asyncio.sleep(reply.delay)
        if reply.body is not None:
            return aiohttp.web.Response(status=reply.status, body=reply.body, headers=reply.headers)
        if reply.json is None:
            return aiohttp.web.Response(status=reply.status, headers=reply.headers)
        return aiohttp.web.json_response(reply.json, status=reply.status, headers=reply.headers)


async def until(condition: collections.abc.Callable[[], object], timeout: float = 2.0) -> None:  # ruff: ignore[async-function-with-timeout]
    """Wait for something another task brings about."""
    async with asyncio.timeout(timeout):
        while not condition():  # ruff: ignore[async-busy-wait]
            await asyncio.sleep(0.005)
