from __future__ import annotations

import collections.abc
import json
import os
import secrets
import urllib.request

import pytest

from tests.hub import helpers
from tests.hub import launch


@pytest.fixture(scope="session")
def real_hub() -> collections.abc.Iterator[tuple[launch.LaunchedHub, str]]:
    try:
        hub = launch.launch_hub()
    except launch.MissingToolError as missing:
        if os.environ.get("CI"):
            raise
        pytest.skip(str(missing))
    try:
        password = secrets.token_urlsafe(18)
        setup = urllib.request.Request(  # ruff: ignore[suspicious-url-open-usage]
            f"{hub.url}/api/v1/setup",
            data=json.dumps({"username": "admin", "password": password}).encode(),
            headers={"Content-Type": "application/json"},
            method="POST",
        )
        with urllib.request.urlopen(setup, timeout=10) as response:  # ruff: ignore[suspicious-url-open-usage]
            assert response.status == 201
        yield hub, password
    finally:
        hub.stop()


@pytest.fixture
async def kit(real_hub: tuple[launch.LaunchedHub, str]) -> collections.abc.AsyncIterator[helpers.Kit]:
    made = helpers.Kit(*real_hub)
    try:
        yield made
    finally:
        await made.close()
