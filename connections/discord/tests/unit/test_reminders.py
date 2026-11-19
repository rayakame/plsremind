from __future__ import annotations

import asyncio
import collections.abc
import datetime
import http
import pathlib
import typing
from unittest import mock

import aiohttp
import hikari
import plsremind
import pytest

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

    made.rest.create_message.side_effect = sent
    spy = mock.AsyncMock(wraps=made.store.record, side_effect=recorded)
    with mock.patch.object(made.store, "record", spy):
        await made.reminders.deliver(delivery())
    order.extend(name for name, _ in made.hub.calls if name == "delivered")

    assert order == ["send", "record", "delivered"]
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


def held(made: Made) -> Reminders:
    """Reminders whose wait between tries never ends by itself."""

    async def sleep(_: float) -> None:
        await asyncio.Event().wait()

    return Reminders(
        hub=made.hub,
        views=discord.views(made.hub, ReminderView),
        rest=typing.cast("hikari.api.RESTClient", made.rest),
        store=made.store,
        texts=discord.say(),
        clock=made.clock,
        sleep=sleep,
    )


async def test_the_same_delivery_arriving_again_starts_no_second_try(made: Made) -> None:
    made.rest.create_message.side_effect = broken()
    waiting = held(made)
    try:
        await waiting.deliver(delivery())
        await waiting.deliver(delivery())
        await waiting.deliver(delivery())
        assert waiting.waiting == frozenset({things.uid(9001)})
        assert made.rest.create_message.await_count == 1
    finally:
        await waiting.close()
    assert waiting.waiting == frozenset()


async def test_tries_end_a_day_after_the_reminder_was_due(made: Made) -> None:
    made.rest.create_message.side_effect = broken()
    await made.reminders.deliver(delivery(scheduled_for=made.clock.moment - datetime.timedelta(hours=23)))
    await discord.until(lambda: not made.reminders.waiting)

    assert made.slept == [60.0, 300.0, 900.0, 1800.0, 1800.0]
    assert made.hub.named("delivered") == []
    assert made.hub.named("failed") == []
    assert made.rest.create_message.await_count == 5


async def test_a_withdrawn_delivery_is_not_tried_again(made: Made) -> None:
    made.rest.create_message.side_effect = broken()
    waiting = held(made)
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


async def test_a_confirmation_that_does_not_get_through_is_made_up_for_later(
    made: Made, caplog: pytest.LogCaptureFixture
) -> None:
    made.hub.fail["delivered"] = plsremind.NetworkError("no route")
    await made.reminders.deliver(delivery())
    assert await made.store.knows_delivery(things.uid(9001))
    assert "could not be confirmed" in caplog.text

    del made.hub.fail["delivered"]
    await made.reminders.deliver(delivery())
    assert made.rest.create_message.await_count == 1
    assert len(made.hub.named("delivered")) == 2


async def test_a_report_that_does_not_get_through_is_logged(made: Made, caplog: pytest.LogCaptureFixture) -> None:
    made.rest.create_message.side_effect = forbidden()
    made.hub.fail["failed"] = plsremind.NetworkError("no route")
    await made.reminders.deliver(delivery())
    assert "could not be reported" in caplog.text
