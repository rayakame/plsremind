#!/usr/bin/env bash
# Every file that names a version must agree with release-please's manifest, or a release ships mixed versions.
set -euo pipefail

cd "$(dirname "$0")/.."
python3 - <<'PY'
import json
import pathlib
import re
import sys

manifest = json.loads(pathlib.Path(".release-please-manifest.json").read_text())
lock = json.loads(pathlib.Path("package-lock.json").read_text())


def about(path: str) -> str:
    found = re.search(r'__version__[^=]*= "([^"]+)"', pathlib.Path(path).read_text())
    return found.group(1) if found else "missing"


def self_hosting() -> str:
    found = re.search(r"^version=(\S+) # x-release-please-version$", pathlib.Path("docs/self-hosting.md").read_text(), re.M)
    return found.group(1) if found else "missing"


def image(name: str) -> str:
    found = re.search(rf"image: ghcr\.io/rayakame/{name}:(\S+)", pathlib.Path("deploy/compose.yaml").read_text())
    return found.group(1) if found else "missing"


found = {
    ".": {
        "connections/discord/plsremind_discord/about.py": about("connections/discord/plsremind_discord/about.py"),
        "web/package.json": json.loads(pathlib.Path("web/package.json").read_text())["version"],
        "package-lock.json web": lock["packages"]["web"]["version"],
        "deploy/compose.yaml hub image": image("plsremind"),
        "deploy/compose.yaml bot image": image("plsremind-discord"),
        "docs/self-hosting.md": self_hosting(),
    },
    "sdk/python": {
        "sdk/python/plsremind/internal/about.py": about("sdk/python/plsremind/internal/about.py"),
    },
    "sdk/ts": {
        "sdk/ts/package.json": json.loads(pathlib.Path("sdk/ts/package.json").read_text())["version"],
        "package-lock.json sdk/ts": lock["packages"]["sdk/ts"]["version"],
    },
}
wrong = [
    f"{name} says {version}, the manifest {manifest.get(component)}"
    for component, files in found.items()
    for name, version in files.items()
    if version != manifest.get(component)
]
if wrong:
    print("\n".join(wrong), file=sys.stderr)
    sys.exit(1)
print("ok  versions agree:", ", ".join(f"{component} {manifest[component]}" for component in found))
PY
