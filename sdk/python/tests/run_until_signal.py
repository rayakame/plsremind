"""Runs a client against a fake hub until a signal ends it. Started by test_client.py."""

from __future__ import annotations

import asyncio
import threading

import plsremind
from tests import fakes

started = threading.Event()
address: list[str] = []


async def serve() -> None:
    hub = fakes.FakeHub()
    await hub.start()
    address.append(hub.url)
    started.set()
    connection = await hub.gateway.connection(timeout=10)
    await connection.hello()
    await connection.receive()
    await connection.ready(users=[])
    print("hub saw close", await connection.closed(timeout=30), flush=True)


threading.Thread(target=lambda: asyncio.run(serve()), daemon=True).start()
started.wait(10)
client = plsremind.Client(address[0], token="plsr_pat_x", intents=plsremind.Intents.DELIVERIES)


@client.listen()
async def on_ready(event: plsremind.ReadyEvent) -> None:
    print("ready", len(event.users), flush=True)


client.run()
print("stopped", client.status.value, flush=True)
