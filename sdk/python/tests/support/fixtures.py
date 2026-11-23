from __future__ import annotations

import json
import typing

from plsremind import models

USER_ID = "01930000-0000-7000-8000-000000000001"
OTHER_USER_ID = "01930000-0000-7000-8000-000000000002"
LIST_ID = "01930000-0000-7000-8000-0000000000a1"
ITEM_ID = "01930000-0000-7000-8000-0000000000b1"
SUBITEM_ID = "01930000-0000-7000-8000-0000000000c1"
REMINDER_ID = "01930000-0000-7000-8000-0000000000d1"
DELIVERY_ID = "01930000-0000-7000-8000-0000000000e1"
CONNECTION_ID = "01930000-0000-7000-8000-0000000000f1"
TOKEN_ID = "01930000-0000-7000-8000-0000000000f2"

QUIET_HOURS: dict[str, typing.Any] = {"start": "22:00", "end": "07:30"}

PROFILE: dict[str, typing.Any] = {
    "id": USER_ID,
    "display_name": "Mia",
    "timezone": "Europe/Berlin",
    "quiet_hours": QUIET_HOURS,
    "locale": "de",
}
USER: dict[str, typing.Any] = {
    **PROFILE,
    "role": "user",
    "preferences": {"theme": "dark", "keys": {"done": "x"}},
    "created_at": "2026-11-01T09:00:00Z",
}
CONNECTION_USER: dict[str, typing.Any] = {**PROFILE, "external_ids": ["1234", "5678"], "remind": True}

