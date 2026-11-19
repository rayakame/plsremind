from __future__ import annotations

import typing
import uuid

import hikari
import plsremind

from plsremind_discord.texts import Texts

WHEN: typing.Final = "when"
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
