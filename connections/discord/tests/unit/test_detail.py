from __future__ import annotations

import asyncio
import datetime
import uuid
from unittest import mock

import hikari
import msgspec
import plsremind
import pytest
from risa import ui

from plsremind_discord import modals
from plsremind_discord import patience
from plsremind_discord import texts
from plsremind_discord.views import common
from plsremind_discord.views.detail import DetailView
from plsremind_discord.views.overview import OverviewView
from tests.support import discord
from tests.support import things
from tests.support.fake_hub import FakeHub

NOW, BERLIN, MIA = things.NOW, things.BERLIN, things.MIA
HOUR = datetime.timedelta(hours=1)
DE = hikari.Locale.DE


def opened(
    item: plsremind.Item | None,
    *,
    confirm: bool = False,
    upcoming: datetime.datetime | None = None,
    back: tuple[plsremind.ID | None, int] = (None, 0),
) -> DetailView:
    view = DetailView(
        user=common.pack(MIA),
        item=common.pack(item.id if item is not None else things.uid(1001)),
        back_list=common.pack(back[0]) if back[0] is not None else None,
        back_page=back[1],
        confirm=confirm,
    )
    view.shown = discord.shown(item)
    view.upcoming = upcoming
    return view


def known(hub: FakeHub, item: plsremind.Item) -> DetailView:
    hub.knows(things.user(), item)
    return opened(item, back=(things.WORK, 2))


def test_an_open_item_shows_everything_that_can_be_done_with_it() -> None:
    item = things.item(1, title="Report", note="Template is in the team folder.", due_at=NOW + HOUR, subitems=2)
    layout = opened(item, upcoming=NOW + HOUR / 2).render()
    text = discord.written(layout)
    assert "## Report" in text
    assert f"Nächste Erinnerung: <t:{int((NOW + HOUR / 2).timestamp())}:F>" in text
    assert "> Template is in the team folder." in text
    assert "-# Unterpunkte · 0 von 2" in text
    assert discord.labels(layout) == [
        "Abhaken",
        "Abhaken",
        "Erledigt",
        "Bearbeiten",
        "Löschen",
        "Dashboard",
        "Zur Übersicht",
    ]
    assert len(discord.nodes(layout, ui.TextSelect)) == 1


def test_asking_before_deleting_leaves_only_the_two_answers() -> None:
    layout = opened(things.item(1), confirm=True).render()
    assert discord.labels(layout) == ["Wirklich löschen", "Abbrechen", "Zur Übersicht"]
    assert discord.nodes(layout, ui.TextSelect) == []


def test_steps_beyond_the_eighth_are_counted_and_not_shown() -> None:
    item = things.item(1, subitems=9)
    layout = opened(item).render()
    assert len(discord.nodes(layout, ui.Section)) == 8
    assert "-# + 1 weitere im Dashboard" in discord.written(layout)
    ticks = [section.accessory for section in discord.nodes(layout, ui.Section)]
    assert [discord.bound(tick) for tick in ticks if isinstance(tick, ui.Button)] == [
        common.arg(step.id) for step in item.subitems[:8]
    ]


def test_a_ticked_step_is_struck_and_can_be_reopened() -> None:
    item = things.item(1, subitems=1)
    ticked = msgspec.structs.replace(item, subitems=(things.subitem(1, item.id, done=True),))
    layout = opened(ticked).render()
    assert "~~step 1~~" in discord.written(layout)
    assert discord.labels(layout)[0] == "Offen"
    assert "-# Unterpunkte · 1 von 1" in discord.written(layout)


def test_a_done_item_can_be_reopened_and_nothing_else() -> None:
    layout = opened(things.item(1, done=True)).render()
    assert discord.labels(layout) == ["Rückgängig", "Dashboard", "Zur Übersicht"]
    assert discord.nodes(layout, ui.TextSelect) == []
    assert discord.nodes(layout, ui.Container)[0].accent_color == hikari.Color(common.DONE)


def test_an_item_that_is_gone_only_leads_back() -> None:
    layout = opened(None).render()
    assert discord.written(layout) == "Diese Aufgabe gibt es nicht mehr."
    assert discord.labels(layout) == ["Zur Übersicht"]


def test_a_long_note_is_cut() -> None:
    text = discord.written(opened(things.item(1, note="n" * 700)).render())
    assert "> " + "n" * 599 + "…" in text
    assert "n" * 600 not in text


