from __future__ import annotations

import collections.abc
import datetime
import pathlib
import typing
from unittest import mock

import hikari
import plsremind
import pytest

from plsremind_discord import bot as bot_module
from plsremind_discord import hub as hub_
from plsremind_discord.clock import Clock
from plsremind_discord.config import Config
from plsremind_discord.reminders import Reminders
from plsremind_discord.store import Sent
from plsremind_discord.store import Store
from plsremind_discord.sync import Sync
from plsremind_discord.timeparse import Day
from plsremind_discord.timeparse import Moment
from plsremind_discord.views.common import Notice
from plsremind_discord.views.reminder import ReminderView
from tests.hub import kit as kit_
from tests.support import discord

pytestmark = pytest.mark.hub

CHANNEL = 7000


class Running(typing.NamedTuple):
    hub: hub_.Hub
    mia: kit_.Person
    connection: plsremind.ID
    rest: mock.Mock
    store: Store
    redraws: kit_.Redraws
    sync: Sync


def now() -> datetime.datetime:
    return datetime.datetime.now(datetime.UTC)


def config(kit: kit_.Kit, token: str, tmp_path: pathlib.Path) -> Config:
    return Config(discord_token="x", hub_url=kit.url, hub_token=token, public_url=kit.url, database=tmp_path / "bot.db")


@pytest.fixture
async def running(kit: kit_.Kit, tmp_path: pathlib.Path) -> collections.abc.AsyncIterator[Running]:
    mia = await kit.person("mia")
    connection, token = await kit.connection()
    hub = hub_.connect(config(kit, token, tmp_path))
    store = await Store.open(tmp_path / "bot.db")
    rest = mock.Mock(spec=hikari.api.RESTClient)
    numbers = iter(range(100, 1000))

    def channel(_: int) -> mock.Mock:
        return mock.Mock(id=hikari.Snowflake(CHANNEL))

    def message(*_: object, **__: object) -> mock.Mock:
        return mock.Mock(id=hikari.Snowflake(next(numbers)))

    rest.create_dm_channel.side_effect = channel
    rest.create_message.side_effect = message
    clock = Clock()
    views = discord.views(hub, ReminderView, Notice, clock=clock)
    redraws = kit_.Redraws()

    async def at_once(_: float) -> None:
        return

    sync = Sync(hub=hub, views=redraws, store=store, sleep=at_once)
    reminders = Reminders(
        hub=hub,
        views=views,
        rest=typing.cast("hikari.api.RESTClient", rest),
        store=store,
        texts=discord.say(),
        clock=clock,
        redraw=sync.redraw_soon,
    )
    gate = bot_module.Gate()
    bot_module.listen(hub.client, reminders=reminders, sync=sync, gate=gate)
    await hub.start()
    gate.open()
    try:
        yield Running(hub, mia, connection, rest, store, redraws, sync)
    finally:
        await hub.close()
        await reminders.close()
        await sync.close()
        await store.close()


async def reminded_soon(mia: kit_.Person, title: str = "Laundry") -> plsremind.Item:
    item = await mia.rest.create_item(title=title)
    await mia.rest.create_reminder(item.id, at=now() + datetime.timedelta(seconds=2))
    return item


async def outcome(mia: kit_.Person, item: plsremind.Item) -> plsremind.ReminderLogDelivery | None:
    page = await mia.rest.list_reminder_log(item_id=item.id)
    for entry in page.data:
        for delivery in entry.deliveries:
            if delivery.status is not plsremind.DeliveryStatus.PENDING:
                return delivery
    return None


async def test_the_bot_knows_who_is_who_after_its_start(running: Running) -> None:
    assert running.hub.ready.is_set()
    assert running.hub.user_of(running.mia.discord_id) == running.mia.id
    profile = running.hub.profile(running.mia.id)
    assert profile is not None
    assert profile.external_ids == (str(running.mia.discord_id),)
    assert [one.is_inbox for one in running.hub.lists(running.mia.id)] == [True]


