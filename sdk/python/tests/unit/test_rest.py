from __future__ import annotations

import asyncio
import collections.abc
import dataclasses
import datetime
import inspect
import socket
import zoneinfo

import aiohttp
import pytest

from plsremind import errors
from plsremind import models
from plsremind import rest as rest_module
from plsremind.enums import DeliveryStatus
from plsremind.enums import FollowUp
from plsremind.enums import ItemSort
from plsremind.enums import ItemStatus
from plsremind.enums import OverrideMode
from plsremind.enums import Priority
from plsremind.enums import SortOrder
from plsremind.rest import RESTClient
from plsremind.rest import paginate
from plsremind.types import ID
from plsremind.types import ClockTime
from tests.support import fakes
from tests.support import fixtures

TOKEN = "plsr_pat_secret"
CONNECTION_TOKEN = "plsr_con_secret"


def client(hub: fakes.FakeHub, token: str = TOKEN, **options: object) -> RESTClient:
    return RESTClient(hub.url, token=token, **options)  # pyright: ignore[reportArgumentType]


async def test_sends_token_and_path(hub: fakes.FakeHub) -> None:
    hub.answer("GET", "/users/@me/lists", fakes.Reply(json=[fixtures.LIST]))
    async with client(hub) as rest:
        lists = await rest.list_lists()
    assert lists[0].name == "Inbox"
    request = hub.requests[0]
    assert (request.method, request.path) == ("GET", "/users/@me/lists")
    assert request.headers["Authorization"] == f"Bearer {TOKEN}"
    assert request.headers["Accept"] == "application/json"
    assert "X-Request-Nonce" not in request.headers


async def test_trailing_slash_in_the_address(hub: fakes.FakeHub) -> None:
    hub.answer("GET", "/info", fakes.Reply(json=fixtures.INFO))
    async with RESTClient(hub.url + "/", token=TOKEN) as rest:
        assert (await rest.get_info()).name == "plsremind"


async def test_user_goes_into_the_path(hub: fakes.FakeHub) -> None:
    hub.answer("GET", "/users/u/1/lists", fakes.Reply(json=[]))
    async with client(hub, CONNECTION_TOKEN) as rest:
        assert await rest.list_lists(user=ID("u/1")) == ()
    assert hub.requests[0].raw_path == "/api/v1/users/u%2F1/lists"


async def test_connection_must_name_the_user(hub: fakes.FakeHub) -> None:
    async with client(hub, CONNECTION_TOKEN) as rest:
        assert rest.is_connection
        with pytest.raises(TypeError, match="which user"):
            await rest.list_lists()
    assert hub.requests == []


async def test_personal_token_is_no_connection(hub: fakes.FakeHub) -> None:
    async with client(hub) as rest:
        assert not rest.is_connection


async def test_nonce_and_body(hub: fakes.FakeHub) -> None:
    hub.answer("POST", "/users/@me/lists", fakes.Reply(201, fixtures.LIST))
    async with client(hub) as rest:
        await rest.create_list(name="Work", nonce="n-1")
    request = hub.requests[0]
    assert request.headers["X-Request-Nonce"] == "n-1"
    assert request.headers["Content-Type"] == "application/json"
    assert request.json == {"name": "Work"}


async def test_no_content(hub: fakes.FakeHub) -> None:
    hub.answer("DELETE", "/users/@me/lists/l1", fakes.Reply(204))
    async with client(hub) as rest:
        await rest.delete_list(ID("l1"))
    assert hub.requests[0].body == b""


async def test_error_answer_becomes_an_api_error(hub: fakes.FakeHub) -> None:
    hub.answer("PUT", "/deliveries/d1", fakes.error(409, "delivery_closed", "The delivery is closed."))
    async with client(hub) as rest:
        with pytest.raises(errors.ConflictError) as caught:
            await rest.acknowledge_delivery(ID("d1"), status=DeliveryStatus.DELIVERED)
    assert caught.value.code == "delivery_closed"
    assert caught.value.status == 409
    assert caught.value.message == "The delivery is closed."
    assert caught.value.fields == {}


async def test_validation_fields(hub: fakes.FakeHub) -> None:
    hub.answer("POST", "/users/@me/items", fakes.error(422, "invalid", "No.", {"title": "must not be empty"}))
    async with client(hub) as rest:
        with pytest.raises(errors.BadRequestError) as caught:
            await rest.create_item(title="")
    assert caught.value.fields == {"title": "must not be empty"}


