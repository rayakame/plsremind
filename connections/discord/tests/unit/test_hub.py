from __future__ import annotations

import asyncio
import collections.abc
import datetime
import inspect
import typing
from unittest import mock

import msgspec
import plsremind
import pytest

from plsremind_discord import hub as hub_module
from plsremind_discord.config import Config
from plsremind_discord.hub import Hub
from plsremind_discord.hub import HubError
from plsremind_discord.timeparse import Day
from plsremind_discord.timeparse import Moment
from tests.support import things
from tests.support.fake_hub import FakeHub

MIA, BEN = things.MIA, things.BEN
ITEM = things.item(1)


class Made(typing.NamedTuple):
    hub: Hub
    client: mock.Mock


def connection(provider: str = "discord") -> plsremind.Principal:
    return plsremind.Principal(
        kind=plsremind.CredentialKind.CONNECTION,
        id=things.uid(50),
        connection=plsremind.PrincipalConnection(id=things.uid(50), name="Discord", provider=provider),
    )


@pytest.fixture
def made() -> Made:
    client = mock.Mock(spec=plsremind.Client)
    client.cache = mock.Mock(spec=plsremind.Cache)
    client.rest = mock.Mock(spec=plsremind.RESTClient)
    client.principal = connection()
    client.status = plsremind.ClientStatus.CONNECTED
    client.cache.get_users.return_value = ()
    client.cache.get_item.return_value = None
    client.cache.get_lists.return_value = (things.a_list(),)
    return Made(Hub(typing.cast("plsremind.Client", client)), client)


def learn(made: Made, user: plsremind.ConnectionUser) -> None:
    made.client.cache.get_profile.return_value = user
    made.hub.user_ready(plsremind.UserReadyEvent(user_id=user.id))


async def test_the_hub_listens_for_users_coming_changing_and_going(made: Made) -> None:
    kinds = [call.args[0] for call in made.client.subscribe.call_args_list]
    assert kinds == [plsremind.UserReadyEvent, plsremind.UserUpdateEvent, plsremind.UserGoneEvent]
    came, changed, went = (call.args[1] for call in made.client.subscribe.call_args_list)
    made.client.cache.get_profile.return_value = things.user()
    await came(plsremind.UserReadyEvent(user_id=MIA))
    assert made.hub.user_of(1234) == MIA
    moved = things.user(external_ids=("9",))
    await changed(plsremind.UserUpdateEvent(user_id=MIA, actor=None, nonce=None, user=moved))
    assert made.hub.user_of(9) == MIA
    await went(plsremind.UserGoneEvent(user_id=MIA))
    assert made.hub.user_of(9) is None


def test_every_discord_account_of_a_user_leads_to_them(made: Made) -> None:
    learn(made, things.user(external_ids=("1234", "5678")))
    assert made.hub.user_of(1234) == MIA
    assert made.hub.user_of(5678) == MIA
    assert made.hub.user_of(999) is None
    assert made.hub.discord_ids(MIA) == (1234, 5678)
    assert made.hub.discord_ids(BEN) == ()


def test_an_account_that_is_not_a_number_is_skipped(made: Made) -> None:
    learn(made, things.user(external_ids=("mia#0001", "1234")))
    assert made.hub.discord_ids(MIA) == (1234,)


def test_a_changed_profile_replaces_the_accounts(made: Made) -> None:
    learn(made, things.user(external_ids=("1234", "5678")))
    changed = things.user(external_ids=("5678", "9"))
    made.hub.user_updated(plsremind.UserUpdateEvent(user_id=MIA, actor=None, nonce=None, user=changed))
    assert [made.hub.user_of(account) for account in (1234, 5678, 9)] == [None, MIA, MIA]


def test_a_user_who_is_gone_is_forgotten(made: Made) -> None:
    learn(made, things.user())
    learn(made, things.user(BEN, external_ids=("77",)))
    made.hub.user_gone(plsremind.UserGoneEvent(user_id=MIA))
    assert made.hub.user_of(1234) is None
    assert made.hub.discord_ids(MIA) == ()
    assert made.hub.user_of(77) == BEN


def test_a_user_the_cache_does_not_hold_is_not_learned(made: Made) -> None:
    made.client.cache.get_profile.return_value = None
    made.hub.user_ready(plsremind.UserReadyEvent(user_id=MIA))
    assert made.hub.discord_ids(MIA) == ()


