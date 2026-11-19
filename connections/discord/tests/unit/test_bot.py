from __future__ import annotations

import asyncio
import collections.abc
import os
import pathlib
import signal
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
from plsremind_discord.views.detail import DetailView
from plsremind_discord.views.overview import OverviewView
from plsremind_discord.views.reminder import Notice
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
    bot.listen(typing.cast("plsremind.Client", client), reminders=reminders, sync=sync)
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


async def test_a_click_that_fails_for_a_reason_nobody_foresaw_is_left_to_be_logged() -> None:
    ctx = discord.click()
    pipeline = risa.ExecutionPipeline([risa.Failure(RuntimeError("boom"), risa.FailureStage.HANDLER)])
    assert await bot.click_failed(pipeline, typing.cast("risa.ComponentContext", ctx), tools(a_hub())) is False
    ctx.respond.assert_not_called()


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


@pytest.fixture
def running(tmp_path: pathlib.Path) -> collections.abc.Iterator[tuple[Running, bot.Config]]:
    hub = a_hub()
    config = msgspec.structs.replace(discord.CONFIG, discord_token=TOKEN, database=tmp_path / "discord.db")
    with (
        mock.patch.object(bot.hub_, "connect", return_value=hub),
        mock.patch.object(hikari.GatewayBot, "start", mock.AsyncMock()) as started,
        mock.patch.object(hikari.GatewayBot, "close", mock.AsyncMock()) as closed,
        mock.patch.object(lightbulb.Client, "start", mock.AsyncMock()),
        mock.patch.object(lightbulb.Client, "stop", mock.AsyncMock()) as stopped,
    ):
        yield Running(hub, started, closed, stopped), config


async def test_the_bot_runs_until_it_is_told_to_stop(running: tuple[Running, bot.Config]) -> None:
    parts, config = running
    serving = asyncio.create_task(bot.main(config))
    await discord.until(lambda: parts.discord_started.await_count == 1 and parts.hub.ready.is_set())
    await asyncio.sleep(0)
    os.kill(os.getpid(), signal.SIGTERM)
    assert await asyncio.wait_for(serving, 5) == 0
    parts.discord_started.assert_awaited_once_with(check_for_updates=False)
    parts.discord_closed.assert_awaited_once()
    parts.commands_stopped.assert_awaited_once()
    assert not parts.hub.ready.is_set()


async def test_a_hub_that_refuses_the_bot_ends_it_with_a_reason(
    running: tuple[Running, bot.Config], caplog: pytest.LogCaptureFixture
) -> None:
    parts, config = running
    with mock.patch.object(FakeHub, "start", mock.AsyncMock(side_effect=bot.hub_.HubError("not a discord connection"))):
        assert await bot.main(config) == 1
    assert "stopping: HubError: not a discord connection" in caplog.text
    parts.discord_closed.assert_awaited_once()


async def test_being_turned_away_later_ends_the_bot_too(
    running: tuple[Running, bot.Config], caplog: pytest.LogCaptureFixture
) -> None:
    parts, config = running
    client = typing.cast("mock.Mock", parts.hub.client)
    serving = asyncio.create_task(bot.main(config))
    await discord.until(lambda: parts.discord_started.await_count == 1 and parts.hub.ready.is_set())
    heard = {call.args[0]: call.args[1] for call in client.subscribe.call_args_list}
    await heard[plsremind.ClosedEvent](plsremind.ClosedEvent(error=plsremind.GatewayError("token revoked", 4004)))
    assert await asyncio.wait_for(serving, 5) == 1
    assert "the hub turned the bot away" in caplog.text
    assert "discord-secret" not in caplog.text
    assert "plsr_con_secret" not in caplog.text
