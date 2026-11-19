from __future__ import annotations

import asyncio
import collections.abc
import datetime
import typing
from unittest import mock

import hikari
import lightbulb
import plsremind
import pytest

from plsremind_discord import modals
from plsremind_discord import patience
from plsremind_discord import texts
from plsremind_discord.commands import common
from plsremind_discord.commands import deadline
from plsremind_discord.commands import done
from plsremind_discord.commands import every
from plsremind_discord.commands import message
from plsremind_discord.commands import remind
from plsremind_discord.commands import todos
from plsremind_discord.views.common import Notice
from plsremind_discord.views.detail import DetailView
from plsremind_discord.views.overview import OverviewView
from tests.support import discord
from tests.support import things
from tests.support.fake_hub import FakeHub

NOW, MIA, BERLIN = things.NOW, things.MIA, things.BERLIN
HOUR = datetime.timedelta(hours=1)
DE, EN = hikari.Locale.DE, hikari.Locale.EN_US
CLOCK = discord.FixedClock()


@pytest.fixture
def hub() -> FakeHub:
    made = FakeHub()
    made.knows(
        things.user(),
        things.item(1, title="Report", due_at=NOW + 3 * HOUR, list_id=things.WORK),
        things.item(2, title="Call Ben"),
        things.item(3, title="Reply to Ben", due_at=NOW + HOUR),
        things.item(4, title="Old", done=True),
    )
    return made


def the(hub: FakeHub) -> common.Tools:
    return common.Tools(
        hub=hub,
        texts=discord.say(),
        clock=CLOCK,
        config=discord.CONFIG,
        views=discord.views(hub, Notice, OverviewView, DetailView),
    )


def test_items_are_suggested_by_part_of_their_title_soonest_first(hub: FakeHub) -> None:
    items = hub.open_items(MIA)
    assert common.choices_for_items(items, "", NOW, BERLIN) == [
        ("Reply to Ben", things.uid(1003)),
        ("Report", things.uid(1001)),
        ("Call Ben", things.uid(1002)),
    ]
    assert [name for name, _ in common.choices_for_items(items, "bEn", NOW, BERLIN)] == ["Reply to Ben", "Call Ben"]
    assert common.choices_for_items(items, "nothing", NOW, BERLIN) == []


def test_at_most_twenty_five_suggestions_with_labels_that_fit() -> None:
    many = [things.item(number, title=f"{number} " + "x" * 200) for number in range(1, 40)]
    found = common.choices_for_items(many, "", NOW, BERLIN)
    assert len(found) == 25
    assert all(len(name) <= 100 for name, _ in found)
    lists = tuple(things.a_list(things.uid(200 + number), "L" * 150, number) for number in range(30))
    offered = common.choices_for_lists(lists, "l")
    assert len(offered) == 25
    assert all(len(name) <= 100 for name, _ in offered)


def test_lists_are_suggested_in_their_order(hub: FakeHub) -> None:
    lists = hub.lists(MIA)
    assert common.choices_for_lists(lists, "") == [("Inbox", things.INBOX), ("Work", things.WORK)]
    assert common.choices_for_lists(lists, "wo") == [("Work", things.WORK)]


def test_an_item_is_picked_by_its_id_or_by_a_title_only_it_has(hub: FakeHub) -> None:
    items = (*hub.open_items(MIA), things.item(9, title="call ben"))
    assert common.pick_item(things.uid(1001), items) == items[0]
    assert common.pick_item(" report ", items) == items[0]
    assert common.pick_item("Call Ben", items) is None
    assert common.pick_item("nothing", items) is None
    lists = hub.lists(MIA)
    assert common.pick_list(things.WORK, lists) == lists[1]
    assert common.pick_list("work", lists) == lists[1]
    assert common.pick_list("nothing", lists) is None


async def test_remind_makes_an_item_and_a_reminder_and_says_so_in_private(hub: FakeHub) -> None:
    ctx = discord.command()
    await remind.remind(ctx, the(hub), when="2h", what=" Laundry ", note="the white one", public=False)
    (added,) = hub.named("add")
    assert (added["user"], added["title"], added["note"]) == (MIA, "Laundry", "the white one")
    assert added["due_at"] is None
    assert hub.named("remind")[0]["at"] == NOW + 2 * HOUR
    hidden, text = discord.replied(ctx)
    assert hidden is True
    assert f"Ich erinnere dich <t:{int((NOW + 2 * HOUR).timestamp())}:F>: **Laundry**" in text
    assert "https://todo.example.com/items/" in text


