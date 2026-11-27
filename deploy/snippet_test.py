import pathlib
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

    def test_a_shorter_fence_does_not_end_a_longer_block(self) -> None:
        guide = "<!-- snippet: notes -->\n````sh\ncat <<'EOF'\n```\nEOF\n````\n"
        self.assertEqual(snippet.snippet(guide, "notes"), "cat <<'EOF'\n```\nEOF\n")


class FencesTest(unittest.TestCase):
    def test_a_block_inside_a_longer_one_is_part_of_it(self) -> None:
        text = "````markdown\n```sh\nif then\n```\n````\n\n```sh\necho\n```\n"
        self.assertEqual(
            [(info, body) for info, body, _ in snippet.fences(text)],
            [("markdown", "```sh\nif then\n```\n"), ("sh", "echo\n")],
        )

    def test_a_tilde_fence_ends_only_with_tildes(self) -> None:
        text = "~~~sh\necho\n```\n~~~\n"
        self.assertEqual(
            [(info, body) for info, body, _ in snippet.fences(text)],
            [("sh", "echo\n```\n")],
        )

    def test_a_block_in_a_list_item_loses_the_item_indentation(self) -> None:
        text = "1. Run:\n\n   ```sh\n   echo one\n     echo two\n   ```\n"
        self.assertEqual(
            [(info, body) for info, body, _ in snippet.fences(text)],
            [("sh", "echo one\n  echo two\n")],
        )

    def test_the_offset_is_where_the_block_starts(self) -> None:
        text = "intro\n\n```sh\necho\n```\n"
        self.assertEqual(
            [offset for _, _, offset in snippet.fences(text)], [text.index("```")]
        )


class GuidesTest(unittest.TestCase):
    def test_every_sh_block_parses_in_a_posix_shell(self) -> None:
        # dash, the /bin/sh of Debian and Ubuntu, rejects what POSIX sh does not have.
        for path in [ROOT / "README.md", *sorted((ROOT / "docs").rglob("*.md"))]:
            text = path.read_text()
            for info, body, offset in snippet.fences(text):
                if info != "sh":
                    continue
                line = text.count("\n", 0, offset) + 1
                with self.subTest(f"{path.relative_to(ROOT)}:{line}"):
                    parsed = subprocess.run(
                        ["dash", "-n"],
                        input=body,
                        text=True,
                        capture_output=True,
                    )
                    self.assertEqual(parsed.returncode, 0, parsed.stderr)


if __name__ == "__main__":
    unittest.main()
