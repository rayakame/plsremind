from __future__ import annotations

import importlib.metadata

import plsremind


def test_the_installed_version_is_the_one_in_about() -> None:
    assert importlib.metadata.version("plsremind") == plsremind.__version__
