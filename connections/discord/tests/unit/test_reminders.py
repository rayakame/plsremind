from __future__ import annotations

import asyncio
import collections.abc
import datetime
import http
import pathlib
import sqlite3
import typing
from unittest import mock

import aiohttp
import hikari
import plsremind
import pytest

from plsremind_discord import reminders as reminders_module
from plsremind_discord.reminders import Reminders
from plsremind_discord.store import Store
from plsremind_discord.views.reminder import ReminderView
from tests.support import discord
from tests.support import things
from tests.support.fake_hub import FakeHub

MIA = things.MIA
ITEM = things.item(1, updated_at=datetime.datetime(2026, 11, 19, 9, 0, 0, 123456, tzinfo=datetime.UTC))
CHANNEL = 7000


def forbidden() -> hikari.ForbiddenError:
    return hikari.ForbiddenError(
        url="https://discord.test", headers={}, raw_body=b"", message="Cannot send messages to this user", code=50007
    )


def broken() -> hikari.InternalServerError:
    return hikari.InternalServerError(
        url="https://discord.test", status=http.HTTPStatus.BAD_GATEWAY, headers={}, raw_body=b""
    )


def refused() -> hikari.BadRequestError:
    return hikari.BadRequestError(
        url="https://discord.test", headers={}, raw_body=b"", message="Invalid Form Body", code=50035
    )


def channel(account: int) -> mock.Mock:
    return mock.Mock(id=hikari.Snowflake(CHANNEL + account))


class Made(typing.NamedTuple):
    reminders: Reminders
    hub: FakeHub
    rest: mock.Mock
    store: Store
    clock: discord.FixedClock
    slept: list[float]


@pytest.fixture
async def made(tmp_path: pathlib.Path) -> collections.abc.AsyncIterator[Made]:
    hub = FakeHub()
    hub.knows(things.user(), ITEM)
    store = await Store.open(tmp_path / "discord.db")
    clock = discord.FixedClock()
    slept: list[float] = []
    rest = mock.Mock(spec=hikari.api.RESTClient)
    rest.create_dm_channel.side_effect = channel
    sent = iter(range(100, 200))

    def message(*_: object, **__: object) -> mock.Mock:
        return mock.Mock(id=hikari.Snowflake(next(sent)))

    rest.create_message.side_effect = message

    async def sleep(seconds: float) -> None:
        slept.append(seconds)
        clock.moment += datetime.timedelta(seconds=seconds)

    reminders = Reminders(
        hub=hub,
        views=discord.views(hub, ReminderView, clock=clock),
        rest=typing.cast("hikari.api.RESTClient", rest),
        store=store,
        texts=discord.say(),
        clock=clock,
        sleep=sleep,
    )
    try:
        yield Made(reminders, hub, rest, store, clock, slept)
    finally:
        await reminders.close()
        await store.close()


def delivery(
    *, to: plsremind.ConnectionUser | None = None, scheduled_for: datetime.datetime = things.NOW
) -> plsremind.Delivery:
    return things.delivery(1, of=ITEM, to=to, scheduled_for=scheduled_for)


async def test_a_reminder_goes_out_is_recorded_and_then_confirmed(made: Made) -> None:
    order: list[str] = []

    def sent(*_: object, **__: object) -> mock.Mock:
        order.append("send")
        return mock.Mock(id=hikari.Snowflake(100))

    def recorded(**_: object) -> object:
        order.append("record")
        return mock.DEFAULT

    def confirmed(*_: object) -> object:
        order.append("confirm")
        return mock.DEFAULT

    made.rest.create_message.side_effect = sent
    record = mock.AsyncMock(wraps=made.store.record, side_effect=recorded)
    confirm = mock.AsyncMock(wraps=made.hub.delivered, side_effect=confirmed)
    with mock.patch.object(made.store, "record", record), mock.patch.object(made.hub, "delivered", confirm):
        await made.reminders.deliver(delivery())

    assert order == ["send", "record", "confirm"]
    made.rest.create_dm_channel.assert_awaited_once_with(1234)
    assert made.rest.create_message.await_args.args[0].id == CHANNEL + 1234
    assert made.rest.create_message.await_args.kwargs["components"]
    assert made.hub.named("delivered") == [{"delivery": things.uid(9001)}]
    (row,) = await made.store.messages_for_item(ITEM.id)
    assert (row.message_id, row.channel_id, row.user_id) == (100, CHANNEL + 1234, MIA)
    assert row.item_updated_at == ITEM.updated_at
    assert await made.store.knows_delivery(things.uid(9001))
    assert made.reminders.waiting == frozenset()


