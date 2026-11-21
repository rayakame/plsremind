from __future__ import annotations

import asyncio
import collections.abc
import contextlib
import datetime
import pathlib
import typing
from unittest import mock

import hikari
import msgspec
import plsremind
import pytest
import risa

from plsremind_discord.store import Sent
from plsremind_discord.store import Store
from plsremind_discord.sync import QUIET
from plsremind_discord.sync import Sync
from plsremind_discord.views import common
from plsremind_discord.views.reminder import ReminderView
from tests.support import discord
from tests.support import things
from tests.support.fake_hub import FakeHub

MIA, BEN = things.MIA, things.BEN
ITEM = things.item(1)
OTHER = things.item(2, list_id=things.WORK)
CHANNEL = 7


class Views:
    """Stands in for the risa client: records every redraw and yields a loaded view."""

    def __init__(self, hub: FakeHub) -> None:
        self.hub = hub
        self.redrawn: list[int] = []
        self.fail: dict[int, BaseException] = {}
        self.hold: asyncio.Event | None = None
        self.entered = asyncio.Event()
        self.items: dict[int, plsremind.ID] = {}

    @contextlib.asynccontextmanager
    async def open[T: risa.View](
        self, channel: int, message: int, cls: type[T], /
    ) -> collections.abc.AsyncGenerator[T]:
        assert channel == CHANNEL
        assert cls is ReminderView
        self.redrawn.append(message)
        self.entered.set()
        if self.hold is not None:
            await self.hold.wait()
        if message in self.fail:
            raise self.fail[message]
        item = await self.hub.item(MIA, self.items[message])
        view = ReminderView(user=common.pack(MIA), item=common.pack(self.items[message]))
        view.shown = discord.shown(item)
        yield typing.cast("T", view)


class Made(typing.NamedTuple):
    sync: Sync
    hub: FakeHub
    views: Views
    store: Store
    slept: list[float]
    quiet: asyncio.Event


@pytest.fixture
async def made(tmp_path: pathlib.Path) -> collections.abc.AsyncIterator[Made]:
    hub = FakeHub()
    hub.knows(things.user(), ITEM, OTHER)
    store = await Store.open(tmp_path / "discord.db")
    views = Views(hub)
    slept: list[float] = []
    quiet = asyncio.Event()
    quiet.set()

    async def sleep(seconds: float) -> None:
        slept.append(seconds)
        await quiet.wait()

    sync = Sync(hub=hub, views=views, store=store, sleep=sleep)
    try:
        yield Made(sync, hub, views, store, slept, quiet)
    finally:
        await sync.close()
        await store.close()


async def sent(
    made: Made, message: int, item: plsremind.Item = ITEM, user: plsremind.ID = MIA, *, account: int = 1234
) -> None:
    made.views.items[message] = item.id
    await made.store.record(
        delivery_id=f"d{message}",
        user_id=user,
        item_id=item.id,
        shown_at=item.updated_at,
        messages=[Sent(message, CHANNEL, account)],
        now=things.NOW,
    )


def updated(item: plsremind.Item, old: plsremind.Item | None, *, nonce: str | None = None) -> plsremind.ItemUpdateEvent:
    return plsremind.ItemUpdateEvent(user_id=MIA, actor=None, nonce=nonce, item=item, old_item=old)


def renamed(item: plsremind.Item, title: str = "new") -> plsremind.Item:
    return msgspec.structs.replace(item, title=title, updated_at=item.updated_at + datetime.timedelta(minutes=1))


async def settled(made: Made) -> None:
    await discord.until(lambda: not made.sync.pending)


async def test_a_change_that_shows_redraws_the_message_after_a_quiet_moment(made: Made) -> None:
    await sent(made, 100)
    changed = renamed(ITEM)
    made.hub.knows(things.user(), changed, OTHER)
    await made.sync.item_changed(updated(changed, ITEM))
    assert made.sync.pending == frozenset({100})
    await settled(made)
    assert made.views.redrawn == [100]
    assert made.slept == [QUIET]
    (row,) = await made.store.messages_for_item(ITEM.id)
    assert row.shown_at == changed.updated_at


async def test_several_changes_in_a_row_cause_one_redraw(made: Made) -> None:
    await sent(made, 100)
    made.quiet.clear()
    for title in ("a", "b", "c"):
        await made.sync.item_changed(updated(renamed(ITEM, title), ITEM))
    assert made.views.redrawn == []
    made.quiet.set()
    await settled(made)
    assert made.views.redrawn == [100]


@pytest.mark.parametrize(
    "change",
    [
        {"title": "x"},
        {"note": "x"},
        {"priority": plsremind.Priority.HIGH},
        {"list_id": things.WORK},
        {"status": plsremind.ItemStatus.DONE},
        {"due_at": things.NOW},
        {"due_date": things.NOW.date()},
    ],
)
async def test_every_field_the_message_shows_counts(made: Made, change: dict[str, typing.Any]) -> None:
    await sent(made, 100)
    await made.sync.item_changed(updated(msgspec.structs.replace(ITEM, **change), ITEM))
    await settled(made)
    assert made.views.redrawn == [100]


