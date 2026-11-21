from __future__ import annotations

import datetime
import zoneinfo

import pytest

from plsremind_discord import timeparse
from plsremind_discord.timeparse import Day
from plsremind_discord.timeparse import Moment
from plsremind_discord.timeparse import TimeError

BERLIN = zoneinfo.ZoneInfo("Europe/Berlin")


def at(year: int, month: int, day: int, hour: int = 0, minute: int = 0) -> datetime.datetime:
    return datetime.datetime(year, month, day, hour, minute, tzinfo=BERLIN)


def on(year: int, month: int, day: int) -> Day:
    return Day(date=datetime.date(year, month, day))


# A Thursday.
NOW = at(2026, 11, 19, 14, 30)

MOMENTS = [
    ("10m", at(2026, 11, 19, 14, 40)),
    ("in 10 min", at(2026, 11, 19, 14, 40)),
    ("in 10 minuten", at(2026, 11, 19, 14, 40)),
    ("in 10 minutes", at(2026, 11, 19, 14, 40)),
    ("2h", at(2026, 11, 19, 16, 30)),
    ("in 2h", at(2026, 11, 19, 16, 30)),
    ("in 2 stunden", at(2026, 11, 19, 16, 30)),
    ("in 2 hours", at(2026, 11, 19, 16, 30)),
    ("1h30m", at(2026, 11, 19, 16, 0)),
    ("1h 30m", at(2026, 11, 19, 16, 0)),
    ("3d", at(2026, 11, 22, 14, 30)),
    ("in 3 tagen", at(2026, 11, 22, 14, 30)),
    ("in 3 days", at(2026, 11, 22, 14, 30)),
    ("1w", at(2026, 11, 26, 14, 30)),
    ("in 1 woche", at(2026, 11, 26, 14, 30)),
    ("in 1 week", at(2026, 11, 26, 14, 30)),
    ("18:00", at(2026, 11, 19, 18, 0)),
    ("18 uhr", at(2026, 11, 19, 18, 0)),
    ("6pm", at(2026, 11, 19, 18, 0)),
    ("6:45 pm", at(2026, 11, 19, 18, 45)),
    ("9", at(2026, 11, 20, 9, 0)),
    ("9am", at(2026, 11, 20, 9, 0)),
    ("09:00", at(2026, 11, 20, 9, 0)),
    ("12am", at(2026, 11, 20, 0, 0)),
    ("12pm", at(2026, 11, 20, 12, 0)),
    ("14:30", at(2026, 11, 20, 14, 30)),
    ("14:31", at(2026, 11, 19, 14, 31)),
    ("morgen 9", at(2026, 11, 20, 9, 0)),
    ("tomorrow 9am", at(2026, 11, 20, 9, 0)),
    ("fr 18:00", at(2026, 11, 20, 18, 0)),
    ("friday 6pm", at(2026, 11, 20, 18, 0)),
    ("24.12. 10:00", at(2026, 12, 24, 10, 0)),
    ("2026-12-24 10:00", at(2026, 12, 24, 10, 0)),
    ("heute 18", at(2026, 11, 19, 18, 0)),
    ("today 18:00", at(2026, 11, 19, 18, 0)),
    ("  Morgen   9  ", at(2026, 11, 20, 9, 0)),
    ("FRIDAY 6PM", at(2026, 11, 20, 18, 0)),
]

DAYS = [
    ("heute", on(2026, 11, 19)),
    ("today", on(2026, 11, 19)),
    ("morgen", on(2026, 11, 20)),
    ("tomorrow", on(2026, 11, 20)),
    ("übermorgen", on(2026, 11, 21)),
    ("fr", on(2026, 11, 20)),
    ("freitag", on(2026, 11, 20)),
    ("fri", on(2026, 11, 20)),
    ("friday", on(2026, 11, 20)),
    ("do", on(2026, 11, 26)),
    ("donnerstag", on(2026, 11, 26)),
    ("thu", on(2026, 11, 26)),
    ("thursday", on(2026, 11, 26)),
    ("mi", on(2026, 11, 25)),
    ("sunday", on(2026, 11, 22)),
    ("24.12.", on(2026, 12, 24)),
    ("24.12", on(2026, 12, 24)),
    ("24.12.2026", on(2026, 12, 24)),
    ("2026-12-24", on(2026, 12, 24)),
    ("1.1.", on(2027, 1, 1)),
    ("19.11.", on(2026, 11, 19)),
    ("18.11.", on(2027, 11, 18)),
]