async def test_a_delivery_that_went_out_before_is_only_confirmed(made: Made) -> None:
    await made.reminders.deliver(delivery())
    await made.reminders.deliver(delivery())
    assert made.rest.create_message.await_count == 1
    assert len(made.hub.named("delivered")) == 2


async def test_closed_dms_are_reported_in_the_users_language(made: Made) -> None:
    made.rest.create_message.side_effect = forbidden()
    await made.reminders.deliver(delivery())
    (call,) = made.hub.named("failed")
    assert call["delivery"] == things.uid(9001)
    assert str(call["reason"]).startswith("Discord hat die Direktnachricht nicht angenommen")
    assert made.hub.named("delivered") == []
    assert not await made.store.knows_delivery(things.uid(9001))
    assert made.slept == []


async def test_an_account_that_cannot_even_be_opened_counts_as_closed(made: Made) -> None:
    made.rest.create_dm_channel.side_effect = hikari.NotFoundError(
        url="https://discord.test", headers={}, raw_body=b"", message="Unknown User", code=10013
    )
    await made.reminders.deliver(delivery(to=things.user(locale="en")))
    assert str(made.hub.named("failed")[0]["reason"]).startswith("Discord did not accept")


async def test_one_account_that_takes_the_message_is_enough(made: Made) -> None:
    replies = iter([forbidden(), mock.Mock(id=hikari.Snowflake(101))])

    def answer(*_: object, **__: object) -> object:
        reply = next(replies)
        if isinstance(reply, Exception):
            raise reply
        return reply

    made.rest.create_message.side_effect = answer
    await made.reminders.deliver(delivery(to=things.user(external_ids=("1234", "5678"))))
    assert len(made.hub.named("delivered")) == 1
    assert made.hub.named("failed") == []
    assert [row.channel_id for row in await made.store.messages_for_item(ITEM.id)] == [CHANNEL + 5678]


async def test_each_reminder_mentions_the_account_it_goes_to_and_only_that_one(made: Made) -> None:
    await made.reminders.deliver(delivery(to=things.user(external_ids=("1234", "5678"))))
    calls = made.rest.create_message.await_args_list
    assert len(calls) == 2
    for call, account in zip(calls, (1234, 5678), strict=True):
        built = [builder.build()[0] for builder in call.kwargs["components"]]
        said = "\n".join(discord.payload_texts(built))
        assert said.startswith(f"<@{account}>\n")
        assert (call.kwargs["user_mentions"], call.kwargs["role_mentions"]) == ([account], False)
        assert call.kwargs["mentions_everyone"] is False


async def test_a_user_without_a_discord_account_is_reported(made: Made) -> None:
    await made.reminders.deliver(delivery(to=things.user(external_ids=("mia#0001",))))
    assert made.hub.named("failed") == [
        {"delivery": things.uid(9001), "reason": "Es ist kein Discord-Konto verknüpft."}
    ]
    made.rest.create_dm_channel.assert_not_called()


@pytest.mark.parametrize("trouble", [broken(), aiohttp.ClientConnectionError("gone"), TimeoutError()])
async def test_passing_trouble_is_tried_again_and_not_confirmed_until_it_works(made: Made, trouble: Exception) -> None:
    replies: list[object] = [trouble, trouble, mock.Mock(id=hikari.Snowflake(100))]

    def answer(*_: object, **__: object) -> object:
        reply = replies.pop(0)
        if isinstance(reply, Exception):
            raise reply
        return reply

    made.rest.create_message.side_effect = answer
    await made.reminders.deliver(delivery())
    await discord.until(lambda: not made.reminders.waiting)

    assert made.slept == [60.0, 300.0]
    assert len(made.hub.named("delivered")) == 1
    assert made.hub.named("failed") == []
    assert made.rest.create_message.await_count == 3


