from __future__ import annotations

import lightbulb
import plsremind

from plsremind_discord.commands import common
from plsremind_discord.views import common as views_
from plsremind_discord.views.detail import DetailView

loader = lightbulb.Loader()


async def done(ctx: lightbulb.Context, tools: common.Tools, *, item: str) -> None:
    who = await common.asking(ctx, tools)
    if who is None:
        return
    found = common.pick_item(item, tools.hub.open_items(who.user))
    if found is None:
        await common.say(ctx, tools, tools.texts("error.unknown_item", who.locale))
        return
    try:
        await common.patient(ctx, tools.hub.set_done(who.user, found.id, done=True))
    except plsremind.PlsremindError as error:
        await common.say(ctx, tools, tools.texts(common.problem(error), who.locale))
        return
    view = DetailView(user=views_.pack(who.user), item=views_.pack(found.id), speaks=str(ctx.interaction.locale))
    await ctx.respond(**await tools.views.build(view), ephemeral=True)


@loader.command
class Done(
    lightbulb.SlashCommand,
    name="command.done.name",
    description="command.done.description",
    localize=True,
    integration_types=common.INSTALLS,
    contexts=common.CONTEXTS,
):
    item = lightbulb.string(
        "option.item.name", "option.item.description", localize=True, autocomplete=common.complete_item
    )

    @lightbulb.invoke
    async def invoke(self, ctx: lightbulb.Context, tools: common.Tools) -> None:
        await done(ctx, tools, item=self.item)
