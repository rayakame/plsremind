from __future__ import annotations

import collections.abc
import types
import typing

import msgspec

from plsremind.enums import DeliveryStatus
from plsremind.events import ConnectionUpdateEvent
from plsremind.events import DeliveryCloseEvent
from plsremind.events import DeliveryCreateEvent
from plsremind.events import DeliveryTestEvent
from plsremind.events import Event
from plsremind.events import ItemCreateEvent
from plsremind.events import ItemDeleteEvent
from plsremind.events import ItemUpdateEvent
from plsremind.events import ListCreateEvent
from plsremind.events import ListDeleteEvent
from plsremind.events import ListUpdateEvent
from plsremind.events import PlanUpdateEvent
from plsremind.events import ReminderCreateEvent
from plsremind.events import ReminderDeleteEvent
from plsremind.events import SubitemCreateEvent
from plsremind.events import SubitemDeleteEvent
from plsremind.events import SubitemUpdateEvent
from plsremind.events import SyncChunk
from plsremind.events import UserGoneEvent
from plsremind.events import UserSyncEvent
from plsremind.events import UserUpdateEvent
from plsremind.internal import codec
from plsremind.models import Actor
from plsremind.models import ConnectionUser
from plsremind.models import Item
from plsremind.models import List
from plsremind.models import Principal
from plsremind.models import Profile
from plsremind.models import ReminderPlan
from plsremind.models import User
from plsremind.models import _Intents  # pyright: ignore[reportPrivateUsage]
from plsremind.types import ID


class Ready(msgspec.Struct, frozen=True, kw_only=True, gc=False):
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
        wire = codec.decode(payload, _Change[_Reference])
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
        return codec.decode(payload, kind)

    return build


def _user_updated(payload: msgspec.Raw, connection: bool) -> Event:  # ruff: ignore[boolean-type-hint-positional-argument]
    wire = codec.decode(payload, _Change[ConnectionUser] if connection else _Change[User])
    return UserUpdateEvent(user_id=wire.user_id, actor=wire.actor, nonce=wire.nonce, user=wire.data)


def _delivery_closed(payload: msgspec.Raw, connection: bool) -> Event:  # ruff: ignore[unused-function-argument, boolean-type-hint-positional-argument]
    wire = codec.decode(payload, _Change[_Closed])
    return DeliveryCloseEvent(user_id=wire.user_id, delivery_id=wire.data.id, status=wire.data.status)


def _user_sync(payload: msgspec.Raw, connection: bool) -> Event:  # ruff: ignore[boolean-type-hint-positional-argument]
    wire = codec.decode(payload, _Sync[ConnectionUser] if connection else _Sync[User])
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
    "delivery.test": (DeliveryTestEvent, _direct(DeliveryTestEvent)),
}

EVENT_CLASSES: typing.Final[collections.abc.Mapping[str, type[Event]]] = types.MappingProxyType(
    {name: kind for name, (kind, _) in _BUILDERS.items()}
)

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
    entry = _BUILDERS.get(name)
    return None if entry is None else entry[1](payload, connection)


def decode_ready(payload: msgspec.Raw) -> Ready:
    return codec.decode(payload, Ready)


def with_old[E: Event](event: E, old: object | None) -> E:
    field = _OLD_FIELDS.get(type(event))
    if field is None or old is None:
        return event
    return msgspec.structs.replace(event, **{field: old})
