from __future__ import annotations

import datetime
import uuid

import hikari
import plsremind
import pytest
from risa import ui

from plsremind_discord.views import common
from plsremind_discord.views import overview
from plsremind_discord.views.detail import DetailView
from plsremind_discord.views.overview import OverviewView
from tests.support import discord
from tests.support import things
from tests.support.fake_hub import FakeHub

NOW, BERLIN, MIA = things.NOW, things.BERLIN, things.MIA
HOUR = datetime.timedelta(hours=1)
DE = hikari.Locale.DE
LISTS = (things.a_list(), things.a_list(things.WORK, "Work", 1))


def listed(*items: plsremind.Item, lists: tuple[plsremind.List, ...] = LISTS) -> overview.Listed:
    return overview.Listed(
        items=items,
        lists=lists,
        locale=DE,
        zone=BERLIN,
        now=NOW,
        public_url="https://todo.example.com",
        texts=discord.say(),
    )


def drawn(*items: plsremind.Item, only: plsremind.ID | None = None, page: int = 0) -> ui.Layout:
    view = OverviewView(user=common.pack(MIA), only=common.pack(only) if only is not None else None, page=page)
    view.shown = listed(*items)
    return view.render()


def test_the_order_of_the_overview() -> None:
    tomorrow = (NOW + datetime.timedelta(days=1)).replace(hour=10, minute=0)
    items = [
        things.item(1, title="plain"),
        things.item(2, title="soon", due_at=tomorrow),
        things.item(3, title="later today", due_at=NOW + 2 * HOUR),
        things.item(4, title="late", due_at=NOW - HOUR),
        things.item(5, title="reminded", reminders=(NOW + HOUR,)),
        things.item(6, title="reminded once", reminders=(NOW - HOUR,)),
        things.item(7, title="all day", due_date=NOW.date()),
        things.item(8, title="urgent", due_at=NOW + 2 * HOUR, priority=plsremind.Priority.HIGH),
        things.item(9, title="Also later today", due_at=NOW + 2 * HOUR),
        things.item(10, title="a plain one", priority=plsremind.Priority.LOW),
    ]
    found = overview.lines(items, NOW, BERLIN)
    assert [(line.item.title, line.group) for line in found] == [
        ("late", "overdue"),
        ("reminded", "today"),
        ("urgent", "today"),
        ("Also later today", "today"),
        ("later today", "today"),
        ("all day", "today"),
        ("soon", "soon"),
        ("plain", "none"),
        ("reminded once", "none"),
        ("a plain one", "none"),
    ]
    assert found[1].when == NOW + HOUR
    assert found[8].when is None


def test_a_day_ends_at_midnight_where_the_user_lives() -> None:
    night = NOW.replace(hour=23, minute=30)
    item = things.item(1, due_at=night + HOUR)
    assert overview.lines([item], night, BERLIN)[0].group == "soon"
    assert overview.lines([item], night.astimezone(datetime.UTC), BERLIN)[0].group == "soon"


def test_eight_items_fit_on_a_page() -> None:
    items = [things.item(number, due_at=NOW + number * HOUR) for number in range(1, 10)]
    first = drawn(*items)
    assert len(discord.nodes(first, ui.Section)) == 8
    back, forward = discord.nodes(first, ui.Row)[-1].components[:2]
    assert isinstance(back, ui.Button)
    assert isinstance(forward, ui.Button)
    assert (back.disabled, forward.disabled) == (True, False)
    assert "9 offen · 0 überfällig" in discord.written(first)

    second = drawn(*items, page=1)
    assert len(discord.nodes(second, ui.Section)) == 1
    assert "item 9" in discord.written(second)
    back, forward = discord.nodes(second, ui.Row)[-1].components[:2]
    assert isinstance(back, ui.Button)
    assert isinstance(forward, ui.Button)
    assert (back.disabled, forward.disabled) == (False, True)

    assert discord.written(drawn(*items, page=7)) == discord.written(second)


def test_a_row_says_what_matters_about_its_item() -> None:
    item = things.item(
        1, title="Report", due_at=NOW + 2 * HOUR, list_id=things.WORK, priority=plsremind.Priority.HIGH, subitems=5
    )
    text = discord.written(drawn(item, things.item(2, reminders=(NOW + HOUR,)), things.item(3, due_date=NOW.date())))
    assert "-# HEUTE" in text
    assert f"**Report**\n-# <t:{int((NOW + 2 * HOUR).timestamp())}:t> · Work · Hohe Priorität · 0 von 5" in text
    assert f"-# Erinnerung <t:{int((NOW + HOUR).timestamp())}:t> · Inbox" in text
    assert "-# 19.11.2026 · Inbox" in text


