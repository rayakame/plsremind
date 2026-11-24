from __future__ import annotations

import importlib.metadata

from plsremind_discord import about


def test_the_installed_version_is_the_one_in_about() -> None:
    assert importlib.metadata.version("plsremind-discord") == about.__version__
