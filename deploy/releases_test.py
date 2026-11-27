import unittest

import releases

FILES = ["compose.yaml", "stack.yaml", "env.example", "plsremind-monitoring.tar.gz"]


def release(
    tag: str,
    created: str,
    files: list[str] = FILES,
    state: str = "uploaded",
    **flags: bool,
) -> dict:
    return {
        "tag_name": tag,
        "created_at": created,
        "draft": flags.get("draft", False),
        "prerelease": flags.get("prerelease", False),
        "assets": [{"name": name, "state": state} for name in files],
    }


class NewestTest(unittest.TestCase):
    def test_takes_the_newest_by_creation_not_by_list_order(self) -> None:
        listed = [
            release("v0.2.0", "2026-11-01T10:00:00Z"),
            release("v0.3.0", "2026-11-10T10:00:00Z"),
            release("v0.2.1", "2026-11-05T10:00:00Z"),
        ]
        self.assertEqual(releases.newest(listed, FILES), "v0.3.0")

    def test_passes_over_a_release_with_a_file_missing_or_not_fully_uploaded(
        self,
    ) -> None:
        listed = [
            release("v0.2.0", "2026-11-01T10:00:00Z"),
            release("v0.3.0", "2026-11-10T10:00:00Z", files=["compose.yaml"]),
            release("v0.3.1", "2026-11-15T10:00:00Z", state="starter"),
        ]
        self.assertEqual(releases.newest(listed, FILES), "v0.2.0")
        self.assertEqual(releases.newest(listed, ["compose.yaml"]), "v0.3.0")

    def test_ignores_sdk_releases_drafts_and_prereleases(self) -> None:
        listed = [
            release("v0.2.0", "2026-11-01T10:00:00Z"),
            release("sdk-python-v0.4.0", "2026-11-20T10:00:00Z"),
            release("v0.3.0", "2026-11-21T10:00:00Z", draft=True),
            release("v0.4.0-rc.1", "2026-11-22T10:00:00Z", prerelease=True),
        ]
        self.assertEqual(releases.newest(listed, FILES), "v0.2.0")

    def test_without_files_takes_the_newest_server_release(self) -> None:
        listed = [
            release("v0.2.0", "2026-11-01T10:00:00Z"),
            release("v0.3.0", "2026-11-10T10:00:00Z", files=[]),
        ]
        self.assertEqual(releases.newest(listed, []), "v0.3.0")

    def test_none_when_no_release_qualifies(self) -> None:
        self.assertIsNone(releases.newest([], FILES))
        listed = [release("v0.2.0", "2026-11-01T10:00:00Z", state="starter")]
        self.assertIsNone(releases.newest(listed, FILES))


if __name__ == "__main__":
    unittest.main()
