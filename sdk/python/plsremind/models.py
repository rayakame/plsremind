"""What the hub sends: plain, frozen data."""

from __future__ import annotations

import datetime
import sys
import types
import typing
import zoneinfo

import msgspec

from plsremind.enums import ChannelKind
from plsremind.enums import ConnectionTestStatus
from plsremind.enums import CredentialKind
from plsremind.enums import DeliveryStatus
from plsremind.enums import FollowUp
from plsremind.enums import Intents
from plsremind.enums import ItemStatus
from plsremind.enums import OverrideMode
from plsremind.enums import Priority
from plsremind.enums import ProviderType
from plsremind.enums import Registration
from plsremind.enums import ReminderKind
from plsremind.enums import ReminderOutcome
from plsremind.enums import Role
from plsremind.types import ID
from plsremind.types import ClockTime
from plsremind.types import Duration

__all__ = [
    "Actor",
    "ConnectionTest",
    "ConnectionUser",
    "Delivery",
    "DeliveryReminder",
    "DeliveryResult",
    "DeliveryTest",
    "DeliveryUser",
    "Identity",
    "Info",
    "Item",
    "LastDelivery",
    "List",
    "LoginMethod",
    "LookupResult",
    "ManualReminder",
    "Page",
    "Principal",
    "PrincipalConnection",
    "Profile",
    "QuietHours",
    "ReminderLogChannel",
    "ReminderLogDelivery",
    "ReminderLogEntry",
    "ReminderOverride",
    "ReminderPlan",
    "ScheduledReminder",
    "Subitem",
    "User",
    "UserConnection",
]


class _IntentNamesMeta(type):
    # msgspec checks what the decoding hook returns against the annotation, and this lets `Intents` pass.
    def __instancecheck__(cls, instance: object) -> bool:
        return isinstance(instance, Intents)


class _IntentNames(metaclass=_IntentNamesMeta):
    pass


if typing.TYPE_CHECKING:
    import collections.abc

    _Preferences = collections.abc.Mapping[str, object]
    _Intents = Intents
else:
    # msgspec decodes a frozendict itself; before 3.15 the decoding hook wraps the dict.
    _Preferences = (
        frozendict[str, typing.Any]  # ruff: ignore[undefined-name]
        if sys.version_info >= (3, 15)
        else types.MappingProxyType
    )
    _Intents = _IntentNames


class QuietHours(msgspec.Struct, frozen=True, kw_only=True, gc=False):
    """The local hours in which a user is not reminded."""

    start: ClockTime
    end: ClockTime


class Profile(msgspec.Struct, frozen=True, kw_only=True, gc=False):
    """What everyone who may act for a user sees of them."""

    id: ID
    display_name: str
    timezone: zoneinfo.ZoneInfo
    quiet_hours: QuietHours | None
    locale: str | None


class User(Profile, frozen=True, kw_only=True, gc=True):
    """A user as they see themselves."""

    role: Role
    preferences: _Preferences
    """The user's own client settings. Only the top level is read-only."""
    created_at: datetime.datetime


class ConnectionUser(Profile, frozen=True, kw_only=True, gc=False):
    """A user as the gateway shows them to a connection."""

    external_ids: tuple[str, ...]
    """The user's accounts at the connection's provider."""
    remind: bool
    """Whether the user wants reminders through this connection."""


class Identity(msgspec.Struct, frozen=True, kw_only=True, gc=False):
    """An account elsewhere that a user signs in with or has linked."""

    id: ID
    provider: str
    external_id: str
    display_name: str
    created_at: datetime.datetime
    username: str = ""
    """The handle at the provider; empty when it has none or the account was linked before the hub kept it."""
    has_avatar: bool = False
    """Whether `RESTClient.get_identity_avatar` has a picture."""


class LoginMethod(msgspec.Struct, frozen=True, kw_only=True, gc=False):
    """One way to sign in to a hub."""

    key: str
    type: ProviderType
    name: str
    login: bool = True
    """Whether people can log in with it. Hubs that do not say only list those that allow it."""
    link: bool = False
    """Whether logged-in users can link their account there."""


class Info(msgspec.Struct, frozen=True, kw_only=True, gc=False):
    """What a hub says about itself to anyone."""

    name: str
    version: str
    gateway_url: str
    setup_required: bool
    registration: Registration
    login_methods: tuple[LoginMethod, ...]


class List(msgspec.Struct, frozen=True, kw_only=True, gc=False):
    """A list of items."""

    id: ID
    name: str
    position: int
    is_inbox: bool
    created_at: datetime.datetime
    updated_at: datetime.datetime


class Subitem(msgspec.Struct, frozen=True, kw_only=True, gc=False):
    """A step of an item that can be ticked off."""

    id: ID
    item_id: ID
    title: str
    done: bool
    position: int
    created_at: datetime.datetime
    updated_at: datetime.datetime


class ManualReminder(msgspec.Struct, frozen=True, kw_only=True, gc=False):
    """A reminder a user asked for at a time of their choosing."""

    id: ID
    item_id: ID
    remind_at: datetime.datetime
    created_at: datetime.datetime


class ReminderOverride(msgspec.Struct, frozen=True, kw_only=True, gc=False):
    """How one item departs from the reminder plan of its priority."""

    mode: OverrideMode
    before: tuple[Duration, ...] = ()
    """How long before the deadline to remind, with `OverrideMode.CUSTOM`."""


