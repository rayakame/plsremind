from __future__ import annotations

import lightbulb
import plsremind

from plsremind_discord import texts as texts_
from plsremind_discord import timeparse
from plsremind_discord.commands import common
from plsremind_discord.views import common as views_

loader = lightbulb.Loader()


async def deadline(
    ctx: lightbulb.Context,
    tools: common.Tools,
    *,
    due: str,
    what: str,
    note: str | None,
    priority: str | None,
    list_: str | None,
    public: bool,
) -> None:
    who = await common.asking(ctx, tools)
    if who is None:
        return
    say, hub = tools.texts, tools.hub
    target = common.pick_list(list_, hub.lists(who.user)) if list_ is not None else None
    if list_ is not None and target is None:
        await common.say(ctx, tools, say("error.unknown_list", who.locale))
        return
    title = what.strip()
    try:
        parsed = timeparse.parse(due, now=tools.clock.now(), zone=who.zone)
        item = await hub.add(
            who.user,
            title=title,
            note=(note or "").strip(),
            list_id=target.id if target is not None else None,
            priority=plsremind.Priority(priority) if priority is not None else None,
            due_at=parsed.at if isinstance(parsed, timeparse.Moment) else None,
            due_date=parsed.date if isinstance(parsed, timeparse.Day) else None,
        )
    except (timeparse.TimeError, plsremind.PlsremindError) as error:
        await common.say(ctx, tools, say(common.problem(error), who.locale))
        return
    when = texts_.stamp(parsed.at, "F") if isinstance(parsed, timeparse.Moment) else texts_.day(parsed.date, who.locale)
    text = say("ack.deadline", who.locale, when=when, title=views_.plain(title, views_.TITLE_SHOWN))
    if public:
        await common.say(ctx, tools, text, public=True)
        return
    link = f"{tools.config.public_url}/items/{item.id}"
    await common.say(ctx, tools, text, link, say("button.dashboard", who.locale))


@loader.command
class Deadline(
    lightbulb.SlashCommand,
    name="command.deadline.name",
    description="command.deadline.description",
    localize=True,
    integration_types=common.INSTALLS,
    contexts=common.CONTEXTS,
):
    due = lightbulb.string("option.due.name", "option.due.description", localize=True)
    what = lightbulb.string("option.what.name", "option.what.description", localize=True, max_length=500)
    note = lightbulb.string("option.note.name", "option.note.description", localize=True, default=None, max_length=6000)
    priority = lightbulb.string(
        "option.priority.name",
        "option.priority.description",
        localize=True,
        default=None,
        choices=[
            lightbulb.Choice("choice.priority.low", "low", localize=True),
            lightbulb.Choice("choice.priority.normal", "normal", localize=True),
            lightbulb.Choice("choice.priority.high", "high", localize=True),
        ],
    )
    list = lightbulb.string(
        "option.list.name", "option.list.description", localize=True, default=None, autocomplete=common.complete_list
    )
    public_ack = lightbulb.boolean(
        "option.public_ack.name", "option.public_ack.description", localize=True, default=False
    )

    @lightbulb.invoke
    async def invoke(self, ctx: lightbulb.Context, tools: common.Tools) -> None:
        await deadline(
            ctx,
            tools,
            due=self.due,
            what=self.what,
            note=self.note,
            priority=self.priority,
            list_=self.list,
            public=self.public_ack,
        )