async def test_a_reminder_becomes_a_dm_and_is_confirmed(running: Running) -> None:
    mia = running.mia
    item = await reminded_soon(mia)
    await discord.until(lambda: running.rest.create_message.await_count == 1, within=30)
    running.rest.create_dm_channel.assert_awaited_once_with(mia.discord_id)
    assert running.rest.create_message.await_args.kwargs["components"]

    async def confirmed() -> bool:
        found = await outcome(mia, item)
        return found is not None and found.status is plsremind.DeliveryStatus.DELIVERED

    await kit_.eventually(confirmed)
    (row,) = await running.store.messages_for_item(item.id)
    assert (row.message_id, row.channel_id, row.user_id) == (100, CHANNEL, mia.id)

    async def spent() -> bool:
        return (await mia.rest.get_item(item.id)).reminders == ()

    await kit_.eventually(spent)


async def test_closed_dms_are_reported_to_the_hub(running: Running) -> None:
    running.rest.create_message.side_effect = hikari.ForbiddenError(
        url="https://discord.test", headers={}, raw_body=b"", message="Cannot send messages to this user", code=50007
    )
    item = await reminded_soon(running.mia)

    async def reported() -> bool:
        found = await outcome(running.mia, item)
        return found is not None and found.status is plsremind.DeliveryStatus.FAILED

    await kit_.eventually(reported)
    found = await outcome(running.mia, item)
    assert found is not None
    assert found.reason.startswith("Discord did not accept the direct message")
    assert await running.store.messages_for_item(item.id) == []


async def test_a_reminder_is_not_sent_twice_after_a_new_login(running: Running) -> None:
    mia, hub = running.mia, running.hub
    lost = mock.AsyncMock(side_effect=plsremind.NetworkError("the confirmation got lost"))
    with mock.patch.object(hub.client.rest, "acknowledge_delivery", lost):
        item = await reminded_soon(mia)
        await discord.until(lambda: lost.await_count == 1, within=30)
    assert running.rest.create_message.await_count == 1
    assert await outcome(mia, item) is None

    await hub.close()
    await hub.start()

    async def confirmed() -> bool:
        found = await outcome(mia, item)
        return found is not None and found.status is plsremind.DeliveryStatus.DELIVERED

    await kit_.eventually(confirmed)
    assert running.rest.create_message.await_count == 1


async def test_done_in_discord_reaches_the_hub_and_only_other_messages_are_redrawn(running: Running) -> None:
    mia = running.mia
    item = await mia.rest.create_item(title="Report")
    await running.store.record(
        delivery_id="d1",
        user_id=mia.id,
        item_id=item.id,
        shown_at=item.updated_at,
        messages=[Sent(555, CHANNEL, 1234), Sent(556, CHANNEL, 1234)],
        now=now(),
    )
    await discord.until(lambda: running.hub.cached(mia.id, item.id) is not None, within=10)

    changed = await running.hub.set_done(mia.id, item.id, done=True, origin=555)

    assert changed.status is plsremind.ItemStatus.DONE
    assert (await mia.rest.get_item(item.id)).status is plsremind.ItemStatus.DONE
    await discord.until(lambda: bool(running.redraws.messages), within=10)
    await discord.until(lambda: not running.sync.pending, within=10)
    assert running.redraws.messages == [556]
    assert running.hub.cached(mia.id, item.id) is not None, "a completed item stays readable through the history"


async def test_what_the_bot_wrote_is_what_it_reads_next(running: Running) -> None:
    mia, hub = running.mia, running.hub
    made = await hub.add(mia.id, title="Report")
    assert hub.cached(mia.id, made.id) is not None
    assert made.id in [item.id for item in hub.open_items(mia.id)]

    await hub.edit(mia.id, made.id, title="Final report", due=None, note="n")
    seen = hub.cached(mia.id, made.id)
    assert seen is not None
    assert (seen.title, seen.note) == ("Final report", "n")

    await hub.remind(mia.id, made.id, now() + datetime.timedelta(hours=1))
    seen = hub.cached(mia.id, made.id)
    assert seen is not None
    assert len(seen.reminders) == 1

    step = await mia.rest.create_subitem(made.id, title="first")
    await discord.until(lambda: bool((hub.cached(mia.id, made.id) or made).subitems), within=10)
    await hub.tick(mia.id, made.id, step.id, done=True)
    seen = hub.cached(mia.id, made.id)
    assert seen is not None
    assert seen.subitems[0].done is True

    await hub.set_done(mia.id, made.id, done=True)
    seen = hub.cached(mia.id, made.id)
    assert seen is not None
    assert seen.status is plsremind.ItemStatus.DONE
    assert made.id not in [item.id for item in hub.open_items(mia.id)]

    await hub.set_done(mia.id, made.id, done=False)
    await hub.delete(mia.id, made.id)
    assert hub.cached(mia.id, made.id) is None
    assert made.id not in [item.id for item in hub.open_items(mia.id)]