def test_an_account_that_moved_to_someone_else_stays_theirs(made: Made) -> None:
    learn(made, things.user())
    learn(made, things.user(BEN))
    made.hub.user_gone(plsremind.UserGoneEvent(user_id=MIA))
    assert made.hub.user_of(1234) == BEN


def test_reads_come_from_the_cache(made: Made) -> None:
    made.client.cache.get_profile.return_value = things.user()
    made.client.cache.get_items.return_value = (ITEM,)
    made.client.cache.get_list.return_value = things.a_list(things.WORK, "Work")
    assert made.hub.profile(MIA) == things.user()
    assert made.hub.lists(MIA) == (things.a_list(),)
    assert made.hub.open_items(MIA) == (ITEM,)
    assert made.hub.list_name(things.WORK) == "Work"
    made.client.cache.get_list.return_value = None
    assert made.hub.list_name(things.WORK) is None
    made.client.cache.get_items.assert_called_once_with(MIA)


async def test_starting_checks_the_token_and_learns_who_is_there(made: Made) -> None:
    made.client.cache.get_users.return_value = (MIA,)
    made.client.cache.get_profile.return_value = things.user()
    assert not made.hub.ready.is_set()
    await made.hub.start()
    assert made.hub.ready.is_set()
    assert made.hub.user_of(1234) == MIA


@pytest.mark.parametrize(
    "principal",
    [
        None,
        plsremind.Principal(kind=plsremind.CredentialKind.PERSONAL_TOKEN, id=things.uid(51), user_id=MIA),
        connection("telegram"),
    ],
)
async def test_a_token_that_is_no_discord_connection_is_refused(
    made: Made, principal: plsremind.Principal | None
) -> None:
    made.client.principal = principal
    with pytest.raises(HubError, match="discord"):
        await made.hub.start()
    made.client.close.assert_awaited_once()
    assert not made.hub.ready.is_set()


async def test_closing_closes_the_client(made: Made) -> None:
    await made.hub.close()
    made.client.close.assert_awaited_once()


async def test_an_item_of_the_user_comes_from_the_cache(made: Made) -> None:
    assert made.hub.cached(MIA, ITEM.id) is None
    made.client.cache.get_item.return_value = ITEM
    assert made.hub.cached(MIA, ITEM.id) == ITEM
    assert await made.hub.item(MIA, ITEM.id) == ITEM
    made.client.rest.get_item.assert_not_called()


async def test_a_cached_item_of_someone_else_is_asked_for_at_the_hub(made: Made) -> None:
    made.client.cache.get_item.return_value = things.item(1, list_id=things.WORK)
    assert made.hub.cached(MIA, ITEM.id) is None
    made.client.rest.get_item.side_effect = plsremind.NotFoundError(404, "not_found", "no")
    assert await made.hub.item(MIA, ITEM.id) is None
    made.client.rest.get_item.assert_awaited_once_with(ITEM.id, user=MIA)


async def test_an_item_the_cache_lacks_is_fetched(made: Made) -> None:
    made.client.rest.get_item.return_value = ITEM
    assert await made.hub.item(MIA, ITEM.id) == ITEM


async def test_other_failures_of_a_fetch_come_out(made: Made) -> None:
    made.client.rest.get_item.side_effect = plsremind.ServerError(500, "boom", "no")
    with pytest.raises(plsremind.ServerError):
        await made.hub.item(MIA, ITEM.id)


def sent(call: mock.Mock) -> tuple[tuple[object, ...], dict[str, object]]:
    """What a REST method was last called with, without the nonce."""
    last = call.await_args
    return last.args, {name: value for name, value in last.kwargs.items() if name != "nonce"}


def nonce(call: mock.Mock) -> str:
    return call.await_args.kwargs["nonce"]


