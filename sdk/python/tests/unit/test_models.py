from __future__ import annotations

import datetime
import gc
import json
import logging
import typing
import zoneinfo

import msgspec
import pytest

import plsremind
from plsremind import models
from plsremind.enums import CredentialKind
from plsremind.enums import Intents
from plsremind.enums import OverrideMode
from plsremind.enums import Priority
from plsremind.enums import ReminderKind
from plsremind.internal import codec
from plsremind.types import ClockTime
from plsremind.types import Duration
from tests.support import fixtures

UTC = datetime.UTC


def decode[T](kind: type[T], document: object) -> T:
    return codec.decode(fixtures.raw(document), kind)


def test_item_decodes_into_real_types() -> None:
    item = decode(models.Item, fixtures.ITEM)
    assert item.id == fixtures.ITEM_ID
    assert item.due_at == datetime.datetime(2026, 11, 13, 18, 0, tzinfo=UTC)
    assert item.due_date == datetime.date(2026, 11, 13)
    assert item.priority is Priority.HIGH
    assert item.completed_at is None
    assert isinstance(item.subitems, tuple)
    assert item.subitems[0].title == "Print it"
    assert item.reminders[0].remind_at == datetime.datetime(2026, 11, 12, 10, 0, tzinfo=UTC)
    assert item.reminder_override is not None
    assert item.reminder_override.mode is OverrideMode.CUSTOM
    assert item.reminder_override.before == (datetime.timedelta(hours=3),)
    assert type(item.reminder_override.before[0]) is Duration


def test_timestamps_keep_microseconds() -> None:
    assert decode(models.Item, fixtures.ITEM).updated_at.microsecond == 123456


def test_override_without_times() -> None:
    item = decode(models.Item, {**fixtures.ITEM, "reminder_override": {"mode": "off"}})
    assert item.reminder_override == models.ReminderOverride(mode=OverrideMode.OFF, before=())


def test_unknown_field_is_ignored() -> None:
    assert decode(models.Item, {**fixtures.ITEM, "colour": "red"}) == decode(models.Item, fixtures.ITEM)


def test_unknown_enum_value_decodes() -> None:
    assert decode(models.Item, {**fixtures.ITEM, "priority": "urgent"}).priority is Priority.UNKNOWN


def test_unknown_time_zone_falls_back_to_utc(caplog: pytest.LogCaptureFixture) -> None:
    with caplog.at_level(logging.WARNING, logger="plsremind"):
        first = decode(models.Profile, {**fixtures.PROFILE, "timezone": "Venus/Maxwell"})
        decode(models.Profile, {**fixtures.PROFILE, "timezone": "Venus/Maxwell"})
    assert first.timezone == zoneinfo.ZoneInfo("UTC")
    assert [record.getMessage() for record in caplog.records].count(
        "The time zone 'Venus/Maxwell' is unknown on this system, UTC stands in for it."
    ) == 1


def test_time_zone_is_a_zone() -> None:
    assert decode(models.Profile, fixtures.PROFILE).timezone == zoneinfo.ZoneInfo("Europe/Berlin")


def test_quiet_hours_are_clock_times() -> None:
    hours = decode(models.Profile, fixtures.PROFILE).quiet_hours
    assert hours is not None
    assert hours.start == datetime.time(22, 0)
    assert hours.end == datetime.time(7, 30)
    assert type(hours.start) is ClockTime


def test_structs_are_frozen() -> None:
    item = decode(models.Item, fixtures.ITEM)
    with pytest.raises(AttributeError):
        item.title = "x"  # pyright: ignore[reportAttributeAccessIssue]


def test_preferences_can_be_read_but_not_written() -> None:
    user = decode(models.User, fixtures.USER)
    assert user.preferences["theme"] == "dark"
    assert dict(user.preferences) == fixtures.USER["preferences"]
    with pytest.raises(TypeError):
        typing.cast("dict[str, object]", user.preferences)["theme"] = "light"


def test_user_and_connection_user_are_profiles() -> None:
    user = decode(models.User, fixtures.USER)
    reduced = decode(models.ConnectionUser, fixtures.CONNECTION_USER)
    assert isinstance(user, models.Profile)
    assert isinstance(reduced, models.Profile)
    assert reduced.external_ids == ("1234", "5678")
    assert reduced.remind is True


