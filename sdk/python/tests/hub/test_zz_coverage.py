from __future__ import annotations

import pytest

from plsremind import events
from tests.hub import helpers

pytestmark = pytest.mark.hub


def test_every_event_class_was_decoded_from_the_real_hub() -> None:
    """Runs last: the other hub tests have by now made the hub send one of everything."""
    missing = set(events.EVENT_CLASSES.values()) - helpers.seen_event_types
    assert missing == set()
