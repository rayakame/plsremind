from __future__ import annotations

import collections.abc
import datetime
import pathlib
import typing

import hikari
import msgspec
import plsremind
import pytest
import risa

from plsremind_discord import bot
from plsremind_discord.commands import common as commands_
from plsremind_discord.store import Sent
from plsremind_discord.store import Store
from plsremind_discord.sync import Sync
from plsremind_discord.views import common
from plsremind_discord.views.detail import DetailView
from plsremind_discord.views.overview import OverviewView
from plsremind_discord.views.reminder import Notice
from plsremind_discord.views.reminder import ReminderView
from tests.support import discord
from tests.support import things
from tests.support import wire
from tests.support.fake_hub import FakeHub

NOW, MIA = things.NOW, things.MIA
HOUR = datetime.timedelta(hours=1)
UPDATE = hikari.ResponseType.MESSAGE_UPDATE
ITEM = things.item(1, title="Report", due_at=NOW + HOUR, subitems=2)
OTHER = things.item(2, title="Call Ben", list_id=things.WORK)


class Made(typing.NamedTuple):
    hub: FakeHub
    views: risa.GatewayEnabledClient
    store: Store


@pytest.fixture
async def made(tmp_path: pathlib.Path) -> collections.abc.AsyncIterator[Made]:
    hub = FakeHub()
    hub.knows(things.user(), ITEM, OTHER)
    hub.knows(things.user(things.BEN, external_ids=("77",)))
    store = await Store.open(tmp_path / "discord.db")
    views = discord.views(hub, Notice, ReminderView, OverviewView, DetailView, store=store)
    tools = commands_.Tools(
        hub=hub, texts=discord.say(), clock=discord.FixedClock(), config=discord.CONFIG, views=views
    )
    views.di.registry_for(risa.Contexts.DEFAULT).register_value(commands_.Tools, tools)
    views.error_handler(bot.click_failed)
    try:
        yield Made(hub, views, store)
    finally:
        await store.close()


async def drawn(made: Made, view: risa.View) -> list[dict[str, typing.Any]]:
    return wire.sent(await made.views.build(view))


def reminder() -> ReminderView:
    return ReminderView(user=common.pack(MIA), item=common.pack(ITEM.id))


async def test_done_on_a_reminder_reaches_the_hub_and_redraws_the_message(made: Made) -> None:
    payloads = await drawn(made, reminder())
    rest = await wire.click(made.views, payloads, wire.custom_id(payloads, label="Erledigt"))

    assert made.hub.named("set_done") == [{"user": MIA, "item": ITEM.id, "done": True, "origin": 555}]
    kind, hidden, after = wire.answered(rest)
    assert (kind, hidden) == (UPDATE, False)
    assert "~~Report~~" in wire.texts(after)

    rest = await wire.click(made.views, after, wire.custom_id(after, label="Rückgängig"))
    assert made.hub.named("set_done")[-1]["done"] is False
    assert "~~" not in wire.texts(wire.answered(rest)[2])


@pytest.mark.parametrize("stranger", [77, 999], ids=["another user", "nobody the hub knows"])
async def test_a_click_by_anyone_but_the_owner_changes_nothing_and_is_answered_in_private(
    made: Made, stranger: int
) -> None:
    payloads = await drawn(made, reminder())
    rest = await wire.click(made.views, payloads, wire.custom_id(payloads, label="Erledigt"), user=stranger)

    assert made.hub.named("set_done") == []
    kind, hidden, said = wire.answered(rest)
    assert (kind, hidden) == (hikari.ResponseType.MESSAGE_CREATE, True)
    assert "nicht mit plsremind verbunden" in wire.texts(said)


async def test_a_fixed_snooze_sets_the_reminder_and_turns_the_message_into_the_notice(made: Made) -> None:
    payloads = await drawn(made, reminder())
    await made.store.record(
        delivery_id="d1",
        user_id=MIA,
        item_id=ITEM.id,
        item_updated_at=ITEM.updated_at,
        messages=[Sent(555, wire.CHANNEL, 1234)],
        now=NOW,
    )
    select = wire.custom_id(payloads, kind=3)
    rest = await wire.click(made.views, payloads, select, values=("1h",))

    assert made.hub.named("remind") == [{"user": MIA, "item": ITEM.id, "at": NOW + HOUR, "origin": 555}]
    assert rest.create_interaction_response.await_args.args[2] is hikari.ResponseType.DEFERRED_MESSAGE_UPDATE
    edited = wire.sent({"components": rest.edit_interaction_response.await_args.kwargs["components"]})
    assert "Report" in wire.texts(edited)
    assert f"<t:{int((NOW + HOUR).timestamp())}:F>" in wire.texts(edited)
    assert await made.store.messages_for_item(ITEM.id) == [], "a notice has nothing left to keep true"