async def test_error_without_a_readable_body(hub: fakes.FakeHub) -> None:
    hub.answer("GET", "/info", fakes.Reply(502, body=b"<html>Bad Gateway</html>"))
    async with client(hub) as rest:
        with pytest.raises(errors.ServerError) as caught:
            await rest.get_info()
    assert caught.value.status == 502
    assert caught.value.code == "unknown"
    assert hub.requests[0].method == "GET"
    assert len(hub.requests) == 1


async def test_rate_limit_is_retried(hub: fakes.FakeHub) -> None:
    limited = fakes.Reply(
        429, {"error": {"code": "rate_limited", "message": "Slow down."}}, headers={"Retry-After": "0"}
    )
    hub.answer("GET", "/info", limited, fakes.Reply(json=fixtures.INFO))
    async with client(hub) as rest:
        assert (await rest.get_info()).version == "0.3.0"
    assert len(hub.requests) == 2


async def test_rate_limit_gives_up_after_three_retries(hub: fakes.FakeHub) -> None:
    limited = fakes.Reply(
        429, {"error": {"code": "rate_limited", "message": "Slow down."}}, headers={"Retry-After": "0"}
    )
    hub.answer("GET", "/info", limited)
    async with client(hub) as rest:
        with pytest.raises(errors.RateLimitedError) as caught:
            await rest.get_info()
    assert caught.value.retry_after == 0
    assert len(hub.requests) == 4


@pytest.mark.parametrize("header", [{"Retry-After": "30"}, {"Retry-After": "soon"}, {"Retry-After": "-1"}, {}])
async def test_a_wait_that_is_long_or_unclear_is_not_sat_out(hub: fakes.FakeHub, header: dict[str, str]) -> None:
    hub.answer("GET", "/info", fakes.limited(header.get("Retry-After")))
    async with client(hub) as rest:
        with pytest.raises(errors.RateLimitedError) as caught:
            await rest.get_info()
    assert caught.value.retry_after == (30 if header.get("Retry-After") == "30" else None)
    assert len(hub.requests) == 1


async def test_retry_can_be_switched_off(hub: fakes.FakeHub) -> None:
    hub.answer(
        "GET",
        "/info",
        fakes.Reply(429, {"error": {"code": "rate_limited", "message": "x"}}, headers={"Retry-After": "0"}),
    )
    async with client(hub, retry=False) as rest:
        with pytest.raises(errors.RateLimitedError):
            await rest.get_info()
    assert len(hub.requests) == 1


async def test_other_failures_are_not_retried(hub: fakes.FakeHub) -> None:
    hub.answer("GET", "/info", fakes.error(500, "internal", "Oops."))
    async with client(hub) as rest:
        with pytest.raises(errors.ServerError):
            await rest.get_info()
    assert len(hub.requests) == 1


@pytest.mark.parametrize("body", [b"<html>", b'{"name": 1}', b""])
async def test_unreadable_answer_is_a_network_error(hub: fakes.FakeHub, body: bytes) -> None:
    hub.answer("GET", "/info", fakes.Reply(200, body=body))
    async with client(hub) as rest:
        with pytest.raises(errors.NetworkError, match="could not be read"):
            await rest.get_info()


async def test_unreachable_hub_is_a_network_error() -> None:
    with socket.socket() as probe:
        probe.bind(("127.0.0.1", 0))
        port = probe.getsockname()[1]
    async with RESTClient(f"http://127.0.0.1:{port}", token=TOKEN) as rest:
        with pytest.raises(errors.NetworkError, match="did not answer GET /info") as caught:
            await rest.get_info()
    assert isinstance(caught.value.__cause__, aiohttp.ClientError)


async def test_timeout_is_a_network_error(hub: fakes.FakeHub) -> None:
    hub.answer("GET", "/info", fakes.Reply(json=fixtures.INFO, delay=0.5))
    async with client(hub, timeout=0.05) as rest:
        with pytest.raises(errors.NetworkError, match="did not answer"):
            await rest.get_info()


async def test_close_leaves_a_foreign_session_open(hub: fakes.FakeHub) -> None:
    hub.answer("GET", "/info", fakes.Reply(json=fixtures.INFO))
    async with aiohttp.ClientSession() as session:
        rest = RESTClient(hub.url, token=TOKEN, session=session)
        await rest.get_info()
        await rest.close()
        assert not session.closed


