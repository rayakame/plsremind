from __future__ import annotations

import asyncio
import collections.abc
import logging
import os
import pathlib
import signal
import sqlite3
import typing
from unittest import mock

import hikari
import lightbulb
import msgspec
import plsremind
import pytest
import risa

from plsremind_discord import bot
from plsremind_discord.commands import common as commands_
from plsremind_discord.store import Store
from plsremind_discord.views import common
from plsremind_discord.views.common import Notice
from plsremind_discord.views.detail import DetailView
from plsremind_discord.views.overview import OverviewView
from plsremind_discord.views.reminder import ReminderView
from tests.support import discord
from tests.support import things
from tests.support.fake_hub import FakeHub

TOKEN = "MTIzNDU2Nzg5MDEyMzQ1Njc4.x.y"


@pytest.fixture
async def store(tmp_path: pathlib.Path) -> collections.abc.AsyncIterator[Store]:
    opened = await Store.open(tmp_path / "discord.db")
    try:
        yield opened
    finally:
        await opened.close()


def a_hub() -> FakeHub:
    hub = FakeHub()
    hub.client = typing.cast("plsremind.Client", mock.Mock(spec=plsremind.Client))
    hub.knows(things.user(), things.item(1))
    return hub


def tools(hub: FakeHub) -> commands_.Tools:
    views = discord.views(hub, Notice, ReminderView, OverviewView, DetailView)
    return commands_.Tools(hub=hub, texts=discord.say(), clock=discord.FixedClock(), config=discord.CONFIG, views=views)


def test_without_its_settings_the_bot_says_what_is_missing(
    monkeypatch: pytest.MonkeyPatch, capsys: pytest.CaptureFixture[str]
) -> None:
    for name in ("DISCORD_TOKEN", "PLSREMIND_URL", "PLSREMIND_TOKEN", "PLSREMIND_PUBLIC_URL"):
        monkeypatch.delenv(name, raising=False)
    with pytest.raises(SystemExit) as stopped:
        bot.run()
    assert stopped.value.code == 2
    assert "DISCORD_TOKEN" in capsys.readouterr().err


async def test_the_bot_is_put_together_without_intents_and_with_everything_it_listens_to(store: Store) -> None:
    hub = a_hub()
    config = msgspec.structs.replace(discord.CONFIG, discord_token=TOKEN)
    parts = await bot.build(config, store, hub)

    assert parts.discord.intents == hikari.Intents.NONE
    heard = {call.args[0] for call in typing.cast("mock.Mock", hub.client).subscribe.call_args_list}
    assert heard == {
        plsremind.DeliveryCreateEvent,
        plsremind.DeliveryCloseEvent,
        plsremind.ItemUpdateEvent,
        plsremind.ItemDeleteEvent,
        plsremind.ListUpdateEvent,
        plsremind.UserReadyEvent,
        plsremind.UserUpdateEvent,
        plsremind.UserGoneEvent,
    }
    view = OverviewView(user=common.pack(things.MIA))
    assert (await parts.views.build(view)).components
    assert view.shown is not None
    assert view.shown.public_url == "https://todo.example.com"


async def test_the_listeners_hand_events_to_the_parts_that_deal_with_them() -> None:
    client = mock.Mock(spec=plsremind.Client)
    reminders, sync = mock.AsyncMock(), mock.AsyncMock()
    reminders.withdraw = mock.Mock()
    gate = bot.Gate()
    gate.open()
    bot.listen(typing.cast("plsremind.Client", client), reminders=reminders, sync=sync, gate=gate)
    heard = {call.args[0]: call.args[1] for call in client.subscribe.call_args_list}

    delivery = things.delivery()
    await heard[plsremind.DeliveryCreateEvent](plsremind.DeliveryCreateEvent(user_id=things.MIA, delivery=delivery))
    reminders.deliver.assert_awaited_once_with(delivery)
    closed = plsremind.DeliveryCloseEvent(
        user_id=things.MIA, delivery_id=delivery.id, status=plsremind.DeliveryStatus.CANCELLED
    )
    await heard[plsremind.DeliveryCloseEvent](closed)
    reminders.withdraw.assert_called_once_with(delivery.id)
    changed = plsremind.ItemUpdateEvent(user_id=things.MIA, actor=None, nonce=None, item=things.item(1))
    await heard[plsremind.ItemUpdateEvent](changed)
    sync.item_changed.assert_awaited_once_with(changed)
    gone = plsremind.UserGoneEvent(user_id=things.MIA)
    await heard[plsremind.UserGoneEvent](gone)
    sync.user_gone.assert_awaited_once_with(gone)
    ready = plsremind.UserReadyEvent(user_id=things.MIA)
    await heard[plsremind.UserReadyEvent](ready)
    sync.user_ready.assert_awaited_once_with(ready)
    moved = plsremind.UserUpdateEvent(user_id=things.MIA, actor=None, nonce=None, user=things.user())
    await heard[plsremind.UserUpdateEvent](moved)
    sync.user_changed.assert_awaited_once_with(moved)


