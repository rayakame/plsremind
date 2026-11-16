from __future__ import annotations

import pathlib
import re
import typing

import msgspec
import pytest

from plsremind import _events
from plsremind import events
from plsremind import models
from plsremind.enums import DeliveryStatus
from plsremind.enums import Intents
from plsremind.enums import ReminderKind
from tests import fixtures

GATEWAY_DOC = pathlib.Path(__file__).parents[3] / "docs" / "gateway.md"
CHANGE: dict[str, typing.Any] = {"user_id": fixtures.USER_ID, "actor": fixtures.ACTOR, "nonce": "n-1"}


def change(data: object) -> msgspec.Raw:
    return msgspec.Raw(fixtures.raw({**CHANGE, "data": data}))


def decode(name: str, payload: msgspec.Raw, *, connection: bool = False) -> events.Event:
    event = _events.decode_event(name, payload, connection=connection)
    assert event is not None
    return event


def test_every_gateway_event_has_a_class() -> None:
    table = GATEWAY_DOC.read_text(encoding="utf-8").split("## Events at a glance")[1].split("\n## ")[0]
    names = set(re.findall(r"`([a-z]+\.[a-z_]+)`", table))
    assert names == set(_events.EVENT_CLASSES)
    assert len(names) == 18


def test_item_created() -> None:
    event = decode("item.created", change(fixtures.ITEM))
    assert type(event) is events.ItemCreateEvent
    assert event.item.id == fixtures.ITEM_ID
    assert event.user_id == fixtures.USER_ID
    assert event.actor == models.Actor(kind=models.CredentialKind.PERSONAL_TOKEN, id=models.ID(fixtures.TOKEN_ID))
    assert event.nonce == "n-1"


def test_item_updated_has_no_old_item_until_it_is_given_one() -> None:
    event = decode("item.updated", change(fixtures.ITEM))
    assert type(event) is events.ItemUpdateEvent
    assert event.old_item is None
    previous = decode("item.created", change(fixtures.PLAIN_ITEM))
    assert isinstance(previous, events.ItemCreateEvent)
    filled = _events.with_old(event, previous.item)
    assert filled.old_item is previous.item
    assert filled.item is event.item


def test_with_old_leaves_events_without_a_past_alone() -> None:
    event = decode("item.created", change(fixtures.ITEM))
    assert _events.with_old(event, object()) is event
    update = decode("item.updated", change(fixtures.ITEM))
    assert _events.with_old(update, None) is update


@pytest.mark.parametrize(
    ("name", "data", "kind", "fields"),
    [
        ("item.deleted", {"id": fixtures.ITEM_ID}, events.ItemDeleteEvent, {"item_id": fixtures.ITEM_ID}),
        ("list.deleted", {"id": fixtures.LIST_ID}, events.ListDeleteEvent, {"list_id": fixtures.LIST_ID}),
        (
            "subitem.deleted",
            {"id": fixtures.SUBITEM_ID, "item_id": fixtures.ITEM_ID},
            events.SubitemDeleteEvent,
            {"subitem_id": fixtures.SUBITEM_ID, "item_id": fixtures.ITEM_ID},
        ),
        (
            "reminder.deleted",
            {"id": fixtures.REMINDER_ID, "item_id": fixtures.ITEM_ID},
            events.ReminderDeleteEvent,
            {"reminder_id": fixtures.REMINDER_ID, "item_id": fixtures.ITEM_ID},
        ),
    ],
)
def test_deleted_events_carry_ids(
    name: str, data: dict[str, str], kind: type[events.Event], fields: dict[str, str]
) -> None:
    event = decode(name, change(data))
    assert type(event) is kind
    assert {field: getattr(event, field) for field in fields} == fields
    assert isinstance(event, events.ChangeEvent)
    assert event.nonce == "n-1"


@pytest.mark.parametrize(
    ("name", "data", "kind", "field"),
    [
        ("list.created", fixtures.LIST, events.ListCreateEvent, "list"),
        ("list.updated", fixtures.LIST, events.ListUpdateEvent, "list"),
        ("subitem.created", fixtures.SUBITEM, events.SubitemCreateEvent, "subitem"),
        ("subitem.updated", fixtures.SUBITEM, events.SubitemUpdateEvent, "subitem"),
        ("reminder.created", fixtures.MANUAL_REMINDER, events.ReminderCreateEvent, "reminder"),
        ("plan.updated", fixtures.REMINDER_PLAN, events.PlanUpdateEvent, "plan"),
        ("connection.updated", fixtures.USER_CONNECTION, events.ConnectionUpdateEvent, "connection"),
        ("delivery.created", fixtures.DELIVERY, events.DeliveryCreateEvent, "delivery"),
        ("user.updated", fixtures.USER, events.UserUpdateEvent, "user"),
    ],
)
def test_events_that_carry_an_object(name: str, data: object, kind: type[events.Event], field: str) -> None:
    event = decode(name, change(data))
    assert type(event) is kind
    assert isinstance(getattr(event, field), msgspec.Struct)