async def test_close_twice_and_before_use(hub: fakes.FakeHub) -> None:
    rest = client(hub)
    await rest.close()
    await rest.close()


U = ID(fixtures.USER_ID)
ITEM = ID(fixtures.ITEM_ID)
SUB = ID(fixtures.SUBITEM_ID)
REM = ID(fixtures.REMINDER_ID)
LST = ID(fixtures.LIST_ID)
CON = ID(fixtures.CONNECTION_ID)
DEL = ID(fixtures.DELIVERY_ID)
WHEN = datetime.datetime(2026, 11, 13, 18, 0, tzinfo=datetime.UTC)
PAGE = {"data": [fixtures.ITEM], "next_cursor": None}
LOG_PAGE = {"data": [fixtures.REMINDER_LOG_ENTRY], "next_cursor": "next"}

type Call = collections.abc.Callable[[RESTClient], collections.abc.Awaitable[object]]


@dataclasses.dataclass(frozen=True)
class Case:
    name: str
    call: Call
    method: str
    path: str
    reply: object = None
    status: int = 200
    result: type[object] | None = None
    body: object = None
    query: dict[str, str] = dataclasses.field(default_factory=dict[str, str])
    token: str = TOKEN


ME = "/users/@me"
CASES = [
    Case("get_info", lambda r: r.get_info(), "GET", "/info", fixtures.INFO, result=models.Info),
    Case("get_user", lambda r: r.get_user(), "GET", ME, fixtures.USER, result=models.User),
    Case(
        "update_user",
        lambda r: r.update_user(
            display_name="Mia",
            timezone=zoneinfo.ZoneInfo("Europe/Berlin"),
            locale=None,
            preferences={"theme": "dark"},
            quiet_hours=models.QuietHours(start=ClockTime(22, 0), end=ClockTime(7, 0)),
        ),
        "PATCH",
        ME,
        fixtures.USER,
        result=models.User,
        body={
            "display_name": "Mia",
            "timezone": "Europe/Berlin",
            "locale": None,
            "preferences": {"theme": "dark"},
            "quiet_hours": {"start": "22:00", "end": "07:00"},
        },
    ),
    Case(
        "update_user_zone_name",
        lambda r: r.update_user(timezone="Europe/Paris", quiet_hours=None),
        "PATCH",
        ME,
        fixtures.USER,
        body={"timezone": "Europe/Paris", "quiet_hours": None},
    ),
    Case(
        "list_identities", lambda r: r.list_identities(), "GET", f"{ME}/identities", [fixtures.IDENTITY], result=tuple
    ),
    Case(
        "get_identity_avatar",
        lambda r: r.get_identity_avatar(ID("i1")),
        "GET",
        f"{ME}/identities/i1/avatar",
        "png",
        result=bytes,
    ),
    Case("list_lists", lambda r: r.list_lists(), "GET", f"{ME}/lists", [fixtures.LIST], result=tuple),
    Case(
        "create_list",
        lambda r: r.create_list(name="Work"),
        "POST",
        f"{ME}/lists",
        fixtures.LIST,
        201,
        models.List,
        {"name": "Work"},
    ),
    Case(
        "reorder_lists",
        lambda r: r.reorder_lists([LST]),
        "PUT",
        f"{ME}/lists/order",
        [fixtures.LIST],
        result=tuple,
        body={"ids": [LST]},
    ),
    Case(
        "rename_list",
        lambda r: r.rename_list(LST, name="Home"),
        "PATCH",
        f"{ME}/lists/{LST}",
        fixtures.LIST,
        result=models.List,
        body={"name": "Home"},
    ),
    Case("delete_list", lambda r: r.delete_list(LST), "DELETE", f"{ME}/lists/{LST}", status=204),
    Case(
        "list_reminder_plans",
        lambda r: r.list_reminder_plans(),
        "GET",
        f"{ME}/reminder-plans",
        [fixtures.REMINDER_PLAN],
        result=tuple,
    ),
    Case(
        "set_reminder_plan",
        lambda r: r.set_reminder_plan(
            Priority.HIGH, before=[datetime.timedelta(days=1), datetime.timedelta(hours=3)], follow_up=FollowUp.DAILY
        ),
        "PUT",
        f"{ME}/reminder-plans/high",
        fixtures.REMINDER_PLAN,
        result=models.ReminderPlan,
        body={"before": ["1d", "3h"], "follow_up": "daily"},
    ),
    Case(
        "reset_reminder_plan",
        lambda r: r.reset_reminder_plan(Priority.LOW),
        "DELETE",
        f"{ME}/reminder-plans/low",
        status=204,
    ),
    Case("list_items", lambda r: r.list_items(), "GET", f"{ME}/items", PAGE, result=models.Page),
    Case(
        "list_items_filtered",
        lambda r: r.list_items(
            list_id=LST,
            status="all",
            has_due=True,
            due_before=WHEN,
            due_after=WHEN,
            sort=ItemSort.COMPLETED,
            order=SortOrder.DESCENDING,
            limit=20,
            cursor="abc",
        ),
        "GET",
        f"{ME}/items",
        PAGE,
        query={
            "list_id": LST,
            "status": "all",
            "has_due": "true",
            "due_before": "2026-11-13T18:00:00Z",
            "due_after": "2026-11-13T18:00:00Z",
            "sort": "completed",
            "order": "desc",
            "limit": "20",
            "cursor": "abc",
        },
    ),
    Case(
        "list_items_done",
        lambda r: r.list_items(status=ItemStatus.DONE, has_due=False),
        "GET",
        f"{ME}/items",
        PAGE,
        query={"status": "done", "has_due": "false"},
    ),
    Case(
        "create_item",
        lambda r: r.create_item(
            title="Report",
            note="Two copies",
            list_id=LST,
            priority=Priority.HIGH,
            due_at=WHEN,
            subitems=["Print it"],
            reminder_override=models.ReminderOverride(mode=OverrideMode.OFF),
        ),
        "POST",
        f"{ME}/items",
        fixtures.ITEM,
        201,
        models.Item,
        {
            "title": "Report",
            "note": "Two copies",
            "list_id": LST,
            "priority": "high",
            "due_at": "2026-11-13T18:00:00Z",
            "subitems": ["Print it"],
            "reminder_override": {"mode": "off"},
        },
    ),
    Case(
        "create_item_all_day",
        lambda r: r.create_item(title="Report", due_date=datetime.date(2026, 11, 13)),
        "POST",
        f"{ME}/items",
        fixtures.ITEM,
        201,
        body={"title": "Report", "due_date": "2026-11-13"},
    ),
    Case("get_item", lambda r: r.get_item(ITEM), "GET", f"{ME}/items/{ITEM}", fixtures.ITEM, result=models.Item),
    Case(
        "update_item",
        lambda r: r.update_item(ITEM, status=ItemStatus.DONE),
        "PATCH",
        f"{ME}/items/{ITEM}",
        fixtures.ITEM,
        result=models.Item,
        body={"status": "done"},
    ),
    Case("delete_item", lambda r: r.delete_item(ITEM), "DELETE", f"{ME}/items/{ITEM}", status=204),
    Case(
        "create_subitem",
        lambda r: r.create_subitem(ITEM, title="Print it"),
        "POST",
        f"{ME}/items/{ITEM}/subitems",
        fixtures.SUBITEM,
        201,
        models.Subitem,
        {"title": "Print it"},
    ),
    Case(
        "reorder_subitems",
        lambda r: r.reorder_subitems(ITEM, [SUB]),
        "PUT",
        f"{ME}/items/{ITEM}/subitems/order",
        [fixtures.SUBITEM],
        result=tuple,
        body={"ids": [SUB]},
    ),
    Case(
        "update_subitem",
        lambda r: r.update_subitem(ITEM, SUB, done=True),
        "PATCH",
        f"{ME}/items/{ITEM}/subitems/{SUB}",
        fixtures.SUBITEM,
        result=models.Subitem,
        body={"done": True},
    ),
    Case(
        "delete_subitem",
        lambda r: r.delete_subitem(ITEM, SUB),
        "DELETE",
        f"{ME}/items/{ITEM}/subitems/{SUB}",
        status=204,
    ),
    Case(
        "get_item_schedule",
        lambda r: r.get_item_schedule(ITEM),
        "GET",
        f"{ME}/items/{ITEM}/schedule",
        [fixtures.SCHEDULED_REMINDER],
        result=tuple,
    ),
    Case(
        "list_reminders",
        lambda r: r.list_reminders(ITEM),
        "GET",
        f"{ME}/items/{ITEM}/reminders",
        [fixtures.MANUAL_REMINDER],
        result=tuple,
    ),
    Case(
        "create_reminder_at",
        lambda r: r.create_reminder(ITEM, at=WHEN),
        "POST",
        f"{ME}/items/{ITEM}/reminders",
        fixtures.MANUAL_REMINDER,
        201,
        models.ManualReminder,
        {"at": "2026-11-13T18:00:00Z"},
    ),
    Case(
        "create_reminder_after",
        lambda r: r.create_reminder(ITEM, after=datetime.timedelta(hours=2)),
        "POST",
        f"{ME}/items/{ITEM}/reminders",
        fixtures.MANUAL_REMINDER,
        201,
        body={"in": "2h"},
    ),
    Case(
        "delete_reminder",
        lambda r: r.delete_reminder(ITEM, REM),
        "DELETE",
        f"{ME}/items/{ITEM}/reminders/{REM}",
        status=204,
    ),
    Case(
        "lookup_user",
        lambda r: r.lookup_user("1234"),
        "GET",
        "/connection/users/lookup",
        fixtures.LOOKUP_RESULT,
        result=models.LookupResult,
        query={"external_id": "1234"},
        token=CONNECTION_TOKEN,
    ),
    Case(
        "list_user_connections",
        lambda r: r.list_user_connections(),
        "GET",
        f"{ME}/connections",
        [fixtures.USER_CONNECTION],
        result=tuple,
    ),
    Case(
        "set_connection_grant",
        lambda r: r.set_connection_grant(CON, enabled=True, remind=False),
        "PUT",
        f"{ME}/connections/{CON}",
        fixtures.USER_CONNECTION,
        result=models.UserConnection,
        body={"enabled": True, "remind": False},
    ),
    Case("list_deliveries", lambda r: r.list_deliveries(), "GET", "/deliveries", [fixtures.DELIVERY], result=tuple),
    Case(
        "acknowledge_delivery_delivered",
        lambda r: r.acknowledge_delivery(DEL, status=DeliveryStatus.DELIVERED),
        "PUT",
        f"/deliveries/{DEL}",
        fixtures.DELIVERY_RESULT,
        result=models.DeliveryResult,
        body={"status": "delivered"},
    ),
    Case(
        "report_test",
        lambda r: r.report_test(
            ID(fixtures.TEST_ID), status=DeliveryStatus.FAILED, reason="direct messages are closed"
        ),
        "PUT",
        f"/deliveries/tests/{fixtures.TEST_ID}",
        fixtures.CONNECTION_TEST,
        result=models.ConnectionTest,
        body={"status": "failed", "reason": "direct messages are closed"},
    ),
    Case(
        "acknowledge_delivery_failed",
        lambda r: r.acknowledge_delivery(DEL, status=DeliveryStatus.FAILED, reason="direct messages are closed"),
        "PUT",
        f"/deliveries/{DEL}",
        fixtures.DELIVERY_RESULT,
        body={"status": "failed", "reason": "direct messages are closed"},
    ),
    Case(
        "list_reminder_log",
        lambda r: r.list_reminder_log(item_id=ITEM, limit=5),
        "GET",
        f"{ME}/reminder-log",
        LOG_PAGE,
        result=models.Page,
        query={"item_id": ITEM, "limit": "5"},
    ),
    Case(
        "for_a_user",
        lambda r: r.get_item(ITEM, user=U),
        "GET",
        f"/users/{U}/items/{ITEM}",
        fixtures.ITEM,
        token=CONNECTION_TOKEN,
    ),
    Case(
        "get_user_as_connection",
        lambda r: r.get_user(user=U),
        "GET",
        f"/users/{U}",
        fixtures.PROFILE,
        result=models.Profile,
        token=CONNECTION_TOKEN,
    ),
]


