from __future__ import annotations

import collections.abc
import datetime
import pathlib
import typing
import uuid

import hikari
import plsremind
import pytest

from plsremind_discord import hub as hub_module
from plsremind_discord import modals
from plsremind_discord.store import Sent
from plsremind_discord.store import Store
from plsremind_discord.submits import Modals
from plsremind_discord.timeparse import Day
from plsremind_discord.timeparse import Moment
from plsremind_discord.views.detail import DetailView
from plsremind_discord.views.overview import OverviewView
from plsremind_discord.views.reminder import Notice
from plsremind_discord.views.reminder import ReminderView
from tests.support import discord
from tests.support import things
from tests.support.fake_hub import FakeHub

NOW, MIA = things.NOW, things.MIA
CREATE, UPDATE = hikari.ResponseType.MESSAGE_CREATE, hikari.ResponseType.MESSAGE_UPDATE
LINK = "https://discord.com/channels/1/2/3"
ITEM = things.item(1, title="Report", note="see wiki", due_at=NOW + datetime.timedelta(days=1))


class Made(typing.NamedTuple):
    modals: Modals
    hub: FakeHub
    store: Store


@pytest.fixture
async def made(tmp_path: pathlib.Path) -> collections.abc.AsyncIterator[Made]:
    hub = FakeHub()
    hub.knows(things.user(), ITEM)
    store = await Store.open(tmp_path / "discord.db")
    views = discord.views(hub, ReminderView, Notice, DetailView, OverviewView)
    built = Modals(
        hub=hub, views=views, store=store, texts=discord.say(), clock=discord.FixedClock(), config=discord.CONFIG
    )
    try:
        yield Made(built, hub, store)
    finally:
        await store.close()


def test_every_id_is_read_back() -> None:
    item = things.uid(1001)
    assert modals.read(modals.message_id(guild=1, channel=2, message=3)) == modals.MessageModal(link=LINK)
    assert modals.read(modals.message_id(guild=None, channel=2, message=3)) == modals.MessageModal(
        link="https://discord.com/channels/@me/2/3"
    )
    assert modals.read(modals.snooze_id(item, detail=None)) == modals.SnoozeModal(item=item, detail=None)
    assert modals.read(modals.snooze_id(item, detail=(things.WORK, 4))) == modals.SnoozeModal(
        item=item, detail=(things.WORK, 4)
    )
    assert modals.read(modals.snooze_id(item, detail=(None, 0))) == modals.SnoozeModal(item=item, detail=(None, 0))
    assert modals.read(modals.edit_id(item, (things.WORK, 4))) == modals.EditModal(item=item, back=(things.WORK, 4))
    longest = modals.message_id(guild=2**64 - 1, channel=2**64 - 1, message=2**64 - 1)
    assert len(longest) <= 100


@pytest.mark.parametrize(
    "other",
    ["", "other", "pr", "pr:nope:1", "pr:snooze:r:zz", "pr:edit:" + "0" * 32 + ":-:x", "pr:msg:1:2", "pr:snooze:x:1"],
)
def test_an_id_that_is_not_the_bots_is_left_alone(other: str) -> None:
    assert modals.read(other) is None


def test_the_message_modal_offers_the_title_to_change() -> None:
    rest = discord.click().rest
    title, rows = modals.message_modal(rest, discord.say(), hikari.Locale.DE, title="Buy milk")
    assert title == "Erinnere mich"
    assert len(rows) == 3
    calls = rest.build_modal_action_row.return_value.add_text_input.call_args_list
    assert [call.args[0] for call in calls] == [modals.TITLE, modals.WHEN, modals.NOTE]
    assert calls[0].kwargs["value"] == "Buy milk"
    assert calls[0].kwargs["max_length"] == 500
    assert [call.kwargs["required"] for call in calls] == [True, True, False]


async def test_a_message_becomes_an_item_with_a_reminder_and_the_way_back(made: Made) -> None:
    event = discord.submitted(
        modals.message_id(guild=1, channel=2, message=3),
        {modals.TITLE: " Call Ben ", modals.WHEN: "morgen 9", modals.NOTE: "about the report"},
        message=None,
    )
    await made.modals.submitted(event)
    (added,) = made.hub.named("add")
    assert (added["user"], added["title"], added["note"]) == (MIA, "Call Ben", f"about the report\n{LINK}")
    (reminded,) = made.hub.named("remind")
    tomorrow = (NOW + datetime.timedelta(days=1)).replace(hour=9, minute=0)
    assert reminded["at"] == tomorrow
    kind, hidden, text = discord.answer(event.interaction)
    assert (kind, hidden) == (CREATE, True)
    assert f"Ich erinnere dich <t:{int(tomorrow.timestamp())}:F>: **Call Ben**" in text


