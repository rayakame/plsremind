from __future__ import annotations

import asyncio
import datetime
import typing

import msgspec
import plsremind

from plsremind_discord.hub import Hub
from plsremind_discord.timeparse import Day
from plsremind_discord.timeparse import Moment
from tests.support import things


class FakeHub(Hub):
    """A hub held in dictionaries. Every write is recorded in `calls` and changes what later reads return."""

    def __init__(self) -> None:
        self.ready = asyncio.Event()
        self.ready.set()
        self.people: dict[plsremind.ID, plsremind.ConnectionUser] = {}
        self.their_lists: dict[plsremind.ID, tuple[plsremind.List, ...]] = {}
        self.their_items: dict[plsremind.ID, dict[plsremind.ID, plsremind.Item]] = {}
        self.schedules: dict[plsremind.ID, tuple[plsremind.ScheduledReminder, ...]] = {}
        self.calls: list[tuple[str, dict[str, object]]] = []
        self.fail: dict[str, BaseException] = {}
        self.uncached: set[plsremind.ID] = set()
        self._made = 0

    def knows(
        self,
        user: plsremind.ConnectionUser,
        *items: plsremind.Item,
        lists: tuple[plsremind.List, ...] | None = None,
    ) -> None:
        self.people[user.id] = user
        self.their_lists[user.id] = (
            lists if lists is not None else (things.a_list(), things.a_list(things.WORK, "Work", 1))
        )
        self.their_items[user.id] = {item.id: item for item in items}

    def named(self, name: str) -> list[dict[str, object]]:
        return [arguments for called, arguments in self.calls if called == name]

    @typing.override
    async def start(self) -> None:
        self.ready.set()

    @typing.override
    async def close(self) -> None:
        self.ready.clear()

    @typing.override
    def user_ready(self, event: plsremind.UserReadyEvent) -> None:
        del event

    @typing.override
    def user_updated(self, event: plsremind.UserUpdateEvent) -> None:
        del event

    @typing.override
    def user_gone(self, event: plsremind.UserGoneEvent) -> None:
        self.people.pop(event.user_id, None)

    @typing.override
    def user_of(self, discord_id: int) -> plsremind.ID | None:
        for user in self.people.values():
            if str(discord_id) in user.external_ids:
                return user.id
        return None

    @typing.override
    def profile(self, user: plsremind.ID) -> plsremind.ConnectionUser | None:
        return self.people.get(user)

    @typing.override
    def discord_ids(self, user: plsremind.ID) -> tuple[int, ...]:
        found = self.people.get(user)
        return tuple(int(account) for account in found.external_ids if account.isdecimal()) if found else ()

    @typing.override
    def lists(self, user: plsremind.ID) -> tuple[plsremind.List, ...]:
        return self.their_lists.get(user, ())

    @typing.override
    def list_name(self, list_id: plsremind.ID) -> str | None:
        for lists in self.their_lists.values():
            for one in lists:
                if one.id == list_id:
                    return one.name
        return None

    @typing.override
    def open_items(self, user: plsremind.ID) -> tuple[plsremind.Item, ...]:
        return tuple(item for item in self._items(user).values() if item.status is plsremind.ItemStatus.OPEN)

    @typing.override
    def cached(self, user: plsremind.ID, item: plsremind.ID) -> plsremind.Item | None:
        return None if item in self.uncached else self._items(user).get(item)

    @typing.override
    async def item(self, user: plsremind.ID, item: plsremind.ID) -> plsremind.Item | None:
        self._called("item", user=user, item=item)
        return self._items(user).get(item)

    @typing.override
    async def schedule(self, user: plsremind.ID, item: plsremind.ID) -> tuple[plsremind.ScheduledReminder, ...]:
        self._called("schedule", user=user, item=item)
        return self.schedules.get(item, ())

    @typing.override
    async def add(
        self,
        user: plsremind.ID,
        *,
        title: str,
        note: str = "",
        list_id: plsremind.ID | None = None,
        priority: plsremind.Priority | None = None,
        due_at: datetime.datetime | None = None,
        due_date: datetime.date | None = None,
        nonce: str | None = None,
    ) -> plsremind.Item:
        self._called(
            "add",
            user=user,
            title=title,
            note=note,
            list_id=list_id,
            priority=priority,
            due_at=due_at,
            due_date=due_date,
            nonce=nonce,
        )
        self._made += 1
        made = things.item(
            500 + self._made,
            title=title,
            note=note,
            list_id=list_id or things.INBOX,
            priority=priority or plsremind.Priority.NORMAL,
            due_at=due_at,
            due_date=due_date,
        )
        self._items(user)[made.id] = made
        return made

    @typing.override
    async def remind(
        self, user: plsremind.ID, item: plsremind.ID, at: datetime.datetime, *, nonce: str | None = None
    ) -> plsremind.ManualReminder:
        self._called("remind", user=user, item=item, at=at, nonce=nonce)
        before = self._items(user)[item]
        made = things.reminder(len(before.reminders) + 1, item, at)
        self._items(user)[item] = msgspec.structs.replace(before, reminders=(*before.reminders, made))
        return made

    @typing.override
    async def set_done(
        self, user: plsremind.ID, item: plsremind.ID, *, done: bool, nonce: str | None = None
    ) -> plsremind.Item:
        self._called("set_done", user=user, item=item, done=done, nonce=nonce)
        changed = msgspec.structs.replace(
            self._items(user)[item],
            status=plsremind.ItemStatus.DONE if done else plsremind.ItemStatus.OPEN,
            completed_at=things.NOW if done else None,
        )
        self._items(user)[item] = changed
        return changed

    @typing.override
    async def edit(
        self,
        user: plsremind.ID,
        item: plsremind.ID,
        *,
        title: str,
        due: Moment | Day | None,
        note: str | None,
        nonce: str | None = None,
    ) -> plsremind.Item:
        self._called("edit", user=user, item=item, title=title, due=due, note=note, nonce=nonce)
        before = self._items(user)[item]
        changed = msgspec.structs.replace(
            before,
            title=title,
            note=before.note if note is None else note,
            due_at=due.at if isinstance(due, Moment) else None,
            due_date=due.date if isinstance(due, Day) else None,
            all_day=isinstance(due, Day),
        )
        self._items(user)[item] = changed
        return changed

    @typing.override
    async def delete(self, user: plsremind.ID, item: plsremind.ID, *, nonce: str | None = None) -> None:
        self._called("delete", user=user, item=item, nonce=nonce)
        self._items(user).pop(item, None)

    @typing.override
    async def tick(
        self, user: plsremind.ID, item: plsremind.ID, subitem: plsremind.ID, *, done: bool, nonce: str | None = None
    ) -> plsremind.Subitem:
        self._called("tick", user=user, item=item, subitem=subitem, done=done, nonce=nonce)
        before = self._items(user)[item]
        steps = tuple(msgspec.structs.replace(one, done=done) if one.id == subitem else one for one in before.subitems)
        self._items(user)[item] = msgspec.structs.replace(before, subitems=steps)
        return next(one for one in steps if one.id == subitem)

    @typing.override
    async def delivered(self, delivery: plsremind.ID) -> None:
        self._called("delivered", delivery=delivery)

    @typing.override
    async def failed(self, delivery: plsremind.ID, reason: str) -> None:
        self._called("failed", delivery=delivery, reason=reason)

    def _items(self, user: plsremind.ID) -> dict[plsremind.ID, plsremind.Item]:
        return self.their_items.setdefault(user, {})

    def _called(self, name: str, **arguments: object) -> None:
        self.calls.append((name, arguments))
        if name in self.fail:
            raise self.fail[name]
