from __future__ import annotations

import collections.abc
import typing

import aiohttp.web
import prometheus_client

from plsremind_discord.about import __version__


class Metrics:
    """The bot's own registry and what is registered in it."""

    def __init__(self) -> None:
        self.registry: typing.Final = prometheus_client.CollectorRegistry()
        prometheus_client.ProcessCollector(registry=self.registry)
        prometheus_client.PlatformCollector(registry=self.registry)
        info = prometheus_client.Gauge(
            "plsremind_discord_build_info",
            "The version of the running bot. Always 1.",
            ["version"],
            registry=self.registry,
        )
        info.labels(version=__version__).set(1)

    def render(self) -> bytes:
        return prometheus_client.generate_latest(self.registry)


async def serve(
    metrics: Metrics, address: tuple[str, int], ready: collections.abc.Callable[[], bool]
) -> aiohttp.web.AppRunner:
    """Serve /metrics, /healthz and /readyz on `address`. An address that cannot be bound raises at once."""

    async def scraped(_: aiohttp.web.Request) -> aiohttp.web.Response:  # ruff: ignore[unused-async]
        return aiohttp.web.Response(
            body=metrics.render(), headers={"Content-Type": prometheus_client.CONTENT_TYPE_LATEST}
        )

    async def alive(_: aiohttp.web.Request) -> aiohttp.web.Response:  # ruff: ignore[unused-async]
        return aiohttp.web.Response(text="ok")

    async def readiness(_: aiohttp.web.Request) -> aiohttp.web.Response:  # ruff: ignore[unused-async]
        if ready():
            return aiohttp.web.Response(text="ok")
        return aiohttp.web.Response(text="unavailable", status=503)

    app = aiohttp.web.Application()
    app.router.add_get("/metrics", scraped)
    app.router.add_get("/healthz", alive)
    app.router.add_get("/readyz", readiness)
    runner = aiohttp.web.AppRunner(app, access_log=None)
    await runner.setup()
    try:
        await aiohttp.web.TCPSite(runner, *address).start()
    except BaseException:
        await runner.cleanup()
        raise
    return runner