@pytest.mark.parametrize("case", CASES, ids=lambda case: case.name)
async def test_call(hub: fakes.FakeHub, case: Case) -> None:
    hub.answer(case.method, case.path, fakes.Reply(case.status, case.reply))
    async with client(hub, case.token) as rest:
        result = await case.call(rest)
    request = hub.requests[0]
    assert (request.method, request.path) == (case.method, case.path)
    assert request.query == case.query
    assert (request.json if request.body else None) == case.body
    if case.result is None:
        assert case.status != 204 or result is None
    else:
        assert isinstance(result, case.result)


def test_every_method_has_a_case() -> None:
    methods = {
        name
        for name, member in vars(RESTClient).items()
        if not name.startswith("_") and inspect.iscoroutinefunction(member) and name != "close"
    }
    assert {name for name in methods if not any(case.name.startswith(name) for case in CASES)} == set()


async def test_get_user_shape_follows_the_token(hub: fakes.FakeHub) -> None:
    hub.answer("GET", f"/users/{U}", fakes.Reply(json=fixtures.PROFILE))
    async with client(hub, CONNECTION_TOKEN) as rest:
        assert type(await rest.get_user(user=U)) is models.Profile
    hub.answer("GET", "/users/@me", fakes.Reply(json=fixtures.USER))
    async with client(hub) as rest:
        assert type(await rest.get_user()) is models.User


