from __future__ import annotations

import gc
import tracemalloc
import typing

import msgspec

from plsremind import models
from plsremind.enums import Intents
from plsremind.internal import wire
from plsremind.internal.store import CacheStore
from tests.support import fixtures

ITEMS = 5000
CHUNK = 500
# Measured on CPython 3.12 and 3.15: 2.3 MB, about 460 bytes for an item. The limit leaves a quarter of room.
LIMIT = 2_900_000


def chunk(index: int) -> msgspec.Raw:
    items = [
        {**fixtures.PLAIN_ITEM, "id": f"01930000-0000-7000-8000-{number:012x}", "title": f"Item number {number}"}
        for number in range(index * CHUNK, (index + 1) * CHUNK)
    ]
    first: dict[str, typing.Any] = (
        {"user": fixtures.USER, "lists": [fixtures.LIST], "plans": [fixtures.REMINDER_PLAN]} if index == 0 else {}
    )
    payload = {"user_id": fixtures.USER_ID, "chunk": {"index": index, "count": ITEMS // CHUNK}, **first, "items": items}
    return msgspec.Raw(fixtures.raw(payload))


def synced() -> CacheStore:
    cache = CacheStore()
    cache.begin(msgspec.convert(fixtures.PRINCIPAL, models.Principal), [models.ID(fixtures.USER_ID)], Intents.ALL)
    for index in range(ITEMS // CHUNK):
        event = wire.decode_event("user.sync", chunk(index), connection=False)
        assert event is not None
        cache.apply(event)
    return cache


def test_a_cache_of_5000_items_stays_small() -> None:
    synced()
    gc.collect()
    tracemalloc.start()
    try:
        cache = synced()
        gc.collect()
        used, _ = tracemalloc.get_traced_memory()
    finally:
        tracemalloc.stop()
    assert len(cache.get_items()) == ITEMS
    assert used < LIMIT, f"{used} bytes for {ITEMS} items"


def test_reading_allocates_nothing_per_item() -> None:
    cache = synced()
    first = cache.get_items()
    tracemalloc.start()
    try:
        before, _ = tracemalloc.get_traced_memory()
        for _ in range(100):
            assert cache.get_items() is first
            assert cache.get_item(first[0].id) is first[0]
        after, _ = tracemalloc.get_traced_memory()
    finally:
        tracemalloc.stop()
    assert after - before < 2000
