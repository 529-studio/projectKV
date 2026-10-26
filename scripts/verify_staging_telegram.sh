#!/usr/bin/env bash
#
# verify_staging_telegram.sh — Live staging Telegram flood alert verification (Issue #246 / S8-T9).
#
# Pre-flight verification for the staging Telegram flood alert channel on the
# staging application stack. Performs, in order:
#   1. Fail-fast credential validation (TELEGRAM_BOT_TOKEN, TELEGRAM_CHAT_ID from
#      environment or repo-root .env — never hardcoded, per AGENTS.md Rule 8).
#   2. Bot API getMe round-trip: proves the token authenticates (HTTP 200, ok=true).
#   3. Message contract check: the composed alert mirrors notify.FormatTelegramMessage
#      for camera 586e2995f9fab7001111b0b5 (Ung Van Khiem - Nguyen Gia Tri, D2),
#      bilingual Vietnamese/English, strictly zero emojis per Rule 1.
#   4. sendMessage round-trip: proves live HTML delivery to the staging chat
#      (HTTP 200, ok=true).
#   5. (Optional) Staging go-server reachability probe when STAGING_BASE_URL is set.
#
# Environment variables:
#   TELEGRAM_BOT_TOKEN   Required. Bot token issued by @BotFather for staging.
#   TELEGRAM_CHAT_ID     Required. Chat ID of the staging verification group.
#   TELEGRAM_API_BASE    Optional. Defaults to https://api.telegram.org.
#                        Override with a local mock server for dry-run testing.
#   STAGING_BASE_URL     Optional. e.g. https://warded.529studio.site — when set,
#                        probes GET /api/flood/chokepoints for liveness.
#
# Usage:
#   ./scripts/verify_staging_telegram.sh
#   TELEGRAM_API_BASE=http://127.0.0.1:8081 ./scripts/verify_staging_telegram.sh  # mock dry-run
#
# Exit codes: 0 = all checks passed, 1 = one or more checks failed.
# The 30-minute sliding debouncer is a go-server in-process control and cannot be
# exercised over the Bot API; it is verified deterministically by:
#   go test -race ./cmd/server -run TestNotifyIntegration
# and observed on staging via the log line
#   "notify: flood alert suppressed by 30-minute debounce window"
# See docs/runbooks/telegram_staging_verification.md for the full procedure.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

# Staging verification camera (authentic municipal flood chokepoint fixture).
# CAMERA_DISTRICT mirrors the production dispatchFloodAlert value for this
# camera: district = cam.Zone, which resolves to the default traffic zone
# "Khu vực Nội thành TP.HCM" (ZoneNoiThanh) — NOT the administrative district
# from the chokepoints fixture. Pinned by TestFormatTelegramMessage_StagingChokepointFixture.
CAMERA_ID="586e2995f9fab7001111b0b5"
CAMERA_NAME="Ung Văn Khiêm - Nguyễn Gia Trí (D2)"
CAMERA_WARD="Thạnh Mỹ Tây"
CAMERA_DISTRICT="Khu vực Nội thành TP.HCM"
CAMERA_DEPTH_CM=35
SEVERITY_VI="NGẬP SÂU"
SEVERITY_EN="DEEP FLOOD"

API_BASE="${TELEGRAM_API_BASE:-https://api.telegram.org}"
CURL_OPTS=(--silent --show-error --max-time 15 --connect-timeout 5)

PASS_COUNT=0
FAIL_COUNT=0

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

# Load credentials from the gitignored repo-root .env when not already exported.
load_env_file() {
    if { [[ -z "${TELEGRAM_BOT_TOKEN:-}" ]] || [[ -z "${TELEGRAM_CHAT_ID:-}" ]]; } && [[ -f "$REPO_ROOT/.env" ]]; then
        set -a
        # shellcheck disable=SC1091
        source "$REPO_ROOT/.env"
        set +a
    fi
}

# Scrub the raw bot token from any diagnostic output before printing (Rule 8).
redact_token() {
    sed -e "s|${TELEGRAM_BOT_TOKEN:-__UNSET__}|[REDACTED_BOT_TOKEN]|g"
}

record_pass() {
    PASS_COUNT=$((PASS_COUNT + 1))
    printf '[PASS] %s\n' "$1"
}

record_fail() {
    FAIL_COUNT=$((FAIL_COUNT + 1))
    printf '[FAIL] %s\n' "$1"
}

