from __future__ import annotations

import contextlib
import datetime
import logging
import typing
import zoneinfo

import hikari
import plsremind
import risa

from plsremind_discord import actions
from plsremind_discord import modals
from plsremind_discord import patience
from plsremind_discord import texts as texts_
from plsremind_discord import timeparse
from plsremind_discord.clock import Clock
from plsremind_discord.config import Config
from plsremind_discord.hub import UNCHANGED
from plsremind_discord.hub import Hub
from plsremind_discord.hub import Unchanged
from plsremind_discord.store import Store
from plsremind_discord.views import common
from plsremind_discord.views.common import Notice
from plsremind_discord.views.detail import DetailView

_LOGGER: typing.Final = logging.getLogger(__name__)
_UTC: typing.Final = zoneinfo.ZoneInfo("UTC")

type Values = dict[str, str]


class _Reply:
    def __init__(self, interaction: hikari.ModalInteraction, views: risa.Client) -> None:
        self._interaction: typing.Final = interaction
        self._views: typing.Final = views
        self._waiting = False

    async def wait(self) -> None:
        if self._interaction.message is None:
            await self._interaction.create_initial_response(
                hikari.ResponseType.DEFERRED_MESSAGE_CREATE, flags=hikari.MessageFlag.EPHEMERAL
            )
        else:
            await self._interaction.create_initial_response(hikari.ResponseType.DEFERRED_MESSAGE_UPDATE)
        self._waiting = True

    async def redraw(self, view: risa.View) -> None:
        rendered = await self._views.build(view)
        if self._waiting:
            await self._interaction.edit_initial_response(**rendered)
        else:
            await self._interaction.create_initial_response(hikari.ResponseType.MESSAGE_UPDATE, **rendered)

    async def say(self, text: str, link: str = "", label: str = "") -> None:
        rendered = await self._views.build(Notice(text=text, link=link, label=label))
        if not self._waiting:
            await self._interaction.create_initial_response(
                hikari.ResponseType.MESSAGE_CREATE, flags=hikari.MessageFlag.EPHEMERAL, **rendered
            )
        elif self._interaction.message is None:
            await self._interaction.edit_initial_response(**rendered)
        else:
            await self._interaction.execute(flags=hikari.MessageFlag.EPHEMERAL, **rendered)


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
        try:
            await self._answer(interaction, modal, _Reply(interaction, self._views))
        except hikari.HikariError as error:
            _LOGGER.warning("a modal could not be answered: %s", error)

    async def _answer(
        self,
        interaction: hikari.ModalInteraction,
        modal: modals.MessageModal | modals.SnoozeModal | modals.EditModal,
        reply: _Reply,
    ) -> None:
        speaks = str(interaction.locale)
        if not self._hub.ready.is_set():
            await reply.say(self._texts("error.starting", texts_.locale_for(None, speaks)))
            return
        user = self._hub.user_of(int(interaction.user.id))
        if user is None:
            await reply.say(*common.unlinked(self._texts, texts_.locale_for(None, speaks), self._config))
            return
        locale = texts_.locale_for(self._hub.profile(user), speaks)
        values = {field.custom_id: field.value for row in interaction.components for field in row}
        try:
            if isinstance(modal, modals.MessageModal):
                await self._message(reply, user, modal, values, locale)
            elif isinstance(modal, modals.SnoozeModal):
                await self._snooze(interaction, reply, user, modal, values)
            else:
                await self._edit(interaction, reply, user, modal, values)
        except common.RefusedError as refused:
            await reply.say(*common.refusal(self._texts, locale, self._config.public_url, refused))
        except timeparse.TimeError as error:
            key = "error.time_past" if error.reason == "past" else "error.time_unreadable"
            await reply.say(self._texts(key, locale))
        except plsremind.BadRequestError:
            await reply.say(self._texts("error.invalid", locale))
        except hikari.HikariError:
            raise
        except Exception:
            _LOGGER.exception("a modal failed")
            await reply.say(self._texts("error.failed", locale))

    async def _message(
        self, reply: _Reply, user: plsremind.ID, modal: modals.MessageModal, values: Values, locale: hikari.Locale
    ) -> None:
        now, zone = self._clock.now(), self._zone(user)
        at = timeparse.moment(timeparse.parse(values.get(modals.WHEN, ""), now=now, zone=zone), now=now, zone=zone)
        title = values.get(modals.TITLE, "").strip()
        note = "\n".join(part for part in (values.get(modals.NOTE, "").strip(), modal.link) if part)
        item = await patience.unhurried(actions.remind_me(self._hub, user, title=title, note=note, at=at), reply.wait)
        shown = common.plain(title, common.TITLE_SHOWN)
        text = self._texts("ack.remind", locale, when=texts_.stamp(at, "F"), title=shown)
        await reply.say(*common.confirmation(self._hub, self._config, self._texts, locale, text=text, item=item))

    async def _snooze(
        self,
        interaction: hikari.ModalInteraction,
        reply: _Reply,
        user: plsremind.ID,
        modal: modals.SnoozeModal,
        values: Values,
    ) -> None:
        message = interaction.message
        view, at = await patience.unhurried(self._snoozed(interaction, user, modal, values), reply.wait)
        if modal.detail is not None:
            await reply.redraw(self._detail(interaction, user, modal.item, modal.detail))
            return
        text, link, label = common.snoozed(view, at)
        await reply.redraw(Notice(text=text, link=link, label=label))
        if message is not None:
            await self._store.forget_message(int(message.id))

    async def _snoozed(
        self, interaction: hikari.ModalInteraction, user: plsremind.ID, modal: modals.SnoozeModal, values: Values
    ) -> tuple[common.Shown, datetime.datetime]:
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
        await common.remind_at(self._hub, user, modal.item, at, origin=_origin(interaction.message))
        return view, at

    async def _edit(
        self,
        interaction: hikari.ModalInteraction,
        reply: _Reply,
        user: plsremind.ID,
        modal: modals.EditModal,
        values: Values,
    ) -> None:
        await patience.unhurried(self._edited(interaction, user, modal, values), reply.wait)
        await reply.redraw(self._detail(interaction, user, modal.item, modal.back))

    async def _edited(
        self, interaction: hikari.ModalInteraction, user: plsremind.ID, modal: modals.EditModal, values: Values
    ) -> None:
        item = await self._hub.item(user, modal.item)
        if item is None:
            return
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

    @staticmethod
    def _detail(
        interaction: hikari.ModalInteraction,
        user: plsremind.ID,
        item: plsremind.ID,
        back: tuple[plsremind.ID | None, int],
    ) -> DetailView:
        only, page = back
        # The same durable fields as before the modal, so that what risa remembers of the message still holds.
        return DetailView(
            user=common.pack(user),
            item=common.pack(item),
            back_list=common.pack(only) if only is not None else None,
            back_page=page,
            speaks=str(interaction.locale),
        )

    def _zone(self, user: plsremind.ID) -> zoneinfo.ZoneInfo:
        profile = self._hub.profile(user)
        return profile.timezone if profile is not None else _UTC


def _origin(message: hikari.Message | None) -> int | None:
    return int(message.id) if message is not None else None