async def test_a_message_without_a_note_keeps_only_the_link(made: Made) -> None:
    event = discord.submitted(
        modals.message_id(guild=None, channel=2, message=3), {modals.TITLE: "x", modals.WHEN: "2h"}, message=None
    )
    await made.modals.submitted(event)
    assert made.hub.named("add")[0]["note"] == "https://discord.com/channels/@me/2/3"


async def test_a_reminder_that_cannot_be_set_takes_the_item_back(made: Made) -> None:
    made.hub.fail["remind"] = plsremind.ServerError(500, "boom", "no")
    event = discord.submitted(
        modals.message_id(guild=1, channel=2, message=3), {modals.TITLE: "x", modals.WHEN: "2h"}, message=None
    )
    await made.modals.submitted(event)
    assert len(made.hub.named("delete")) == 1
    kind, hidden, text = discord.answer(event.interaction)
    assert (kind, hidden) == (CREATE, True)
    assert "Das hat nicht geklappt" in text


@pytest.mark.parametrize(
    ("when", "said"), [("bald", "Diese Zeit habe ich nicht verstanden"), ("heute 9", "Diese Zeit ist schon vorbei")]
)
async def test_a_time_that_cannot_be_used_creates_nothing(made: Made, when: str, said: str) -> None:
    event = discord.submitted(
        modals.message_id(guild=1, channel=2, message=3), {modals.TITLE: "x", modals.WHEN: when}, message=None
    )
    await made.modals.submitted(event)
    assert made.hub.calls == []
    assert said in discord.answer(event.interaction)[2]


async def test_a_custom_snooze_from_the_dm_ends_the_message(made: Made) -> None:
    await made.store.record(
        delivery_id="d1", user_id=MIA, item_id=ITEM.id, item_updated_at=None, messages=[Sent(555, 7)], now=NOW
    )
    event = discord.submitted(modals.snooze_id(ITEM.id, detail=None), {modals.WHEN: "in 3h"})
    await made.modals.submitted(event)
    (reminded,) = made.hub.named("remind")
    assert (reminded["at"], reminded["origin"]) == (NOW + datetime.timedelta(hours=3), 555)
    kind, hidden, text = discord.answer(event.interaction)
    assert (kind, hidden) == (UPDATE, False)
    assert "Verschoben" in text
    assert "Report" in text
    assert await made.store.messages_for_item(ITEM.id) == []


async def test_a_custom_snooze_from_the_detail_view_redraws_it(made: Made) -> None:
    event = discord.submitted(modals.snooze_id(ITEM.id, detail=(things.WORK, 2)), {modals.WHEN: "in 3h"})
    await made.modals.submitted(event)
    assert len(made.hub.named("remind")) == 1
    kind, hidden, text = discord.answer(event.interaction)
    assert (kind, hidden) == (UPDATE, False)
    assert "## Report" in text
    assert "Zur Übersicht" in text


async def test_a_refused_snooze_says_why(made: Made) -> None:
    made.hub.fail["remind"] = plsremind.ConflictError(409, "reminder_limit", "no")
    event = discord.submitted(modals.snooze_id(ITEM.id, detail=None), {modals.WHEN: "in 3h"})
    await made.modals.submitted(event)
    kind, hidden, text = discord.answer(event.interaction)
    assert (kind, hidden) == (CREATE, True)
    assert "20 anstehende Erinnerungen" in text


async def test_snoozing_an_item_that_is_gone_says_so(made: Made) -> None:
    event = discord.submitted(modals.snooze_id(things.uid(4242), detail=None), {modals.WHEN: "in 3h"})
    await made.modals.submitted(event)
    assert "gibt es nicht mehr" in discord.answer(event.interaction)[2]
    assert made.hub.named("remind") == []


