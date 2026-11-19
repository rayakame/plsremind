from __future__ import annotations

import collections.abc
import datetime
import pathlib
import string

import hikari
import lightbulb
import pytest

from plsremind_discord import texts
from tests.support import things

DE, EN = hikari.Locale.DE, hikari.Locale.EN_US


def tables() -> collections.abc.Mapping[hikari.Locale, collections.abc.Mapping[str, str]]:
    return texts.provider()._dict_provider.localizations  # pyright: ignore[reportPrivateUsage]


def placeholders(text: str) -> set[str]:
    return {name for _, name, _, _ in string.Formatter().parse(text) if name is not None}


def po(folder: pathlib.Path, locale: str, entries: dict[str, str]) -> None:
    lines = ['msgid ""', 'msgstr ""', '"Content-Type: text/plain; charset=UTF-8\\n"', ""]
    for key, text in entries.items():
        lines += [f'msgid "{key}"', f'msgstr "{text}"', ""]
    target = folder / locale / "LC_MESSAGES"
    target.mkdir(parents=True)
    (target / "bot.po").write_text("\n".join(lines), encoding="utf-8")


def test_both_languages_hold_the_same_keys() -> None:
    assert set(tables()) == {DE, EN}
    assert sorted(tables()[DE]) == sorted(tables()[EN])
    assert tables()[EN]


def test_a_text_asks_for_the_same_parts_in_both_languages() -> None:
    for key, english in tables()[EN].items():
        assert placeholders(tables()[DE][key]) == placeholders(english), key


def test_no_text_is_empty_or_carries_an_emoji() -> None:
    for table in tables().values():
        for key, text in table.items():
            assert text.strip(), key
            assert not any(
                0x2600 <= ord(char) <= 0x27BF or ord(char) >= 0x1F000 or ord(char) == 0xFE0F for char in text
            ), key


def test_a_text_comes_in_the_language_asked_for() -> None:
    say = texts.Texts(texts.provider())
    assert say("button.undo", DE) == "Rückgängig"
    assert say("button.undo", EN) == "Undo"
    assert say("button.undo", hikari.Locale.FR) == "Undo"


def test_the_changing_parts_are_filled_in() -> None:
    say = texts.Texts(texts.provider())
    assert say("reminder.upcoming", DE, when="<t:1:R>") == "Fällig <t:1:R>"


def test_a_key_the_language_lacks_falls_back_to_english(tmp_path: pathlib.Path) -> None:
    po(tmp_path, "en-US", {"a": "one", "b": "two"})
    po(tmp_path, "de", {"a": "eins"})
    say = texts.Texts(lightbulb.GnuLocalizationProvider(filename="bot.po", directory=str(tmp_path)))
    assert say("a", DE) == "eins"
    assert say("b", DE) == "two"


def test_a_key_nobody_knows_is_an_error() -> None:
    with pytest.raises(KeyError, match=r"no\.such\.key"):
        texts.Texts(texts.provider())("no.such.key", EN)


@pytest.mark.parametrize(
    ("hub", "interaction", "expected"),
    [
        ("de", None, DE),
        ("de", "en-US", DE),
        ("en", "de", EN),
        ("fr", "de", EN),
        (None, "de", DE),
        (None, hikari.Locale.DE, DE),
        (None, "en-GB", EN),
        (None, None, EN),
    ],
)
def test_the_hub_decides_the_language_and_discord_only_if_it_does_not(
    hub: str | None, interaction: str | None, expected: hikari.Locale
) -> None:
    assert texts.locale_for(things.user(locale=hub), interaction) is expected


@pytest.mark.parametrize(("interaction", "expected"), [("de", DE), ("en-US", EN), (None, EN)])
def test_someone_who_is_not_linked_gets_the_language_of_discord(
    interaction: str | None, expected: hikari.Locale
) -> None:
    assert texts.locale_for(None, interaction) is expected


def test_a_moment_becomes_a_discord_timestamp() -> None:
    moment = datetime.datetime(2026, 11, 19, 13, 30, tzinfo=datetime.UTC)
    assert texts.stamp(moment, "R") == f"<t:{int(moment.timestamp())}:R>"
    assert texts.stamp(moment + datetime.timedelta(microseconds=900000), "F") == f"<t:{int(moment.timestamp())}:F>"


def test_a_day_is_written_as_the_language_writes_it() -> None:
    date = datetime.date(2026, 12, 4)
    assert texts.day(date, DE) == "04.12.2026"
    assert texts.day(date, EN) == "2026-12-04"
