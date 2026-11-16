from __future__ import annotations

import collections.abc

import pytest

from tests.support import fakes


@pytest.fixture
async def hub() -> collections.abc.AsyncIterator[fakes.FakeHub]:
    fake = fakes.FakeHub()
    await fake.start()
    try:
        yield fake
    finally:
        await fake.stop()
