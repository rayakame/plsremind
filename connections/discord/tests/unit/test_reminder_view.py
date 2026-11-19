from __future__ import annotations

import collections.abc
import datetime
import pathlib
import typing
from unittest import mock

import hikari
import msgspec
import plsremind
import pytest
import risa
from risa import ui

from plsremind_discord import modals
from plsremind_discord import texts
from plsremind_discord.store import Sent
from plsremind_discord.store import Store
from plsremind_discord.views import common
from plsremind_discord.views.common import Notice
from plsremind_discord.views.reminder import ReminderView
from tests.support import discord
from tests.support import things
from tests.support.fake_hub import FakeHub

NOW, BERLIN, MIA = things.NOW, things.BERLIN, things.MIA
DE = hikari.Locale.DE
HOUR = datetime.timedelta(hours=1)


def seen(item: plsremind.Item | None, *, now: datetime.datetime = NOW, list_name: str = "Inbox") -> common.Shown:
    return common.Shown(
        item=item,
        list_name=list_name,
        locale=DE,
        zone=BERLIN,
        now=now,
        public_url="https://todo.example.com",
        texts=discord.say(),
    )


def drawn(item: plsremind.Item | None, *, now: datetime.datetime = NOW) -> ui.Layout:
    view = ReminderView(user=common.pack(MIA), item=common.pack(things.uid(1001)))
    view.shown = seen(item, now=now)
    return view.render()


def test_ids_survive_the_trip_through_state_and_arguments() -> None:
    assert common.unpack(common.pack(MIA)) == MIA
    assert len(common.pack(MIA)) == 16
    assert common.from_arg(common.arg(MIA)) == MIA


@pytest.mark.parametrize(
    ("item", "expected"),
    [
        (things.item(due_at=NOW + HOUR), "upcoming"),
        (things.item(due_at=NOW - HOUR), "overdue"),
        (things.item(due_at=NOW), "overdue"),
        (things.item(), "plain"),
        (things.item(due_at=NOW - HOUR, done=True), "done"),
        (None, "gone"),
        (things.item(due_date=NOW.date()), "upcoming"),
        (things.item(due_date=NOW.date() - datetime.timedelta(days=1)), "overdue"),
    ],
)
def test_the_state_follows_the_item_as_it_is_now(item: plsremind.Item | None, expected: str) -> None:
    assert common.state_of(item, NOW) == expected


def test_the_fixed_times_to_snooze_to() -> None:
    assert common.snooze_at("15m", NOW, BERLIN) == NOW + datetime.timedelta(minutes=15)
    assert common.snooze_at("1h", NOW, BERLIN) == NOW + HOUR
    assert common.snooze_at("evening", NOW, BERLIN) == NOW.replace(hour=20, minute=0)
    assert common.snooze_at("morning", NOW, BERLIN) == (NOW + datetime.timedelta(days=1)).replace(hour=8, minute=0)
    assert common.snooze_at("custom", NOW, BERLIN) is None
    assert common.snooze_at("evening", NOW.astimezone(datetime.UTC), BERLIN) == NOW.replace(hour=20, minute=0)


@pytest.mark.parametrize(
    "now",
    [
        datetime.datetime(2026, 10, 25, 2, 50, tzinfo=BERLIN),
        datetime.datetime(2026, 10, 25, 2, 50, tzinfo=BERLIN, fold=1),
        datetime.datetime(2027, 3, 28, 1, 50, tzinfo=BERLIN),
    ],
)
def test_a_short_snooze_counts_time_that_passes_when_the_clocks_change(now: datetime.datetime) -> None:
    instant = now.astimezone(datetime.UTC)

    def snoozed(choice: str, start: datetime.datetime) -> datetime.datetime:
        at = common.snooze_at(choice, start, BERLIN)
        assert at is not None
        assert at.tzinfo is BERLIN
        # In UTC, because a time in the hour the clocks show twice equals none of another zone.
        return at.astimezone(datetime.UTC)

    assert snoozed("15m", now) == instant + datetime.timedelta(minutes=15)
    assert snoozed("1h", now) == instant + HOUR
    assert snoozed("15m", instant) == instant + datetime.timedelta(minutes=15)


def test_the_morning_after_a_clock_change_is_still_eight() -> None:
    before = datetime.datetime(2027, 3, 27, 22, 0, tzinfo=BERLIN)
    morning = common.snooze_at("morning", before, BERLIN)
    assert morning is not None
    assert morning.isoformat() == "2027-03-28T08:00:00+02:00"


