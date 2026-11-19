from __future__ import annotations

import collections.abc
import importlib.util
import json
import os
import pathlib
import secrets
import sys
import typing
import urllib.request

import pytest

from tests.hub import kit as kit_


def _launcher() -> typing.Any:  # ruff: ignore[any-type]
    # The SDK's tests already know how to build and start a hub; it is loaded from there instead of copied.
    path = pathlib.Path(__file__).resolve().parents[4] / "sdk" / "python" / "tests" / "hub" / "launch.py"
    spec = importlib.util.spec_from_file_location("plsremind_sdk_hub_launch", path)
    assert spec is not None
    assert spec.loader is not None
    module = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = module
    spec.loader.exec_module(module)
    return module


@pytest.fixture(scope="session")
def real_hub() -> collections.abc.Iterator[tuple[kit_.Launched, str]]:
    launch = _launcher()
    try:
        hub: kit_.Launched = launch.launch_hub()
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
async def kit(real_hub: tuple[kit_.Launched, str]) -> collections.abc.AsyncIterator[kit_.Kit]:
    made = kit_.Kit(*real_hub)
    try:
        yield made
    finally:
        await made.close()
