import pathlib
import re
import subprocess
import unittest

import snippet

ROOT = pathlib.Path(__file__).resolve().parent.parent

GUIDE = """# Backups

<!-- snippet: backup -->
```sh
docker compose exec -T db pg_dump > plsremind.dump
```

Then, to put it back:

<!-- snippet: restore -->
Run this in the same folder.

```sh
docker compose stop
docker compose up -d
```
"""


class SnippetTest(unittest.TestCase):
    def test_returns_the_block_after_its_marker(self) -> None:
        self.assertEqual(
            snippet.snippet(GUIDE, "backup"),
            "docker compose exec -T db pg_dump > plsremind.dump\n",
        )

    def test_skips_prose_between_marker_and_block(self) -> None:
        self.assertEqual(
            snippet.snippet(GUIDE, "restore"),
            "docker compose stop\ndocker compose up -d\n",
        )

    def test_a_name_that_is_not_there_is_an_error(self) -> None:
        with self.assertRaises(LookupError):
            snippet.snippet(GUIDE, "upgrade")


class GuidesTest(unittest.TestCase):
    def test_every_sh_block_parses_in_a_posix_shell(self) -> None:
        # dash, the /bin/sh of Debian and Ubuntu, rejects what POSIX sh does not have.
        fence = re.compile(r"^```sh\n(.*?)^```$", re.MULTILINE | re.DOTALL)
        for path in [ROOT / "README.md", *sorted((ROOT / "docs").rglob("*.md"))]:
            text = path.read_text()
            for block in fence.finditer(text):
                line = text.count("\n", 0, block.start()) + 1
                with self.subTest(f"{path.relative_to(ROOT)}:{line}"):
                    parsed = subprocess.run(
                        ["dash", "-n"],
                        input=block.group(1),
                        text=True,
                        capture_output=True,
                    )
                    self.assertEqual(parsed.returncode, 0, parsed.stderr)


if __name__ == "__main__":
    unittest.main()
