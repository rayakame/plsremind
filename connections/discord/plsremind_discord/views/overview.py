from __future__ import annotations

import collections.abc
import datetime
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
from plsremind_discord.views import common
from plsremind_discord.views import detail

ROWS: typing.Final = 8
LISTS_OFFERED: typing.Final = 24
_ALL: typing.Final = "all"
_GROUPS: typing.Final = ("overdue", "today", "soon", "none")
_STYLES: typing.Final[dict[str, texts_.Style]] = {"overdue": "R", "today": "t"}
_URGENCY: typing.Final = {plsremind.Priority.HIGH: 0, plsremind.Priority.NORMAL: 1, plsremind.Priority.LOW: 2}

type Group = typing.Literal["overdue", "today", "soon", "none"]


class Line(msgspec.Struct, frozen=True):
    item: plsremind.Item
    when: datetime.datetime | None
    group: Group


class Listed(msgspec.Struct, frozen=True, kw_only=True):
    """What the overview shows, read once before it is drawn."""

    items: tuple[plsremind.Item, ...]
    lists: tuple[plsremind.List, ...]
    locale: hikari.Locale
    zone: zoneinfo.ZoneInfo
    now: datetime.datetime
    public_url: str
    texts: texts_.Texts


def lines(
    items: collections.abc.Iterable[plsremind.Item], now: datetime.datetime, zone: zoneinfo.ZoneInfo
) -> list[Line]:
    today = now.astimezone(zone).date()
    found: list[Line] = []
    for item in items:
        ahead = min((one.remind_at for one in item.reminders if one.remind_at > now), default=None)
        when = item.due_at or ahead
        if when is None:
            group: Group = "none"
        elif item.due_at is not None and item.due_at <= now:
            group = "overdue"
        else:
            group = "today" if when.astimezone(zone).date() == today else "soon"
        found.append(Line(item=item, when=when, group=group))
    far = datetime.datetime.max.replace(tzinfo=datetime.UTC)
    return sorted(
        found,
        key=lambda line: (
            _GROUPS.index(line.group),
            line.when or far,
            _URGENCY.get(line.item.priority, 1),
            line.item.title.casefold(),
        ),
    )


