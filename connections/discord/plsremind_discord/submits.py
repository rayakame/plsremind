from __future__ import annotations

import contextlib
import logging
import typing
import zoneinfo

import hikari
import plsremind
import risa

from plsremind_discord import actions
from plsremind_discord import modals
from plsremind_discord import texts as texts_
from plsremind_discord import timeparse
from plsremind_discord.clock import Clock
from plsremind_discord.config import Config
from plsremind_discord.hub import UNCHANGED
from plsremind_discord.hub import Hub
from plsremind_discord.hub import Unchanged
from plsremind_discord.store import Store
from plsremind_discord.views import common
from plsremind_discord.views.detail import DetailView
from plsremind_discord.views.reminder import Notice

_LOGGER: typing.Final = logging.getLogger(__name__)
_UTC: typing.Final = zoneinfo.ZoneInfo("UTC")

type Values = dict[str, str]


class Modals:
    """Answers the modals the bot opened once someone sends one off."""

    def __init__(
        self, *, hub: Hub, views: risa.Client, store: Store, texts: texts_.Texts, clock: Clock, config: Config
    ) -> None:
        self._hub: typing.Final = hub
        self._views: typing.Final = views
        self._store: typing.Final = store
        self._texts: typing.Final = texts
        self._clock: typing.Final = clock
        self._config: typing.Final = config

    async def submitted(self, event: hikari.ModalInteractionCreateEvent) -> None:
        interaction = event.interaction
        modal = modals.read(interaction.custom_id)
        if modal is None:
            return
        speaks = str(interaction.locale)
        if not self._hub.ready.is_set():
            await self._say(interaction, self._texts("error.starting", texts_.locale_for(None, speaks)))
            return
        user = self._hub.user_of(int(interaction.user.id))
        if user is None:
            await self._say(interaction, *common.unlinked(self._texts, texts_.locale_for(None, speaks), self._config))
            return
        locale = texts_.locale_for(self._hub.profile(user), speaks)
        values = {field.custom_id: field.value for row in interaction.components for field in row}
        try:
            if isinstance(modal, modals.MessageModal):
                await self._message(interaction, user, modal, values, locale)
            elif isinstance(modal, modals.SnoozeModal):
                await self._snooze(interaction, user, modal, values)
            else:
                await self._edit(interaction, user, modal, values)
        except common.RefusedError as refused:
            await self._say(interaction, self._texts(refused.key, locale))
        except timeparse.TimeError as error:
            key = "error.time_past" if error.reason == "past" else "error.time_unreadable"
            await self._say(interaction, self._texts(key, locale))
        except plsremind.BadRequestError:
            await self._say(interaction, self._texts("error.invalid", locale))
        except plsremind.PlsremindError:
            _LOGGER.exception("the hub did not take what a modal asked for")
            await self._say(interaction, self._texts("error.failed", locale))

    async def _message(
        self,
        interaction: hikari.ModalInteraction,
        user: plsremind.ID,
        modal: modals.MessageModal,
        values: Values,
        locale: hikari.Locale,
    ) -> None:
        now, zone = self._clock.now(), self._zone(user)
        at = timeparse.moment(timeparse.parse(values.get(modals.WHEN, ""), now=now, zone=zone), now=now, zone=zone)
        title = values.get(modals.TITLE, "").strip()
        note = "\n".join(part for part in (values.get(modals.NOTE, "").strip(), modal.link) if part)
        item = await actions.remind_me(self._hub, user, title=title, note=note, at=at)
        shown = common.plain(title, common.TITLE_SHOWN)
        text = self._texts("ack.remind", locale, when=texts_.stamp(at, "F"), title=shown)
        link = f"{self._config.public_url}/items/{item.id}"
        await self._say(interaction, text, link, self._texts("button.dashboard", locale))

    async def _snooze(
        self, interaction: hikari.ModalInteraction, user: plsremind.ID, modal: modals.SnoozeModal, values: Values
    ) -> None:
        # A reminder message speaks the hub's language only, so that it never changes between redraws.
        speaks = str(interaction.locale) if modal.detail is not None else None
        view = await common.shown(
            self._hub,
            self._config,
            self._clock,
            self._texts,
            user=common.pack(user),
            item=common.pack(modal.item),
            speaks=speaks,
        )
        if view.item is None:
            raise common.RefusedError(common.GONE)
        typed = timeparse.parse(values.get(modals.WHEN, ""), now=view.now, zone=view.zone)
        at = timeparse.moment(typed, now=view.now, zone=view.zone)
        message = interaction.message
        await common.remind_at(self._hub, user, modal.item, at, origin=_origin(message))
        if modal.detail is not None:
            await self._detail(interaction, user, modal.item, modal.detail)
            return
        text, link, label = common.snoozed(view, at)
        rendered = await self._views.build(Notice(text=text, link=link, label=label))
        await interaction.create_initial_response(hikari.ResponseType.MESSAGE_UPDATE, **rendered)
        if message is not None:
            await self._store.forget_message(int(message.id))

    async def _edit(
        self, interaction: hikari.ModalInteraction, user: plsremind.ID, modal: modals.EditModal, values: Values
    ) -> None:
        item = await self._hub.item(user, modal.item)
        if item is not None:
            now, zone = self._clock.now(), self._zone(user)
            typed = values.get(modals.DUE, "").strip()
            due: timeparse.Moment | timeparse.Day | Unchanged | None
            if typed == modals.due_text(item, zone):
                # Left as it was: a deadline that has passed must not stand in the way of a new title.
                due = UNCHANGED
            else:
                due = timeparse.parse(typed, now=now, zone=zone) if typed else None
            with contextlib.suppress(plsremind.NotFoundError):
                await self._hub.edit(
                    user,
                    modal.item,
                    title=values.get(modals.TITLE, "").strip(),
                    due=due,
                    note=values.get(modals.NOTE),
                    origin=_origin(interaction.message),
                )
        await self._detail(interaction, user, modal.item, modal.back)

    async def _detail(
        self,
        interaction: hikari.ModalInteraction,
        user: plsremind.ID,
        item: plsremind.ID,
        back: tuple[plsremind.ID | None, int],
    ) -> None:
        only, page = back
        # The same durable fields as before the modal, so that what risa remembers of the message still holds.
        view = DetailView(
            user=common.pack(user),
            item=common.pack(item),
            back_list=common.pack(only) if only is not None else None,
            back_page=page,
            speaks=str(interaction.locale),
        )
        rendered = await self._views.build(view)
        await interaction.create_initial_response(hikari.ResponseType.MESSAGE_UPDATE, **rendered)

    async def _say(self, interaction: hikari.ModalInteraction, text: str, link: str = "", label: str = "") -> None:
        rendered = await self._views.build(Notice(text=text, link=link, label=label))
        await interaction.create_initial_response(
            hikari.ResponseType.MESSAGE_CREATE, flags=hikari.MessageFlag.EPHEMERAL, **rendered
        )

    def _zone(self, user: plsremind.ID) -> zoneinfo.ZoneInfo:
        profile = self._hub.profile(user)
        return profile.timezone if profile is not None else _UTC


def _origin(message: hikari.Message | None) -> int | None:
    return int(message.id) if message is not None else None
