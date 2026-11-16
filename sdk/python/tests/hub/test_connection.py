from __future__ import annotations

import asyncio
import datetime

import pytest

import plsremind
from plsremind import Intents
from tests.hub import helpers

pytestmark = pytest.mark.hub
until = helpers.until


async def test_a_connection_sees_every_linked_user_and_acts_for_each(kit: helpers.Kit) -> None:
    provider = helpers.unique("chat")
    mia = await kit.linked(provider, "mia")
    tom = await kit.linked(provider, "tom")
    await kit.account("unlinked")
    await mia.rest.create_item(title="mia's item")
    bot_id, bot_token = await kit.connection(provider)
    client = kit.client(bot_token, Intents.USERS | Intents.ITEMS).client
    await client.start()

    principal = client.principal
    assert principal is not None
    assert principal.kind is plsremind.CredentialKind.CONNECTION
    assert principal.id == bot_id
    assert principal.user_id is None
    assert principal.connection is not None
    assert principal.connection.provider == provider
    assert sorted(client.cache.get_users()) == sorted([mia.id, tom.id])
    with pytest.raises(TypeError, match="which user"):
        client.cache.get_items()
    profile = client.cache.get_profile(mia.id)
    assert isinstance(profile, plsremind.ConnectionUser)
    assert profile.display_name == "mia"
    assert profile.external_ids == (mia.external_id,)
    assert profile.remind is True
    assert [item.title for item in client.cache.get_items(mia.id)] == ["mia's item"]

    found = await client.rest.lookup_user(tom.external_id)
    assert found.id == tom.id
    created = await client.rest.create_item(title="from the bot", user=tom.id)
    await until(lambda: client.cache.get_item(created.id) is not None)
    assert [item.id for item in client.cache.get_items(tom.id)] == [created.id]
    assert created.id not in {item.id for item in client.cache.get_items(mia.id)}
    assert (await tom.rest.get_item(created.id)).title == "from the bot"


async def test_a_user_who_switches_the_connection_off_is_gone_and_comes_back(kit: helpers.Kit) -> None:
    provider = helpers.unique("chat")
    mia = await kit.linked(provider, "mia")
    bot_id, bot_token = await kit.connection(provider)
    client = kit.client(bot_token, Intents.USERS | Intents.ITEMS).client
    await client.start()
    seen: list[str] = []

    @client.listen()
    async def on_gone(event: plsremind.UserGoneEvent) -> None:
        seen.append(f"gone {event.user_id}")

    @client.listen()
    async def on_ready(event: plsremind.UserReadyEvent) -> None:
        seen.append(f"ready {event.user_id}")

    await mia.rest.set_connection_grant(bot_id, enabled=False)
    await until(lambda: len(seen) == 1)
    assert client.cache.get_users() == ()
    assert client.cache.get_profile(mia.id) is None
    await mia.rest.set_connection_grant(bot_id, enabled=True)
    await until(lambda: len(seen) == 2)
    assert seen == [f"gone {mia.id}", f"ready {mia.id}"]
    assert client.cache.get_profile(mia.id) is not None
    assert on_gone is not on_ready


async def test_a_connection_is_told_when_a_user_stops_wanting_reminders(kit: helpers.Kit) -> None:
    provider = helpers.unique("chat")
    mia = await kit.linked(provider, "mia")
    bot_id, bot_token = await kit.connection(provider)
    client = kit.client(bot_token, Intents.USERS).client
    await client.start()
    waiting = asyncio.create_task(client.wait_for(plsremind.UserUpdateEvent, timeout=10))
    await mia.rest.set_connection_grant(bot_id, remind=False)
    event = await waiting
    assert event.actor is None
    assert isinstance(event.user, plsremind.ConnectionUser)
    assert event.user.remind is False
    assert isinstance(event.old_user, plsremind.ConnectionUser)
    assert event.old_user.remind is True


async def test_an_intent_the_admin_does_not_allow_ends_the_client_with_4014(kit: helpers.Kit) -> None:
    _, bot_token = await kit.connection(helpers.unique("chat"), ["users", "deliveries"])
    client = kit.client(bot_token, Intents.USERS | Intents.ITEMS).client
    with pytest.raises(plsremind.GatewayError) as caught:
        await client.start()
    assert caught.value.code == 4014
    assert client.status is plsremind.ClientStatus.CLOSED


