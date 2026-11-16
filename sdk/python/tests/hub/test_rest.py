from __future__ import annotations

import datetime
import zoneinfo

import pytest

import plsremind
from tests.hub import helpers

pytestmark = pytest.mark.hub
UTC = datetime.UTC


async def test_the_hub_is_up_and_tells_where_its_gateway_is(kit: helpers.Kit) -> None:
    mia = await kit.account("mia")
    info = await mia.rest.get_info()
    assert info.gateway_url.startswith("ws://127.0.0.1:")
    assert info.setup_required is False
    assert info.registration is plsremind.Registration.OPEN or info.registration is plsremind.Registration.INVITE
    assert info.login_methods[0].type is plsremind.ProviderType.PASSWORD


async def test_a_token_that_does_not_exist_is_unauthorized(kit: helpers.Kit) -> None:
    with pytest.raises(plsremind.UnauthorizedError):
        await kit.rest("plsr_pat_no-such-token").get_info()
    with pytest.raises(plsremind.UnauthorizedError) as caught:
        await kit.rest("plsr_pat_no-such-token").list_lists()
    assert caught.value.status == 401
    assert caught.value.code


async def test_validation_error_names_the_field(kit: helpers.Kit) -> None:
    mia = await kit.account("mia")
    with pytest.raises(plsremind.BadRequestError) as caught:
        await mia.rest.create_item(title="")
    assert "title" in caught.value.fields
    with pytest.raises(plsremind.NotFoundError):
        await mia.rest.get_item(plsremind.ID("01930000-0000-7000-8000-000000000000"))