async def test_a_change_in_the_hub_redraws_the_message(running: Running) -> None:
    mia = running.mia
    item = await mia.rest.create_item(title="Report")
    await running.store.record(
        delivery_id="d1",
        user_id=mia.id,
        item_id=item.id,
        shown_at=None,
        messages=[Sent(555, CHANNEL, 1234)],
        now=now(),
    )
    await mia.rest.update_item(item.id, title="Final report")
    await discord.until(lambda: running.redraws.messages == [555], within=10)
    await mia.rest.delete_item(item.id)
    await discord.until(lambda: running.redraws.messages == [555, 555], within=10)


async def test_a_user_who_switches_the_connection_off_is_forgotten(running: Running) -> None:
    mia = running.mia
    item = await mia.rest.create_item(title="Report")
    await running.store.record(
        delivery_id="d1",
        user_id=mia.id,
        item_id=item.id,
        shown_at=None,
        messages=[Sent(555, CHANNEL, 1234)],
        now=now(),
    )
    await mia.browser.call("PUT", f"/users/@me/connections/{running.connection}", {"enabled": False})

    await discord.until(lambda: running.hub.user_of(mia.discord_id) is None, within=10)

    async def forgotten() -> bool:
        return await running.store.messages_for_user(mia.id) == []

    await kit_.eventually(forgotten, within=10)


async def test_an_edit_sets_the_deadline_the_way_it_was_meant(running: Running) -> None:
    mia, hub = running.mia, running.hub
    item = await mia.rest.create_item(title="Report", note="keep me")
    tomorrow = (now() + datetime.timedelta(days=1)).date()
    at = now().replace(microsecond=0) + datetime.timedelta(days=2)

    whole_day = await hub.edit(mia.id, item.id, title="Report 2", due=Day(date=tomorrow), note=None)
    assert (whole_day.title, whole_day.due_date, whole_day.all_day, whole_day.note) == (
        "Report 2",
        tomorrow,
        True,
        "keep me",
    )
    assert whole_day.due_at is not None

    timed = await hub.edit(mia.id, item.id, title="Report 2", due=Moment(at=at), note="")
    assert (timed.due_at, timed.due_date, timed.all_day, timed.note) == (at, None, False, "")

    kept = await hub.edit(mia.id, item.id, title="Report 3", due=hub_.UNCHANGED, note=None)
    assert (kept.title, kept.due_at) == ("Report 3", at)

    cleared = await hub.edit(mia.id, item.id, title="Report 3", due=None, note=None)
    assert (cleared.due_at, cleared.due_date) == (None, None)


async def test_an_item_of_someone_else_cannot_be_reached_through_a_user(kit: kit_.Kit, running: Running) -> None:
    ben = await kit.person("ben")
    theirs = await ben.rest.create_item(title="Ben's")
    assert await running.hub.item(running.mia.id, theirs.id) is None
    own = await running.mia.rest.create_item(title="Mia's")
    found = await running.hub.item(running.mia.id, own.id)
    assert found is not None
    assert found.title == "Mia's"


async def test_a_personal_token_is_refused(kit: kit_.Kit, tmp_path: pathlib.Path) -> None:
    owner = await kit.person("solo", remind=True)
    hub = hub_.connect(config(kit, owner.token, tmp_path))
    with pytest.raises(hub_.HubError, match="discord"):
        await hub.start()
    assert not hub.ready.is_set()
