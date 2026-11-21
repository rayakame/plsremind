from __future__ import annotations

import datetime
import pathlib

import pytest

from plsremind_discord.clock import Clock
from plsremind_discord.config import Config
from plsremind_discord.config import ConfigError

FULL = {
    "DISCORD_TOKEN": "discord-secret",
    "PLSREMIND_URL": "http://hub:8080/",
    "PLSREMIND_TOKEN": "plsr_con_secret",
    "PLSREMIND_PUBLIC_URL": "https://todo.example.com/",
}


def test_a_complete_environment_is_read() -> None:
    config = Config.from_env(FULL)
    assert config.discord_token == "discord-secret"
    assert config.hub_url == "http://hub:8080"
    assert config.hub_token == "plsr_con_secret"
    assert config.public_url == "https://todo.example.com"
    assert config.database == pathlib.Path("discord.db")


def test_the_database_can_live_elsewhere() -> None:
    config = Config.from_env({**FULL, "PLSREMIND_DISCORD_DB": "/data/discord.db"})
    assert config.database == pathlib.Path("/data/discord.db")


@pytest.mark.parametrize(
    ("hub", "gateway"),
    [
        ("http://hub:8080", "ws://hub:8080/api/v1/gateway"),
        ("https://todo.example.com/", "wss://todo.example.com/api/v1/gateway"),
    ],
)
def test_the_gateway_is_reached_where_the_hub_is(hub: str, gateway: str) -> None:
    assert Config.from_env({**FULL, "PLSREMIND_URL": hub}).gateway_url == gateway


def test_every_missing_variable_is_named() -> None:
    with pytest.raises(ConfigError) as caught:
        Config.from_env({})
    for name in FULL:
        assert name in str(caught.value)
    assert "PLSREMIND_DISCORD_DB" not in str(caught.value)


@pytest.mark.parametrize("empty", ["", "   "])
def test_an_empty_value_counts_as_missing(empty: str) -> None:
    with pytest.raises(ConfigError, match="DISCORD_TOKEN"):
        Config.from_env({**FULL, "DISCORD_TOKEN": empty})


@pytest.mark.parametrize("name", ["PLSREMIND_URL", "PLSREMIND_PUBLIC_URL"])
def test_an_address_without_a_scheme_is_refused(name: str) -> None:
    with pytest.raises(ConfigError, match=name):
        Config.from_env({**FULL, name: "hub:8080"})


def test_the_tokens_stay_out_of_what_is_printed() -> None:
    shown = repr(Config.from_env(FULL)) + str(Config.from_env(FULL))
    assert "discord-secret" not in shown
    assert "plsr_con_secret" not in shown


def test_the_clock_tells_the_time_in_utc() -> None:
    now = Clock().now()
    assert now.tzinfo is datetime.UTC
    assert abs(datetime.datetime.now(datetime.UTC) - now) < datetime.timedelta(seconds=5)