async def test_writes_name_the_user_and_say_which_message_they_came_from(made: Made) -> None:
    rest = made.client.rest
    at = things.NOW + datetime.timedelta(hours=1)
    await made.hub.schedule(MIA, ITEM.id)
    rest.get_item_schedule.assert_awaited_once_with(ITEM.id, user=MIA)

    await made.hub.remind(MIA, ITEM.id, at, origin=555)
    assert sent(rest.create_reminder) == ((ITEM.id,), {"at": at, "user": MIA})
    assert hub_module.origin_of(nonce(rest.create_reminder)) == 555

    await made.hub.set_done(MIA, ITEM.id, done=True)
    assert sent(rest.update_item) == ((ITEM.id,), {"status": plsremind.ItemStatus.DONE, "user": MIA})
    assert hub_module.origin_of(nonce(rest.update_item)) is None
    await made.hub.set_done(MIA, ITEM.id, done=False, origin=556)
    assert sent(rest.update_item) == ((ITEM.id,), {"status": plsremind.ItemStatus.OPEN, "user": MIA})

    await made.hub.delete(MIA, ITEM.id, origin=557)
    assert sent(rest.delete_item) == ((ITEM.id,), {"user": MIA})
    assert hub_module.origin_of(nonce(rest.delete_item)) == 557

    await made.hub.tick(MIA, ITEM.id, things.uid(5001), done=True, origin=558)
    assert sent(rest.update_subitem) == ((ITEM.id, things.uid(5001)), {"done": True, "user": MIA})


async def test_a_new_item_sends_only_what_was_given(made: Made) -> None:
    rest = made.client.rest
    await made.hub.add(MIA, title="Laundry")
    assert sent(rest.create_item) == ((), {"title": "Laundry", "user": MIA})
    due = things.NOW + datetime.timedelta(days=1)
    await made.hub.add(
        MIA, title="Report", note="see wiki", list_id=things.WORK, priority=plsremind.Priority.HIGH, due_at=due
    )
    assert sent(rest.create_item)[1] == {
        "title": "Report",
        "note": "see wiki",
        "list_id": things.WORK,
        "priority": plsremind.Priority.HIGH,
        "due_at": due,
        "user": MIA,
    }
    await made.hub.add(MIA, title="Party", due_date=datetime.date(2026, 12, 24))
    assert sent(rest.create_item)[1] == {"title": "Party", "due_date": datetime.date(2026, 12, 24), "user": MIA}


async def test_an_edit_sends_the_deadline_the_way_it_was_meant(made: Made) -> None:
    rest = made.client.rest
    at = things.NOW + datetime.timedelta(days=1)
    await made.hub.edit(MIA, ITEM.id, title="A", due=Moment(at=at), note="n")
    assert sent(rest.update_item) == ((ITEM.id,), {"title": "A", "due_at": at, "note": "n", "user": MIA})
    await made.hub.edit(MIA, ITEM.id, title="A", due=Day(date=at.date()), note=None)
    assert sent(rest.update_item)[1] == {"title": "A", "due_date": at.date(), "user": MIA}
    await made.hub.edit(MIA, ITEM.id, title="A", due=None, note="")
    assert sent(rest.update_item)[1] == {"title": "A", "due_at": None, "note": "", "user": MIA}
    await made.hub.edit(MIA, ITEM.id, title="A", due=hub_module.UNCHANGED, note=None)
    assert sent(rest.update_item)[1] == {"title": "A", "user": MIA}


async def test_a_write_waits_until_the_gateway_has_reported_it(made: Made) -> None:
    await made.hub.set_done(MIA, ITEM.id, done=True, origin=7)
    waited = made.client.wait_for.await_args
    assert waited.args == (plsremind.ItemUpdateEvent,)
    assert waited.kwargs["timeout"] is None, "the time to wait starts once the hub has answered"
    accepts = waited.kwargs["predicate"]
    own = nonce(made.client.rest.update_item)
    assert accepts(plsremind.ItemUpdateEvent(user_id=MIA, actor=None, nonce=own, item=ITEM))
    assert not accepts(plsremind.ItemUpdateEvent(user_id=MIA, actor=None, nonce="7.someone-else", item=ITEM))
    assert not accepts(plsremind.ItemUpdateEvent(user_id=MIA, actor=None, nonce=None, item=ITEM))


class Gateway:
    """Stands in for the client's wait for events: what the hub reports reaches the waits in place."""

    def __init__(self) -> None:
        self.waits: list[tuple[type, collections.abc.Callable[[object], bool], asyncio.Future[object]]] = []

    async def wait_for(
        self,
        kind: type,
        *,
        timeout: float | None,  # ruff: ignore[async-function-with-timeout]
        predicate: collections.abc.Callable[[object], bool],
    ) -> object:
        wait = (kind, predicate, asyncio.get_running_loop().create_future())
        self.waits.append(wait)
        try:
            async with asyncio.timeout(timeout):
                return await wait[2]
        finally:
            self.waits.remove(wait)

    def report(self, event: object) -> None:
        for kind, accepts, future in list(self.waits):
            if isinstance(event, kind) and accepts(event) and not future.done():
                future.set_result(event)


