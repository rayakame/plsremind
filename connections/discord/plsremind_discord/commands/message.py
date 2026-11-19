from __future__ import annotations

import typing

import hikari
import lightbulb

from plsremind_discord import modals
from plsremind_discord.commands import common

TITLE_OFFERED: typing.Final = 100

loader = lightbulb.Loader()


async def remind_me(
    ctx: lightbulb.Context, tools: common.Tools, rest: hikari.api.RESTClient, *, target: hikari.Message
) -> None:
    who = await common.asking(ctx, tools)
    if who is None:
        return
    lines = (target.content or "").strip().splitlines()
    title = lines[0].strip()[:TITLE_OFFERED] if lines else ""
    if not title:
        title = tools.texts("modal.message.fallback", who.locale, name=target.author.display_name)[:TITLE_OFFERED]
    heading, rows = modals.message_modal(rest, tools.texts, who.locale, title=title)
    guild = int(ctx.guild_id) if ctx.guild_id is not None else None
    custom_id = modals.message_id(guild=guild, channel=int(target.channel_id), message=int(target.id))
    await ctx.respond_with_modal(heading, custom_id, components=rows)


@loader.command
class RemindMe(
    lightbulb.MessageCommand,
    name="command.remind_me.name",
    localize=True,
    integration_types=common.INSTALLS,
    contexts=common.CONTEXTS,
):
    @lightbulb.invoke
    async def invoke(self, ctx: lightbulb.Context, tools: common.Tools, rest: hikari.api.RESTClient) -> None:
        await remind_me(ctx, tools, rest, target=self.target)
