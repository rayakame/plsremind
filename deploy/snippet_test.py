import unittest

import snippet

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


if __name__ == "__main__":
    unittest.main()
