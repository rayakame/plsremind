from __future__ import annotations

import asyncio
import collections.abc
import contextlib
import logging
import typing

import hikari
import plsremind
import risa

from plsremind_discord.hub import Hub
from plsremind_discord.hub import accounts_of
from plsremind_discord.hub import origin_of
from plsremind_discord.store import Store
from plsremind_discord.views.reminder import ReminderView

QUIET: typing.Final = 2.0
SHOWN_FIELDS: typing.Final = ("title", "due_at", "due_date", "status", "priority", "list_id", "note")
_LOGGER: typing.Final = logging.getLogger(__name__)


class Opens(typing.Protocol):
    """The part of the risa client that redraws a message outside a click."""

    def open[T: risa.View](
        self, channel: int, message: int, cls: type[T], /
    ) -> contextlib.AbstractAsyncContextManager[T]: ...


class Sync:
    """Keeps reminder messages true to the hub when an item changes somewhere else."""

    def __init__(
        self,
        *,
        hub: Hub,
        views: Opens,
        store: Store,
        sleep: collections.abc.Callable[[float], collections.abc.Awaitable[None]] = asyncio.sleep,
    ) -> None:
        self._hub: typing.Final = hub
        self._views: typing.Final = views
        self._store: typing.Final = store
        self._sleep: typing.Final = sleep
        self._asked: dict[int, int] = {}
        self._tasks: dict[int, asyncio.Task[None]] = {}

    @property
    def pending(self) -> frozenset[int]:
        """The messages that are waiting to be redrawn or are being redrawn."""
        return frozenset(self._tasks)

    async def item_changed(self, event: plsremind.ItemUpdateEvent | plsremind.ItemDeleteEvent) -> None:
        if isinstance(event, plsremind.ItemUpdateEvent):
            old, new = event.old_item, event.item
            if old is not None and all(getattr(old, name) == getattr(new, name) for name in SHOWN_FIELDS):
                return
            item_id = new.id
        else:
            item_id = event.item_id
        for row in await self._store.messages_for_item(item_id):
            # The click that made this change has redrawn its own message already.
            if origin_of(event.nonce) != row.message_id:
                self._redraw_soon(row.message_id, row.channel_id)

    async def list_changed(self, event: plsremind.ListUpdateEvent) -> None:
        if event.old_list is not None and event.old_list.name == event.list.name:
            return
        for row in await self._store.messages_for_user(event.user_id):
            item = await self._hub.item(event.user_id, plsremind.ID(row.item_id))
            if item is not None and item.list_id == event.list.id:
                self._redraw_soon(row.message_id, row.channel_id)

    async def user_ready(self, event: plsremind.UserReadyEvent) -> None:
        profile = self._hub.profile(event.user_id)
        if profile is not None:
            await self._drop_unlinked(event.user_id, accounts_of(profile.external_ids))
        for row in await self._store.messages_for_user(event.user_id):
            item = self._hub.cached(event.user_id, plsremind.ID(row.item_id))
            if item is None or self._hub.version(item) != row.shown_at:
                self._redraw_soon(row.message_id, row.channel_id)

    async def user_changed(self, event: plsremind.UserUpdateEvent) -> None:
        if isinstance(event.user, plsremind.ConnectionUser):
            await self._drop_unlinked(event.user_id, accounts_of(event.user.external_ids))

    async def user_gone(self, event: plsremind.UserGoneEvent) -> None:
        for row in await self._store.messages_for_user(event.user_id):
            self._call_off(row.message_id)
        await self._store.forget_user(event.user_id)

    async def close(self) -> None:
        tasks = list(self._tasks.values())
        self._tasks.clear()
        self._asked.clear()
        for task in tasks:
            task.cancel()
        await asyncio.gather(*tasks, return_exceptions=True)

    async def _drop_unlinked(self, user: plsremind.ID, linked: collections.abc.Collection[int]) -> None:
        # An account that was unlinked must not keep seeing what becomes of the user's items.
        for row in await self._store.messages_for_user(user):
            if row.account_id not in linked:
                self._call_off(row.message_id)
                await self._store.forget_message(row.message_id)

    def _call_off(self, message: int) -> None:
        # A redraw already waiting would still show the item to whoever sees the message.
        task = self._tasks.pop(message, None)
        self._asked.pop(message, None)
        if task is not None and task is not asyncio.current_task():
            task.cancel()

    def _redraw_soon(self, message: int, channel: int) -> None:
        self._asked[message] = self._asked.get(message, 0) + 1
        if message not in self._tasks:
            self._tasks[message] = asyncio.create_task(self._follow(message, channel), name=f"redraw-{message}")

    async def _follow(self, message: int, channel: int) -> None:
        try:
            while True:
                asked = self._asked[message]
                await self._sleep(QUIET)
                if self._asked[message] != asked:
                    continue
                await self._redraw(message, channel)
                if self._asked[message] == asked:
                    return
        finally:
            if self._tasks.get(message) is asyncio.current_task():
                del self._tasks[message]
                self._asked.pop(message, None)

    async def _redraw(self, message: int, channel: int) -> None:
        try:
            async with self._views.open(channel, message, ReminderView) as view:
                pass
        except risa.MessageOutdatedError, hikari.NotFoundError, hikari.ForbiddenError:
            await self._store.forget_message(message)
            return
        except Exception:
            _LOGGER.exception("message %s could not be redrawn", message)
            return
        item = view.shown.item if view.shown is not None else None
        await self._store.stamp(message, self._hub.version(item) if item is not None else None)