LIST: dict[str, typing.Any] = {
    "id": LIST_ID,
    "name": "Inbox",
    "position": 0,
    "is_inbox": True,
    "created_at": "2026-11-01T09:00:00Z",
    "updated_at": "2026-11-01T09:00:00Z",
}
SUBITEM: dict[str, typing.Any] = {
    "id": SUBITEM_ID,
    "item_id": ITEM_ID,
    "title": "Print it",
    "done": False,
    "position": 0,
    "created_at": "2026-11-10T08:00:00Z",
    "updated_at": "2026-11-10T08:00:00Z",
}
MANUAL_REMINDER: dict[str, typing.Any] = {
    "id": REMINDER_ID,
    "item_id": ITEM_ID,
    "remind_at": "2026-11-12T10:00:00Z",
    "created_at": "2026-11-10T08:00:00Z",
}
ITEM: dict[str, typing.Any] = {
    "id": ITEM_ID,
    "list_id": LIST_ID,
    "title": "Hand in the report",
    "note": "Two copies",
    "priority": "high",
    "due_at": "2026-11-13T18:00:00Z",
    "due_date": "2026-11-13",
    "all_day": False,
    "status": "open",
    "completed_at": None,
    "reminder_override": {"mode": "custom", "before": ["3h"]},
    "subitems": [SUBITEM],
    "reminders": [MANUAL_REMINDER],
    "created_at": "2026-11-10T08:00:00Z",
    "updated_at": "2026-11-10T08:00:00.123456Z",
}
PLAIN_ITEM: dict[str, typing.Any] = {
    **ITEM,
    "priority": "normal",
    "due_at": None,
    "due_date": None,
    "reminder_override": None,
    "subitems": [],
    "reminders": [],
}
REMINDER_PLAN: dict[str, typing.Any] = {
    "priority": "high",
    "before": ["7d", "3d", "1d", "3h"],
    "follow_up": "twice_daily",
    "is_default": True,
}
SCHEDULED_REMINDER: dict[str, typing.Any] = {
    "kind": "before",
    "offset": "1d",
    "reminder_id": None,
    "scheduled_for": "2026-11-12T18:00:00Z",
    "fire_at": "2026-11-13T06:30:00Z",
}
USER_CONNECTION: dict[str, typing.Any] = {
    "id": CONNECTION_ID,
    "name": "Discord",
    "description": "Reminders as direct messages",
    "provider": "discord",
    "linked": True,
    "enabled": True,
    "remind": True,
    "intents": ["items", "deliveries"],
}
IDENTITY: dict[str, typing.Any] = {
    "id": "01930000-0000-7000-8000-000000000011",
    "provider": "discord",
    "external_id": "1234",
    "display_name": "mia",
    "created_at": "2026-11-01T09:00:00Z",
}
INFO: dict[str, typing.Any] = {
    "name": "plsremind",
    "version": "0.3.0",
    "gateway_url": "wss://remind.example.com/api/v1/gateway",
    "setup_required": False,
    "registration": "invite",
    "login_methods": [{"key": "password", "type": "password", "name": "Password", "login": True, "link": False}],
}
LOOKUP_RESULT: dict[str, typing.Any] = {"id": USER_ID, "display_name": "Mia", "timezone": "Europe/Berlin"}
DELIVERY: dict[str, typing.Any] = {
    "id": DELIVERY_ID,
    "attempt": 1,
    "late": False,
    "reminder": {"id": REMINDER_ID, "kind": "before", "offset": "1d", "scheduled_for": "2026-11-12T18:00:00Z"},
    "item": ITEM,
    "user": {
        "id": USER_ID,
        "display_name": "Mia",
        "timezone": "Europe/Berlin",
        "locale": "de",
        "external_ids": ["1234"],
    },
}
DELIVERY_RESULT: dict[str, typing.Any] = {"id": DELIVERY_ID, "status": "delivered", "reason": ""}
TEST_ID = "01930000-0000-7000-8000-000000000091"
CONNECTION_TEST: dict[str, typing.Any] = {
    "id": TEST_ID,
    "connection_id": CONNECTION_ID,
    "status": "failed",
    "reason": "direct messages are closed",
    "created_at": "2026-11-12T18:00:00Z",
    "closed_at": "2026-11-12T18:00:02Z",
}
DELIVERY_TEST: dict[str, typing.Any] = {
    "id": TEST_ID,
    "user": {
        "id": USER_ID,
        "display_name": "Mia",
        "timezone": "Europe/Berlin",
        "locale": "de",
        "external_ids": ["1234"],
    },
}
REMINDER_LOG_ENTRY: dict[str, typing.Any] = {
    "id": REMINDER_ID,
    "item_id": ITEM_ID,
    "kind": "before",
    "offset": "1d",
    "scheduled_for": "2026-11-12T18:00:00Z",
    "fired_at": "2026-11-12T18:00:01Z",
    "outcome": "fired",
    "deliveries": [
        {
            "id": DELIVERY_ID,
            "channel": {"kind": "connection", "id": CONNECTION_ID, "name": "Discord"},
            "status": "failed",
            "reason": "direct messages are closed",
            "attempts": 2,
        }
    ],
}
ACTOR: dict[str, typing.Any] = {"kind": "personal_token", "id": TOKEN_ID}
PRINCIPAL: dict[str, typing.Any] = {"kind": "personal_token", "id": TOKEN_ID, "user_id": USER_ID}
CONNECTION_PRINCIPAL: dict[str, typing.Any] = {
    "kind": "connection",
    "id": CONNECTION_ID,
    "connection": {"id": CONNECTION_ID, "name": "Discord", "provider": "discord"},
}

ALL: list[tuple[type[typing.Any], dict[str, typing.Any]]] = [
    (models.QuietHours, QUIET_HOURS),
    (models.Profile, PROFILE),
    (models.User, USER),
    (models.ConnectionUser, CONNECTION_USER),
    (models.List, LIST),
    (models.Subitem, SUBITEM),
    (models.ManualReminder, MANUAL_REMINDER),
    (models.Item, ITEM),
    (models.Item, PLAIN_ITEM),
    (models.ReminderPlan, REMINDER_PLAN),
    (models.ScheduledReminder, SCHEDULED_REMINDER),
    (models.UserConnection, USER_CONNECTION),
    (models.Identity, IDENTITY),
    (models.Info, INFO),
    (models.LookupResult, LOOKUP_RESULT),
    (models.Delivery, DELIVERY),
    (models.DeliveryResult, DELIVERY_RESULT),
    (models.ReminderLogEntry, REMINDER_LOG_ENTRY),
    (models.Actor, ACTOR),
    (models.Principal, PRINCIPAL),
    (models.Principal, CONNECTION_PRINCIPAL),
]


def raw(document: object) -> bytes:
    return json.dumps(document).encode()
