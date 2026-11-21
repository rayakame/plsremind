from __future__ import annotations

import typing

from plsremind_discord.commands import deadline
from plsremind_discord.commands import done
from plsremind_discord.commands import message
from plsremind_discord.commands import remind
from plsremind_discord.commands import todos

LOADERS: typing.Final = (remind.loader, deadline.loader, todos.loader, done.loader, message.loader)