def test_a_choice_whose_time_has_passed_is_not_offered() -> None:
    afternoon = [option.value for option in common.snooze_options(seen(things.item()))]
    assert afternoon == ["15m", "1h", "evening", "morning", "custom"]
    night = seen(things.item(), now=NOW.replace(hour=20, minute=30))
    assert [option.value for option in common.snooze_options(night)] == ["15m", "1h", "morning", "custom"]
    assert common.snooze_options(night)[-1].label == "Eigene Zeit ..."


def test_in_the_hour_the_clocks_show_twice_a_short_snooze_is_still_offered() -> None:
    first = datetime.datetime(2026, 10, 25, 2, 50, tzinfo=BERLIN)
    offered = [option.value for option in common.snooze_options(seen(things.item(), now=first))]
    assert offered[:2] == ["15m", "1h"]


@pytest.mark.parametrize(
    ("note", "expected"),
    [
        ("https://discord.com/channels/1/2/3", "https://discord.com/channels/1/2/3"),
        ("see wiki\nhttps://discord.com/channels/@me/22/33 and more", "https://discord.com/channels/@me/22/33"),
        ("https://canary.discord.com/channels/1/2/3", "https://canary.discord.com/channels/1/2/3"),
        ("no link, only https://example.com/channels/1/2/3", None),
        ("", None),
    ],
)
def test_a_link_to_a_discord_message_is_found_in_the_note(note: str, expected: str | None) -> None:
    assert common.message_link(note) == expected


def test_a_title_is_shown_on_one_line_cut_and_without_its_markdown() -> None:
    assert common.plain("a **b** _c_\n# d", 100) == r"a \*\*b\*\* \_c\_ \# d"
    assert common.plain("<@1> @everyone [x](y)", 100) == r"\<@1\> @everyone \[x\](y)"
    assert common.plain("x" * 301, 300) == "x" * 299 + "…"
    assert common.plain("x" * 300, 300) == "x" * 300


def test_a_link_is_left_as_it_is_so_that_it_still_leads_there() -> None:
    assert common.plain("https://example.com/a_b#frag", 300) == "https://example.com/a_b#frag"
    assert common.plain("see https://x.example/a_b and *this*", 100) == r"see https://x.example/a_b and \*this\*"
    assert common.plain("<https://x.example/a>", 100) == r"\<https://x.example/a\>"


def test_a_shortened_text_counts_as_discord_may_count() -> None:
    wide = common.plain(things.WIDE * 200, 300)
    assert texts.units(wide) <= 300
    assert wide.endswith("…")
    assert common.plain(things.WIDE * 150, 300) == things.WIDE * 150


def test_the_link_back_to_a_message_is_the_one_the_bot_put_last() -> None:
    typed = "see https://discord.com/channels/1/2/3"
    assert (
        common.message_link(f"{typed}\nhttps://discord.com/channels/@me/5/6") == "https://discord.com/channels/@me/5/6"
    )
    assert common.message_link(typed) == "https://discord.com/channels/1/2/3"


def test_a_shortened_text_keeps_its_limit_however_much_of_it_is_escaped() -> None:
    assert common.plain("*" * 500, 300) == "\\*" * 149 + "…"
    assert common.plain("*" * 150, 300) == "\\*" * 150
    assert common.plain("ab*", 4) == "ab\\*"
    assert common.plain("ab*c", 4) == "ab…"
    assert len(common.plain(things.WORST, 300)) <= 300


def test_the_list_of_a_done_item_is_escaped_like_any_other() -> None:
    view = ReminderView(user=common.pack(MIA), item=common.pack(things.uid(1001)))
    view.shown = seen(things.item(done=True), list_name="**Work**")
    assert "\\*\\*Work\\*\\*" in discord.written(view.render())


@pytest.mark.parametrize("done", [False, True])
def test_the_fullest_reminder_stays_within_what_discord_allows_of_text(*, done: bool) -> None:
    item = things.item(title=things.WORST, due_at=NOW + HOUR, priority=plsremind.Priority.HIGH, done=done)
    view = ReminderView(user=common.pack(MIA), item=common.pack(item.id))
    view.shown = seen(item, list_name=things.WORST)
    assert texts.units(discord.written(view.render())) <= 1000
    text, _, _ = common.snoozed(seen(item, list_name=things.WORST), NOW + HOUR)
    assert texts.units(text) <= 1000
    wide = msgspec.structs.replace(item, title=things.WIDE * 500)
    view.shown = seen(wide, list_name=things.WIDE * 100)
    assert texts.units(discord.written(view.render())) <= 1000