async def test_a_day_without_a_time_reminds_at_nine(hub: FakeHub) -> None:
    await remind.remind(discord.command(), the(hub), when="morgen", what="x", note=None, public=False)
    assert hub.named("remind")[0]["at"] == (NOW + datetime.timedelta(days=1)).replace(hour=9, minute=0)

    ctx = discord.command()
    await remind.remind(ctx, the(hub), when="heute", what="y", note=None, public=False)
    assert "Diese Zeit ist schon vorbei" in discord.replied(ctx)[1]
    assert len(hub.named("add")) == 1


async def test_a_public_confirmation_pings_nobody_and_keeps_the_note_private(hub: FakeHub) -> None:
    ctx = discord.command()
    await remind.remind(ctx, the(hub), when="2h", what="@everyone <@&5> <@7> party", note="secret", public=True)
    hidden, text = discord.replied(ctx)
    assert hidden is False
    assert "party" in text
    assert "secret" not in text
    assert "todo.example.com" not in text
    sent = ctx.respond.await_args.kwargs
    assert (sent["mentions_everyone"], sent["user_mentions"], sent["role_mentions"]) == (False, False, False)


async def test_a_reminder_that_fails_takes_the_item_back_and_says_so(hub: FakeHub) -> None:
    hub.fail["remind"] = plsremind.ServerError(500, "boom", "no")
    ctx = discord.command()
    await remind.remind(ctx, the(hub), when="2h", what="x", note=None, public=True)
    assert len(hub.named("delete")) == 1
    hidden, text = discord.replied(ctx)
    assert hidden is True
    assert "Das hat nicht geklappt" in text


async def test_a_time_nobody_can_read_creates_nothing(hub: FakeHub) -> None:
    ctx = discord.command()
    await remind.remind(ctx, the(hub), when="bald", what="x", note=None, public=False)
    assert hub.calls == []
    assert "Diese Zeit habe ich nicht verstanden" in discord.replied(ctx)[1]


async def test_a_deadline_on_a_day_is_all_day_and_one_with_a_time_is_not(hub: FakeHub) -> None:
    ctx = discord.command()
    await deadline.deadline(ctx, the(hub), due="fr", what="Taxes", note=None, priority=None, list_=None, public=False)
    first = hub.named("add")[0]
    assert (first["due_date"], first["due_at"]) == (datetime.date(2026, 11, 20), None)
    assert "Frist gesetzt für 20.11.2026: **Taxes**" in discord.replied(ctx)[1]

    ctx = discord.command()
    await deadline.deadline(
        ctx, the(hub), due="fr 18:00", what="Report", note="n", priority="high", list_=things.WORK, public=False
    )
    second = hub.named("add")[1]
    at = NOW.replace(day=20, hour=18, minute=0)
    assert (second["due_at"], second["due_date"]) == (at, None)
    assert (second["priority"], second["list_id"], second["note"]) == (plsremind.Priority.HIGH, things.WORK, "n")
    assert f"Frist gesetzt für <t:{int(at.timestamp())}:F>: **Report**" in discord.replied(ctx)[1]
    assert hub.named("remind") == []


async def test_a_deadline_in_a_list_nobody_knows_creates_nothing(hub: FakeHub) -> None:
    ctx = discord.command()
    await deadline.deadline(ctx, the(hub), due="fr", what="x", note=None, priority=None, list_="nope", public=False)
    assert hub.calls == []
    assert "Diese Liste kenne ich nicht" in discord.replied(ctx)[1]


async def test_todos_answers_with_the_overview_in_private(hub: FakeHub) -> None:
    ctx = discord.command()
    await todos.todos(ctx, the(hub), list_=None)
    hidden, text = discord.replied(ctx)
    assert hidden is True
    assert "## Deine Aufgaben" in text
    assert "3 offen" in text

    ctx = discord.command()
    await todos.todos(ctx, the(hub), list_="work")
    assert "1 offen" in discord.replied(ctx)[1]

    ctx = discord.command()
    await todos.todos(ctx, the(hub), list_="nope")
    assert "Diese Liste kenne ich nicht" in discord.replied(ctx)[1]


