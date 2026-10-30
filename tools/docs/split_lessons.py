#!/usr/bin/env python3
"""Split the monolithic lesson register into one file per lesson.

The split is mechanical: every lesson chunk is copied byte-for-byte from the
source document, so no prose can be paraphrased or silently dropped. `--check`
reassembles the parts and compares them against the source, byte for byte.

Usage:
    python3 tools/docs/split_lessons.py                    # write docs/lessons/*
    python3 tools/docs/split_lessons.py --check            # verify round-trip
    python3 tools/docs/split_lessons.py --source <path>    # use another source

Lesson numbers are preserved exactly as they appear in the source, including
gaps. They are referenced by number from other documents, so renumbering would
break those references.
"""

from __future__ import annotations

import argparse
import re
import sys
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parents[2]
DEFAULT_SOURCE = REPO_ROOT / "docs" / "lesson_learn.md"
DEFAULT_OUT = REPO_ROOT / "docs" / "lessons"

LESSON_RE = re.compile(r"^## Lesson (\d+): (.+)$", re.MULTILINE)
SEPARATOR_RE = re.compile(r"\n+-{3,}\n*$")
GUARDRAIL_LABELS = (
    "Mandatory Takeaway",
    "Mandatory Takeaways",
    "Resolution",
    "Root Cause & Engineering Resolution",
    "Architectural Conflict",
)


def slugify(title: str, limit: int = 60) -> str:
    text = title.lower()
    text = re.sub(r"[`*_]", "", text)
    text = re.sub(r"[^a-z0-9]+", "-", text)
    text = re.sub(r"-{2,}", "-", text).strip("-")
    if len(text) > limit:
        text = text[:limit].rsplit("-", 1)[0].strip("-")
    return text or "lesson"


def split_source(text: str) -> tuple[str, list[tuple[int, str, str]]]:
    """Return (preamble, [(number, title, chunk)]).

    Each chunk keeps its own `## Lesson ...` heading verbatim and, except for
    the final lesson, ends with the document separator.
    """
    matches = list(LESSON_RE.finditer(text))
    if not matches:
        raise SystemExit("no '## Lesson N: <title>' headings found in source")

    preamble = text[: matches[0].start()]
    chunks: list[tuple[int, str, str]] = []
    for index, match in enumerate(matches):
        start = match.start()
        end = matches[index + 1].start() if index + 1 < len(matches) else len(text)
        chunks.append((int(match.group(1)), match.group(2).strip(), text[start:end]))
    return preamble, chunks


def strip_separator(chunk: str) -> str:
    body = SEPARATOR_RE.sub("", chunk)
    return body if body.endswith("\n") else body + "\n"


def first_guardrail(body: str, limit: int = 100) -> str:
    """Extract a one-line guardrail from the lesson body, mechanically."""
    lines = body.splitlines()
    start = None
    for index, line in enumerate(lines):
        if line.startswith("- **") and line[4:].split("**", 1)[0].startswith(GUARDRAIL_LABELS):
            start = index
            break
    if start is None:
        return "See lesson."

    collected: list[str] = []
    inline = lines[start].split("**", 2)[-1].lstrip(" :")
    if inline.strip():
        collected.append(inline)
    for follow in lines[start + 1 :]:
        stripped = follow.strip()
        if not stripped:
            if collected:
                break
            continue
        if stripped.startswith(("- **", "## ")):
            break
        stripped = re.sub(r"^(\d+\.|[-*])\s*", "", stripped)
        if not stripped:
            continue
        collected.append(stripped)
        if sum(len(part) for part in collected) > limit * 2:
            break

    text = " ".join(collected)
    text = re.sub(r"\*\*(.+?)\*\*", r"\1", text)
    text = re.sub(r"`([^`]*)`", r"\1", text)
    text = re.sub(r"\s+", " ", text).strip()
    if not text:
        return "See lesson."
    text = text.split(". ")[0].rstrip(".")
    if len(text) > limit:
        text = text[:limit].rsplit(" ", 1)[0].rstrip(",;:") + "..."
    return text.replace("|", "\\|")