json_field() {
    # json_field <file> <key> — prints the top-level JSON string field, empty on absent.
    python3 - "$1" "$2" <<'PY'
import json, sys
try:
    with open(sys.argv[1], encoding="utf-8") as fh:
        data = json.load(fh)
    value = data.get(sys.argv[2], "")
    print(value if isinstance(value, str) else json.dumps(value, ensure_ascii=False))
except Exception:
    print("")
PY
}

# ---------------------------------------------------------------------------
# Check 1 — Credential fail-fast validation
# ---------------------------------------------------------------------------
load_env_file

if [[ -z "${TELEGRAM_BOT_TOKEN:-}" ]]; then
    record_fail "TELEGRAM_BOT_TOKEN is not set. Export it or place it in $REPO_ROOT/.env (see .env.example)."
else
    record_pass "TELEGRAM_BOT_TOKEN is present (loaded from environment or .env)."
fi

if [[ -z "${TELEGRAM_CHAT_ID:-}" ]]; then
    record_fail "TELEGRAM_CHAT_ID is not set. Export it or place it in $REPO_ROOT/.env (see .env.example)."
else
    record_pass "TELEGRAM_CHAT_ID is present."
fi

if [[ "$FAIL_COUNT" -gt 0 ]]; then
    printf '\n%d check(s) failed. Aborting before any network round-trip.\n' "$FAIL_COUNT"
    exit 1
fi

WORKDIR="$(mktemp -d)"
trap 'rm -rf "$WORKDIR"' EXIT

# ---------------------------------------------------------------------------
# Check 2 — Bot API getMe (token authenticity)
# ---------------------------------------------------------------------------
GETME_BODY="$WORKDIR/getme.json"
GETME_CODE="$(curl "${CURL_OPTS[@]}" -o "$GETME_BODY" -w '%{http_code}' \
    "${API_BASE}/bot${TELEGRAM_BOT_TOKEN}/getMe" 2> >(redact_token >&2) || echo "000")"

if [[ "$GETME_CODE" == "200" && "$(json_field "$GETME_BODY" ok)" == "true" ]]; then
    BOT_USERNAME="$(json_field "$GETME_BODY" result | python3 -c 'import json,sys; print(json.load(sys.stdin).get("username",""))' 2>/dev/null || true)"
    record_pass "getMe authenticated (HTTP 200, ok=true, bot=@${BOT_USERNAME:-unknown})."
else
    record_fail "getMe failed (HTTP ${GETME_CODE}). Body: $(redact_token < "$GETME_BODY" | head -c 300)"
fi

# ---------------------------------------------------------------------------
# Check 3 — Message contract: bilingual, zero-emoji (Rule 1)
# ---------------------------------------------------------------------------
# Mirrors notify.FormatTelegramMessage output for this chokepoint. Keep in sync
# with TestFormatTelegramMessage_StagingChokepointFixture (internal/notify).
ICT_TIME="$(TZ=Asia/Ho_Chi_Minh date '+%H:%M %d/%m/%Y')"
LIVE_URL="https://warded.529studio.site/?cam=${CAMERA_ID}"
{
    printf '<b>[CẢNH BÁO NGẬP NƯỚC / FLOOD ALERT]</b>\n\n'
    printf '<b>Điểm ngập / Chokepoint:</b> %s\n' "$CAMERA_NAME"
    printf '<b>Khu vực / Area:</b> %s, %s\n' "$CAMERA_WARD" "$CAMERA_DISTRICT"
    printf '<b>Mức độ / Severity:</b> [%s / %s]\n' "$SEVERITY_VI" "$SEVERITY_EN"
    printf '<b>Độ sâu ước lượng / Depth:</b> ~%d cm\n' "$CAMERA_DEPTH_CM"
    printf '<b>Thời gian / Time:</b> %s (ICT)\n' "$ICT_TIME"
    printf '<b>Chi tiết / Live View:</b> <a href="%s">%s</a>' "$LIVE_URL" "$LIVE_URL"
} > "$WORKDIR/message.txt"

