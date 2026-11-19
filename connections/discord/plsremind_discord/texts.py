from __future__ import annotations

import datetime
import pathlib
import typing

import hikari
import lightbulb
import plsremind
from lightbulb import localization

_FILE: typing.Final = "bot.po"
_FOLDER: typing.Final = pathlib.Path(__file__).resolve().parent / "translations"
_GERMAN: typing.Final = "de"

type Style = typing.Literal["R", "F", "f", "t", "D"]


def provider() -> lightbulb.GnuLocalizationProvider:
    # An absolute folder, so that it does not matter where the bot is started from.
    return lightbulb.GnuLocalizationProvider(filename=_FILE, directory=str(_FOLDER))


class Texts:
    def __init__(self, source: localization.LocalizationProvider) -> None:
        self._source: typing.Final = source

    def __call__(self, key: str, locale: hikari.Locale, /, **fields: object) -> str:
        found = typing.cast("localization.LocalizationMapping", self._source(key))
        text = found.get(locale) or found.get(hikari.Locale.EN_US)
        if text is None:
            raise KeyError(key)
        return text.format(**fields)


def locale_for(
    user: plsremind.ConnectionUser | plsremind.DeliveryUser | None, interaction_locale: str | None
) -> hikari.Locale:
    wanted = (user.locale if user is not None else None) or interaction_locale or ""
    return hikari.Locale.DE if str(wanted).lower().startswith(_GERMAN) else hikari.Locale.EN_US


def stamp(at: datetime.datetime, style: Style) -> str:
    return f"<t:{int(at.timestamp())}:{style}>"


def day(date: datetime.date, locale: hikari.Locale) -> str:
    return date.strftime("%d.%m.%Y") if locale is hikari.Locale.DE else date.isoformat()


def units(text: str) -> int:
    """The length of `text` in UTF-16 code units, the larger of the two ways Discord may count it."""
    return len(text.encode("utf-16-le")) // 2