async def test_done_completes_and_shows_the_item_with_a_way_back(hub: FakeHub) -> None:
    ctx = discord.command()
    await done.done(ctx, the(hub), item=things.uid(1002))
    assert hub.named("set_done") == [{"user": MIA, "item": things.uid(1002), "done": True, "origin": None}]
    hidden, text = discord.replied(ctx)
    assert hidden is True
    assert "~~Call Ben~~" in text
    assert "Rückgängig" in text

    ctx = discord.command()
    await done.done(ctx, the(hub), item="nothing like it")
    assert "Diese Aufgabe kenne ich nicht" in discord.replied(ctx)[1]
    assert len(hub.named("set_done")) == 1


@pytest.mark.parametrize(
    ("content", "title"),
    [
        ("Can you send me the report?\nBefore Friday please.", "Can you send me the report?"),
        ("x" * 300, "x" * 100),
        ("", "Nachricht von Ben"),
        ("   \n  ", "Nachricht von Ben"),
    ],
)
async def test_remind_me_opens_the_modal_with_the_start_of_the_message(hub: FakeHub, content: str, title: str) -> None:
    ctx = discord.command(guild=9)
    rest = mock.Mock(spec=hikari.api.RESTClient)
    target = mock.Mock(spec=hikari.Message)
    target.content = content
    target.id, target.channel_id = hikari.Snowflake(3), hikari.Snowflake(2)
    target.author.display_name = "Ben"
    await message.remind_me(ctx, the(hub), rest, target=target)
    heading, custom_id = ctx.respond_with_modal.await_args.args
    assert heading == "Erinnere mich"
    assert custom_id == modals.message_id(guild=9, channel=2, message=3)
    first = rest.build_modal_action_row.return_value.add_text_input.call_args_list[0]
    assert first.kwargs["value"] == title
    ctx.respond.assert_not_called()


type Call = collections.abc.Callable[[mock.Mock, common.Tools], collections.abc.Awaitable[None]]


async def _remind(ctx: mock.Mock, tools: common.Tools) -> None:
    await remind.remind(ctx, tools, when="2h", what="x", note=None, public=True)


async def _deadline(ctx: mock.Mock, tools: common.Tools) -> None:
    await deadline.deadline(ctx, tools, due="fr", what="x", note=None, priority=None, list_=None, public=True)


async def _todos(ctx: mock.Mock, tools: common.Tools) -> None:
    await todos.todos(ctx, tools, list_=None)


async def _done(ctx: mock.Mock, tools: common.Tools) -> None:
    await done.done(ctx, tools, item=things.uid(1001))


async def _remind_me(ctx: mock.Mock, tools: common.Tools) -> None:
    rest = typing.cast("hikari.api.RESTClient", mock.Mock(spec=hikari.api.RESTClient))
    await message.remind_me(ctx, tools, rest, target=mock.Mock(spec=hikari.Message))


EVERY: tuple[Call, ...] = (_remind, _deadline, _todos, _done, _remind_me)


@pytest.mark.parametrize("call", EVERY)
async def test_every_command_tells_someone_who_is_not_linked_how_to_link(hub: FakeHub, call: Call) -> None:
    ctx = discord.command(user=999, locale="en-US")
    await call(ctx, the(hub))
    hidden, text = discord.replied(ctx)
    assert hidden is True
    assert "is not linked to plsremind" in text
    assert "https://todo.example.com/settings" in text
    assert hub.calls == []


@pytest.mark.parametrize("call", EVERY)
async def test_every_command_says_so_while_the_hub_is_not_there_yet(hub: FakeHub, call: Call) -> None:
    hub.ready.clear()
    ctx = discord.command(locale="en-US")
    await call(ctx, the(hub))
    hidden, text = discord.replied(ctx)
    assert hidden is True
    assert "The bot is just starting" in text
    assert hub.calls == []


async def test_a_deadline_nobody_can_read_creates_nothing(hub: FakeHub) -> None:
    ctx = discord.command()
    await deadline.deadline(ctx, the(hub), due="bald", what="x", note=None, priority=None, list_=None, public=True)
    hidden, text = discord.replied(ctx)
    assert hidden is True
    assert "Diese Zeit habe ich nicht verstanden" in text
    assert hub.calls == []


