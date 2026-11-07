#!/usr/bin/env python3
"""Enforce the documentation rules the audit had to find by hand.

Six checks, one gate:

  entry-budget   the always-loaded set stays inside 36 KB total and 16 KB per file
                 (docs/lessons/INDEX.md has its own 20 KB ceiling, per Rule 11 / L66)
  doc-budget     every on-demand document stays inside 25 KB
  adr-number     no two ADRs claim the same number
  links          every relative link resolves
  file-size      no tracked file is over 1 MiB
  english        prose in tracked markdown is English

A gate that is red on the day it lands gets muted within a week, so this one is a
ratchet. `.docs-lint-baseline.txt` records the violations that already exist, and
the run fails only on `current - baseline`. A baseline entry that no longer matches
a violation is itself an error, so a fix has to delete its entry in the same pull
request and the file can only shrink.

Keys carry `check`, `path` and a stable `detail`, never a line number: an unrelated
edit above a known violation must not churn the baseline.

Usage:
    python3 tools/docs/docs_lint.py                    # gate: same code path as CI
    python3 tools/docs/docs_lint.py --update-baseline   # rewrite the baseline
    python3 tools/docs/docs_lint.py --check links       # one check only
    python3 tools/docs/docs_lint.py --list              # show baseline contents

Exit status is 0 only when there is nothing new and nothing stale.
"""

from __future__ import annotations

import argparse
import re
import subprocess
import sys
from dataclasses import dataclass
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parents[2]
# Detectors read this instead of REPO_ROOT so a fixture test can point them at a
# throwaway repository. Production never calls set_root().
_ROOT = REPO_ROOT


def set_root(path: str | Path) -> None:
    global _ROOT
    _ROOT = Path(path)


def root() -> Path:
    return _ROOT

BASELINE_NAME = ".docs-lint-baseline.txt"

# Two tiers, ratified in #353 and recorded in L60. The always-loaded set is read on
# every task, so its cost is paid thousands of times and is capped hard. On-demand
# documents are read deliberately and get a looser ceiling.
ENTRY_TOTAL_BUDGET = 36_864
ENTRY_FILE_BUDGET = 16_384
# `docs/lessons/INDEX.md` is the one sanctioned exception, per Rule 11 and L66. It is a
# table of one row per lesson; holding it at 18 KB meant every new lesson had to shorten
# an existing row's guardrail, which erodes the mechanism the index exists to provide.
# The set total still bounds the whole recurring cost.
ENTRY_FILE_BUDGET_OVERRIDE = {"docs/lessons/INDEX.md": 20_480}
DOC_BUDGET = 25_600
FILE_SIZE_BUDGET = 1_048_576

# The files an agent loads before it knows what the task is. Adding one here is a
# real cost, which is the point of listing them explicitly rather than globbing.
ENTRY_FILES = (
    "AGENTS.md",
    "CLAUDE.md",
    "GEMINI.md",
    "docs/ONBOARDING.md",
    "docs/lessons/INDEX.md",
)

# The Vietnamese README is the one file Rule 1 allows to be Vietnamese.
ENGLISH_EXEMPT = frozenset({"README.vi.md"})

LINK_RE = re.compile(r"\]\(([^)\s]+)\)")
SCHEME_RE = re.compile(r"^[a-z][a-z0-9+.-]*:", re.IGNORECASE)
FENCE_RE = re.compile(r"^\s*(```|~~~)")
INLINE_CODE_RE = re.compile(r"`[^`]*`")
ADR_NAME_RE = re.compile(r"^ADR-(\d{3})-")

# Vietnamese function words. Prose uses them; proper nouns, addresses and quoted UI
# copy do not. This is a far better signal than counting diacritics, which flags
# every table of camera names and every `KHÔNG THỂ TẢI HÌNH ẢNH` in a code span.
VI_FUNCTION_WORDS = re.compile(
    r"\b(của|và|là|các|được|không|với|cho|trong|những|này|khi|có|để|một|từ|theo"
    r"|nhưng|hoặc|đã|sẽ|phải|cũng|thì|về|ra|vào|nên|rồi|lúc|sau|trước|rất|chỉ)\b",
    re.IGNORECASE,
)
# Tuned against the corpus: 38.7% and 21.7% are the two real Vietnamese documents,
# the next highest is 3.3% and is a single quoted UI string. The gap is wide enough
# that the threshold is not a guess.
# Five prose lines is the shortest document that can be Vietnamese "as a document" rather
# than a caption. Below that the signal is too weak to accuse anyone.
VI_MIN_LINES = 5
VI_MIN_HITS = 3
VI_MIN_PERCENT = 5.0

CHECKS = ("entry-budget", "doc-budget", "adr-number", "links", "file-size", "english")


