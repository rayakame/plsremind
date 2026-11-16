"""Every call of the REST API."""

from __future__ import annotations

import asyncio
import datetime
import re
import typing

import aiohttp
import msgspec

from plsremind import errors
from plsremind.enums import DeliveryStatus
from plsremind.internal import codec
from plsremind.internal import routes
from plsremind.models import Delivery
from plsremind.models import DeliveryResult
from plsremind.models import Identity
from plsremind.models import Info
from plsremind.models import Item
from plsremind.models import List
from plsremind.models import LookupResult
from plsremind.models import ManualReminder
from plsremind.models import Page
from plsremind.models import Profile
from plsremind.models import ReminderLogEntry
from plsremind.models import ReminderPlan
from plsremind.models import ScheduledReminder
from plsremind.models import Subitem
from plsremind.models import User
from plsremind.models import UserConnection

if typing.TYPE_CHECKING:
    import collections.abc
    import types
    import zoneinfo

    from plsremind.enums import FollowUp
    from plsremind.enums import ItemSort
    from plsremind.enums import ItemStatus
    from plsremind.enums import Priority
    from plsremind.enums import SortOrder
    from plsremind.models import QuietHours
    from plsremind.models import ReminderOverride
    from plsremind.types import ID

__all__ = ["RESTClient", "paginate"]

type _Unset = msgspec.UnsetType
_UNSET: typing.Final = msgspec.UNSET

_CONNECTION_PREFIX: typing.Final = "plsr_con_"
_MAX_RETRIES: typing.Final = 3
_LONGEST_WAIT: typing.Final = 30.0
_TOO_MANY: typing.Final = 429
_FIRST_ERROR: typing.Final = 400

_SECONDS: typing.Final = re.compile(r"\d{1,9}(\.\d{1,9})?", re.ASCII)
_sleep = asyncio.sleep


class _ErrorDetail(msgspec.Struct, frozen=True, kw_only=True):
    code: str
    message: str
    fields: dict[str, str] | None = None


class _ErrorBody(msgspec.Struct, frozen=True, kw_only=True):
    error: _ErrorDetail


_BY_STATUS: typing.Final[dict[int, type[errors.APIError]]] = {
    400: errors.BadRequestError,
    401: errors.UnauthorizedError,
    403: errors.ForbiddenError,
    404: errors.NotFoundError,
    409: errors.ConflictError,
    422: errors.BadRequestError,
}
_SERVER: typing.Final = 500


def _api_error(
    status: int,
    code: str,
    message: str,
    fields: collections.abc.Mapping[str, str] | None = None,
    retry_after: float | None = None,
) -> errors.APIError:
    if status == _TOO_MANY:
        return errors.RateLimitedError(status, code, message, fields, retry_after)
    kind = _BY_STATUS.get(status, errors.ServerError if status >= _SERVER else errors.APIError)
    return kind(status, code, message, fields)


def _retry_after(response: aiohttp.ClientResponse) -> float | None:
    seconds = response.headers.get("Retry-After", "")
    return float(seconds) if _SECONDS.fullmatch(seconds) else None