PAST = ["heute 9", "today 14:30", "18.11.2026", "2020-01-01", "19.11. 9:00", "2026-11-19 14:00"]

UNREADABLE = [
    "",
    "   ",
    "bald",
    "25:00",
    "18:60",
    "32.1.",
    "30.2.2027",
    "0m",
    "in -2h",
    "13pm",
    "0am",
    "24",
    "9 9",
    "morgen bald",
    "2h 9",
]


@pytest.mark.parametrize(("text", "expected"), MOMENTS, ids=[text for text, _ in MOMENTS])
def test_a_moment_is_read(text: str, expected: datetime.datetime) -> None:
    parsed = timeparse.parse(text, now=NOW, zone=BERLIN)
    assert isinstance(parsed, Moment)
    assert parsed.at == expected
    assert parsed.at.tzinfo is BERLIN


@pytest.mark.parametrize(("text", "expected"), DAYS, ids=[text for text, _ in DAYS])
def test_a_day_is_read(text: str, expected: Day) -> None:
    assert timeparse.parse(text, now=NOW, zone=BERLIN) == expected


@pytest.mark.parametrize("text", PAST)
def test_a_time_that_has_passed_is_refused(text: str) -> None:
    with pytest.raises(TimeError) as caught:
        timeparse.parse(text, now=NOW, zone=BERLIN)
    assert caught.value.reason == "past"


@pytest.mark.parametrize("text", UNREADABLE)
def test_what_cannot_be_read_is_refused(text: str) -> None:
    with pytest.raises(TimeError) as caught:
        timeparse.parse(text, now=NOW, zone=BERLIN)
    assert caught.value.reason == "unreadable"


def test_now_may_come_in_any_zone() -> None:
    parsed = timeparse.parse("18:00", now=NOW.astimezone(datetime.UTC), zone=BERLIN)
    assert parsed == Moment(at=at(2026, 11, 19, 18, 0))


def test_a_time_the_clocks_skip_moves_to_the_next_that_exists() -> None:
    parsed = timeparse.parse("morgen 2:30", now=at(2027, 3, 27, 12, 0), zone=BERLIN)
    assert isinstance(parsed, Moment)
    assert parsed.at.isoformat() == "2027-03-28T03:30:00+02:00"


def test_a_span_counts_time_that_passes_and_not_the_clock() -> None:
    parsed = timeparse.parse("24h", now=at(2027, 3, 27, 12, 0), zone=BERLIN)
    assert isinstance(parsed, Moment)
    assert parsed.at.isoformat() == "2027-03-28T13:00:00+02:00"


def test_a_time_the_clocks_show_twice_is_the_first_one() -> None:
    parsed = timeparse.parse("morgen 2:30", now=at(2026, 10, 24, 12, 0), zone=BERLIN)
    assert isinstance(parsed, Moment)
    assert parsed.at.isoformat() == "2026-10-25T02:30:00+02:00"


@pytest.mark.parametrize(
    ("text", "expected"),
    [
        ("1d", "2027-03-28T12:00:00+02:00"),
        ("1w", "2027-04-03T12:00:00+02:00"),
        ("1d2h", "2027-03-28T14:00:00+02:00"),
        ("in 1 tag", "2027-03-28T12:00:00+02:00"),
        ("48h", "2027-03-29T13:00:00+02:00"),
    ],
)
def test_days_and_weeks_keep_the_time_of_day_across_a_clock_change(text: str, expected: str) -> None:
    parsed = timeparse.parse(text, now=at(2027, 3, 27, 12, 0), zone=BERLIN)
    assert isinstance(parsed, Moment)
    assert parsed.at.isoformat() == expected