async def test_every_call_round_trips(kit: helpers.Kit) -> None:  # ruff: ignore[too-many-statements]
    mia = await kit.account("mia")
    rest = mia.rest

    user = await rest.get_user()
    assert isinstance(user, plsremind.User)
    assert user.id == mia.id
    assert user.timezone == zoneinfo.ZoneInfo("Europe/Berlin")
    assert user.quiet_hours is None
    hours = plsremind.QuietHours(start=plsremind.ClockTime(22, 0), end=plsremind.ClockTime(7, 30))
    changed = await rest.update_user(
        display_name="Mia B.",
        timezone=zoneinfo.ZoneInfo("Europe/Paris"),
        locale="de",
        preferences={"theme": "dark", "keys": {"done": "x"}},
        quiet_hours=hours,
    )
    assert changed.display_name == "Mia B."
    assert changed.timezone == zoneinfo.ZoneInfo("Europe/Paris")
    assert changed.locale == "de"
    assert changed.quiet_hours == hours
    assert dict(changed.preferences) == {"theme": "dark", "keys": {"done": "x"}}
    assert (await rest.update_user(quiet_hours=None, locale=None)).quiet_hours is None
    assert [identity.provider for identity in await rest.list_identities()] == ["password"]

    inbox = (await rest.list_lists())[0]
    assert inbox.is_inbox
    uni = await rest.create_list(name="Uni")
    home = await rest.create_list(name="Home")
    assert [entry.name for entry in await rest.reorder_lists([home.id, inbox.id, uni.id])] == ["Home", "Inbox", "Uni"]
    assert (await rest.rename_list(uni.id, name="University")).name == "University"
    await rest.delete_list(home.id)
    assert [entry.id for entry in await rest.list_lists()] == [inbox.id, uni.id]

    plans = await rest.list_reminder_plans()
    assert {plan.priority for plan in plans} == {
        plsremind.Priority.LOW,
        plsremind.Priority.NORMAL,
        plsremind.Priority.HIGH,
    }
    assert all(plan.is_default for plan in plans)
    plan = await rest.set_reminder_plan(
        plsremind.Priority.HIGH,
        before=[datetime.timedelta(days=1, hours=12), datetime.timedelta(hours=2)],
        follow_up=plsremind.FollowUp.DAILY,
    )
    assert plan.before == (datetime.timedelta(days=1, hours=12), datetime.timedelta(hours=2))
    assert plan.follow_up is plsremind.FollowUp.DAILY
    assert not plan.is_default
    await rest.reset_reminder_plan(plsremind.Priority.HIGH)
    assert all(plan.is_default for plan in await rest.list_reminder_plans())

    due = datetime.datetime(2030, 1, 1, 10, 0, 0, 123456, tzinfo=UTC)
    override = plsremind.ReminderOverride(
        mode=plsremind.OverrideMode.CUSTOM, before=(plsremind.Duration(days=1), plsremind.Duration(hours=1))
    )
    report = await rest.create_item(
        title="Hand in the report",
        note="Two copies",
        list_id=uni.id,
        priority=plsremind.Priority.HIGH,
        due_at=due,
        subitems=["print", "sign"],
        reminder_override=override,
        nonce="n-1",
    )
    assert report.due_at == due
    assert report.all_day is False
    assert report.list_id == uni.id
    assert report.reminder_override == override
    assert [sub.title for sub in report.subitems] == ["print", "sign"]
    assert await rest.get_item(report.id) == report

    milk = await rest.create_item(title="Buy milk", due_date=datetime.date(2030, 2, 1))
    assert milk.all_day is True
    assert milk.due_date == datetime.date(2030, 2, 1)
    assert milk.due_at is not None
    assert milk.priority is plsremind.Priority.NORMAL
    cleared = await rest.update_item(milk.id, due_date=None, title="Buy oat milk")
    assert (cleared.due_date, cleared.due_at, cleared.title) == (None, None, "Buy oat milk")
    without = await rest.update_item(report.id, reminder_override=None)
    assert without.reminder_override is None
    off = await rest.update_item(
        report.id, reminder_override=plsremind.ReminderOverride(mode=plsremind.OverrideMode.OFF)
    )
    assert off.reminder_override is not None
    assert off.reminder_override.mode is plsremind.OverrideMode.OFF
    await rest.update_item(report.id, reminder_override=override)

    staple = await rest.create_subitem(report.id, title="staple")
    order = [staple.id, report.subitems[1].id, report.subitems[0].id]
    assert [sub.id for sub in await rest.reorder_subitems(report.id, order)] == order
    ticked = await rest.update_subitem(report.id, staple.id, done=True, title="staple twice")
    assert (ticked.done, ticked.title) == (True, "staple twice")
    await rest.delete_subitem(report.id, staple.id)
    assert len((await rest.get_item(report.id)).subitems) == 2

    at = datetime.datetime(2029, 12, 30, 8, 0, tzinfo=UTC)
    reminder = await rest.create_reminder(report.id, at=at)
    assert reminder.remind_at == at
    later = await rest.create_reminder(report.id, after=datetime.timedelta(hours=2))
    assert (
        datetime.timedelta(minutes=119) < later.remind_at - datetime.datetime.now(UTC) < datetime.timedelta(minutes=121)
    )
    assert {entry.id for entry in await rest.list_reminders(report.id)} == {reminder.id, later.id}
    await rest.delete_reminder(report.id, later.id)

    schedule = await rest.get_item_schedule(report.id)
    kinds = {entry.kind for entry in schedule}
    assert {plsremind.ReminderKind.BEFORE, plsremind.ReminderKind.DUE, plsremind.ReminderKind.MANUAL} <= kinds
    offsets = {entry.offset for entry in schedule if entry.kind is plsremind.ReminderKind.BEFORE}
    assert offsets == {datetime.timedelta(days=1), datetime.timedelta(hours=1)}
    manual = next(entry for entry in schedule if entry.kind is plsremind.ReminderKind.MANUAL)
    assert manual.reminder_id == reminder.id
    assert manual.scheduled_for == at

    done = await rest.update_item(milk.id, status=plsremind.ItemStatus.DONE)
    assert done.status is plsremind.ItemStatus.DONE
    assert done.completed_at is not None
    listed = await rest.list_items()
    assert [entry.id for entry in listed.data] == [report.id]
    assert listed.next_cursor is None
    everything = await rest.list_items(
        status="all", sort=plsremind.ItemSort.CREATED, order=plsremind.SortOrder.ASCENDING
    )
    assert [entry.id for entry in everything.data] == [report.id, milk.id]
    filtered = await rest.list_items(
        list_id=uni.id,
        has_due=True,
        due_before=due + datetime.timedelta(days=1),
        due_after=due - datetime.timedelta(days=1),
        limit=5,
    )
    assert [entry.id for entry in filtered.data] == [report.id]
    completed = await rest.list_items(
        status=plsremind.ItemStatus.DONE, sort=plsremind.ItemSort.COMPLETED, order=plsremind.SortOrder.DESCENDING
    )
    assert [entry.id for entry in completed.data] == [milk.id]

    assert (await rest.list_reminder_log(item_id=report.id, limit=5)).data == ()
    with pytest.raises(plsremind.ForbiddenError):
        await rest.list_deliveries()
    _, reminding = await kit.personal_token(mia.browser, remind=True)
    assert await kit.rest(reminding).list_deliveries() == ()

    provider = helpers.unique("chat")
    bot_id, bot_token = await kit.connection(provider)
    linked = await kit.linked(provider, "tom")
    connections = await linked.rest.list_user_connections()
    ours = next(entry for entry in connections if entry.id == bot_id)
    assert (ours.linked, ours.enabled, ours.remind, ours.provider) == (True, True, True, provider)
    assert plsremind.Intents.ITEMS in ours.intents
    grant = await linked.rest.set_connection_grant(bot_id, remind=False)
    assert (grant.enabled, grant.remind) == (True, False)

    bot = kit.rest(bot_token)
    found = await bot.lookup_user(linked.external_id)
    assert found.id == linked.id
    assert found.display_name == "tom"
    profile = await bot.get_user(user=linked.id)
    assert type(profile) is plsremind.Profile
    made = await bot.create_item(title="from the bot", user=linked.id)
    assert (await linked.rest.get_item(made.id)).title == "from the bot"
    with pytest.raises(TypeError, match="which user"):
        await bot.list_items()
    with pytest.raises(plsremind.NotFoundError):
        await bot.lookup_user("nobody")

    await rest.delete_item(report.id)
    with pytest.raises(plsremind.NotFoundError):
        await rest.get_item(report.id)


async def test_paginate_over_real_pages(kit: helpers.Kit) -> None:
    mia = await kit.account("mia")
    for number in range(7):
        await mia.rest.create_item(title=f"item {number}")
    first = await mia.rest.list_items(limit=3, sort=plsremind.ItemSort.CREATED, order=plsremind.SortOrder.ASCENDING)
    assert len(first.data) == 3
    assert first.next_cursor is not None
    titles = [
        entry.title
        async for entry in plsremind.paginate(
            mia.rest.list_items, limit=3, sort=plsremind.ItemSort.CREATED, order=plsremind.SortOrder.ASCENDING
        )
    ]
    assert titles == [f"item {number}" for number in range(7)]