async def test_none_clears_and_unset_is_left_out(hub: fakes.FakeHub) -> None:
    hub.answer("PATCH", f"{ME}/items/{ITEM}", fakes.Reply(json=fixtures.ITEM))
    async with client(hub) as rest:
        await rest.update_item(ITEM, due_at=None, reminder_override=None)
    assert hub.requests[0].json == {"due_at": None, "reminder_override": None}


async def test_me_is_spelled_out_or_left_out(hub: fakes.FakeHub) -> None:
    hub.answer("GET", f"{ME}/lists", fakes.Reply(json=[]))
    async with client(hub) as rest:
        await rest.list_lists(user=ID("@me"))
    assert hub.requests[0].path == f"{ME}/lists"


async def test_create_reminder_needs_exactly_one(hub: fakes.FakeHub) -> None:
    async with client(hub) as rest:
        with pytest.raises(ValueError, match="exactly one"):
            await rest.create_reminder(ITEM)
        with pytest.raises(ValueError, match="exactly one"):
            await rest.create_reminder(ITEM, at=WHEN, after=datetime.timedelta(hours=1))
    assert hub.requests == []


async def test_failed_delivery_needs_a_reason(hub: fakes.FakeHub) -> None:
    async with client(hub) as rest:
        with pytest.raises(ValueError, match="reason"):
            await rest.acknowledge_delivery(DEL, status=DeliveryStatus.FAILED)
        with pytest.raises(ValueError, match="DELIVERED or as FAILED"):
            await rest.acknowledge_delivery(DEL, status=DeliveryStatus.EXPIRED)
    assert hub.requests == []