def test_connection_intents_are_flags() -> None:
    connection = decode(models.UserConnection, fixtures.USER_CONNECTION)
    assert connection.intents == Intents.ITEMS | Intents.DELIVERIES
    newer = decode(models.UserConnection, {**fixtures.USER_CONNECTION, "intents": ["items", "telepathy"]})
    assert newer.intents == Intents.ITEMS


def test_delivery() -> None:
    delivery = decode(models.Delivery, fixtures.DELIVERY)
    assert delivery.reminder.kind is ReminderKind.BEFORE
    assert delivery.reminder.offset == datetime.timedelta(days=1)
    assert delivery.item.title == "Hand in the report"
    assert delivery.user.external_ids == ("1234",)
    assert delivery.user.timezone == zoneinfo.ZoneInfo("Europe/Berlin")


def test_delivery_without_offset_and_external_ids() -> None:
    document = json.loads(fixtures.raw(fixtures.DELIVERY))
    del document["reminder"]["offset"]
    del document["user"]["external_ids"]
    delivery = decode(models.Delivery, document)
    assert delivery.reminder.offset is None
    assert delivery.user.external_ids == ()


def test_principals() -> None:
    personal = decode(models.Principal, fixtures.PRINCIPAL)
    assert personal.kind is CredentialKind.PERSONAL_TOKEN
    assert personal.user_id == fixtures.USER_ID
    assert personal.connection is None
    connection = decode(models.Principal, fixtures.CONNECTION_PRINCIPAL)
    assert connection.user_id is None
    assert connection.connection is not None
    assert connection.connection.provider == "discord"


def test_page() -> None:
    page = decode(models.Page[models.Item], {"data": [fixtures.ITEM], "next_cursor": "abc"})
    assert page.data[0].id == fixtures.ITEM_ID
    assert page.next_cursor == "abc"


@pytest.mark.parametrize("kind", [models.List, models.Item, models.Delivery, models.Profile, models.ConnectionUser])
def test_plain_data_is_not_tracked_by_the_collector(kind: type[typing.Any]) -> None:
    document = next(document for candidate, document in fixtures.ALL if candidate is kind)
    assert not gc.is_tracked(decode(kind, document))


@pytest.mark.parametrize(("kind", "document"), fixtures.ALL, ids=lambda value: getattr(value, "__name__", ""))
def test_every_fixture_decodes(kind: type[typing.Any], document: dict[str, typing.Any]) -> None:
    assert isinstance(decode(kind, document), kind)


@pytest.mark.parametrize(("kind", "document"), fixtures.ALL, ids=lambda value: getattr(value, "__name__", ""))
def test_every_struct_is_frozen_and_bare(kind: type[typing.Any], document: dict[str, typing.Any]) -> None:
    del document
    config = kind.__struct_config__
    assert config.frozen
    public = [name for name in vars(kind) if not name.startswith("_") and name not in kind.__struct_fields__]
    assert public == []


def test_missing_required_field_is_an_error() -> None:
    document = {key: value for key, value in fixtures.ITEM.items() if key != "title"}
    with pytest.raises(msgspec.ValidationError):
        decode(models.Item, document)


def test_encode_body_formats_values_and_drops_unset() -> None:
    body = codec.encode_body(
        {
            "due_at": datetime.datetime(2026, 11, 13, 19, 0, tzinfo=zoneinfo.ZoneInfo("Europe/Berlin")),
            "in": datetime.timedelta(hours=2),
            "note": msgspec.UNSET,
            "due_date": None,
            "day": datetime.date(2026, 11, 13),
            "priority": Priority.LOW,
            "timezone": zoneinfo.ZoneInfo("Europe/Berlin"),
            "quiet_hours": models.QuietHours(start=ClockTime(22, 0), end=ClockTime(7, 0)),
            "before": [datetime.timedelta(days=1), Duration(hours=3)],
            "preferences": {"theme": "dark"},
            "exact": datetime.datetime(2026, 11, 13, 18, 0, 0, 5, tzinfo=UTC),
        }
    )
    assert json.loads(body) == {
        "due_at": "2026-11-13T18:00:00Z",
        "in": "2h",
        "due_date": None,
        "day": "2026-11-13",
        "priority": "low",
        "timezone": "Europe/Berlin",
        "quiet_hours": {"start": "22:00", "end": "07:00"},
        "before": ["1d", "3h"],
        "preferences": {"theme": "dark"},
        "exact": "2026-11-13T18:00:00.000005Z",
    }