async def test_a_change_the_message_does_not_show_is_left_alone(made: Made) -> None:
    await sent(made, 100)
    touched = msgspec.structs.replace(
        ITEM, updated_at=ITEM.updated_at + datetime.timedelta(minutes=1), subitems=(things.subitem(1, ITEM.id),)
    )
    await made.sync.item_changed(updated(touched, ITEM))
    assert made.sync.pending == frozenset()
    assert made.views.redrawn == []


async def test_a_change_to_an_item_the_cache_did_not_hold_redraws(made: Made) -> None:
    await sent(made, 100)
    await made.sync.item_changed(updated(ITEM, None))
    await settled(made)
    assert made.views.redrawn == [100]


async def test_the_message_that_was_clicked_is_skipped_and_the_others_are_not(made: Made) -> None:
    await sent(made, 100)
    await sent(made, 101)
    await made.sync.item_changed(updated(renamed(ITEM), ITEM, nonce="100.4f2a9c01b7de"))
    await settled(made)
    assert made.views.redrawn == [101]


async def test_a_change_from_a_message_the_bot_does_not_hold_redraws_everything(made: Made) -> None:
    await sent(made, 100)
    await sent(made, 101)
    await made.sync.item_changed(updated(renamed(ITEM), ITEM, nonce="999.4f2a9c01b7de"))
    await settled(made)
    assert sorted(made.views.redrawn) == [100, 101]


async def test_a_deleted_item_redraws_its_messages(made: Made) -> None:
    await sent(made, 100)
    await sent(made, 200, OTHER)
    gone = plsremind.ItemDeleteEvent(user_id=MIA, actor=None, nonce=None, item_id=ITEM.id, old_item=ITEM)
    made.hub.knows(things.user(), OTHER)
    await made.sync.item_changed(gone)
    await settled(made)
    assert made.views.redrawn == [100]
    assert (await made.store.messages_for_item(ITEM.id))[0].shown_at is None


@pytest.mark.parametrize(
    "ended",
    [
        risa.MessageOutdatedError("reminder", hikari.Snowflake(100)),
        hikari.NotFoundError(
            url="https://discord.test", headers={}, raw_body=b"", message="Unknown Message", code=10008
        ),
        hikari.ForbiddenError(
            url="https://discord.test", headers={}, raw_body=b"", message="Missing Access", code=50001
        ),
    ],
)
async def test_a_message_that_can_no_longer_be_drawn_is_forgotten(made: Made, ended: Exception) -> None:
    await sent(made, 100)
    made.views.fail[100] = ended
    await made.sync.item_changed(updated(renamed(ITEM), ITEM))
    await settled(made)
    assert await made.store.messages_for_item(ITEM.id) == []


async def test_any_other_trouble_keeps_the_message(made: Made, caplog: pytest.LogCaptureFixture) -> None:
    await sent(made, 100)
    made.views.fail[100] = RuntimeError("boom")
    await made.sync.item_changed(updated(renamed(ITEM), ITEM))
    await settled(made)
    assert len(await made.store.messages_for_item(ITEM.id)) == 1
    assert "message 100 could not be redrawn" in caplog.text


async def test_a_change_during_a_redraw_causes_exactly_one_more(made: Made) -> None:
    await sent(made, 100)
    made.views.hold = asyncio.Event()
    await made.sync.item_changed(updated(renamed(ITEM, "a"), ITEM))
    await asyncio.wait_for(made.views.entered.wait(), 2)
    await made.sync.item_changed(updated(renamed(ITEM, "b"), ITEM))
    await made.sync.item_changed(updated(renamed(ITEM, "c"), ITEM))
    made.views.hold.set()
    await settled(made)
    assert made.views.redrawn == [100, 100]


async def test_a_renamed_list_redraws_the_messages_of_its_items(made: Made) -> None:
    await sent(made, 100)
    await sent(made, 200, OTHER)
    work = things.a_list(things.WORK, "Work", 1)
    renamed_list = msgspec.structs.replace(work, name="Job")
    await made.sync.list_changed(
        plsremind.ListUpdateEvent(user_id=MIA, actor=None, nonce=None, list=renamed_list, old_list=work)
    )
    await settled(made)
    assert made.views.redrawn == [200]


async def test_a_list_that_only_moved_redraws_nothing(made: Made) -> None:
    await sent(made, 200, OTHER)
    work = things.a_list(things.WORK, "Work", 1)
    moved = msgspec.structs.replace(work, position=5)
    await made.sync.list_changed(
        plsremind.ListUpdateEvent(user_id=MIA, actor=None, nonce=None, list=moved, old_list=work)
    )
    assert made.sync.pending == frozenset()


