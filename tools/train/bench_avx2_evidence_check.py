#!/usr/bin/env python3
"""bench_avx2_evidence_check.py — post-run validation of the AVX2 latency evidence.

The harness writes the evidence document; this checker decides whether the
document is the one the sprint gate demands (S12-T5 §2.9 / S13-T1 #438):

* the fixed protocol ran — 3 repeats x 200 timed runs after 10 warm-ups at
  416 px, i.e. 600 delivered measurements;
* the head is ``trained`` and the host is a native x86-64 AVX2 machine;
* the gate verdict agrees with the reported p95 and the 25.0 ms ceiling;
* every ``*_at`` / ``*_timestamp`` field is timezone-aware and not in the
  future (ADR-013 D1);
* nothing in the payload claims a synthetic data source.

It is executed inside the benchmark image by
``tools/train/bench_avx2_docker.sh`` after the run, and imported directly by the
contract tests, so the acceptance rule is one implementation rather than a
shell copy.

Usage:
    python3 bench_avx2_evidence_check.py --file docs/reports/s13_t1_latency_avx2.json

Exit codes: 0 evidence accepted, 3 evidence rejected, 1 unreadable input.
"""

from __future__ import annotations

import argparse
import json
import math
import sys
from datetime import datetime, timedelta, timezone
from pathlib import Path
from typing import Any, Dict, List, Optional, Sequence, Tuple

EXIT_OK = 0
EXIT_UNREADABLE = 1
EXIT_REJECTED = 3

REQUIRED_TIMED_RUNS = 200
REQUIRED_REPEATS = 3
REQUIRED_WARMUP = 10
GATE_RESOLUTION = 416
SECONDARY_RESOLUTION = 320
GATE_THRESHOLD_MS = 25.0
EXPECTED_MEASUREMENTS = REQUIRED_TIMED_RUNS * REQUIRED_REPEATS
FUTURE_SKEW_SECONDS = 5
ACCEPTED_MACHINES = {"x86_64", "amd64", "x64"}
SYNTHETIC_SOURCES = {"synthetic", "mock", "template", "fixture"}

REQUIRED_TOP_LEVEL = (
    "version",
    "generated_at",
    "protocol",
    "head_status",
    "ir_sha256",
    "host",
    "latency",
    "repeats",
    "gate",
)


def _walk_timestamps(payload: Any, prefix: str = "") -> List[Tuple[str, str]]:
    found: List[Tuple[str, str]] = []
    if isinstance(payload, dict):
        for key, value in payload.items():
            path = f"{prefix}.{key}" if prefix else str(key)
            if isinstance(value, str) and (
                key.endswith("_at") or key.endswith("_timestamp")
            ):
                found.append((path, value))
            else:
                found.extend(_walk_timestamps(value, path))
    elif isinstance(payload, list):
        for index, value in enumerate(payload):
            found.extend(_walk_timestamps(value, f"{prefix}[{index}]"))
    return found


def _walk_source_claims(payload: Any, prefix: str = "") -> List[Tuple[str, str]]:
    found: List[Tuple[str, str]] = []
    if isinstance(payload, dict):
        for key, value in payload.items():
            path = f"{prefix}.{key}" if prefix else str(key)
            if key in {"fit_source", "source", "dataset_source"} and isinstance(
                value, str
            ):
                found.append((path, value))
            else:
                found.extend(_walk_source_claims(value, path))
    elif isinstance(payload, list):
        for index, value in enumerate(payload):
            found.extend(_walk_source_claims(value, f"{prefix}[{index}]"))
    return found


def _parse_timestamp(value: str) -> Optional[datetime]:
    try:
        parsed = datetime.fromisoformat(str(value).replace("Z", "+00:00"))
    except ValueError:
        return None
    if parsed.tzinfo is None:
        return None
    return parsed


def _is_number(value: Any) -> bool:
    return (
        isinstance(value, (int, float))
        and not isinstance(value, bool)
        and math.isfinite(float(value))
    )


