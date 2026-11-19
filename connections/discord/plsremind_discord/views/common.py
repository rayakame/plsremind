from __future__ import annotations

import datetime
import re
import typing
import uuid
import zoneinfo

import hikari
import msgspec
import plsremind
import risa
from risa import ui

from plsremind_discord import texts as texts_
from plsremind_discord.clock import Clock
from plsremind_discord.config import Config
from plsremind_discord.hub import Hub

UPCOMING: typing.Final = 0xE0A526
OVERDUE: typing.Final = 0xDA373C
DONE: typing.Final = 0x248046
TITLE_SHOWN: typing.Final = 300
CUSTOM: typing.Final = "custom"
SNOOZE: typing.Final = ("15m", "1h", "evening", "morning", CUSTOM)
_EVENING: typing.Final = datetime.time(20, 0)
_MORNING: typing.Final = datetime.time(8, 0)
_UTC: typing.Final = zoneinfo.ZoneInfo("UTC")
_LINK: typing.Final = re.compile(r"https://(?:(?:ptb|canary)\.)?discord\.com/channels/(?:@me|\d+)/\d+/\d+")
_MARKDOWN: typing.Final = re.compile(r"([\\*_~`|#\[\]<>])")
GONE: typing.Final = "reminder.gone"
_PAST: typing.Final = "error.time_past"
_REFUSALS: typing.Final = {"reminder_limit": "error.reminder_limit", "item_done": "reminder.item_done"}

type State = typing.Literal["upcoming", "overdue", "plain", "done", "gone"]


class Shown(msgspec.Struct, frozen=True, kw_only=True):
    """What a view shows of an item, read once before it is drawn."""

    item: plsremind.Item | None
    list_name: str
    locale: hikari.Locale
    zone: zoneinfo.ZoneInfo
    now: datetime.datetime
    public_url: str
    texts: texts_.Texts


class Owned(risa.View):
    """A view that belongs to one user of the hub."""

    user: bytes

    @classmethod
    @typing.override
    async def on_outdated(cls, ctx: risa.ComponentContext, texts: texts_.Texts = risa.INJECTED) -> None:
        locale = texts_.locale_for(None, str(ctx.interaction.locale))
        await ctx.respond(texts("error.outdated", locale), ephemeral=True)


class NotYoursError(Exception):
    pass


class StartingError(Exception):
    pass


class RefusedError(Exception):
    def __init__(self, key: str) -> None:
        super().__init__(key)
        self.key: typing.Final = key


@risa.hook(risa.ExecutionSteps.CHECKS)
async def owned(  # ruff: ignore[unused-async]
    _pipeline: risa.ExecutionPipeline, ctx: risa.ComponentContext, view: Owned, hub: Hub
) -> None:
    if not hub.ready.is_set():
        raise StartingError
    # risa's own check for this refuses every click on a message the bot sent by itself.
    if hub.user_of(int(ctx.user.id)) != unpack(view.user):
        raise NotYoursError


def pack(value: plsremind.ID) -> bytes:
    return uuid.UUID(value).bytes


def unpack(raw: bytes) -> plsremind.ID:
    return plsremind.ID(str(uuid.UUID(bytes=raw)))


def arg(value: plsremind.ID) -> int:
    return uuid.UUID(value).int


def from_arg(number: int) -> plsremind.ID:
    return plsremind.ID(str(uuid.UUID(int=number)))


def seen(shown: Shown | None) -> Shown:
    if shown is None:
        msg = "The view was drawn before it was loaded."
        raise RuntimeError(msg)
    return shown


async def shown(
    hub: Hub,
    config: Config,
    clock: Clock,
    texts: texts_.Texts,
    *,
    user: bytes,
    item: bytes,
    speaks: str | None = None,
) -> Shown:
    user_id = unpack(user)
    profile = hub.profile(user_id)
    found = await hub.item(user_id, unpack(item)) if profile is not None else None
    return Shown(
        item=found,
        list_name=(hub.list_name(found.list_id) or "") if found is not None else "",
        locale=texts_.locale_for(profile, speaks),
        zone=profile.timezone if profile is not None else _UTC,
        now=clock.now(),
        public_url=config.public_url,
        texts=texts,
    )


def state_of(item: plsremind.Item | None, now: datetime.datetime) -> State:
    if item is None:
        return "gone"
    if item.status is plsremind.ItemStatus.DONE:
        return "done"
    if item.due_at is None:
        return "plain"
    return "overdue" if item.due_at <= now else "upcoming"


def plain(text: str, limit: int) -> str:
    line = " ".join(text.split())
    if len(line) > limit:
        line = line[: limit - 1] + "…"
    return _MARKDOWN.sub(r"\\\1", line)


def message_link(note: str) -> str | None:
    found = _LINK.search(note)
    return found.group(0) if found is not None else None


def item_url(view: Shown, item: plsremind.Item) -> str:
    return f"{view.public_url}/items/{item.id}"


def card(view: Shown, item: plsremind.Item) -> str:
    """The three lines every view opens an item with: what is up, the title, where it belongs."""
    say, locale = view.texts, view.locale
    title = plain(item.title, TITLE_SHOWN)
    if item.status is plsremind.ItemStatus.DONE:
        finished = texts_.stamp(item.completed_at or view.now, "R")
        return f"-# {say('reminder.done', locale, when=finished)}\n## ~~{title}~~\n{view.list_name}"
    return f"-# {_heading(view, item)}\n## {title}\n{' · '.join(_facts(view, item))}"