def write_parts(source_text: str, out_dir: Path) -> list[dict]:
    _, chunks = split_source(source_text)
    out_dir.mkdir(parents=True, exist_ok=True)
    for stale in out_dir.glob("L[0-9][0-9]-*.md"):
        stale.unlink()

    records: list[dict] = []
    for number, title, chunk in chunks:
        name = f"L{number:02d}-{slugify(title)}.md"
        body = strip_separator(chunk)
        (out_dir / name).write_text(body, encoding="utf-8")
        records.append(
            {"number": number, "title": title, "file": name, "guardrail": first_guardrail(body)}
        )
    write_index(out_dir, records)
    return records


def write_index(out_dir: Path, records: list[dict]) -> None:
    numbers = [record["number"] for record in records]
    missing = sorted(set(range(1, max(numbers) + 1)) - set(numbers))

    lines = [
        "# Lessons Index",
        "",
        "One row per engineering lesson. The full text lives in the linked file; this index",
        "is the only part an agent should read by default.",
        "",
        "| # | Lesson | Mandatory guardrail | File |",
        "|---|---|---|---|",
    ]
    for record in records:
        lines.append(
            f"| {record['number']} | {record['title'].replace('|', chr(92) + '|')} "
            f"| {record['guardrail']} | [L{record['number']:02d}]({record['file']}) |"
        )
    if missing:
        lines += [
            "",
            f"> Numbering note: Lesson {', '.join(str(n) for n in missing)} "
            "does not exist in the source register. Numbers are preserved as-is because",
            "> other documents reference lessons by number; nothing has been renumbered.",
        ]
    lines.append("")
    (out_dir / "INDEX.md").write_text("\n".join(lines), encoding="utf-8")


def check(source_text: str, out_dir: Path) -> int:
    _, chunks = split_source(source_text)
    parts = sorted(out_dir.glob("L[0-9][0-9]-*.md"))
    if len(parts) != len(chunks):
        print(f"FAIL: {len(parts)} part files for {len(chunks)} lessons in the source")
        return 1

    by_number = {int(part.name[1:3]): part for part in parts}
    if len(by_number) != len(parts):
        print("FAIL: duplicate lesson numbers in file names")
        return 1

    mismatches: list[str] = []
    body_bytes = 0
    for number, title, chunk in chunks:
        part = by_number.get(number)
        if part is None:
            mismatches.append(f"L{number:02d}: missing part file")
            continue
        expected = strip_separator(chunk).rstrip("\n")
        actual = part.read_text(encoding="utf-8").rstrip("\n")
        body_bytes += len(expected)
        if actual == expected:
            continue
        for offset, (left, right) in enumerate(zip(actual, expected)):
            if left != right:
                mismatches.append(
                    f"{part.name}: first difference at byte {offset}: "
                    f"{actual[max(0, offset - 40):offset + 40]!r} != "
                    f"{expected[max(0, offset - 40):offset + 40]!r}"
                )
                break
        else:
            mismatches.append(f"{part.name}: length {len(actual)} != expected {len(expected)}")

    if mismatches:
        print(f"FAIL: {len(mismatches)} of {len(parts)} lessons differ")
        for line in mismatches[:10]:
            print(f"  {line}")
        return 1

    print(
        f"OK: {len(chunks)}/{len(chunks)} lesson bodies identical to the source "
        f"({body_bytes} bytes of prose, separators normalised, source order preserved)"
    )
    return 0


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--source", type=Path, default=DEFAULT_SOURCE)
    parser.add_argument("--out", type=Path, default=DEFAULT_OUT)
    parser.add_argument("--check", action="store_true")
    args = parser.parse_args()

    if not args.source.exists():
        print(
            f"error: source not found: {args.source}\n"
            "hint: after the monolith is deleted, pass a historical copy, e.g.\n"
            "  git show <base-sha>:docs/lesson_learn.md > /tmp/lesson_learn.md\n"
            "  python3 tools/docs/split_lessons.py --check --source /tmp/lesson_learn.md",
            file=sys.stderr,
        )
        return 2

    source_text = args.source.read_text(encoding="utf-8")
    if args.check:
        return check(source_text, args.out)

    records = write_parts(source_text, args.out)
    print(f"wrote {len(records)} lesson files + INDEX.md to {args.out}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