async def test_a_deadline_can_be_confirmed_for_everyone_to_see(hub: FakeHub) -> None:
    ctx = discord.command()
    await deadline.deadline(
        ctx, the(hub), due="fr", what="Taxes", note="secret", priority=None, list_=None, public=True
    )
    hidden, text = discord.replied(ctx)
    assert hidden is False
    assert "**Taxes**" in text
    assert "secret" not in text
    assert "todo.example.com" not in text


async def test_what_the_hub_refuses_is_said(hub: FakeHub) -> None:
    hub.fail["set_done"] = plsremind.NetworkError("no route")
    ctx = discord.command()
    await done.done(ctx, the(hub), item=things.uid(1001))
    assert "Das hat nicht geklappt" in discord.replied(ctx)[1]

    hub.fail["add"] = plsremind.BadRequestError(422, "validation_failed", "no", {"title": "too long"})
    ctx = discord.command()
    await deadline.deadline(ctx, the(hub), due="fr", what="x", note=None, priority=None, list_=None, public=False)
    assert "Der Hub hat das nicht angenommen" in discord.replied(ctx)[1]


async def test_each_command_hands_its_options_on(hub: FakeHub) -> None:
    tools = the(hub)
    ctx = discord.command()
    await remind.Remind.invoke(mock.Mock(when="2h", what="Laundry", note=None, public_ack=False), ctx, tools)
    assert hub.named("add")[0]["title"] == "Laundry"

    options = mock.Mock(due="fr", what="Taxes", note=None, priority="low", list=None, public_ack=False)
    await deadline.Deadline.invoke(options, discord.command(), tools)
    assert hub.named("add")[1]["priority"] is plsremind.Priority.LOW

    ctx = discord.command()
    await todos.Todos.invoke(mock.Mock(list="work"), ctx, tools)
    assert "1 offen" in discord.replied(ctx)[1]

    await done.Done.invoke(mock.Mock(item=things.uid(1002)), discord.command(), tools)
    assert hub.named("set_done")[0]["item"] == things.uid(1002)

    ctx = discord.command()
    target = mock.Mock(spec=hikari.Message)
    target.content = "hello"
    target.id, target.channel_id = hikari.Snowflake(3), hikari.Snowflake(2)
    rest = typing.cast("hikari.api.RESTClient", mock.Mock(spec=hikari.api.RESTClient))
    await message.RemindMe.invoke(mock.Mock(target=target), ctx, tools, rest)
    ctx.respond_with_modal.assert_awaited_once()


async def test_suggestions_come_from_the_callers_own_items_and_lists(hub: FakeHub) -> None:
    def typing_of(text: str, user: int = 1234) -> mock.Mock:
        ctx = mock.Mock(spec=lightbulb.AutocompleteContext)
        ctx.interaction.user.id = hikari.Snowflake(user)
        ctx.focused.value = text
        return ctx

    ctx = typing_of("rep")
    await common.complete_item(ctx, hub, CLOCK)
    ctx.respond.assert_awaited_once_with([("Reply to Ben", things.uid(1003)), ("Report", things.uid(1001))])
    ctx = typing_of("")
    await common.complete_list(ctx, hub)
    ctx.respond.assert_awaited_once_with([("Inbox", things.INBOX), ("Work", things.WORK)])
    stranger = typing_of("", user=999)
    await common.complete_item(stranger, hub, CLOCK)
    stranger.respond.assert_awaited_once_with([])
    stranger = typing_of("", user=999)
    await common.complete_list(stranger, hub)
    stranger.respond.assert_awaited_once_with([])


async def test_every_command_is_announced_in_both_languages_for_both_kinds_of_install() -> None:
    provider = texts.provider()
    assert len(every.LOADERS) == 5
    for kind in (remind.Remind, deadline.Deadline, todos.Todos, done.Done, message.RemindMe):
        builder = await kind.as_command_builder(EN, provider)
        assert builder.name_localizations[DE]
        assert set(builder.integration_types or ()) == set(common.INSTALLS)
        assert set(builder.context_types or ()) == set(common.CONTEXTS)
    built = typing.cast("hikari.api.SlashCommandBuilder", await remind.Remind.as_command_builder(EN, provider))
    assert built.name == "remind"
    assert [option.name for option in built.options] == ["when", "what", "note", "public_ack"]
    assert [option.is_required for option in built.options] == [True, True, False, False]
    assert built.options[0].description_localizations[DE].startswith("Wann")
    deadlines = typing.cast("hikari.api.SlashCommandBuilder", await deadline.Deadline.as_command_builder(EN, provider))
    assert [option.name for option in deadlines.options] == ["due", "what", "note", "priority", "list", "public_ack"]
    assert [choice.value for choice in deadlines.options[3].choices or ()] == ["low", "normal", "high"]
    assert deadlines.options[4].autocomplete is True
    assert built.options[0].max_length == deadlines.options[0].max_length == modals.WHEN_LIMIT
    assert (await message.RemindMe.as_command_builder(EN, provider)).name == "Remind me"


