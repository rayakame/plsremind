"""A stand-in hub on a local port: real HTTP and WebSocket, scripted answers."""

from __future__ import annotations

import asyncio
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


class FakeHub:
    def __init__(self) -> None:
        self.requests: list[Recorded] = []
        self.url = ""
        self._answers: dict[tuple[str, str], list[Reply]] = {}
        self._app = aiohttp.web.Application()
        self._app.router.add_route("*", "/{tail:.*}", self._handle)
        self._runner = aiohttp.web.AppRunner(self._app, shutdown_timeout=0.1)

    async def start(self) -> None:
        await self._runner.setup()
        site = aiohttp.web.TCPSite(self._runner, "127.0.0.1", 0)
        await site.start()
        host, port = typing.cast("tuple[str, int]", self._runner.addresses[0])
        self.url = f"http://{host}:{port}"

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
