from __future__ import annotations

import typing

import msgspec
import pytest

from plsremind import _events
from plsremind import events
from plsremind import models
from plsremind._cache import CacheStore
from plsremind.enums import Intents
from plsremind.enums import ItemStatus
from plsremind.enums import Priority
from plsremind.types import ID
from tests import fixtures

ME = ID(fixtures.USER_ID)
OTHER = ID(fixtures.OTHER_USER_ID)
STATE = Intents.USERS | Intents.LISTS | Intents.ITEMS | Intents.PLANS
Document = dict[str, typing.Any]


def uuid(number: int) -> str:
    return f"01930000-0000-7000-8000-{number:012x}"


def item(number: int, **changes: object) -> Document:
    return {**fixtures.PLAIN_ITEM, "id": uuid(number), "title": f"item {number}", **changes}


def done(number: int, completed_at: str, **changes: object) -> Document:
    return item(number, status="done", completed_at=completed_at, **changes)


def event(name: str, payload: Document, *, connection: bool = False) -> events.Event:
    built = _events.decode_event(name, msgspec.Raw(fixtures.raw(payload)), connection=connection)
    assert built is not None
    return built


def change(name: str, data: Document, user: str = ME, *, connection: bool = False) -> events.Event:
    return event(name, {"user_id": user, "actor": fixtures.ACTOR, "nonce": None, "data": data}, connection=connection)


def sync(user: str = ME, index: int = 0, count: int = 1, *, connection: bool = False, **parts: object) -> events.Event:
    if index == 0:
        parts = {
            "user": fixtures.CONNECTION_USER if connection else fixtures.USER,
            "lists": [fixtures.LIST],
            "plans": [fixtures.REMINDER_PLAN],
            **parts,
        }
        if connection:
            parts["user"] = {**typing.cast("Document", parts["user"]), "id": user}
    return event(
        "user.sync", {"user_id": user, "chunk": {"index": index, "count": count}, **parts}, connection=connection
    )


def personal(intents: Intents = STATE, **parts: object) -> CacheStore:
    cache = CacheStore()
    cache.begin(msgspec.convert(fixtures.PRINCIPAL, models.Principal), [ME], intents)
    cache.apply(sync(ME, 0, 1, connection=False, **parts))
    return cache


def connected(*users: str) -> CacheStore:
    cache = CacheStore()
    cache.begin(msgspec.convert(fixtures.CONNECTION_PRINCIPAL, models.Principal), [ID(user) for user in users], STATE)
    return cache


def titles(items: tuple[models.Item, ...]) -> list[str]:
    return [entry.title for entry in items]


def test_empty_before_anything_arrives() -> None:
    cache = CacheStore()
    assert cache.get_users() == ()
    assert cache.get_profile() is None
    assert cache.get_items() == ()
    assert cache.get_item(ID(uuid(1))) is None
    assert cache.get_history_cursor() is None
    assert cache.get_plan(Priority.HIGH) is None


def test_sync_fills_the_cache() -> None:
    cache = personal(items=[item(1), item(2)])
    assert cache.get_users() == (ME,)
    profile = cache.get_profile()
    assert isinstance(profile, models.User)
    assert profile.display_name == "Mia"
    assert [entry.name for entry in cache.get_lists()] == ["Inbox"]
    assert cache.get_list(ID(fixtures.LIST_ID)) is cache.get_lists()[0]
    assert titles(cache.get_items()) == ["item 1", "item 2"]
    assert cache.get_item(ID(uuid(2))) is cache.get_items()[1]
    assert cache.get_plans()[0].priority is Priority.HIGH
    assert cache.get_plan(Priority.HIGH) is cache.get_plans()[0]
    assert cache.get_plan(Priority.LOW) is None
    assert cache.get_items(ME) is cache.get_items()


def test_sync_reports_the_user_as_ready_on_its_last_part() -> None:
    cache = CacheStore()
    cache.begin(msgspec.convert(fixtures.PRINCIPAL, models.Principal), [ME], STATE)
    assert cache.apply(sync(count=2, items=[item(1)])).ready is None
    assert cache.apply(sync(index=1, count=2, items=[item(2)])).ready == ME


