# Lesson 86: RFC3339Nano 9-Digit Timestamps, Cross-Runtime Serialization, and Microsecond Pre-normalization

## Navigation Aid & Quick Reference
- **Domain**: Cross-language serialization, timestamp parsing, Go to Python interop, SQLite lexical ordering, Python datetime limitations.
- **Relevant Issues**: Issue #422, Lesson L07, Lesson L78, Lesson L80.
- **Key Files**:
  - `tools/forecast/extract_features.py` (`parse_timestamp`, `_FRACTIONAL_SECONDS`)
  - `tools/forecast/backtest.py` (`parse_utc_timestamp`, `_FRACTIONAL_SECONDS`)
  - `tools/eval/ingest_proof_archive.py` (`parse_timestamp`, `_FRACTIONAL_SECONDS`)
  - `tools/ci/check_evidence_integrity.py` (`parse_iso_timestamp`)
  - `services/go-server/internal/store/history_repo.go` (`TimestampFixedLayout`)
- **Core Invariants**:
  - **Lexical Sorting Equivalence**: Go and SQLite persist observations using fixed-width 9-digit nanosecond strings (`2006-01-02T15:04:05.000000000Z`) so ASCII lexical ordering (`ORDER BY observed_at`) is strictly identical to chronological ordering.
  - **Python Microsecond Truncation**: Python runtimes prior to 3.11 (such as macOS system Python 3.9) reject ISO timestamps with more than 6 fractional digits in `datetime.fromisoformat()` and `datetime.strptime("%f")`. Pre-normalizing via `re.sub(r'(\.\d{6})\d+', r'\1', text)` preserves microsecond resolution and strictly maintains UTC timezone offsets.
  - **Synchronized Tooling Parity**: All offline ingestion, feature extraction, backtesting, and CI verification scripts must accept the canonical 9-digit timestamp layout without dropping valid observations.

---

## 1. Context & Problem Statement

In the Go backend modulith, `store.VerdictHistoryRepo` stores verdict history records in SQLite hotset tables. To guarantee that SQLite index scans and `ORDER BY observed_at` queries sort chronologically using native ASCII string comparison without invoking expensive custom collations or date functions, timestamps are formatted with a fixed 9-digit fraction:
```go
const TimestampFixedLayout = "2006-01-02T15:04:05.000000000Z"
```

However, downstream Python pipelines running in environments prior to Python 3.11 (such as Python 3.9 on macOS developer machines and standard CI runners) rely on `datetime.fromisoformat()` and `datetime.strptime()`. In Python 3.9:
```python
datetime.fromisoformat("2026-11-06T10:00:00.000000000+00:00")
# Raises ValueError: Invalid isoformat string: '2026-11-06T10:00:00.000000000+00:00'
```
Similarly, `strptime` with `%f` only matches up to 6 digits, failing on 9-digit strings with `unconverted data remains`.

When `tools/forecast/extract_features.py` queried historical observations, `parse_timestamp()` failed on every row stored by Go, causing `load_sqlite_observations()` to increment the `invalid` counter and silently drop valid historical observations. This led to test failure in `test_load_sqlite_observations_directional_compatibility` and compromised live model refits.

---

## 2. Core Architectural Decisions

### A. Pre-normalization via Compiled Microsecond Truncation
Rather than attempting complex format parsing or requiring Python 3.11+ across all developer workstations and host platforms, we pre-normalize timestamp strings before passing them to `datetime.fromisoformat()` or format matchers:
```python
_FRACTIONAL_SECONDS = re.compile(r"(\.\d{6})\d+")
text = _FRACTIONAL_SECONDS.sub(r"\1", text)
```
This regex captures the decimal point and the first 6 digits (`\.\d{6}`), discarding any subsequent fractional digits (`\d+`). Strings with 6 or fewer digits (e.g. microseconds, milliseconds, or whole seconds) remain untouched.

### B. Preserving UTC Invariants & Timezone Offsets
The normalization executes after stripping monotonic clock suffixes (`m=+...`) and standardizing trailing `Z` markers to `+00:00`. The fractional truncation does not modify timezone offsets (e.g. `+07:00` or `+00:00`). After parsing, `astimezone(timezone.utc)` converts naive or local timestamps into timezone-aware UTC datetimes.

### C. Holistic Tooling Parity
The pre-normalization was synchronized across all Python tooling reading observation timestamps:
- `tools/forecast/extract_features.py`: SQLite history and label parsing.
- `tools/forecast/backtest.py`: Feature CSV time bin parsing (`bin_start_utc`).
- `tools/eval/ingest_proof_archive.py`: Historical proof archive replay into SQLite.
- `tools/ci/check_evidence_integrity.py`: CI audit of proof archive manifests and telemetry timestamps.

---

## 3. Verification & Guardrails

- `make test-forecast`: 145 unit and integration tests passing 100%, including `test_load_sqlite_observations_directional_compatibility` and new explicit 9-digit RFC3339Nano test cases.
- Dedicated unit tests covering:
  - Fixed-layout 9-digit zero-padded nanoseconds (`2026-11-06T10:00:00.000000000Z`).
  - Fractional nanoseconds truncated to microseconds (`2026-11-06T10:00:00.123456789Z` -> `123456`).
  - Explicit non-UTC offsets (`2026-11-06T17:00:00.123456789+07:00`).
  - Go monotonic suffixes combined with nanoseconds (`m=+0.944197834`).
- End-to-end SQLite extraction and backtest pipeline execution verified with zero dropped invalid rows (`invalid_rows: 0`).
