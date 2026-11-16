from __future__ import annotations

import dataclasses
import secrets
import typing

import aiohttp
import asyncpg

import plsremind
from tests import fakes
from tests.hub import launch

if typing.TYPE_CHECKING:
    import collections.abc

EVERYTHING: typing.Final = plsremind.Intents.ALL
until = fakes.until

_addresses = 0


def unique(name: str) -> str:
    return f"{name}-{secrets.token_hex(4)}"


def _address() -> str:
    # The hub counts anonymous requests per address, so every caller in a test gets its own.
    global _addresses  # ruff: ignore[global-statement]
    _addresses += 1
    return f"10.{secrets.randbelow(250)}.{secrets.randbelow(250)}.{_addresses % 250 + 1}"


class Browser:
    # A signed-in browser: what the SDK leaves to the dashboard.

    def __init__(self, hub_url: str) -> None:
        self._base = f"{hub_url}/api/v1"
        self._session = aiohttp.ClientSession(
            cookie_jar=aiohttp.CookieJar(unsafe=True),
            headers={launch.PROXY_HEADER: _address(), "Origin": hub_url},
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


class Tap:
    # A session that remembers its sockets, so a test can cut one the way a network would.

    def __init__(self, session: aiohttp.ClientSession) -> None:
        self._session = session
        self.sockets: list[aiohttp.ClientWebSocketResponse] = []

    def __getattr__(self, name: str) -> typing.Any:  # ruff: ignore[any-type]
        return getattr(self._session, name)

    async def ws_connect(self, *arguments: typing.Any, **options: typing.Any) -> aiohttp.ClientWebSocketResponse:  # ruff: ignore[any-type]
        socket = await self._session.ws_connect(*arguments, **options)
        self.sockets.append(socket)
        return socket

    def drop_current(self) -> None:
        connection = self.sockets[-1]._response.connection  # pyright: ignore[reportPrivateUsage]
        assert connection is not None
        assert connection.transport is not None
        connection.transport.abort()


@dataclasses.dataclass
class Account:
    id: plsremind.ID
    browser: Browser
    rest: plsremind.RESTClient
    token: str
    token_id: str
    external_id: str = ""


@dataclasses.dataclass
class Tapped:
    client: plsremind.Client
    tap: Tap


class Kit:
    def __init__(self, hub: launch.LaunchedHub, admin_password: str) -> None:
        self.hub = hub
        self._admin_password = admin_password
        self._admin: Browser | None = None
        self._closers: list[collections.abc.Callable[[], collections.abc.Awaitable[object]]] = []

    async def close(self) -> None:
        for close in reversed(self._closers):
            await close()

    def _browser(self) -> Browser:
        browser = Browser(self.hub.url)
        self._closers.append(browser.close)
        return browser

    def _session(self) -> aiohttp.ClientSession:
        session = aiohttp.ClientSession(headers={launch.PROXY_HEADER: _address()})
        self._closers.append(session.close)
        return session

    async def admin(self) -> Browser:
        if self._admin is None:
            self._admin = self._browser()
            await self._admin.call("POST", "/auth/login", {"username": "admin", "password": self._admin_password})
            await self._admin.call("PATCH", "/admin/settings", {"registration": "open"})
        return self._admin

    async def account(self, name: str, *, remind: bool = False) -> Account:
        await self.admin()
        browser = self._browser()
        user = await browser.call(
            "POST",
            "/auth/register",
            {
                "username": unique(name),
                "password": secrets.token_urlsafe(18),
                "display_name": name,
                "timezone": "Europe/Berlin",
            },
            expect=201,
        )
        token_id, token = await self.personal_token(browser, remind=remind)
        return Account(plsremind.ID(user["id"]), browser, self.rest(token), token, token_id)

    async def personal_token(self, browser: Browser, *, remind: bool = False) -> tuple[str, str]:
        body = {"name": unique("adapter"), "remind": remind}
        created = await browser.call("POST", "/users/@me/tokens", body, expect=201)
        return created["id"], created["token"]

    async def linked(self, provider: str, name: str) -> Account:
        account = await self.account(name)
        account.external_id = unique(name)
        await self._sql(
            """INSERT INTO identities (id, user_id, provider, external_id, display_name, created_at)
               VALUES (gen_random_uuid(), $1, $2, $3, $3, now())""",
            account.id,
            provider,
            account.external_id,
        )
        return account

    async def connection(
        self, provider: str, allowed_intents: collections.abc.Sequence[str] | None = None
    ) -> tuple[plsremind.ID, str]:
        body: dict[str, object] = {"name": unique("bot"), "provider": provider}
        if allowed_intents is not None:
            body["allowed_intents"] = list(allowed_intents)
        created = await (await self.admin()).call("POST", "/admin/connections", body, expect=201)
        return plsremind.ID(created["id"]), created["token"]

    async def complete_in_the_past(self, user: plsremind.ID, count: int) -> None:
        await self._sql(
            """INSERT INTO items (id, user_id, list_id, title, status, completed_at, created_at, updated_at)
               SELECT gen_random_uuid(), $1, (SELECT id FROM lists WHERE user_id = $1 AND is_inbox),
                      'old ' || n, 'done', now() - make_interval(secs => n), now(), now()
               FROM generate_series(1, $2::int) AS n""",
            user,
            count,
        )

    async def _sql(self, text: str, *values: object) -> None:
        connection = await asyncpg.connect(self.hub.database_url)
        try:
            await connection.execute(text, *values)
        finally:
            await connection.close()

    def rest(self, token: str) -> plsremind.RESTClient:
        return plsremind.RESTClient(self.hub.url, token=token, session=self._session())

    def client(self, token: str, intents: plsremind.Intents = EVERYTHING) -> Tapped:
        tap = Tap(self._session())
        session = typing.cast("aiohttp.ClientSession", tap)
        client = plsremind.Client(self.hub.url, token=token, intents=intents, session=session)
        self._closers.append(client.close)
        return Tapped(client, tap)