class Held(typing.NamedTuple):
    reminders: Reminders
    go: asyncio.Event


def held(made: Made) -> Held:
    """Reminders whose wait between tries only ends when the test says so."""
    go = asyncio.Event()

    async def sleep(_: float) -> None:
        await go.wait()

    reminders = Reminders(
        hub=made.hub,
        views=discord.views(made.hub, ReminderView),
        rest=typing.cast("hikari.api.RESTClient", made.rest),
        store=made.store,
        texts=discord.say(),
        clock=made.clock,
        sleep=sleep,
    )
    return Held(reminders, go)


async def test_the_same_delivery_arriving_during_a_try_starts_no_second_one(made: Made) -> None:
    sending, let_go = asyncio.Event(), asyncio.Event()

    async def stuck(*_: object, **__: object) -> mock.Mock:
        sending.set()
        await let_go.wait()
        return mock.Mock(id=hikari.Snowflake(100))

    made.rest.create_message.side_effect = stuck
    first = asyncio.create_task(made.reminders.deliver(delivery()))
    await asyncio.wait_for(sending.wait(), 2)
    await made.reminders.deliver(delivery())
    await made.reminders.deliver(delivery())
    let_go.set()
    await first
    assert made.rest.create_message.await_count == 1
    assert len(made.hub.named("delivered")) == 1


async def test_a_delivery_the_hub_sends_again_is_tried_at_once_instead_of_waiting_on(made: Made) -> None:
    waiting = held(made).reminders
    try:
        made.rest.create_message.side_effect = broken()
        await waiting.deliver(delivery())
        assert waiting.waiting == frozenset({things.uid(9001)})

        made.rest.create_message.side_effect = None
        made.rest.create_message.return_value = mock.Mock(id=hikari.Snowflake(300))
        await waiting.deliver(delivery())
        assert waiting.waiting == frozenset()
        assert len(made.hub.named("delivered")) == 1
        assert made.rest.create_message.await_count == 2
    finally:
        await waiting.close()


async def test_a_delivery_sent_again_that_still_fails_keeps_exactly_one_wait(made: Made) -> None:
    waiting = held(made).reminders
    try:
        made.rest.create_message.side_effect = broken()
        await waiting.deliver(delivery())
        await waiting.deliver(delivery())
        await waiting.deliver(delivery())
        assert waiting.waiting == frozenset({things.uid(9001)})
        assert made.rest.create_message.await_count == 3
        chains = [task for task in asyncio.all_tasks() if task.get_name() == f"reminder-{things.uid(9001)}"]
        await asyncio.sleep(0)
        assert len([task for task in chains if not task.done()]) == 1
    finally:
        await waiting.close()
    assert waiting.waiting == frozenset()


async def test_a_delivery_sent_again_while_a_later_try_is_under_way_is_left_to_it(made: Made) -> None:
    waiting, go = held(made)
    sending, let_go = asyncio.Event(), asyncio.Event()
    try:
        made.rest.create_message.side_effect = broken()
        await waiting.deliver(delivery())

        async def stuck(*_: object, **__: object) -> mock.Mock:
            sending.set()
            await let_go.wait()
            return mock.Mock(id=hikari.Snowflake(300))

        made.rest.create_message.side_effect = stuck
        go.set()
        await asyncio.wait_for(sending.wait(), 2)
        await waiting.deliver(delivery())
        let_go.set()
        await discord.until(lambda: not waiting.waiting)
    finally:
        await waiting.close()
    assert made.rest.create_message.await_count == 2, "one failed try and the one that was under way"
    assert len(made.hub.named("delivered")) == 1