@pytest.mark.parametrize("done", [False, True])
def test_a_reminder_for_an_account_opens_with_its_mention(*, done: bool) -> None:
    view = ReminderView(user=common.pack(MIA), item=common.pack(things.uid(1001)), account=1234)
    view.shown = seen(things.item(done=done))
    assert discord.written(view.render()).startswith("<@1234>\n")


def test_a_reminder_without_an_account_mentions_nobody() -> None:
    assert "<@" not in discord.written(drawn(things.item()))


def test_an_item_ahead_shows_when_it_is_due_and_what_can_be_done() -> None:
    item = things.item(title="Tax return", due_at=NOW + HOUR, priority=plsremind.Priority.HIGH)
    layout = drawn(item)
    (container,) = discord.nodes(layout, ui.Container)
    assert container.accent_color == hikari.Color(common.UPCOMING)
    text = discord.written(layout)
    assert f"Fällig <t:{int((NOW + HOUR).timestamp())}:R>" in text
    assert "## Tax return" in text
    assert f"**<t:{int((NOW + HOUR).timestamp())}:F>** · Inbox · Hohe Priorität" in text
    (select,) = discord.nodes(layout, ui.TextSelect)
    assert select.placeholder == "Später erinnern"
    assert discord.labels(layout) == ["Erledigt", "Dashboard"]
    (link,) = discord.nodes(layout, ui.LinkButton)
    assert link.url == f"https://todo.example.com/items/{item.id}"
    assert discord.count(layout) <= 40


def test_a_note_with_a_message_link_adds_the_way_back() -> None:
    layout = drawn(things.item(note="from chat\nhttps://discord.com/channels/1/2/3"))
    assert discord.labels(layout) == ["Erledigt", "Zur Nachricht", "Dashboard"]
    assert discord.nodes(layout, ui.LinkButton)[0].url == "https://discord.com/channels/1/2/3"
    assert "Erinnerung" in discord.written(layout)


def test_an_overdue_item_is_marked() -> None:
    layout = drawn(things.item(due_at=NOW - HOUR))
    assert discord.nodes(layout, ui.Container)[0].accent_color == hikari.Color(common.OVERDUE)
    assert "Überfällig, fällig war" in discord.written(layout)


def test_an_all_day_item_shows_its_day() -> None:
    assert "Fällig am 19.11.2026" in discord.written(drawn(things.item(due_date=NOW.date())))
    late = drawn(things.item(due_date=NOW.date() - datetime.timedelta(days=1)))
    assert "Überfällig, fällig war am 18.11.2026" in discord.written(late)


def test_a_done_item_can_only_be_reopened() -> None:
    layout = drawn(things.item(title="Tax return", done=True))
    assert discord.nodes(layout, ui.Container)[0].accent_color == hikari.Color(common.DONE)
    assert "~~Tax return~~" in discord.written(layout)
    assert f"Erledigt <t:{int(NOW.timestamp())}:R>" in discord.written(layout)
    assert discord.labels(layout) == ["Rückgängig", "Dashboard"]
    assert discord.nodes(layout, ui.TextSelect) == []


def test_an_item_that_is_gone_leaves_nothing_to_click() -> None:
    layout = drawn(None)
    assert discord.written(layout) == "Diese Aufgabe gibt es nicht mehr."
    assert discord.nodes(layout, ui.Row) == []


def test_a_long_title_is_cut() -> None:
    assert "x" * 299 + "…" in discord.written(drawn(things.item(title="x" * 400)))


async def test_load_reads_the_item_and_who_it_is_for() -> None:
    hub = FakeHub()
    item = things.item(1, list_id=things.WORK)
    hub.knows(things.user(locale="en"), item)
    view = ReminderView(user=common.pack(MIA), item=common.pack(item.id))
    await discord.views(hub, ReminderView).build(view)
    assert view.shown is not None
    assert (view.shown.item, view.shown.list_name, view.shown.locale) == (item, "Work", hikari.Locale.EN_US)
    assert (view.shown.zone, view.shown.now) == (BERLIN, NOW)


