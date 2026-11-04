#!/usr/bin/env python3
"""Fixture tests for docs_lint.py, one failing case per check.

Each test builds a throwaway git repository, so `git ls-files` behaves exactly as it
does in CI and no fixture touches the real tree. The Vietnamese strings below are test
data, not prose: they exist so the language check can be proven to fire on Vietnamese
prose and stay quiet on quoted UI copy, which is the distinction that decides whether
the gate is usable or ignored.

Run: python3 -m unittest discover -s tools/docs -p "*_test.py"
"""

from __future__ import annotations

import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import docs_lint  # noqa: E402


class LintCase(unittest.TestCase):
    def setUp(self) -> None:
        self._tmp = tempfile.TemporaryDirectory()
        self.root = Path(self._tmp.name)
        self.git("init", "-q")
        self.git("config", "user.email", "lint@example.test")
        self.git("config", "user.name", "lint")
        docs_lint.set_root(self.root)
        self._entry_files = docs_lint.ENTRY_FILES
        docs_lint.ENTRY_FILES = ("AGENTS.md",)

    def tearDown(self) -> None:
        docs_lint.ENTRY_FILES = self._entry_files
        docs_lint.set_root(docs_lint.REPO_ROOT)
        self._tmp.cleanup()

    def git(self, *args: str) -> None:
        subprocess.run(
            ["git", *args], cwd=self.root, check=True, capture_output=True, text=True
        )

    def write(self, rel: str, text: str) -> Path:
        path = self.root / rel
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(text, encoding="utf-8")
        return path

    def write_bytes(self, rel: str, size: int) -> Path:
        path = self.root / rel
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(b"x" * size)
        return path

    def stage(self) -> None:
        self.git("add", "-A", "-f")

    def checks(self, name: str) -> list[docs_lint.Violation]:
        return docs_lint.DETECTORS[name]()


class EntryBudgetTest(LintCase):
    def test_flags_file_over_per_file_ceiling(self) -> None:
        self.write_bytes("AGENTS.md", docs_lint.ENTRY_FILE_BUDGET + 1)
        self.stage()
        found = self.checks("entry-budget")
        self.assertEqual([v.path for v in found], ["AGENTS.md"])
        self.assertEqual(found[0].detail, "per-file")

    def test_flags_total_over_ceiling(self) -> None:
        docs_lint.ENTRY_FILES = ("AGENTS.md", "CLAUDE.md", "GEMINI.md")
        per_file = docs_lint.ENTRY_FILE_BUDGET - 1
        for name in docs_lint.ENTRY_FILES:
            self.write_bytes(name, per_file)
        self.stage()
        found = self.checks("entry-budget")
        self.assertEqual([(v.path, v.detail) for v in found], [("<always-loaded set>", "total")])

    def test_passes_inside_limits(self) -> None:
        self.write_bytes("AGENTS.md", 1024)
        self.stage()
        self.assertEqual(self.checks("entry-budget"), [])

    def test_flags_declared_entry_file_that_is_missing(self) -> None:
        docs_lint.ENTRY_FILES = ("AGENTS.md", "GEMINI.md")
        self.write_bytes("AGENTS.md", 10)
        self.stage()
        self.assertEqual([v.detail for v in self.checks("entry-budget")], ["missing"])

    def test_index_gets_the_larger_ceiling(self) -> None:
        docs_lint.ENTRY_FILES = ("AGENTS.md", "docs/lessons/INDEX.md")
        self.write_bytes("AGENTS.md", 1024)
        # Over the shared 16 KB cap, inside the index's 18 KB one.
        self.write_bytes("docs/lessons/INDEX.md", docs_lint.ENTRY_FILE_BUDGET + 1024)
        self.stage()
        self.assertEqual(self.checks("entry-budget"), [])

    def test_index_ceiling_still_binds(self) -> None:
        docs_lint.ENTRY_FILES = ("docs/lessons/INDEX.md",)
        index_budget = docs_lint.ENTRY_FILE_BUDGET_OVERRIDE["docs/lessons/INDEX.md"]
        self.write_bytes("docs/lessons/INDEX.md", index_budget + 1)
        self.stage()
        found = self.checks("entry-budget")
        self.assertEqual([v.detail for v in found], ["per-file"])
        self.assertIn(f"> {index_budget} B", found[0].measured)


