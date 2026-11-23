from __future__ import annotations

import asyncio
import collections.abc
import datetime
import importlib.resources
import pathlib
import sqlite3
import zoneinfo

import aiosqlite
import pytest

from plsremind_discord import store as store_module
from plsremind_discord.store import Sent
from plsremind_discord.store import Store

NOW = datetime.datetime(2026, 11, 19, 13, 30, 0, 250000, tzinfo=datetime.UTC)
ITEM = "01930000-0000-7000-8000-00000000000a"
OTHER_ITEM = "01930000-0000-7000-8000-00000000000b"
MIA = "01930000-0000-7000-8000-000000000001"
BEN = "01930000-0000-7000-8000-000000000002"
BOT, OTHER_BOT = 900, 901


@pytest.fixture
async def store(tmp_path: pathlib.Path) -> collections.abc.AsyncIterator[Store]:
    opened = await Store.open(tmp_path / "discord.db")
    try:
        yield opened
    finally:
        await opened.close()


async def record(
    store: Store,
    delivery: str,
    *messages: int,
    user: str = MIA,
    item: str = ITEM,
    now: datetime.datetime = NOW,
    stamp: datetime.datetime | None = None,
    account: int = 1234,
    channel: int = 7,
    bot: int = 900,
) -> None:
    await store.record(
        delivery_id=delivery,
        user_id=user,
        item_id=item,
        shown_at=stamp,
        messages=[Sent(message_id=message, channel_id=channel, account_id=account) for message in messages],
        bot_id=bot,
        now=now,
    )


async def version(path: pathlib.Path) -> int:
    async with aiosqlite.connect(path) as connection, connection.execute("PRAGMA user_version") as cursor:
        row = await cursor.fetchone()
    assert row is not None
    return int(row[0])


async def test_a_recorded_delivery_is_known(store: Store) -> None:
    assert not await store.knows_delivery("d1")
    await record(store, "d1", 100)
    assert await store.knows_delivery("d1")
    assert not await store.knows_delivery("d2")


async def test_opening_again_keeps_the_rows_and_the_schema(tmp_path: pathlib.Path) -> None:
    path = tmp_path / "discord.db"
    first = await Store.open(path)
    await record(first, "d1", 100)
    await first.close()
    assert await version(path) == 2

    second = await Store.open(path)
    try:
        assert await second.knows_delivery("d1")
        assert [row.message_id for row in await second.messages_for_item(ITEM)] == [100]
    finally:
        await second.close()
    assert await version(path) == 2


async def test_a_folder_that_does_not_exist_yet_is_made(tmp_path: pathlib.Path) -> None:
    opened = await Store.open(tmp_path / "deep" / "er" / "discord.db")
    await opened.close()
    assert (tmp_path / "deep" / "er" / "discord.db").is_file()


async def test_a_record_that_fails_leaves_nothing_behind(store: Store) -> None:
    with pytest.raises(sqlite3.IntegrityError):
        await record(store, "d1", 100, 100)
    assert not await store.knows_delivery("d1")
    assert await store.messages_for_item(ITEM) == []
    await record(store, "d1", 100)
    assert await store.knows_delivery("d1")


async def test_recording_a_delivery_twice_is_no_error(store: Store) -> None:
    await record(store, "d1", 100)
    await record(store, "d1")
    assert [row.message_id for row in await store.messages_for_item(ITEM)] == [100]


async def test_times_come_back_as_they_went_in(store: Store) -> None:
    berlin = zoneinfo.ZoneInfo("Europe/Berlin")
    stamp = datetime.datetime(2026, 11, 19, 14, 0, 0, 123456, tzinfo=berlin)
    await record(store, "d1", 100, stamp=stamp)
    (row,) = await store.messages_for_item(ITEM)
    assert row.sent_at == NOW
    assert row.sent_at.utcoffset() == datetime.timedelta(0)
    assert row.shown_at == stamp
    assert row.channel_id == 7
    assert (row.user_id, row.item_id) == (MIA, ITEM)