async def test_a_user_who_is_out_of_reach_sees_the_item_as_gone() -> None:
    hub = FakeHub()
    view = ReminderView(user=common.pack(MIA), item=common.pack(things.uid(1001)))
    await discord.views(hub, ReminderView).build(view)
    assert view.shown is not None
    assert view.shown.item is None
    assert hub.named("item") == []


async def test_the_state_fits_into_the_message() -> None:
    hub = FakeHub()
    item = things.item(note="https://discord.com/channels/1/2/3", due_at=NOW + HOUR)
    hub.knows(things.user(), item)
    built = await discord.views(hub, ReminderView).build(ReminderView(user=common.pack(MIA), item=common.pack(item.id)))
    payloads = [builder.build()[0] for builder in built.components]
    ids = [found for found in _custom_ids(payloads) if found]
    assert ids
    assert all(len(found) <= 100 for found in ids)


def _custom_ids(payload: object) -> list[str]:
    if isinstance(payload, dict):
        document = typing.cast("dict[str, object]", payload)
        own = [str(document["custom_id"])] if "custom_id" in document else []
        return own + [found for value in document.values() for found in _custom_ids(value)]
    if isinstance(payload, list):
        return [found for value in typing.cast("list[object]", payload) for found in _custom_ids(value)]
    return []


async def test_finishing_writes_once_and_shows_the_result() -> None:
    hub = FakeHub()
    item = things.item(1)
    hub.knows(things.user(), item)
    after = await common.finish(hub, seen(item), common.pack(MIA), done=True, origin=77)
    assert after.item is not None
    assert after.item.status is plsremind.ItemStatus.DONE
    assert hub.named("set_done") == [{"user": MIA, "item": item.id, "done": True, "origin": 77}]

    again = await common.finish(hub, after, common.pack(MIA), done=True, origin=78)
    assert again is after
    assert len(hub.named("set_done")) == 1

    reopened = await common.finish(hub, after, common.pack(MIA), done=False, origin=79)
    assert reopened.item is not None
    assert reopened.item.status is plsremind.ItemStatus.OPEN


async def test_finishing_an_item_that_is_gone_writes_nothing() -> None:
    hub = FakeHub()
    assert (await common.finish(hub, seen(None), common.pack(MIA), done=True, origin=1)).item is None
    assert hub.calls == []


async def test_an_item_deleted_while_it_is_finished_shows_as_gone() -> None:
    hub = FakeHub()
    item = things.item(1)
    hub.knows(things.user(), item)
    hub.fail["set_done"] = plsremind.NotFoundError(404, "not_found", "no")
    assert (await common.finish(hub, seen(item), common.pack(MIA), done=True, origin=1)).item is None


async def test_snoozing_sets_a_reminder_at_the_chosen_time() -> None:
    hub = FakeHub()
    item = things.item(1)
    hub.knows(things.user(), item)
    at = await common.remind_later(hub, seen(item), common.pack(MIA), "1h", origin=55)
    assert at == NOW + HOUR
    assert hub.named("remind") == [{"user": MIA, "item": item.id, "at": NOW + HOUR, "origin": 55}]


@pytest.mark.parametrize(
    ("failure", "key"),
    [
        (plsremind.ConflictError(409, "reminder_limit", "no"), "error.reminder_limit"),
        (plsremind.ConflictError(409, "item_done", "no"), "reminder.item_done"),
        (plsremind.NotFoundError(404, "not_found", "no"), "reminder.gone"),
    ],
)
async def test_a_refused_snooze_says_why(failure: plsremind.APIError, key: str) -> None:
    hub = FakeHub()
    item = things.item(1)
    hub.knows(things.user(), item)
    hub.fail["remind"] = failure
    with pytest.raises(common.RefusedError) as caught:
        await common.remind_later(hub, seen(item), common.pack(MIA), "15m", origin=1)
    assert caught.value.key == key


async def test_a_snooze_to_a_time_that_has_passed_is_refused_before_the_hub_is_asked() -> None:
    hub = FakeHub()
    item = things.item(1)
    hub.knows(things.user(), item)
    late = seen(item, now=NOW.replace(hour=21))
    with pytest.raises(common.RefusedError) as caught:
        await common.remind_later(hub, late, common.pack(MIA), "evening", origin=1)
    assert caught.value.key == "error.time_past"
    with pytest.raises(common.RefusedError):
        await common.remind_later(hub, seen(None), common.pack(MIA), "15m", origin=1)
    assert hub.calls == []