async def test_a_failed_test_needs_a_reason(hub: fakes.FakeHub) -> None:
    async with client(hub) as rest:
        with pytest.raises(ValueError, match="reason"):
            await rest.report_test(ID(fixtures.TEST_ID), status=DeliveryStatus.FAILED)
        with pytest.raises(ValueError, match="DELIVERED or as FAILED"):
            await rest.report_test(ID(fixtures.TEST_ID), status=DeliveryStatus.EXPIRED)
    assert hub.requests == []


async def test_the_picture_of_a_linked_account_comes_as_bytes(hub: fakes.FakeHub) -> None:
    hub.answer(
        "GET", "/users/@me/identities/i1/avatar", fakes.Reply(body=b"png", headers={"Content-Type": "image/png"})
    )
    async with client(hub) as rest:
        assert await rest.get_identity_avatar(ID("i1")) == b"png"


async def test_paginate_walks_every_page(hub: fakes.FakeHub) -> None:
    def page(title: str, cursor: str | None) -> fakes.Reply:
        return fakes.Reply(json={"data": [{**fixtures.ITEM, "title": title}], "next_cursor": cursor})

    hub.answer("GET", f"{ME}/items", page("one", "c1"), page("two", "c2"), page("three", None))
    async with client(hub) as rest:
        titles = [item.title async for item in paginate(rest.list_items, status=ItemStatus.DONE, limit=1)]
    assert titles == ["one", "two", "three"]
    assert [request.query for request in hub.requests] == [
        {"status": "done", "limit": "1"},
        {"status": "done", "limit": "1", "cursor": "c1"},
        {"status": "done", "limit": "1", "cursor": "c2"},
    ]


async def test_paginate_starts_at_a_given_cursor(hub: fakes.FakeHub) -> None:
    hub.answer("GET", f"{ME}/items", fakes.Reply(json=PAGE))
    async with client(hub) as rest:
        assert len([item async for item in paginate(rest.list_items, cursor="from-sync")]) == 1
    assert hub.requests[0].query == {"cursor": "from-sync"}


