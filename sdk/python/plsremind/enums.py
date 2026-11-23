"""The fixed sets of values the hub uses."""

from __future__ import annotations

import enum
import typing

if typing.TYPE_CHECKING:
    import collections.abc

__all__ = [
    "ChannelKind",
    "ClientStatus",
    "ConnectionTestStatus",
    "CredentialKind",
    "DeliveryStatus",
    "FollowUp",
    "Intents",
    "ItemSort",
    "ItemStatus",
    "OverrideMode",
    "Priority",
    "ProviderType",
    "Registration",
    "ReminderKind",
    "ReminderOutcome",
    "Role",
    "SortOrder",
    "intent_names",
    "intents_from_names",
]


class _Received(enum.Enum):
    @classmethod
    def _missing_(cls, value: object) -> _Received:  # ruff: ignore[unused-class-method-argument]
        return cls["UNKNOWN"]


class Priority(_Received):
    """How important an item is."""

    LOW = "low"
    NORMAL = "normal"
    HIGH = "high"
    UNKNOWN = "unknown"


class ItemStatus(_Received):
    """Whether an item is still to do."""

    OPEN = "open"
    DONE = "done"
    UNKNOWN = "unknown"


class FollowUp(_Received):
    """How often the hub reminds again once a deadline has passed."""

    NEVER = "never"
    DAILY = "daily"
    TWICE_DAILY = "twice_daily"
    UNKNOWN = "unknown"


class ReminderKind(_Received):
    """Why a reminder goes out."""

    BEFORE = "before"
    DUE = "due"
    FOLLOW_UP = "follow_up"
    MANUAL = "manual"
    UNKNOWN = "unknown"


class DeliveryStatus(_Received):
    """What became of a delivery."""

    PENDING = "pending"
    DELIVERED = "delivered"
    FAILED = "failed"
    SUPERSEDED = "superseded"
    CANCELLED = "cancelled"
    EXPIRED = "expired"
    UNKNOWN = "unknown"


class ConnectionTestStatus(_Received):
    """What became of a test the user asked an app for."""

    PENDING = "pending"
    DELIVERED = "delivered"
    FAILED = "failed"
    UNANSWERED = "unanswered"
    UNKNOWN = "unknown"


class Role(_Received):
    """What a user may do on the hub."""

    USER = "user"
    ADMIN = "admin"
    UNKNOWN = "unknown"


class Registration(_Received):
    """Who may create an account."""

    OPEN = "open"
    INVITE = "invite"
    UNKNOWN = "unknown"


class ProviderType(_Received):
    """A way to sign in."""

    PASSWORD = "password"  # ruff: ignore[hardcoded-password-string]
    GITHUB = "github"
    DISCORD = "discord"
    OIDC = "oidc"
    UNKNOWN = "unknown"


class CredentialKind(_Received):
    """What someone came to the hub with."""

    SESSION = "session"
    PERSONAL_TOKEN = "personal_token"  # ruff: ignore[hardcoded-password-string]
    CONNECTION = "connection"
    UNKNOWN = "unknown"


class OverrideMode(_Received):
    """How an item departs from the reminder plan of its priority."""

    OFF = "off"
    CUSTOM = "custom"
    UNKNOWN = "unknown"


class ChannelKind(_Received):
    """What a reminder went out through."""

    CONNECTION = "connection"
    TOKEN = "token"  # ruff: ignore[hardcoded-password-string]
    UNKNOWN = "unknown"


class ReminderOutcome(_Received):
    """What became of a reminder when its time came."""

    FIRED = "fired"
    MISSED = "missed"
    SUPERSEDED = "superseded"
    UNKNOWN = "unknown"


class ItemSort(enum.Enum):
    """What a listing of items is ordered by."""

    CREATED = "created"
    DUE = "due"
    PRIORITY = "priority"
    COMPLETED = "completed"


class SortOrder(enum.Enum):
    """The direction of a listing."""

    ASCENDING = "asc"
    DESCENDING = "desc"


class ClientStatus(enum.Enum):
    """Where a client stands with its gateway connection."""

    IDLE = "idle"
    CONNECTING = "connecting"
    CONNECTED = "connected"
    RECONNECTING = "reconnecting"
    CLOSED = "closed"


class Intents(enum.Flag):
    """What a client asks the gateway for."""

    NONE = 0
    USERS = enum.auto()
    LISTS = enum.auto()
    ITEMS = enum.auto()
    PLANS = enum.auto()
    HISTORY = enum.auto()
    DELIVERIES = enum.auto()
    ALL = USERS | LISTS | ITEMS | PLANS | HISTORY | DELIVERIES


_INTENT_NAMES: typing.Final = (
    (Intents.USERS, "users"),
    (Intents.LISTS, "lists"),
    (Intents.ITEMS, "items"),
    (Intents.PLANS, "plans"),
    (Intents.HISTORY, "history"),
    (Intents.DELIVERIES, "deliveries"),
)


def intent_names(intents: Intents) -> list[str]:
    """Return the names of the intents as the gateway spells them."""
    return [name for intent, name in _INTENT_NAMES if intent in intents]


def intents_from_names(names: collections.abc.Iterable[str]) -> Intents:
    """Build intents from the gateway's names, leaving out the ones this version does not know."""
    known = {name: intent for intent, name in _INTENT_NAMES}
    result = Intents.NONE
    for name in names:
        result |= known.get(name, Intents.NONE)
    return result