@pytest.fixture
def gateway(made: Made, monkeypatch: pytest.MonkeyPatch) -> Gateway:
    stand_in = Gateway()
    made.client.wait_for.side_effect = stand_in.wait_for
    monkeypatch.setattr(hub_module, "ECHO", 0.2)
    return stand_in


def echoed(gateway: Gateway, made: Made, order: list[str], *, after: float) -> None:
    reports: list[asyncio.Task[None]] = []

    async def answer(*_: object, nonce: str, **__: object) -> plsremind.Item:
        await asyncio.sleep(after)
        order.append("answered")

        async def later() -> None:
            await asyncio.sleep(0.05)
            order.append("reported")
            gateway.report(plsremind.ItemUpdateEvent(user_id=MIA, actor=None, nonce=nonce, item=ITEM))

        reports.append(asyncio.get_running_loop().create_task(later()))
        return ITEM

    made.client.rest.update_item.side_effect = answer


async def test_a_slow_write_is_still_waited_for(made: Made, gateway: Gateway) -> None:
    order: list[str] = []
    echoed(gateway, made, order, after=0.3)
    await made.hub.set_done(MIA, ITEM.id, done=True)
    order.append("returned")
    assert order == ["answered", "reported", "returned"]
    assert gateway.waits == []


async def test_a_report_that_comes_before_the_answer_is_not_missed(made: Made, gateway: Gateway) -> None:
    async def answer(*_: object, nonce: str, **__: object) -> plsremind.Item:
        gateway.report(plsremind.ItemUpdateEvent(user_id=MIA, actor=None, nonce=nonce, item=ITEM))
        return ITEM

    made.client.rest.update_item.side_effect = answer
    started = asyncio.get_running_loop().time()
    await made.hub.set_done(MIA, ITEM.id, done=True)
    assert asyncio.get_running_loop().time() - started < 0.1
    assert gateway.waits == []


async def test_a_report_that_never_comes_is_waited_for_only_so_long(made: Made, gateway: Gateway) -> None:
    made.client.rest.update_item.return_value = ITEM
    started = asyncio.get_running_loop().time()
    assert await made.hub.set_done(MIA, ITEM.id, done=True) == ITEM
    assert 0.15 < asyncio.get_running_loop().time() - started < 0.5
    await asyncio.sleep(0)
    assert gateway.waits == []


async def test_a_failed_write_leaves_no_wait_behind(made: Made, gateway: Gateway) -> None:
    made.client.rest.update_item.side_effect = plsremind.ServerError(500, "boom", "no")
    with pytest.raises(plsremind.ServerError):
        await made.hub.set_done(MIA, ITEM.id, done=True)
    await asyncio.sleep(0)
    assert gateway.waits == []


async def test_a_write_that_is_called_off_leaves_no_wait_behind(made: Made, gateway: Gateway) -> None:
    order: list[str] = []
    echoed(gateway, made, order, after=0.3)
    writing = asyncio.create_task(made.hub.set_done(MIA, ITEM.id, done=True))
    await asyncio.sleep(0.05)
    writing.cancel()
    with pytest.raises(asyncio.CancelledError):
        await writing
    await asyncio.sleep(0)
    assert gateway.waits == []


async def test_each_kind_of_write_waits_for_its_own_report(made: Made) -> None:
    waited = made.client.wait_for
    await made.hub.add(MIA, title="x")
    assert waited.await_args.args == (plsremind.ItemCreateEvent,)
    await made.hub.delete(MIA, ITEM.id)
    assert waited.await_args.args == (plsremind.ItemDeleteEvent,)
    for write in (
        made.hub.remind(MIA, ITEM.id, things.NOW),
        made.hub.tick(MIA, ITEM.id, things.uid(5001), done=True),
        made.hub.edit(MIA, ITEM.id, title="x", due=None, note=None),
    ):
        await write
        assert waited.await_args.args == (plsremind.ItemUpdateEvent,)


async def test_no_two_writes_share_a_nonce(made: Made) -> None:
    seen: set[str] = set()
    for _ in range(20):
        await made.hub.set_done(MIA, ITEM.id, done=True, origin=555)
        seen.add(nonce(made.client.rest.update_item))
    assert len(seen) == 20
    assert all(len(one) <= 64 and " " not in one for one in seen)
    await made.hub.set_done(MIA, ITEM.id, done=True, origin=2**64 - 1)
    assert len(nonce(made.client.rest.update_item)) <= 64