@pytest.fixture
def waits(monkeypatch: pytest.MonkeyPatch) -> list[float]:
    recorded: list[float] = []

    async def sleep(seconds: float) -> None:
        recorded.append(seconds)
        await asyncio.sleep(0)

    monkeypatch.setattr(rest_module, "_sleep", sleep)
    return recorded


async def test_the_wait_the_hub_asks_for_is_sat_out(hub: fakes.FakeHub, waits: list[float]) -> None:
    hub.answer("GET", "/info", fakes.limited("7"), fakes.limited("29.5"), fakes.Reply(json=fixtures.INFO))
    async with client(hub) as rest:
        assert (await rest.get_info()).version == "0.3.0"
    assert waits == [7.0, 29.5]
    assert len(hub.requests) == 3


async def test_a_wait_of_thirty_seconds_is_too_long(hub: fakes.FakeHub, waits: list[float]) -> None:
    hub.answer("GET", "/info", fakes.limited("1"), fakes.limited("30"), fakes.Reply(json=fixtures.INFO))
    async with client(hub) as rest:
        with pytest.raises(errors.RateLimitedError) as caught:
            await rest.get_info()
    assert caught.value.retry_after == 30
    assert waits == [1.0]
    assert len(hub.requests) == 2


async def test_a_timeout_is_not_retried(hub: fakes.FakeHub, waits: list[float]) -> None:
    hub.answer("GET", "/info", fakes.Reply(json=fixtures.INFO, delay=0.5), fakes.Reply(json=fixtures.INFO))
    async with client(hub, timeout=0.05) as rest:
        with pytest.raises(errors.NetworkError):
            await rest.get_info()
    assert len(hub.requests) == 1
    assert waits == []


async def test_an_unreadable_answer_is_not_fetched_again(hub: fakes.FakeHub, waits: list[float]) -> None:
    hub.answer("GET", "/info", fakes.Reply(200, body=b"<html>"), fakes.Reply(json=fixtures.INFO))
    async with client(hub) as rest:
        with pytest.raises(errors.NetworkError):
            await rest.get_info()
    assert len(hub.requests) == 1
    assert waits == []


@pytest.mark.parametrize("status", [400, 401, 403, 404, 409, 422, 500, 502, 503])
async def test_no_other_error_is_retried(hub: fakes.FakeHub, waits: list[float], status: int) -> None:
    refused = fakes.Reply(status, {"error": {"code": "x", "message": "no"}}, headers={"Retry-After": "1"})
    hub.answer("GET", "/info", refused, fakes.Reply(json=fixtures.INFO))
    async with client(hub) as rest:
        with pytest.raises(errors.APIError):
            await rest.get_info()
    assert len(hub.requests) == 1
    assert waits == []


async def test_durations_the_hub_cannot_hold_are_refused_before_sending(hub: fakes.FakeHub) -> None:
    async with client(hub) as rest:
        with pytest.raises(ValueError, match="whole minutes"):
            await rest.create_reminder(ITEM, after=datetime.timedelta(seconds=30))
        with pytest.raises(ValueError, match="whole minutes"):
            await rest.create_reminder(ITEM, after=datetime.timedelta(minutes=5, seconds=1))
        with pytest.raises(ValueError, match="whole minutes"):
            await rest.set_reminder_plan(Priority.LOW, before=[datetime.timedelta(0)], follow_up=FollowUp.NEVER)
    assert hub.requests == []


@pytest.mark.parametrize(
    ("status", "kind"),
    [
        (400, errors.BadRequestError),
        (401, errors.UnauthorizedError),
        (403, errors.ForbiddenError),
        (404, errors.NotFoundError),
        (409, errors.ConflictError),
        (418, errors.APIError),
        (422, errors.BadRequestError),
        (429, errors.RateLimitedError),
        (500, errors.ServerError),
        (503, errors.ServerError),
    ],
)
async def test_the_status_picks_the_error_class(hub: fakes.FakeHub, status: int, kind: type[errors.APIError]) -> None:
    hub.answer("GET", "/info", fakes.error(status, "some_code", "Something is wrong."))
    async with client(hub) as rest:
        with pytest.raises(errors.APIError) as caught:
            await rest.get_info()
    assert type(caught.value) is kind
    assert (caught.value.status, caught.value.code) == (status, "some_code")


OWN_ACCOUNT: list[Call] = [
    lambda r: r.update_user(display_name="x"),
    lambda r: r.list_identities(),
    lambda r: r.set_reminder_plan(Priority.LOW, before=[datetime.timedelta(days=1)], follow_up=FollowUp.NEVER),
    lambda r: r.reset_reminder_plan(Priority.LOW),
    lambda r: r.list_user_connections(),
    lambda r: r.set_connection_grant(CON, enabled=True),
    lambda r: r.list_reminder_log(),
]


