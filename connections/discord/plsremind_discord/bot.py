from __future__ import annotations

import asyncio
import collections.abc
import contextlib
import logging
import os
import signal
import sys
import typing

import aiohttp.web
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
from plsremind_discord.metrics import Metrics
from plsremind_discord.metrics import serve
from plsremind_discord.reminders import Reminders
from plsremind_discord.store import Store
from plsremind_discord.submits import Modals
from plsremind_discord.sync import Sync
from plsremind_discord.views import common
from plsremind_discord.views.common import Notice
from plsremind_discord.views.detail import DetailView
from plsremind_discord.views.overview import OverviewView
from plsremind_discord.views.reminder import ReminderView

PURGE_EVERY: typing.Final = 3600.0
_KINDS: typing.Final[dict[type[hikari.PartialInteraction], str]] = {
    hikari.CommandInteraction: "command",
    hikari.ComponentInteraction: "component",
    hikari.ModalInteraction: "modal",
    hikari.AutocompleteInteraction: "autocomplete",
}
_MISCONFIGURED: typing.Final = 2
_LOGGER: typing.Final = logging.getLogger("plsremind_discord")


class Parts(typing.NamedTuple):
    discord: hikari.GatewayBot
    commands: lightbulb.Client
    views: risa.Client
    reminders: Reminders
    sync: Sync
    gate: Gate
    metrics: Metrics


async def build(config: Config, store: Store, hub: hub_.Hub, metrics: Metrics) -> Parts:
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
    tools = commands_.Tools(hub=hub, texts=texts, clock=clock, config=config, views=views, metrics=metrics)
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

    sync = Sync(hub=hub, views=views, store=store, metrics=metrics)
    reminders = Reminders(
        hub=hub,
        views=views,
        rest=discord.rest,
        store=store,
        texts=texts,
        clock=clock,
        bot=config.bot_id,
        metrics=metrics,
        redraw=sync.redraw_soon,
    )
    modals = Modals(hub=hub, views=views, store=store, texts=texts, clock=clock, config=config, metrics=metrics)
    discord.subscribe(hikari.ModalInteractionCreateEvent, modals.submitted)

    async def counted(event: hikari.InteractionCreateEvent) -> None:  # ruff: ignore[unused-async]
        for cls, kind in _KINDS.items():
            if isinstance(event.interaction, cls):
                metrics.interactions.labels(kind).inc()
                return

    discord.subscribe(hikari.InteractionCreateEvent, counted)
    metrics.watch(
        connected=lambda: hub.client.status is plsremind.ClientStatus.CONNECTED,
        latency=lambda: discord.heartbeat_latency,
    )
    gate = Gate()
    listen(hub.client, reminders=reminders, sync=sync, gate=gate)
    return Parts(discord, commands, views, reminders, sync, gate, metrics)


class Gate:
    """Holds what the hub reports until Discord can be reached, and lets it go unheard on the way out."""

    def __init__(self) -> None:
        self._decided = asyncio.Event()
        self._open = False

    def open(self) -> None:
        self._open = True
        self._decided.set()

    def shut(self) -> None:
        self._open = False
        self._decided.set()

    @property
    def holding(self) -> bool:
        return not self._decided.is_set()

    @property
    def is_open(self) -> bool:
        return self._open

    async def passes(self) -> bool:
        await self._decided.wait()
        return self._open


def listen(client: plsremind.Client, *, reminders: Reminders, sync: Sync, gate: Gate) -> None:
    """Hand what the hub reports to the parts that deal with it, once the gate lets it through."""

    def once_up[E: plsremind.Event](
        deal: collections.abc.Callable[[E], collections.abc.Awaitable[None]],
    ) -> collections.abc.Callable[[E], collections.abc.Coroutine[typing.Any, typing.Any, None]]:
        async def waited(event: E) -> None:
            # What is dropped on the way out comes again: the hub sends what is open at the next login.
            if await gate.passes():
                await deal(event)

        return waited

    # Held events pass the gate in no fixed order, so a delivery closed meanwhile is noted on arrival.
    closed_early: set[plsremind.ID] = set()

    async def due(event: plsremind.DeliveryCreateEvent) -> None:
        if event.delivery.id in closed_early:
            closed_early.discard(event.delivery.id)
            return
        await reminders.deliver(event.delivery)

    async def withdrawn(event: plsremind.DeliveryCloseEvent) -> None:
        if gate.holding:
            closed_early.add(event.delivery_id)
        if await gate.passes():
            reminders.withdraw(event.delivery_id)

    client.subscribe(plsremind.DeliveryCreateEvent, once_up(due))
    client.subscribe(plsremind.DeliveryCloseEvent, withdrawn)
    client.subscribe(plsremind.DeliveryTestEvent, once_up(lambda event: reminders.probe(event.test)))
    client.subscribe(plsremind.ItemUpdateEvent, once_up(sync.item_changed))
    client.subscribe(plsremind.ItemDeleteEvent, once_up(sync.item_changed))
    client.subscribe(plsremind.ListUpdateEvent, once_up(sync.list_changed))
    client.subscribe(plsremind.UserReadyEvent, once_up(sync.user_ready))
    client.subscribe(plsremind.UserUpdateEvent, once_up(sync.user_changed))
    client.subscribe(plsremind.UserGoneEvent, once_up(sync.user_gone))


def discord_connected(discord: hikari.GatewayBot) -> bool:
    # is_alive only says the bot was started and not closed, not that its shards are connected right now.
    return discord.is_alive and all(shard.is_connected for shard in discord.shards.values())


def ready(gate: Gate, hub: plsremind.ClientStatus, *, discord_alive: bool) -> bool:
    """Whether the bot can take a click and pass a reminder on: the hub and Discord are both there."""
    return gate.is_open and hub is plsremind.ClientStatus.CONNECTED and discord_alive