@dataclass(frozen=True, order=True)
class Violation:
    check: str
    path: str
    detail: str = ""
    line: int | None = None
    measured: str = ""

    @property
    def key(self) -> str:
        # The detail is only present when it carries meaning. A trailing tab would be
        # invisible in the baseline and would silently mismatch on a whitespace edit.
        if self.detail:
            return f"{self.check}\t{self.path}\t{self.detail}"
        return f"{self.check}\t{self.path}"

    def render(self) -> str:
        where = f"{self.path}:{self.line}" if self.line is not None else self.path
        parts = [where, self.check]
        if self.detail:
            parts.append(self.detail)
        if self.measured:
            parts.append(f"({self.measured})")
        return " ".join(parts)


# --------------------------------------------------------------------------- #
# helpers
# --------------------------------------------------------------------------- #


def tracked_files(*patterns: str) -> list[str]:
    result = subprocess.run(
        ["git", "ls-files", "--", *patterns],
        cwd=_ROOT,
        capture_output=True,
        text=True,
        check=True,
    )
    return [line for line in result.stdout.splitlines() if line]


def read(path: str) -> str:
    return (_ROOT / path).read_text(encoding="utf-8", errors="replace")


def on_disk(path: str) -> bool:
    return (_ROOT / path).is_file()


def prose_lines(text: str) -> list[str]:
    """Lines a reader reads, with code fences and inline code removed."""
    out: list[str] = []
    in_fence = False
    for line in text.splitlines():
        if FENCE_RE.match(line):
            in_fence = not in_fence
            continue
        if in_fence or not line.strip():
            continue
        stripped = INLINE_CODE_RE.sub("", line)
        if stripped.strip():
            out.append(stripped)
    return out


# --------------------------------------------------------------------------- #
# checks
# --------------------------------------------------------------------------- #


def check_entry_budget() -> list[Violation]:
    violations: list[Violation] = []
    total = 0
    for path in ENTRY_FILES:
        full = _ROOT / path
        if not full.exists():
            violations.append(
                Violation("entry-budget", path, "missing", measured="declared entry file not found")
            )
            continue
        size = full.stat().st_size
        total += size
        budget = ENTRY_FILE_BUDGET_OVERRIDE.get(path, ENTRY_FILE_BUDGET)
        if size > budget:
            violations.append(
                Violation(
                    "entry-budget",
                    path,
                    "per-file",
                    measured=f"{size} B > {budget} B",
                )
            )
    if total > ENTRY_TOTAL_BUDGET:
        violations.append(
            Violation(
                "entry-budget",
                "<always-loaded set>",
                "total",
                measured=f"{total} B > {ENTRY_TOTAL_BUDGET} B",
            )
        )
    return violations


def check_doc_budget() -> list[Violation]:
    violations: list[Violation] = []
    # Root-level markdown counts too. README.md is a standalone document rather than an
    # always-loaded entry file, but "standalone" is not "unbounded": it gets the same
    # on-demand ceiling as anything under docs/.
    for path in tracked_files("docs/**/*.md", "docs/*.md", "*.md"):
        if path in ENTRY_FILES or not on_disk(path):
            continue
        size = (_ROOT / path).stat().st_size
        if size > DOC_BUDGET:
            violations.append(
                Violation(
                    "doc-budget", path, "", measured=f"{size} B > {DOC_BUDGET} B"
                )
            )
    return violations


def check_adr_number() -> list[Violation]:
    by_number: dict[str, list[str]] = {}
    for path in tracked_files("docs/ADR-*.md", "docs/*/ADR-*.md"):
        match = ADR_NAME_RE.match(Path(path).name)
        if match:
            by_number.setdefault(match.group(1), []).append(path)
    violations: list[Violation] = []
    for number, paths in sorted(by_number.items()):
        if len(paths) > 1:
            for path in sorted(paths):
                violations.append(
                    Violation(
                        "adr-number",
                        path,
                        f"ADR-{number}",
                        measured=f"{len(paths)} files claim this number",
                    )
                )
    return violations


def check_links() -> list[Violation]:
    violations: list[Violation] = []
    for path in tracked_files("*.md"):
        if not on_disk(path):
            continue
        full = _ROOT / path
        in_fence = False
        for number, line in enumerate(read(path).splitlines(), 1):
            if FENCE_RE.match(line):
                in_fence = not in_fence
                continue
            if in_fence:
                continue
            for target in LINK_RE.findall(line):
                if not target or target.startswith(("#", "/")) or SCHEME_RE.match(target):
                    continue
                clean = target.split("#", 1)[0]
                if not clean:
                    continue
                if not (full.parent / clean).exists():
                    violations.append(Violation("links", path, target, line=number))
    return violations