@pytest.mark.parametrize(
    ("typed", "due"),
    [
        ("", None),
        ("fr", Day(date=datetime.date(2026, 11, 20))),
        ("fr 18:00", Moment(at=NOW.replace(day=20, hour=18, minute=0))),
        (modals.due_text(ITEM, things.BERLIN), hub_module.UNCHANGED),
    ],
)
async def test_an_edit_sends_what_was_typed(made: Made, typed: str, due: object) -> None:
    event = discord.submitted(
        modals.edit_id(ITEM.id, (None, 0)), {modals.TITLE: "Final report", modals.DUE: typed, modals.NOTE: "new note"}
    )
    await made.modals.submitted(event)
    (edited,) = made.hub.named("edit")
    assert (edited["title"], edited["due"], edited["note"], edited["origin"]) == ("Final report", due, "new note", 555)
    kind, hidden, text = discord.answer(event.interaction)
    assert (kind, hidden) == (UPDATE, False)
    assert "## Final report" in text


async def test_an_edit_without_the_note_field_leaves_the_note(made: Made) -> None:
    event = discord.submitted(modals.edit_id(ITEM.id, (None, 0)), {modals.TITLE: "x", modals.DUE: ""})
    await made.modals.submitted(event)
    assert made.hub.named("edit")[0]["note"] is None


async def test_an_overdue_deadline_left_as_it_was_does_not_block_the_edit(made: Made) -> None:
    late = things.item(2, title="Late", due_at=NOW - datetime.timedelta(days=1))
    made.hub.knows(things.user(), late)
    event = discord.submitted(
        modals.edit_id(late.id, (None, 0)),
        {modals.TITLE: "Still late", modals.DUE: modals.due_text(late, things.BERLIN)},
    )
    await made.modals.submitted(event)
    assert made.hub.named("edit")[0]["due"] is hub_module.UNCHANGED
    assert "## Still late" in discord.answer(event.interaction)[2]


async def test_editing_an_item_that_is_gone_shows_that_it_is_gone(made: Made) -> None:
    event = discord.submitted(modals.edit_id(things.uid(4242), (None, 0)), {modals.TITLE: "x", modals.DUE: ""})
    await made.modals.submitted(event)
    assert made.hub.named("edit") == []
    kind, _, text = discord.answer(event.interaction)
    assert kind == UPDATE
    assert "gibt es nicht mehr" in text


async def test_what_the_hub_refuses_is_said(made: Made) -> None:
    made.hub.fail["edit"] = plsremind.BadRequestError(422, "validation_failed", "no", {"title": "too long"})
    event = discord.submitted(modals.edit_id(ITEM.id, (None, 0)), {modals.TITLE: "x", modals.DUE: ""})
    await made.modals.submitted(event)
    kind, hidden, text = discord.answer(event.interaction)
    assert (kind, hidden) == (CREATE, True)
    assert "Der Hub hat das nicht angenommen" in text


async def test_someone_who_is_not_linked_gets_the_hint(made: Made) -> None:
    event = discord.submitted(modals.snooze_id(ITEM.id, detail=None), {modals.WHEN: "2h"}, user=999, locale="en-US")
    await made.modals.submitted(event)
    kind, hidden, text = discord.answer(event.interaction)
    assert (kind, hidden) == (CREATE, True)
    assert "is not linked to plsremind" in text
    assert "https://todo.example.com/settings" in text
    assert made.hub.calls == []


async def test_while_the_hub_is_not_there_yet_the_bot_says_so(made: Made) -> None:
    made.hub.ready.clear()
    event = discord.submitted(modals.snooze_id(ITEM.id, detail=None), {modals.WHEN: "2h"})
    await made.modals.submitted(event)
    assert "Der Bot startet gerade" in discord.answer(event.interaction)[2]


async def test_a_modal_of_someone_else_is_not_answered(made: Made) -> None:
    event = discord.submitted("lightbulb:something", {})
    await made.modals.submitted(event)
    event.interaction.create_initial_response.assert_not_called()


def test_the_longest_way_back_fits_into_an_id() -> None:
    assert len(modals.snooze_id(things.uid(1001), detail=(things.WORK, 2**31))) <= 100
    assert uuid.UUID(things.WORK).hex in modals.edit_id(things.uid(1001), (things.WORK, 1))


async def test_a_snooze_modal_without_its_message_still_sets_the_reminder(made: Made) -> None:
    event = discord.submitted(modals.snooze_id(ITEM.id, detail=None), {modals.WHEN: "in 3h"}, message=None)
    await made.modals.submitted(event)
    assert made.hub.named("remind")[0]["origin"] is None
    assert discord.answer(event.interaction)[0] == UPDATE
