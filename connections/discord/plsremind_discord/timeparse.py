from __future__ import annotations

import datetime
import re
import typing
import zoneinfo

import msgspec

_PAST: typing.Final = "past"
_UNREADABLE: typing.Final = "unreadable"
_MORNING: typing.Final = datetime.time(9, 0)
_LONGEST: typing.Final = datetime.timedelta(days=3660)
_NOON: typing.Final = 12
_WEEK: typing.Final = 7
_MINUTE, _HOUR, _DAY = 60, 3600, 86400
_SECONDS: typing.Final = {
    **dict.fromkeys(("m", "min", "minute", "minutes", "minuten"), _MINUTE),
    **dict.fromkeys(("h", "std", "stunde", "stunden", "hour", "hours"), _HOUR),
    **dict.fromkeys(("d", "tag", "tage", "tagen", "day", "days"), _DAY),
    **dict.fromkeys(("w", "woche", "wochen", "week", "weeks"), _DAY * _WEEK),
}
_OFFSETS: typing.Final = {"heute": 0, "today": 0, "morgen": 1, "tomorrow": 1, "übermorgen": 2, "uebermorgen": 2}
_WEEKDAYS: typing.Final = {
    name: index
    for index, names in enumerate(
        (
            ("mo", "montag", "mon", "monday"),
            ("di", "dienstag", "tue", "tuesday"),
            ("mi", "mittwoch", "wed", "wednesday"),
            ("do", "donnerstag", "thu", "thursday"),
            ("fr", "freitag", "fri", "friday"),
            ("sa", "samstag", "sat", "saturday"),
            ("so", "sonntag", "sun", "sunday"),
        )
    )
    for name in names
}
_SPAN: typing.Final = re.compile(r"(?:in\s+)?((?:\d+\s*[a-z]+\s*)+)")
_PART: typing.Final = re.compile(r"(\d+)\s*([a-z]+)")
_CLOCK: typing.Final = re.compile(r"(\d{1,2})(?::(\d{2}))?\s*(uhr|am|pm)?")
_DOTTED: typing.Final = re.compile(r"(\d{1,2})\.(\d{1,2})(?:\.(\d{4})?)?")
_ISO: typing.Final = re.compile(r"(\d{4})-(\d{2})-(\d{2})")


class Moment(msgspec.Struct, frozen=True):
    at: datetime.datetime


class Day(msgspec.Struct, frozen=True):
    date: datetime.date


class TimeError(ValueError):
    def __init__(self, reason: typing.Literal["unreadable", "past"]) -> None:
        super().__init__(reason)
        self.reason: typing.Final = reason


def parse(text: str, *, now: datetime.datetime, zone: zoneinfo.ZoneInfo) -> Moment | Day:
    here = now.astimezone(zone)
    words = " ".join(text.lower().split())
    span = _span(words)
    if span is not None:
        return Moment(at=(here.astimezone(datetime.UTC) + span).astimezone(zone))
    head, _, rest = words.partition(" ")
    date = _date(head, here.date())
    if date is None:
        clock = _clock(words)
        today = _local(here.date(), clock, zone)
        return Moment(at=today if today > here else _local(here.date() + datetime.timedelta(days=1), clock, zone))
    if date < here.date():
        raise TimeError(_PAST)
    if not rest:
        return Day(date=date)
    at = _local(date, _clock(rest), zone)
    if at <= here:
        raise TimeError(_PAST)
    return Moment(at=at)


def moment(parsed: Moment | Day, *, now: datetime.datetime, zone: zoneinfo.ZoneInfo) -> datetime.datetime:
    if isinstance(parsed, Moment):
        return parsed.at
    at = _local(parsed.date, _MORNING, zone)
    if at <= now:
        raise TimeError(_PAST)
    return at


def _span(words: str) -> datetime.timedelta | None:
    match = _SPAN.fullmatch(words)
    if match is None:
        return None
    seconds = 0
    for count, unit in _PART.findall(match.group(1)):
        if unit not in _SECONDS:
            return None
        seconds += int(count) * _SECONDS[unit]
    if not 0 < seconds <= _LONGEST.total_seconds():
        raise TimeError(_UNREADABLE)
    return datetime.timedelta(seconds=seconds)


def _date(word: str, today: datetime.date) -> datetime.date | None:
    if word in _OFFSETS:
        return today + datetime.timedelta(days=_OFFSETS[word])
    if word in _WEEKDAYS:
        return today + datetime.timedelta(days=(_WEEKDAYS[word] - today.weekday()) % _WEEK or _WEEK)
    return _written(word, today)


def _written(word: str, today: datetime.date) -> datetime.date | None:
    iso = _ISO.fullmatch(word)
    if iso is not None:
        return _must(_calendar(int(iso[1]), int(iso[2]), int(iso[3])))
    dotted = _DOTTED.fullmatch(word)
    if dotted is None:
        return None
    day, month = int(dotted[1]), int(dotted[2])
    if dotted[3]:
        return _must(_calendar(int(dotted[3]), month, day))
    this = _calendar(today.year, month, day)
    if this is not None and this >= today:
        return this
    return _must(_calendar(today.year + 1, month, day))


def _calendar(year: int, month: int, day: int) -> datetime.date | None:
    try:
        return datetime.date(year, month, day)
    except ValueError:
        return None


def _must(date: datetime.date | None) -> datetime.date:
    if date is None:
        raise TimeError(_UNREADABLE)
    return date


def _clock(text: str) -> datetime.time:
    match = _CLOCK.fullmatch(text)
    if match is None:
        raise TimeError(_UNREADABLE)
    hour, minute, half = int(match[1]), int(match[2] or 0), match[3]
    if half in {"am", "pm"}:
        if not 1 <= hour <= _NOON:
            raise TimeError(_UNREADABLE)
        hour = hour % _NOON + (_NOON if half == "pm" else 0)
    try:
        return datetime.time(hour, minute)
    except ValueError:
        raise TimeError(_UNREADABLE) from None


def _local(date: datetime.date, clock: datetime.time, zone: zoneinfo.ZoneInfo) -> datetime.datetime:
    # Through UTC and back, so that a time the clocks skip lands on one that exists.
    return datetime.datetime.combine(date, clock, tzinfo=zone).astimezone(datetime.UTC).astimezone(zone)