@risa.register(name="overview", hooks=[common.owned])
class OverviewView(common.Owned):
    only: bytes | None = None
    page: int = 0
    speaks: risa.Prop[str | None] = None
    shown: risa.Prop[Listed | None] = None

    async def load(
        self, hub: Hub, config: Config, clock: Clock, texts: texts_.Texts, ctx: risa.ComponentContext | None
    ) -> None:
        user = common.unpack(self.user)
        profile = hub.profile(user)
        speaks = str(ctx.interaction.locale) if ctx is not None else self.speaks
        self.shown = Listed(
            items=hub.open_items(user),
            lists=hub.lists(user),
            locale=texts_.locale_for(profile, speaks),
            zone=profile.timezone if profile is not None else zoneinfo.ZoneInfo("UTC"),
            now=clock.now(),
            public_url=config.public_url,
            texts=texts,
        )
        # Items may have gone since the page was drawn; paging goes on from the last page there is.
        self.page = min(self.page, _pages(self._every()) - 1)

    def render(self) -> ui.Layout:
        view = self._seen()
        say, locale = view.texts, view.locale
        only = common.unpack(self.only) if self.only is not None else None
        every = self._every()
        pages = _pages(every)
        page = min(self.page, pages - 1)
        overdue = sum(1 for line in every if line.group == "overdue")
        summary = say("overview.summary", locale, open=len(every), overdue=overdue)
        if pages > 1:
            summary += f" · {say('overview.page', locale, page=page + 1, pages=pages)}"
        parts: list[ui.ContainerChild] = [ui.TextDisplay(f"## {say('overview.title', locale)}\n-# {summary}")]
        group: Group | None = None
        for line in every[page * ROWS : (page + 1) * ROWS]:
            if line.group != group:
                group = line.group
                parts.append(ui.TextDisplay(f"-# {say(f'group.{group}', locale).upper()}"))
            parts.append(self._row(view, line))
        if not every:
            parts.append(ui.TextDisplay(say("overview.empty", locale)))
        place = f"{view.public_url}/lists/{only}" if only is not None else view.public_url
        return ui.Container(
            *parts,
            ui.Separator(),
            ui.Row(ui.TextSelect(self.choose, *self._lists(view, only))),
            ui.Row(
                ui.Button(
                    self.back, label=say("button.back", locale), style=hikari.ButtonStyle.SECONDARY, disabled=page == 0
                ),
                ui.Button(
                    self.forward,
                    label=say("button.next", locale),
                    style=hikari.ButtonStyle.SECONDARY,
                    disabled=page >= pages - 1,
                ),
                ui.LinkButton(place, label=say("button.dashboard", locale)),
            ),
        )

    @risa.handler
    async def open(self, ctx: risa.ComponentContext, item: int) -> None:
        await ctx.replace(
            detail.DetailView(
                user=self.user,
                item=common.pack(common.from_arg(item)),
                back_list=self.only,
                back_page=self.page,
                speaks=str(ctx.interaction.locale),
            )
        )

    @risa.handler
    async def choose(self, ctx: risa.ComponentContext) -> None:
        chosen = ctx.values[0]
        self.only = None if chosen == _ALL else uuid.UUID(hex=chosen).bytes
        self.page = 0
        await ctx.rerender()

    @risa.handler
    async def back(self, ctx: risa.ComponentContext) -> None:
        self.page = max(0, self.page - 1)
        await ctx.rerender()

    @risa.handler
    async def forward(self, ctx: risa.ComponentContext) -> None:
        self.page += 1
        await ctx.rerender()

    def _every(self) -> list[Line]:
        view = self._seen()
        only = common.unpack(self.only) if self.only is not None else None
        return lines((item for item in view.items if only is None or item.list_id == only), view.now, view.zone)

    def _seen(self) -> Listed:
        if self.shown is None:
            msg = "The view was drawn before it was loaded."
            raise RuntimeError(msg)
        return self.shown

    def _row(self, view: Listed, line: Line) -> ui.Section:
        say, locale, item = view.texts, view.locale, line.item
        facts: list[str] = []
        if item.due_date is not None:
            facts.append(texts_.day(item.due_date, locale))
        elif line.when is not None:
            moment = texts_.stamp(line.when, _STYLES.get(line.group, "f"))
            facts.append(moment if item.due_at is not None else say("overview.reminder", locale, when=moment))
        name = next((one.name for one in view.lists if one.id == item.list_id), "")
        if name:
            facts.append(common.plain(name, common.LIST_SHOWN))
        if item.priority is plsremind.Priority.HIGH:
            facts.append(say("priority.high", locale))
        if item.subitems:
            ticked = sum(1 for step in item.subitems if step.done)
            facts.append(say("overview.steps", locale, done=ticked, all=len(item.subitems)))
        text = f"**{common.plain(item.title, _TITLE)}**" + (f"\n-# {' · '.join(facts)}" if facts else "")
        look = ui.Button(
            risa.bind(self.open, common.arg(item.id)),
            label=say("button.open", locale),
            style=hikari.ButtonStyle.SECONDARY,
        )
        return ui.Section(ui.TextDisplay(text), accessory=look)

    def _lists(self, view: Listed, only: plsremind.ID | None) -> list[ui.SelectOption]:
        options = [ui.SelectOption(view.texts("filter.all", view.locale), _ALL, default=only is None)]
        options.extend(
            ui.SelectOption(one.name[:_LABEL], uuid.UUID(one.id).hex, default=one.id == only)
            for one in view.lists[:LISTS_OFFERED]
        )
        return options


_TITLE: typing.Final = 80
_LABEL: typing.Final = 100


def _pages(every: list[Line]) -> int:
    return max(1, -(-len(every) // ROWS))