def test_sync_in_three_chunks_is_invisible_until_complete() -> None:
    cache = CacheStore()
    cache.begin(msgspec.convert(fixtures.PRINCIPAL, models.Principal), [ME], STATE)
    cache.apply(sync(count=3, items=[item(1)]))
    cache.apply(sync(index=1, count=3, items=[item(2)]))
    assert cache.get_users() == ()
    assert cache.get_items() == ()
    assert cache.get_item(ID(uuid(1))) is None
    cache.apply(sync(index=2, count=3, items=[item(3)]))
    assert titles(cache.get_items()) == ["item 1", "item 2", "item 3"]


def test_resync_keeps_the_old_state_readable_until_done() -> None:
    cache = personal(items=[item(1)])
    cache.begin(msgspec.convert(fixtures.PRINCIPAL, models.Principal), [ME], STATE)
    cache.apply(sync(count=2, items=[item(2)]))
    assert titles(cache.get_items()) == ["item 1"]
    assert cache.get_item(ID(uuid(2))) is None
    cache.apply(sync(index=1, count=2, items=[item(3)]))
    assert titles(cache.get_items()) == ["item 2", "item 3"]
    assert cache.get_item(ID(uuid(1))) is None


def test_a_later_part_without_a_first_one_is_ignored() -> None:
    cache = CacheStore()
    cache.begin(msgspec.convert(fixtures.PRINCIPAL, models.Principal), [ME], STATE)
    assert cache.apply(sync(index=1, count=2, items=[item(2)])).ready is None
    assert cache.get_users() == ()


def test_users_missing_from_a_new_ready_are_dropped() -> None:
    cache = connected(ME, OTHER)
    cache.apply(sync(ME, connection=True, items=[item(1)]))
    cache.apply(sync(OTHER, connection=True, items=[item(2)]))
    assert cache.get_users() == (ME, OTHER)
    cache.begin(msgspec.convert(fixtures.CONNECTION_PRINCIPAL, models.Principal), [OTHER], STATE)
    assert cache.get_users() == (OTHER,)
    assert cache.get_item(ID(uuid(1))) is None
    assert cache.get_item(ID(uuid(2))) is not None
    assert cache.get_items(ME) == ()


def test_gone_user_is_dropped() -> None:
    cache = connected(ME, OTHER)
    cache.apply(sync(ME, connection=True, items=[item(1)]))
    cache.apply(sync(OTHER, connection=True, items=[item(2)], lists=[{**fixtures.LIST, "id": uuid(0xA9)}]))
    cache.apply(event("user.gone", {"user_id": ME}))
    assert cache.get_users() == (OTHER,)
    assert cache.get_profile(ME) is None
    assert cache.get_item(ID(uuid(1))) is None
    assert cache.get_list(ID(fixtures.LIST_ID)) is None
    assert cache.get_list(ID(uuid(0xA9))) is not None


def test_gone_during_a_sync_discards_what_arrived() -> None:
    cache = connected(ME)
    cache.apply(sync(ME, count=2, connection=True, items=[item(1)]))
    cache.apply(event("user.gone", {"user_id": ME}))
    assert cache.apply(sync(ME, index=1, count=2, connection=True, items=[item(2)])).ready is None
    assert cache.get_users() == ()


def test_user_coming_into_reach_later() -> None:
    cache = connected(ME)
    cache.apply(sync(ME, connection=True))
    assert cache.apply(sync(OTHER, connection=True, items=[item(5)])).ready == OTHER
    assert cache.get_users() == (ME, OTHER)
    profile = cache.get_profile(OTHER)
    assert isinstance(profile, models.ConnectionUser)
    assert profile.id == OTHER


def test_created_updated_deleted_item() -> None:
    cache = personal(items=[item(1)])
    cache.apply(change("item.created", item(2)))
    assert titles(cache.get_items()) == ["item 1", "item 2"]
    cache.apply(change("item.updated", item(1, title="renamed")))
    assert titles(cache.get_items()) == ["renamed", "item 2"]
    cache.apply(change("item.deleted", {"id": uuid(1)}))
    assert titles(cache.get_items()) == ["item 2"]
    assert cache.get_item(ID(uuid(1))) is None


def test_update_returns_the_object_the_cache_held() -> None:
    cache = personal(items=[item(1)])
    before = cache.get_item(ID(uuid(1)))
    applied = cache.apply(change("item.updated", item(1, title="renamed"))).event
    assert isinstance(applied, events.ItemUpdateEvent)
    assert applied.old_item is before
    assert applied.item is cache.get_item(ID(uuid(1)))
    deleted = cache.apply(change("item.deleted", {"id": uuid(1)})).event
    assert isinstance(deleted, events.ItemDeleteEvent)
    assert deleted.old_item is applied.item


