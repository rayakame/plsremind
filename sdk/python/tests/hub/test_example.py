from __future__ import annotations

import asyncio
import datetime
import os
import pathlib
import sys

import pytest

import plsremind
from tests.hub import helpers

pytestmark = pytest.mark.hub
ROOT = pathlib.Path(__file__).parents[2]


async def test_the_example_adapter_acknowledges_a_reminder_and_exits(kit: helpers.Kit) -> None:
    mia = await kit.account("mia", remind=True)
    adapter = await asyncio.create_subprocess_exec(
        sys.executable,
        "examples/remind.py",
        cwd=ROOT,
        env={**os.environ, "PLSREMIND_URL": kit.hub.url, "PLSREMIND_TOKEN": mia.token, "PLSREMIND_ONCE": "1"},
        stdout=asyncio.subprocess.PIPE,
        stderr=asyncio.subprocess.STDOUT,
    )
    assert adapter.stdout is not None
    try:
        first = await asyncio.wait_for(adapter.stdout.readline(), 20)
        assert first.decode().strip() == "connected, waiting for reminders"
        item = await mia.rest.create_item(title="example reminder")
        soon = datetime.datetime.now(datetime.UTC) + datetime.timedelta(seconds=2)
        await mia.rest.create_reminder(item.id, at=soon)
        output, _ = await asyncio.wait_for(adapter.communicate(), 40)
    finally:
        if adapter.returncode is None:
            adapter.kill()
            await adapter.wait()
    assert 'mia: "example reminder" you asked to be reminded' in output.decode()
    assert adapter.returncode == 0, output.decode()
    log = await mia.rest.list_reminder_log(item_id=item.id)
    assert log.data[0].deliveries[0].status is plsremind.DeliveryStatus.DELIVERED


async def test_the_example_says_what_it_needs() -> None:
    environment = {key: value for key, value in os.environ.items() if not key.startswith("PLSREMIND_")}
    adapter = await asyncio.create_subprocess_exec(
        sys.executable,
        "examples/remind.py",
        cwd=ROOT,
        env=environment,
        stdout=asyncio.subprocess.PIPE,
        stderr=asyncio.subprocess.STDOUT,
    )
    output, _ = await asyncio.wait_for(adapter.communicate(), 20)
    assert adapter.returncode == 1
    assert "PLSREMIND_URL" in output.decode()
