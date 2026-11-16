"""Turns the hub's JSON into the SDK's types and back."""

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
from plsremind.enums import OverrideMode
from plsremind.enums import intents_from_names
from plsremind.types import ClockTime
from plsremind.types import Duration
from plsremind.types import format_clock
from plsremind.types import format_duration
from plsremind.types import parse_clock
from plsremind.types import parse_duration

_LOGGER: typing.Final = logging.getLogger("plsremind")
_UTC: typing.Final = zoneinfo.ZoneInfo("UTC")
_unknown_zones: set[str] = set()


def _zone(name: str) -> zoneinfo.ZoneInfo:
    try:
        return zoneinfo.ZoneInfo(name)
    except (zoneinfo.ZoneInfoNotFoundError, ValueError, OSError):
        if name not in _unknown_zones:
            _unknown_zones.add(name)
            _LOGGER.warning("The time zone %r is unknown on this system, UTC stands in for it.", name)
        return _UTC


def _hook(kind: type, value: typing.Any) -> object:  # ruff: ignore[any-type]
    if kind is Duration:
        return parse_duration(value)
    if kind is ClockTime:
        return parse_clock(value)
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


def _plain(value: object) -> object:  # ruff: ignore[complex-structure, too-many-return-statements]
    if value is None or isinstance(value, str | bool | int | float):
        return value
    if isinstance(value, enum.Enum):
        return value.value
    if isinstance(value, datetime.datetime):
        return _instant(value)
    if isinstance(value, datetime.date):
        return value.isoformat()
    if isinstance(value, datetime.timedelta):
        return format_duration(value)
    if isinstance(value, datetime.time):
        return format_clock(value)
    if isinstance(value, zoneinfo.ZoneInfo):
        return value.key
    if isinstance(value, models.ReminderOverride):
        if value.mode is OverrideMode.CUSTOM:
            return {"mode": value.mode.value, "before": [format_duration(offset) for offset in value.before]}
        return {"mode": value.mode.value}
    if isinstance(value, msgspec.Struct):
        return {name: _plain(getattr(value, name)) for name in value.__struct_fields__}
    if isinstance(value, collections.abc.Mapping):
        mapping = typing.cast("collections.abc.Mapping[object, object]", value)
        return {str(key): _plain(item) for key, item in mapping.items() if item is not msgspec.UNSET}
    if isinstance(value, collections.abc.Iterable):
        return [_plain(item) for item in typing.cast("collections.abc.Iterable[object]", value)]
    msg = f"The SDK cannot send a {type(value).__name__}."
    raise TypeError(msg)


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
