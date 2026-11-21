from __future__ import annotations

import asyncio
import collections.abc
import contextlib
import dataclasses
import secrets
import typing

import aiohttp
import asyncpg
import plsremind
import risa

from plsremind_discord.clock import Clock
from plsremind_discord.hub import Hub
from plsremind_discord.views import common
from plsremind_discord.views.reminder import ReminderView
from tests.support import discord

PROXY_HEADER: typing.Final = "X-Forwarded-For"
_addresses = 0


class Launched(typing.Protocol):
    url: str
    database_url: str

    def stop(self) -> None: ...


def _address() -> str:
    # The hub counts anonymous requests per address, so every caller in a test gets its own.
    global _addresses  # ruff: ignore[global-statement]
    _addresses += 1
    return f"10.{secrets.randbelow(250)}.{secrets.randbelow(250)}.{_addresses % 250 + 1}"


class Browser:
    """A signed-in browser: what a user does in the dashboard."""

    def __init__(self, hub_url: str) -> None:
        self._base = f"{hub_url}/api/v1"
        self._session = aiohttp.ClientSession(
            cookie_jar=aiohttp.CookieJar(unsafe=True), headers={PROXY_HEADER: _address(), "Origin": hub_url}
        )

    async def call(self, method: str, path: str, body: object = None, *, expect: int = 200) -> typing.Any:  # ruff: ignore[any-type]
        async with self._session.request(method, self._base + path, json=body) as response:
            text = await response.text()
            if response.status != expect:
                msg = f"{method} {path} answered {response.status}: {text}"
                raise AssertionError(msg)
            return await response.json() if text else None

    async def close(self) -> None:
        await self._session.close()


@dataclasses.dataclass
class Person:
    id: plsremind.ID
    browser: Browser
    rest: plsremind.RESTClient
    token: str
    discord_id: int


class Kit:
    def __init__(self, hub: Launched, admin_password: str) -> None:
        self.hub = hub
        self.url = hub.url
        self._admin_password = admin_password
        self._admin: Browser | None = None
        self._closers: list[collections.abc.Callable[[], collections.abc.Awaitable[object]]] = []

    async def close(self) -> None:
        for close in reversed(self._closers):
            await close()

    async def admin(self) -> Browser:
        if self._admin is None:
            self._admin = self._browser()
            await self._admin.call("POST", "/auth/login", {"username": "admin", "password": self._admin_password})
            await self._admin.call("PATCH", "/admin/settings", {"registration": "open"})
        return self._admin

    async def person(self, name: str, *, remind: bool = False) -> Person:
        """A user with a personal token whose account is linked to a Discord account."""
        await self.admin()
        browser = self._browser()
        body = {
            "username": f"{name}-{secrets.token_hex(4)}",
            "password": secrets.token_urlsafe(18),
            "display_name": name,
            "timezone": "Europe/Berlin",
        }
        user = await browser.call("POST", "/auth/register", body, expect=201)
        token = await browser.call(
            "POST", "/users/@me/tokens", {"name": f"own-{secrets.token_hex(4)}", "remind": remind}, expect=201
        )
        discord_id = secrets.randbelow(10**17) + 10**17
        await self._sql(
            """INSERT INTO identities (id, user_id, provider, external_id, display_name, created_at)
               VALUES (gen_random_uuid(), $1, 'discord', $2, $2, now())""",
            user["id"],
            str(discord_id),
        )
        session = aiohttp.ClientSession(headers={PROXY_HEADER: _address()})
        self._closers.append(session.close)
        rest = plsremind.RESTClient(self.url, token=token["token"], session=session)
        return Person(plsremind.ID(user["id"]), browser, rest, token["token"], discord_id)

    async def connection(self) -> tuple[plsremind.ID, str]:
        body = {"name": f"bot-{secrets.token_hex(4)}", "provider": "discord"}
        created = await (await self.admin()).call("POST", "/admin/connections", body, expect=201)
        return plsremind.ID(created["id"]), created["token"]

    def _browser(self) -> Browser:
        browser = Browser(self.url)
        self._closers.append(browser.close)
        return browser

    async def _sql(self, text: str, *values: object) -> None:
        connection = await asyncpg.connect(self.hub.database_url)
        try:
            await connection.execute(text, *values)
        finally:
            await connection.close()


class Redraws:
    """Stands in for the risa client: records which messages were redrawn and loads what they show."""

    def __init__(self, hub: Hub) -> None:
        self.hub = hub
        self.messages: list[int] = []
        self.shows: dict[int, tuple[plsremind.ID, plsremind.ID]] = {}

    @contextlib.asynccontextmanager
    async def open[T: risa.View](
        self, channel: int, message: int, cls: type[T], /
    ) -> collections.abc.AsyncGenerator[T]:
        del channel
        assert cls is ReminderView
        self.messages.append(message)
        user, item = self.shows[message]
        view = ReminderView(user=common.pack(user), item=common.pack(item))
        view.shown = await common.shown(
            self.hub, discord.CONFIG, Clock(), discord.say(), user=view.user, item=view.item
        )
        yield typing.cast("T", view)


async def eventually(
    check: collections.abc.Callable[[], collections.abc.Awaitable[bool]], within: float = 30.0
) -> None:
    async with asyncio.timeout(within):
        while not await check():  # ruff: ignore[async-busy-wait]
            await asyncio.sleep(0.2)