async def test_a_connection_is_told_about_a_reminder_with_the_address_to_use(kit: helpers.Kit) -> None:
    provider = helpers.unique("chat")
    mia = await kit.linked(provider, "mia")
    await mia.rest.update_user(locale="de")
    _, bot_token = await kit.connection(provider)
    client = kit.client(bot_token, Intents.DELIVERIES).client
    await client.start()
    waiting = asyncio.create_task(client.wait_for(plsremind.DeliveryCreateEvent, timeout=30))
    item = await mia.rest.create_item(title="ping me")
    await mia.rest.create_reminder(item.id, at=datetime.datetime.now(datetime.UTC) + datetime.timedelta(seconds=2))
    user = (await waiting).delivery.user
    assert (user.id, user.locale, user.external_ids) == (mia.id, "de", (mia.external_id,))
    assert str(user.timezone) == "Europe/Berlin"


async def test_a_connection_acknowledges_and_the_log_names_it(kit: helpers.Kit) -> None:
    provider = helpers.unique("chat")
    mia = await kit.linked(provider, "mia")
    bot_id, bot_token = await kit.connection(provider)
    client = kit.client(bot_token, Intents.DELIVERIES).client
    await client.start()
    waiting = asyncio.create_task(client.wait_for(plsremind.DeliveryCreateEvent, timeout=30))
    item = await mia.rest.create_item(title="ping me")
    await mia.rest.create_reminder(item.id, at=datetime.datetime.now(datetime.UTC) + datetime.timedelta(seconds=2))
    delivery = (await waiting).delivery
    assert [open_one.id for open_one in await client.rest.list_deliveries()] == [delivery.id]
    result = await client.rest.acknowledge_delivery(delivery.id, status=plsremind.DeliveryStatus.DELIVERED)
    assert result.status is plsremind.DeliveryStatus.DELIVERED
    assert await client.rest.list_deliveries() == ()
    entry = (await mia.rest.list_reminder_log(item_id=item.id)).data[0].deliveries[0]
    assert (entry.channel.kind, entry.channel.id) == (plsremind.ChannelKind.CONNECTION, bot_id)
    assert entry.status is plsremind.DeliveryStatus.DELIVERED


async def test_a_connection_resumes_after_a_drop_and_misses_nothing(kit: helpers.Kit) -> None:
    provider = helpers.unique("chat")
    mia = await kit.linked(provider, "mia")
    tom = await kit.linked(provider, "tom")
    _, bot_token = await kit.connection(provider)
    tapped = kit.client(bot_token, Intents.USERS | Intents.ITEMS)
    client = tapped.client
    await client.start()
    resumed = asyncio.create_task(client.wait_for(plsremind.ResumedEvent, timeout=20))
    disconnected = asyncio.create_task(client.wait_for(plsremind.DisconnectedEvent, timeout=10))
    await asyncio.sleep(0)
    tapped.tap.drop_current()
    await disconnected
    first = await mia.rest.create_item(title="for mia, while away")
    second = await tom.rest.create_item(title="for tom, while away")
    await resumed
    await until(lambda: client.cache.get_item(first.id) and client.cache.get_item(second.id))
    assert [item.id for item in client.cache.get_items(mia.id)] == [first.id]
    assert [item.id for item in client.cache.get_items(tom.id)] == [second.id]
    assert len(tapped.tap.sockets) == 2


async def test_a_connection_identifies_again_after_a_hub_restart(kit: helpers.Kit) -> None:
    provider = helpers.unique("chat")
    mia = await kit.linked(provider, "mia")
    before = await mia.rest.create_item(title="before the restart")
    _, bot_token = await kit.connection(provider)
    client = kit.client(bot_token, Intents.USERS | Intents.ITEMS).client
    await client.start()
    ready = asyncio.create_task(client.wait_for(plsremind.ReadyEvent, timeout=40))
    await asyncio.sleep(0)
    await asyncio.to_thread(kit.hub.restart)
    during = await mia.rest.create_item(title="during the restart")
    assert (await ready).users == (mia.id,)
    await until(lambda: client.cache.get_item(during.id), timeout=15)
    assert sorted(item.id for item in client.cache.get_items(mia.id)) == sorted([before.id, during.id])
    assert client.status is plsremind.ClientStatus.CONNECTED
