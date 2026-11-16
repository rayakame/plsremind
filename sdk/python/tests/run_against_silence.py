from __future__ import annotations

import socket

import plsremind

# A port that accepts connections and never answers, like a hub behind a dead proxy.
silent = socket.socket()
silent.bind(("127.0.0.1", 0))
silent.listen()
client = plsremind.Client(
    f"http://127.0.0.1:{silent.getsockname()[1]}", token="plsr_pat_x", intents=plsremind.Intents.DELIVERIES
)
print("connecting", flush=True)
client.run()
print("stopped", client.status.value, flush=True)