async def test_any_other_refusal_of_the_hub_comes_out() -> None:
    hub = FakeHub()
    item = things.item(1)
    hub.knows(things.user(), item)
    hub.fail["remind"] = plsremind.ConflictError(409, "something_else", "no")
    with pytest.raises(plsremind.ConflictError):
        await common.remind_later(hub, seen(item), common.pack(MIA), "15m", origin=1)


async def test_only_the_user_in_the_state_may_click() -> None:
    hub = FakeHub()
    hub.knows(things.user())
    hub.knows(things.user(things.BEN, external_ids=("77",)))
    view = ReminderView(user=common.pack(MIA), item=common.pack(things.uid(1001)))

    def clicked_by(account: int) -> risa.ComponentContext:
        ctx = mock.Mock(spec=risa.ComponentContext)
        ctx.user.id = hikari.Snowflake(account)
        return typing.cast("risa.ComponentContext", ctx)

    async with discord.views(hub, ReminderView).di.enter_context(risa.Contexts.DEFAULT):
        await common.owned(risa.ExecutionPipeline(), clicked_by(1234), view)
        with pytest.raises(common.NotYoursError):
            await common.owned(risa.ExecutionPipeline(), clicked_by(77), view)
        with pytest.raises(common.NotYoursError):
            await common.owned(risa.ExecutionPipeline(), clicked_by(999), view)
        hub.ready.clear()
        with pytest.raises(common.StartingError):
            await common.owned(risa.ExecutionPipeline(), clicked_by(1234), view)


def test_the_snoozed_notice_names_the_new_time() -> None:
    item = things.item(title="Tax return")
    until = NOW + HOUR
    layout = common.notice_layout(*common.snoozed(seen(item), until))
    assert "Verschoben" in discord.written(layout)
    assert "## Tax return" in discord.written(layout)
    assert f"Ich erinnere dich wieder <t:{int(until.timestamp())}:F>." in discord.written(layout)
    assert discord.labels(layout) == ["Dashboard"]
    assert discord.nodes(layout, ui.Button) == []


async def test_a_notice_can_be_built_outside_a_click() -> None:
    client = discord.views(FakeHub(), Notice)
    built = await client.build(Notice(text="## Hello", link="https://todo.example.com/items/x", label="Dashboard"))
    payload, _ = built.components[0].build()
    assert "custom_id" not in str(payload)


def test_the_time_modal_asks_one_question() -> None:
    rest = mock.Mock(spec=hikari.api.RESTClient)
    title, rows = modals.time_modal(rest, discord.say(), DE)
    assert title == "Später erinnern"
    assert len(rows) == 1
    rest.build_modal_action_row.return_value.add_text_input.assert_called_once()
    arguments = rest.build_modal_action_row.return_value.add_text_input.call_args
    assert arguments.args == (modals.WHEN, "Wann?")
    assert arguments.kwargs["required"] is True


def test_the_id_of_the_time_modal_says_where_it_came_from() -> None:
    item = things.uid(1001)
    assert modals.snooze_id(item, detail=None) == "pr:snooze:r:" + "0193000000007000800000000000" + "03e9"
    with_list = modals.snooze_id(item, detail=(things.WORK, 12))
    assert with_list.startswith("pr:snooze:d:")
    assert with_list.endswith(":12")
    assert len(modals.snooze_id(item, detail=(things.WORK, 999999))) <= 100
    assert modals.snooze_id(item, detail=(None, 0)).endswith(":-:0")


MESSAGE = 555


@pytest.fixture
async def store(tmp_path: pathlib.Path) -> collections.abc.AsyncIterator[Store]:
    opened = await Store.open(tmp_path / "discord.db")
    try:
        yield opened
    finally:
        await opened.close()


def click(*values: str) -> mock.Mock:
    ctx = mock.Mock(spec=risa.ComponentContext)
    ctx.message.id = hikari.Snowflake(MESSAGE)
    ctx.values = list(values)
    ctx.rest = mock.Mock(spec=hikari.api.RESTClient)
    ctx.interaction = mock.Mock(spec=hikari.ComponentInteraction)
    ctx.interaction.locale = "de"
    return ctx


def loaded(hub: FakeHub, item: plsremind.Item) -> ReminderView:
    hub.knows(things.user(), item)
    view = ReminderView(user=common.pack(MIA), item=common.pack(item.id))
    view.shown = seen(item)
    return view


