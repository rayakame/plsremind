from __future__ import annotations

import typing

import msgspec

from plsremind import events
from plsremind.cache import Cache
from plsremind.enums import CredentialKind
from plsremind.enums import Intents
from plsremind.enums import ItemStatus
from plsremind.internal import wire
from plsremind.internal.state import UserState

if typing.TYPE_CHECKING:
    import collections.abc

    from plsremind.models import Item
    from plsremind.models import Principal
    from plsremind.types import ID


class Applied(typing.NamedTuple):
    event: events.Event
    ready: ID | None


def _sharing(state: UserState, item: Item) -> Item:
    # Every item arrives with its own copy of its list's ID; pointing at the list's saves that string.
    known = state.lists.get(item.list_id)
    if known is not None:
        msgspec.structs.force_setattr(item, "list_id", known.id)
    return item


class CacheStore(Cache):
    def begin(self, principal: Principal, users: collections.abc.Iterable[ID], intents: Intents) -> tuple[ID, ...]:
        self._self = principal.user_id
        self._connection = self._connection or principal.kind is CredentialKind.CONNECTION
        self._history = Intents.HISTORY in intents
        self._pending.clear()
        named = set(users)
        dropped = tuple(user for user in self._users if user not in named)
        for user in dropped:
            self._drop(user)
        return dropped

    def clear(self) -> None:
        self._users.clear()
        self._pending.clear()
        self._items.clear()
        self._lists.clear()
        self._users_view = None

    def apply(self, event: events.Event) -> Applied:
        if isinstance(event, events.UserSyncEvent):
            return Applied(event, self._sync(event))
        if isinstance(event, events.UserGoneEvent):
            self._pending.pop(event.user_id, None)
            self._drop(event.user_id)
            return Applied(event, None)
        if isinstance(event, events.ChangeEvent):
            state = self._users.get(event.user_id)
            if state is not None:
                return Applied(wire.with_old(event, self._change(state, event)), None)
        return Applied(event, None)

    def _sync(self, event: events.UserSyncEvent) -> ID | None:
        user = event.user_id
        if event.chunk.index == 0:
            state = self._pending[user] = UserState()
            state.profile = event.user
            state.lists = {entry.id: entry for entry in event.lists or ()}
            state.plans = {plan.priority: plan for plan in event.plans or ()}
        else:
            pending = self._pending.get(user)
            if pending is None:
                return None
            state = pending
        for item in event.items or ():
            state.items[item.id] = _sharing(state, item)
        for item in event.history or ():
            state.add_done(_sharing(state, item))
        if event.history_cursor is not msgspec.UNSET:
            state.history_cursor = event.history_cursor
        if event.chunk.index < event.chunk.count - 1:
            return None
        del self._pending[user]
        self._drop(user)
        self._users[user] = state
        self._users_view = None
        self._items.update(state.items)
        self._items.update(state.done)
        self._lists.update(state.lists)
        return user

    def _drop(self, user: ID) -> None:
        state = self._users.pop(user, None)
        if state is None:
            return
        self._users_view = None
        for item_id in (*state.items, *state.done):
            self._items.pop(item_id, None)
        for list_id in state.lists:
            self._lists.pop(list_id, None)

    def _change(self, state: UserState, event: events.ChangeEvent) -> object | None:  # ruff: ignore[complex-structure, too-many-return-statements, too-many-branches]
        match event:
            case events.ItemCreateEvent() | events.ItemUpdateEvent():
                return self._put_item(state, event.item)
            case events.ItemDeleteEvent():
                old = state.items.pop(event.item_id, None)
                if old is None:
                    old = state.remove_done(event.item_id)
                else:
                    state.items_view = None
                if old is not None:
                    del self._items[event.item_id]
                return old
            case events.ListCreateEvent() | events.ListUpdateEvent():
                old = state.lists.get(event.list.id)
                state.lists[event.list.id] = self._lists[event.list.id] = event.list
                state.lists_view = None
                return old
            case events.ListDeleteEvent():
                old = state.lists.pop(event.list_id, None)
                if old is not None:
                    del self._lists[event.list_id]
                    state.lists_view = None
                return old
            case events.PlanUpdateEvent():
                old = state.plans.get(event.plan.priority)
                state.plans[event.plan.priority] = event.plan
                state.plans_view = None
                return old
            case events.UserUpdateEvent():
                old = state.profile
                state.profile = event.user
                return old
            case events.SubitemUpdateEvent():
                return self._part(state, event.subitem.item_id, event.subitem.id, "subitems")
            case events.SubitemDeleteEvent():
                return self._part(state, event.item_id, event.subitem_id, "subitems")
            case events.ReminderDeleteEvent():
                return self._part(state, event.item_id, event.reminder_id, "reminders")
            case _:
                return None

    def _put_item(self, state: UserState, item: Item) -> Item | None:
        _sharing(state, item)
        was_open = state.items.get(item.id)
        if item.status is not ItemStatus.DONE:
            was_done = state.remove_done(item.id)
            state.items[item.id] = self._items[item.id] = item
            state.items_view = None
            return was_open or was_done
        if was_open is not None:
            del state.items[item.id]
            state.items_view = None
        if not self._history:
            self._items.pop(item.id, None)
            return was_open
        was_done = state.remove_done(item.id)
        state.add_done(item)
        self._items[item.id] = item
        return was_open or was_done

    @staticmethod
    def _part(state: UserState, item_id: ID, part_id: ID, kind: typing.Literal["subitems", "reminders"]) -> object:
        item = state.items.get(item_id) or state.done.get(item_id)
        if item is None:
            return None
        return next((part for part in getattr(item, kind) if part.id == part_id), None)
