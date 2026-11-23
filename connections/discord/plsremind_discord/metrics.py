from __future__ import annotations

import collections.abc
import math
import typing

import aiohttp.web
import prometheus_client
import prometheus_client.metrics_core
import prometheus_client.registry

from plsremind_discord.about import __version__


class Metrics:
    """The bot's own registry and what is registered in it."""

    def __init__(self) -> None:
        prometheus_client.disable_created_metrics()
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
        self.attempts: typing.Final = self._counter(
            "plsremind_discord_delivery_attempts_total",
            "Tries to send a reminder, by result.",
            result=("sent", "closed", "refused", "no_account", "passing", "unreadable", "error"),
        )
        self.deliveries_waiting: typing.Final = prometheus_client.Gauge(
            "plsremind_discord_deliveries_waiting", "Deliveries that are tried again later.", registry=self.registry
        )
        self.dm_channels_opened: typing.Final = prometheus_client.Counter(
            "plsremind_discord_dm_channels_opened_total", "DM channels the bot had to open.", registry=self.registry
        )
        kinds = ("command", "component", "modal", "autocomplete")
        self.interactions: typing.Final = self._counter(
            "plsremind_discord_interactions_total", "Interactions from Discord, by kind.", kind=kinds
        )
        self.interaction_failures: typing.Final = self._counter(
            "plsremind_discord_interaction_failures_total",
            "Interactions that failed unexpectedly, by kind.",
            kind=kinds,
        )
        self.redraws: typing.Final = self._counter(
            "plsremind_discord_redraws_total",
            "Reminder messages drawn again after a change, by result.",
            result=("redrawn", "forgotten", "failed"),
        )
        self.echo_timeouts: typing.Final = prometheus_client.Counter(
            "plsremind_discord_write_echo_timeouts_total",
            "Writes of the bot whose event did not come back in time.",
            registry=self.registry,
        )

    def watch(
        self, *, connected: collections.abc.Callable[[], bool], latency: collections.abc.Callable[[], float]
    ) -> None:
        """Report the hub connection and Discord's heartbeat latency at each scrape."""
        self.registry.register(_Connection(connected, latency))

    def render(self) -> bytes:
        return prometheus_client.generate_latest(self.registry)

    def _counter(self, name: str, documentation: str, **labels: tuple[str, ...]) -> prometheus_client.Counter:
        ((label, values),) = labels.items()
        counter = prometheus_client.Counter(name, documentation, [label], registry=self.registry)
        # Present from the start, so that a rate over a value that has not happened yet is 0, not empty.
        for value in values:
            counter.labels(value)
        return counter


class _Connection(prometheus_client.registry.Collector):
    def __init__(
        self, connected: collections.abc.Callable[[], bool], latency: collections.abc.Callable[[], float]
    ) -> None:
        self._connected: typing.Final = connected
        self._latency: typing.Final = latency

    def collect(self) -> collections.abc.Iterator[prometheus_client.metrics_core.Metric]:
        yield prometheus_client.metrics_core.GaugeMetricFamily(
            "plsremind_discord_hub_connected",
            "1 while the bot is connected to the hub.",
            value=float(self._connected()),
        )
        latency = self._latency()
        # hikari reports NaN until the first heartbeat came back.
        if not math.isnan(latency):
            yield prometheus_client.metrics_core.GaugeMetricFamily(
                "plsremind_discord_discord_latency_seconds", "Heartbeat latency to Discord.", value=latency
            )


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
