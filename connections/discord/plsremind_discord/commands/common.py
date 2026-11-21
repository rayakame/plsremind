from __future__ import annotations

import collections.abc
import datetime
import typing
import zoneinfo

import hikari
import lightbulb
import msgspec
import plsremind
import risa

from plsremind_discord import patience
from plsremind_discord import texts as texts_
from plsremind_discord import timeparse
from plsremind_discord.clock import Clock
from plsremind_discord.config import Config
from plsremind_discord.hub import Hub
from plsremind_discord.views import common as views_
from plsremind_discord.views import overview
from plsremind_discord.views.common import Notice

INSTALLS: typing.Final = (
    hikari.ApplicationIntegrationType.GUILD_INSTALL,
    hikari.ApplicationIntegrationType.USER_INSTALL,
)
CONTEXTS: typing.Final = (
    hikari.ApplicationContextType.GUILD,
    hikari.ApplicationContextType.BOT_DM,
    hikari.ApplicationContextType.PRIVATE_CHANNEL,
)
SUGGESTED: typing.Final = 25
LABEL: typing.Final = 100
_UTC: typing.Final = zoneinfo.ZoneInfo("UTC")


class Tools(msgspec.Struct, frozen=True, kw_only=True):
    """What every command works with."""

    hub: Hub
    texts: texts_.Texts
    clock: Clock
    config: Config
    views: risa.Client


class Asking(msgspec.Struct, frozen=True, kw_only=True):
    """Who runs a command, in what language and where they live."""

    user: plsremind.ID
    locale: hikari.Locale
    zone: zoneinfo.ZoneInfo


async def say(
    ctx: lightbulb.Context, tools: Tools, text: str, link: str = "", label: str = "", *, public: bool = False
) -> None:
    rendered = await tools.views.build(Notice(text=text, link=link, label=label))
    # A title may hold a mention; nobody is to be pinged by someone else's todo.
    await ctx.respond(
        **rendered, ephemeral=not public, mentions_everyone=False, user_mentions=False, role_mentions=False
    )


async def asking(ctx: lightbulb.Context, tools: Tools) -> Asking | None:
    """Tell who is asking, or answer why the bot cannot serve them and return `None`."""
    speaks = str(ctx.interaction.locale)
    if not tools.hub.ready.is_set():
        await say(ctx, tools, tools.texts("error.starting", texts_.locale_for(None, speaks)))
        return None
    user = tools.hub.user_of(int(ctx.user.id))
    if user is None:
        await say(ctx, tools, *views_.unlinked(tools.texts, texts_.locale_for(None, speaks), tools.config))
        return None
    profile = tools.hub.profile(user)
    return Asking(
        user=user,
        locale=texts_.locale_for(profile, speaks),
        zone=profile.timezone if profile is not None else _UTC,
    )


async def patient[T](ctx: lightbulb.Context, work: collections.abc.Coroutine[object, object, T]) -> tuple[T, bool]:
    """Do `work`, and have Discord wait if the hub takes long. Also returns whether it had to wait."""
    waited = False

    async def wait() -> None:
        nonlocal waited
        # Private, because only a success may be seen by others, and the outcome is not known yet.
        await ctx.defer(ephemeral=True)
        waited = True

    return await patience.unhurried(work, wait), waited


def for_all(ctx: lightbulb.Context, *, asked: bool) -> bool:
    """Whether a confirmation goes to the channel. Alone with the bot there is nobody else to show it to."""
    return asked and ctx.interaction.context is not hikari.ApplicationContextType.BOT_DM


async def confirm(
    ctx: lightbulb.Context, tools: Tools, who: Asking, text: str, item: plsremind.Item, *, public: bool, waited: bool
) -> None:
    # After a wait the private confirmation comes first, because it takes the place of the wait.
    if not public or waited:
        private = views_.confirmation(tools.hub, tools.config, tools.texts, who.locale, text=text, item=item)
        await say(ctx, tools, *private)
    if public:
        await say(ctx, tools, text, public=True)


def problem(error: timeparse.TimeError | plsremind.PlsremindError) -> str:
    """The text for what went wrong."""
    if isinstance(error, timeparse.TimeError):
        return "error.time_past" if error.reason == "past" else "error.time_unreadable"
    if isinstance(error, plsremind.BadRequestError):
        return "error.invalid"
    return "error.failed"


def choices_for_items(
    items: collections.abc.Iterable[plsremind.Item], typed: str, now: datetime.datetime, zone: zoneinfo.ZoneInfo
) -> list[tuple[str, str]]:
    wanted = typed.strip().casefold()
    ordered = (line.item for line in overview.lines(items, now, zone))
    found = [(item.title[:LABEL], str(item.id)) for item in ordered if wanted in item.title.casefold()]
    return found[:SUGGESTED]


def choices_for_lists(lists: collections.abc.Iterable[plsremind.List], typed: str) -> list[tuple[str, str]]:
    wanted = typed.strip().casefold()
    return [(one.name[:LABEL], str(one.id)) for one in lists if wanted in one.name.casefold()][:SUGGESTED]


def pick_item(value: str, items: collections.abc.Iterable[plsremind.Item]) -> plsremind.Item | None:
    return _pick(value, [(item.id, item.title, item) for item in items])


def pick_list(value: str, lists: collections.abc.Iterable[plsremind.List]) -> plsremind.List | None:
    return _pick(value, [(one.id, one.name, one) for one in lists])


def _pick[T](value: str, known: list[tuple[plsremind.ID, str, T]]) -> T | None:
    # A suggestion sends the ID. Someone who typed on without choosing sent a name.
    wanted = value.strip()
    for key, _, thing in known:
        if key == wanted:
            return thing
    named = [thing for _, name, thing in known if name.casefold() == wanted.casefold()]
    return named[0] if len(named) == 1 else None


async def complete_item(
    ctx: lightbulb.AutocompleteContext[str], hub: Hub = lightbulb.di.INJECTED, clock: Clock = lightbulb.di.INJECTED
) -> None:
    user = hub.user_of(int(ctx.interaction.user.id))
    profile = hub.profile(user) if user is not None else None
    if user is None or profile is None:
        await ctx.respond([])
        return
    typed = str(ctx.focused.value or "")
    await ctx.respond(choices_for_items(hub.open_items(user), typed, clock.now(), profile.timezone))


async def complete_list(ctx: lightbulb.AutocompleteContext[str], hub: Hub = lightbulb.di.INJECTED) -> None:
    user = hub.user_of(int(ctx.interaction.user.id))
    if user is None:
        await ctx.respond([])
        return
    await ctx.respond(choices_for_lists(hub.lists(user), str(ctx.focused.value or "")))
