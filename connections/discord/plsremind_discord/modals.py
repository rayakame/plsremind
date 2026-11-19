from __future__ import annotations

import typing
import uuid
import zoneinfo

import hikari
import plsremind

from plsremind_discord.texts import Texts

WHEN: typing.Final = "when"
TITLE: typing.Final = "title"
DUE: typing.Final = "due"
NOTE: typing.Final = "note"
TITLE_LIMIT: typing.Final = 500
NOTE_LIMIT: typing.Final = 4000
_SHORT: typing.Final = 100


def snooze_id(item: plsremind.ID, *, detail: tuple[plsremind.ID | None, int] | None) -> str:
    key = uuid.UUID(item).hex
    if detail is None:
        return f"pr:snooze:r:{key}"
    back, page = detail
    return f"pr:snooze:d:{key}:{uuid.UUID(back).hex if back is not None else '-'}:{page}"


def time_modal(
    rest: hikari.api.RESTClient, texts: Texts, locale: hikari.Locale
) -> tuple[str, list[hikari.api.ModalActionRowBuilder]]:
    row = rest.build_modal_action_row().add_text_input(
        WHEN,
        texts("modal.time.label", locale),
        placeholder=texts("modal.time.placeholder", locale),
        required=True,
        max_length=_SHORT,
    )
    return texts("modal.time.title", locale), [row]


def edit_id(item: plsremind.ID, back: tuple[plsremind.ID | None, int]) -> str:
    only, page = back
    return f"pr:edit:{uuid.UUID(item).hex}:{uuid.UUID(only).hex if only is not None else '-'}:{page}"


def due_text(item: plsremind.Item, zone: zoneinfo.ZoneInfo) -> str:
    """The deadline in a form the time parser reads back."""
    if item.due_date is not None:
        return item.due_date.strftime("%d.%m.%Y")
    if item.due_at is not None:
        return item.due_at.astimezone(zone).strftime("%d.%m.%Y %H:%M")
    return ""


def edit_modal(
    rest: hikari.api.RESTClient, texts: Texts, locale: hikari.Locale, item: plsremind.Item, zone: zoneinfo.ZoneInfo
) -> tuple[str, list[hikari.api.ModalActionRowBuilder]]:
    rows = [
        rest.build_modal_action_row().add_text_input(
            TITLE,
            texts("modal.edit.name", locale),
            value=item.title,
            required=True,
            min_length=1,
            max_length=TITLE_LIMIT,
        ),
        rest.build_modal_action_row().add_text_input(
            DUE,
            texts("modal.edit.due", locale),
            value=due_text(item, zone) or hikari.UNDEFINED,
            placeholder=texts("modal.time.placeholder", locale),
            required=False,
            max_length=_SHORT,
        ),
    ]
    # A text field of a modal holds 4000 characters; a longer note is left as it is.
    if len(item.note) <= NOTE_LIMIT:
        rows.append(
            rest.build_modal_action_row().add_text_input(
                NOTE,
                texts("modal.edit.note", locale),
                style=hikari.TextInputStyle.PARAGRAPH,
                value=item.note or hikari.UNDEFINED,
                required=False,
                max_length=NOTE_LIMIT,
            )
        )
    return texts("modal.edit.title", locale), rows