async def test_a_time_of_ones_own_opens_the_modal_instead_of_answering(made: Made) -> None:
    payloads = await drawn(made, reminder())
    rest = await wire.click(made.views, payloads, wire.custom_id(payloads, kind=3), values=("custom",))

    assert made.hub.named("remind") == []
    rest.create_interaction_response.assert_not_called()
    modal = rest.create_modal_response.await_args
    assert modal.kwargs["custom_id"].startswith("pr:snooze:r:")


async def test_the_overview_leads_into_an_item_and_back_to_the_same_page(made: Made) -> None:
    overview = await drawn(made, OverviewView(user=common.pack(MIA), only=common.pack(things.WORK)))
    rest = await wire.click(made.views, overview, wire.custom_id(overview, label="Öffnen"))
    kind, _, detail = wire.answered(rest)
    assert kind is UPDATE
    assert "## Call Ben" in wire.texts(detail)

    rest = await wire.click(made.views, detail, wire.custom_id(detail, label="Zur Übersicht"))
    back = wire.texts(wire.answered(rest)[2])
    assert "Call Ben" in back
    assert "Report" not in back, "the list that was chosen is still the only one shown"


async def test_ticking_a_step_names_that_step_to_the_hub(made: Made) -> None:
    detail = await drawn(made, DetailView(user=common.pack(MIA), item=common.pack(ITEM.id)))
    rest = await wire.click(made.views, detail, wire.custom_id(detail, label="Abhaken", nth=1))

    (ticked,) = made.hub.named("tick")
    assert (ticked["subitem"], ticked["done"], ticked["origin"]) == (ITEM.subitems[1].id, True, 555)
    assert "~~step 2~~" in wire.texts(wire.answered(rest)[2])


async def test_deleting_asks_first_and_then_leaves_for_an_overview_without_the_item(made: Made) -> None:
    detail = await drawn(made, DetailView(user=common.pack(MIA), item=common.pack(ITEM.id)))
    rest = await wire.click(made.views, detail, wire.custom_id(detail, label="Löschen"))
    asking = wire.answered(rest)[2]
    assert made.hub.named("delete") == []

    rest = await wire.click(made.views, asking, wire.custom_id(asking, label="Wirklich löschen"))
    assert made.hub.named("delete") == [{"user": MIA, "item": ITEM.id, "origin": 555}]
    after = wire.texts(wire.answered(rest)[2])
    assert "Deine Aufgaben" in after
    assert "Report" not in after


async def test_a_change_made_elsewhere_redraws_the_message_discord_holds(made: Made) -> None:
    store = made.store
    payloads = await drawn(made, reminder())
    rest = typing.cast("typing.Any", made.views.rest)
    rest.fetch_message.return_value = wire.as_message(made.views, payloads, 555)
    await store.record(
        delivery_id="d1",
        user_id=MIA,
        item_id=ITEM.id,
        item_updated_at=ITEM.updated_at,
        messages=[Sent(555, wire.CHANNEL, 1234)],
        now=NOW,
    )

    async def at_once(_: float) -> None:
        return

    sync = Sync(hub=made.hub, views=made.views, store=store, sleep=at_once)
    renamed = msgspec.structs.replace(ITEM, title="Final report", updated_at=NOW)
    made.hub.their_items[MIA][ITEM.id] = renamed
    try:
        await sync.item_changed(
            plsremind.ItemUpdateEvent(user_id=MIA, actor=None, nonce=None, item=renamed, old_item=ITEM)
        )
        await discord.until(lambda: not sync.pending)
    finally:
        await sync.close()

    rest.fetch_message.assert_awaited_once_with(wire.CHANNEL, 555)
    edit = rest.edit_message.await_args
    assert edit.args == (wire.CHANNEL, 555)
    assert "## Final report" in wire.texts(wire.sent({"components": edit.kwargs["components"]}))
    (row,) = await store.messages_for_item(ITEM.id)
    assert row.item_updated_at == NOW


async def test_a_message_that_shows_something_else_by_now_is_forgotten_instead_of_redrawn(made: Made) -> None:
    store = made.store
    notice = await drawn(made, Notice(text="snoozed"))
    rest = typing.cast("typing.Any", made.views.rest)
    rest.fetch_message.return_value = wire.as_message(made.views, notice, 555)
    await store.record(
        delivery_id="d1",
        user_id=MIA,
        item_id=ITEM.id,
        item_updated_at=ITEM.updated_at,
        messages=[Sent(555, wire.CHANNEL, 1234)],
        now=NOW,
    )

    async def at_once(_: float) -> None:
        return

    sync = Sync(hub=made.hub, views=made.views, store=store, sleep=at_once)
    renamed = msgspec.structs.replace(ITEM, title="Final report", updated_at=NOW)
    try:
        await sync.item_changed(
            plsremind.ItemUpdateEvent(user_id=MIA, actor=None, nonce=None, item=renamed, old_item=ITEM)
        )
        await discord.until(lambda: not sync.pending)
    finally:
        await sync.close()

    rest.edit_message.assert_not_called()
    assert await store.messages_for_item(ITEM.id) == []
