"""The value types that are not plain Python builtins."""

from __future__ import annotations

import datetime
import re
import string
import typing

__all__ = ["ID", "ClockTime", "Duration", "format_clock", "format_duration", "parse_clock", "parse_duration"]

ID = typing.NewType("ID", str)
"""The ID of anything the hub keeps: a user, a list, an item, a delivery."""

_UNITS: typing.Final = (("d", 86_400), ("h", 3_600), ("m", 60))
# The hub reads each count as a 32-bit number.
_LARGEST_COUNT: typing.Final = 2**31 - 1
_CLOCK: typing.Final = re.compile(r"([01]\d|2[0-3]):([0-5]\d)", re.ASCII)


class Duration(datetime.timedelta):
    """A span of time as the hub writes it, for example `1d12h`. It is an ordinary `timedelta`."""

    __slots__ = ()


class ClockTime(datetime.time):
    """A local time of day as the hub writes it, for example `22:00`. It is an ordinary `time`."""

    __slots__ = ()


def parse_duration(text: str) -> Duration:
    """Read a duration such as `3d`, `1d12h` or `90m`.

    Raises
    ------
    ValueError
        If the hub would not accept the text either.
    """
    total = 0
    rest = text
    units = _UNITS
    while rest:
        digits = len(rest) - len(rest.lstrip(string.digits))
        if digits == 0 or digits == len(rest):
            raise ValueError(_bad_duration(text))
        position = next((i for i, (suffix, _) in enumerate(units) if suffix == rest[digits]), None)
        if position is None:
            raise ValueError(_bad_duration(text))
        count = int(rest[:digits])
        if count > _LARGEST_COUNT:
            raise ValueError(_bad_duration(text))
        total += count * units[position][1]
        units = units[position + 1 :]
        rest = rest[digits + 1 :]
    if total <= 0:
        raise ValueError(_bad_duration(text))
    try:
        return Duration(seconds=total)
    except OverflowError:
        raise ValueError(_bad_duration(text)) from None


def _bad_duration(text: str) -> str:
    return f'{text!r} is not a duration, expected a form like "3d", "1d12h" or "90m"'


def format_duration(value: datetime.timedelta) -> str:
    """Write a duration the way the hub does, largest unit first. Seconds are dropped.

    Raises
    ------
    ValueError
        If the duration is negative.
    """
    seconds = value.days * 86_400 + value.seconds
    if seconds < 0:
        msg = f"{value!r} is negative, and the hub has no negative durations"
        raise ValueError(msg)
    parts: list[str] = []
    for suffix, size in _UNITS:
        count, seconds = divmod(seconds, size)
        if count:
            parts.append(f"{count}{suffix}")
    return "".join(parts) or "0m"


def parse_clock(text: str) -> ClockTime:
    """Read a time of day such as `22:00`.

    Raises
    ------
    ValueError
        If the text is not hours and minutes on a 24-hour clock.
    """
    match = _CLOCK.fullmatch(text)
    if match is None:
        msg = f'{text!r} is not a time of day, expected a form like "22:00"'
        raise ValueError(msg)
    return ClockTime(int(match[1]), int(match[2]))


def format_clock(value: datetime.time) -> str:
    """Write a time of day the way the hub does, as hours and minutes."""
    return f"{value.hour:02d}:{value.minute:02d}"