class Item(msgspec.Struct, frozen=True, kw_only=True, gc=False):
    """A todo or a deadline."""

    id: ID
    list_id: ID
    title: str
    note: str
    priority: Priority
    due_at: datetime.datetime | None
    due_date: datetime.date | None
    """The day of an all-day item."""
    all_day: bool
    status: ItemStatus
    completed_at: datetime.datetime | None
    reminder_override: ReminderOverride | None
    subitems: tuple[Subitem, ...]
    reminders: tuple[ManualReminder, ...]
    created_at: datetime.datetime
    updated_at: datetime.datetime


class ReminderPlan(msgspec.Struct, frozen=True, kw_only=True, gc=False):
    """When a user is reminded of deadlines of one priority."""

    priority: Priority
    before: tuple[Duration, ...]
    follow_up: FollowUp
    is_default: bool


class ScheduledReminder(msgspec.Struct, frozen=True, kw_only=True, gc=False):
    """A reminder an item still has ahead."""

    kind: ReminderKind
    offset: Duration | None
    reminder_id: ID | None
    scheduled_for: datetime.datetime
    fire_at: datetime.datetime
    """When it goes out, which quiet hours can move past `scheduled_for`."""


class UserConnection(msgspec.Struct, frozen=True, kw_only=True, gc=False):
    """A connection as one user sees it."""

    id: ID
    name: str
    description: str
    provider: str
    linked: bool
    enabled: bool
    remind: bool
    intents: _Intents
    install_url: str = ""
    """Where the user adds the app on the provider's side; empty when there is no such place."""
    installs_with_link: bool = False
    """Whether linking the account at the provider also adds the app; then start the link instead."""
    last_delivery: LastDelivery | None = None
    """How the newest delivery through this connection to the user went, `None` before the first."""


class LastDelivery(msgspec.Struct, frozen=True, kw_only=True, gc=False):
    """How a delivery, or a test the user sent, went."""

    status: DeliveryStatus
    reason: str
    """What the connection reported when it failed; empty otherwise."""
    at: datetime.datetime
    """When it was closed, or created while it is still pending."""
    test: bool
    """Whether it was a test the user sent, not a reminder."""


class ConnectionTest(msgspec.Struct, frozen=True, kw_only=True, gc=False):
    """A test the user asked an app to send."""

    id: ID
    connection_id: ID
    status: ConnectionTestStatus
    reason: str
    """For `FAILED` what the app reported; for `UNANSWERED` `not_connected` or `no_answer`."""
    created_at: datetime.datetime
    closed_at: datetime.datetime | None


class LookupResult(msgspec.Struct, frozen=True, kw_only=True, gc=False):
    """The hub user behind an account at a connection's provider."""

    id: ID
    display_name: str
    timezone: zoneinfo.ZoneInfo


class DeliveryReminder(msgspec.Struct, frozen=True, kw_only=True, gc=False):
    """The reminder a delivery carries."""

    id: ID
    kind: ReminderKind
    scheduled_for: datetime.datetime
    offset: Duration | None = None
    """How far ahead of the deadline, for `ReminderKind.BEFORE`."""


class DeliveryUser(msgspec.Struct, frozen=True, kw_only=True, gc=False):
    """Whom a delivery is for."""

    id: ID
    display_name: str
    timezone: zoneinfo.ZoneInfo
    locale: str | None
    external_ids: tuple[str, ...] = ()
    """For a connection: the user's accounts at its provider."""


class Delivery(msgspec.Struct, frozen=True, kw_only=True, gc=False):
    """A reminder waiting to reach its user through this token or connection."""

    id: ID
    attempt: int
    late: bool
    reminder: DeliveryReminder
    item: Item
    user: DeliveryUser


class DeliveryTest(msgspec.Struct, frozen=True, kw_only=True, gc=False):
    """What an app receives to show the user that it reaches them."""

    id: ID
    user: DeliveryUser


class DeliveryResult(msgspec.Struct, frozen=True, kw_only=True, gc=False):
    """A delivery after it was acknowledged."""

    id: ID
    status: DeliveryStatus
    reason: str


class ReminderLogChannel(msgspec.Struct, frozen=True, kw_only=True, gc=False):
    """The connection or token a reminder went out through."""

    kind: ChannelKind
    id: ID
    name: str


class ReminderLogDelivery(msgspec.Struct, frozen=True, kw_only=True, gc=False):
    """How a past reminder fared on one channel."""

    id: ID
    channel: ReminderLogChannel
    status: DeliveryStatus
    reason: str
    attempts: int


class ReminderLogEntry(msgspec.Struct, frozen=True, kw_only=True, gc=False):
    """A reminder that came due."""

    id: ID
    item_id: ID
    kind: ReminderKind
    scheduled_for: datetime.datetime
    fired_at: datetime.datetime
    outcome: ReminderOutcome
    deliveries: tuple[ReminderLogDelivery, ...]
    offset: Duration | None = None


class Actor(msgspec.Struct, frozen=True, kw_only=True, gc=False):
    """Who made a change. Compare `id` with `client.principal.id` to recognise your own."""

    kind: CredentialKind
    id: ID


class PrincipalConnection(msgspec.Struct, frozen=True, kw_only=True, gc=False):
    """The connection a client is logged in as."""

    id: ID
    name: str
    provider: str


class Principal(msgspec.Struct, frozen=True, kw_only=True, gc=False):
    """Who a client is logged in as."""

    kind: CredentialKind
    id: ID
    """The token or connection the client came with."""
    user_id: ID | None = None
    """The one user of a personal token."""
    connection: PrincipalConnection | None = None


class Page[T](msgspec.Struct, frozen=True, kw_only=True, gc=False):
    """One page of a listing."""

    data: tuple[T, ...]
    next_cursor: str | None
    """Pass it as `cursor` for the next page. `None` on the last one."""