@pytest.mark.parametrize("call", OWN_ACCOUNT)
async def test_what_only_the_user_may_do_is_refused_for_a_connection_before_sending(
    hub: fakes.FakeHub, call: Call
) -> None:
    async with client(hub, CONNECTION_TOKEN) as rest:
        with pytest.raises(TypeError, match="personal token"):
            await call(rest)
    assert hub.requests == []


def test_calls_about_the_account_itself_take_no_user() -> None:
    names = [
        "update_user",
        "list_identities",
        "get_identity_avatar",
        "set_reminder_plan",
        "reset_reminder_plan",
        "list_user_connections",
        "set_connection_grant",
        "list_reminder_log",
    ]
    assert [name for name in names if "user" in inspect.signature(getattr(RESTClient, name)).parameters] == []
    assert "user" in inspect.signature(RESTClient.list_reminder_plans).parameters
    assert "user" in inspect.signature(RESTClient.get_user).parameters


@pytest.mark.parametrize("bad", ["", ".", ".."])
async def test_an_id_that_would_change_the_path_is_refused_before_sending(hub: fakes.FakeHub, bad: str) -> None:
    async with client(hub) as rest:
        with pytest.raises(ValueError, match="not an ID"):
            await rest.delete_subitem(ITEM, ID(bad))
        with pytest.raises(ValueError, match="not an ID"):
            await rest.get_item(ID(bad))
        with pytest.raises(ValueError, match="not an ID"):
            await rest.delete_reminder(ID(bad), REM)
    async with client(hub, CONNECTION_TOKEN) as rest:
        with pytest.raises(ValueError, match="not an ID"):
            await rest.list_lists(user=ID(bad))
    assert hub.requests == []


async def test_ids_with_awkward_characters_stay_one_segment(hub: fakes.FakeHub) -> None:
    hub.answer("GET", "/users/@me/items/a/b?c#d e%2F..", fakes.Reply(json=fixtures.ITEM))
    async with client(hub) as rest:
        await rest.get_item(ID("a/b?c#d e%2F.."))
    assert hub.requests[0].raw_path == "/api/v1/users/@me/items/a%2Fb%3Fc%23d%20e%252F.."


async def test_due_date_takes_a_day_and_not_a_moment(hub: fakes.FakeHub) -> None:
    async with client(hub) as rest:
        with pytest.raises(TypeError, match="due_at"):
            await rest.create_item(title="x", due_date=WHEN)
        with pytest.raises(TypeError, match="due_at"):
            await rest.update_item(ITEM, due_date=WHEN)
    assert hub.requests == []


async def test_paginate_stops_on_a_cursor_that_repeats(hub: fakes.FakeHub) -> None:
    hub.answer("GET", f"{ME}/items", fakes.Reply(json={"data": [fixtures.ITEM], "next_cursor": "again"}))
    async with client(hub) as rest:
        walking = paginate(rest.list_items)
        assert (await anext(walking)).id == ITEM
        assert (await anext(walking)).id == ITEM
        with pytest.raises(errors.PlsremindError, match="same cursor"):
            await anext(walking)
    assert len(hub.requests) == 2


@pytest.mark.parametrize("header", ["1_0", "\u0661", "1e1", "0x5", "+3", "5 5"])
async def test_a_retry_after_that_is_not_plain_seconds_is_not_trusted(
    hub: fakes.FakeHub, waits: list[float], header: str
) -> None:
    hub.answer("GET", "/info", fakes.limited(header), fakes.Reply(json=fixtures.INFO))
    async with client(hub) as rest:
        with pytest.raises(errors.RateLimitedError) as caught:
            await rest.get_info()
    assert caught.value.retry_after is None
    assert waits == []


async def test_a_clock_time_the_hub_cannot_hold_is_refused(hub: fakes.FakeHub) -> None:
    async with client(hub) as rest:
        for start in (ClockTime(22, 0, 30), ClockTime(22, 0, tzinfo=datetime.UTC)):
            with pytest.raises(ValueError, match="hours and minutes"):
                await rest.update_user(quiet_hours=models.QuietHours(start=start, end=ClockTime(7, 0)))
    assert hub.requests == []