if python3 - "$WORKDIR/message.txt" <<'PY'
import re, sys
text = open(sys.argv[1], encoding="utf-8").read()
emoji = re.compile(
    "[\U0001F000-\U0001FAFF"     # pictographs, emoticons, transport, supplemental symbols
    "\U00002600-\U000027BF"      # misc symbols, dingbats
    "\U00002B00-\U00002BFF"      # arrows/stars block
    "\U0000FE00-\U0000FE0F"      # variation selectors (emoji modifiers)
    "\U00002190-\U000021FF"      # arrows
    "\U00002300-\U000023FF]"     # misc technical (clocks, weather glyphs)
)
found = emoji.findall(text)
if found:
    print("EMOJI_CODEPOINTS=" + ",".join("U+%04X" % ord(c) for c in found))
    sys.exit(1)
required = [
    "CẢNH BÁO NGẬP NƯỚC / FLOOD ALERT",
    "Điểm ngập / Chokepoint:",
    "Khu vực / Area:",
    "Mức độ / Severity:",
    "Thời gian / Time:",
    "Chi tiết / Live View:",
]
missing = [part for part in required if part not in text]
if missing:
    print("MISSING_SECTIONS=" + ",".join(missing))
    sys.exit(1)
sys.exit(0)
PY
then
    record_pass "Message contract holds (bilingual headers present, zero emojis)."
else
    record_fail "Message contract violated. Composed message rejected; refusing to send."
fi

if [[ "$FAIL_COUNT" -gt 0 ]]; then
    printf '\nAborting: refusing to dispatch a non-conforming message.\n'
    exit 1
fi

# ---------------------------------------------------------------------------
# Check 4 — sendMessage live delivery (HTTP 200 from Telegram Bot API)
# ---------------------------------------------------------------------------
SEND_BODY="$WORKDIR/send.json"
HTTP_CODE="$(python3 - "$WORKDIR/message.txt" "${TELEGRAM_CHAT_ID}" <<'PY' | curl "${CURL_OPTS[@]}" -o "$SEND_BODY" -w '%{http_code}' \
    -X POST -H 'Content-Type: application/json' --data-binary @- \
    "${API_BASE}/bot${TELEGRAM_BOT_TOKEN}/sendMessage" 2> >(redact_token >&2) || echo "000"
import json, sys
with open(sys.argv[1], encoding="utf-8") as fh:
    text = fh.read()
print(json.dumps({"chat_id": sys.argv[2], "text": text, "parse_mode": "HTML"}, ensure_ascii=False))
PY
)"

if [[ "$HTTP_CODE" == "200" && "$(json_field "$SEND_BODY" ok)" == "true" ]]; then
    record_pass "sendMessage delivered (HTTP 200, ok=true) to chat ${TELEGRAM_CHAT_ID}."
else
    record_fail "sendMessage failed (HTTP ${HTTP_CODE}). Body: $(redact_token < "$SEND_BODY" | head -c 300)"
fi

# ---------------------------------------------------------------------------
# Check 5 — Optional staging go-server liveness probe
# ---------------------------------------------------------------------------
if [[ -n "${STAGING_BASE_URL:-}" ]]; then
    CHOKEPOINTS_CODE="$(curl "${CURL_OPTS[@]}" -o "$WORKDIR/chokepoints.json" -w '%{http_code}' \
        "${STAGING_BASE_URL}/api/flood/chokepoints" || echo "000")"
    if [[ "$CHOKEPOINTS_CODE" == "200" ]]; then
        if grep -q "$CAMERA_ID" "$WORKDIR/chokepoints.json"; then
            record_pass "Staging flood chokepoints reachable and contain camera ${CAMERA_ID}."
        else
            record_fail "Staging chokepoints reachable but camera ${CAMERA_ID} missing from fixture."
        fi
    else
        record_fail "Staging probe failed: GET ${STAGING_BASE_URL}/api/flood/chokepoints returned HTTP ${CHOKEPOINTS_CODE}."
    fi
else
    printf '[SKIP] STAGING_BASE_URL not set; skipping go-server liveness probe.\n'
fi

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------
printf '\n%s\n' "------------------------------------------------------------"
printf 'Verification summary: %d passed, %d failed.\n' "$PASS_COUNT" "$FAIL_COUNT"
if [[ "$FAIL_COUNT" -gt 0 ]]; then
    printf 'Result: FAILED. Consult docs/runbooks/telegram_staging_verification.md.\n'
    exit 1
fi
printf 'Result: PASSED. Confirm the message in the staging Telegram group renders correctly (HTML, zero emojis).\n'