async def test_messages_are_found_by_item_and_by_user(store: Store) -> None:
    await record(store, "d1", 100, 101)
    await record(store, "d2", 200, item=OTHER_ITEM)
    await record(store, "d3", 300, user=BEN, item=OTHER_ITEM)
    assert [row.message_id for row in await store.messages_for_item(ITEM)] == [100, 101]
    assert [row.message_id for row in await store.messages_for_item(OTHER_ITEM)] == [200, 300]
    assert [row.message_id for row in await store.messages_for_user(MIA)] == [100, 101, 200]
    assert [row.message_id for row in await store.messages_for_user(BEN)] == [300]


async def test_a_stamp_can_be_set_and_cleared(store: Store) -> None:
    await record(store, "d1", 100, 101)
    later = NOW + datetime.timedelta(minutes=5)
    await store.stamp(100, later)
    first, second = await store.messages_for_item(ITEM)
    assert (first.shown_at, second.shown_at) == (later, None)
    await store.stamp(100, None)
    assert (await store.messages_for_item(ITEM))[0].shown_at is None


async def test_a_message_can_be_forgotten(store: Store) -> None:
    await record(store, "d1", 100, 101)
    await store.forget_message(100)
    await store.forget_message(999)
    assert [row.message_id for row in await store.messages_for_item(ITEM)] == [101]


async def test_forgetting_a_user_leaves_the_others(store: Store) -> None:
    await record(store, "d1", 100)
    await record(store, "d2", 300, user=BEN)
    await store.forget_user(MIA)
    assert await store.messages_for_user(MIA) == []
    assert [row.message_id for row in await store.messages_for_user(BEN)] == [300]
    assert await store.knows_delivery("d1"), "what was sent stays known"


async def test_old_rows_are_purged(store: Store) -> None:
    hours, days = datetime.timedelta(hours=1), datetime.timedelta(days=1)
    await record(store, "kept", 1, now=NOW - 47 * hours - 59 * datetime.timedelta(minutes=1))
    await record(store, "gone", 2, now=NOW - 48 * hours - datetime.timedelta(minutes=1))
    await record(store, "month", 3, now=NOW - 29 * days)
    await record(store, "older", 4, now=NOW - 31 * days)

    await store.purge(NOW)

    assert [await store.knows_delivery(name) for name in ("kept", "gone", "month", "older")] == [
        True,
        False,
        False,
        False,
    ]
    assert [row.message_id for row in await store.messages_for_item(ITEM)] == [1, 2, 3]