async def test_after_a_start_what_was_missed_is_caught_up_on(made: Made) -> None:
    await sent(made, 100)
    await sent(made, 200, OTHER)
    await sent(made, 300, things.item(3))
    changed = renamed(ITEM)
    made.hub.knows(things.user(), changed, OTHER, things.item(3))
    made.hub.uncached.add(things.item(3).id)

    await made.sync.user_ready(plsremind.UserReadyEvent(user_id=MIA))
    await settled(made)

    assert sorted(made.views.redrawn) == [100, 300]


async def test_a_user_who_is_gone_takes_their_messages_along(made: Made) -> None:
    await sent(made, 100)
    await sent(made, 900, things.item(9), user=BEN)
    await made.sync.user_gone(plsremind.UserGoneEvent(user_id=MIA))
    assert await made.store.messages_for_user(MIA) == []
    assert len(await made.store.messages_for_user(BEN)) == 1


async def test_closing_drops_what_was_still_waiting(made: Made) -> None:
    await sent(made, 100)
    made.views.hold = asyncio.Event()
    await made.sync.item_changed(updated(renamed(ITEM), ITEM))
    await asyncio.wait_for(made.views.entered.wait(), 2)
    await made.sync.close()
    assert made.sync.pending == frozenset()


def moved(user: plsremind.ConnectionUser) -> plsremind.UserUpdateEvent:
    return plsremind.UserUpdateEvent(user_id=user.id, actor=None, nonce=None, user=user)


async def test_an_account_that_was_unlinked_loses_its_messages(made: Made) -> None:
    both = things.user(external_ids=("1234", "5678"))
    made.hub.knows(both, ITEM, OTHER)
    await sent(made, 100, account=1234)
    await sent(made, 101, account=5678)

    await made.sync.user_changed(moved(both))
    assert [row.message_id for row in await made.store.messages_for_user(MIA)] == [100, 101]

    one = things.user(external_ids=("1234",))
    made.hub.knows(one, ITEM, OTHER)
    await made.sync.user_changed(moved(one))
    assert [row.message_id for row in await made.store.messages_for_user(MIA)] == [100]

    await made.sync.item_changed(updated(renamed(ITEM), ITEM))
    await settled(made)
    assert made.views.redrawn == [100]


async def test_a_new_session_drops_the_messages_of_accounts_unlinked_meanwhile(made: Made) -> None:
    await sent(made, 100, account=1234)
    await sent(made, 101, account=5678)
    made.hub.their_items[MIA][ITEM.id] = renamed(ITEM)

    await made.sync.user_ready(plsremind.UserReadyEvent(user_id=MIA))
    await settled(made)

    assert [row.message_id for row in await made.store.messages_for_user(MIA)] == [100]
    assert made.views.redrawn == [100]


async def test_a_user_whose_profile_is_not_known_keeps_every_message(made: Made) -> None:
    await sent(made, 100, account=1234)
    made.hub.people.clear()

    await made.sync.user_ready(plsremind.UserReadyEvent(user_id=MIA))
    personal = typing.cast("plsremind.User", mock.Mock(spec=plsremind.User))
    await made.sync.user_changed(plsremind.UserUpdateEvent(user_id=MIA, actor=None, nonce=None, user=personal))
    await settled(made)

    assert [row.message_id for row in await made.store.messages_for_user(MIA)] == [100]


async def test_a_list_renamed_while_the_bot_was_away_is_caught_up_on_return(made: Made) -> None:
    await sent(made, 100)
    inbox, work = made.hub.their_lists[MIA]
    renamed_list = msgspec.structs.replace(
        inbox, name="Eingang", updated_at=inbox.updated_at + datetime.timedelta(hours=1)
    )
    made.hub.their_lists[MIA] = (renamed_list, work)

    await made.sync.user_ready(plsremind.UserReadyEvent(user_id=MIA))
    await settled(made)

    assert made.views.redrawn == [100]
    (row,) = await made.store.messages_for_item(ITEM.id)
    assert row.shown_at == renamed_list.updated_at, "the next return does not redraw it again"


async def test_a_redraw_waiting_for_quiet_is_called_off_when_its_account_is_unlinked(made: Made) -> None:
    both = things.user(external_ids=("1234", "5678"))
    made.hub.knows(both, ITEM, OTHER)
    await sent(made, 100, account=5678)
    made.quiet.clear()
    await made.sync.item_changed(updated(renamed(ITEM), ITEM))
    assert made.sync.pending == frozenset({100})

    await made.sync.user_changed(moved(things.user(external_ids=("1234",))))
    assert made.sync.pending == frozenset()
    made.quiet.set()
    await asyncio.sleep(0)
    assert made.views.redrawn == []


async def test_a_redraw_waiting_for_quiet_is_called_off_when_the_user_is_gone(made: Made) -> None:
    await sent(made, 100)
    made.quiet.clear()
    await made.sync.item_changed(updated(renamed(ITEM), ITEM))
    await made.sync.user_gone(plsremind.UserGoneEvent(user_id=MIA))
    assert made.sync.pending == frozenset()
    made.quiet.set()
    await asyncio.sleep(0)
    assert made.views.redrawn == []