def _heading(view: Shown, item: plsremind.Item) -> str:
    say, locale = view.texts, view.locale
    if item.due_at is None:
        return say("reminder.plain", locale)
    late = item.due_at <= view.now
    if item.due_date is not None:
        key = "reminder.overdue_day" if late else "reminder.upcoming_day"
        return say(key, locale, day=texts_.day(item.due_date, locale))
    return say("reminder.overdue" if late else "reminder.upcoming", locale, when=texts_.stamp(item.due_at, "R"))


def _facts(view: Shown, item: plsremind.Item) -> list[str]:
    facts: list[str] = []
    if item.due_date is not None:
        facts.append(f"**{texts_.day(item.due_date, view.locale)}**")
    elif item.due_at is not None:
        facts.append(f"**{texts_.stamp(item.due_at, 'F')}**")
    if view.list_name:
        facts.append(plain(view.list_name, TITLE_SHOWN))
    if item.priority in {plsremind.Priority.LOW, plsremind.Priority.HIGH}:
        facts.append(view.texts(f"priority.{item.priority.value}", view.locale))
    return facts


def snooze_at(choice: str, now: datetime.datetime, zone: zoneinfo.ZoneInfo) -> datetime.datetime | None:
    # Reckoned in UTC: within one zone, datetimes add up and compare by what the clock shows.
    instant = now.astimezone(datetime.UTC)
    today = instant.astimezone(zone).date()
    if choice == "15m":
        return (instant + datetime.timedelta(minutes=15)).astimezone(zone)
    if choice == "1h":
        return (instant + datetime.timedelta(hours=1)).astimezone(zone)
    if choice == "evening":
        return _wall(today, _EVENING, zone)
    if choice == "morning":
        return _wall(today + datetime.timedelta(days=1), _MORNING, zone)
    return None


def _wall(date: datetime.date, clock: datetime.time, zone: zoneinfo.ZoneInfo) -> datetime.datetime:
    return datetime.datetime.combine(date, clock, tzinfo=zone).astimezone(datetime.UTC).astimezone(zone)


def snooze_options(view: Shown) -> list[ui.SelectOption]:
    options: list[ui.SelectOption] = []
    for choice in SNOOZE:
        at = snooze_at(choice, view.now, view.zone)
        if choice == CUSTOM or (at is not None and at > view.now.astimezone(datetime.UTC)):
            options.append(ui.SelectOption(view.texts(f"snooze.{choice}", view.locale), choice))
    return options


async def finish(hub: Hub, view: Shown, user: bytes, *, done: bool, origin: int | None) -> Shown:
    item = view.item
    if item is None or (item.status is plsremind.ItemStatus.DONE) == done:
        return view
    try:
        changed = await hub.set_done(unpack(user), item.id, done=done, origin=origin)
    except plsremind.NotFoundError:
        return msgspec.structs.replace(view, item=None)
    return msgspec.structs.replace(view, item=changed)


async def remind_at(
    hub: Hub, user: plsremind.ID, item: plsremind.ID, at: datetime.datetime, *, origin: int | None
) -> None:
    try:
        await hub.remind(user, item, at, origin=origin)
    except plsremind.NotFoundError:
        raise RefusedError(GONE) from None
    except plsremind.ConflictError as error:
        if error.code not in _REFUSALS:
            raise
        raise RefusedError(_REFUSALS[error.code]) from None


async def remind_later(hub: Hub, view: Shown, user: bytes, choice: str, *, origin: int | None) -> datetime.datetime:
    if view.item is None:
        raise RefusedError(GONE)
    at = snooze_at(choice, view.now, view.zone)
    if at is None or at <= view.now.astimezone(datetime.UTC):
        raise RefusedError(_PAST)
    await remind_at(hub, unpack(user), view.item.id, at, origin=origin)
    return at


def snoozed(view: Shown, until: datetime.datetime) -> tuple[str, str, str]:
    """The text, the link and the link's label of the notice a snoozed reminder turns into."""
    say, locale = view.texts, view.locale
    item = view.item
    title = plain(item.title, TITLE_SHOWN) if item is not None else ""
    again = say("reminder.snoozed_until", locale, when=texts_.stamp(until, "F"))
    text = f"-# {say('reminder.snoozed', locale)}\n## {title}\n{again}"
    return text, item_url(view, item) if item is not None else view.public_url, say("button.dashboard", locale)


def confirmation(
    hub: Hub, config: Config, texts: texts_.Texts, locale: hikari.Locale, *, text: str, item: plsremind.Item
) -> tuple[str, str, str]:
    """The text, the link and the link's label of a confirmation only the user sees."""
    where = hub.list_name(item.list_id)
    if where:
        text += f"\n-# {plain(where, TITLE_SHOWN)}"
    return text, f"{config.public_url}/items/{item.id}", texts("button.dashboard", locale)


def notice_layout(text: str, link: str, label: str) -> ui.Layout:
    if not link:
        return ui.Container(ui.TextDisplay(text))
    return ui.Container(ui.TextDisplay(text), ui.Separator(), ui.Row(ui.LinkButton(link, label=label)))


def unlinked(texts: texts_.Texts, locale: hikari.Locale, config: Config) -> tuple[str, str, str]:
    """The text, the link and the link's label of the hint for a Discord account the hub does not know."""
    return texts("error.not_linked", locale), f"{config.public_url}/settings", texts("button.settings", locale)