async def test_what_the_hub_reports_waits_until_discord_is_there() -> None:
    client = mock.Mock(spec=plsremind.Client)
    reminders, sync = mock.AsyncMock(), mock.AsyncMock()
    reminders.withdraw = mock.Mock()
    gate = bot.Gate()
    bot.listen(typing.cast("plsremind.Client", client), reminders=reminders, sync=sync, gate=gate)
    heard = {call.args[0]: call.args[1] for call in client.subscribe.call_args_list}
    delivery = things.delivery()
    changed = plsremind.ItemUpdateEvent(user_id=things.MIA, actor=None, nonce=None, item=things.item(1))

    waiting = [
        asyncio.create_task(
            heard[plsremind.DeliveryCreateEvent](plsremind.DeliveryCreateEvent(user_id=things.MIA, delivery=delivery))
        ),
        asyncio.create_task(heard[plsremind.ItemUpdateEvent](changed)),
        asyncio.create_task(heard[plsremind.UserReadyEvent](plsremind.UserReadyEvent(user_id=things.MIA))),
    ]
    for _ in range(5):
        await asyncio.sleep(0)
    reminders.deliver.assert_not_called()
    sync.item_changed.assert_not_called()
    sync.user_ready.assert_not_called()

    gate.open()
    await asyncio.gather(*waiting)
    reminders.deliver.assert_awaited_once_with(delivery)
    sync.item_changed.assert_awaited_once_with(changed)
    sync.user_ready.assert_awaited_once()


async def test_on_the_way_out_what_waits_for_discord_is_let_go_unheard() -> None:
    client = mock.Mock(spec=plsremind.Client)
    reminders, sync = mock.AsyncMock(), mock.AsyncMock()
    gate = bot.Gate()
    bot.listen(typing.cast("plsremind.Client", client), reminders=reminders, sync=sync, gate=gate)
    heard = {call.args[0]: call.args[1] for call in client.subscribe.call_args_list}
    delivery = things.delivery()
    waiting = asyncio.create_task(
        heard[plsremind.DeliveryCreateEvent](plsremind.DeliveryCreateEvent(user_id=things.MIA, delivery=delivery))
    )
    await asyncio.sleep(0)
    gate.shut()
    await asyncio.wait_for(waiting, 1)
    reminders.deliver.assert_not_called()
    await heard[plsremind.UserGoneEvent](plsremind.UserGoneEvent(user_id=things.MIA))
    sync.user_gone.assert_not_called()


@pytest.mark.parametrize(
    ("failure", "said"),
    [
        (common.NotYoursError(), "nicht mit plsremind verbunden"),
        (plsremind.NetworkError("no route"), "Das hat nicht geklappt"),
    ],
)
async def test_a_click_that_fails_is_answered_in_private(failure: Exception, said: str) -> None:
    ctx = discord.click()
    pipeline = risa.ExecutionPipeline([risa.Failure(failure, risa.FailureStage.HANDLER)])
    assert await bot.click_failed(pipeline, typing.cast("risa.ComponentContext", ctx), tools(a_hub())) is True
    sent = ctx.respond.await_args.kwargs
    assert sent["ephemeral"] is True
    built = [builder.build()[0] for builder in sent["components"]]
    assert said in "\n".join(discord.payload_texts(built))


async def test_a_click_that_fails_for_a_reason_nobody_foresaw_is_answered_and_logged(
    caplog: pytest.LogCaptureFixture,
) -> None:
    ctx = discord.click()
    pipeline = risa.ExecutionPipeline([risa.Failure(RuntimeError("boom"), risa.FailureStage.HANDLER)])
    assert await bot.click_failed(pipeline, typing.cast("risa.ComponentContext", ctx), tools(a_hub())) is True
    sent = ctx.respond.await_args.kwargs
    assert sent["ephemeral"] is True
    built = [builder.build()[0] for builder in sent["components"]]
    assert "Das hat nicht geklappt" in "\n".join(discord.payload_texts(built))
    assert "a click failed" in caplog.text
    assert "boom" in caplog.text