async def test_a_write_does_not_wait_while_the_gateway_is_away(made: Made) -> None:
    made.client.status = plsremind.ClientStatus.RECONNECTING
    made.client.rest.update_item.return_value = ITEM
    assert await made.hub.set_done(MIA, ITEM.id, done=True) == ITEM
    made.client.wait_for.assert_not_called()


@pytest.mark.parametrize("silence", [TimeoutError(), plsremind.GatewayError("closed")])
async def test_a_report_that_never_comes_does_not_fail_the_write(made: Made, silence: Exception) -> None:
    made.client.wait_for.side_effect = silence
    made.client.rest.update_item.return_value = ITEM
    assert await made.hub.set_done(MIA, ITEM.id, done=True) == ITEM


async def test_a_refused_write_comes_out(made: Made) -> None:
    made.client.rest.update_item.side_effect = plsremind.NotFoundError(404, "not_found", "no")
    with pytest.raises(plsremind.NotFoundError):
        await made.hub.set_done(MIA, ITEM.id, done=True)


@pytest.mark.parametrize(
    ("written", "origin"),
    [("555.4f2a9c01b7de", 555), ("0.4f2a9c01b7de", None), (None, None), ("", None), ("n1", None), ("x.555", None)],
)
def test_the_message_a_change_came_from_is_read_back(written: str | None, origin: int | None) -> None:
    assert hub_module.origin_of(written) == origin


async def test_a_delivery_the_hub_closed_meanwhile_counts_as_confirmed(made: Made) -> None:
    rest = made.client.rest
    rest.acknowledge_delivery.side_effect = plsremind.ConflictError(409, "delivery_closed", "closed")
    await made.hub.delivered(things.uid(9001))
    rest.acknowledge_delivery.assert_awaited_once_with(things.uid(9001), status=plsremind.DeliveryStatus.DELIVERED)
    rest.acknowledge_delivery.reset_mock()
    await made.hub.failed(things.uid(9001), "closed DMs")
    rest.acknowledge_delivery.assert_awaited_once_with(
        things.uid(9001), status=plsremind.DeliveryStatus.FAILED, reason="closed DMs"
    )


async def test_any_other_refusal_of_a_confirmation_comes_out(made: Made) -> None:
    made.client.rest.acknowledge_delivery.side_effect = plsremind.ConflictError(409, "something_else", "no")
    with pytest.raises(plsremind.ConflictError):
        await made.hub.delivered(things.uid(9001))


def test_the_client_asks_for_what_the_bot_needs_and_goes_to_the_gateway_beside_the_hub() -> None:
    config = Config(
        discord_token="d",
        hub_url="http://hub:8080",
        hub_token="plsr_con_x",
        public_url="https://x",
        database=things.DATABASE,
    )
    with mock.patch.object(plsremind, "Client") as client:
        hub_module.connect(config)
    client.assert_called_once_with(
        "http://hub:8080", token="plsr_con_x", intents=hub_module.INTENTS, gateway_url="ws://hub:8080/api/v1/gateway"
    )
    wanted = plsremind.Intents
    assert hub_module.INTENTS == wanted.USERS | wanted.LISTS | wanted.ITEMS | wanted.HISTORY | wanted.DELIVERIES


def test_the_stand_in_answers_everything_the_hub_answers() -> None:
    public = {name for name, _ in inspect.getmembers(Hub, inspect.isfunction) if not name.startswith("_")}
    own = {name for name in public if name in vars(FakeHub)}
    assert public - own == set()


def later(item: plsremind.Item, minutes: int = 1, **changes: object) -> plsremind.Item:
    return msgspec.structs.replace(item, updated_at=item.updated_at + datetime.timedelta(minutes=minutes), **changes)


@pytest.fixture
def away(made: Made) -> Made:
    made.client.status = plsremind.ClientStatus.RECONNECTING
    made.client.cache.get_item.return_value = ITEM
    made.client.cache.get_items.return_value = (ITEM,)
    return made


async def test_a_write_the_gateway_has_not_reported_is_read_back_as_written(away: Made) -> None:
    done = later(ITEM, status=plsremind.ItemStatus.DONE)
    away.client.rest.update_item.return_value = done
    await away.hub.set_done(MIA, ITEM.id, done=True)
    assert away.hub.cached(MIA, ITEM.id) == done
    assert away.hub.open_items(MIA) == ()

    newer = later(ITEM, 2, title="renamed elsewhere")
    away.client.cache.get_item.return_value = newer
    away.client.cache.get_items.return_value = (newer,)
    assert away.hub.cached(MIA, ITEM.id) == newer, "once the cache has caught up, it counts again"
    assert away.hub.open_items(MIA) == (newer,)


