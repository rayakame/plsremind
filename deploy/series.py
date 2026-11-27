"""Keeps the image tags in the deploy files and docs at the series of the server's release.

python3 deploy/series.py           prints the series
python3 deploy/series.py --check   lists every reference to another series and exits 1
python3 deploy/series.py --write   rewrites them
"""

from __future__ import annotations

import json
import pathlib
import re
import sys

ROOT = pathlib.Path(__file__).resolve().parent.parent
FILES = [
    "deploy/compose.yaml",
    "deploy/stack.yaml",
    "deploy/examples/*.yaml",
    "docs/deploy/*.md",
    "docs/self-hosting.md",
    "README.md",
]
IMAGE = re.compile(
    r"(ghcr\.io/rayakame/plsremind(?:-discord)?:)([0-9][0-9.]*[0-9]|[0-9])"
)
POSTGRES = re.compile(r"image:\s*(postgres:\S+)")


def series(version: str) -> str:
    major, minor = version.split(".")[:2]
    return f"{major}.{minor}" if major == "0" else major


def _files(root: pathlib.Path) -> list[pathlib.Path]:
    found: list[pathlib.Path] = []
    for pattern in FILES:
        found.extend(sorted(root.glob(pattern)))
    return found


def _postgres(path: pathlib.Path) -> str | None:
    if not path.exists():
        return None
    found = POSTGRES.search(path.read_text())
    return found.group(1) if found else None


def stale(root: pathlib.Path, version: str) -> list[str]:
    want = series(version)
    wrong = [
        f"{path.relative_to(root)}: {found.group(0)}, want {want}"
        for path in _files(root)
        for found in IMAGE.finditer(path.read_text())
        if found.group(2) != want
    ]
    compose = _postgres(root / "deploy/compose.yaml")
    stack = _postgres(root / "deploy/stack.yaml")
    if compose and stack and compose != stack:
        wrong.append(
            f"deploy/stack.yaml: {stack}, want {compose} as in deploy/compose.yaml"
        )
    return wrong


def write(root: pathlib.Path, version: str) -> list[str]:
    want = series(version)
    changed = []
    for path in _files(root):
        text = path.read_text()
        new = IMAGE.sub(lambda found: found.group(1) + want, text)
        if new != text:
            path.write_text(new)
            changed.append(str(path.relative_to(root)))
    return changed


def main(args: list[str]) -> int:
    version = json.loads((ROOT / ".release-please-manifest.json").read_text())["."]
    if args == ["--check"]:
        wrong = stale(ROOT, version)
        if wrong:
            print("\n".join(wrong), file=sys.stderr)
            print("run python3 deploy/series.py --write", file=sys.stderr)
            return 1
        print(f"ok  the deploy files name the {series(version)} series")
        return 0
    if args == ["--write"]:
        for name in write(ROOT, version):
            print(name)
        return 0
    if not args:
        print(series(version))
        return 0
    print(__doc__, file=sys.stderr)
    return 2


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
