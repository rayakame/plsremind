from __future__ import annotations

import asyncio
import contextlib
import logging
import os
import signal
import sys
import typing

import hikari
import lightbulb
import plsremind
import risa

from plsremind_discord import hub as hub_
from plsremind_discord import texts as texts_
from plsremind_discord.clock import Clock
from plsremind_discord.commands import common as commands_
from plsremind_discord.commands import every
from plsremind_discord.config import Config
from plsremind_discord.config import ConfigError
from plsremind_discord.reminders import Reminders
from plsremind_discord.store import Store
from plsremind_discord.submits import Modals
from plsremind_discord.sync import Sync
from plsremind_discord.views import common
from plsremind_discord.views.detail import DetailView
from plsremind_discord.views.overview import OverviewView
from plsremind_discord.views.reminder import Notice
from plsremind_discord.views.reminder import ReminderView

PURGE_EVERY: typing.Final = 3600.0
_MISCONFIGURED: typing.Final = 2
_LOGGER: typing.Final = logging.getLogger("plsremind_discord")


class Parts(typing.NamedTuple):
    discord: hikari.GatewayBot
    commands: lightbulb.Client
    views: risa.Client
    reminders: Reminders
    sync: Sync


async def build(config: Config, store: Store, hub: hub_.Hub) -> Parts:
    """Put the bot together. Nothing connects yet."""
    clock, texts = Clock(), texts_.Texts(texts_.provider())
    # Interactions arrive without any intent, and nothing here needs hikari's cache.
    discord = hikari.GatewayBot(
        config.discord_token,
        intents=hikari.Intents.NONE,
        cache_settings=hikari.impl.CacheSettings(components=hikari.api.CacheComponents.NONE),
    )
    commands = lightbulb.client_from_app(
        discord, localization_provider=texts_.provider(), default_locale=hikari.Locale.EN_US
    )
    views = risa.client_from_lightbulb(commands, auto_defer=risa.AutoDefer.UPDATE)
    tools = commands_.Tools(hub=hub, texts=texts, clock=clock, config=config, views=views)
    registry = commands.di.registry_for(lightbulb.di.Contexts.DEFAULT)
    registry.register_value(hub_.Hub, hub)
    registry.register_value(Config, config)
    registry.register_value(Clock, clock)
    registry.register_value(texts_.Texts, texts)
    registry.register_value(Store, store)
    registry.register_value(commands_.Tools, tools)
    for view in (Notice, ReminderView, OverviewView, DetailView):
        views.add_view(view)
    views.error_handler(click_failed)
    commands.error_handler(command_failed)
    for loader in every.LOADERS:
        await loader.add_to_client(commands)

    reminders = Reminders(hub=hub, views=views, rest=discord.rest, store=store, texts=texts, clock=clock)
    sync = Sync(hub=hub, views=views, store=store)
    modals = Modals(hub=hub, views=views, store=store, texts=texts, clock=clock, config=config)
    discord.subscribe(hikari.ModalInteractionCreateEvent, modals.submitted)
    listen(hub.client, reminders=reminders, sync=sync)
    return Parts(discord, commands, views, reminders, sync)


def listen(client: plsremind.Client, *, reminders: Reminders, sync: Sync) -> None:
    """Hand what the hub reports to the parts that deal with it."""

    async def due(event: plsremind.DeliveryCreateEvent) -> None:
        await reminders.deliver(event.delivery)

    async def withdrawn(event: plsremind.DeliveryCloseEvent) -> None:  # ruff: ignore[unused-async]
        reminders.withdraw(event.delivery_id)

    client.subscribe(plsremind.DeliveryCreateEvent, due)
    client.subscribe(plsremind.DeliveryCloseEvent, withdrawn)
    client.subscribe(plsremind.ItemUpdateEvent, sync.item_changed)
    client.subscribe(plsremind.ItemDeleteEvent, sync.item_changed)
    client.subscribe(plsremind.ListUpdateEvent, sync.list_changed)
    client.subscribe(plsremind.UserReadyEvent, sync.user_ready)
    client.subscribe(plsremind.UserGoneEvent, sync.user_gone)


async def click_failed(pipeline: risa.ExecutionPipeline, ctx: risa.ComponentContext, tools: commands_.Tools) -> bool:
    say = tools.texts
    user = tools.hub.user_of(int(ctx.user.id))
    profile = tools.hub.profile(user) if user is not None else None
    locale = texts_.locale_for(profile, str(ctx.interaction.locale))
    if pipeline.find(common.NotYoursError) is not None:
        text, link, label = common.unlinked(say, locale, tools.config)
    elif pipeline.find(plsremind.PlsremindError) is not None:
        text, link, label = say("error.failed", locale), "", ""
    else:
        return False
    rendered = await tools.views.build(Notice(text=text, link=link, label=label))
    await ctx.respond(**rendered, ephemeral=True)
    return True


async def command_failed(
    failure: lightbulb.exceptions.ExecutionPipelineFailedException, tools: commands_.Tools
) -> bool:
    _LOGGER.error("a command failed", exc_info=failure)
    ctx = failure.context
    locale = texts_.locale_for(None, str(ctx.interaction.locale))
    # If the command had already answered, there is nothing left to say it with.
    with contextlib.suppress(hikari.HikariError, RuntimeError):
        await commands_.say(ctx, tools, tools.texts("error.failed", locale))
    return True


async def main(config: Config) -> int:
    store = await Store.open(config.database)
    try:
        return await _serve(config, store)
    finally:
        await store.close()


async def _serve(config: Config, store: Store) -> int:
    hub = hub_.connect(config)
    parts = await build(config, store, hub)
    loop = asyncio.get_running_loop()
    stopped: asyncio.Future[str | None] = loop.create_future()

    def stop(reason: str | None) -> None:
        if not stopped.done():
            stopped.set_result(reason)

    async def turned_away(event: plsremind.ClosedEvent) -> None:  # ruff: ignore[unused-async]
        stop(f"the hub turned the bot away: {event.error}")

    hub.client.subscribe(plsremind.ClosedEvent, turned_away)
    for number in (signal.SIGINT, signal.SIGTERM):
        loop.add_signal_handler(number, stop, None)
    purging: asyncio.Task[None] | None = None
    try:
        await parts.discord.start(check_for_updates=False)
        await parts.commands.start()
        await hub.start()
        purging = asyncio.create_task(_purge(store), name="purge")
        _LOGGER.info("the bot is running")
        reason = await stopped
    except (
        plsremind.PlsremindError,
        hub_.HubError,
        hikari.HikariError,
        lightbulb.exceptions.LightbulbException,
    ) as error:
        reason = f"{type(error).__name__}: {error}"
    finally:
        if purging is not None:
            purging.cancel()
        await parts.reminders.close()
        await parts.sync.close()
        await hub.close()
        with contextlib.suppress(hikari.HikariError):
            await parts.commands.stop()
            await parts.discord.close()
    if reason is None:
        return 0
    _LOGGER.error("stopping: %s", reason)
    return 1


async def _purge(store: Store) -> None:
    clock = Clock()
    while True:
        await store.purge(clock.now())
        await asyncio.sleep(PURGE_EVERY)


def run() -> None:
    logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(name)s: %(message)s")
    try:
        config = Config.from_env(os.environ)
    except ConfigError as error:
        print(error, file=sys.stderr)  # ruff: ignore[print]
        sys.exit(_MISCONFIGURED)
    sys.exit(asyncio.run(main(config)))
