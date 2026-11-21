from __future__ import annotations

import lightbulb

from plsremind_discord.commands import common
from plsremind_discord.views import common as views_
from plsremind_discord.views.overview import OverviewView

loader = lightbulb.Loader()


async def todos(ctx: lightbulb.Context, tools: common.Tools, *, list_: str | None) -> None:
    who = await common.asking(ctx, tools)
    if who is None:
        return
    only = common.pick_list(list_, tools.hub.lists(who.user)) if list_ is not None else None
    if list_ is not None and only is None:
        await common.say(ctx, tools, tools.texts("error.unknown_list", who.locale))
        return
    view = OverviewView(
        user=views_.pack(who.user),
        only=views_.pack(only.id) if only is not None else None,
        speaks=str(ctx.interaction.locale),
    )
    await ctx.respond(**await tools.views.build(view), ephemeral=True)


@loader.command
class Todos(
    lightbulb.SlashCommand,
    name="command.todos.name",
    description="command.todos.description",
    localize=True,
    integration_types=common.INSTALLS,
    contexts=common.CONTEXTS,
):
    list = lightbulb.string(
        "option.list.name", "option.list.description", localize=True, default=None, autocomplete=common.complete_list
    )

    @lightbulb.invoke
    async def invoke(self, ctx: lightbulb.Context, tools: common.Tools) -> None:
        await todos(ctx, tools, list_=self.list)
