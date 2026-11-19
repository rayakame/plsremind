from __future__ import annotations

import collections.abc
import datetime
import functools
import typing
from unittest import mock

import hikari
import risa
from risa import ui

from plsremind_discord import texts
from plsremind_discord.clock import Clock
from plsremind_discord.config import Config
from plsremind_discord.hub import Hub
from plsremind_discord.store import Store
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
