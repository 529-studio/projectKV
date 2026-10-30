# Telegram Flood Alert Staging Verification & notify-preview Runbook

## Overview

This runbook documents how to preview and verify Telegram flood alert notifications using the production formatting engine, eliminating discrepancies between verification scripts and backend runtime alerts.

Issue Context: S9-T5 (#301) / ADR-011.

## Prerequisites

1. Go 1.26+ installed and available in `$PATH`.
2. Camera catalog present (defaults to `data/cameras_enriched.json` or path specified via `CAMERAS_FILE_PATH`).
3. Chokepoint fixtures available at `tools/eval/fixtures/flood_chokepoints.json`.
4. Bash & Python 3 for executing `scripts/verify_staging_telegram.sh`.

## notify-preview CLI

The `notify-preview` CLI generates production-exact flood alert messages by running the same alert builder and HTML formatter used by `dispatchFloodAlert` in `services/go-server/cmd/server/main.go`.

### Usage

```bash
# Preview deep flood alert for a camera:
go run -C services/go-server ./cmd/notify-preview -camera-id 586e2995f9fab7001111b0b5 -severity FLOODED_DEEP

# Preview shallow flood alert with custom RFC3339 timestamp:
go run -C services/go-server ./cmd/notify-preview -camera-id 586e2995f9fab7001111b0b5 -severity FLOODED_SHALLOW -observed-at 2026-10-30T14:30:00+07:00
```

### CLI Arguments

- `-camera-id` (string, required): Camera identifier in catalog.
- `-severity` (string, default `FLOODED_DEEP`): Severity level (`WET`, `FLOODED_SHALLOW`, `FLOODED_DEEP`).
- `-observed-at` (string, optional): RFC3339 formatted observation time (defaults to `time.Now()`).

### Important Semantics: Area Line (Zone vs District)

The Area line (`Khu vực / Area`) renders:
`<Ward>, <District/Zone>`

In production, `District` is mapped to `cam.Zone` (the municipal traffic zone classification, e.g. `Khu vực Nội thành TP.HCM`), **not** the administrative district from the chokepoints fixture. `notify-preview` uses this exact logic to guarantee byte parity.

## Staging Verification Script

`scripts/verify_staging_telegram.sh` runs pre-flight end-to-end checks before deploying flood alerts to staging Telegram channels:

1. Validates `TELEGRAM_BOT_TOKEN` and `TELEGRAM_CHAT_ID` exist in environment or `.env`.
2. Validates Bot API token authenticity via `getMe`.
3. Runs `notify-preview` to generate the message and asserts zero-emoji (Rule 1) and bilingual format.
4. Posts a live test alert to the target chat ID.
5. (Optional) Checks staging base URL reachability when `STAGING_BASE_URL` is set.

### Running with a local mock:

```bash
TELEGRAM_BOT_TOKEN=fake_token TELEGRAM_CHAT_ID=12345 \
  TELEGRAM_API_BASE=http://127.0.0.1:19999 \
  bash scripts/verify_staging_telegram.sh
```