@pytest.mark.parametrize("due", [datetime.timedelta(0), datetime.timedelta(hours=23, minutes=59, seconds=30)])
async def test_tries_end_a_day_after_the_first_one(made: Made, due: datetime.timedelta) -> None:
    made.rest.create_message.side_effect = broken()
    await made.reminders.deliver(delivery(scheduled_for=made.clock.moment - due))
    await discord.until(lambda: not made.reminders.waiting)

    day = 24 * 3600
    assert made.slept[:4] == [60.0, 300.0, 900.0, 1800.0]
    assert sum(made.slept[:-1]) < day <= sum(made.slept)
    assert made.hub.named("delivered") == []
    assert made.hub.named("failed") == []
    assert made.rest.create_message.await_count == len(made.slept)


async def test_a_withdrawn_delivery_is_not_tried_again(made: Made) -> None:
    made.rest.create_message.side_effect = broken()
    waiting = held(made).reminders
    try:
        await waiting.deliver(delivery())
        assert waiting.waiting == frozenset({things.uid(9001)})
        await asyncio.sleep(0)
        waiting.withdraw(things.uid(9001))
        waiting.withdraw(things.uid(9999))
        await discord.until(lambda: not waiting.waiting)
        assert made.rest.create_message.await_count == 1
    finally:
        await waiting.close()


async def test_a_delivery_withdrawn_during_its_first_try_is_not_tried_again(made: Made) -> None:
    sending, let_go = asyncio.Event(), asyncio.Event()

    async def stuck(*_: object, **__: object) -> mock.Mock:
        sending.set()
        await let_go.wait()
        raise broken()

    made.rest.create_message.side_effect = stuck
    first = asyncio.create_task(made.reminders.deliver(delivery()))
    await asyncio.wait_for(sending.wait(), 2)
    made.reminders.withdraw(things.uid(9001))
    let_go.set()
    await first
    assert made.reminders.waiting == frozenset()
    assert made.slept == []
    assert made.rest.create_message.await_count == 1


async def later_try_under_way(made: Made) -> tuple[Held, asyncio.Event]:
    """A delivery whose second try is stuck inside Discord until the returned event is set."""
    waiting = held(made)
    sending, let_go = asyncio.Event(), asyncio.Event()
    made.rest.create_message.side_effect = broken()
    await waiting.reminders.deliver(delivery())

    async def stuck(*_: object, **__: object) -> mock.Mock:
        sending.set()
        await let_go.wait()
        return mock.Mock(id=hikari.Snowflake(300))

    made.rest.create_message.side_effect = stuck
    waiting.go.set()
    await asyncio.wait_for(sending.wait(), 2)
    return waiting, let_go


async def test_a_delivery_withdrawn_during_a_later_try_lets_it_finish_and_record(made: Made) -> None:
    waiting, let_go = await later_try_under_way(made)
    try:
        waiting.reminders.withdraw(things.uid(9001))
        let_go.set()
        await discord.until(lambda: not waiting.reminders.waiting)
    finally:
        await waiting.reminders.close()
    assert [row.message_id for row in await made.store.messages_for_item(ITEM.id)] == [300]
    assert made.rest.create_message.await_count == 2


async def test_closing_lets_a_try_that_is_under_way_record_its_message(made: Made) -> None:
    waiting, let_go = await later_try_under_way(made)
    closing = asyncio.create_task(waiting.reminders.close())
    await asyncio.sleep(0)
    let_go.set()
    await asyncio.wait_for(closing, 2)
    assert [row.message_id for row in await made.store.messages_for_item(ITEM.id)] == [300]
    assert waiting.reminders.waiting == frozenset()


async def test_closing_does_not_wait_forever_for_a_try_that_hangs(made: Made) -> None:
    waiting, _ = await later_try_under_way(made)
    with mock.patch.object(reminders_module, "CLOSE_GRACE", 0.01):
        await asyncio.wait_for(waiting.reminders.close(), 2)
    assert waiting.reminders.waiting == frozenset()
    assert await made.store.messages_for_item(ITEM.id) == []


