from __future__ import annotations

import collections.abc
import typing
from unittest import mock

import hikari
import hikari.impl
import risa

from tests.support import discord

CHANNEL: typing.Final = 7
_BOT: typing.Final = {"id": "1", "username": "bot", "discriminator": "0", "avatar": None, "bot": True}


def sent(built: collections.abc.Mapping[str, typing.Any]) -> list[dict[str, typing.Any]]:
    """What Discord would hold of a built view: the components as JSON, each with an ID of its own."""
    payloads = [builder.build()[0] for builder in built["components"]]
    count = 0

    def number(node: object) -> None:
        nonlocal count
        if isinstance(node, dict):
            part = typing.cast("dict[str, typing.Any]", node)
            if isinstance(part.get("type"), int):
                count += 1
                part["id"] = count
            for value in part.values():
                number(value)
        elif isinstance(node, list):
            for value in typing.cast("list[object]", node):
                number(value)

    number(payloads)
    return payloads


def custom_id(payloads: object, *, label: str | None = None, kind: int | None = None, nth: int = 0) -> str:
    """The ID of the `nth` component with that label or of that type."""
    found = [
        part["custom_id"]
        for part in _parts(payloads)
        if "custom_id" in part and (label is None or part.get("label") == label) and kind in {None, part.get("type")}
    ]
    return found[nth]


def _parts(node: object) -> collections.abc.Iterator[dict[str, typing.Any]]:
    if isinstance(node, dict):
        part = typing.cast("dict[str, typing.Any]", node)
        yield part
        for value in part.values():
            yield from _parts(value)
    elif isinstance(node, list):
        for value in typing.cast("list[object]", node):
            yield from _parts(value)


def _message(payloads: list[dict[str, typing.Any]], message: int) -> dict[str, typing.Any]:
    return {
        "id": str(message),
        "channel_id": str(CHANNEL),
        "author": _BOT,
        "content": "",
        "timestamp": "2026-11-19T13:30:00+00:00",
        "edited_timestamp": None,
        "tts": False,
        "mention_everyone": False,
        "mentions": [],
        "mention_roles": [],
        "attachments": [],
        "embeds": [],
        "pinned": False,
        "type": 0,
        "flags": int(hikari.MessageFlag.IS_COMPONENTS_V2),
        "components": payloads,
    }


def as_message(views: risa.GatewayEnabledClient, payloads: list[dict[str, typing.Any]], message: int) -> hikari.Message:
    factory = hikari.impl.EntityFactoryImpl(app=typing.cast("hikari.RESTAware", views.app))
    return factory.deserialize_message(_message(payloads, message))


async def click(
    views: risa.GatewayEnabledClient,
    payloads: list[dict[str, typing.Any]],
    pressed: str,
    *,
    user: int = 1234,
    message: int = 555,
    values: tuple[str, ...] | None = None,
) -> mock.Mock:
    """Send a click on one component of a message through risa, as the gateway would. Returns the REST mock."""
    app = views.app
    factory = hikari.impl.EntityFactoryImpl(app=typing.cast("hikari.RESTAware", app))
    data: dict[str, typing.Any] = {"custom_id": pressed, "component_type": 2 if values is None else 3}
    if values is not None:
        data["values"] = list(values)
    interaction = factory.deserialize_component_interaction(
        {
            "id": "900",
            "application_id": "1",
            "type": 3,
            "token": "token",
            "version": 1,
            "data": data,
            "message": _message(payloads, message),
            "user": {"id": str(user), "username": "someone", "discriminator": "0", "avatar": None},
            "channel_id": str(CHANNEL),
            "channel": {"id": str(CHANNEL), "type": 1},
            "locale": "de",
            "app_permissions": "0",
            "entitlements": [],
            "authorizing_integration_owners": {"1": str(user)},
            "context": 1,
            "attachment_size_limit": 1000,
        }
    )
    rest = typing.cast("mock.Mock", app.rest)
    rest.reset_mock()
    listener = typing.cast("mock.Mock", app.event_manager).subscribe.call_args.args[1]
    await listener(hikari.ComponentInteractionCreateEvent(shard=mock.Mock(), interaction=interaction))
    return rest


def answered(rest: mock.Mock) -> tuple[object, bool, list[dict[str, typing.Any]]]:
    """How a click was first answered: the kind of response, whether only the user sees it, and the components."""
    call = rest.create_interaction_response.await_args
    flags = call.kwargs.get("flags")
    hidden = flags is not hikari.UNDEFINED and flags is not None and bool(flags & hikari.MessageFlag.EPHEMERAL)
    built = call.kwargs.get("components")
    return call.args[2], hidden, sent({"components": built}) if built else []


def texts(payloads: object) -> str:
    return "\n".join(discord.payload_texts(payloads))