def check_file_size() -> list[Violation]:
    violations: list[Violation] = []
    for path in tracked_files():
        full = _ROOT / path
        if not full.is_file():
            continue
        size = full.stat().st_size
        if size > FILE_SIZE_BUDGET:
            violations.append(
                Violation(
                    "file-size",
                    path,
                    "",
                    measured=f"{size / 1_048_576:.1f} MB > 1 MB",
                )
            )
    return violations


def check_english() -> list[Violation]:
    violations: list[Violation] = []
    for path in tracked_files("*.md"):
        if Path(path).name in ENGLISH_EXEMPT or not on_disk(path):
            continue
        lines = prose_lines(read(path))
        if len(lines) < VI_MIN_LINES:
            continue
        hits = sum(1 for line in lines if VI_FUNCTION_WORDS.search(line))
        percent = hits / len(lines) * 100
        if hits >= VI_MIN_HITS and percent >= VI_MIN_PERCENT:
            violations.append(
                Violation(
                    "english",
                    path,
                    "",
                    measured=f"{hits}/{len(lines)} prose lines Vietnamese ({percent:.1f}%)",
                )
            )
    return violations


DETECTORS = {
    "entry-budget": check_entry_budget,
    "doc-budget": check_doc_budget,
    "adr-number": check_adr_number,
    "links": check_links,
    "file-size": check_file_size,
    "english": check_english,
}


# --------------------------------------------------------------------------- #
# baseline
# --------------------------------------------------------------------------- #


def baseline_path(explicit: str | None) -> Path:
    return Path(explicit) if explicit else _ROOT / BASELINE_NAME


def load_baseline(path: Path) -> set[str]:
    if not path.exists():
        return set()
    entries: set[str] = set()
    for raw in path.read_text(encoding="utf-8").splitlines():
        # Do not strip the whole line: the key is tab-delimited and a path may legitimately
        # end in a character that strip() would remove.
        if not raw.strip() or raw.lstrip().startswith("#"):
            continue
        entries.add(raw.rstrip())
    return entries


def write_baseline(path: Path, violations: list[Violation]) -> None:
    header = [
        "# Docs-lint baseline: violations that already existed when the gate landed.",
        "#",
        "# This is a ratchet, not a whitelist. Fixing a violation means deleting its",
        "# line here in the same pull request; a stale line fails the gate, so this file",
        "# can only shrink. Never add a line to silence a regression.",
        "#",
        "# Format: <check>\\t<path>\\t<detail>  (no line numbers, so edits do not churn it)",
    ]
    body = sorted({violation.key for violation in violations})
    path.write_text("\n".join(header + body) + "\n", encoding="utf-8")


# --------------------------------------------------------------------------- #
# main
# --------------------------------------------------------------------------- #


def run(selected: tuple[str, ...]) -> list[Violation]:
    violations: list[Violation] = []
    for name in selected:
        violations.extend(DETECTORS[name]())
    return sorted(violations)


def main() -> int:
    parser = argparse.ArgumentParser(
        description="Enforce the documentation budget, structure and language rules."
    )
    parser.add_argument(
        "--check",
        action="append",
        choices=CHECKS,
        help="run one check (repeatable); default is all six",
    )
    parser.add_argument("--baseline", help=f"baseline file (default: {BASELINE_NAME})")
    parser.add_argument(
        "--update-baseline",
        action="store_true",
        help="rewrite the baseline from the current tree",
    )
    parser.add_argument("--list", action="store_true", help="print the baseline and exit")
    parser.add_argument("-v", "--verbose", action="store_true", help="print every violation")
    args = parser.parse_args()

    path = baseline_path(args.baseline)
    selected = tuple(args.check) if args.check else CHECKS

    if args.list:
        for entry in sorted(load_baseline(path)):
            print(entry)
        return 0

    violations = run(selected)

    if args.update_baseline:
        write_baseline(path, violations)
        print(f"wrote {path}")
        print(f"{len(violations)} violation(s) recorded")
        return 0

    baseline = load_baseline(path)
    current = {violation.key: violation for violation in violations}
    new = [violation for key, violation in current.items() if key not in baseline]
    stale = sorted(baseline - set(current))

    if args.verbose:
        for violation in violations:
            print(f"  {violation.render()}")

    for violation in sorted(new):
        print(f"NEW   {violation.render()}")
    for entry in stale:
        print(f"STALE {entry.replace(chr(9), '  ')}")

    print()
    print(
        f"{len(violations)} violation(s), {len(baseline)} in the baseline, "
        f"{len(new)} new, {len(stale)} stale"
    )

    if new:
        print("\nFail: a new violation landed. Fix it, or explain in the pull request why")
        print("the baseline has to grow, and add the line with --update-baseline.")
    if stale:
        print("\nFail: the baseline lists violations that no longer exist. Delete those")
        print("lines: a baseline that cannot shrink is a whitelist.")
    if new or stale:
        return 1
    print("Pass: nothing new, nothing stale.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