async def test_a_schema_step_that_fails_changes_nothing(
    tmp_path: pathlib.Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    path = tmp_path / "discord.db"
    steps = [
        (1, "CREATE TABLE first (x integer);"),
        (2, "CREATE TABLE second (y integer);\nCREATE TABLE first (x integer);"),
    ]
    monkeypatch.setattr(store_module, "_migrations", lambda: steps)

    with pytest.raises(sqlite3.OperationalError, match="already exists"):
        await Store.open(path)

    assert await version(path) == 1
    async with aiosqlite.connect(path) as connection, connection.execute("SELECT name FROM sqlite_master") as cursor:
        assert [row[0] for row in await cursor.fetchall()] == ["first"]


async def test_age_counts_the_same_whatever_zone_a_time_came_in(store: Store) -> None:
    berlin = zoneinfo.ZoneInfo("Europe/Berlin")
    await record(store, "d1", 100, now=NOW.astimezone(berlin))
    await store.purge(NOW + datetime.timedelta(days=30, minutes=15))
    assert await store.messages_for_item(ITEM) == []
    assert not await store.knows_delivery("d1")


async def test_the_dm_channel_of_a_recorded_message_is_kept_per_bot_and_account(store: Store) -> None:
    assert await store.dm_channel(BOT, 1234) is None
    await record(store, "d1", 100, account=1234, channel=70)
    await record(store, "d2", 200, account=5678, channel=80)
    await record(store, "d3", 300, account=1234, channel=90, bot=OTHER_BOT)
    await record(store, "d4", 400, account=1234, channel=71)
    found = [await store.dm_channel(bot, account) for bot, account in ((BOT, 1234), (BOT, 5678), (OTHER_BOT, 1234))]
    assert found == [71, 80, 90]


async def test_a_dm_channel_is_only_forgotten_while_it_is_the_one_named(store: Store) -> None:
    await record(store, "d1", 100, channel=71)
    await store.forget_dm_channel(BOT, 1234, 70)
    assert await store.dm_channel(BOT, 1234) == 71
    await store.forget_dm_channel(BOT, 1234, 71)
    assert await store.dm_channel(BOT, 1234) is None


async def test_dm_channels_go_once_their_account_has_no_messages_left(store: Store) -> None:
    await record(store, "d1", 100, account=1234, channel=70)
    await record(store, "d2", 200, account=5678, channel=80, now=NOW - datetime.timedelta(days=31))
    await store.purge(NOW)
    assert [await store.dm_channel(BOT, account) for account in (1234, 5678)] == [70, None]
    await store.forget_message(100)
    await store.purge(NOW)
    assert await store.dm_channel(BOT, 1234) is None


async def test_forgetting_a_user_takes_the_dm_channels_of_their_accounts(store: Store) -> None:
    await record(store, "d1", 100, account=1234, channel=70)
    await record(store, "d2", 300, user=BEN, account=5678, channel=80)
    await record(store, "d3", 400, account=4321, channel=60)
    await store.forget_message(400)
    await store.forget_user(MIA)
    assert [await store.dm_channel(BOT, account) for account in (1234, 5678, 4321)] == [None, 80, None]


async def test_forgetting_an_account_takes_the_users_messages_and_its_dm_channels(store: Store) -> None:
    await record(store, "d1", 100, 101, account=1234, channel=70)
    await record(store, "d2", 200, account=5678, channel=80)
    await record(store, "d3", 300, user=BEN, account=1234, channel=90, bot=OTHER_BOT)
    await record(store, "d4", 400, account=4321, channel=60)
    await store.forget_message(400)
    await store.forget_account(MIA, 1234)
    assert await store.dm_channel(BOT, 4321) is None, "a channel with no message left goes with it"
    assert [row.message_id for row in await store.messages_for_user(MIA)] == [200]
    assert [row.message_id for row in await store.messages_for_user(BEN)] == [300], "the account may be Ben's now"
    found = [await store.dm_channel(bot, account) for bot, account in ((BOT, 1234), (OTHER_BOT, 1234), (BOT, 5678))]
    assert found == [None, None, 80]


@pytest.mark.parametrize("trouble", [sqlite3.OperationalError, asyncio.CancelledError])
async def test_a_write_whose_commit_fails_is_not_carried_into_the_next_one(
    tmp_path: pathlib.Path, monkeypatch: pytest.MonkeyPatch, trouble: type[BaseException]
) -> None:
    path = tmp_path / "discord.db"
    store = await Store.open(path)
    real = aiosqlite.Connection.commit
    commits = 0

    async def commit(connection: aiosqlite.Connection) -> None:
        nonlocal commits
        commits += 1
        if commits == 1:
            raise trouble
        await real(connection)

    monkeypatch.setattr(aiosqlite.Connection, "commit", commit)
    with pytest.raises(trouble):
        await record(store, "lost", 100)
    await record(store, "kept", 200)
    await store.close()
    reopened = await Store.open(path)
    try:
        assert not await reopened.knows_delivery("lost")
        assert await reopened.knows_delivery("kept")
    finally:
        await reopened.close()


async def test_a_store_from_before_dm_channels_gains_them_and_keeps_its_rows(tmp_path: pathlib.Path) -> None:
    path = tmp_path / "discord.db"
    first_step = (importlib.resources.files("plsremind_discord") / "sql" / "migrations" / "0001_init.sql").read_text(
        encoding="utf-8"
    )
    async with aiosqlite.connect(path) as old:
        await old.executescript(first_step)
        await old.execute("INSERT INTO deliveries (delivery_id, sent_at) VALUES ('d1', '2026-11-19 13:30:00+00:00')")
        await old.execute("PRAGMA user_version = 1")
        await old.commit()

    store = await Store.open(path)
    try:
        assert await store.knows_delivery("d1")
        await record(store, "d2", 200, channel=70)
        assert await store.dm_channel(BOT, 1234) == 70
    finally:
        await store.close()
    assert await version(path) == 2
