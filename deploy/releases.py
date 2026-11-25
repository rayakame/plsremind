"""Finds the newest server release that has the given files fully uploaded.

python3 deploy/releases.py [file ...]   prints its tag, or nothing when there is none

Without files, it is the newest server release. Reads $GITHUB_REPOSITORY and needs gh.
"""

from __future__ import annotations

import json
import os
import re
import subprocess
import sys

SERVER = re.compile(r"v[0-9]")


def newest(releases: list[dict], files: list[str]) -> str | None:
    def finished(release: dict) -> bool:
        uploaded = {a["name"] for a in release["assets"] if a["state"] == "uploaded"}
        return (
            not release["draft"]
            and not release["prerelease"]
            and SERVER.match(release["tag_name"]) is not None
            and uploaded.issuperset(files)
        )

    found = [release for release in releases if finished(release)]
    if not found:
        return None
    # The list's order is not documented; created_at is what GitHub itself orders "latest" by.
    return max(found, key=lambda release: release["created_at"])["tag_name"]


def fetch(repository: str) -> list[dict]:
    pages = subprocess.run(
        [
            "gh",
            "api",
            "--paginate",
            "--slurp",
            f"repos/{repository}/releases?per_page=100",
        ],
        check=True,
        stdout=subprocess.PIPE,
        text=True,
    ).stdout
    return [release for page in json.loads(pages) for release in page]


def main(args: list[str]) -> int:
    if any(arg.startswith("-") for arg in args):
        print(__doc__, file=sys.stderr)
        return 2
    try:
        listed = fetch(os.environ["GITHUB_REPOSITORY"])
    except subprocess.CalledProcessError as error:
        return error.returncode
    tag = newest(listed, args)
    if tag:
        print(tag)
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