def test_encode_body_refuses_a_time_without_zone() -> None:
    with pytest.raises(ValueError, match="time zone"):
        codec.encode_body({"due_at": datetime.datetime(2026, 11, 13, 18, 0)})  # ruff: ignore[call-datetime-without-tzinfo]


def test_encode_body_writes_an_override() -> None:
    override = models.ReminderOverride(mode=OverrideMode.CUSTOM, before=(Duration(hours=3),))
    assert json.loads(codec.encode_body({"reminder_override": override})) == {
        "reminder_override": {"mode": "custom", "before": ["3h"]}
    }
    off = models.ReminderOverride(mode=OverrideMode.OFF)
    assert json.loads(codec.encode_body({"reminder_override": off})) == {"reminder_override": {"mode": "off"}}


def test_encode_query() -> None:
    query = codec.encode_query(
        {
            "status": "all",
            "has_due": True,
            "limit": 50,
            "cursor": None,
            "list_id": msgspec.UNSET,
            "due_before": datetime.datetime(2026, 11, 13, 18, 0, tzinfo=UTC),
            "priority": Priority.HIGH,
        }
    )
    assert query == {
        "status": "all",
        "has_due": "true",
        "limit": "50",
        "due_before": "2026-11-13T18:00:00Z",
        "priority": "high",
    }


def test_a_duration_or_clock_time_the_sdk_cannot_read_does_not_fail_the_object(
    caplog: pytest.LogCaptureFixture,
) -> None:
    document = json.loads(fixtures.raw(fixtures.DELIVERY))
    document["reminder"]["offset"] = "2w"
    with caplog.at_level(logging.WARNING, logger="plsremind"):
        delivery = decode(models.Delivery, document)
        decode(models.Delivery, document)
        profile = decode(models.Profile, {**fixtures.PROFILE, "quiet_hours": {"start": "24:00", "end": "07:00"}})
    assert delivery.reminder.offset == datetime.timedelta(0)
    assert delivery.item.title == "Hand in the report"
    assert profile.quiet_hours is not None
    assert profile.quiet_hours.start == datetime.time(0, 0)
    assert profile.quiet_hours.end == datetime.time(7, 0)
    assert [record.getMessage() for record in caplog.records] == [
        "The duration '2w' is in a form this version does not know, zero stands in for it.",
        "The time of day '24:00' is in a form this version does not know, midnight stands in for it.",
    ]


def test_encode_body_refuses_durations_the_hub_cannot_hold() -> None:
    for unsendable in (datetime.timedelta(seconds=30), datetime.timedelta(0), datetime.timedelta(minutes=1, seconds=1)):
        with pytest.raises(ValueError, match="whole minutes"):
            codec.encode_body({"in": unsendable})


@pytest.mark.parametrize(("kind", "document"), fixtures.ALL, ids=lambda value: getattr(value, "__name__", ""))
def test_everything_received_can_be_written_and_read_again(
    kind: type[typing.Any], document: dict[str, typing.Any]
) -> None:
    received = decode(kind, document)
    written = plsremind.to_json(received)
    assert plsremind.from_json(written, kind) == received
    assert json.loads(written) == json.loads(plsremind.to_json(plsremind.from_json(written, kind)))


def test_to_json_writes_the_form_the_hub_uses() -> None:
    written = json.loads(plsremind.to_json(decode(models.Delivery, fixtures.DELIVERY)))
    assert written["reminder"]["offset"] == "1d"
    assert written["user"]["timezone"] == "Europe/Berlin"
    assert written["item"]["due_at"] == "2026-11-13T18:00:00Z"
    connection = json.loads(plsremind.to_json(decode(models.UserConnection, fixtures.USER_CONNECTION)))
    assert connection["intents"] == ["items", "deliveries"]
    assert json.loads(plsremind.to_json((decode(models.List, fixtures.LIST),)))[0]["name"] == "Inbox"