async def test_a_click_while_the_bot_is_starting_says_so_instead_of_blaming_the_account() -> None:
    hub = a_hub()
    hub.ready.clear()
    ctx = discord.click()
    pipeline = risa.ExecutionPipeline([risa.Failure(common.StartingError(), risa.FailureStage.HOOK)])
    assert await bot.click_failed(pipeline, typing.cast("risa.ComponentContext", ctx), tools(hub)) is True
    built = [builder.build()[0] for builder in ctx.respond.await_args.kwargs["components"]]
    said = "\n".join(discord.payload_texts(built))
    assert "Der Bot startet gerade" in said
    assert "nicht mit plsremind verbunden" not in said


async def test_a_click_that_can_no_longer_be_answered_is_only_logged(caplog: pytest.LogCaptureFixture) -> None:
    ctx = discord.click()
    ctx.respond.side_effect = hikari.HikariError("the token ran out")
    pipeline = risa.ExecutionPipeline([risa.Failure(plsremind.NetworkError("no route"), risa.FailureStage.HANDLER)])
    assert await bot.click_failed(pipeline, typing.cast("risa.ComponentContext", ctx), tools(a_hub())) is True
    assert "could not be told" in caplog.text


def failed(ctx: mock.Mock) -> lightbulb.exceptions.ExecutionPipelineFailedException:
    return lightbulb.exceptions.ExecutionPipelineFailedException([], RuntimeError("boom"), mock.Mock(), ctx)


async def test_a_command_that_fails_is_answered_and_logged(caplog: pytest.LogCaptureFixture) -> None:
    ctx = discord.command()
    assert await bot.command_failed(failed(ctx), tools(a_hub())) is True
    hidden, text = discord.replied(ctx)
    assert hidden is True
    assert "Das hat nicht geklappt" in text
    assert "a command failed" in caplog.text
    assert "boom" in caplog.text


async def test_a_command_that_fails_after_it_answered_is_only_logged(caplog: pytest.LogCaptureFixture) -> None:
    ctx = discord.command()
    ctx.respond.side_effect = hikari.HikariError("already answered")
    assert await bot.command_failed(failed(ctx), tools(a_hub())) is True
    assert "a command failed" in caplog.text


class Running(typing.NamedTuple):
    hub: FakeHub
    discord_started: mock.AsyncMock
    discord_closed: mock.AsyncMock
    commands_stopped: mock.AsyncMock
    order: list[str]


@pytest.fixture
def running(
    tmp_path: pathlib.Path, caplog: pytest.LogCaptureFixture
) -> collections.abc.Iterator[tuple[Running, bot.Config]]:
    caplog.set_level(logging.DEBUG)
    hub = a_hub()
    hub.ready.clear()
    order: list[str] = []

    def step(name: str, result: object = None) -> collections.abc.Callable[..., object]:
        def stepped(*_: object, **__: object) -> object:
            order.append(name)
            return result

        return stepped

    config = msgspec.structs.replace(discord.CONFIG, discord_token=TOKEN, database=tmp_path / "discord.db")
    with (
        mock.patch.object(bot.hub_, "connect", return_value=hub),
        mock.patch.object(FakeHub, "start", mock.AsyncMock(wraps=hub.start, side_effect=step("hub", mock.DEFAULT))),
        mock.patch.object(
            FakeHub, "close", mock.AsyncMock(wraps=hub.close, side_effect=step("hub closed", mock.DEFAULT))
        ),
        mock.patch.object(bot.Reminders, "close", mock.AsyncMock(side_effect=step("reminders closed"))),
        mock.patch.object(bot.Sync, "close", mock.AsyncMock(side_effect=step("sync closed"))),
        mock.patch.object(hikari.GatewayBot, "start", mock.AsyncMock(side_effect=step("discord"))) as started,
        mock.patch.object(hikari.GatewayBot, "close", mock.AsyncMock(side_effect=step("discord closed"))) as closed,
        mock.patch.object(lightbulb.Client, "start", mock.AsyncMock(side_effect=step("commands"))),
        mock.patch.object(lightbulb.Client, "stop", mock.AsyncMock()) as stopped,
    ):
        yield Running(hub, started, closed, stopped, order), config


