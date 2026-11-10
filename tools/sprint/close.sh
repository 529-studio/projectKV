#!/usr/bin/env bash
# Close a sprint boundary: validate the handoff, then open the next conversation.
#
#   tools/sprint/close.sh                 # validate + print the bootstrap prompt
#   tools/sprint/close.sh --launch        # same, then start a NEW agy conversation
#   tools/sprint/close.sh --sprint 12     # explicit sprint number
#   tools/sprint/close.sh --init          # write the handoff skeleton, missing sections only
#
# AGENTS.md #12: a conversation covers one sprint. This is the sanctioned way to
# start the next one, so the handoff cannot be skipped by accident.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$REPO_ROOT"

SPRINT=""
LAUNCH=0
INIT=0

while [ $# -gt 0 ]; do
  case "$1" in
    --sprint) SPRINT="${2:?--sprint needs a number}"; shift 2 ;;
    --launch) LAUNCH=1; shift ;;
    --init) INIT=1; shift ;;
    -h|--help) sed -n '2,11p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) echo "unknown argument: $1" >&2; exit 2 ;;
  esac
done

if [ -z "$SPRINT" ]; then
  branch="$(git rev-parse --abbrev-ref HEAD 2>/dev/null || true)"
  SPRINT="$(printf '%s' "$branch" | sed -n 's/.*[/_-]s\([0-9][0-9]*\)[/_.-].*/\1/p' | head -1)"
fi
if [ -z "$SPRINT" ]; then
  latest="$( (ls -1 docs/HANDOFF-s*.md 2>/dev/null || true) \
    | sed -n 's/.*HANDOFF-s\([0-9][0-9]*\)\.md/\1/p' | sort -n | tail -1)"
  SPRINT="${latest:-}"
fi
if [ -z "$SPRINT" ]; then
  echo "Cannot infer the sprint number. Pass --sprint <N>." >&2
  exit 2
fi

HANDOFF="docs/HANDOFF-s${SPRINT}.md"
TEMPLATE=".agents/skills/sprint-close/templates/handoff.md"

if [ "$INIT" -eq 1 ] && [ ! -f "$HANDOFF" ]; then
  sed "s/<N>/${SPRINT}/g" "$TEMPLATE" > "$HANDOFF"
  echo "created $HANDOFF from $TEMPLATE - fill it in, then re-run without --init"
  exit 0
fi

fail() { echo "FAIL: $*" >&2; exit 1; }

[ -f "$HANDOFF" ] || fail "$HANDOFF is missing - run the sprint-close skill, or --init for a skeleton"

size=$(wc -c < "$HANDOFF" | tr -d ' ')
[ "$size" -ge 1200 ] || fail "$HANDOFF is only ${size} B - a handoff that short cannot carry the sprint"
[ "$size" -le 12288 ] || fail "$HANDOFF is ${size} B, over the 12 KB handoff ceiling"

if grep -q '<YYYY-MM-DD>' "$HANDOFF"; then
  fail "$HANDOFF is still the template skeleton - fill every section"
fi

sections=$(grep -c '^## ' "$HANDOFF" || true)
[ "$sections" -ge 4 ] || fail "$HANDOFF has ${sections} sections - expected the numbered handoff structure"

if ! grep -qi '^## .*\(next\|horizon\)' "$HANDOFF"; then
  fail "$HANDOFF has no next-sprint section - the next conversation has nowhere to start"
fi

git ls-files --error-unmatch "$HANDOFF" >/dev/null 2>&1 \
  || fail "$HANDOFF is untracked - commit it with the sprint close (AGENTS.md #5, #12)"

if [ -n "$(git status --porcelain -- "$HANDOFF")" ]; then
  fail "$HANDOFF has uncommitted changes - commit it first"
fi

report="docs/reports/sprint${SPRINT}_report.md"
[ -f "$report" ] || echo "WARN: $report not found - close the sprint report before the handoff" >&2

PROMPT="Read AGENTS.md (rules 2, 11, 12), then ${HANDOFF}. This conversation starts sprint $((SPRINT + 1)) for ProjectKV. Confirm in one short paragraph: current branch, what sprint ${SPRINT} shipped, and the first task from the handoff's next-sprint section. Do not re-plan sprint ${SPRINT} and do not re-derive decisions recorded in the handoff; if a fact looks wrong, say so instead of assuming."

printf '\nSprint %s handoff OK (%s B, %s sections, tracked, committed).\n\n' "$SPRINT" "$size" "$sections"
printf 'Bootstrap prompt for the next conversation:\n\n%s\n\n' "$PROMPT"
printf 'Start it with: tools/sprint/close.sh --launch\n\n'

if [ "$LAUNCH" -eq 1 ]; then
  command -v agy >/dev/null 2>&1 || fail "agy is not on PATH"
  exec agy -i "$PROMPT"
fi
