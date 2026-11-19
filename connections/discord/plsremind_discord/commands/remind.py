from __future__ import annotations

import lightbulb
import plsremind

from plsremind_discord import actions
from plsremind_discord import texts as texts_
from plsremind_discord import timeparse
from plsremind_discord.commands import common
from plsremind_discord.views import common as views_

loader = lightbulb.Loader()


async def remind(
    ctx: lightbulb.Context, tools: common.Tools, *, when: str, what: str, note: str | None, public: bool
) -> None:
    who = await common.asking(ctx, tools)
    if who is None:
        return
    say, now = tools.texts, tools.clock.now()
    title = what.strip()
    public = common.for_all(ctx, asked=public)
    try:
        at = timeparse.moment(timeparse.parse(when, now=now, zone=who.zone), now=now, zone=who.zone)
        making = actions.remind_me(tools.hub, who.user, title=title, note=(note or "").strip(), at=at)
        item = await common.patient(ctx, making, public=public)
    except (timeparse.TimeError, plsremind.PlsremindError) as error:
        await common.say(ctx, tools, say(common.problem(error), who.locale))
        return
    shown = views_.plain(title, views_.TITLE_SHOWN)
    text = say("ack.remind", who.locale, when=texts_.stamp(at, "F"), title=shown)
    await common.confirm(ctx, tools, who, text, item, public=public)


@loader.command
class Remind(
    lightbulb.SlashCommand,
    name="command.remind.name",
    description="command.remind.description",
    localize=True,
    integration_types=common.INSTALLS,
    contexts=common.CONTEXTS,
):
    when = lightbulb.string("option.when.name", "option.when.description", localize=True)
    what = lightbulb.string("option.what.name", "option.what.description", localize=True, max_length=500)
    note = lightbulb.string("option.note.name", "option.note.description", localize=True, default=None, max_length=6000)
    public_ack = lightbulb.boolean(
        "option.public_ack.name", "option.public_ack.description", localize=True, default=False
    )

    @lightbulb.invoke
    async def invoke(self, ctx: lightbulb.Context, tools: common.Tools) -> None:
        await remind(ctx, tools, when=self.when, what=self.what, note=self.note, public=self.public_ack)
