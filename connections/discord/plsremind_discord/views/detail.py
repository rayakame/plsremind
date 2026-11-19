from __future__ import annotations

import asyncio
import contextlib
import datetime
import typing

import hikari
import msgspec
import plsremind
import risa
from risa import ui

from plsremind_discord import modals
from plsremind_discord import patience
from plsremind_discord import texts as texts_
from plsremind_discord.clock import Clock
from plsremind_discord.config import Config
from plsremind_discord.hub import Hub
from plsremind_discord.views import common
from plsremind_discord.views import overview

SUBITEMS: typing.Final = 8
NOTE_SHOWN: typing.Final = 600
STEP_SHOWN: typing.Final = 150
_LEAST: typing.Final = 8


@risa.register(name="detail", hooks=[common.owned])
class DetailView(common.Owned):
    item: bytes
    back_list: bytes | None = None
    back_page: int = 0
    confirm: bool = False
    speaks: risa.Prop[str | None] = None
    shown: risa.Prop[common.Shown | None] = None
    upcoming: risa.Prop[datetime.datetime | None] = None

    async def load(
        self, hub: Hub, config: Config, clock: Clock, texts: texts_.Texts, ctx: risa.ComponentContext | None
    ) -> None:
        speaks = str(ctx.interaction.locale) if ctx is not None else self.speaks
        view = await common.shown(
            hub, config, clock, texts, user=self.user, item=self.item, speaks=speaks, bounded=ctx is not None
        )
        self.shown = view
        self.upcoming = None
        if view.item is None or view.item.status is plsremind.ItemStatus.DONE:
            return
        try:
            # Bounded, because a click that opens a modal cannot ask Discord to wait.
            async with asyncio.timeout(patience.PATIENCE):
                schedule = await hub.schedule(common.unpack(self.user), view.item.id)
        except plsremind.PlsremindError, TimeoutError:
            return
        self.upcoming = min((one.fire_at for one in schedule), default=None)

    def render(self) -> ui.Layout:
        view = common.seen(self.shown)
        item, say, locale = view.item, view.texts, view.locale
        home = ui.Row(
            ui.Button(self.to_overview, label=say("button.to_overview", locale), style=hikari.ButtonStyle.SECONDARY)
        )
        if item is None:
            return ui.Container(ui.TextDisplay(say("reminder.gone", locale)), home)
        state = common.state_of(item, view.now)
        parts: list[ui.ContainerChild] = [ui.TextDisplay(self._text(view, item))]
        parts.extend(self._steps(view, item))
        parts.append(ui.Separator())
        if state != "done" and not self.confirm:
            options = common.snooze_options(view)
            parts.append(ui.Row(ui.TextSelect(self.snooze, *options, placeholder=say("snooze.placeholder", locale))))
        parts.append(self._actions(view, item, done=state == "done"))
        colour = {"done": common.DONE, "overdue": common.OVERDUE}.get(state, common.UPCOMING)
        return ui.Container(*parts, home, accent_color=colour)

    @risa.handler
    async def done(self, ctx: risa.ComponentContext, hub: Hub) -> None:
        view = common.seen(self.shown)
        self.shown = await common.finish(hub, view, self.user, done=True, origin=int(ctx.message.id))
        self.upcoming = None
        await ctx.rerender()

    @risa.handler
    async def undo(self, ctx: risa.ComponentContext, hub: Hub) -> None:
        view = common.seen(self.shown)
        self.shown = await common.finish(hub, view, self.user, done=False, origin=int(ctx.message.id))
        await ctx.rerender()

    @risa.handler
    async def tick(self, ctx: risa.ComponentContext, step: int, hub: Hub) -> None:
        view = common.seen(self.shown)
        item, wanted = view.item, common.from_arg(step)
        before = next((one for one in item.subitems if one.id == wanted), None) if item is not None else None
        if item is not None and before is not None:
            try:
                after = await hub.tick(
                    common.unpack(self.user), item.id, wanted, done=not before.done, origin=int(ctx.message.id)
                )
            except plsremind.NotFoundError:
                steps = tuple(one for one in item.subitems if one.id != wanted)
            else:
                steps = tuple(after if one.id == wanted else one for one in item.subitems)
            self.shown = msgspec.structs.replace(view, item=msgspec.structs.replace(item, subitems=steps))
        await ctx.rerender()

    @risa.handler(defer=risa.AutoDefer.OFF)
    async def snooze(self, ctx: risa.ComponentContext, hub: Hub, views: risa.Client) -> None:
        view = common.seen(self.shown)
        choice = ctx.values[0]
        if choice == common.CUSTOM:
            title, rows = modals.time_modal(ctx.rest, view.texts, view.locale)
            custom_id = modals.snooze_id(common.unpack(self.item), detail=self._back())
            await ctx.interaction.create_modal_response(title, custom_id, components=rows)
            return
        await ctx.acknowledge()
        try:
            until = await common.remind_later(hub, view, self.user, choice, origin=int(ctx.message.id))
        except common.RefusedError as refused:
            await ctx.rerender()
            await common.tell(ctx, views, *common.refusal(view.texts, view.locale, view.public_url, refused))
            return
        self.upcoming = until if self.upcoming is None else min(until, self.upcoming)
        await ctx.rerender()

    @risa.handler(defer=risa.AutoDefer.OFF)
    async def edit(self, ctx: risa.ComponentContext) -> None:
        view = common.seen(self.shown)
        if view.item is None:
            await ctx.rerender()
            return
        title, rows = modals.edit_modal(ctx.rest, view.texts, view.locale, view.item, view.zone)
        custom_id = modals.edit_id(view.item.id, self._back())
        await ctx.interaction.create_modal_response(title, custom_id, components=rows)

    @risa.handler
    async def delete(self, ctx: risa.ComponentContext) -> None:
        self.confirm = True
        await ctx.rerender()

    @risa.handler
    async def cancel(self, ctx: risa.ComponentContext) -> None:
        self.confirm = False
        await ctx.rerender()

    @risa.handler
    async def really_delete(self, ctx: risa.ComponentContext, hub: Hub) -> None:
        with contextlib.suppress(plsremind.NotFoundError):
            await hub.delete(common.unpack(self.user), common.unpack(self.item), origin=int(ctx.message.id))
        await self._leave(ctx)

    @risa.handler
    async def to_overview(self, ctx: risa.ComponentContext) -> None:
        await self._leave(ctx)

    async def _leave(self, ctx: risa.ComponentContext) -> None:
        await ctx.replace(
            overview.OverviewView(
                user=self.user, only=self.back_list, page=self.back_page, speaks=str(ctx.interaction.locale)
            )
        )

    def _back(self) -> tuple[plsremind.ID | None, int]:
        return (common.unpack(self.back_list) if self.back_list is not None else None, self.back_page)

    def _text(self, view: common.Shown, item: plsremind.Item) -> str:
        text = common.card(view, item)
        if self.upcoming is not None:
            when = texts_.stamp(self.upcoming, "F")
            text += f"\n{view.texts('detail.next_reminder', view.locale, when=when)}"
        if item.note.strip():
            text += f"\n{_quoted(item.note)}"
        return text

    def _steps(self, view: common.Shown, item: plsremind.Item) -> list[ui.ContainerChild]:
        if not item.subitems:
            return []
        say, locale = view.texts, view.locale
        ticked = sum(1 for step in item.subitems if step.done)
        parts: list[ui.ContainerChild] = [
            ui.Separator(),
            ui.TextDisplay(f"-# {say('detail.steps', locale, done=ticked, all=len(item.subitems))}"),
        ]
        for step in item.subitems[:SUBITEMS]:
            title = common.plain(step.title, STEP_SHOWN)
            toggle = ui.Button(
                risa.bind(self.tick, common.arg(step.id)),
                label=say("button.untick" if step.done else "button.tick", locale),
                style=hikari.ButtonStyle.SECONDARY if step.done else hikari.ButtonStyle.SUCCESS,
            )
            parts.append(ui.Section(ui.TextDisplay(f"~~{title}~~" if step.done else title), accessory=toggle))
        more = len(item.subitems) - SUBITEMS
        if more > 0:
            parts.append(ui.TextDisplay(f"-# {say('detail.more_steps', locale, count=more)}"))
        return parts

    def _actions(self, view: common.Shown, item: plsremind.Item, *, done: bool) -> ui.Row:
        say, locale = view.texts, view.locale
        if self.confirm:
            return ui.Row(
                ui.Button(
                    self.really_delete, label=say("button.really_delete", locale), style=hikari.ButtonStyle.DANGER
                ),
                ui.Button(self.cancel, label=say("button.cancel", locale), style=hikari.ButtonStyle.SECONDARY),
            )
        dashboard = ui.LinkButton(common.item_url(view, item), label=say("button.dashboard", locale))
        if done:
            undo = ui.Button(self.undo, label=say("button.undo", locale), style=hikari.ButtonStyle.SECONDARY)
            return ui.Row(undo, dashboard)
        return ui.Row(
            ui.Button(self.done, label=say("button.done", locale), style=hikari.ButtonStyle.SUCCESS),
            ui.Button(self.edit, label=say("button.edit", locale), style=hikari.ButtonStyle.SECONDARY),
            ui.Button(self.delete, label=say("button.delete", locale), style=hikari.ButtonStyle.DANGER),
            dashboard,
        )


def _quoted(note: str) -> str:
    lines: list[str] = []
    room = NOTE_SHOWN + 2
    rest = note.strip().splitlines()
    while rest and room > _LEAST:
        line = f"> {common.plain(rest.pop(0), room - 2)}"
        lines.append(line)
        room -= texts_.units(line) + 1
    if rest:
        lines.append("> …")
    return "\n".join(lines)
