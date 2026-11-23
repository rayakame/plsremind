from __future__ import annotations

import socket

import aiohttp
import plsremind
import pytest

from plsremind_discord import bot
from plsremind_discord.about import __version__
from plsremind_discord.metrics import Metrics
from plsremind_discord.metrics import serve


def free_port() -> int:
    with socket.socket() as probe:
        probe.bind(("127.0.0.1", 0))
        return int(probe.getsockname()[1])


async def fetch(port: int, path: str) -> tuple[int, str]:
    async with aiohttp.ClientSession() as session, session.get(f"http://127.0.0.1:{port}{path}") as response:
        return response.status, await response.text()


def test_build_info_names_the_version() -> None:
    assert f'plsremind_discord_build_info{{version="{__version__}"}} 1.0' in Metrics().render().decode()


async def test_the_server_answers_metrics_health_and_readiness() -> None:
    port, ready = free_port(), False
    runner = await serve(Metrics(), ("127.0.0.1", port), lambda: ready)
    try:
        status, body = await fetch(port, "/metrics")
        assert status == 200
        assert "plsremind_discord_build_info" in body
        assert await fetch(port, "/healthz") == (200, "ok")
        assert await fetch(port, "/readyz") == (503, "unavailable")
        ready = True
        assert await fetch(port, "/readyz") == (200, "ok")
        assert (await fetch(port, "/elsewhere"))[0] == 404
    finally:
        await runner.cleanup()


async def test_an_address_already_taken_is_an_error() -> None:
    with socket.socket() as taken:
        taken.bind(("127.0.0.1", 0))
        taken.listen()
        with pytest.raises(OSError, match=r"(?i)in use"):
            await serve(Metrics(), ("127.0.0.1", int(taken.getsockname()[1])), lambda: True)


async def test_the_address_is_free_again_after_cleanup() -> None:
    port = free_port()
    runner = await serve(Metrics(), ("127.0.0.1", port), lambda: True)
    await runner.cleanup()
    again = await serve(Metrics(), ("127.0.0.1", port), lambda: True)
    await again.cleanup()


@pytest.mark.parametrize(
    ("gate_open", "status", "discord_alive", "expected"),
    [
        (True, plsremind.ClientStatus.CONNECTED, True, True),
        (False, plsremind.ClientStatus.CONNECTED, True, False),
        (True, plsremind.ClientStatus.RECONNECTING, True, False),
        (True, plsremind.ClientStatus.CONNECTED, False, False),
    ],
)
def test_the_bot_is_ready_only_when_both_sides_are_there(
    *, gate_open: bool, status: plsremind.ClientStatus, discord_alive: bool, expected: bool
) -> None:
    gate = bot.Gate()
    if gate_open:
        gate.open()
    assert bot.ready(gate, status, discord_alive=discord_alive) is expected
