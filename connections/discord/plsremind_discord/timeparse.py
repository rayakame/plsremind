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
_FURTHEST: typing.Final = datetime.timedelta(days=36525)
_ONE_DAY: typing.Final = datetime.timedelta(days=1)
_NOON: typing.Final = 12
_DIGITS: typing.Final = 6
_WEEK: typing.Final = 7
_MINUTE, _HOUR = 60, 3600
_SECONDS: typing.Final = {
    **dict.fromkeys(("m", "min", "minute", "minutes", "minuten"), _MINUTE),
    **dict.fromkeys(("h", "std", "stunde", "stunden", "hour", "hours"), _HOUR),
}
_DAYS: typing.Final = {
    **dict.fromkeys(("d", "tag", "tage", "tagen", "day", "days"), 1),
    **dict.fromkeys(("w", "woche", "wochen", "week", "weeks"), _WEEK),
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
    # Two times in the same zone compare by what the clock shows, which is wrong in the hour it shows twice.
    instant = now.astimezone(datetime.UTC)
    here = instant.astimezone(zone)
    words = " ".join(text.lower().split())
    span = _span(words)
    if span is not None:
        return Moment(at=_later(here, *span, zone))
    head, _, rest = words.partition(" ")
    date = _date(head, here.date())
    if date is None:
        clock = _clock(words)
        today = _next(here.date(), clock, zone, instant)
        return Moment(at=today if today is not None else _local(here.date() + _ONE_DAY, clock, zone))
    if date < here.date():
        raise TimeError(_PAST)
    if date - here.date() > _FURTHEST:
        raise TimeError(_UNREADABLE)
    if not rest:
        return Day(date=date)
    at = _next(date, _clock(rest), zone, instant)
    if at is None:
        raise TimeError(_PAST)
    return Moment(at=at)


def moment(parsed: Moment | Day, *, now: datetime.datetime, zone: zoneinfo.ZoneInfo) -> datetime.datetime:
    if isinstance(parsed, Moment):
        return parsed.at
    at = _local(parsed.date, _MORNING, zone)
    if at <= now.astimezone(datetime.UTC):
        raise TimeError(_PAST)
    return at


def _span(words: str) -> tuple[int, int] | None:
    """The days and the seconds a span adds up to."""
    match = _SPAN.fullmatch(words)
    if match is None:
        return None
    days = seconds = 0
    for count, unit in _PART.findall(match.group(1)):
        if len(count) > _DIGITS:
            raise TimeError(_UNREADABLE)
        if unit in _DAYS:
            days += int(count) * _DAYS[unit]
        elif unit in _SECONDS:
            seconds += int(count) * _SECONDS[unit]
        else:
            return None
    if not 0 < days * _ONE_DAY.total_seconds() + seconds <= _LONGEST.total_seconds():
        raise TimeError(_UNREADABLE)
    return days, seconds


def _later(here: datetime.datetime, days: int, seconds: int, zone: zoneinfo.ZoneInfo) -> datetime.datetime:
    # Days keep the time of day across a clock change; hours and minutes count the time that passes.
    start = here
    if days:
        moved = here.replace(tzinfo=None) + datetime.timedelta(days=days)
        start = _local(moved.date(), moved.time(), zone)
    return (start.astimezone(datetime.UTC) + datetime.timedelta(seconds=seconds)).astimezone(zone)


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


def _next(
    date: datetime.date, clock: datetime.time, zone: zoneinfo.ZoneInfo, after: datetime.datetime
) -> datetime.datetime | None:
    """The first time after `after` that the clocks show `clock` on that day."""
    for fold in (0, 1):
        at = _local(date, clock, zone, fold=fold)
        if at.astimezone(datetime.UTC) > after:
            return at
    return None


def _local(date: datetime.date, clock: datetime.time, zone: zoneinfo.ZoneInfo, *, fold: int = 0) -> datetime.datetime:
    # Through UTC and back, so that a time the clocks skip lands on one that exists.
    wall = datetime.datetime.combine(date, clock, tzinfo=zone).replace(fold=fold)
    return wall.astimezone(datetime.UTC).astimezone(zone)