def test_every_row_opens_its_own_item() -> None:
    items = [things.item(1), things.item(2)]
    layout = drawn(*items)
    buttons = [section.accessory for section in discord.nodes(layout, ui.Section)]
    assert [button.label for button in buttons if isinstance(button, ui.Button)] == ["Öffnen", "Öffnen"]
    carried = sorted(discord.bound(button) for button in buttons if isinstance(button, ui.Button))
    assert carried == sorted(common.arg(item.id) for item in items)


def test_the_filter_leaves_one_list() -> None:
    layout = drawn(things.item(1), things.item(2, list_id=things.WORK), only=things.WORK)
    assert len(discord.nodes(layout, ui.Section)) == 1
    assert "item 2" in discord.written(layout)
    (select,) = discord.nodes(layout, ui.TextSelect)
    assert [(option.label, option.default) for option in select.options] == [
        ("Alle Listen", False),
        ("Inbox", False),
        ("Work", True),
    ]
    assert select.options[2].value == uuid.UUID(things.WORK).hex
    (link,) = discord.nodes(layout, ui.LinkButton)
    assert link.url == f"https://todo.example.com/lists/{things.WORK}"


def test_nothing_open_is_said_in_words() -> None:
    layout = drawn()
    assert "Nichts offen." in discord.written(layout)
    assert discord.nodes(layout, ui.Section) == []
    assert discord.nodes(layout, ui.LinkButton)[0].url == "https://todo.example.com"


def test_no_more_lists_are_offered_than_a_menu_holds() -> None:
    many = tuple(things.a_list(things.uid(200 + number), f"List {number}", number) for number in range(30))
    view = OverviewView(user=common.pack(MIA))
    view.shown = listed(lists=many)
    (select,) = discord.nodes(view.render(), ui.TextSelect)
    assert len(select.options) == 25


def test_the_fullest_page_stays_within_what_discord_allows() -> None:
    tomorrow = NOW + datetime.timedelta(days=1)
    items = [
        things.item(1, due_at=NOW - HOUR),
        things.item(2, due_at=NOW - 2 * HOUR),
        things.item(3, due_at=NOW + HOUR),
        things.item(4, due_at=NOW + 2 * HOUR),
        things.item(5, due_at=tomorrow),
        things.item(6, due_at=tomorrow + HOUR),
        things.item(7),
        things.item(8),
        things.item(9),
    ]
    assert discord.count(drawn(*items)) <= 40


async def test_the_overview_reads_everything_open_and_fits_into_the_message() -> None:
    hub = FakeHub()
    items = [things.item(number) for number in range(1, 10)]
    hub.knows(things.user(locale=None), *items, things.item(20, done=True))
    view = OverviewView(user=common.pack(MIA), only=common.pack(things.WORK), page=3, speaks="de")
    built = await discord.views(hub, OverviewView, DetailView).build(view)
    assert built.components
    assert view.shown is not None
    assert len(view.shown.items) == 9
    assert (view.shown.locale, view.shown.zone, view.shown.now) == (DE, BERLIN, NOW)
    assert view.shown.lists == hub.lists(MIA)


async def test_choosing_a_list_starts_at_its_first_page() -> None:
    ctx = discord.click(uuid.UUID(things.WORK).hex)
    view = OverviewView(user=common.pack(MIA), page=4)
    await OverviewView.choose.func(view, ctx)
    assert (view.only, view.page) == (common.pack(things.WORK), 0)
    ctx.rerender.assert_awaited_once()

    await OverviewView.choose.func(view, discord.click("all"))
    assert view.only is None


async def test_paging_goes_back_and_forth_and_never_before_the_start() -> None:
    ctx = discord.click()
    view = OverviewView(user=common.pack(MIA))
    await OverviewView.back.func(view, ctx)
    assert view.page == 0
    await OverviewView.forward.func(view, ctx)
    await OverviewView.forward.func(view, ctx)
    assert view.page == 2
    await OverviewView.back.func(view, ctx)
    assert view.page == 1
    assert ctx.rerender.await_count == 4


async def test_opening_a_row_swaps_in_the_detail_view_and_remembers_the_way_back() -> None:
    ctx = discord.click()
    item = things.item(3)
    view = OverviewView(user=common.pack(MIA), only=common.pack(things.WORK), page=2)
    await OverviewView.open.func(view, ctx, common.arg(item.id))
    (opened,) = ctx.replace.await_args.args
    assert isinstance(opened, DetailView)
    assert (opened.user, opened.item) == (common.pack(MIA), common.pack(item.id))
    assert (opened.back_list, opened.back_page, opened.speaks) == (common.pack(things.WORK), 2, "de")


def test_drawing_the_overview_before_loading_is_a_mistake_that_shows() -> None:
    with pytest.raises(RuntimeError, match="loaded"):
        OverviewView(user=common.pack(MIA)).render()


def test_an_item_in_a_list_nobody_knows_is_shown_without_one() -> None:
    text = discord.written(drawn(things.item(1, list_id=things.uid(999), due_date=NOW.date())))
    assert "**item 1**\n-# 19.11.2026" in text
    assert "19.11.2026 ·" not in text
