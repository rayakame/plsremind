from __future__ import annotations

import collections.abc
import pathlib
import typing
from unittest import mock

import hikari
import pytest

from plsremind_discord.metrics import Metrics
from plsremind_discord.reminders import Reminders
from plsremind_discord.store import Sent
from plsremind_discord.store import Store
from plsremind_discord.views.common import Notice
from plsremind_discord.views.reminder import ReminderView
from tests.support import discord
from tests.support import things
from tests.support.fake_hub import FakeHub
from tests.unit.test_reminders import BOT
from tests.unit.test_reminders import CHANNEL
from tests.unit.test_reminders import broken
from tests.unit.test_reminders import channel
from tests.unit.test_reminders import forbidden
from tests.unit.test_reminders import refused

TESTS = "plsremind_discord_tests_total"


class Made(typing.NamedTuple):
    reminders: Reminders
    hub: FakeHub
    rest: mock.Mock
    store: Store
    metrics: Metrics


@pytest.fixture
async def made(tmp_path: pathlib.Path) -> collections.abc.AsyncIterator[Made]:
    hub = FakeHub()
    hub.knows(things.user())
    store = await Store.open(tmp_path / "discord.db")
    rest = mock.Mock(spec=hikari.api.RESTClient)
    rest.create_dm_channel.side_effect = channel
    rest.create_message.return_value = mock.Mock(id=hikari.Snowflake(100))
    metrics = Metrics()
    clock = discord.FixedClock()
    reminders = Reminders(
        metrics=metrics,
        hub=hub,
        views=discord.views(hub, ReminderView, Notice, clock=clock),
        rest=typing.cast("hikari.api.RESTClient", rest),
        store=store,
        texts=discord.say(),
        clock=clock,
        bot=BOT,
        redraw=lambda _message, _channel: None,
    )
    try:
        yield Made(reminders, hub, rest, store, metrics)
    finally:
        await reminders.close()
        await store.close()


def said(call: mock.Mock) -> str:
    built = [builder.build()[0] for builder in call.kwargs["components"]]
    return "\n".join(discord.payload_texts(built))


async def test_a_test_goes_to_every_linked_account_and_is_confirmed(made: Made) -> None:
    await made.reminders.probe(things.probe(to=things.user(external_ids=("1234", "5678"))))
    calls = made.rest.create_message.await_args_list
    assert [call.args[0] for call in calls] == [CHANNEL + 1234, CHANNEL + 5678]
    assert all("Hier kommen deine Erinnerungen an" in said(call) for call in calls)
    assert made.hub.named("tested") == [{"test": things.uid(9501)}]
    assert made.hub.named("test_failed") == []
    assert discord.counted(made.metrics, TESTS, result="sent") == 1
    assert await made.store.messages_for_user(things.MIA) == []


async def test_a_test_speaks_the_users_language(made: Made) -> None:
    await made.reminders.probe(things.probe(to=things.user(locale="en")))
    assert "this is where your reminders arrive" in said(made.rest.create_message.await_args)


async def test_a_kept_dm_channel_is_used(made: Made) -> None:
    await made.store.record(
        delivery_id="earlier",
        user_id=things.MIA,
        item_id=things.uid(1),
        shown_at=None,
        messages=[Sent(555, 4242, 1234)],
        bot_id=BOT,
        now=things.NOW,
    )
    await made.reminders.probe(things.probe())
    made.rest.create_dm_channel.assert_not_called()
    assert made.rest.create_message.await_args.args[0] == 4242


async def test_closed_dms_fail_the_test_with_the_usual_reason(made: Made) -> None:
    made.rest.create_message.side_effect = forbidden()
    await made.reminders.probe(things.probe())
    (call,) = made.hub.named("test_failed")
    assert str(call["reason"]).startswith("Discord hat die Direktnachricht nicht angenommen")
    assert made.hub.named("tested") == []
    assert discord.counted(made.metrics, TESTS, result="closed") == 1


async def test_discord_refusing_fails_the_test(made: Made) -> None:
    made.rest.create_message.side_effect = refused()
    await made.reminders.probe(things.probe())
    assert made.hub.named("test_failed")[0]["reason"] == "Discord hat die Erinnerung nicht angenommen."
    assert discord.counted(made.metrics, TESTS, result="refused") == 1


async def test_passing_trouble_fails_the_test_without_trying_again(made: Made) -> None:
    made.rest.create_message.side_effect = broken()
    await made.reminders.probe(things.probe())
    assert made.rest.create_message.await_count == 1
    assert str(made.hub.named("test_failed")[0]["reason"]).startswith("Discord war gerade nicht erreichbar")
    assert discord.counted(made.metrics, TESTS, result="trouble") == 1
    assert made.reminders.waiting == frozenset()


async def test_no_account_fails_the_test(made: Made) -> None:
    await made.reminders.probe(things.probe(to=things.user(external_ids=("mia#0001",))))
    assert made.hub.named("test_failed") == [
        {"test": things.uid(9501), "reason": "Es ist kein Discord-Konto verknüpft."}
    ]
    made.rest.create_dm_channel.assert_not_called()
    assert discord.counted(made.metrics, TESTS, result="no_account") == 1


async def test_an_unforeseen_error_is_logged_and_reported(made: Made, caplog: pytest.LogCaptureFixture) -> None:
    made.rest.create_dm_channel.side_effect = RuntimeError("boom")
    await made.reminders.probe(things.probe())
    assert "test" in caplog.text
    reason = str(made.hub.named("test_failed")[0]["reason"])
    assert reason.startswith("Beim Bot ist etwas schiefgegangen")
    assert "Admin" in reason
    assert "Log" not in reason
    assert discord.counted(made.metrics, TESTS, result="error") == 1
