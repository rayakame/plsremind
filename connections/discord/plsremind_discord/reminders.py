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
from risa import ui

from plsremind_discord import texts as texts_
from plsremind_discord.clock import Clock
from plsremind_discord.hub import Hub
from plsremind_discord.hub import accounts_of
from plsremind_discord.metrics import Metrics
from plsremind_discord.store import Sent
from plsremind_discord.store import Store
from plsremind_discord.views import common
from plsremind_discord.views.reminder import ReminderView

RETRY_AFTER: typing.Final = (60.0, 300.0, 900.0)
RETRY_EVERY: typing.Final = 1800.0
LIFETIME: typing.Final = datetime.timedelta(hours=24)
CLOSE_GRACE = 5.0
_LOGGER: typing.Final = logging.getLogger(__name__)


class _Outcome(enum.Enum):
    SETTLED = enum.auto()
    PASSING = enum.auto()


class _Sending(typing.NamedTuple):
    sent: list[Sent]
    passing: bool
    refused: bool


class _Drawn(typing.NamedTuple):
    rendered: ui.Rendered
    version: datetime.datetime | None


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
        bot: int,
        metrics: Metrics,
        redraw: collections.abc.Callable[[int, int], None],
        sleep: collections.abc.Callable[[float], collections.abc.Awaitable[None]] = asyncio.sleep,
    ) -> None:
        self._hub: typing.Final = hub
        self._views: typing.Final = views
        self._rest: typing.Final = rest
        self._store: typing.Final = store
        self._texts: typing.Final = texts
        self._clock: typing.Final = clock
        self._bot: typing.Final = bot
        self._metrics: typing.Final = metrics
        self._sleep: typing.Final = sleep
        self._redraw: typing.Final = redraw
        self._busy: set[plsremind.ID] = set()
        self._withdrawn: set[plsremind.ID] = set()
        self._refused: dict[plsremind.ID, str] = {}
        self._sent: set[plsremind.ID] = set()
        self._tries: dict[plsremind.ID, asyncio.Task[None]] = {}
        self._closing = False
        metrics.deliveries_waiting.set_function(lambda: len(self._tries))

    @property
    def waiting(self) -> frozenset[plsremind.ID]:
        """The deliveries that are not settled with the hub yet and are tried again."""
        return frozenset(self._tries)

    async def deliver(self, delivery: plsremind.Delivery) -> None:
        if delivery.id in self._busy:
            return
        # The hub sends an open delivery again after every login and resume: the moment for the next try.
        waiting = self._tries.pop(delivery.id, None)
        if waiting is not None:
            waiting.cancel()
        if await self._try(delivery, first=True) is _Outcome.PASSING:
            self._tries[delivery.id] = asyncio.create_task(self._again(delivery), name=f"reminder-{delivery.id}")

    def withdraw(self, delivery_id: plsremind.ID) -> None:
        self._forget(delivery_id)
        if delivery_id in self._busy:
            # A try that is under way finishes, so that a message that went out is recorded.
            self._withdrawn.add(delivery_id)
            return
        task = self._tries.pop(delivery_id, None)
        if task is not None:
            task.cancel()

    async def close(self) -> None:
        tasks, self._tries = self._tries, {}
        self._closing = True
        try:
            for delivery_id, task in tasks.items():
                if delivery_id not in self._busy:
                    task.cancel()
            if tasks:
                # A try that is under way gets a moment, so that a message that went out is recorded.
                _, late = await asyncio.wait(tasks.values(), timeout=CLOSE_GRACE)
                for task in late:
                    task.cancel()
                await asyncio.gather(*tasks.values(), return_exceptions=True)
        finally:
            self._closing = False
            self._withdrawn.clear()
            self._refused.clear()
            self._sent.clear()

    async def _again(self, delivery: plsremind.Delivery) -> None:
        # The hub keeps a delivery open for a day from when it made it, which can be long after it was due.
        over = self._clock.now() + LIFETIME
        waits = itertools.chain(RETRY_AFTER, itertools.repeat(RETRY_EVERY))
        try:
            while True:
                await self._sleep(next(waits))
                if self._clock.now() >= over:
                    _LOGGER.warning("delivery %s could not be settled within a day and is given up", delivery.id)
                    return
                if await self._try(delivery, first=False) is _Outcome.SETTLED or self._closing:
                    return
        finally:
            if self._tries.get(delivery.id) is asyncio.current_task():
                del self._tries[delivery.id]
                self._forget(delivery.id)

    def _forget(self, delivery_id: plsremind.ID) -> None:
        self._refused.pop(delivery_id, None)
        self._sent.discard(delivery_id)

    async def _try(self, delivery: plsremind.Delivery, *, first: bool) -> _Outcome:
        self._busy.add(delivery.id)
        try:
            outcome = await self._attempt(delivery, first=first)
        except Exception:
            # Whatever it was, the reminder must not be lost over it.
            _LOGGER.exception("delivery %s could not be handled and is tried again", delivery.id)
            self._metrics.attempts.labels("error").inc()
            outcome = _Outcome.PASSING
        finally:
            self._busy.discard(delivery.id)
        if delivery.id in self._withdrawn:
            self._withdrawn.discard(delivery.id)
            self._forget(delivery.id)
            return _Outcome.SETTLED
        return outcome

    async def _attempt(self, delivery: plsremind.Delivery, *, first: bool) -> _Outcome:
        if delivery.id in self._refused:
            return await self._report(delivery.id, self._refused[delivery.id])
        # Remembered here too, for when the store could not take it.
        if delivery.id in self._sent or await self._store.knows_delivery(delivery.id):
            return await self._confirm(delivery.id)
        locale = texts_.locale_for(delivery.user, None)
        accounts = self._accounts(delivery, first=first)
        if not accounts:
            self._metrics.attempts.labels("no_account").inc()
            return await self._report(delivery.id, self._texts("delivery.no_account", locale))
        views: dict[int, _Drawn] = {}
        for account in accounts:
            view = ReminderView(user=common.pack(delivery.user.id), item=common.pack(delivery.item.id), account=account)
            rendered = await self._views.build(view)
            if view.shown is None or view.shown.item is None:
                # The user or the item is out of reach this moment; if it stays so, the hub withdraws the delivery.
                _LOGGER.info("delivery %s waits until its item can be read", delivery.id)
                self._metrics.attempts.labels("unreadable").inc()
                return _Outcome.PASSING
            views[account] = _Drawn(rendered, view.shown.version)
        return await self._conclude(delivery, await self._send(delivery, views), locale)

    async def _conclude(self, delivery: plsremind.Delivery, sending: _Sending, locale: hikari.Locale) -> _Outcome:
        if sending.sent:
            self._metrics.attempts.labels("sent").inc()
            return await self._confirm(delivery.id)
        if sending.passing:
            self._metrics.attempts.labels("passing").inc()
            return _Outcome.PASSING
        self._metrics.attempts.labels("refused" if sending.refused else "closed").inc()
        reason = "delivery.refused" if sending.refused else "delivery.closed_dms"
        return await self._report(delivery.id, self._texts(reason, locale))

    async def _send(self, delivery: plsremind.Delivery, views: dict[int, _Drawn]) -> _Sending:
        sent: list[Sent] = []
        passing = refused = False
        for account, (rendered, version) in views.items():
            try:
                channel, message = await self._post(account, rendered)
            except hikari.ForbiddenError, hikari.NotFoundError:
                continue
            except hikari.ClientHTTPResponseError:
                _LOGGER.exception("Discord refused delivery %s for good", delivery.id)
                refused = True
                continue
            except (hikari.HTTPError, aiohttp.ClientError, TimeoutError) as trouble:
                _LOGGER.warning("delivery %s did not get through to Discord: %s", delivery.id, trouble)
                passing = True
                continue
            one = Sent(message_id=int(message.id), channel_id=channel, account_id=account)
            # At once, so that a stop before the next account cannot make this one go out twice.
            self._sent.add(delivery.id)
            await self._record(delivery, [one], version)
            sent.append(one)
        return _Sending(sent, passing=passing, refused=refused)

    async def _post(self, account: int, rendered: ui.Rendered) -> tuple[int, hikari.Message]:
        # Opening the DM channel for every reminder would double the requests at the busy hours.
        kept = await self._kept(account)
        stale: hikari.NotFoundError | None = None
        if kept is not None:
            try:
                return kept, await self._message(kept, account, rendered)
            except hikari.NotFoundError as unknown:
                stale = unknown
            await self._forget_channel(account, kept)
            # Another reminder may have opened the channel anew meanwhile.
            current = await self._kept(account)
            if current is not None and current != kept:
                return current, await self._message(current, account, rendered)
        channel = int((await self._rest.create_dm_channel(account)).id)
        self._metrics.dm_channels_opened.inc()
        # Discord gives back the same channel when it is opened again, and it would fail again.
        if stale is not None and channel == kept:
            raise stale
        # Kept when the message is recorded, so that a channel lasts exactly as long as its messages.
        return channel, await self._message(channel, account, rendered)

    async def _kept(self, account: int) -> int | None:
        try:
            return await self._store.dm_channel(self._bot, account)
        except Exception:
            _LOGGER.exception("a kept DM channel could not be read")
            return None

    async def _forget_channel(self, account: int, channel: int) -> None:
        try:
            await self._store.forget_dm_channel(self._bot, account, channel)
        except Exception:
            _LOGGER.exception("a DM channel Discord no longer knows could not be forgotten")

    async def _message(self, channel: int, account: int, rendered: ui.Rendered) -> hikari.Message:
        return await self._rest.create_message(
            channel, **rendered, user_mentions=[account], role_mentions=False, mentions_everyone=False
        )

    def _accounts(self, delivery: plsremind.Delivery, *, first: bool) -> list[int]:
        if first:
            # During a login a delivery can arrive before its user is known here, so it names them itself.
            return list(accounts_of(delivery.user.external_ids))
        return list(self._hub.discord_ids(delivery.user.id))

    async def _record(self, delivery: plsremind.Delivery, sent: list[Sent], version: datetime.datetime | None) -> None:
        try:
            await self._store.record(
                delivery_id=delivery.id,
                user_id=delivery.user.id,
                item_id=delivery.item.id,
                shown_at=version,
                messages=sent,
                bot_id=self._bot,
                now=self._clock.now(),
            )
        except Exception:
            # The user has the reminder; trying again would only send it twice.
            _LOGGER.exception("delivery %s was sent but could not be recorded", delivery.id)
            return
        # A change while the message was on its way found no message to redraw yet; a deletion too.
        now = self._hub.cached(delivery.user.id, delivery.item.id)
        if now is None or self._hub.version(now) != version:
            for one in sent:
                self._redraw(one.message_id, one.channel_id)

    async def _confirm(self, delivery_id: plsremind.ID) -> _Outcome:
        try:
            await self._hub.delivered(delivery_id)
        except plsremind.PlsremindError as error:
            _LOGGER.warning("delivery %s was sent but could not be confirmed yet: %s", delivery_id, error)
            return _Outcome.PASSING
        self._sent.discard(delivery_id)
        return _Outcome.SETTLED

    async def _report(self, delivery_id: plsremind.ID, reason: str) -> _Outcome:
        # Kept, so that a later try reports the same verdict instead of writing to the user again.
        self._refused[delivery_id] = reason
        try:
            await self._hub.failed(delivery_id, reason)
        except plsremind.PlsremindError as error:
            _LOGGER.warning("the failure of delivery %s could not be reported yet: %s", delivery_id, error)
            return _Outcome.PASSING
        self._refused.pop(delivery_id, None)
        return _Outcome.SETTLED
