from __future__ import annotations

import collections.abc
import pathlib
import typing

import hikari
import msgspec

_REQUIRED: typing.Final = ("DISCORD_TOKEN", "PLSREMIND_URL", "PLSREMIND_TOKEN", "PLSREMIND_PUBLIC_URL")
_ADDRESSES: typing.Final = ("PLSREMIND_URL", "PLSREMIND_PUBLIC_URL")
_DATABASE: typing.Final = "PLSREMIND_DISCORD_DB"


class ConfigError(Exception):
    pass


class Config(msgspec.Struct, frozen=True, kw_only=True):
    discord_token: str
    hub_url: str
    hub_token: str
    public_url: str
    database: pathlib.Path

    @classmethod
    def from_env(cls, env: collections.abc.Mapping[str, str]) -> Config:
        values = {name: env.get(name, "").strip() for name in (*_REQUIRED, _DATABASE)}
        missing = [name for name in _REQUIRED if not values[name]]
        if missing:
            msg = f"These variables must be set: {', '.join(missing)}."
            raise ConfigError(msg)
        for name in _ADDRESSES:
            if not values[name].startswith(("http://", "https://")):
                msg = f"{name} must be an address that starts with http:// or https://."
                raise ConfigError(msg)
        try:
            hikari.applications.get_token_id(values["DISCORD_TOKEN"])
        except ValueError:
            msg = "DISCORD_TOKEN must be the token of a Discord bot."
            raise ConfigError(msg) from None
        return cls(
            discord_token=values["DISCORD_TOKEN"],
            hub_url=values["PLSREMIND_URL"].rstrip("/"),
            hub_token=values["PLSREMIND_TOKEN"],
            public_url=values["PLSREMIND_PUBLIC_URL"].rstrip("/"),
            database=pathlib.Path(values[_DATABASE] or "discord.db"),
        )

    @property
    def bot_id(self) -> int:
        return int(hikari.applications.get_token_id(self.discord_token))

    @property
    def gateway_url(self) -> str:
        return "ws" + self.hub_url.removeprefix("http") + "/api/v1/gateway"

    @typing.override
    def __repr__(self) -> str:
        return f"Config(hub_url={self.hub_url!r}, public_url={self.public_url!r}, database={str(self.database)!r})"
