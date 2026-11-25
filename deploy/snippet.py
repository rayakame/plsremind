"""Prints a command block of a guide, so that the tests run exactly what the guide says.

python3 deploy/snippet.py <file.md> <name>   the first code block after <!-- snippet: <name> -->
"""

from __future__ import annotations

import pathlib
import re
import sys


def snippet(text: str, name: str) -> str:
    marker = re.search(rf"^<!-- snippet: {re.escape(name)} -->$", text, re.MULTILINE)
    if marker is None:
        raise LookupError(name)
    block = re.compile(r"^```[^\n]*\n(.*?)^```$", re.MULTILINE | re.DOTALL).search(
        text, marker.end()
    )
    if block is None:
        raise LookupError(name)
    return block.group(1)


def main(args: list[str]) -> int:
    if len(args) != 2:
        print(__doc__, file=sys.stderr)
        return 2
    path, name = args
    try:
        print(snippet(pathlib.Path(path).read_text(), name), end="")
    except LookupError:
        print(f"no snippet {name} in {path}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
