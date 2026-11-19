from __future__ import annotations

import hikari
import risa
from risa import ui

from plsremind_discord import modals
from plsremind_discord import texts as texts_
from plsremind_discord.clock import Clock
from plsremind_discord.config import Config
from plsremind_discord.hub import Hub
from plsremind_discord.store import Store
from plsremind_discord.views import common


@risa.register(name="notice")
class Notice(risa.View):
    """A message that only says something, for what a view turns into when it ends."""

    text: str
    link: str = ""
    label: str = ""

    def render(self) -> ui.Layout:
        return common.notice_layout(self.text, self.link, self.label)


@risa.register(name="reminder", hooks=[common.owned])
class ReminderView(common.Owned):
    item: bytes
    shown: risa.Prop[common.Shown | None] = None

    async def load(self, hub: Hub, config: Config, clock: Clock, texts: texts_.Texts) -> None:
        self.shown = await common.shown(hub, config, clock, texts, user=self.user, item=self.item)

    def render(self) -> ui.Layout:
        view = common.seen(self.shown)
        item, say, locale = view.item, view.texts, view.locale
        if item is None:
            return ui.Container(ui.TextDisplay(say("reminder.gone", locale)))
        text = ui.TextDisplay(common.card(view, item))
        dashboard = ui.LinkButton(common.item_url(view, item), label=say("button.dashboard", locale))
        state = common.state_of(item, view.now)
        if state == "done":
            undo = ui.Button(self.undo, label=say("button.undo", locale), style=hikari.ButtonStyle.SECONDARY)
            return ui.Container(text, ui.Separator(), ui.Row(undo, dashboard), accent_color=common.DONE)
        buttons: list[ui.Button | ui.LinkButton] = [
            ui.Button(self.done, label=say("button.done", locale), style=hikari.ButtonStyle.SUCCESS)
        ]
        link = common.message_link(item.note)
        if link is not None:
            buttons.append(ui.LinkButton(link, label=say("button.message", locale)))
        later = ui.TextSelect(self.snooze, *common.snooze_options(view), placeholder=say("snooze.placeholder", locale))
        return ui.Container(
            text,
            ui.Separator(),
            ui.Row(later),
            ui.Row(*buttons, dashboard),
            accent_color=common.OVERDUE if state == "overdue" else common.UPCOMING,
        )

    @risa.handler
    async def done(self, ctx: risa.ComponentContext, hub: Hub) -> None:
        view = common.seen(self.shown)
        self.shown = await common.finish(hub, view, self.user, done=True, nonce=str(ctx.message.id))
        await ctx.rerender()

    @risa.handler
    async def undo(self, ctx: risa.ComponentContext, hub: Hub) -> None:
        view = common.seen(self.shown)
        self.shown = await common.finish(hub, view, self.user, done=False, nonce=str(ctx.message.id))
        await ctx.rerender()

    @risa.handler(defer=risa.AutoDefer.OFF)
    async def snooze(self, ctx: risa.ComponentContext, hub: Hub, store: Store) -> None:
        view = common.seen(self.shown)
        choice = ctx.values[0]
        if choice == common.CUSTOM:
            title, rows = modals.time_modal(ctx.rest, view.texts, view.locale)
            custom_id = modals.snooze_id(common.unpack(self.item), detail=None)
            await ctx.interaction.create_modal_response(title, custom_id, components=rows)
            return
        await ctx.acknowledge()
        try:
            until = await common.remind_later(hub, view, self.user, choice, nonce=str(ctx.message.id))
        except common.RefusedError as refused:
            await ctx.respond(view.texts(refused.key, view.locale), ephemeral=True)
            await ctx.rerender()
            return
        # Nothing is left to click, so the message is done with and no longer followed.
        await ctx.edit(common.notice_layout(*common.snoozed(view, until)))
        await store.forget_message(int(ctx.message.id))