def test_a_note_of_many_lines_is_cut_as_a_whole() -> None:
    text = discord.written(opened(things.item(1, note="\n".join(["line"] * 400))).render())
    quoted = [line for line in text.splitlines() if line.startswith(">")]
    assert quoted[-1] == "> …"
    assert len("\n".join(quoted)) <= 620
    assert quoted[0] == "> line"


def test_the_fullest_detail_view_stays_within_what_discord_allows() -> None:
    item = things.item(1, note="x", due_at=NOW + HOUR, subitems=12)
    assert discord.count(opened(item, upcoming=NOW).render()) <= 40


def test_the_fullest_detail_view_stays_within_what_discord_allows_of_text() -> None:
    item = things.item(
        1,
        title=things.WORST,
        note="\n".join([things.WORST] * 20),
        due_at=NOW + HOUR,
        priority=plsremind.Priority.HIGH,
        subitems=12,
    )
    steps = tuple(msgspec.structs.replace(step, title=things.WORST) for step in item.subitems)
    view = opened(msgspec.structs.replace(item, subitems=steps), upcoming=NOW)
    view.shown = discord.shown(view.shown.item if view.shown else None, list_name=things.WORST)
    assert texts.units(discord.written(view.render())) <= 4000


def test_the_fullest_detail_view_stays_within_what_discord_allows_of_wide_text() -> None:
    wide = things.WIDE * 150
    item = things.item(1, title=wide * 4, note="\n".join([wide] * 20), due_at=NOW + HOUR, subitems=12)
    steps = tuple(msgspec.structs.replace(step, title=wide) for step in item.subitems)
    view = opened(msgspec.structs.replace(item, subitems=steps), upcoming=NOW)
    view.shown = discord.shown(view.shown.item if view.shown else None, list_name=wide)
    assert texts.units(discord.written(view.render())) <= 4000


def test_a_title_of_wide_characters_still_fits_into_the_edit_modal() -> None:
    rest = mock.Mock(spec=hikari.api.RESTClient)
    modals.edit_modal(rest, discord.say(), DE, things.item(1, title=things.WIDE * 300, note=things.WIDE * 2500), BERLIN)
    fields = rest.build_modal_action_row.return_value.add_text_input.call_args_list
    assert [call.args[0] for call in fields] == [modals.TITLE, modals.DUE]
    assert fields[0].kwargs["max_length"] >= 600


async def test_loading_reads_the_item_and_its_next_reminder() -> None:
    hub = FakeHub()
    item = things.item(1, due_at=NOW + 3 * HOUR)
    hub.knows(things.user(locale=None), item)
    hub.schedules[item.id] = (things.scheduled(NOW + 2 * HOUR), things.scheduled(NOW + HOUR))
    view = DetailView(
        user=common.pack(MIA), item=common.pack(item.id), back_list=common.pack(things.WORK), back_page=9, speaks="de"
    )
    built = await discord.views(hub, DetailView, OverviewView).build(view)
    assert built.components
    assert view.shown is not None
    assert (view.shown.item, view.shown.locale) == (item, DE)
    assert view.upcoming == NOW + HOUR


async def test_a_schedule_that_cannot_be_read_is_left_out() -> None:
    hub = FakeHub()
    item = things.item(1)
    hub.knows(things.user(), item)
    hub.fail["schedule"] = plsremind.NetworkError("no route")
    view = DetailView(user=common.pack(MIA), item=common.pack(item.id))
    await discord.views(hub, DetailView, OverviewView).build(view)
    assert view.upcoming is None
    assert view.shown is not None
    assert view.shown.item == item


async def test_a_schedule_that_takes_too_long_is_left_out(monkeypatch: pytest.MonkeyPatch) -> None:
    hub = FakeHub()
    item = things.item(1)
    hub.knows(things.user(), item)
    hub.schedules[item.id] = (things.scheduled(NOW + HOUR),)
    discord.slow(monkeypatch, hub, "schedule")
    monkeypatch.setattr(patience, "PATIENCE", 0.01)
    view = DetailView(user=common.pack(MIA), item=common.pack(item.id))
    await asyncio.wait_for(discord.views(hub, DetailView, OverviewView).build(view), 2)
    assert view.upcoming is None
    assert view.shown is not None
    assert view.shown.item == item