async def test_an_edit_the_gateway_has_not_reported_is_shown(away: Made) -> None:
    edited = later(ITEM, title="Final report")
    away.client.rest.update_item.return_value = edited
    await away.hub.edit(MIA, ITEM.id, title="Final report", due=hub_module.UNCHANGED, note=None)
    assert away.hub.cached(MIA, ITEM.id) == edited
    assert away.hub.open_items(MIA) == (edited,)


async def test_a_deletion_the_gateway_has_not_reported_leaves_the_item_out(away: Made) -> None:
    await away.hub.delete(MIA, ITEM.id)
    assert away.hub.cached(MIA, ITEM.id) is None
    assert away.hub.open_items(MIA) == ()
    away.client.cache.get_item.return_value = None
    away.client.cache.get_items.return_value = ()
    assert away.hub.open_items(MIA) == ()


async def test_a_new_item_the_gateway_has_not_reported_is_among_the_open_ones(away: Made) -> None:
    made_now = things.item(7, title="Laundry")
    away.client.rest.create_item.return_value = made_now
    away.client.cache.get_item.side_effect = {ITEM.id: ITEM}.get
    await away.hub.add(MIA, title="Laundry")
    assert {item.id for item in away.hub.open_items(MIA)} == {ITEM.id, made_now.id}


async def test_what_was_written_counts_only_for_its_own_user_and_only_for_a_while(
    away: Made, monkeypatch: pytest.MonkeyPatch
) -> None:
    done = later(ITEM, status=plsremind.ItemStatus.DONE)
    away.client.rest.update_item.return_value = done
    await away.hub.set_done(MIA, ITEM.id, done=True)
    assert away.hub.open_items(BEN) == (ITEM,)
    monkeypatch.setattr(hub_module, "RECENT", 0.0)
    assert away.hub.cached(MIA, ITEM.id) == ITEM


async def test_a_write_the_gateway_reported_needs_nothing_remembered(made: Made, gateway: Gateway) -> None:
    done = later(ITEM, status=plsremind.ItemStatus.DONE)

    async def answer(*_: object, nonce: str, **__: object) -> plsremind.Item:
        gateway.report(plsremind.ItemUpdateEvent(user_id=MIA, actor=None, nonce=nonce, item=done))
        return done

    made.client.rest.update_item.side_effect = answer
    made.client.cache.get_item.return_value = done
    await made.hub.set_done(MIA, ITEM.id, done=True)
    made.client.cache.get_item.return_value = ITEM
    assert made.hub.cached(MIA, ITEM.id) == ITEM


def test_what_a_message_shows_changes_with_the_item_and_with_the_name_of_its_list(made: Made) -> None:
    renamed = msgspec.structs.replace(things.a_list(), updated_at=ITEM.updated_at + datetime.timedelta(hours=1))
    made.client.cache.get_list.return_value = renamed
    assert made.hub.version(ITEM) == renamed.updated_at
    assert made.hub.version(later(ITEM, 120)) == ITEM.updated_at + datetime.timedelta(minutes=120)
    made.client.cache.get_list.return_value = None
    assert made.hub.version(ITEM) == ITEM.updated_at


async def test_an_older_answer_that_arrives_later_does_not_replace_a_newer_write(away: Made) -> None:
    reopened = later(ITEM, 3)
    done = later(ITEM, 2, status=plsremind.ItemStatus.DONE)
    away.client.rest.update_item.return_value = reopened
    await away.hub.set_done(MIA, ITEM.id, done=False)
    away.client.rest.update_item.return_value = done
    await away.hub.set_done(MIA, ITEM.id, done=True)
    assert away.hub.cached(MIA, ITEM.id) == reopened
    assert away.hub.open_items(MIA) == (reopened,)


async def test_a_deletion_is_not_undone_by_an_answer_to_an_earlier_write(away: Made) -> None:
    await away.hub.delete(MIA, ITEM.id)
    away.client.rest.update_item.return_value = later(ITEM, 5)
    await away.hub.set_done(MIA, ITEM.id, done=False)
    assert away.hub.cached(MIA, ITEM.id) is None
    assert away.hub.open_items(MIA) == ()
