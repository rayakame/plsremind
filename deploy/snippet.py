"""Prints a command block of a guide, so that the tests run exactly what the guide says.

python3 deploy/snippet.py <file.md> <name>   the first code block after <!-- snippet: <name> -->
"""

from __future__ import annotations

import pathlib
import re
import sys
from collections.abc import Iterator

OPENING = re.compile(r"^( {0,3})(`{3,}|~{3,})([^\n]*)\n", re.MULTILINE)


def fences(text: str, start: int = 0) -> Iterator[tuple[str, str, int]]:
    """Yields the info string, the content and the offset of every closed fenced code block."""
    while (opening := OPENING.search(text, start)) is not None:
        indent, fence = len(opening.group(1)), opening.group(2)
        closing = re.compile(
            rf"^ {{0,3}}{re.escape(fence[0])}{{{len(fence)},}}[ \t]*$", re.MULTILINE
        ).search(text, opening.end())
        if closing is None:
            return
        lines = text[opening.end() : closing.start()].splitlines(keepends=True)
        content = "".join(re.sub(rf"^ {{0,{indent}}}", "", line) for line in lines)
        yield opening.group(3).strip(), content, opening.start()
        start = closing.end()


def snippet(text: str, name: str) -> str:
    marker = re.search(rf"^<!-- snippet: {re.escape(name)} -->$", text, re.MULTILINE)
    if marker is None:
        raise LookupError(name)
    for _, content, _ in fences(text, marker.end()):
        return content
    raise LookupError(name)


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
