from __future__ import annotations

import pathlib
import tomllib

from plsremind_discord import about


def test_version_matches_the_project_file() -> None:
    project = tomllib.loads(pathlib.Path("pyproject.toml").read_text(encoding="utf-8"))["project"]
    assert about.__version__ == project["version"]