def validate(
    payload: Dict[str, Any], now: Optional[datetime] = None
) -> List[str]:
    """Return every reason the payload is not gate-eligible evidence."""
    failures: List[str] = []
    if not isinstance(payload, dict):
        return ["evidence payload is not a JSON object"]

    for key in REQUIRED_TOP_LEVEL:
        if key not in payload:
            failures.append(f"payload is missing required field '{key}'")

    protocol = payload.get("protocol")
    if not isinstance(protocol, dict):
        failures.append("protocol block is missing or not an object")
        protocol = {}

    expected_protocol = {
        "timed_runs": REQUIRED_TIMED_RUNS,
        "repeats": REQUIRED_REPEATS,
        "warmup_runs": REQUIRED_WARMUP,
        "imgsz": GATE_RESOLUTION,
        "gate_resolution": GATE_RESOLUTION,
        "secondary_resolution": SECONDARY_RESOLUTION,
        "compile_device": "CPU",
    }
    for key, expected in expected_protocol.items():
        actual = protocol.get(key)
        if actual != expected:
            failures.append(
                f"protocol.{key} is {actual!r}, expected {expected!r}: a shorter or "
                f"differently shaped run is not the 3 x 200 gate"
            )

    if payload.get("head_status") != "trained":
        failures.append(
            f"head_status is {payload.get('head_status')!r}; only a trained head "
            f"produces meaningful latency evidence"
        )

    ir_sha = payload.get("ir_sha256")
    if not isinstance(ir_sha, str) or len(ir_sha) != 64 or any(
        character not in "0123456789abcdef" for character in ir_sha.lower()
    ):
        failures.append("ir_sha256 is not a 64-character hexadecimal digest")

    host = payload.get("host")
    if not isinstance(host, dict):
        failures.append("host block is missing or not an object")
    else:
        if host.get("avx2") is not True:
            failures.append("host.avx2 is not true; a non-AVX2 host is not gate-eligible")
        machine = str(host.get("machine", "")).lower()
        if machine not in ACCEPTED_MACHINES:
            failures.append(
                f"host.machine is {host.get('machine')!r}, not an x86-64 machine"
            )
        if not str(host.get("cpu_model", "")).strip():
            failures.append("host.cpu_model is empty; the measured CPU is unidentified")

    repeats = payload.get("repeats")
    if not isinstance(repeats, list) or len(repeats) != REQUIRED_REPEATS:
        failures.append(
            f"repeats holds {len(repeats) if isinstance(repeats, list) else 'no'} "
            f"series; the gate requires exactly {REQUIRED_REPEATS}"
        )
    else:
        for index, series in enumerate(repeats):
            count = series.get("n") if isinstance(series, dict) else None
            if count != REQUIRED_TIMED_RUNS:
                failures.append(
                    f"repeat[{index}] reports n={count!r}, expected "
                    f"{REQUIRED_TIMED_RUNS} timed runs"
                )

    latency = payload.get("latency")
    p95: Optional[float] = None
    if not isinstance(latency, dict):
        failures.append("latency block is missing or not an object")
    else:
        if latency.get("n") != EXPECTED_MEASUREMENTS:
            failures.append(
                f"latency.n is {latency.get('n')!r}, expected "
                f"{EXPECTED_MEASUREMENTS} ({REQUIRED_REPEATS} x {REQUIRED_TIMED_RUNS})"
            )
        if not _is_number(latency.get("p95_ms")):
            failures.append("latency.p95_ms is missing or not a finite number")
        else:
            p95 = float(latency["p95_ms"])
        worst = latency.get("worst_repeat_p95_ms")
        if not _is_number(worst) or (p95 is not None and float(worst) < p95):
            failures.append(
                "latency.worst_repeat_p95_ms must be present and at least the median p95"
            )

    gate = payload.get("gate")
    if not isinstance(gate, dict):
        failures.append("gate block is missing or not an object")
    else:
        if gate.get("metric") != "p95_ms":
            failures.append("gate.metric is not 'p95_ms'")
        if gate.get("resolution") != GATE_RESOLUTION:
            failures.append(f"gate.resolution is not {GATE_RESOLUTION} px")
        if gate.get("threshold_ms") != GATE_THRESHOLD_MS:
            failures.append(f"gate.threshold_ms is not {GATE_THRESHOLD_MS}")
        if gate.get("aggregation") != "median_of_repeats_p95":
            failures.append("gate.aggregation is not 'median_of_repeats_p95'")
        verdict = gate.get("verdict")
        gate_failures = gate.get("failures")
        if verdict not in {"PASSED", "FAILED"}:
            failures.append(f"gate.verdict is {verdict!r}, expected PASSED or FAILED")
        elif not isinstance(gate_failures, list):
            failures.append("gate.failures is not a list")
        elif p95 is not None:
            expected_verdict = "PASSED" if p95 <= GATE_THRESHOLD_MS else "FAILED"
            if verdict != expected_verdict:
                failures.append(
                    f"gate.verdict {verdict} contradicts p95 {p95:.2f} ms against "
                    f"the {GATE_THRESHOLD_MS} ms ceiling"
                )
            if verdict == "PASSED" and gate_failures:
                failures.append("gate.verdict PASSED carries failure messages")
            if verdict == "FAILED" and not gate_failures:
                failures.append("gate.verdict FAILED carries no failure message")

    stamps = _walk_timestamps(payload)
    if not stamps:
        failures.append("payload carries no *_at timestamp")
    current = now or datetime.now(timezone.utc)
    if current.tzinfo is None:
        current = current.replace(tzinfo=timezone.utc)
    horizon = current + timedelta(seconds=FUTURE_SKEW_SECONDS)
    for path, value in stamps:
        parsed = _parse_timestamp(value)
        if parsed is None:
            failures.append(f"{path} is not a timezone-aware ISO-8601 timestamp")
        elif parsed > horizon:
            failures.append(f"{path} ({value}) is in the future; evidence cannot be pre-dated")

    for path, value in _walk_source_claims(payload):
        if value.strip().lower() in SYNTHETIC_SOURCES:
            failures.append(
                f"{path} claims source {value!r}; release evidence cannot come from "
                f"synthetic or fixture data"
            )

    return failures


def parse_args(argv: Optional[Sequence[str]] = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Validate AVX2 latency evidence against the 3 x 200 sprint gate.",
    )
    parser.add_argument("--file", type=Path, required=True, help="Evidence JSON to check")
    return parser.parse_args(argv)


def main(argv: Optional[Sequence[str]] = None) -> int:
    args = parse_args(argv)
    try:
        payload = json.loads(Path(args.file).read_text(encoding="utf-8"))
    except (OSError, ValueError) as exc:
        print(f"cannot read evidence {args.file}: {exc}", file=sys.stderr)
        return EXIT_UNREADABLE

    failures = validate(payload)
    if failures:
        print(f"EVIDENCE REJECTED: {args.file}", file=sys.stderr)
        for failure in failures:
            print(f"  - {failure}", file=sys.stderr)
        return EXIT_REJECTED

    latency = payload["latency"]
    print(
        f"EVIDENCE ACCEPTED: p95={latency['p95_ms']:.2f} ms "
        f"(worst repeat {latency['worst_repeat_p95_ms']:.2f} ms, "
        f"n={latency['n']}), gate {payload['gate']['verdict']}"
    )
    return EXIT_OK


if __name__ == "__main__":
    sys.exit(main())
