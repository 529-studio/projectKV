#!/usr/bin/env python3
"""Keep README.vi.md a quickstart instead of letting it become a translation.

`README.vi.md` is deliberately partial: prerequisites, launch commands and
service ports, plus pointers into the canonical English `README.md`. A full
translation was tried and drifted within four days, so AGENTS.md Rule 6 now
requires only the operational facts to stay in sync.

That promise is worth exactly as much as the check behind it. This tool enforces
four things:

1. Every `make <target>` the Vietnamese file tells a reader to run exists in the
   Makefile. A quickstart that invents a command is worse than no quickstart.
2. Every `localhost:<port>` it prints appears in `README.md`.
3. Every port-table row `(port, url)` it prints appears in `README.md`.
4. The file stays under its byte budget, so nobody quietly re-adds the 18 KB of
   translated prose that used to live here.

Usage:
    python3 tools/docs/check_readme_vi_parity.py
    python3 tools/docs/check_readme_vi_parity.py --budget 8192

Exit code 0 means the quickstart is consistent with the canonical README.
"""

from __future__ import annotations

import argparse
import re
import sys
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parents[2]
CANONICAL = REPO_ROOT / "README.md"
QUICKSTART = REPO_ROOT / "README.vi.md"
MAKEFILE = REPO_ROOT / "Makefile"

MAKE_RE = re.compile(r"\bmake\s+([a-zA-Z0-9][a-zA-Z0-9_.-]*)")
PORT_RE = re.compile(r"localhost:(\d{2,5})")
TABLE_ROW_RE = re.compile(r"^\|\s*([^|]+?)\s*\|\s*`(\d{2,5})`\s*\|\s*(http\S+)\s*\|", re.MULTILINE)
LINK_RE = re.compile(r"\]\(([^)\s]+)\)")
SCHEME_RE = re.compile(r"^[a-z][a-z0-9+.-]*:", re.IGNORECASE)
SWITCHER = "[English](README.md#english) &nbsp;•&nbsp; [Tiếng Việt](README.vi.md)"


def slugify(heading: str) -> str:
    """Approximate GitHub's heading anchor slug for a plain heading."""
    text = heading.strip().lower()
    text = re.sub(r"[^\w\s-]", "", text)
    return re.sub(r"\s+", "-", text).strip("-")


def make_targets() -> set[str]:
    targets: set[str] = set()
    for line in MAKEFILE.read_text(encoding="utf-8").splitlines():
        if line.startswith("\t") or line.startswith("#") or not line.strip():
            continue
        match = re.match(r"^([a-zA-Z0-9][a-zA-Z0-9_.-]*)\s*:(?!=)", line)
        if match:
            targets.add(match.group(1))
    return targets


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--budget", type=int, default=8192, help="maximum size of README.vi.md in bytes")
    args = parser.parse_args()

    canonical = CANONICAL.read_text(encoding="utf-8")
    quickstart = QUICKSTART.read_text(encoding="utf-8")
    size = len(QUICKSTART.read_bytes())
    failures: list[str] = []

    # 1. every `make <target>` must be real
    known = make_targets()
    invoked = sorted(set(MAKE_RE.findall(quickstart)))
    missing = [t for t in invoked if t not in known]
    if missing:
        failures.append(f"make targets not in Makefile: {missing}")
    else:
        print(f"OK  {len(invoked)} make targets in the quickstart all exist ({', '.join(invoked)})")

    # 2. every localhost port must be documented in the canonical README
    quick_ports = set(PORT_RE.findall(quickstart))
    canonical_ports = set(PORT_RE.findall(canonical))
    undocumented = sorted(quick_ports - canonical_ports)
    if undocumented:
        failures.append(f"ports in README.vi.md but not in README.md: {undocumented}")
    else:
        print(f"OK  all {len(quick_ports)} ports in the quickstart are documented in README.md")

    # 3. every port-table row must exist in the canonical README
    quick_rows = {(port, url) for _, port, url in TABLE_ROW_RE.findall(quickstart)}
    canonical_rows = {(port, url) for _, port, url in TABLE_ROW_RE.findall(canonical)}
    if not quick_rows:
        failures.append("README.vi.md has no port table rows to verify")
    else:
        orphans = sorted(quick_rows - canonical_rows)
        if orphans:
            failures.append(f"port-table rows not in README.md: {orphans}")
        else:
            print(f"OK  all {len(quick_rows)} port-table rows match README.md exactly")

    # 4. the file must stay a quickstart
    if size > args.budget:
        failures.append(
            f"README.vi.md is {size} B, over the {args.budget} B quickstart budget "
            f"(a full translation was ~20 KB and drifted within days)"
        )
    else:
        print(f"OK  README.vi.md is {size} B, within the {args.budget} B quickstart budget")

    # 5. relative links resolve
    dead = []
    for target in LINK_RE.findall(quickstart):
        if target.startswith("#") or SCHEME_RE.match(target):
            continue
        path = target.split("#", 1)[0]
        if path and not (QUICKSTART.parent / path).exists():
            dead.append(target)
    if dead:
        failures.append(f"dead relative links: {dead}")
    else:
        print("OK  every relative link in the quickstart resolves")

    # 6. both files carry the language switcher
    for name, text in (("README.md", canonical), ("README.vi.md", quickstart)):
        if SWITCHER not in text:
            failures.append(f"{name} is missing the language switcher")

    # 7. every in-file anchor the switcher uses must land on a real heading.
    #    The Vietnamese file is a quickstart, so its own anchor is the file root;
    #    only the English anchor is checked.
    english_slugs = {slugify(h) for h in re.findall(r"^#{1,6}\s+(.+)$", canonical, re.MULTILINE)}
    if "english" not in english_slugs:
        failures.append("README.md has no `# English` heading for the switcher anchor to land on")

    if failures:
        print()
        for failure in failures:
            print(f"FAIL: {failure}")
        return 1
    print("OK  language switcher present in both files")
    return 0


if __name__ == "__main__":
    sys.exit(main())
