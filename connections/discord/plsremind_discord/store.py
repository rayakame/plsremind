from __future__ import annotations

import asyncio
import collections.abc
import datetime
import importlib.resources
import pathlib
import sqlite3
import typing

import aiosqlite
import msgspec

from plsremind_discord.db import models
from plsremind_discord.db import queries

_DELIVERIES_KEPT: typing.Final = datetime.timedelta(hours=48)
_MESSAGES_KEPT: typing.Final = datetime.timedelta(days=30)


class Sent(msgspec.Struct, frozen=True):
    message_id: int
    channel_id: int
    account_id: int


class Store:
    def __init__(self, connection: aiosqlite.Connection) -> None:
        self._db: typing.Final = connection
        self._writing: typing.Final = asyncio.Lock()

    @classmethod
    async def open(cls, path: pathlib.Path) -> Store:
        await asyncio.to_thread(path.parent.mkdir, parents=True, exist_ok=True)
        # Without the declared types the generated code hands back text instead of datetimes.
        connection = await aiosqlite.connect(path, detect_types=sqlite3.PARSE_DECLTYPES)
        try:
            await connection.execute("PRAGMA journal_mode=WAL")
            await _migrate(connection)
        except BaseException:
            await connection.close()
            raise
        return cls(connection)

    async def close(self) -> None:
        await self._db.close()

    async def knows_delivery(self, delivery_id: str) -> bool:
        return await queries.find_delivery(self._db, delivery_id=delivery_id) is not None

    async def record(
        self,
        *,
        delivery_id: str,
        user_id: str,
        item_id: str,
        shown_at: datetime.datetime | None,
        messages: collections.abc.Sequence[Sent],
        now: datetime.datetime,
    ) -> None:
        async with self._write():
            await queries.add_delivery(self._db, delivery_id=delivery_id, sent_at=_utc(now))
            for sent in messages:
                await queries.add_message(
                    self._db,
                    message_id=sent.message_id,
                    channel_id=sent.channel_id,
                    account_id=sent.account_id,
                    user_id=user_id,
                    item_id=item_id,
                    shown_at=_utc(shown_at),
                    sent_at=_utc(now),
                )

    async def knows_message(self, message_id: int) -> bool:
        return await queries.find_message(self._db, message_id=message_id) is not None

    async def messages_for_item(self, item_id: str) -> collections.abc.Sequence[models.Message]:
        return await queries.messages_for_item(self._db, item_id=item_id)

    async def messages_for_user(self, user_id: str) -> collections.abc.Sequence[models.Message]:
        return await queries.messages_for_user(self._db, user_id=user_id)

    async def stamp(self, message_id: int, shown_at: datetime.datetime | None) -> None:
        async with self._write():
            await queries.stamp_message(self._db, shown_at=_utc(shown_at), message_id=message_id)

    async def forget_message(self, message_id: int) -> None:
        async with self._write():
            await queries.forget_message(self._db, message_id=message_id)

    async def forget_user(self, user_id: str) -> None:
        async with self._write():
            await queries.forget_user(self._db, user_id=user_id)

    async def purge(self, now: datetime.datetime) -> None:
        async with self._write():
            await queries.purge_deliveries(self._db, sent_at=_utc(now - _DELIVERIES_KEPT))
            await queries.purge_messages(self._db, sent_at=_utc(now - _MESSAGES_KEPT))

    def _write(self) -> _Write:
        return _Write(self._db, self._writing)


class _Write:
    def __init__(self, connection: aiosqlite.Connection, lock: asyncio.Lock) -> None:
        self._db: typing.Final = connection
        self._lock: typing.Final = lock

    async def __aenter__(self) -> None:
        await self._lock.acquire()

    async def __aexit__(self, kind: type[BaseException] | None, *_: object) -> None:
        try:
            if kind is None:
                await self._db.commit()
            else:
                await self._db.rollback()
        finally:
            self._lock.release()


@typing.overload
def _utc(moment: datetime.datetime) -> datetime.datetime: ...
@typing.overload
def _utc(moment: None) -> None: ...
def _utc(moment: datetime.datetime | None) -> datetime.datetime | None:
    # Stored as text, so only one offset keeps rows comparable in SQL.
    return None if moment is None else moment.astimezone(datetime.UTC)


def _migrations() -> list[tuple[int, str]]:
    folder = importlib.resources.files("plsremind_discord") / "sql" / "migrations"
    files = sorted((entry for entry in folder.iterdir() if entry.name.endswith(".sql")), key=lambda entry: entry.name)
    return [(int(entry.name.split("_", 1)[0]), entry.read_text(encoding="utf-8")) for entry in files]


async def _migrate(connection: aiosqlite.Connection) -> None:
    async with connection.execute("PRAGMA user_version") as cursor:
        row = await cursor.fetchone()
    current = int(row[0]) if row is not None else 0
    for version, script in await asyncio.to_thread(_migrations):
        if version <= current:
            continue
        try:
            await connection.executescript(f"BEGIN;\n{script}")
            await connection.execute(f"PRAGMA user_version = {version}")
            await connection.commit()
        except BaseException:
            await connection.rollback()
            raise
