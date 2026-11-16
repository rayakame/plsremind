from __future__ import annotations

import asyncio
import sys
import threading

import plsremind
from tests.support import fakes

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


server = threading.Thread(target=lambda: asyncio.run(serve()), daemon=True)
server.start()
if not started.wait(10):
    sys.exit("The hub did not start.")
client = plsremind.Client(address[0], token="plsr_pat_x", intents=plsremind.Intents.DELIVERIES)


@client.listen()
async def on_ready(event: plsremind.ReadyEvent) -> None:
    print("ready", len(event.users), flush=True)


client.run()
# A daemon thread still printing while the interpreter shuts down aborts the process.
server.join(10)
print("stopped", client.status.value, flush=True)
