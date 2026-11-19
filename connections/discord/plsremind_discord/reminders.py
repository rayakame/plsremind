from __future__ import annotations

import asyncio
import collections.abc
import datetime
import enum
import itertools
import logging
import typing

import aiohttp
import hikari
import plsremind
import risa

from plsremind_discord import texts as texts_
from plsremind_discord.clock import Clock
from plsremind_discord.hub import Hub
from plsremind_discord.store import Sent
from plsremind_discord.store import Store
from plsremind_discord.views import common
from plsremind_discord.views.reminder import ReminderView

RETRY_AFTER: typing.Final = (60.0, 300.0, 900.0)
RETRY_EVERY: typing.Final = 1800.0
LIFETIME: typing.Final = datetime.timedelta(hours=24)
_LOGGER: typing.Final = logging.getLogger(__name__)


class _Outcome(enum.Enum):
    SETTLED = enum.auto()
    PASSING = enum.auto()


class Reminders:
    """Turns the hub's deliveries into direct messages and tells the hub how each one went."""

    def __init__(
        self,
        *,
        hub: Hub,
        views: risa.Client,
        rest: hikari.api.RESTClient,
        store: Store,
        texts: texts_.Texts,
        clock: Clock,
        sleep: collections.abc.Callable[[float], collections.abc.Awaitable[None]] = asyncio.sleep,
    ) -> None:
        self._hub: typing.Final = hub
        self._views: typing.Final = views
        self._rest: typing.Final = rest
        self._store: typing.Final = store
        self._texts: typing.Final = texts
        self._clock: typing.Final = clock
        self._sleep: typing.Final = sleep
        self._busy: set[plsremind.ID] = set()
        self._withdrawn: set[plsremind.ID] = set()
        self._refused: dict[plsremind.ID, str] = {}
        self._tries: dict[plsremind.ID, asyncio.Task[None]] = {}

    @property
    def waiting(self) -> frozenset[plsremind.ID]:
        """The deliveries that are not settled with the hub yet and are tried again."""
        return frozenset(self._tries)

    async def deliver(self, delivery: plsremind.Delivery) -> None:
        # The hub sends an open delivery again after every login and resume.
        if delivery.id in self._busy or delivery.id in self._tries:
            return
        self._busy.add(delivery.id)
        try:
            outcome = await self._try(delivery, first=True)
        finally:
            self._busy.discard(delivery.id)
        withdrawn = delivery.id in self._withdrawn
        self._withdrawn.discard(delivery.id)
        if outcome is _Outcome.PASSING and not withdrawn:
            self._tries[delivery.id] = asyncio.create_task(self._again(delivery), name=f"reminder-{delivery.id}")

    def withdraw(self, delivery_id: plsremind.ID) -> None:
        self._refused.pop(delivery_id, None)
        task = self._tries.pop(delivery_id, None)
        if task is not None:
            task.cancel()
        elif delivery_id in self._busy:
            self._withdrawn.add(delivery_id)

    async def close(self) -> None:
        tasks = list(self._tries.values())
        self._tries.clear()
        self._withdrawn.clear()
        self._refused.clear()
        for task in tasks:
            task.cancel()
        await asyncio.gather(*tasks, return_exceptions=True)

    async def _again(self, delivery: plsremind.Delivery) -> None:
        over = delivery.reminder.scheduled_for + LIFETIME
        waits = itertools.chain(RETRY_AFTER, itertools.repeat(RETRY_EVERY))
        try:
            while True:
                await self._sleep(next(waits))
                if self._clock.now() >= over:
                    _LOGGER.warning("delivery %s could not be settled within a day and is given up", delivery.id)
                    return
                if await self._try(delivery, first=False) is _Outcome.SETTLED:
                    return
        finally:
            if self._tries.get(delivery.id) is asyncio.current_task():
                del self._tries[delivery.id]
                self._refused.pop(delivery.id, None)

    async def _try(self, delivery: plsremind.Delivery, *, first: bool) -> _Outcome:
        try:
            return await self._attempt(delivery, first=first)
        except Exception:
            # Whatever it was, the reminder must not be lost over it.
            _LOGGER.exception("delivery %s could not be handled and is tried again", delivery.id)
            return _Outcome.PASSING

    async def _attempt(self, delivery: plsremind.Delivery, *, first: bool) -> _Outcome:
        if delivery.id in self._refused:
            return await self._report(delivery.id, self._refused[delivery.id])
        if await self._store.knows_delivery(delivery.id):
            return await self._confirm(delivery.id)
        locale = texts_.locale_for(delivery.user, None)
        accounts = self._accounts(delivery, first=first)
        if not accounts:
            return await self._report(delivery.id, self._texts("delivery.no_account", locale))
        view = ReminderView(user=common.pack(delivery.user.id), item=common.pack(delivery.item.id))
        rendered = await self._views.build(view)
        sent: list[Sent] = []
        passing = False
        for account in accounts:
            try:
                channel = await self._rest.create_dm_channel(account)
                message = await rendered.send_to(self._rest, channel)
            except hikari.ForbiddenError, hikari.NotFoundError:
                continue
            except (hikari.HTTPError, aiohttp.ClientError, TimeoutError) as trouble:
                _LOGGER.warning("delivery %s did not get through to Discord: %s", delivery.id, trouble)
                passing = True
                continue
            sent.append(Sent(message_id=int(message.id), channel_id=int(channel.id)))
        if sent:
            await self._record(delivery, sent)
            return await self._confirm(delivery.id)
        if passing:
            return _Outcome.PASSING
        return await self._report(delivery.id, self._texts("delivery.closed_dms", locale))

    def _accounts(self, delivery: plsremind.Delivery, *, first: bool) -> list[int]:
        if first:
            # During a login a delivery can arrive before its user is known here, so it names them itself.
            return [int(account) for account in delivery.user.external_ids if account.isdecimal()]
        return list(self._hub.discord_ids(delivery.user.id))

    async def _record(self, delivery: plsremind.Delivery, sent: list[Sent]) -> None:
        # Recorded before it is confirmed: a crash in between can then only repeat the confirmation.
        try:
            await self._store.record(
                delivery_id=delivery.id,
                user_id=delivery.user.id,
                item_id=delivery.item.id,
                item_updated_at=delivery.item.updated_at,
                messages=sent,
                now=self._clock.now(),
            )
        except Exception:
            # The user has the reminder; trying again would only send it twice.
            _LOGGER.exception("delivery %s was sent but could not be recorded", delivery.id)

    async def _confirm(self, delivery_id: plsremind.ID) -> _Outcome:
        try:
            await self._hub.delivered(delivery_id)
        except plsremind.PlsremindError as error:
            _LOGGER.warning("delivery %s was sent but could not be confirmed yet: %s", delivery_id, error)
            return _Outcome.PASSING
        return _Outcome.SETTLED

    async def _report(self, delivery_id: plsremind.ID, reason: str) -> _Outcome:
        # Kept, so that a later try reports the same verdict instead of writing to the user again.
        self._refused[delivery_id] = reason
        try:
            await self._hub.failed(delivery_id, reason)
        except plsremind.PlsremindError as error:
            _LOGGER.warning("the failure of delivery %s could not be reported yet: %s", delivery_id, error)
            return _Outcome.PASSING
        del self._refused[delivery_id]
        return _Outcome.SETTLED