@pytest.fixture
def impatient(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr(patience, "PATIENCE", 0.01)


async def test_a_private_confirmation_names_the_list(hub: FakeHub) -> None:
    ctx = discord.command()
    await deadline.deadline(ctx, the(hub), due="fr", what="Taxes", note=None, priority=None, list_="Work", public=False)
    assert "-# Work" in discord.replied(ctx)[1]

    ctx = discord.command()
    await remind.remind(ctx, the(hub), when="2h", what="Laundry", note=None, public=False)
    assert "-# Inbox" in discord.replied(ctx)[1]

    ctx = discord.command()
    await remind.remind(ctx, the(hub), when="2h", what="Laundry", note=None, public=True)
    assert "Inbox" not in discord.replied(ctx)[1]

    hub.their_lists[MIA] = ()
    ctx = discord.command()
    await remind.remind(ctx, the(hub), when="2h", what="Laundry", note=None, public=False)
    assert "-#" not in discord.replied(ctx)[1]


async def test_in_a_dm_with_the_bot_a_public_confirmation_is_the_private_one(hub: FakeHub) -> None:
    alone = hikari.ApplicationContextType.BOT_DM
    ctx = discord.command(where=alone)
    await remind.remind(ctx, the(hub), when="2h", what="Laundry", note=None, public=True)
    hidden, text = discord.replied(ctx)
    assert hidden is True
    assert "todo.example.com/items/" in text

    ctx = discord.command(where=alone)
    await deadline.deadline(ctx, the(hub), due="fr", what="Taxes", note=None, priority=None, list_=None, public=True)
    hidden, text = discord.replied(ctx)
    assert hidden is True
    assert "todo.example.com/items/" in text


async def test_between_two_people_a_public_confirmation_stays_public(hub: FakeHub) -> None:
    ctx = discord.command(where=hikari.ApplicationContextType.PRIVATE_CHANNEL)
    await remind.remind(ctx, the(hub), when="2h", what="Laundry", note=None, public=True)
    assert discord.replied(ctx)[0] is False


def answers(ctx: mock.Mock) -> list[tuple[bool, str]]:
    """Every answer a command gave, in order: whether only the user sees it, and what it says."""
    found: list[tuple[bool, str]] = []
    for call in ctx.respond.await_args_list:
        built = [builder.build()[0] for builder in call.kwargs.get("components", [])]
        found.append((call.kwargs["ephemeral"], "\n".join(discord.payload_texts(built))))
    return found


@pytest.mark.usefixtures("impatient")
async def test_a_hub_that_takes_its_time_makes_remind_ask_discord_to_wait(
    hub: FakeHub, monkeypatch: pytest.MonkeyPatch
) -> None:
    go = discord.slow(monkeypatch, hub, "add")
    ctx = discord.command()
    running = asyncio.create_task(remind.remind(ctx, the(hub), when="2h", what="Laundry", note=None, public=False))
    await discord.until(lambda: ctx.defer.await_count == 1)
    ctx.defer.assert_awaited_once_with(ephemeral=True)
    ctx.respond.assert_not_called()
    go.set()
    await running
    ((hidden, text),) = answers(ctx)
    assert hidden is True
    assert "**Laundry**" in text


@pytest.mark.usefixtures("impatient")
async def test_a_slow_public_confirmation_resolves_the_private_wait_first_and_then_goes_to_the_channel(
    hub: FakeHub, monkeypatch: pytest.MonkeyPatch
) -> None:
    go = discord.slow(monkeypatch, hub, "add")
    ctx = discord.command()
    running = asyncio.create_task(remind.remind(ctx, the(hub), when="2h", what="Laundry", note=None, public=True))
    await discord.until(lambda: ctx.defer.await_count == 1)
    ctx.defer.assert_awaited_once_with(ephemeral=True)
    go.set()
    await running
    (private, private_text), (public, public_text) = answers(ctx)
    assert (private, public) == (True, False)
    assert "todo.example.com/items/" in private_text
    assert "**Laundry**" in public_text
    assert "todo.example.com" not in public_text
    sent = ctx.respond.await_args.kwargs
    assert (sent["mentions_everyone"], sent["user_mentions"], sent["role_mentions"]) == (False, False, False)


@pytest.mark.usefixtures("impatient")
async def test_what_goes_wrong_after_a_public_command_waited_is_said_in_private(
    hub: FakeHub, monkeypatch: pytest.MonkeyPatch
) -> None:
    hub.fail["add"] = plsremind.ServerError(500, "boom", "no")
    go = discord.slow(monkeypatch, hub, "add")
    ctx = discord.command()
    running = asyncio.create_task(
        deadline.deadline(ctx, the(hub), due="fr", what="Taxes", note=None, priority=None, list_=None, public=True)
    )
    await discord.until(lambda: ctx.defer.await_count == 1)
    ctx.defer.assert_awaited_once_with(ephemeral=True)
    go.set()
    await running
    ((hidden, text),) = answers(ctx)
    assert hidden is True
    assert "Das hat nicht geklappt" in text


@pytest.mark.usefixtures("impatient")
async def test_a_hub_that_takes_its_time_makes_deadline_ask_discord_to_wait(
    hub: FakeHub, monkeypatch: pytest.MonkeyPatch
) -> None:
    go = discord.slow(monkeypatch, hub, "add")
    ctx = discord.command()
    running = asyncio.create_task(
        deadline.deadline(ctx, the(hub), due="fr", what="Taxes", note=None, priority=None, list_=None, public=False)
    )
    await discord.until(lambda: ctx.defer.await_count == 1)
    ctx.defer.assert_awaited_once_with(ephemeral=True)
    go.set()
    await running
    assert "**Taxes**" in discord.replied(ctx)[1]


@pytest.mark.usefixtures("impatient")
async def test_a_hub_that_takes_its_time_makes_done_ask_discord_to_wait(
    hub: FakeHub, monkeypatch: pytest.MonkeyPatch
) -> None:
    go = discord.slow(monkeypatch, hub, "set_done")
    ctx = discord.command()
    running = asyncio.create_task(done.done(ctx, the(hub), item=things.uid(1002)))
    await discord.until(lambda: ctx.defer.await_count == 1)
    ctx.defer.assert_awaited_once_with(ephemeral=True)
    go.set()
    await running
    assert "Call Ben" in discord.replied(ctx)[1]


@pytest.mark.usefixtures("impatient")
async def test_a_slow_hub_that_then_refuses_is_still_answered(hub: FakeHub, monkeypatch: pytest.MonkeyPatch) -> None:
    hub.fail["add"] = plsremind.ServerError(500, "boom", "no")
    go = discord.slow(monkeypatch, hub, "add")
    ctx = discord.command()
    running = asyncio.create_task(remind.remind(ctx, the(hub), when="2h", what="Laundry", note=None, public=False))
    await discord.until(lambda: ctx.defer.await_count == 1)
    go.set()
    await running
    assert "Das hat nicht geklappt" in discord.replied(ctx)[1]


async def test_a_hub_that_answers_at_once_needs_no_waiting(hub: FakeHub) -> None:
    ctx = discord.command()
    await remind.remind(ctx, the(hub), when="2h", what="Laundry", note=None, public=False)
    await done.done(ctx, the(hub), item=things.uid(1002))
    ctx.defer.assert_not_called()


async def test_if_discord_cannot_be_asked_to_wait_the_work_is_given_up() -> None:
    started, cancelled = asyncio.Event(), asyncio.Event()

    async def work() -> None:
        started.set()
        try:
            await asyncio.sleep(60)
        except asyncio.CancelledError:
            cancelled.set()
            raise

    async def wait() -> None:
        raise hikari.HikariError

    with mock.patch.object(patience, "PATIENCE", 0.01), pytest.raises(hikari.HikariError):
        await patience.unhurried(work(), wait)
    await asyncio.sleep(0)
    assert started.is_set()
    assert cancelled.is_set()