class RESTClient:
    """Every call of the REST API as a method named after its operation in the API reference.

    Use it through `Client.rest`, or alone for a script that needs no gateway:
    `async with RESTClient(url, token=token) as rest: ...`.
    """

    def __init__(
        self,
        url: str,
        *,
        token: str,
        timeout: float = 30.0,
        retry: bool = True,
        session: aiohttp.ClientSession | None = None,
    ) -> None:
        """Make a client for the hub at `url`.

        Parameters
        ----------
        url
            The address of the hub, without `/api/v1`.
        token
            A personal token or the token of a connection.
        timeout
            How many seconds a call may take.
        retry
            Whether a call the hub turned away for its rate limit is tried again.
        session
            A session to use instead of one of the client's own. It is left open on `close`.
        """
        self._base: typing.Final = url.rstrip("/") + "/api/v1"
        self._token: typing.Final = token
        self._timeout: typing.Final = aiohttp.ClientTimeout(total=timeout)
        self._retry: typing.Final = retry
        self._session = session
        self._owns_session: typing.Final = session is None

    @property
    def is_connection(self) -> bool:
        """Whether the token belongs to a connection, which acts for many users."""
        return self._token.startswith(_CONNECTION_PREFIX)

    async def close(self) -> None:
        """Close the client's own session."""
        if self._owns_session and self._session is not None:
            session, self._session = self._session, None
            await session.close()

    async def __aenter__(self) -> typing.Self:
        return self

    async def __aexit__(
        self, kind: type[BaseException] | None, error: BaseException | None, trace: types.TracebackType | None
    ) -> None:
        await self.close()

    def _http(self) -> aiohttp.ClientSession:
        if self._session is None:
            self._session = aiohttp.ClientSession()
        return self._session

    def _user(self, user: ID | None) -> str:
        if user is None or user == routes.ME:
            if self.is_connection:
                msg = "A connection must say which user it acts for: pass user=..."
                raise TypeError(msg)
            return routes.ME
        return user

    def _account(self) -> str:
        if self.is_connection:
            msg = "This is about the account itself, so it takes a personal token and not a connection's."
            raise TypeError(msg)
        return routes.ME

    async def _send(
        self,
        route: routes.CompiledRoute,
        *,
        query: collections.abc.Mapping[str, object] | None = None,
        body: collections.abc.Mapping[str, object] | None = None,
        nonce: str | None = None,
    ) -> bytes:
        headers = {"Authorization": f"Bearer {self._token}", "Accept": "application/json"}
        if nonce is not None:
            headers["X-Request-Nonce"] = nonce
        data: bytes | None = None
        if body is not None:
            headers["Content-Type"] = "application/json"
            data = codec.encode_body(body)
        params = codec.encode_query(query) if query else None
        attempt = 0
        while True:
            try:
                async with self._http().request(
                    route.method,
                    self._base + route.path,
                    params=params,
                    data=data,
                    headers=headers,
                    timeout=self._timeout,
                ) as response:
                    status = response.status
                    wait = _retry_after(response) if status == _TOO_MANY else None
                    answer = await response.read()
            except (aiohttp.ClientError, TimeoutError) as cause:
                msg = f"The hub did not answer {route}."
                raise errors.NetworkError(msg) from cause
            if status < _FIRST_ERROR:
                return answer
            if self._retry and wait is not None and wait < _LONGEST_WAIT and attempt < _MAX_RETRIES:
                attempt += 1
                await _sleep(wait)
                continue
            try:
                detail = codec.decode(answer, _ErrorBody).error
            except msgspec.MsgspecError:
                detail = _ErrorDetail(code="unknown", message=f"The hub answered {route} with {status}.")
            raise _api_error(status, detail.code, detail.message, detail.fields, wait)

    async def _fetch[T](
        self,
        kind: type[T],
        route: routes.CompiledRoute,
        *,
        query: collections.abc.Mapping[str, object] | None = None,
        body: collections.abc.Mapping[str, object] | None = None,
        nonce: str | None = None,
    ) -> T:
        answer = await self._send(route, query=query, body=body, nonce=nonce)
        try:
            return codec.decode(answer, kind)
        except msgspec.MsgspecError as cause:
            msg = f"The hub's answer to {route} could not be read."
            raise errors.NetworkError(msg) from cause

    async def get_info(self) -> Info:
        """Ask the hub about itself: its name, its version and where its gateway is."""
        return await self._fetch(Info, routes.GET_INFO.compile())

    async def get_user(self, *, user: ID | None = None) -> Profile:
        """Fetch a user's profile: a `User` with a personal token, a plain `Profile` for a connection."""
        kind = Profile if self.is_connection else User
        return await self._fetch(kind, routes.GET_USER.compile(user=self._user(user)))

    async def update_user(
        self,
        *,
        display_name: str | _Unset = _UNSET,
        timezone: zoneinfo.ZoneInfo | str | _Unset = _UNSET,
        locale: str | _Unset | None = _UNSET,
        preferences: collections.abc.Mapping[str, object] | _Unset = _UNSET,
        quiet_hours: QuietHours | _Unset | None = _UNSET,
        nonce: str | None = None,
    ) -> User:
        """Change the user's profile. `preferences` replaces the stored ones as a whole."""
        body = {
            "display_name": display_name,
            "timezone": timezone,
            "locale": locale,
            "preferences": preferences,
            "quiet_hours": quiet_hours,
        }
        return await self._fetch(User, routes.UPDATE_USER.compile(user=self._account()), body=body, nonce=nonce)

    async def list_identities(self) -> tuple[Identity, ...]:
        """List the accounts the user signs in with or has linked."""
        return await self._fetch(tuple[Identity, ...], routes.LIST_IDENTITIES.compile(user=self._account()))

    async def list_lists(self, *, user: ID | None = None) -> tuple[List, ...]:
        """List a user's lists in their order."""
        return await self._fetch(tuple[List, ...], routes.LIST_LISTS.compile(user=self._user(user)))

    async def create_list(self, *, name: str, user: ID | None = None, nonce: str | None = None) -> List:
        """Create a list at the end."""
        return await self._fetch(
            List, routes.CREATE_LIST.compile(user=self._user(user)), body={"name": name}, nonce=nonce
        )

    async def reorder_lists(
        self, ids: collections.abc.Sequence[ID], *, user: ID | None = None, nonce: str | None = None
    ) -> tuple[List, ...]:
        """Put a user's lists into the order of `ids`, which names every one of them."""
        route = routes.REORDER_LISTS.compile(user=self._user(user))
        return await self._fetch(tuple[List, ...], route, body={"ids": ids}, nonce=nonce)

    async def rename_list(self, list_id: ID, *, name: str, user: ID | None = None, nonce: str | None = None) -> List:
        """Rename a list. The inbox keeps its name."""
        route = routes.RENAME_LIST.compile(user=self._user(user), id=list_id)
        return await self._fetch(List, route, body={"name": name}, nonce=nonce)

    async def delete_list(self, list_id: ID, *, user: ID | None = None, nonce: str | None = None) -> None:
        """Delete a list. Its items move to the inbox, which itself cannot be deleted."""
        await self._send(routes.DELETE_LIST.compile(user=self._user(user), id=list_id), nonce=nonce)

    async def list_reminder_plans(self, *, user: ID | None = None) -> tuple[ReminderPlan, ...]:
        """List a user's reminder plans, one for each priority."""
        return await self._fetch(tuple[ReminderPlan, ...], routes.LIST_REMINDER_PLANS.compile(user=self._user(user)))

    async def set_reminder_plan(
        self,
        priority: Priority,
        *,
        before: collections.abc.Sequence[datetime.timedelta],
        follow_up: FollowUp,
        nonce: str | None = None,
    ) -> ReminderPlan:
        """Replace the reminder plan of one priority."""
        route = routes.SET_REMINDER_PLAN.compile(user=self._account(), priority=priority.value)
        body = {"before": before, "follow_up": follow_up}
        return await self._fetch(ReminderPlan, route, body=body, nonce=nonce)

    async def reset_reminder_plan(self, priority: Priority, *, nonce: str | None = None) -> None:
        """Put the reminder plan of one priority back to the hub's default."""
        await self._send(routes.RESET_REMINDER_PLAN.compile(user=self._account(), priority=priority.value), nonce=nonce)

    async def list_items(
        self,
        *,
        list_id: ID | _Unset = _UNSET,
        status: ItemStatus | typing.Literal["all"] | _Unset = _UNSET,
        has_due: bool | _Unset = _UNSET,
        due_before: datetime.datetime | _Unset = _UNSET,
        due_after: datetime.datetime | _Unset = _UNSET,
        sort: ItemSort | _Unset = _UNSET,
        order: SortOrder | _Unset = _UNSET,
        limit: int | _Unset = _UNSET,
        cursor: str | None = None,
        user: ID | None = None,
    ) -> Page[Item]:
        """List one page of a user's items. Without `status` these are the open ones."""
        query = {
            "list_id": list_id,
            "status": status,
            "has_due": has_due,
            "due_before": due_before,
            "due_after": due_after,
            "sort": sort,
            "order": order,
            "limit": limit,
            "cursor": cursor,
        }
        return await self._fetch(Page[Item], routes.LIST_ITEMS.compile(user=self._user(user)), query=query)

    async def create_item(
        self,
        *,
        title: str,
        note: str | _Unset = _UNSET,
        list_id: ID | _Unset = _UNSET,
        priority: Priority | _Unset = _UNSET,
        due_at: datetime.datetime | _Unset | None = _UNSET,
        due_date: datetime.date | _Unset | None = _UNSET,
        subitems: collections.abc.Sequence[str] | _Unset = _UNSET,
        reminder_override: ReminderOverride | _Unset | None = _UNSET,
        user: ID | None = None,
        nonce: str | None = None,
    ) -> Item:
        """Create an item, in the inbox unless `list_id` says otherwise.

        Give `due_at` for a deadline at a moment or `due_date` for an all-day one.

        Raises
        ------
        TypeError
            If `due_date` is a `datetime`.
        """
        if isinstance(due_date, datetime.datetime):
            msg = "due_date takes a day; for a moment use due_at."
            raise TypeError(msg)
        body = {
            "title": title,
            "note": note,
            "list_id": list_id,
            "priority": priority,
            "due_at": due_at,
            "due_date": due_date,
            "subitems": subitems,
            "reminder_override": reminder_override,
        }
        return await self._fetch(Item, routes.CREATE_ITEM.compile(user=self._user(user)), body=body, nonce=nonce)

    async def get_item(self, item_id: ID, *, user: ID | None = None) -> Item:
        """Fetch one item with its subitems and manual reminders."""
        return await self._fetch(Item, routes.GET_ITEM.compile(user=self._user(user), id=item_id))

    async def update_item(
        self,
        item_id: ID,
        *,
        title: str | _Unset = _UNSET,
        note: str | _Unset = _UNSET,
        list_id: ID | _Unset = _UNSET,
        priority: Priority | _Unset = _UNSET,
        due_at: datetime.datetime | _Unset | None = _UNSET,
        due_date: datetime.date | _Unset | None = _UNSET,
        status: ItemStatus | _Unset = _UNSET,
        reminder_override: ReminderOverride | _Unset | None = _UNSET,
        user: ID | None = None,
        nonce: str | None = None,
    ) -> Item:
        """Change an item. What is left out stays, and `None` clears a deadline or an override.

        Raises
        ------
        TypeError
            If `due_date` is a `datetime`.
        """
        if isinstance(due_date, datetime.datetime):
            msg = "due_date takes a day; for a moment use due_at."
            raise TypeError(msg)
        body = {
            "title": title,
            "note": note,
            "list_id": list_id,
            "priority": priority,
            "due_at": due_at,
            "due_date": due_date,
            "status": status,
            "reminder_override": reminder_override,
        }
        return await self._fetch(
            Item, routes.UPDATE_ITEM.compile(user=self._user(user), id=item_id), body=body, nonce=nonce
        )

    async def delete_item(self, item_id: ID, *, user: ID | None = None, nonce: str | None = None) -> None:
        """Delete an item."""
        await self._send(routes.DELETE_ITEM.compile(user=self._user(user), id=item_id), nonce=nonce)

    async def create_subitem(
        self, item_id: ID, *, title: str, user: ID | None = None, nonce: str | None = None
    ) -> Subitem:
        """Add a subitem at the end of an item's."""
        route = routes.CREATE_SUBITEM.compile(user=self._user(user), id=item_id)
        return await self._fetch(Subitem, route, body={"title": title}, nonce=nonce)

    async def reorder_subitems(
        self, item_id: ID, ids: collections.abc.Sequence[ID], *, user: ID | None = None, nonce: str | None = None
    ) -> tuple[Subitem, ...]:
        """Put an item's subitems into the order of `ids`, which names every one of them."""
        route = routes.REORDER_SUBITEMS.compile(user=self._user(user), id=item_id)
        return await self._fetch(tuple[Subitem, ...], route, body={"ids": ids}, nonce=nonce)

    async def update_subitem(
        self,
        item_id: ID,
        subitem_id: ID,
        *,
        title: str | _Unset = _UNSET,
        done: bool | _Unset = _UNSET,
        user: ID | None = None,
        nonce: str | None = None,
    ) -> Subitem:
        """Rename a subitem or tick it off."""
        route = routes.UPDATE_SUBITEM.compile(user=self._user(user), id=item_id, sid=subitem_id)
        return await self._fetch(Subitem, route, body={"title": title, "done": done}, nonce=nonce)

    async def delete_subitem(
        self, item_id: ID, subitem_id: ID, *, user: ID | None = None, nonce: str | None = None
    ) -> None:
        """Delete a subitem."""
        await self._send(routes.DELETE_SUBITEM.compile(user=self._user(user), id=item_id, sid=subitem_id), nonce=nonce)

    async def get_item_schedule(self, item_id: ID, *, user: ID | None = None) -> tuple[ScheduledReminder, ...]:
        """List the reminders an item still has ahead, as the hub has them scheduled."""
        route = routes.GET_ITEM_SCHEDULE.compile(user=self._user(user), id=item_id)
        return await self._fetch(tuple[ScheduledReminder, ...], route)

    async def list_reminders(self, item_id: ID, *, user: ID | None = None) -> tuple[ManualReminder, ...]:
        """List the manual reminders of an item."""
        route = routes.LIST_REMINDERS.compile(user=self._user(user), id=item_id)
        return await self._fetch(tuple[ManualReminder, ...], route)

    async def create_reminder(
        self,
        item_id: ID,
        *,
        at: datetime.datetime | _Unset = _UNSET,
        after: datetime.timedelta | _Unset = _UNSET,
        user: ID | None = None,
        nonce: str | None = None,
    ) -> ManualReminder:
        """Remind the user of an item at a moment (`at`) or once a span of time has passed (`after`).

        Raises
        ------
        ValueError
            If neither or both are given.
        """
        if (at is _UNSET) == (after is _UNSET):
            msg = "Say when to remind with exactly one of at=... and after=..."
            raise ValueError(msg)
        route = routes.CREATE_REMINDER.compile(user=self._user(user), id=item_id)
        return await self._fetch(ManualReminder, route, body={"at": at, "in": after}, nonce=nonce)

    async def delete_reminder(
        self, item_id: ID, reminder_id: ID, *, user: ID | None = None, nonce: str | None = None
    ) -> None:
        """Delete a manual reminder."""
        await self._send(
            routes.DELETE_REMINDER.compile(user=self._user(user), id=item_id, rid=reminder_id), nonce=nonce
        )

    async def lookup_user(self, external_id: str) -> LookupResult:
        """For a connection: find the hub user behind an account at the connection's provider."""
        return await self._fetch(LookupResult, routes.LOOKUP_USER.compile(), query={"external_id": external_id})

    async def list_user_connections(self) -> tuple[UserConnection, ...]:
        """List the hub's connections as the user sees them."""
        return await self._fetch(tuple[UserConnection, ...], routes.LIST_USER_CONNECTIONS.compile(user=self._account()))

    async def set_connection_grant(
        self,
        connection_id: ID,
        *,
        enabled: bool | _Unset = _UNSET,
        remind: bool | _Unset = _UNSET,
        nonce: str | None = None,
    ) -> UserConnection:
        """Say whether a connection may act for the user and whether it reminds them."""
        route = routes.SET_CONNECTION_GRANT.compile(user=self._account(), id=connection_id)
        body = {"enabled": enabled, "remind": remind}
        return await self._fetch(UserConnection, route, body=body, nonce=nonce)

    async def list_deliveries(self) -> tuple[Delivery, ...]:
        """List the open deliveries of this token, for adapters that do not use the gateway.

        A personal token needs `remind` switched on for this.
        """
        return await self._fetch(tuple[Delivery, ...], routes.LIST_DELIVERIES.compile())

    async def acknowledge_delivery(
        self, delivery_id: ID, *, status: DeliveryStatus, reason: str | _Unset = _UNSET
    ) -> DeliveryResult:
        """Tell the hub that a reminder reached its user, or why it could not.

        The reason of a failed delivery is shown to the user.

        Raises
        ------
        ValueError
            If the status is neither `DELIVERED` nor `FAILED`, or a failure comes without a reason.
        ConflictError
            With the code `delivery_closed` if the hub closed the delivery in the meantime.
        """
        if status is DeliveryStatus.FAILED:
            if not reason:
                msg = "A failed delivery needs a reason, which the user gets to see."
                raise ValueError(msg)
        elif status is not DeliveryStatus.DELIVERED:
            msg = "A delivery is acknowledged as DELIVERED or as FAILED."
            raise ValueError(msg)
        route = routes.ACKNOWLEDGE_DELIVERY.compile(id=delivery_id)
        return await self._fetch(DeliveryResult, route, body={"status": status, "reason": reason})

    async def list_reminder_log(
        self,
        *,
        item_id: ID | _Unset = _UNSET,
        limit: int | _Unset = _UNSET,
        cursor: str | None = None,
    ) -> Page[ReminderLogEntry]:
        """List one page of the reminders that came due for the user, newest first."""
        query = {"item_id": item_id, "limit": limit, "cursor": cursor}
        return await self._fetch(
            Page[ReminderLogEntry], routes.LIST_REMINDER_LOG.compile(user=self._account()), query=query
        )


async def paginate[T, **P](
    call: collections.abc.Callable[P, collections.abc.Awaitable[Page[T]]], *args: P.args, **kwargs: P.kwargs
) -> collections.abc.AsyncIterator[T]:
    """Walk a listing to its end: `async for item in paginate(client.rest.list_items, status=...)`."""
    fetch = typing.cast("collections.abc.Callable[..., collections.abc.Awaitable[Page[T]]]", call)
    arguments: dict[str, object] = dict(kwargs)
    while True:
        page = await fetch(*args, **arguments)
        for entry in page.data:
            yield entry
        if page.next_cursor is None:
            return
        if page.next_cursor == arguments.get("cursor"):
            msg = "The hub answered with the same cursor it was asked with, so the listing would never end."
            raise errors.PlsremindError(msg)
        arguments["cursor"] = page.next_cursor
