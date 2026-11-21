from __future__ import annotations

import contextlib
import datetime

import plsremind

from plsremind_discord.hub import Hub


async def remind_me(hub: Hub, user: plsremind.ID, *, title: str, note: str, at: datetime.datetime) -> plsremind.Item:
    """Make an item without a deadline and one reminder for it. If the reminder fails, the item goes again."""
    item = await hub.add(user, title=title, note=note)
    try:
        await hub.remind(user, item.id, at)
    except BaseException:
        with contextlib.suppress(plsremind.PlsremindError):
            await hub.delete(user, item.id)
        raise
    return item
