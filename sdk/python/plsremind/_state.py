from __future__ import annotations

import bisect
import datetime
import typing

if typing.TYPE_CHECKING:
    from plsremind.enums import Priority
    from plsremind.models import Item
    from plsremind.models import List
    from plsremind.models import Profile
    from plsremind.models import ReminderPlan
    from plsremind.types import ID

_EARLIEST: typing.Final = datetime.datetime.min.replace(tzinfo=datetime.UTC)


def _completion(item: Item) -> tuple[datetime.datetime, str]:
    return (item.completed_at or _EARLIEST, item.id.lower())


class UserState:
    __slots__ = (
        "done",
        "history",
        "history_cursor",
        "history_view",
        "items",
        "items_view",
        "lists",
        "lists_view",
        "plans",
        "plans_view",
        "profile",
    )

    def __init__(self) -> None:
        self.profile: Profile | None = None
        self.lists: dict[ID, List] = {}
        self.items: dict[ID, Item] = {}
        self.done: dict[ID, Item] = {}
        # Oldest completion first, so that bisect can keep it sorted.
        self.history: list[Item] = []
        self.history_cursor: str | None = None
        self.plans: dict[Priority, ReminderPlan] = {}
        self.lists_view: tuple[List, ...] | None = None
        self.items_view: tuple[Item, ...] | None = None
        self.history_view: tuple[Item, ...] | None = None
        self.plans_view: tuple[ReminderPlan, ...] | None = None

    def add_done(self, item: Item) -> None:
        bisect.insort(self.history, item, key=_completion)
        self.done[item.id] = item
        self.history_view = None

    def remove_done(self, item_id: ID) -> Item | None:
        item = self.done.pop(item_id, None)
        if item is not None:
            del self.history[bisect.bisect_left(self.history, _completion(item), key=_completion)]
            self.history_view = None
        return item
