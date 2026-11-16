"""What a client tells its listeners."""

from __future__ import annotations

import collections.abc
import types
import typing

import msgspec

from plsremind import _codec
from plsremind.enums import DeliveryStatus
from plsremind.enums import Intents
from plsremind.errors import GatewayError
from plsremind.models import Actor
from plsremind.models import ConnectionUser
from plsremind.models import Delivery
from plsremind.models import Item
from plsremind.models import List
from plsremind.models import ManualReminder
from plsremind.models import Principal
from plsremind.models import Profile
from plsremind.models import ReminderPlan
from plsremind.models import Subitem
from plsremind.models import User
from plsremind.models import UserConnection
from plsremind.models import _Intents  # pyright: ignore[reportPrivateUsage]
from plsremind.types import ID

__all__ = [
    "ChangeEvent",
    "ClosedEvent",
    "ConnectionUpdateEvent",
    "DeliveryCloseEvent",
    "DeliveryCreateEvent",
    "DeliveryEvent",
    "DisconnectedEvent",
    "Event",
    "ExceptionEvent",
    "ItemCreateEvent",
    "ItemDeleteEvent",
    "ItemEvent",
    "ItemUpdateEvent",
    "ListCreateEvent",
    "ListDeleteEvent",
    "ListEvent",
    "ListUpdateEvent",
    "PlanEvent",
    "PlanUpdateEvent",
    "ProfileEvent",
    "ReadyEvent",
    "ReminderCreateEvent",
    "ReminderDeleteEvent",
    "ReminderEvent",
    "ResumedEvent",
    "SubitemCreateEvent",
    "SubitemDeleteEvent",
    "SubitemEvent",
    "SubitemUpdateEvent",
    "SyncChunk",
    "UserEvent",
    "UserGoneEvent",
    "UserReadyEvent",
    "UserSyncEvent",
    "UserUpdateEvent",
]


class Event(msgspec.Struct, frozen=True, kw_only=True, gc=False):
    """The root of everything a listener can be told."""


class UserEvent(Event, frozen=True, kw_only=True):
    """Something about one user."""

    user_id: ID


class ChangeEvent(UserEvent, frozen=True, kw_only=True):
    """A change someone made to a user's data."""

    actor: Actor | None
    """Who made the change. `None` when the hub itself changed what a connection may see."""
    nonce: str | None
    """The nonce of the REST call that made the change, if it sent one."""


class ItemEvent(ChangeEvent, frozen=True, kw_only=True):
    """A change to an item."""


class ItemCreateEvent(ItemEvent, frozen=True, kw_only=True):
    """An item was created."""

    item: Item = msgspec.field(name="data")


class ItemUpdateEvent(ItemEvent, frozen=True, kw_only=True):
    """An item changed. Completing and reopening arrive this way too."""

    item: Item = msgspec.field(name="data")
    old_item: Item | None = None
    """The item as the cache held it before, if it held it."""


class ItemDeleteEvent(ItemEvent, frozen=True, kw_only=True):
    """An item was deleted."""

    item_id: ID
    old_item: Item | None = None


class ListEvent(ChangeEvent, frozen=True, kw_only=True):
    """A change to a list."""


class ListCreateEvent(ListEvent, frozen=True, kw_only=True):
    """A list was created."""

    list: List = msgspec.field(name="data")


class ListUpdateEvent(ListEvent, frozen=True, kw_only=True):
    """A list was renamed or moved."""

    list: List = msgspec.field(name="data")
    old_list: List | None = None


class ListDeleteEvent(ListEvent, frozen=True, kw_only=True):
    """A list was deleted."""

    list_id: ID
    old_list: List | None = None


class SubitemEvent(ChangeEvent, frozen=True, kw_only=True):
    """A change to a subitem. An `ItemUpdateEvent` with the whole item follows it."""


class SubitemCreateEvent(SubitemEvent, frozen=True, kw_only=True):
    """A subitem was added."""

    subitem: Subitem = msgspec.field(name="data")


class SubitemUpdateEvent(SubitemEvent, frozen=True, kw_only=True):
    """A subitem was renamed, ticked or moved."""

    subitem: Subitem = msgspec.field(name="data")
    old_subitem: Subitem | None = None


class SubitemDeleteEvent(SubitemEvent, frozen=True, kw_only=True):
    """A subitem was deleted."""

    subitem_id: ID
    item_id: ID
    old_subitem: Subitem | None = None


class ReminderEvent(ChangeEvent, frozen=True, kw_only=True):
    """A change to a manual reminder. An `ItemUpdateEvent` with the whole item follows it."""


class ReminderCreateEvent(ReminderEvent, frozen=True, kw_only=True):
    """A manual reminder was set."""

    reminder: ManualReminder = msgspec.field(name="data")


class ReminderDeleteEvent(ReminderEvent, frozen=True, kw_only=True):
    """A manual reminder was removed."""

    reminder_id: ID
    item_id: ID
    old_reminder: ManualReminder | None = None