def test_created_item_has_no_old_one() -> None:
    cache = personal()
    applied = cache.apply(change("item.updated", item(9))).event
    assert isinstance(applied, events.ItemUpdateEvent)
    assert applied.old_item is None
    assert cache.get_item(ID(uuid(9))) is applied.item


def test_deleting_what_is_not_there_changes_nothing() -> None:
    cache = personal(items=[item(1)])
    view = cache.get_items()
    applied = cache.apply(change("item.deleted", {"id": uuid(7)})).event
    assert isinstance(applied, events.ItemDeleteEvent)
    assert applied.old_item is None
    assert cache.get_items() is view


def test_lists_follow_their_position() -> None:
    cache = personal()
    work = {**fixtures.LIST, "id": uuid(0xA2), "name": "Work", "position": 1, "is_inbox": False}
    home = {**fixtures.LIST, "id": uuid(0xA3), "name": "Home", "position": 2, "is_inbox": False}
    cache.apply(change("list.created", home))
    cache.apply(change("list.created", work))
    assert [entry.name for entry in cache.get_lists()] == ["Inbox", "Work", "Home"]
    moved = cache.apply(change("list.updated", {**home, "position": 0, "name": "Home!"})).event
    assert isinstance(moved, events.ListUpdateEvent)
    assert moved.old_list is not None
    assert moved.old_list.name == "Home"
    cache.apply(change("list.updated", {**fixtures.LIST, "position": 1}))
    cache.apply(change("list.updated", {**work, "position": 2}))
    assert [entry.name for entry in cache.get_lists()] == ["Home!", "Inbox", "Work"]
    gone = cache.apply(change("list.deleted", {"id": uuid(0xA2)})).event
    assert isinstance(gone, events.ListDeleteEvent)
    assert gone.old_list is not None
    assert cache.get_list(ID(uuid(0xA2))) is None
    assert [entry.name for entry in cache.get_lists()] == ["Home!", "Inbox"]


def test_done_item_moves_to_the_history() -> None:
    cache = personal(STATE | Intents.HISTORY, items=[item(1), item(2)], history=[], history_cursor=None)
    applied = cache.apply(change("item.updated", done(1, "2026-11-13T10:00:00Z"))).event
    assert isinstance(applied, events.ItemUpdateEvent)
    assert applied.old_item is not None
    assert applied.old_item.status is ItemStatus.OPEN
    assert titles(cache.get_items()) == ["item 2"]
    assert titles(cache.get_history()) == ["item 1"]
    found = cache.get_item(ID(uuid(1)))
    assert found is not None
    assert found.status is ItemStatus.DONE


def test_done_item_vanishes_without_the_history_intent() -> None:
    cache = personal(items=[item(1)])
    applied = cache.apply(change("item.updated", done(1, "2026-11-13T10:00:00Z"))).event
    assert isinstance(applied, events.ItemUpdateEvent)
    assert applied.old_item is not None
    assert cache.get_items() == ()
    assert cache.get_history() == ()
    assert cache.get_item(ID(uuid(1))) is None


def test_reopened_item_moves_back() -> None:
    cache = personal(
        STATE | Intents.HISTORY, items=[], history=[done(1, "2026-11-13T10:00:00Z")], history_cursor="older"
    )
    assert titles(cache.get_history()) == ["item 1"]
    assert cache.get_history_cursor() == "older"
    applied = cache.apply(change("item.updated", item(1))).event
    assert isinstance(applied, events.ItemUpdateEvent)
    assert applied.old_item is not None
    assert applied.old_item.status is ItemStatus.DONE
    assert cache.get_history() == ()
    assert titles(cache.get_items()) == ["item 1"]


def test_history_is_newest_first_down_to_the_microsecond() -> None:
    history = [
        done(3, "2026-11-13T10:00:00.000002Z"),
        done(2, "2026-11-13T10:00:00.000001Z"),
        done(1, "2026-11-13T10:00:00Z"),
    ]
    cache = personal(STATE | Intents.HISTORY, items=[item(4), item(5), item(6)], history=history, history_cursor=None)
    assert titles(cache.get_history()) == ["item 3", "item 2", "item 1"]
    cache.apply(change("item.updated", done(4, "2026-11-13T10:00:00Z")))
    cache.apply(change("item.updated", done(5, "2026-11-13T10:00:00.000001Z")))
    cache.apply(change("item.updated", done(6, "2026-11-13T09:00:00Z")))
    assert titles(cache.get_history()) == ["item 3", "item 5", "item 2", "item 4", "item 1", "item 6"]