async def test_trouble_nobody_foresaw_is_tried_again_like_any_other(
    made: Made, caplog: pytest.LogCaptureFixture
) -> None:
    answers = mock.AsyncMock(side_effect=[plsremind.NetworkError("the hub is away"), ITEM, ITEM])
    with mock.patch.object(made.hub, "item", answers):
        await made.reminders.deliver(delivery())
        await discord.until(lambda: not made.reminders.waiting)
    assert made.slept == [60.0]
    assert len(made.hub.named("delivered")) == 1
    assert "could not be handled" in caplog.text


async def test_a_sent_reminder_that_cannot_be_recorded_is_still_confirmed_and_not_sent_again(
    made: Made, caplog: pytest.LogCaptureFixture
) -> None:
    with mock.patch.object(
        made.store, "record", mock.AsyncMock(side_effect=sqlite3.OperationalError("disk I/O error"))
    ):
        await made.reminders.deliver(delivery())
    assert made.rest.create_message.await_count == 1
    assert len(made.hub.named("delivered")) == 1
    assert made.reminders.waiting == frozenset()
    assert "could not be recorded" in caplog.text


async def test_a_confirmation_that_does_not_get_through_is_made_again(
    made: Made, caplog: pytest.LogCaptureFixture
) -> None:
    waiting, go = held(made)
    try:
        made.hub.fail["delivered"] = plsremind.NetworkError("no route")
        await waiting.deliver(delivery())
        assert await made.store.knows_delivery(things.uid(9001))
        assert waiting.waiting == frozenset({things.uid(9001)})
        assert "could not be confirmed" in caplog.text

        del made.hub.fail["delivered"]
        go.set()
        await discord.until(lambda: not waiting.waiting)
    finally:
        await waiting.close()
    assert made.rest.create_message.await_count == 1
    assert len(made.hub.named("delivered")) == 2


async def test_a_report_that_does_not_get_through_is_made_again(made: Made, caplog: pytest.LogCaptureFixture) -> None:
    waiting, go = held(made)
    try:
        made.rest.create_message.side_effect = forbidden()
        made.hub.fail["failed"] = plsremind.NetworkError("no route")
        await waiting.deliver(delivery())
        assert waiting.waiting == frozenset({things.uid(9001)})
        assert "could not be reported" in caplog.text

        del made.hub.fail["failed"]
        go.set()
        await discord.until(lambda: not waiting.waiting)
    finally:
        await waiting.close()
    assert made.rest.create_message.await_count == 1, "the verdict stands; the user is not tried a second time"
    assert len(made.hub.named("failed")) == 2
    assert made.hub.named("delivered") == []


async def test_a_later_try_goes_to_the_accounts_the_user_has_then(made: Made) -> None:
    waiting, go = held(made)
    try:
        made.rest.create_message.side_effect = broken()
        await waiting.deliver(delivery())
        made.hub.knows(things.user(external_ids=("5678",)), ITEM)
        made.rest.create_message.side_effect = None
        made.rest.create_message.return_value = mock.Mock(id=hikari.Snowflake(300))
        go.set()
        await discord.until(lambda: not waiting.waiting)
    finally:
        await waiting.close()
    assert [call.args[0] for call in made.rest.create_dm_channel.await_args_list] == [1234, 5678]
    assert len(made.hub.named("delivered")) == 1


async def test_a_later_try_for_a_user_who_is_gone_reports_that_no_account_is_left(made: Made) -> None:
    waiting, go = held(made)
    try:
        made.rest.create_message.side_effect = broken()
        await waiting.deliver(delivery())
        made.hub.people.clear()
        go.set()
        await discord.until(lambda: not waiting.waiting)
    finally:
        await waiting.close()
    assert made.rest.create_message.await_count == 1
    assert made.hub.named("failed")[0]["reason"] == "Es ist kein Discord-Konto verknüpft."


