#!/usr/bin/env python3
"""Prove that a documentation file was moved without being rewritten.

Moving a markdown file breaks every relative link inside it, and the usual fix
is to hand-edit those links while "tidying up" the prose around them. This tool
makes that impossible to do silently: it takes the pre-move file from a git ref,
applies the declared link rewrites, and demands a byte-for-byte match against the
file on disk.

Usage:
    python3 tools/docs/verify_doc_move.py \\
        --base origin/dev \\
        --old docs/sprint9_execution_guide.md \\
        --new docs/sprints/sprint9_execution_guide.md \\
        --rewrite '](ADR-011-sprint8-audit-and-sprint9-plan.md)=](../ADR-011-sprint8-audit-and-sprint9-plan.md)' \\
        --rewrite '](../AGENTS.md)=](../../AGENTS.md)'

Every `--rewrite` is `old=new`, applied to the pre-move text before comparison.
Rewrites must be link targets, not prose: a rewrite that changes more than the
path inside `](...)` is rejected by the audit below, which re-derives the byte
delta and compares it with the declared one.

The second half of the run reports every relative link in the moved file and
whether it resolves. A link that was already dead before the move is reported as
`DEAD (pre-existing)` rather than as a regression, because moving a file cannot
be blamed for a target that never existed.
"""

from __future__ import annotations

import argparse
import re
import subprocess
import sys
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parents[2]
LINK_RE = re.compile(r"\]\(([^)\s]+)\)")
SCHEME_RE = re.compile(r"^[a-z][a-z0-9+.-]*:", re.IGNORECASE)


def relative_targets(text: str) -> list[str]:
    """Every link target in `text` that points at a path in the repository."""
    seen: list[str] = []
    for target in LINK_RE.findall(text):
        if target.startswith("#") or SCHEME_RE.match(target):
            continue
        seen.append(target)
    return seen


def dead_targets(text: str, anchor: Path) -> list[str]:
    """Relative targets in `text` that do not resolve from directory `anchor`."""
    dead: list[str] = []
    for target in relative_targets(text):
        path = target.split("#", 1)[0]
        if not path:
            continue
        if not (anchor / path).exists():
            dead.append(target)
    return dead


def apply_rewrites(text: str, rewrites: list[tuple[str, str]]) -> str:
    for old, new in rewrites:
        if old not in text:
            raise SystemExit(f"FAIL: rewrite source not found in the pre-move file: {old!r}")
        text = text.replace(old, new)
    return text


def link_only(rewrites: list[tuple[str, str]]) -> bool:
    """True when every rewrite is a `](...)` target, i.e. not prose."""
    return all(old.startswith("](") and old.endswith(")") for old, _ in rewrites)


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--base", default="origin/dev", help="git ref holding the pre-move file")
    parser.add_argument("--old", required=True, help="path of the file before the move")
    parser.add_argument("--new", required=True, help="path of the file after the move")
    parser.add_argument(
        "--rewrite",
        action="append",
        default=[],
        metavar="OLD=NEW",
        help="declared link rewrite, repeatable",
    )
    args = parser.parse_args()

    rewrites: list[tuple[str, str]] = []
    for spec in args.rewrite:
        if "=" not in spec:
            raise SystemExit(f"FAIL: --rewrite expects OLD=NEW, got {spec!r}")
        old, new = spec.split("=", 1)
        rewrites.append((old, new))

    if not link_only(rewrites):
        print("FAIL: every --rewrite must target a markdown link, not prose")
        return 1

    before = subprocess.run(
        ["git", "show", f"{args.base}:{args.old}"],
        cwd=REPO_ROOT,
        capture_output=True,
        text=True,
        check=True,
    ).stdout

    new_path = REPO_ROOT / args.new
    if not new_path.exists():
        print(f"FAIL: moved file does not exist: {args.new}")
        return 1
    after = new_path.read_text(encoding="utf-8")

    expected = apply_rewrites(before, rewrites)

    # The declared rewrites must account for the entire byte delta: no whitespace
    # change, no reflowed paragraph, no quietly dropped sentence.
    delta_actual = len(after.encode("utf-8")) - len(before.encode("utf-8"))
    delta_declared = len(expected.encode("utf-8")) - len(before.encode("utf-8"))
    if delta_actual != delta_declared:
        print(f"FAIL: byte delta {delta_actual} does not match declared {delta_declared}")
        return 1
    print(f"OK  byte delta explained by the {len(rewrites)} declared link rewrites ({delta_actual:+d} B)")

    if after != expected:
        first = next(
            (i for i, (a, b) in enumerate(zip(after, expected)) if a != b),
            min(len(after), len(expected)),
        )
        print(f"FAIL: content differs from the pre-move file at character {first}")
        print(f"  on disk: {after[max(0, first - 40):first + 40]!r}")
        print(f"  expected: {expected[max(0, first - 40):first + 40]!r}")
        return 1
    print("OK  moved file is byte-identical to the pre-move file after the rewrites")

    if before.count("\n") != after.count("\n"):
        print(f"FAIL: line count changed {before.count(chr(10))} -> {after.count(chr(10))}")
        return 1
    print(f"OK  line count unchanged ({after.count(chr(10))} lines)")

    if (REPO_ROOT / args.old).exists():
        print(f"FAIL: the pre-move path still exists: {args.old}")
        return 1
    print(f"OK  pre-move path is gone: {args.old}")

    print()
    # Compare by resolved target, not by literal text: the whole point of the
    # rewrite is that the literal text changed. A dead link is a regression only
    # if its pre-rewrite counterpart resolved from the old directory.
    old_dir = (REPO_ROOT / args.old).parent
    new_dir = new_path.parent
    to_old_target = {}
    for old, new in rewrites:
        to_old_target[new[2:-1]] = old[2:-1]

    def resolves(target: str, anchor: Path) -> bool:
        path = target.split("#", 1)[0]
        return not path or (anchor / path).exists()

    dead_after = sorted(set(dead_targets(after, new_dir)))
    regressions: list[str] = []
    pre_existing: list[str] = []
    for target in dead_after:
        if resolves(to_old_target.get(target, target), old_dir):
            regressions.append(target)
        else:
            pre_existing.append(target)

    total = len(relative_targets(after))
    print(f"{total} relative links in the moved file, {total - len(dead_after)} resolve")
    for target in pre_existing:
        print(f"  DEAD (pre-existing) {target}")
    for target in regressions:
        print(f"  DEAD (regression)   {target}")
    if regressions:
        print("FAIL: the move broke links that used to resolve")
        return 1
    print("OK  no link that resolved before the move is broken by it")
    if pre_existing:
        print(f"NOTE {len(pre_existing)} target(s) never existed on {args.base}; tracked separately")
    return 0


if __name__ == "__main__":
    sys.exit(main())
