#!/usr/bin/env bash
# Guard: fail when the working tree contains material that must never be public.
#
# Scans every tracked file for internal infrastructure references (host names,
# private mesh addresses, admin endpoints) and for credential material. Both
# scans run on the full tree so documentation cannot reintroduce what the
# runtime surface already removed.
set -euo pipefail

SCRIPT_PATH="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/$(basename "${BASH_SOURCE[0]}")"
REPO_ROOT="$(git -C "$(dirname "${SCRIPT_PATH}")" rev-parse --show-toplevel)"
cd "${REPO_ROOT}"

SELF="${SCRIPT_PATH#"${REPO_ROOT}/"}"
STATUS=0

fail() {
  echo "[ERR] $1"
  STATUS=1
}

scan() { # scan <label> <regex> <pathspec...>
  local label="$1"
  local pattern="$2"
  shift 2

  local hits
  hits=$(git grep -nE "$pattern" -- "$@" 2>/dev/null | grep -v "^${SELF}:" || true)
  if [ -n "$hits" ]; then
    fail "${label}"
    echo "${hits}" | head -n 20
    echo
  fi
}

echo "[INFO] Scanning every tracked file for internal infrastructure references..."
scan "internal host name, private mesh address or admin endpoint" \
  'edge host|edge host|inference worker|100\.(6[4-9]|[7-9][0-9]|1[01][0-9]|12[0-7])\.[0-9]+\.[0-9]+|portainer\.529studio\.site|n8n\.529studio\.site|hub\.529studio\.site' \
  .

echo "[INFO] Scanning every tracked file for credential material..."
scan "hardcoded credential material" \
  'ak_live_[A-Za-z0-9_]+|(ghp|gho|ghs|ghu)_[A-Za-z0-9]{20,}|sk-[A-Za-z0-9]{20,}|-----BEGIN [A-Z ]*PRIVATE KEY-----|AIza[0-9A-Za-z_-]{35}' \
  .

if [ "${STATUS}" -ne 0 ]; then
  echo "[ERR] Public-safety guard failed. Remove the flagged values or load them from secrets/variables."
  exit 1
fi

echo "[OK] Public-safety guard passed."