async def test_a_done_item_has_no_schedule_to_ask_for() -> None:
    hub = FakeHub()
    item = things.item(1, done=True)
    hub.knows(things.user(), item)
    await discord.views(hub, DetailView, OverviewView).build(
        DetailView(user=common.pack(MIA), item=common.pack(item.id))
    )
    assert hub.named("schedule") == []


async def test_done_and_undo_from_the_detail_view() -> None:
    hub, ctx = FakeHub(), discord.click()
    view = known(hub, things.item(1))
    view.upcoming = NOW + HOUR
    await DetailView.done.func(view, ctx, hub)
    assert hub.named("set_done")[0]["origin"] == 555
    assert view.upcoming is None
    assert discord.labels(view.render())[0] == "Rückgängig"
    await DetailView.undo.func(view, ctx, hub)
    assert discord.labels(view.render())[0] == "Erledigt"
    assert ctx.rerender.await_count == 2


async def test_a_step_is_ticked_and_shown_ticked() -> None:
    hub, ctx = FakeHub(), discord.click()
    item = things.item(1, subitems=2)
    view = known(hub, item)
    await DetailView.tick.func(view, ctx, common.arg(item.subitems[1].id), hub)
    assert hub.named("tick") == [
        {"user": MIA, "item": item.id, "subitem": item.subitems[1].id, "done": True, "origin": 555}
    ]
    assert "~~step 2~~" in discord.written(view.render())
    await DetailView.tick.func(view, ctx, common.arg(item.subitems[1].id), hub)
    assert hub.named("tick")[-1]["done"] is False
    assert "~~step 2~~" not in discord.written(view.render())


async def test_a_step_that_was_removed_elsewhere_disappears() -> None:
    hub, ctx = FakeHub(), discord.click()
    item = things.item(1, subitems=2)
    view = known(hub, item)
    hub.fail["tick"] = plsremind.NotFoundError(404, "not_found", "no")
    await DetailView.tick.func(view, ctx, common.arg(item.subitems[0].id), hub)
    assert "step 1" not in discord.written(view.render())
    await DetailView.tick.func(view, ctx, common.arg(things.uid(4242)), hub)
    assert ctx.rerender.await_count == 2


async def test_a_fixed_snooze_shows_the_new_reminder() -> None:
    hub, ctx = FakeHub(), discord.click("1h")
    view = known(hub, things.item(1))
    await DetailView.snooze.func(view, ctx, hub, discord.views(hub, common.Notice))
    ctx.acknowledge.assert_awaited_once()
    assert hub.named("remind")[0]["at"] == NOW + HOUR
    assert view.upcoming == NOW + HOUR
    ctx.rerender.assert_awaited_once()

    view.upcoming = NOW + HOUR / 4
    await DetailView.snooze.func(view, discord.click("1h"), hub, discord.views(hub, common.Notice))
    assert view.upcoming == NOW + HOUR / 4


async def test_a_custom_snooze_opens_the_modal_with_the_way_back() -> None:
    hub, ctx = FakeHub(), discord.click("custom")
    item = things.item(1)
    view = known(hub, item)
    await DetailView.snooze.func(view, ctx, hub, discord.views(hub, common.Notice))
    _, custom_id = ctx.interaction.create_modal_response.await_args.args
    assert custom_id == modals.snooze_id(item.id, detail=(things.WORK, 2))
    ctx.acknowledge.assert_not_called()
    assert hub.calls == []


async def test_a_refused_snooze_redraws_first_and_then_says_why() -> None:
    hub, ctx = FakeHub(), discord.click("15m")
    view = known(hub, things.item(1))
    hub.fail["remind"] = plsremind.ConflictError(409, "reminder_limit", "no")
    order = mock.Mock()
    order.attach_mock(ctx.rerender, "rerender")
    order.attach_mock(ctx.respond, "respond")
    await DetailView.snooze.func(view, ctx, hub, discord.views(hub, common.Notice))
    assert [call[0] for call in order.mock_calls] == ["rerender", "respond"]
    hidden, said = discord.replied(ctx)
    assert hidden is True
    assert "20 anstehende Erinnerungen" in said
    assert f"https://todo.example.com/items/{things.uid(1001)}" in said
    assert view.upcoming is None


