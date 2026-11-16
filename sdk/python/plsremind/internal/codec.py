from __future__ import annotations

import collections.abc
import datetime
import enum
import functools
import logging
import types
import typing
import zoneinfo

import msgspec

from plsremind import models
from plsremind.enums import Intents
from plsremind.enums import OverrideMode
from plsremind.enums import intent_names
from plsremind.enums import intents_from_names
from plsremind.types import ClockTime
from plsremind.types import Duration
from plsremind.types import format_clock
from plsremind.types import format_duration
from plsremind.types import parse_clock
from plsremind.types import parse_duration

_LOGGER: typing.Final = logging.getLogger("plsremind")
_unknown_zones: set[str] = set()
_unknown_texts: set[str] = set()


@functools.cache
def _utc() -> zoneinfo.ZoneInfo:
    # Looked up on first use: a system without a time zone database must still be able to import the SDK.
    return zoneinfo.ZoneInfo("UTC")


def _zone(name: str) -> zoneinfo.ZoneInfo:
    try:
        return zoneinfo.ZoneInfo(name)
    except (zoneinfo.ZoneInfoNotFoundError, ValueError, OSError):
        if name not in _unknown_zones:
            _unknown_zones.add(name)
            _LOGGER.warning("The time zone %r is unknown on this system, UTC stands in for it.", name)
        return _utc()


def _warn_once(text: str, message: str) -> None:
    if text not in _unknown_texts:
        _unknown_texts.add(text)
        _LOGGER.warning(message, text)


def _duration(text: str) -> Duration:
    try:
        return parse_duration(text)
    except ValueError:
        _warn_once(text, "The duration %r is in a form this version does not know, zero stands in for it.")
        return Duration(0)


def _clock(text: str) -> ClockTime:
    try:
        return parse_clock(text)
    except ValueError:
        _warn_once(text, "The time of day %r is in a form this version does not know, midnight stands in for it.")
        return ClockTime(0, 0)


def _hook(kind: type, value: typing.Any) -> object:  # ruff: ignore[any-type]
    if kind is Duration:
        return _duration(value)
    if kind is ClockTime:
        return _clock(value)
    if kind is zoneinfo.ZoneInfo:
        return _zone(value)
    if kind is types.MappingProxyType:
        return types.MappingProxyType(value)
    if kind is models._IntentNames:  # ruff: ignore[private-member-access]  # pyright: ignore[reportPrivateUsage]
        return intents_from_names(value)
    msg = f"The SDK cannot decode a {kind!r}."
    raise NotImplementedError(msg)


@functools.cache
def decoder[T](kind: type[T]) -> msgspec.json.Decoder[T]:
    return msgspec.json.Decoder(kind, dec_hook=_hook)


def decode[T](data: bytes | msgspec.Raw, kind: type[T]) -> T:
    return decoder(kind).decode(data)


def _instant(value: datetime.datetime) -> str:
    if value.tzinfo is None:
        msg = f"{value!r} has no time zone, so the hub cannot know which moment it means."
        raise ValueError(msg)
    return value.astimezone(datetime.UTC).isoformat().replace("+00:00", "Z")


def _duration_text(value: datetime.timedelta, *, sending: bool) -> str:
    if sending and (value <= datetime.timedelta(0) or value.seconds % 60 or value.microseconds):
        msg = f"{value!r} cannot be sent: the hub counts durations in whole minutes, from one minute up."
        raise ValueError(msg)
    return format_duration(value)


def _clock_text(value: datetime.time, *, sending: bool) -> str:
    if sending and (value.second or value.microsecond or value.tzinfo is not None):
        msg = f"{value!r} cannot be sent: the hub holds a time of day as hours and minutes without a zone."
        raise ValueError(msg)
    return format_clock(value)


def _plain(value: object, *, sending: bool = True) -> object:  # ruff: ignore[complex-structure, too-many-return-statements]
    if value is None or isinstance(value, str | bool | int | float):
        return value
    if isinstance(value, Intents):
        return intent_names(value)
    if isinstance(value, enum.Enum):
        return value.value
    if isinstance(value, datetime.datetime):
        return _instant(value)
    if isinstance(value, datetime.date):
        return value.isoformat()
    if isinstance(value, datetime.timedelta):
        return _duration_text(value, sending=sending)
    if isinstance(value, datetime.time):
        return _clock_text(value, sending=sending)
    if isinstance(value, zoneinfo.ZoneInfo):
        return value.key
    if sending and isinstance(value, models.ReminderOverride) and value.mode is not OverrideMode.CUSTOM:
        return {"mode": value.mode.value}
    if isinstance(value, msgspec.Struct):
        return {
            field.encode_name: _plain(item, sending=sending)
            for field in msgspec.structs.fields(value)
            if (item := getattr(value, field.name)) is not msgspec.UNSET
        }
    if isinstance(value, collections.abc.Mapping):
        mapping = typing.cast("collections.abc.Mapping[object, object]", value)
        return {str(key): _plain(item, sending=sending) for key, item in mapping.items() if item is not msgspec.UNSET}
    if isinstance(value, collections.abc.Iterable):
        return [_plain(item, sending=sending) for item in typing.cast("collections.abc.Iterable[object]", value)]
    msg = f"The SDK cannot write a {type(value).__name__}."
    raise TypeError(msg)


def to_json(value: object) -> bytes:
    """Write what the SDK handed out as JSON in the hub's form, to keep it somewhere.

    msgspec's own encoder does not know `Duration` and `ClockTime`. Read it back with `from_json`.
    """
    return msgspec.json.encode(_plain(value, sending=False))


def from_json[T](data: bytes | str, kind: type[T]) -> T:
    """Read JSON in the hub's form into one of the SDK's types, for example `tuple[Item, ...]`."""
    return decoder(kind).decode(data)


def encode_body(fields: collections.abc.Mapping[str, object]) -> bytes:
    return msgspec.json.encode(_plain(fields))


def encode_query(fields: collections.abc.Mapping[str, object]) -> dict[str, str]:
    query: dict[str, str] = {}
    for name, value in fields.items():
        if value is None or value is msgspec.UNSET:
            continue
        plain = _plain(value)
        query[name] = str(plain).lower() if isinstance(plain, bool) else str(plain)
    return query