class DocBudgetTest(LintCase):
    def test_flags_document_over_ceiling(self) -> None:
        self.write_bytes("docs/huge.md", docs_lint.DOC_BUDGET + 1)
        self.stage()
        found = self.checks("doc-budget")
        self.assertEqual([v.path for v in found], ["docs/huge.md"])

    def test_passes_at_exactly_the_ceiling(self) -> None:
        self.write_bytes("docs/edge.md", docs_lint.DOC_BUDGET)
        self.stage()
        self.assertEqual(self.checks("doc-budget"), [])

    def test_applies_to_root_readme(self) -> None:
        self.write_bytes("README.md", docs_lint.DOC_BUDGET + 1)
        self.stage()
        self.assertEqual([v.path for v in self.checks("doc-budget")], ["README.md"])

    def test_entry_files_are_not_double_counted(self) -> None:
        # An entry file is over the on-demand ceiling but inside its own, larger one.
        self.write_bytes("AGENTS.md", docs_lint.DOC_BUDGET + 1)
        self.stage()
        self.assertEqual(self.checks("doc-budget"), [])


class AdrNumberTest(LintCase):
    def test_flags_two_files_claiming_one_number(self) -> None:
        self.write("docs/ADR-009-first.md", "# one\n")
        self.write("docs/ADR-009-second.md", "# two\n")
        self.stage()
        found = self.checks("adr-number")
        self.assertEqual(len(found), 2)
        self.assertEqual({v.detail for v in found}, {"ADR-009"})

    def test_passes_when_numbers_are_unique(self) -> None:
        self.write("docs/ADR-001-first.md", "# one\n")
        self.write("docs/ADR-002-second.md", "# two\n")
        self.stage()
        self.assertEqual(self.checks("adr-number"), [])


class LinkTest(LintCase):
    def test_flags_missing_relative_target(self) -> None:
        self.write("docs/page.md", "# page\n\nSee [gone](gone.md).\n")
        self.stage()
        found = self.checks("links")
        self.assertEqual([(v.path, v.detail, v.line) for v in found], [("docs/page.md", "gone.md", 3)])

    def test_passes_when_target_exists(self) -> None:
        self.write("docs/page.md", "# page\n\nSee [there](there.md).\n")
        self.write("docs/there.md", "# there\n")
        self.stage()
        self.assertEqual(self.checks("links"), [])

    def test_ignores_external_anchor_and_site_absolute_targets(self) -> None:
        self.write(
            "docs/page.md",
            "# page\n\n[web](https://example.test/x) [anchor](#heading) [route](/app/x)\n",
        )
        self.stage()
        self.assertEqual(self.checks("links"), [])

    def test_ignores_links_inside_fenced_code(self) -> None:
        self.write("docs/page.md", "# page\n\n```\n[example](not-a-real-file.md)\n```\n")
        self.stage()
        self.assertEqual(self.checks("links"), [])

    def test_resolves_relative_parent_paths(self) -> None:
        self.write("docs/reports/page.md", "[up](../up.md)\n")
        self.write("docs/up.md", "# up\n")
        self.stage()
        self.assertEqual(self.checks("links"), [])


class FileSizeTest(LintCase):
    def test_flags_file_over_one_megabyte(self) -> None:
        self.write_bytes("assets/big.bin", docs_lint.FILE_SIZE_BUDGET + 1)
        self.stage()
        self.assertEqual([v.path for v in self.checks("file-size")], ["assets/big.bin"])

    def test_passes_under_the_limit(self) -> None:
        self.write_bytes("assets/small.bin", docs_lint.FILE_SIZE_BUDGET)
        self.stage()
        self.assertEqual(self.checks("file-size"), [])


VIETNAMESE_PROSE = """\
# Báo cáo

Chúng tôi đã thực hiện benchmark với dữ liệu thật của các camera.
Kết quả cho thấy hệ thống đáp ứng được yêu cầu của thành phố.
Không có lỗi nào được ghi nhận trong suốt quá trình kiểm thử.
Độ trễ của hệ thống rất thấp và không có rò rỉ bộ nhớ.
Chúng tôi sẽ tiếp tục theo dõi trong những ngày tiếp theo.
Các chỉ số này là cơ sở để quyết định triển khai chính thức.
Báo cáo này được tổng hợp từ dữ liệu của ba mươi camera.
Mỗi camera được kiểm tra trong khoảng thời gian mười phút.
Những kết quả bất thường đã được ghi lại để phân tích sau.
"""