async def test_edit_opens_a_modal_filled_with_the_item() -> None:
    hub, ctx = FakeHub(), discord.click()
    item = things.item(1, title="Report", note="see wiki", due_at=NOW + HOUR)
    view = known(hub, item)
    await DetailView.edit.func(view, ctx)
    title, custom_id = ctx.interaction.create_modal_response.await_args.args
    assert title == "Aufgabe bearbeiten"
    assert custom_id == modals.edit_id(item.id, (things.WORK, 2), due=modals.due_text(item, BERLIN))
    assert len(ctx.interaction.create_modal_response.await_args.kwargs["components"]) == 3

    gone = opened(None)
    await DetailView.edit.func(gone, ctx)
    ctx.rerender.assert_awaited_once()


async def test_deleting_asks_first() -> None:
    hub, ctx = FakeHub(), discord.click()
    item = things.item(1)
    view = known(hub, item)
    await DetailView.delete.func(view, ctx)
    assert view.confirm is True
    await DetailView.cancel.func(view, ctx)
    assert view.confirm is False
    assert hub.calls == []

    await DetailView.really_delete.func(view, ctx, hub)
    assert hub.named("delete") == [{"user": MIA, "item": item.id, "origin": 555}]
    (back,) = ctx.replace.await_args.args
    assert isinstance(back, OverviewView)
    assert (back.user, back.only, back.page, back.speaks) == (common.pack(MIA), common.pack(things.WORK), 2, "de")


async def test_deleting_what_is_already_gone_still_leads_back() -> None:
    hub, ctx = FakeHub(), discord.click()
    view = known(hub, things.item(1))
    hub.fail["delete"] = plsremind.NotFoundError(404, "not_found", "no")
    await DetailView.really_delete.func(view, ctx, hub)
    ctx.replace.assert_awaited_once()


async def test_the_way_back_to_the_overview() -> None:
    ctx = discord.click()
    await DetailView.to_overview.func(opened(things.item(1)), ctx)
    (back,) = ctx.replace.await_args.args
    assert isinstance(back, OverviewView)
    assert (back.only, back.page) == (None, 0)


def test_the_id_of_the_edit_modal() -> None:
    item = things.uid(1001)
    empty = modals.seal("")
    assert modals.edit_id(item, (None, 0), due="") == f"pr:edit:{uuid.UUID(item).hex}:-:0:{empty}"
    worked = modals.edit_id(item, (things.WORK, 3), due="")
    assert worked == f"pr:edit:{uuid.UUID(item).hex}:{uuid.UUID(things.WORK).hex}:3:{empty}"
    assert len(modals.edit_id(item, (things.WORK, 999999), due="31.12.2099 23:59")) <= 100
    assert modals.seal(" 20.11.2026 ") == modals.seal("20.11.2026") != modals.seal("21.11.2026")


@pytest.mark.parametrize(
    ("item", "expected"),
    [
        (things.item(1), ""),
        (things.item(1, due_date=datetime.date(2026, 12, 4)), "04.12.2026"),
        (things.item(1, due_at=datetime.datetime(2026, 12, 4, 17, 5, tzinfo=datetime.UTC)), "04.12.2026 18:05"),
    ],
)
def test_the_deadline_is_offered_in_a_form_that_can_be_read_back(item: plsremind.Item, expected: str) -> None:
    assert modals.due_text(item, BERLIN) == expected


def test_the_edit_modal_leaves_out_a_note_too_long_for_it() -> None:
    rest = mock.Mock(spec=hikari.api.RESTClient)
    _, short = modals.edit_modal(rest, discord.say(), DE, things.item(1, note="n" * 4000), BERLIN)
    assert len(short) == 3
    _, long = modals.edit_modal(rest, discord.say(), DE, things.item(1, note="n" * 4001), BERLIN)
    assert len(long) == 2
    fields = [call.args[0] for call in rest.build_modal_action_row.return_value.add_text_input.call_args_list]
    assert fields == [modals.TITLE, modals.DUE, modals.NOTE, modals.TITLE, modals.DUE]


@pytest.mark.parametrize("view", [DetailView, OverviewView])
async def test_a_click_on_an_ephemeral_view_whose_state_cannot_be_read_is_answered(view: type[common.Owned]) -> None:
    ctx = discord.click(locale="en-US")
    await view.on_outdated(ctx, discord.say(), discord.views(FakeHub(), common.Notice))
    assert discord.replied(ctx) == (True, "This message is out of date.")
