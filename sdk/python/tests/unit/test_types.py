from __future__ import annotations

import datetime

import pytest

from plsremind.types import ClockTime
from plsremind.types import Duration
from plsremind.types import format_clock
from plsremind.types import format_duration
from plsremind.types import parse_clock
from plsremind.types import parse_duration


@pytest.mark.parametrize(
    ("text", "delta"),
    [
        ("1d", datetime.timedelta(days=1)),
        ("3h", datetime.timedelta(hours=3)),
        ("30m", datetime.timedelta(minutes=30)),
        ("1d12h", datetime.timedelta(days=1, hours=12)),
        ("1d30m", datetime.timedelta(days=1, minutes=30)),
        ("2h5m", datetime.timedelta(hours=2, minutes=5)),
        ("7d3h1m", datetime.timedelta(days=7, hours=3, minutes=1)),
    ],
)
def test_duration_round_trip(text: str, delta: datetime.timedelta) -> None:
    parsed = parse_duration(text)
    assert parsed == delta
    assert type(parsed) is Duration
    assert format_duration(delta) == text


@pytest.mark.parametrize("text", ["", "soon", "1", "d", "1x", "1h1d", "1h2h", "0m", "1.5h", "-1h", " 1h", "1h "])
def test_duration_rejects_what_the_hub_rejects(text: str) -> None:
    with pytest.raises(ValueError, match="duration"):
        parse_duration(text)


def test_duration_accepts_unnormalised_counts() -> None:
    assert parse_duration("90m") == datetime.timedelta(minutes=90)
    assert format_duration(datetime.timedelta(minutes=90)) == "1h30m"


def test_format_duration_drops_seconds() -> None:
    assert format_duration(datetime.timedelta(hours=1, seconds=59)) == "1h"
    assert format_duration(datetime.timedelta(seconds=30)) == "0m"


def test_format_duration_refuses_negative() -> None:
    with pytest.raises(ValueError, match="negative"):
        format_duration(datetime.timedelta(hours=-1))


def test_duration_is_a_timedelta() -> None:
    assert parse_duration("1d") + datetime.timedelta(hours=1) == datetime.timedelta(hours=25)


def test_clock_round_trip() -> None:
    parsed = parse_clock("22:00")
    assert parsed == datetime.time(22, 0)
    assert type(parsed) is ClockTime
    assert format_clock(datetime.time(7, 5)) == "07:05"


@pytest.mark.parametrize("text", ["", "22", "24:00", "22:60", "22:00:00", "ab:cd", "7:5"])
def test_clock_rejects_garbage(text: str) -> None:
    with pytest.raises(ValueError, match="time of day"):
        parse_clock(text)


@pytest.mark.parametrize("text", ["99999999999d", "4294967296m", "2147483648h"])
def test_duration_rejects_counts_the_hub_rejects(text: str) -> None:
    with pytest.raises(ValueError, match="duration"):
        parse_duration(text)


def test_duration_accepts_the_largest_count() -> None:
    assert parse_duration("2147483647m") == datetime.timedelta(minutes=2147483647)
