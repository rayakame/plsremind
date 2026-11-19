from __future__ import annotations

import datetime
import pathlib
import typing
import zoneinfo

import plsremind

BERLIN: typing.Final = zoneinfo.ZoneInfo("Europe/Berlin")
# A Thursday.
NOW: typing.Final = datetime.datetime(2026, 11, 19, 14, 30, tzinfo=BERLIN)
CREATED: typing.Final = datetime.datetime(2026, 11, 1, 9, 0, tzinfo=datetime.UTC)
DATABASE: typing.Final = pathlib.Path("discord.db")


def uid(number: int) -> plsremind.ID:
    return plsremind.ID(f"01930000-0000-7000-8000-{number:012x}")


MIA: typing.Final = uid(1)
BEN: typing.Final = uid(2)
INBOX: typing.Final = uid(100)
WORK: typing.Final = uid(101)


def user(
    user_id: plsremind.ID = MIA,
    *,
    locale: str | None = "de",
    external_ids: tuple[str, ...] = ("1234",),
    remind: bool = True,
    zone: zoneinfo.ZoneInfo = BERLIN,
) -> plsremind.ConnectionUser:
    return plsremind.ConnectionUser(
        id=user_id,
        display_name="Mia",
        timezone=zone,
        quiet_hours=None,
        locale=locale,
        external_ids=external_ids,
        remind=remind,
    )


def a_list(list_id: plsremind.ID = INBOX, name: str = "Inbox", position: int = 0) -> plsremind.List:
    return plsremind.List(
        id=list_id, name=name, position=position, is_inbox=list_id == INBOX, created_at=CREATED, updated_at=CREATED
    )


def subitem(number: int, item_id: plsremind.ID, *, title: str | None = None, done: bool = False) -> plsremind.Subitem:
    return plsremind.Subitem(
        id=uid(5000 + number),
        item_id=item_id,
        title=title or f"step {number}",
        done=done,
        position=number,
        created_at=CREATED,
        updated_at=CREATED,
    )


def reminder(number: int, item_id: plsremind.ID, at: datetime.datetime) -> plsremind.ManualReminder:
    return plsremind.ManualReminder(id=uid(7000 + number), item_id=item_id, remind_at=at, created_at=CREATED)


def item(
    number: int = 1,
    *,
    title: str | None = None,
    note: str = "",
    list_id: plsremind.ID = INBOX,
    priority: plsremind.Priority = plsremind.Priority.NORMAL,
    due_at: datetime.datetime | None = None,
    due_date: datetime.date | None = None,
    done: bool = False,
    subitems: int = 0,
    reminders: tuple[datetime.datetime, ...] = (),
    updated_at: datetime.datetime = CREATED,
) -> plsremind.Item:
    item_id = uid(1000 + number)
    if due_date is not None:
        # The hub puts an all-day deadline at the last instant of its day in the user's zone.
        due_at = datetime.datetime.combine(due_date, datetime.time(23, 59, 59), tzinfo=BERLIN)
    return plsremind.Item(
        id=item_id,
        list_id=list_id,
        title=title or f"item {number}",
        note=note,
        priority=priority,
        due_at=due_at,
        due_date=due_date,
        all_day=due_date is not None,
        status=plsremind.ItemStatus.DONE if done else plsremind.ItemStatus.OPEN,
        completed_at=NOW if done else None,
        reminder_override=None,
        subitems=tuple(subitem(index, item_id) for index in range(1, subitems + 1)),
        reminders=tuple(reminder(index, item_id, at) for index, at in enumerate(reminders, start=1)),
        created_at=CREATED,
        updated_at=updated_at,
    )


def delivery(
    number: int = 1,
    *,
    of: plsremind.Item | None = None,
    to: plsremind.ConnectionUser | None = None,
    scheduled_for: datetime.datetime = NOW,
) -> plsremind.Delivery:
    target = to or user()
    return plsremind.Delivery(
        id=uid(9000 + number),
        attempt=1,
        late=False,
        reminder=plsremind.DeliveryReminder(
            id=uid(8000 + number), kind=plsremind.ReminderKind.MANUAL, scheduled_for=scheduled_for
        ),
        item=of or item(),
        user=plsremind.DeliveryUser(
            id=target.id,
            display_name=target.display_name,
            timezone=target.timezone,
            locale=target.locale,
            external_ids=target.external_ids,
        ),
    )


def scheduled(at: datetime.datetime) -> plsremind.ScheduledReminder:
    return plsremind.ScheduledReminder(
        kind=plsremind.ReminderKind.BEFORE, offset=None, reminder_id=None, scheduled_for=at, fire_at=at
    )


# Every character of it has to be escaped, which doubles what it takes up.
WORST: typing.Final = "*_~`|#[]<>\\" * 60
# Outside the basic plane: one character, but two where text is counted in UTF-16.
WIDE: typing.Final = "\U00020000"