QUOTED_UI_COPY = """\
# Incident

1. Reviewers saw only `Tất cả (200)` and `Chờ duyệt (Auto) (0)` in the filter.
2. Clicking it showed `Không tìm thấy ảnh chụp nào cho ngày đã chọn`.
3. The modal read `KHÔNG THỂ TẢI HÌNH ẢNH SNAPSHOT` before the fix.
4. All 94 bridge cameras were audited for `dạ cầu` and `chân cầu` keywords.
5. The label `Đã duyệt (Human)` appeared with a zero count.
6. The empty-state copy `Chưa có dữ liệu` was shown instead of the table.
7. Retry surfaced `Máy chủ proxy báo lỗi` again after the second attempt.
8. Reviewer notes quoted `Kẹt xe` and `Ngập nước` as ground-truth labels.
9. Sorting by `Mới nhất` reordered the queue as expected.
10. The badge `Đang xử lý` cleared once the job finished.
11. Filter chips `Tất cả`, `Chờ duyệt`, `Đã duyệt` all rendered.
12. Console logged `Không tìm thấy` for the missing fixture path.
"""


class EnglishTest(LintCase):
    def test_flags_vietnamese_prose(self) -> None:
        self.write("docs/report.md", VIETNAMESE_PROSE)
        self.stage()
        self.assertEqual([v.path for v in self.checks("english")], ["docs/report.md"])

    def test_ignores_quoted_ui_copy_in_code_spans(self) -> None:
        self.write("docs/incident.md", QUOTED_UI_COPY)
        self.stage()
        self.assertEqual(self.checks("english"), [])

    def test_ignores_english_prose_with_place_names(self) -> None:
        self.write(
            "docs/cameras.md",
            "# Cameras\n\n"
            "| Camera | Ward |\n|---|---|\n"
            "| Cộng Hòa - Trường Chinh | Phường Tân Bình |\n"
            "| Mai Chí Thọ - Đồng Văn Cống | Phường Thạnh Mỹ Tây |\n",
        )
        self.stage()
        self.assertEqual(self.checks("english"), [])

    def test_exempts_the_vietnamese_readme(self) -> None:
        self.write("README.vi.md", VIETNAMESE_PROSE)
        self.stage()
        self.assertEqual(self.checks("english"), [])

    def test_ignores_fenced_blocks(self) -> None:
        self.write(
            "docs/sample.md",
            "# Sample\n\nSentence in English.\n\n```text\n"
            + VIETNAMESE_PROSE
            + "```\n",
        )
        self.stage()
        self.assertEqual(self.checks("english"), [])


class BaselineKeyTest(LintCase):
    def test_key_excludes_line_numbers(self) -> None:
        self.write("docs/page.md", "# page\n\nSee [gone](gone.md).\n")
        self.stage()
        first = self.checks("links")[0]
        self.assertEqual(first.line, 3)

        # The same violation, pushed down by three lines of unrelated prose.
        self.write(
            "docs/page.md",
            "# page\n\nNew paragraph one.\n\nNew paragraph two.\n\nSee [gone](gone.md).\n",
        )
        self.stage()
        second = self.checks("links")[0]
        self.assertEqual(second.line, 7)
        self.assertEqual(
            second.key,
            first.key,
            "an unrelated edit above a known link must not churn the key",
        )

    def test_roundtrip_preserves_keys_without_detail(self) -> None:
        self.write_bytes("docs/huge.md", docs_lint.DOC_BUDGET + 1)
        self.stage()
        found = self.checks("doc-budget")
        path = self.root / docs_lint.BASELINE_NAME
        docs_lint.write_baseline(path, found)
        self.assertEqual(docs_lint.load_baseline(path), {v.key for v in found})


class RobustnessTest(LintCase):
    def test_missing_tracked_file_does_not_crash(self) -> None:
        p = self.write("docs/deleted.md", "# Hello")
        self.stage()
        p.unlink()
        for name in docs_lint.CHECKS:
            # Should not raise FileNotFoundError across any detector
            self.checks(name)


if __name__ == "__main__":
    unittest.main()