def test_history_arrives_in_parts_of_its_own() -> None:
    cache = CacheStore()
    cache.begin(msgspec.convert(fixtures.PRINCIPAL, models.Principal), [ME], STATE | Intents.HISTORY)
    cache.apply(sync(count=3, items=[item(1)]))
    cache.apply(sync(index=1, count=3, history=[done(3, "2026-11-13T10:00:00Z")]))
    cache.apply(sync(index=2, count=3, history=[done(2, "2026-11-12T10:00:00Z")], history_cursor="older"))
    assert titles(cache.get_history()) == ["item 3", "item 2"]
    assert cache.get_history_cursor() == "older"


def test_changed_done_item_stays_in_place_and_an_older_one_enters() -> None:
    cache = personal(
        STATE | Intents.HISTORY, items=[], history=[done(2, "2026-11-13T10:00:00Z")], history_cursor="older"
    )
    cache.apply(change("item.updated", done(2, "2026-11-13T10:00:00Z", title="renamed")))
    assert titles(cache.get_history()) == ["renamed"]
    cache.apply(change("item.updated", done(1, "2026-01-01T10:00:00Z")))
    assert titles(cache.get_history()) == ["renamed", "item 1"]
    removed = cache.apply(change("item.deleted", {"id": uuid(1)})).event
    assert isinstance(removed, events.ItemDeleteEvent)
    assert removed.old_item is not None
    assert titles(cache.get_history()) == ["renamed"]


def test_plan_update_replaces_one_plan() -> None:
    cache = personal()
    low = {**fixtures.REMINDER_PLAN, "priority": "low", "before": ["1d"], "is_default": False}
    cache.apply(change("plan.updated", low))
    assert [plan.priority for plan in cache.get_plans()] == [Priority.HIGH, Priority.LOW]
    applied = cache.apply(change("plan.updated", {**low, "before": ["2d"]})).event
    assert isinstance(applied, events.PlanUpdateEvent)
    assert applied.old_plan is not None
    assert applied.old_plan.before[0].days == 1
    plan = cache.get_plan(Priority.LOW)
    assert plan is not None
    assert plan.before[0].days == 2


def test_profile_update() -> None:
    cache = personal()
    before = cache.get_profile()
    applied = cache.apply(change("user.updated", {**fixtures.USER, "display_name": "Mia B."})).event
    assert isinstance(applied, events.UserUpdateEvent)
    assert applied.old_user is before
    profile = cache.get_profile()
    assert profile is not None
    assert profile.display_name == "Mia B."


def test_subitem_and_reminder_events_do_not_touch_the_cache() -> None:
    cache = personal(items=[fixtures.ITEM])
    view = cache.get_items()
    renamed = cache.apply(change("subitem.updated", {**fixtures.SUBITEM, "title": "Print twice"})).event
    assert isinstance(renamed, events.SubitemUpdateEvent)
    assert renamed.old_subitem is view[0].subitems[0]
    removed = cache.apply(change("subitem.deleted", {"id": fixtures.SUBITEM_ID, "item_id": fixtures.ITEM_ID})).event
    assert isinstance(removed, events.SubitemDeleteEvent)
    assert removed.old_subitem is view[0].subitems[0]
    reminder = cache.apply(change("reminder.deleted", {"id": fixtures.REMINDER_ID, "item_id": fixtures.ITEM_ID})).event
    assert isinstance(reminder, events.ReminderDeleteEvent)
    assert reminder.old_reminder is view[0].reminders[0]
    cache.apply(change("subitem.created", fixtures.SUBITEM))
    cache.apply(change("reminder.created", fixtures.MANUAL_REMINDER))
    assert cache.get_items() is view


def test_views_stay_the_same_object_until_their_kind_changes() -> None:
    cache = personal(STATE | Intents.HISTORY, items=[item(1)], history=[], history_cursor=None)
    items, lists, plans, history = cache.get_items(), cache.get_lists(), cache.get_plans(), cache.get_history()
    assert cache.get_items() is items
    cache.apply(change("list.created", {**fixtures.LIST, "id": uuid(0xA2), "position": 1, "is_inbox": False}))
    assert cache.get_items() is items
    assert cache.get_plans() is plans
    assert cache.get_lists() is not lists
    cache.apply(change("item.created", item(2)))
    assert cache.get_items() is not items
    assert cache.get_history() is history