def test_delivery_created_survives_what_the_sdk_does_not_know() -> None:
    document = {
        **fixtures.DELIVERY,
        "urgency": 3,
        "reminder": {**fixtures.DELIVERY["reminder"], "kind": "telepathy"},
        "user": {**fixtures.DELIVERY["user"], "timezone": "Mars/Olympus"},
    }
    payload = msgspec.Raw(
        fixtures.raw({"user_id": fixtures.USER_ID, "actor": None, "nonce": None, "data": document, "extra": 1})
    )
    event = decode("delivery.created", payload)
    assert isinstance(event, events.DeliveryCreateEvent)
    assert event.delivery.reminder.kind is ReminderKind.UNKNOWN
    assert event.delivery.item.title == "Hand in the report"
    assert str(event.delivery.user.timezone) == "UTC"


def test_delivery_closed() -> None:
    payload = change({"id": fixtures.DELIVERY_ID, "status": "cancelled"})
    event = decode("delivery.closed", payload)
    assert isinstance(event, events.DeliveryCloseEvent)
    assert event.delivery_id == fixtures.DELIVERY_ID
    assert event.status is DeliveryStatus.CANCELLED


def test_actor_can_be_null() -> None:
    payload = msgspec.Raw(
        fixtures.raw({"user_id": fixtures.USER_ID, "actor": None, "nonce": None, "data": fixtures.CONNECTION_USER})
    )
    event = decode("user.updated", payload, connection=True)
    assert isinstance(event, events.UserUpdateEvent)
    assert event.actor is None
    assert event.nonce is None


def test_profile_shape_follows_the_login() -> None:
    own = decode("user.updated", change(fixtures.USER))
    assert isinstance(own, events.UserUpdateEvent)
    assert type(own.user) is models.User
    seen = decode("user.updated", change(fixtures.CONNECTION_USER), connection=True)
    assert isinstance(seen, events.UserUpdateEvent)
    assert isinstance(seen.user, models.ConnectionUser)
    assert seen.user.external_ids == ("1234", "5678")


def test_first_sync_part() -> None:
    payload = msgspec.Raw(
        fixtures.raw(
            {
                "user_id": fixtures.USER_ID,
                "chunk": {"index": 0, "count": 2},
                "user": fixtures.USER,
                "lists": [fixtures.LIST],
                "items": [fixtures.ITEM],
                "plans": [fixtures.REMINDER_PLAN],
            }
        )
    )
    event = decode("user.sync", payload)
    assert isinstance(event, events.UserSyncEvent)
    assert (event.chunk.index, event.chunk.count) == (0, 2)
    assert type(event.user) is models.User
    assert event.lists is not None
    assert event.lists[0].is_inbox
    assert event.items is not None
    assert event.plans is not None
    assert event.history is None
    assert event.history_cursor is msgspec.UNSET


def test_later_sync_parts_carry_only_what_they_have() -> None:
    payload = msgspec.Raw(
        fixtures.raw(
            {
                "user_id": fixtures.USER_ID,
                "chunk": {"index": 1, "count": 2},
                "history": [fixtures.ITEM],
                "history_cursor": None,
            }
        )
    )
    event = decode("user.sync", payload)
    assert isinstance(event, events.UserSyncEvent)
    assert event.user is None
    assert event.lists is None
    assert event.items is None
    assert event.history is not None
    assert event.history_cursor is None


def test_sync_for_a_connection() -> None:
    payload = msgspec.Raw(
        fixtures.raw({"user_id": fixtures.USER_ID, "chunk": {"index": 0, "count": 1}, "user": fixtures.CONNECTION_USER})
    )
    event = decode("user.sync", payload, connection=True)
    assert isinstance(event, events.UserSyncEvent)
    assert isinstance(event.user, models.ConnectionUser)


def test_user_gone() -> None:
    event = decode("user.gone", msgspec.Raw(fixtures.raw({"user_id": fixtures.USER_ID})))
    assert event == events.UserGoneEvent(user_id=models.ID(fixtures.USER_ID))


def test_unknown_event_is_none() -> None:
    assert _events.decode_event("item.exploded", msgspec.Raw(b"{}"), connection=False) is None


def test_ready() -> None:
    payload = msgspec.Raw(
        fixtures.raw(
            {
                "session_id": "s1",
                "intents": ["items", "deliveries", "telepathy"],
                "principal": fixtures.PRINCIPAL,
                "users": [fixtures.USER_ID],
            }
        )
    )
    ready = _events.decode_ready(payload)
    assert ready.principal.user_id == fixtures.USER_ID
    assert ready.users == (fixtures.USER_ID,)
    assert ready.intents == Intents.ITEMS | Intents.DELIVERIES


def test_hierarchy() -> None:
    assert issubclass(events.ItemUpdateEvent, events.ItemEvent)
    assert issubclass(events.ItemEvent, events.ChangeEvent)
    assert issubclass(events.ChangeEvent, events.UserEvent)
    assert issubclass(events.DeliveryCreateEvent, events.DeliveryEvent)
    assert issubclass(events.DeliveryEvent, events.UserEvent)
    assert issubclass(events.UserSyncEvent, events.UserEvent)
    assert issubclass(events.ReadyEvent, events.Event)
    assert not issubclass(events.ReadyEvent, events.UserEvent)


def test_events_are_frozen() -> None:
    event = decode("item.created", change(fixtures.ITEM))
    with pytest.raises(AttributeError):
        event.nonce = "other"  # pyright: ignore[reportAttributeAccessIssue]


def test_malformed_payload_is_an_error() -> None:
    with pytest.raises(msgspec.MsgspecError):
        _events.decode_event("item.created", msgspec.Raw(b'{"user_id": 5}'), connection=False)
