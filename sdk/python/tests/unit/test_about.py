from __future__ import annotations

import pathlib
import tomllib

import plsremind


def test_version_matches_the_project_file() -> None:
    project = tomllib.loads(pathlib.Path("pyproject.toml").read_text(encoding="utf-8"))["project"]
    assert plsremind.__version__ == project["version"]
