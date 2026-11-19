from __future__ import annotations

import asyncio
import collections.abc
import datetime
import functools
import typing
from unittest import mock

import hikari
import lightbulb
import plsremind
import pytest
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


def click(*values: str, message: int = 555, locale: str = "de", user: int = 1234) -> mock.Mock:
    """A click as a handler sees it."""
    ctx = mock.Mock(spec=risa.ComponentContext)
    ctx.user.id = hikari.Snowflake(user)
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


def submitted(
    custom_id: str, values: dict[str, str], *, user: int = 1234, message: int | None = 555, locale: str = "de"
) -> mock.Mock:
    """The event of a modal someone sent off."""
    interaction = mock.Mock(spec=hikari.ModalInteraction)
    interaction.custom_id = custom_id
    interaction.user.id = hikari.Snowflake(user)
    interaction.locale = locale
    interaction.message = mock.Mock(id=hikari.Snowflake(message)) if message is not None else None
    interaction.components = [[mock.Mock(custom_id=key, value=value)] for key, value in values.items()]
    event = mock.Mock(spec=hikari.ModalInteractionCreateEvent)
    event.interaction = interaction
    return event


def payload_texts(payload: object) -> list[str]:
    """Every text, label and link of a built message, in order."""
    if isinstance(payload, dict):
        document = typing.cast("dict[str, object]", payload)
        own = [str(document[key]) for key in ("content", "label", "url") if key in document]
        return own + [found for value in document.values() for found in payload_texts(value)]
    if isinstance(payload, list):
        return [found for value in typing.cast("list[object]", payload) for found in payload_texts(value)]
    return []


def answer(responder: mock.Mock) -> tuple[object, bool, str]:
    """How an interaction was answered: the kind of response, whether only the user sees it, and what it says."""
    call = responder.create_initial_response.await_args
    built = [builder.build()[0] for builder in call.kwargs.get("components", [])]
    hidden = call.kwargs.get("flags") == hikari.MessageFlag.EPHEMERAL
    return call.args[0], hidden, "\n".join(payload_texts(built))


def command(
    *,
    user: int = 1234,
    locale: str = "de",
    guild: int | None = 9,
    channel: int = 8,
    where: hikari.ApplicationContextType = hikari.ApplicationContextType.GUILD,
) -> mock.Mock:
    """A command as its code sees it."""
    ctx = mock.Mock(spec=lightbulb.Context)
    ctx.user.id = hikari.Snowflake(user)
    ctx.interaction.locale = locale
    ctx.interaction.context = where
    ctx.guild_id = hikari.Snowflake(guild) if guild is not None else None
    ctx.channel_id = hikari.Snowflake(channel)
    return ctx


def replied(ctx: mock.Mock) -> tuple[bool, str]:
    """Whether only the user sees the reply, and what it says."""
    call = ctx.respond.await_args
    built = [builder.build()[0] for builder in call.kwargs.get("components", [])]
    return call.kwargs["ephemeral"], "\n".join(payload_texts(built))


def slow(monkeypatch: pytest.MonkeyPatch, hub: Hub, name: str) -> asyncio.Event:
    """Make one way to the hub wait until the returned event is set."""
    go = asyncio.Event()
    real = getattr(hub, name)

    async def held(*args: object, **kwargs: object) -> object:
        await go.wait()
        return await real(*args, **kwargs)

    monkeypatch.setattr(hub, name, held)
    return go


def edited(responder: mock.Mock) -> str:
    """What the answer Discord was asked to wait for says in the end."""
    call = responder.edit_initial_response.await_args
    built = [builder.build()[0] for builder in call.kwargs.get("components", [])]
    return "\n".join(payload_texts(built))


def followed_up(responder: mock.Mock) -> tuple[bool, str]:
    """Whether only the user sees the message sent after the answer, and what it says."""
    call = responder.execute.await_args
    built = [builder.build()[0] for builder in call.kwargs.get("components", [])]
    return call.kwargs.get("flags") == hikari.MessageFlag.EPHEMERAL, "\n".join(payload_texts(built))