async def test_a_reminder_that_could_be_neither_recorded_nor_confirmed_is_not_sent_again(
    made: Made, caplog: pytest.LogCaptureFixture
) -> None:
    waiting, go = held(made)
    try:
        made.hub.fail["delivered"] = plsremind.NetworkError("no route")
        with mock.patch.object(made.store, "record", mock.AsyncMock(side_effect=sqlite3.OperationalError())):
            await waiting.deliver(delivery())
            assert waiting.waiting == frozenset({things.uid(9001)})
            await waiting.deliver(delivery())
            del made.hub.fail["delivered"]
            go.set()
            await discord.until(lambda: not waiting.waiting)
    finally:
        await waiting.close()
    assert made.rest.create_message.await_count == 1
    assert len(made.hub.named("delivered")) == 3
    assert "could not be recorded" in caplog.text


async def test_a_message_that_went_out_is_not_sent_again_after_a_withdrawal_and_a_new_arrival(made: Made) -> None:
    made.hub.fail["delivered"] = plsremind.NetworkError("no route")
    with mock.patch.object(made.store, "record", mock.AsyncMock(side_effect=sqlite3.OperationalError())):
        await made.reminders.deliver(delivery())
        made.reminders.withdraw(things.uid(9001))
        del made.hub.fail["delivered"]
        await made.reminders.deliver(delivery())
    assert made.rest.create_message.await_count == 2, "a withdrawal ends what the bot knows of it"


async def test_a_message_discord_refuses_for_good_is_reported_and_not_tried_again(
    made: Made, caplog: pytest.LogCaptureFixture
) -> None:
    made.rest.create_message.side_effect = refused()
    await made.reminders.deliver(delivery())
    assert made.reminders.waiting == frozenset()
    assert made.rest.create_message.await_count == 1
    (failed,) = made.hub.named("failed")
    assert failed["reason"] == "Discord hat die Erinnerung nicht angenommen."
    assert "Invalid Form Body" in caplog.text


async def test_passing_trouble_on_one_account_outweighs_a_refusal_on_another(made: Made) -> None:
    made.hub.knows(things.user(external_ids=("1234", "5678")), ITEM)
    made.rest.create_message.side_effect = [refused(), broken()]
    await made.reminders.deliver(delivery(to=things.user(external_ids=("1234", "5678"))))
    assert made.reminders.waiting == frozenset({things.uid(9001)})
    assert made.hub.named("failed") == []


async def test_a_withdrawal_while_the_failure_is_being_reported_is_no_error(
    made: Made, caplog: pytest.LogCaptureFixture
) -> None:
    made.rest.create_message.side_effect = forbidden()
    reporting, let_go = asyncio.Event(), asyncio.Event()
    real = made.hub.failed

    async def slow(delivery_id: plsremind.ID, reason: str) -> None:
        reporting.set()
        await let_go.wait()
        await real(delivery_id, reason)

    with mock.patch.object(made.hub, "failed", slow):
        first = asyncio.create_task(made.reminders.deliver(delivery()))
        await asyncio.wait_for(reporting.wait(), 2)
        made.reminders.withdraw(things.uid(9001))
        let_go.set()
        await first
    assert made.reminders.waiting == frozenset()
    assert "could not be handled" not in caplog.text


async def test_a_reminder_whose_item_cannot_be_read_is_not_sent_as_gone(made: Made) -> None:
    made.hub.people.clear()
    await made.reminders.deliver(delivery())
    made.rest.create_message.assert_not_called()
    assert made.hub.named("delivered") == []
    assert made.reminders.waiting == frozenset({things.uid(9001)})


async def test_closing_does_not_wait_out_the_grace_for_a_try_that_failed(made: Made) -> None:
    waiting, go = held(made)
    sending, let_go = asyncio.Event(), asyncio.Event()
    made.rest.create_message.side_effect = broken()
    await waiting.deliver(delivery())

    async def stuck(*_: object, **__: object) -> mock.Mock:
        sending.set()
        await let_go.wait()
        raise broken()

    made.rest.create_message.side_effect = stuck
    go.set()
    await asyncio.wait_for(sending.wait(), 2)
    with mock.patch.object(reminders_module, "CLOSE_GRACE", 2.0):
        closing = asyncio.create_task(waiting.close())
        await asyncio.sleep(0)
        let_go.set()
        started = asyncio.get_running_loop().time()
        await asyncio.wait_for(closing, 3)
    assert asyncio.get_running_loop().time() - started < 1.0
