from __future__ import annotations

import enum

import msgspec
import pytest

from plsremind import enums
from plsremind.enums import Intents
from plsremind.enums import Priority
from plsremind.enums import ReminderKind
from plsremind.enums import intent_names

WIRE_ENUMS = [
    enums.Priority,
    enums.ItemStatus,
    enums.FollowUp,
    enums.ReminderKind,
    enums.DeliveryStatus,
    enums.Role,
    enums.Registration,
    enums.ProviderType,
    enums.CredentialKind,
    enums.OverrideMode,
    enums.ChannelKind,
    enums.ReminderOutcome,
]


def test_known_values() -> None:
    assert Priority("high") is Priority.HIGH
    assert ReminderKind("follow_up") is ReminderKind.FOLLOW_UP


@pytest.mark.parametrize("kind", WIRE_ENUMS)
def test_unknown_value_becomes_unknown(kind: type[enum.Enum]) -> None:
    assert kind("something new") is kind["UNKNOWN"]


@pytest.mark.parametrize("kind", WIRE_ENUMS)
def test_unknown_value_decodes(kind: type[enum.Enum]) -> None:
    assert msgspec.json.decode(b'"something new"', type=kind) is kind["UNKNOWN"]
    assert msgspec.json.decode(b'["something new"]', type=tuple[kind, ...]) == (kind["UNKNOWN"],)


def test_intent_names_follow_the_wire_order() -> None:
    assert intent_names(Intents.DELIVERIES | Intents.ITEMS) == ["items", "deliveries"]
    assert intent_names(Intents.NONE) == []


def test_all_covers_every_intent() -> None:
    assert intent_names(Intents.ALL) == ["users", "lists", "items", "plans", "history", "deliveries"]


def test_intents_from_names_skips_unknown_ones() -> None:
    assert enums.intents_from_names(["items", "telepathy", "deliveries"]) == Intents.ITEMS | Intents.DELIVERIES