async def test_a_click_on_done_completes_the_item_and_redraws() -> None:
    hub, ctx = FakeHub(), click()
    view = loaded(hub, things.item(1))
    await ReminderView.done.func(view, ctx, hub)
    assert hub.named("set_done") == [{"user": MIA, "item": things.uid(1001), "done": True, "origin": MESSAGE}]
    assert discord.labels(view.render()) == ["Rückgängig", "Dashboard"]
    ctx.rerender.assert_awaited_once()

    await ReminderView.undo.func(view, ctx, hub)
    assert hub.named("set_done")[-1]["done"] is False
    assert discord.labels(view.render()) == ["Erledigt", "Dashboard"]


async def test_a_fixed_snooze_ends_the_message_and_stops_following_it(store: Store) -> None:
    hub, ctx = FakeHub(), click("1h")
    item = things.item(1, title="Tax return")
    view = loaded(hub, item)
    await store.record(
        delivery_id="d1", user_id=MIA, item_id=item.id, item_updated_at=None, messages=[Sent(MESSAGE, 7, 1234)], now=NOW
    )

    await ReminderView.snooze.func(view, ctx, hub, store, discord.views(hub, common.Notice))

    ctx.acknowledge.assert_awaited_once()
    assert hub.named("remind") == [{"user": MIA, "item": item.id, "at": NOW + HOUR, "origin": MESSAGE}]
    (layout,) = ctx.edit.await_args.args
    assert "Verschoben" in discord.written(layout)
    assert discord.nodes(layout, ui.Button) == []
    assert await store.messages_for_item(item.id) == []
    ctx.interaction.create_modal_response.assert_not_called()


async def test_a_custom_snooze_opens_the_modal_and_answers_nothing_else(store: Store) -> None:
    hub, ctx = FakeHub(), click("custom")
    view = loaded(hub, things.item(1))
    await ReminderView.snooze.func(view, ctx, hub, store, discord.views(hub, common.Notice))
    title, custom_id = ctx.interaction.create_modal_response.await_args.args
    assert title == "Später erinnern"
    assert custom_id == modals.snooze_id(things.uid(1001), detail=None)
    assert len(ctx.interaction.create_modal_response.await_args.kwargs["components"]) == 1
    ctx.acknowledge.assert_not_called()
    ctx.edit.assert_not_called()
    assert hub.calls == []


async def test_a_refused_snooze_tells_the_user_and_keeps_the_message(store: Store) -> None:
    hub, ctx = FakeHub(), click("15m")
    item = things.item(1)
    view = loaded(hub, item)
    hub.fail["remind"] = plsremind.ConflictError(409, "reminder_limit", "no")
    await store.record(
        delivery_id="d1", user_id=MIA, item_id=item.id, item_updated_at=None, messages=[Sent(MESSAGE, 7, 1234)], now=NOW
    )

    await ReminderView.snooze.func(view, ctx, hub, store, discord.views(hub, common.Notice))

    hidden, said = discord.replied(ctx)
    assert hidden is True
    assert said.startswith("Diese Aufgabe hat schon 20")
    assert f"https://todo.example.com/items/{item.id}" in said
    ctx.rerender.assert_awaited_once()
    ctx.edit.assert_not_called()
    assert len(await store.messages_for_item(item.id)) == 1


async def test_a_click_on_a_message_whose_state_cannot_be_read_is_answered() -> None:
    ctx = click()
    await ReminderView.on_outdated(
        typing.cast("risa.ComponentContext", ctx), discord.say(), discord.views(FakeHub(), common.Notice)
    )
    assert discord.replied(ctx) == (True, "Diese Nachricht ist veraltet.")


def test_drawing_before_loading_is_a_mistake_that_shows() -> None:
    view = ReminderView(user=common.pack(MIA), item=common.pack(things.uid(1001)))
    with pytest.raises(RuntimeError, match="loaded"):
        view.render()


def test_a_list_without_a_name_is_left_out() -> None:
    view = ReminderView(user=common.pack(MIA), item=common.pack(things.uid(1001)))
    view.shown = seen(things.item(priority=plsremind.Priority.LOW), list_name="")
    assert discord.written(view.render()).endswith("\nNiedrige Priorität")


def test_a_notice_without_a_link_is_only_text() -> None:
    layout = common.notice_layout("gone", "", "")
    assert discord.written(layout) == "gone"
    assert discord.nodes(layout, ui.Row) == []