class PlanEvent(ChangeEvent, frozen=True, kw_only=True):
    """A change to a reminder plan."""


class PlanUpdateEvent(PlanEvent, frozen=True, kw_only=True):
    """The reminder plan of one priority was replaced or reset."""

    plan: ReminderPlan = msgspec.field(name="data")
    old_plan: ReminderPlan | None = None


class ProfileEvent(ChangeEvent, frozen=True, kw_only=True):
    """A change to a user's profile or to what a connection may do for them."""


class UserUpdateEvent(ProfileEvent, frozen=True, kw_only=True):
    """A user's profile changed."""

    user: Profile
    """A `User` with a personal token, a `ConnectionUser` for a connection."""
    old_user: Profile | None = None


class ConnectionUpdateEvent(ProfileEvent, frozen=True, kw_only=True):
    """A user changed what a connection may do for them. Connections never receive this."""

    connection: UserConnection = msgspec.field(name="data")


class DeliveryEvent(UserEvent, frozen=True, kw_only=True):
    """Something about a reminder on its way to a user."""


class DeliveryCreateEvent(DeliveryEvent, frozen=True, kw_only=True):
    """A reminder is due. It arrives again after every reconnect until it is acknowledged."""

    delivery: Delivery = msgspec.field(name="data")


class DeliveryCloseEvent(DeliveryEvent, frozen=True, kw_only=True):
    """An open delivery was withdrawn. If the user has not been reminded yet, leave it."""

    delivery_id: ID
    status: DeliveryStatus


class SyncChunk(msgspec.Struct, frozen=True, kw_only=True, gc=False):
    """Which part of a user's state a `UserSyncEvent` is."""

    index: int
    count: int


class UserSyncEvent(UserEvent, frozen=True, kw_only=True):
    """One part of a user's state as it arrives. Most code wants `UserReadyEvent` instead."""

    chunk: SyncChunk
    user: Profile | None = None
    lists: tuple[List, ...] | None = None
    items: tuple[Item, ...] | None = None
    plans: tuple[ReminderPlan, ...] | None = None
    history: tuple[Item, ...] | None = None
    history_cursor: str | msgspec.UnsetType | None = msgspec.UNSET
    """Comes with the last part that carries `history`. `None` if there are no older items."""


class UserGoneEvent(UserEvent, frozen=True, kw_only=True):
    """A connection lost a user: they switched it off, were disabled or deleted."""


class UserReadyEvent(UserEvent, frozen=True, kw_only=True):
    """A user's state is complete in the cache."""


class ReadyEvent(Event, frozen=True, kw_only=True):
    """The client logged in and the state of every user is complete in the cache."""

    principal: Principal
    users: tuple[ID, ...]
    intents: Intents


class ResumedEvent(Event, frozen=True, kw_only=True):
    """The session continued after a dropped connection, with nothing missed."""


class DisconnectedEvent(Event, frozen=True, kw_only=True):
    """The connection dropped. The client reconnects by itself."""

    code: int | None
    """The close code, if there was one."""
    reason: str


class ClosedEvent(Event, frozen=True, kw_only=True, gc=True):
    """The client stopped for good because the hub turned it away."""

    error: GatewayError


class ExceptionEvent(Event, frozen=True, kw_only=True, gc=True):
    """A listener raised."""

    exception: Exception
    failed_event: Event
    failed_listener: collections.abc.Callable[[typing.Any], collections.abc.Coroutine[typing.Any, typing.Any, None]]


class Ready(msgspec.Struct, frozen=True, kw_only=True, gc=False):
    """The hub's `READY` as it arrives, before any state has."""

    principal: Principal
    users: tuple[ID, ...]
    intents: _Intents


class _Change[T](msgspec.Struct, frozen=True, kw_only=True, gc=False):
    user_id: ID
    actor: Actor | None
    nonce: str | None
    data: T


class _Reference(msgspec.Struct, frozen=True, kw_only=True, gc=False):
    id: ID
    item_id: ID | None = None


class _Closed(msgspec.Struct, frozen=True, kw_only=True, gc=False):
    id: ID
    status: DeliveryStatus


class _Sync[P: Profile](msgspec.Struct, frozen=True, kw_only=True, gc=False):
    user_id: ID
    chunk: SyncChunk
    user: P | None = None
    lists: tuple[List, ...] | None = None
    items: tuple[Item, ...] | None = None
    plans: tuple[ReminderPlan, ...] | None = None
    history: tuple[Item, ...] | None = None
    history_cursor: str | msgspec.UnsetType | None = msgspec.UNSET


def _deleted(kind: collections.abc.Callable[..., Event], field: str, *, of_item: bool = False) -> _Builder:
    def build(payload: msgspec.Raw, connection: bool) -> Event:  # ruff: ignore[unused-function-argument, boolean-type-hint-positional-argument]
        wire = _codec.decode(payload, _Change[_Reference])
        fields: dict[str, object] = {field: wire.data.id}
        if of_item:
            if wire.data.item_id is None:
                msg = "Object missing required field `item_id` - at `$.data`"
                raise msgspec.ValidationError(msg)
            fields["item_id"] = wire.data.item_id
        return kind(user_id=wire.user_id, actor=wire.actor, nonce=wire.nonce, **fields)

    return build


