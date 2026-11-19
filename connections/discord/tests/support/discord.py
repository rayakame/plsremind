from __future__ import annotations

import asyncio
import collections.abc
import datetime
import functools
import typing
from unittest import mock

import hikari
import plsremind
import risa
from risa import ui

from plsremind_discord import texts
from plsremind_discord.clock import Clock
from plsremind_discord.config import Config
from plsremind_discord.hub import Hub
from plsremind_discord.store import Store
from plsremind_discord.views import common
from tests.support import things

CONFIG: typing.Final = Config(
    discord_token="discord-secret",
    hub_url="http://hub:8080",
    hub_token="plsr_con_secret",
    public_url="https://todo.example.com",
    database=things.DATABASE,
)


class FixedClock(Clock):
    def __init__(self, moment: datetime.datetime = things.NOW) -> None:
        self.moment = moment

    @typing.override
    def now(self) -> datetime.datetime:
        return self.moment


class App:
    def __init__(self) -> None:
        self.rest = mock.Mock(spec=hikari.api.RESTClient)
        self.event_manager = mock.Mock(spec=hikari.api.EventManager)


@functools.cache
def say() -> texts.Texts:
    return texts.Texts(texts.provider())


def views(
    hub: Hub, *classes: type[risa.View], clock: Clock | None = None, store: Store | None = None
) -> risa.GatewayEnabledClient:
    client = risa.client_from_app(typing.cast("hikari.GatewayBot", App()), auto_defer=risa.AutoDefer.UPDATE)
    registry = client.di.registry_for(risa.Contexts.DEFAULT)
    registry.register_value(Hub, hub)
    registry.register_value(Config, CONFIG)
    registry.register_value(Clock, clock or FixedClock())
    registry.register_value(texts.Texts, say())
    if store is not None:
        registry.register_value(Store, store)
    for cls in classes:
        client.add_view(cls)
    return client


def walk(layout: object) -> collections.abc.Iterator[object]:
    if isinstance(layout, collections.abc.Sequence):
        for part in typing.cast("collections.abc.Sequence[object]", layout):
            yield from walk(part)
        return
    yield layout
    if isinstance(layout, ui.Container):
        yield from walk(layout.children)
    elif isinstance(layout, ui.Section):
        yield from walk(layout.text_displays)
        yield from walk(layout.accessory)
    elif isinstance(layout, ui.Row):
        yield from walk(layout.components)


def nodes[T](layout: object, kind: type[T]) -> list[T]:
    return [node for node in walk(layout) if isinstance(node, kind)]


def written(layout: object) -> str:
    return "\n".join(node.content for node in nodes(layout, ui.TextDisplay))


def labels(layout: object) -> list[str | None]:
    return [node.label for node in walk(layout) if isinstance(node, ui.Button | ui.LinkButton)]


def count(layout: object) -> int:
    return sum(1 for _ in walk(layout))


async def until(condition: collections.abc.Callable[[], bool], within: float = 2.0) -> None:
    async with asyncio.timeout(within):
        while not condition():  # ruff: ignore[async-busy-wait]
            await asyncio.sleep(0)


def shown(
    item: plsremind.Item | None,
    *,
    now: datetime.datetime = things.NOW,
    list_name: str = "Inbox",
    locale: hikari.Locale = hikari.Locale.DE,
) -> common.Shown:
    return common.Shown(
        item=item,
        list_name=list_name,
        locale=locale,
        zone=things.BERLIN,
        now=now,
        public_url=CONFIG.public_url,
        texts=say(),
    )


def click(*values: str, message: int = 555, locale: str = "de") -> mock.Mock:
    """A click as a handler sees it."""
    ctx = mock.Mock(spec=risa.ComponentContext)
    ctx.message.id = hikari.Snowflake(message)
    ctx.values = list(values)
    ctx.rest = mock.Mock(spec=hikari.api.RESTClient)
    ctx.interaction = mock.Mock(spec=hikari.ComponentInteraction)
    ctx.interaction.locale = locale
    return ctx


def bound(button: ui.Button) -> int:
    """The one whole number a button carries to its handler."""
    (frame,) = button.binding.frames
    return int.from_bytes(frame.data, "little")
