from __future__ import annotations

import asyncio
import collections.abc
import datetime
import typing

import plsremind

from plsremind_discord.config import Config
from plsremind_discord.timeparse import Day
from plsremind_discord.timeparse import Moment

INTENTS: typing.Final = (
    plsremind.Intents.USERS
    | plsremind.Intents.LISTS
    | plsremind.Intents.ITEMS
    | plsremind.Intents.HISTORY
    | plsremind.Intents.DELIVERIES
)
_PROVIDER: typing.Final = "discord"
_CLOSED: typing.Final = "delivery_closed"


class HubError(Exception):
    pass


class Hub:
    """The bot's only way to the hub."""

    def __init__(self, client: plsremind.Client) -> None:
        self.client = client
        self.ready = asyncio.Event()
        self._users: dict[int, plsremind.ID] = {}
        self._accounts: dict[plsremind.ID, tuple[int, ...]] = {}
        client.subscribe(plsremind.UserReadyEvent, self._on_ready)
        client.subscribe(plsremind.UserUpdateEvent, self._on_updated)
        client.subscribe(plsremind.UserGoneEvent, self._on_gone)

    async def start(self) -> None:
        await self.client.start()
        principal = self.client.principal
        connection = principal.connection if principal is not None else None
        if connection is None or connection.provider != _PROVIDER:
            await self.client.close()
            msg = "PLSREMIND_TOKEN must be the token of a connection whose provider is discord."
            raise HubError(msg)
        # The listeners run as tasks, so some users may not have been heard of yet.
        for user in self.client.cache.get_users():
            self._learn(user, self.client.cache.get_profile(user))
        self.ready.set()

    async def close(self) -> None:
        await self.client.close()

    def user_ready(self, event: plsremind.UserReadyEvent) -> None:
        self._learn(event.user_id, self.client.cache.get_profile(event.user_id))

    def user_updated(self, event: plsremind.UserUpdateEvent) -> None:
        self._learn(event.user_id, event.user)

    def user_gone(self, event: plsremind.UserGoneEvent) -> None:
        self._forget(event.user_id)

    def user_of(self, discord_id: int) -> plsremind.ID | None:
        return self._users.get(discord_id)

    def profile(self, user: plsremind.ID) -> plsremind.ConnectionUser | None:
        found = self.client.cache.get_profile(user)
        return found if isinstance(found, plsremind.ConnectionUser) else None

    def discord_ids(self, user: plsremind.ID) -> tuple[int, ...]:
        return self._accounts.get(user, ())

    def lists(self, user: plsremind.ID) -> tuple[plsremind.List, ...]:
        return self.client.cache.get_lists(user)

    def list_name(self, list_id: plsremind.ID) -> str | None:
        found = self.client.cache.get_list(list_id)
        return found.name if found is not None else None

    def open_items(self, user: plsremind.ID) -> tuple[plsremind.Item, ...]:
        return self.client.cache.get_items(user)

    def cached(self, user: plsremind.ID, item: plsremind.ID) -> plsremind.Item | None:
        found = self.client.cache.get_item(item)
        # The cache finds an item of any user, so the list tells whose it is.
        if found is not None and any(one.id == found.list_id for one in self.client.cache.get_lists(user)):
            return found
        return None

    async def item(self, user: plsremind.ID, item: plsremind.ID) -> plsremind.Item | None:
        cached = self.cached(user, item)
        if cached is not None:
            return cached
        try:
            return await self.client.rest.get_item(item, user=user)
        except plsremind.NotFoundError:
            return None

    async def schedule(self, user: plsremind.ID, item: plsremind.ID) -> tuple[plsremind.ScheduledReminder, ...]:
        return await self.client.rest.get_item_schedule(item, user=user)

    async def add(
        self,
        user: plsremind.ID,
        *,
        title: str,
        note: str = "",
        list_id: plsremind.ID | None = None,
        priority: plsremind.Priority | None = None,
        due_at: datetime.datetime | None = None,
        due_date: datetime.date | None = None,
        nonce: str | None = None,
    ) -> plsremind.Item:
        given = {"note": note or None, "list_id": list_id, "priority": priority, "due_at": due_at, "due_date": due_date}
        fields: dict[str, typing.Any] = {name: value for name, value in given.items() if value is not None}
        return await self.client.rest.create_item(title=title, **fields, user=user, nonce=nonce)

    async def remind(
        self, user: plsremind.ID, item: plsremind.ID, at: datetime.datetime, *, nonce: str | None = None
    ) -> plsremind.ManualReminder:
        return await self.client.rest.create_reminder(item, at=at, user=user, nonce=nonce)

    async def set_done(
        self, user: plsremind.ID, item: plsremind.ID, *, done: bool, nonce: str | None = None
    ) -> plsremind.Item:
        status = plsremind.ItemStatus.DONE if done else plsremind.ItemStatus.OPEN
        return await self.client.rest.update_item(item, status=status, user=user, nonce=nonce)

    async def edit(
        self,
        user: plsremind.ID,
        item: plsremind.ID,
        *,
        title: str,
        due: Moment | Day | None,
        note: str | None,
        nonce: str | None = None,
    ) -> plsremind.Item:
        fields: dict[str, typing.Any] = {"title": title}
        # Only one of the two is sent: the hub clears the other, and clears both for a null.
        if isinstance(due, Day):
            fields["due_date"] = due.date
        else:
            fields["due_at"] = due.at if due is not None else None
        if note is not None:
            fields["note"] = note
        return await self.client.rest.update_item(item, **fields, user=user, nonce=nonce)

    async def delete(self, user: plsremind.ID, item: plsremind.ID, *, nonce: str | None = None) -> None:
        await self.client.rest.delete_item(item, user=user, nonce=nonce)

    async def tick(
        self, user: plsremind.ID, item: plsremind.ID, subitem: plsremind.ID, *, done: bool, nonce: str | None = None
    ) -> plsremind.Subitem:
        return await self.client.rest.update_subitem(item, subitem, done=done, user=user, nonce=nonce)

    async def delivered(self, delivery: plsremind.ID) -> None:
        status = plsremind.DeliveryStatus.DELIVERED
        await _unless_closed(self.client.rest.acknowledge_delivery(delivery, status=status))

    async def failed(self, delivery: plsremind.ID, reason: str) -> None:
        status = plsremind.DeliveryStatus.FAILED
        await _unless_closed(self.client.rest.acknowledge_delivery(delivery, status=status, reason=reason))

    async def _on_ready(self, event: plsremind.UserReadyEvent) -> None:
        self.user_ready(event)

    async def _on_updated(self, event: plsremind.UserUpdateEvent) -> None:
        self.user_updated(event)

    async def _on_gone(self, event: plsremind.UserGoneEvent) -> None:
        self.user_gone(event)

    def _learn(self, user: plsremind.ID, profile: plsremind.Profile | None) -> None:
        self._forget(user)
        if not isinstance(profile, plsremind.ConnectionUser):
            return
        accounts = tuple(int(account) for account in profile.external_ids if account.isdecimal())
        self._accounts[user] = accounts
        for account in accounts:
            self._users[account] = user

    def _forget(self, user: plsremind.ID) -> None:
        for account in self._accounts.pop(user, ()):
            if self._users.get(account) == user:
                del self._users[account]


async def _unless_closed(answer: collections.abc.Awaitable[object]) -> None:
    try:
        await answer
    except plsremind.ConflictError as error:
        # The hub withdrew it meanwhile, for example because the item was completed.
        if error.code != _CLOSED:
            raise


def connect(config: Config) -> Hub:
    client = plsremind.Client(config.hub_url, token=config.hub_token, intents=INTENTS, gateway_url=config.gateway_url)
    return Hub(client)
