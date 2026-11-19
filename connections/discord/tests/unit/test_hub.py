from __future__ import annotations

import datetime
import inspect
import typing
from unittest import mock

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


async def test_writes_name_the_user_and_carry_the_nonce(made: Made) -> None:
    rest = made.client.rest
    at = things.NOW + datetime.timedelta(hours=1)
    await made.hub.schedule(MIA, ITEM.id)
    rest.get_item_schedule.assert_awaited_once_with(ITEM.id, user=MIA)
    await made.hub.remind(MIA, ITEM.id, at, nonce="n1")
    rest.create_reminder.assert_awaited_once_with(ITEM.id, at=at, user=MIA, nonce="n1")
    await made.hub.set_done(MIA, ITEM.id, done=True, nonce="n2")
    rest.update_item.assert_awaited_once_with(ITEM.id, status=plsremind.ItemStatus.DONE, user=MIA, nonce="n2")
    rest.update_item.reset_mock()
    await made.hub.set_done(MIA, ITEM.id, done=False)
    rest.update_item.assert_awaited_once_with(ITEM.id, status=plsremind.ItemStatus.OPEN, user=MIA, nonce=None)
    await made.hub.delete(MIA, ITEM.id, nonce="n3")
    rest.delete_item.assert_awaited_once_with(ITEM.id, user=MIA, nonce="n3")
    await made.hub.tick(MIA, ITEM.id, things.uid(5001), done=True, nonce="n4")
    rest.update_subitem.assert_awaited_once_with(ITEM.id, things.uid(5001), done=True, user=MIA, nonce="n4")


async def test_a_new_item_sends_only_what_was_given(made: Made) -> None:
    rest = made.client.rest
    await made.hub.add(MIA, title="Laundry")
    rest.create_item.assert_awaited_once_with(title="Laundry", user=MIA, nonce=None)
    rest.create_item.reset_mock()
    due = things.NOW + datetime.timedelta(days=1)
    await made.hub.add(
        MIA,
        title="Report",
        note="see wiki",
        list_id=things.WORK,
        priority=plsremind.Priority.HIGH,
        due_at=due,
        nonce="n",
    )
    rest.create_item.assert_awaited_once_with(
        title="Report",
        note="see wiki",
        list_id=things.WORK,
        priority=plsremind.Priority.HIGH,
        due_at=due,
        user=MIA,
        nonce="n",
    )
    rest.create_item.reset_mock()
    await made.hub.add(MIA, title="Party", due_date=datetime.date(2026, 12, 24))
    rest.create_item.assert_awaited_once_with(title="Party", due_date=datetime.date(2026, 12, 24), user=MIA, nonce=None)


async def test_an_edit_sends_the_deadline_the_way_it_was_meant(made: Made) -> None:
    rest = made.client.rest
    at = things.NOW + datetime.timedelta(days=1)
    await made.hub.edit(MIA, ITEM.id, title="A", due=Moment(at=at), note="n")
    rest.update_item.assert_awaited_once_with(ITEM.id, title="A", due_at=at, note="n", user=MIA, nonce=None)
    rest.update_item.reset_mock()
    await made.hub.edit(MIA, ITEM.id, title="A", due=Day(date=at.date()), note=None, nonce="x")
    rest.update_item.assert_awaited_once_with(ITEM.id, title="A", due_date=at.date(), user=MIA, nonce="x")
    rest.update_item.reset_mock()
    await made.hub.edit(MIA, ITEM.id, title="A", due=None, note="")
    rest.update_item.assert_awaited_once_with(ITEM.id, title="A", due_at=None, note="", user=MIA, nonce=None)


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