async def click_failed(pipeline: risa.ExecutionPipeline, ctx: risa.ComponentContext, tools: commands_.Tools) -> bool:
    say = tools.texts
    user = tools.hub.user_of(int(ctx.user.id))
    profile = tools.hub.profile(user) if user is not None else None
    locale = texts_.locale_for(profile, str(ctx.interaction.locale))
    link = label = ""
    if pipeline.find(common.StartingError) is not None:
        text = say("error.starting", locale)
    elif pipeline.find(common.NotYoursError) is not None:
        text, link, label = common.unlinked(say, locale, tools.config)
    else:
        tools.metrics.interaction_failures.labels("component").inc()
        for failure in pipeline.failures:
            _LOGGER.error("a click failed at the %s", failure.stage, exc_info=failure.error)
        text = say("error.failed", locale)
    rendered = await tools.views.build(Notice(text=text, link=link, label=label))
    try:
        await ctx.respond(**rendered, ephemeral=True)
    except hikari.HikariError as error:
        _LOGGER.warning("someone could not be told that their click failed: %s", error)
    return True


async def command_failed(
    failure: lightbulb.exceptions.ExecutionPipelineFailedException, tools: commands_.Tools
) -> bool:
    _LOGGER.error("a command failed", exc_info=failure)
    tools.metrics.interaction_failures.labels("command").inc()
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
    metrics = Metrics()
    hub = hub_.connect(config, metrics)
    parts = await build(config, store, hub, metrics)
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
    tasks: list[asyncio.Task[None]] = []
    served: aiohttp.web.AppRunner | None = None
    try:
        if config.metrics_address is not None:
            served = await _serve_metrics(parts, hub, config.metrics_address)
        reason = await _run(parts, hub, store, stopped=stopped, stop=stop, tasks=tasks)
    except (
        plsremind.PlsremindError,
        hub_.HubError,
        hikari.HikariError,
        lightbulb.exceptions.LightbulbException,
        MetricsAddressError,
    ) as error:
        reason = f"{type(error).__name__}: {error}"
    except Exception as error:
        _LOGGER.exception("the bot ran into something nobody foresaw")
        reason = f"{type(error).__name__}: {error}"
    finally:
        await _put_away(parts, hub, tasks)
        if served is not None:
            await served.cleanup()
    if reason is None:
        return 0
    _LOGGER.error("stopping: %s", reason)
    return 1


async def _serve_metrics(parts: Parts, hub: hub_.Hub, address: tuple[str, int]) -> aiohttp.web.AppRunner:
    def answer() -> bool:
        return ready(parts.gate, hub.client.status, discord_alive=discord_connected(parts.discord))

    try:
        return await serve(parts.metrics, address, answer)
    except OSError as error:
        msg = f"PLSREMIND_DISCORD_METRICS_ADDR: {error}"
        raise MetricsAddressError(msg) from error


class MetricsAddressError(Exception):
    pass


async def _run(
    parts: Parts,
    hub: hub_.Hub,
    store: Store,
    *,
    stopped: asyncio.Future[str | None],
    stop: collections.abc.Callable[[str | None], None],
    tasks: list[asyncio.Task[None]],
) -> str | None:
    starting = asyncio.create_task(_start(parts, hub), name="start")
    # A stop during the start ends it there, before Discord is reached if the hub is still being waited for.
    await asyncio.wait({starting, stopped}, return_when=asyncio.FIRST_COMPLETED)
    if not starting.done():
        starting.cancel()
        await asyncio.gather(starting, return_exceptions=True)
        return stopped.result()
    starting.result()
    parts.gate.open()
    tasks.extend(
        (
            asyncio.create_task(_watch_discord(parts.discord, stop), name="discord"),
            asyncio.create_task(purge(store), name="purge"),
        )
    )
    _LOGGER.info("the bot is running")
    return await stopped


async def _start(parts: Parts, hub: hub_.Hub) -> None:
    # The hub first: a restart loop over a hub that refuses the bot must not use up Discord's session starts.
    await hub.start()
    await parts.discord.start(check_for_updates=False)
    await parts.commands.start()


async def _watch_discord(discord: hikari.GatewayBot, stop: collections.abc.Callable[[str | None], None]) -> None:
    # Without a gateway no command reaches the bot, so it ends and the container starts it again.
    try:
        await discord.join()
    except Exception as error:  # ruff: ignore[blind-except]
        stop(f"Discord's gateway closed: {error}")
        return
    stop("Discord's gateway closed")


async def _put_away(parts: Parts, hub: hub_.Hub, tasks: list[asyncio.Task[None]]) -> None:
    for task in tasks:
        task.cancel()
    # The hub goes quiet first, so that nothing new comes in while the rest is put away.
    parts.gate.shut()
    await hub.close()
    await parts.reminders.close()
    await parts.sync.close()
    await parts.commands.stop()
    with contextlib.suppress(hikari.HikariError):
        await parts.discord.close()


async def purge(
    store: Store, sleep: collections.abc.Callable[[float], collections.abc.Awaitable[None]] = asyncio.sleep
) -> None:
    """Clear what the store no longer needs, once an hour, for as long as the bot runs."""
    clock = Clock()
    while True:
        try:
            await store.purge(clock.now())
        except Exception:
            _LOGGER.exception("old rows could not be cleared")
        await sleep(PURGE_EVERY)


def run() -> None:
    logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(name)s: %(message)s")
    try:
        config = Config.from_env(os.environ)
    except ConfigError as error:
        print(error, file=sys.stderr)  # ruff: ignore[print]
        sys.exit(_MISCONFIGURED)
    sys.exit(asyncio.run(main(config)))