def test_events_for_users_the_cache_does_not_hold_change_nothing() -> None:
    cache = personal(items=[item(1)])
    applied = cache.apply(change("item.created", item(2), user=OTHER)).event
    assert isinstance(applied, events.ItemCreateEvent)
    assert cache.get_item(ID(uuid(2))) is None
    assert cache.get_users() == (ME,)


def test_delivery_events_pass_through() -> None:
    cache = personal()
    delivery = change("delivery.created", fixtures.DELIVERY)
    assert cache.apply(delivery).event is delivery


def test_connection_must_name_the_user() -> None:
    cache = connected(ME)
    cache.apply(sync(ME, connection=True, items=[item(1)]))
    with pytest.raises(TypeError, match="which user"):
        cache.get_items()
    with pytest.raises(TypeError, match="which user"):
        cache.get_profile()
    assert titles(cache.get_items(ME)) == ["item 1"]
    assert cache.get_item(ID(uuid(1))) is not None


def test_unknown_user_is_empty() -> None:
    cache = personal(items=[item(1)])
    assert cache.get_items(OTHER) == ()
    assert cache.get_lists(OTHER) == ()
    assert cache.get_plans(OTHER) == ()
    assert cache.get_history(OTHER) == ()
    assert cache.get_profile(OTHER) is None
    assert cache.get_history_cursor(OTHER) is None
    assert cache.get_plan(Priority.HIGH, OTHER) is None


def test_clear_forgets_everything() -> None:
    cache = personal(items=[item(1)])
    cache.clear()
    assert cache.get_users() == ()
    assert cache.get_item(ID(uuid(1))) is None
    assert cache.get_items() == ()


def test_cached_objects_cannot_be_changed() -> None:
    cache = personal(items=[fixtures.ITEM])
    held = cache.get_items()[0]
    with pytest.raises(AttributeError):
        held.title = "x"  # pyright: ignore[reportAttributeAccessIssue]
    with pytest.raises(AttributeError):
        held.subitems[0].done = True  # pyright: ignore[reportAttributeAccessIssue]
    with pytest.raises(TypeError):
        cache.get_items()[0] = held  # pyright: ignore[reportIndexIssue]
    profile = cache.get_profile()
    assert isinstance(profile, models.User)
    with pytest.raises(TypeError):
        typing.cast("dict[str, object]", profile.preferences)["theme"] = "light"


def test_items_share_the_id_of_their_list() -> None:
    cache = personal(STATE | Intents.HISTORY, items=[item(1)], history=[done(2, "2026-11-13T10:00:00Z")])
    cache.apply(change("item.created", item(3)))
    shared = cache.get_lists()[0].id
    assert all(entry.list_id is shared for entry in (*cache.get_items(), *cache.get_history()))


def test_items_completed_at_the_same_moment_are_ordered_by_id() -> None:
    moment = "2026-11-13T10:00:00Z"
    cache = personal(STATE | Intents.HISTORY, items=[item(3)], history=[done(1, moment), done(5, moment)])
    cache.apply(change("item.updated", done(3, moment)))
    assert titles(cache.get_history()) == ["item 5", "item 3", "item 1"]
    cache.apply(change("item.deleted", {"id": uuid(3)}))
    assert titles(cache.get_history()) == ["item 5", "item 1"]


def test_a_completion_time_without_an_offset_does_not_break_the_history() -> None:
    cache = personal(
        STATE | Intents.HISTORY,
        items=[],
        history=[done(1, "2026-11-13T10:00:00"), done(2, "2026-11-13T09:00:00Z")],
    )
    assert titles(cache.get_history()) == ["item 1", "item 2"]


def test_a_sync_that_names_an_item_twice_does_not_break_the_next_one() -> None:
    cache = personal(STATE | Intents.HISTORY, items=[item(1)], history=[done(1, "2026-11-13T10:00:00Z")])
    cache.begin(msgspec.convert(fixtures.PRINCIPAL, models.Principal), [ME], STATE | Intents.HISTORY)
    cache.apply(sync(items=[item(2)], history=[]))
    assert titles(cache.get_items()) == ["item 2"]
    assert cache.get_item(ID(uuid(1))) is None
