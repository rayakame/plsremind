"""The state the gateway sends, kept in memory."""

from __future__ import annotations

import typing

if typing.TYPE_CHECKING:
    from plsremind._state import UserState
    from plsremind.enums import Priority
    from plsremind.models import Item
    from plsremind.models import List
    from plsremind.models import Profile
    from plsremind.models import ReminderPlan
    from plsremind.types import ID

__all__ = ["Cache"]


class Cache:
    """What the gateway has sent about each user, as far as the client's intents reach.

    Only gateway events change it. Reading returns the objects the cache holds, which are
    frozen, so nothing is copied.
    """

    def __init__(self, *, connection: bool = False) -> None:
        """Make an empty cache. A `Client` makes its own."""
        self._users: dict[ID, UserState] = {}
        self._pending: dict[ID, UserState] = {}
        self._users_view: tuple[ID, ...] | None = None
        self._items: dict[ID, Item] = {}
        self._lists: dict[ID, List] = {}
        self._self: ID | None = None
        self._connection = connection
        self._history = False

    def get_users(self) -> tuple[ID, ...]:
        """Return the IDs of the users whose state is complete."""
        if self._users_view is None:
            self._users_view = tuple(self._users)
        return self._users_view

    def get_profile(self, user: ID | None = None) -> Profile | None:
        """Return a user's profile: a `User` with a personal token, a `ConnectionUser` for a connection."""
        state = self._state(user)
        return None if state is None else state.profile

    def get_lists(self, user: ID | None = None) -> tuple[List, ...]:
        """Return a user's lists in their order."""
        state = self._state(user)
        if state is None:
            return ()
        if state.lists_view is None:
            state.lists_view = tuple(sorted(state.lists.values(), key=lambda entry: entry.position))
        return state.lists_view

    def get_list(self, list_id: ID) -> List | None:
        """Return a list of any user in the cache."""
        return self._lists.get(list_id)

    def get_items(self, user: ID | None = None) -> tuple[Item, ...]:
        """Return a user's open items."""
        state = self._state(user)
        if state is None:
            return ()
        if state.items_view is None:
            state.items_view = tuple(state.items.values())
        return state.items_view

    def get_item(self, item_id: ID) -> Item | None:
        """Return an item of any user in the cache: an open one, or a completed one from the history."""
        return self._items.get(item_id)

    def get_history(self, user: ID | None = None) -> tuple[Item, ...]:
        """Return a user's completed items, most recently completed first. Needs `Intents.HISTORY`."""
        state = self._state(user)
        if state is None:
            return ()
        if state.history_view is None:
            state.history_view = tuple(reversed(state.history))
        return state.history_view

    def get_history_cursor(self, user: ID | None = None) -> str | None:
        """Return where `list_items` continues after the history, or `None` if nothing older exists."""
        state = self._state(user)
        return None if state is None else state.history_cursor

    def get_plans(self, user: ID | None = None) -> tuple[ReminderPlan, ...]:
        """Return a user's reminder plans."""
        state = self._state(user)
        if state is None:
            return ()
        if state.plans_view is None:
            state.plans_view = tuple(state.plans.values())
        return state.plans_view

    def get_plan(self, priority: Priority, user: ID | None = None) -> ReminderPlan | None:
        """Return a user's reminder plan for one priority."""
        state = self._state(user)
        return None if state is None else state.plans.get(priority)

    def _state(self, user: ID | None) -> UserState | None:
        if user is None:
            if self._connection:
                msg = "A connection must say which user it means: pass user=..."
                raise TypeError(msg)
            user = self._self
        return None if user is None else self._users.get(user)