def test_a_day_later_from_a_time_the_clocks_skip_lands_on_one_that_exists() -> None:
    parsed = timeparse.parse("1d", now=at(2027, 3, 27, 2, 30), zone=BERLIN)
    assert isinstance(parsed, Moment)
    assert parsed.at.isoformat() == "2027-03-28T03:30:00+02:00"


FIRST_0230 = datetime.datetime(2026, 10, 25, 2, 30, tzinfo=BERLIN)
SECOND_0230 = datetime.datetime(2026, 10, 25, 2, 30, tzinfo=BERLIN, fold=1)


@pytest.mark.parametrize(
    ("now", "text", "expected"),
    [
        (FIRST_0230, "2:45", "2026-10-25T02:45:00+02:00"),
        (SECOND_0230, "2:45", "2026-10-25T02:45:00+01:00"),
        (SECOND_0230, "heute 2:45", "2026-10-25T02:45:00+01:00"),
        (FIRST_0230, "2:15", "2026-10-25T02:15:00+01:00"),
        (SECOND_0230, "2:15", "2026-10-26T02:15:00+01:00"),
        (SECOND_0230, "3:00", "2026-10-25T03:00:00+01:00"),
        (SECOND_0230, "15m", "2026-10-25T02:45:00+01:00"),
        (FIRST_0230, "15m", "2026-10-25T02:45:00+02:00"),
        (FIRST_0230, "1h", "2026-10-25T02:30:00+01:00"),
    ],
)
def test_in_the_hour_the_clocks_show_twice_the_next_such_time_is_meant(
    now: datetime.datetime, text: str, expected: str
) -> None:
    parsed = timeparse.parse(text, now=now, zone=BERLIN)
    assert isinstance(parsed, Moment)
    assert parsed.at.isoformat() == expected
    assert parsed.at.astimezone(datetime.UTC) > now.astimezone(datetime.UTC)


def test_a_time_of_today_that_has_passed_both_times_is_refused() -> None:
    with pytest.raises(TimeError) as caught:
        timeparse.parse("heute 2:15", now=SECOND_0230, zone=BERLIN)
    assert caught.value.reason == "past"


@pytest.mark.parametrize("text", ["31.12.9999 23:00", "31.12.9999", "9999-12-31 9", "01.01.2200", "2200-01-01 12:00"])
def test_a_date_too_far_off_to_reckon_with_is_refused(text: str) -> None:
    with pytest.raises(TimeError) as caught:
        timeparse.parse(text, now=NOW, zone=BERLIN)
    assert caught.value.reason == "unreadable"


def test_a_date_decades_ahead_is_still_a_date() -> None:
    assert timeparse.parse("01.01.2100", now=NOW, zone=BERLIN) == on(2100, 1, 1)


@pytest.mark.parametrize("digits", [20, 310, 4301])
def test_a_number_too_long_to_reckon_with_is_refused(digits: int) -> None:
    for text in (f"{'9' * digits}m", f"{'9' * digits}d", f"in {'9' * digits} stunden", f"{'9' * digits}:00"):
        with pytest.raises(TimeError) as caught:
            timeparse.parse(text, now=NOW, zone=BERLIN)
        assert caught.value.reason == "unreadable"


def test_a_span_too_long_to_reckon_with_is_refused() -> None:
    with pytest.raises(TimeError) as caught:
        timeparse.parse("99999999999w", now=NOW, zone=BERLIN)
    assert caught.value.reason == "unreadable"


def test_a_day_becomes_nine_in_the_morning() -> None:
    assert timeparse.moment(on(2026, 11, 20), now=NOW, zone=BERLIN) == at(2026, 11, 20, 9, 0)


def test_a_moment_stays_what_it_is() -> None:
    when = Moment(at=at(2026, 11, 19, 18, 0))
    assert timeparse.moment(when, now=NOW, zone=BERLIN) == when.at


def test_nine_in_the_morning_of_a_day_that_is_further_along_has_passed() -> None:
    with pytest.raises(TimeError) as caught:
        timeparse.moment(on(2026, 11, 19), now=NOW, zone=BERLIN)
    assert caught.value.reason == "past"