async def test_the_bot_runs_until_it_is_told_to_stop(
    running: tuple[Running, bot.Config], caplog: pytest.LogCaptureFixture
) -> None:
    parts, config = running
    serving = asyncio.create_task(bot.main(config))
    await discord.until(lambda: "the bot is running" in caplog.text)
    os.kill(os.getpid(), signal.SIGTERM)
    assert await asyncio.wait_for(serving, 5) == 0
    parts.discord_started.assert_awaited_once_with(check_for_updates=False)
    parts.discord_closed.assert_awaited_once()
    parts.commands_stopped.assert_awaited_once()
    assert not parts.hub.ready.is_set()
    assert config.discord_token not in caplog.text
    assert config.hub_token not in caplog.text


async def test_the_hub_is_asked_first_so_that_a_refusal_costs_no_discord_session(
    running: tuple[Running, bot.Config], caplog: pytest.LogCaptureFixture
) -> None:
    parts, config = running
    serving = asyncio.create_task(bot.main(config))
    await discord.until(lambda: "the bot is running" in caplog.text)
    assert parts.order == ["hub", "discord", "commands"]
    os.kill(os.getpid(), signal.SIGTERM)
    await asyncio.wait_for(serving, 5)


async def test_on_the_way_out_the_hub_goes_quiet_before_anything_else_is_closed(
    running: tuple[Running, bot.Config], caplog: pytest.LogCaptureFixture
) -> None:
    parts, config = running
    serving = asyncio.create_task(bot.main(config))
    await discord.until(lambda: "the bot is running" in caplog.text)
    os.kill(os.getpid(), signal.SIGTERM)
    await asyncio.wait_for(serving, 5)
    assert parts.order[3:] == ["hub closed", "reminders closed", "sync closed", "discord closed"]


async def test_a_hub_that_refuses_the_bot_ends_it_with_a_reason(
    running: tuple[Running, bot.Config], caplog: pytest.LogCaptureFixture
) -> None:
    parts, config = running
    with mock.patch.object(FakeHub, "start", mock.AsyncMock(side_effect=bot.hub_.HubError("not a discord connection"))):
        assert await bot.main(config) == 1
    assert "stopping: HubError: not a discord connection" in caplog.text
    parts.discord_started.assert_not_called()


async def test_a_hub_that_cannot_be_reached_ends_the_bot_before_discord_is_touched(
    running: tuple[Running, bot.Config], caplog: pytest.LogCaptureFixture
) -> None:
    parts, config = running
    with mock.patch.object(FakeHub, "start", mock.AsyncMock(side_effect=plsremind.GatewayError("connection refused"))):
        assert await bot.main(config) == 1
    assert "stopping: GatewayError" in caplog.text
    parts.discord_started.assert_not_called()


async def test_trouble_from_discord_at_the_start_ends_the_bot_with_a_reason(
    running: tuple[Running, bot.Config], caplog: pytest.LogCaptureFixture
) -> None:
    parts, config = running
    parts.discord_started.side_effect = RuntimeError("no session starts left")
    assert await bot.main(config) == 1
    assert "no session starts left" in caplog.text
    assert parts.order == ["hub", "hub closed", "reminders closed", "sync closed", "discord closed"]


async def test_being_turned_away_later_ends_the_bot_too(
    running: tuple[Running, bot.Config], caplog: pytest.LogCaptureFixture
) -> None:
    parts, config = running
    client = typing.cast("mock.Mock", parts.hub.client)
    serving = asyncio.create_task(bot.main(config))
    await discord.until(lambda: "the bot is running" in caplog.text)
    heard = {call.args[0]: call.args[1] for call in client.subscribe.call_args_list}
    await heard[plsremind.ClosedEvent](plsremind.ClosedEvent(error=plsremind.GatewayError("token revoked", 4004)))
    assert await asyncio.wait_for(serving, 5) == 1
    assert "the hub turned the bot away" in caplog.text
    assert config.discord_token not in caplog.text
    assert config.hub_token not in caplog.text


async def test_old_rows_keep_being_cleared_after_one_round_failed(caplog: pytest.LogCaptureFixture) -> None:
    store = mock.Mock(spec=Store)
    store.purge = mock.AsyncMock(side_effect=[sqlite3.OperationalError(), None, None])
    slept: list[float] = []

    async def sleep(seconds: float) -> None:
        slept.append(seconds)
        if len(slept) == 3:
            raise asyncio.CancelledError
        await asyncio.sleep(0)

    with pytest.raises(asyncio.CancelledError):
        await bot.purge(typing.cast("Store", store), sleep)
    assert store.purge.await_count == 3
    assert slept == [bot.PURGE_EVERY] * 3
    assert "old rows could not be cleared" in caplog.text
