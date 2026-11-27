import pathlib
import tempfile
import unittest

import series


class SeriesTest(unittest.TestCase):
    def test_before_one_the_series_is_major_and_minor(self) -> None:
        self.assertEqual(series.series("0.0.0"), "0.0")
        self.assertEqual(series.series("0.2.3"), "0.2")

    def test_from_one_the_series_is_the_major(self) -> None:
        self.assertEqual(series.series("1.0.0"), "1")
        self.assertEqual(series.series("2.4.1"), "2")


class FilesTest(unittest.TestCase):
    def setUp(self) -> None:
        folder = tempfile.TemporaryDirectory()
        self.addCleanup(folder.cleanup)
        self.root = pathlib.Path(folder.name)
        (self.root / "deploy").mkdir()
        (self.root / "docs/deploy").mkdir(parents=True)
        self.compose = self.root / "deploy/compose.yaml"
        self.compose.write_text(
            "services:\n  hub:\n    image: ghcr.io/rayakame/plsremind:0.1.4\n"
        )
        self.doc = self.root / "docs/deploy/run.md"
        self.doc.write_text(
            "docker run ghcr.io/rayakame/plsremind-discord:0.1\n"
            "or the newest build: ghcr.io/rayakame/plsremind:edge\n"
        )

    def test_names_every_reference_of_another_series(self) -> None:
        self.assertEqual(
            series.stale(self.root, "0.2.0"),
            [
                "deploy/compose.yaml: ghcr.io/rayakame/plsremind:0.1.4, want 0.2",
                "docs/deploy/run.md: ghcr.io/rayakame/plsremind-discord:0.1, want 0.2",
            ],
        )

    def test_write_names_the_series_and_leaves_other_tags(self) -> None:
        self.assertEqual(
            series.write(self.root, "0.2.0"),
            ["deploy/compose.yaml", "docs/deploy/run.md"],
        )
        self.assertIn("ghcr.io/rayakame/plsremind:0.2\n", self.compose.read_text())
        self.assertEqual(
            self.doc.read_text(),
            "docker run ghcr.io/rayakame/plsremind-discord:0.2\n"
            "or the newest build: ghcr.io/rayakame/plsremind:edge\n",
        )
        self.assertEqual(series.stale(self.root, "0.2.0"), [])
        self.assertEqual(series.write(self.root, "0.2.0"), [])

    def test_compose_and_stack_must_run_the_same_postgres(self) -> None:
        series.write(self.root, "0.2.0")
        self.compose.write_text(
            self.compose.read_text() + "  db:\n    image: postgres:18\n"
        )
        (self.root / "deploy/stack.yaml").write_text(
            "services:\n  db:\n    image: postgres:17\n"
        )
        self.assertEqual(
            series.stale(self.root, "0.2.0"),
            [
                "deploy/stack.yaml: postgres:17, want postgres:18 as in deploy/compose.yaml"
            ],
        )


if __name__ == "__main__":
    unittest.main()