def _direct(kind: type[Event]) -> _Builder:
    def build(payload: msgspec.Raw, connection: bool) -> Event:  # ruff: ignore[unused-function-argument, boolean-type-hint-positional-argument]
        return _codec.decode(payload, kind)

    return build


def _user_updated(payload: msgspec.Raw, connection: bool) -> Event:  # ruff: ignore[boolean-type-hint-positional-argument]
    wire = _codec.decode(payload, _Change[ConnectionUser] if connection else _Change[User])
    return UserUpdateEvent(user_id=wire.user_id, actor=wire.actor, nonce=wire.nonce, user=wire.data)


def _delivery_closed(payload: msgspec.Raw, connection: bool) -> Event:  # ruff: ignore[unused-function-argument, boolean-type-hint-positional-argument]
    wire = _codec.decode(payload, _Change[_Closed])
    return DeliveryCloseEvent(user_id=wire.user_id, delivery_id=wire.data.id, status=wire.data.status)


def _user_sync(payload: msgspec.Raw, connection: bool) -> Event:  # ruff: ignore[boolean-type-hint-positional-argument]
    wire = _codec.decode(payload, _Sync[ConnectionUser] if connection else _Sync[User])
    return UserSyncEvent(
        user_id=wire.user_id,
        chunk=wire.chunk,
        user=wire.user,
        lists=wire.lists,
        items=wire.items,
        plans=wire.plans,
        history=wire.history,
        history_cursor=wire.history_cursor,
    )


type _Builder = collections.abc.Callable[[msgspec.Raw, bool], Event]

_BUILDERS: typing.Final[dict[str, tuple[type[Event], _Builder]]] = {
    "user.sync": (UserSyncEvent, _user_sync),
    "user.gone": (UserGoneEvent, _direct(UserGoneEvent)),
    "user.updated": (UserUpdateEvent, _user_updated),
    "connection.updated": (ConnectionUpdateEvent, _direct(ConnectionUpdateEvent)),
    "list.created": (ListCreateEvent, _direct(ListCreateEvent)),
    "list.updated": (ListUpdateEvent, _direct(ListUpdateEvent)),
    "list.deleted": (ListDeleteEvent, _deleted(ListDeleteEvent, "list_id")),
    "item.created": (ItemCreateEvent, _direct(ItemCreateEvent)),
    "item.updated": (ItemUpdateEvent, _direct(ItemUpdateEvent)),
    "item.deleted": (ItemDeleteEvent, _deleted(ItemDeleteEvent, "item_id")),
    "subitem.created": (SubitemCreateEvent, _direct(SubitemCreateEvent)),
    "subitem.updated": (SubitemUpdateEvent, _direct(SubitemUpdateEvent)),
    "subitem.deleted": (SubitemDeleteEvent, _deleted(SubitemDeleteEvent, "subitem_id", of_item=True)),
    "reminder.created": (ReminderCreateEvent, _direct(ReminderCreateEvent)),
    "reminder.deleted": (ReminderDeleteEvent, _deleted(ReminderDeleteEvent, "reminder_id", of_item=True)),
    "plan.updated": (PlanUpdateEvent, _direct(PlanUpdateEvent)),
    "delivery.created": (DeliveryCreateEvent, _direct(DeliveryCreateEvent)),
    "delivery.closed": (DeliveryCloseEvent, _delivery_closed),
}

EVENT_CLASSES: typing.Final[collections.abc.Mapping[str, type[Event]]] = types.MappingProxyType(
    {name: kind for name, (kind, _) in _BUILDERS.items()}
)
"""The class behind each event name of the gateway."""

_OLD_FIELDS: typing.Final[dict[type[Event], str]] = {
    ItemUpdateEvent: "old_item",
    ItemDeleteEvent: "old_item",
    ListUpdateEvent: "old_list",
    ListDeleteEvent: "old_list",
    SubitemUpdateEvent: "old_subitem",
    SubitemDeleteEvent: "old_subitem",
    ReminderDeleteEvent: "old_reminder",
    PlanUpdateEvent: "old_plan",
    UserUpdateEvent: "old_user",
}


def decode_event(name: str, payload: msgspec.Raw, *, connection: bool) -> Event | None:
    """Build the event for a dispatch of the gateway, or `None` for a name this version does not know."""
    entry = _BUILDERS.get(name)
    return None if entry is None else entry[1](payload, connection)


def decode_ready(payload: msgspec.Raw) -> Ready:
    """Read the hub's `READY`."""
    return _codec.decode(payload, Ready)


def with_old[E: Event](event: E, old: object | None) -> E:
    """Return the event with what the cache held before the change, if it has a field for that."""
    field = _OLD_FIELDS.get(type(event))
    if field is None or old is None:
        return event
    return msgspec.structs.replace(event, **{field: old})
