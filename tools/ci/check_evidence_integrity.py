#!/usr/bin/env python3
"""
tools/ci/check_evidence_integrity.py - Evidence Integrity CI Gate & Synthetic Data Prohibition.

Enforces AGENTS.md Rule 2, Rule 6, Rule 10, and Sprint 11 Architecture (ADR-013 D1):
No release gate may pass on synthetic fixtures, simulated soak logs with future timestamps,
or dummy model weight placeholders.

Four Integrity Gates:
  Gate 1: Synthetic Data Prohibition
          - Prohibits `fit_source`, `source`, or `dataset_source` equal to "synthetic".
          - Forecast models and training runs must assert `fit_source == "live_history"`.
          - Rejects references to `synthetic_congestion_v2_5m.csv` or `synthetic_congestion_5m.csv`.
  Gate 2: Temporal Invariant & Anti-Spoofing
          - Asserts `generated_at >= window.end` (cannot observe window before it elapses).
          - Asserts `generated_at <= current_time + 300` (rejects future timestamps, 5m skew grace).
          - Asserts `window.end <= current_time + 300` (soak window cannot end in the future).
          - Asserts `window.end - window.start` matches declared window duration (e.g. 72h, 14d).
          - Supports SUPERSEDED artifacts (bypasses live temporal checks if validly superseded).
  Gate 3: Model Weight Integrity
          - OpenVINO `.bin`, PyTorch `.pt`, and ONNX weights must be > 1 MB (configurable).
          - Rejects repetitive byte patterns & stubs via Shannon entropy (H >= 3.0 bits/byte)
            and unique byte diversity (>= 32 unique bytes per 64 KB chunk).
  Gate 4: Empirical Proof Artifact Integrity
          - Proof images (.jpg, .png) must have non-zero size.
          - Must have valid magic bytes (JPEG: \xff\xd8\xff, PNG: \x89PNG\r\n\x1a\n).
          - Must satisfy minimum resolution (>= 320x240, or 240x320 portrait).

Ratchet & Strict Mode:
  Like `tools/docs/docs_lint.py`, this gate operates as a ratchet against
  `.evidence-integrity-baseline.txt`. Known legacy Sprint 10 violations can be tracked
  in the baseline, while new violations fail CI immediately. When `--strict` is enabled,
  the baseline is ignored and 100% compliance is required.
  Clock-dependent baseline entries (e.g. future timestamp assertions) are strictly prohibited.

Usage:
  python3 tools/ci/check_evidence_integrity.py --verify-all
  python3 tools/ci/check_evidence_integrity.py --repo-root . --mode gate
  python3 tools/ci/check_evidence_integrity.py --file <path>
  python3 tools/ci/check_evidence_integrity.py --strict
  python3 tools/ci/check_evidence_integrity.py --update-baseline
"""

from __future__ import annotations

import argparse
import collections
import gzip
import json
import math
import os
import re
import struct
import subprocess
import sys
from dataclasses import asdict, dataclass, field
from datetime import datetime, timezone
from pathlib import Path
from typing import Any, Dict, List, Optional, Set, Tuple

REPO_ROOT = Path(__file__).resolve().parents[2]
_ROOT = REPO_ROOT


def set_root(path: str | Path) -> None:
    """Override root directory for test fixtures and isolated evaluations."""
    global _ROOT
    _ROOT = Path(path).resolve()


def reset_root() -> None:
    """Reset root directory back to repository root."""
    global _ROOT
    _ROOT = REPO_ROOT


def root() -> Path:
    return _ROOT


BASELINE_FILENAME = ".evidence-integrity-baseline.txt"

# Prohibited fixtures and synthetic references
PROHIBITED_FIXTURE_NAMES = (
    "synthetic_congestion_v2_5m.csv",
    "synthetic_congestion_5m.csv",
)

PROHIBITED_FIXTURE_PATTERNS = [
    re.compile(re.escape(name), re.IGNORECASE) for name in PROHIBITED_FIXTURE_NAMES
] + [
    re.compile(r"synthetic_congestion.*\.csv", re.IGNORECASE),
    re.compile(r"fixtures/.*synthetic.*", re.IGNORECASE),
    re.compile(r"synthetic.*fixture", re.IGNORECASE),
]

SYNTHETIC_KEYWORDS = (
    "synthetic",
    "simulated",
    "mock",
    "dummy",
    "reference-profile",
    "placeholder",
    "fixture",
    "stub",
    "dry-run",
)

SOURCE_FIELD_KEYS = (
    "source",
    "dataset_source",
    "provenance",
    "data_source",
    "measurement_source",
)

FORECAST_KEYWORDS = (
    "forecast",
    "horizon_comparisons",
    "feature_store",
    "hotset_verdict_history",
    "ablation_study",
)

PROHIBITED_BASELINE_PATTERNS = (
    "is in the future",
    "current_time",
)


def is_clock_dependent_violation(detail: str) -> bool:
    """Return True if violation detail contains dynamic clock-dependent conditions."""
    detail_lower = detail.lower()
    return any(p in detail_lower for p in PROHIBITED_BASELINE_PATTERNS)


def validate_baseline_entry(v: Violation, source: str = "baseline") -> None:
    """Validate that a baseline violation is permissible (e.g. not dynamic clock-dependent)."""
    if is_clock_dependent_violation(v.detail):
        raise ValueError(
            f"Dynamic clock-dependent baseline entry prohibited in {source}: '{v.detail}'. "
            "Temporal conditions referencing future timestamps or current clock expire as time passes "
            "and must not be grandfathered in baseline."
        )


@dataclass(frozen=True)
class Violation:
    gate: str
    path: str
    detail: str

    def to_baseline_line(self) -> str:
        return f"{self.gate}\t{self.path}\t{self.detail}"

    @classmethod
    def from_baseline_line(cls, line: str) -> Optional[Violation]:
        line = line.strip()
        if not line or line.startswith("#"):
            return None
        parts = line.split("\t")
        if len(parts) >= 3:
            return cls(gate=parts[0], path=parts[1], detail=parts[2])
        elif len(parts) == 2:
            return cls(gate=parts[0], path=parts[1], detail="")
        return None


@dataclass
class GateReport:
    valid: bool
    violations: List[Violation] = field(default_factory=list)
    checked_files: List[str] = field(default_factory=list)
    new_violations: List[Violation] = field(default_factory=list)
    stale_baseline: List[Violation] = field(default_factory=list)

    def to_dict(self) -> Dict[str, Any]:
        return {
            "valid": self.valid,
            "checked_files_count": len(self.checked_files),
            "checked_files": self.checked_files,
            "violations_count": len(self.violations),
            "violations": [asdict(v) for v in self.violations],
            "new_violations": [asdict(v) for v in self.new_violations],
            "stale_baseline": [asdict(v) for v in self.stale_baseline],
        }


# ==============================================================================
# Mathematical and Utility Helpers
# ==============================================================================


def shannon_entropy(data: bytes) -> float:
    """Compute Shannon entropy in bits per byte (0.0 to 8.0)."""
    if not data:
        return 0.0
    counts = collections.Counter(data)
    total = len(data)
    return -sum((count / total) * math.log2(count / total) for count in counts.values())


def is_naive_iso_timestamp(ts: Any) -> bool:
    """Return True if timestamp string has no timezone offset (naive local time)."""
    if not isinstance(ts, str):
        return False
    s = ts.strip()
    if not s:
        return False
    if s.endswith("Z") or s.endswith("z"):
        return False
    if re.search(r"[+-]\d{2}(?::?\d{2})?$", s):
        return False
    return True


def parse_iso_timestamp(ts: Any) -> Optional[datetime]:
    """Parse ISO 8601 timestamp string into timezone-aware UTC datetime."""
    if not isinstance(ts, str):
        return None
    s = ts.strip()
    if not s:
        return None
    # Normalize trailing Z
    if s.endswith("Z") or s.endswith("z"):
        s = s[:-1] + "+00:00"
    try:
        dt = datetime.fromisoformat(s)
        if dt.tzinfo is None:
            # Default naive strings to UTC
            dt = dt.replace(tzinfo=timezone.utc)
        return dt.astimezone(timezone.utc)
    except Exception:
        # Fallback for formats with space or irregular fractions
        for fmt in (
            "%Y-%m-%d %H:%M:%S",
            "%Y-%m-%d %H:%M:%S%z",
            "%Y-%m-%dT%H:%M:%S",
            "%Y-%m-%dT%H:%M:%S%z",
        ):
            try:
                dt = datetime.strptime(s, fmt)
                if dt.tzinfo is None:
                    dt = dt.replace(tzinfo=timezone.utc)
                return dt.astimezone(timezone.utc)
            except Exception:
                continue
    return None


def parse_duration_to_seconds(val: Any) -> Optional[float]:
    """
    Parse duration declaration into seconds.
    Supports integer/float hours, or strings like '72h', '14d', '30m', '3600s'.
    """
    if val is None:
        return None
    if isinstance(val, (int, float)):
        # Bare numbers in window_hours context are treated as hours by caller
        return float(val)
    if isinstance(val, str):
        s = val.strip().lower()
        if not s:
            return None
        m = re.match(r"^(\d+(?:\.\d+)?)\s*([a-z]+)?$", s)
        if not m:
            return None
        num = float(m.group(1))
        unit = m.group(2) or "h"
        if unit in ("h", "hr", "hours", "hour"):
            return num * 3600.0
        elif unit in ("d", "day", "days"):
            return num * 86400.0
        elif unit in ("m", "min", "mins", "minute", "minutes"):
            return num * 60.0
        elif unit in ("s", "sec", "secs", "second", "seconds"):
            return num
    return None


def read_image_dimensions(data: bytes) -> Tuple[Optional[str], int, int]:
    """
    Extract image format ('JPEG', 'PNG') and dimensions (width, height).
    Pure-Python implementation with zero mandatory third-party dependencies.
    """
    if len(data) >= 8 and data[:8] == b"\x89PNG\r\n\x1a\n":
        if len(data) >= 24:
            w, h = struct.unpack(">II", data[16:24])
            return "PNG", w, h
        return "PNG", 0, 0

    if len(data) >= 3 and data[:3] == b"\xff\xd8\xff":
        idx = 2
        while idx < len(data):
            if data[idx] != 0xFF:
                idx += 1
                continue
            marker = data[idx + 1]
            idx += 2
            # SOF markers: SOF0..SOF3, SOF5..SOF7, SOF9..SOF11, SOF13..SOF15
            if marker in (
                0xC0, 0xC1, 0xC2, 0xC3, 0xC5, 0xC6, 0xC7,
                0xC9, 0xCA, 0xCB, 0xCD, 0xCE, 0xCF,
            ):
                if idx + 7 <= len(data):
                    h, w = struct.unpack(">HH", data[idx + 3 : idx + 7])
                    return "JPEG", w, h
                break
            elif marker in (0xD9, 0xDA):  # EOI or SOS (start of stream)
                break
            else:
                if idx + 2 <= len(data):
                    seg_len = struct.unpack(">H", data[idx : idx + 2])[0]
                    idx += seg_len
                else:
                    break
        return "JPEG", 0, 0

    return None, 0, 0


# ==============================================================================
# Gate 1: Synthetic Data Prohibition
# ==============================================================================


def check_gate1_synthetic(
    rel_path: str, data: Any, text_content: str, is_release: bool = True
) -> List[Violation]:
    """
    Verify synthetic fixtures or mock datasets are not used in release evidence.
    Asserts fit_source == "live_history" for forecast models and training runs.
    """
    violations: List[Violation] = []

    # 1. Prohibited fixture references
    for pattern in PROHIBITED_FIXTURE_PATTERNS:
        m = pattern.search(text_content)
        if m:
            violations.append(
                Violation(
                    gate="gate1_synthetic",
                    path=rel_path,
                    detail=f"Prohibited reference to synthetic fixture matching '{m.group(0)}'",
                )
            )
            break

    # 2. Check JSON data fields recursively
    is_forecast_or_training = (
        "forecast" in rel_path.lower()
        or "train" in rel_path.lower()
        or "ablation" in rel_path.lower()
    )

    def scan_dict(d: Any) -> None:
        nonlocal is_forecast_or_training
        if isinstance(d, dict):
            for k, v in d.items():
                k_lower = str(k).lower()
                if any(kw in k_lower for kw in FORECAST_KEYWORDS):
                    is_forecast_or_training = True

                # Check prohibited source fields
                if k_lower in SOURCE_FIELD_KEYS:
                    if isinstance(v, str):
                        v_strip = v.strip().lower()
                        if any(sk in v_strip for sk in SYNTHETIC_KEYWORDS):
                            violations.append(
                                Violation(
                                    gate="gate1_synthetic",
                                    path=rel_path,
                                    detail=f"Field '{k}' has prohibited synthetic value '{v}'",
                                )
                            )

                # Check fit_source specifically for forecast / training
                if k_lower == "fit_source":
                    if isinstance(v, str):
                        v_strip = v.strip().lower()
                        if any(sk in v_strip for sk in SYNTHETIC_KEYWORDS):
                            violations.append(
                                Violation(
                                    gate="gate1_synthetic",
                                    path=rel_path,
                                    detail=f"Field '{k}' has prohibited synthetic value '{v}'",
                                )
                            )
                        elif v.strip() != "live_history":
                            violations.append(
                                Violation(
                                    gate="gate1_synthetic",
                                    path=rel_path,
                                    detail=f"fit_source must be 'live_history', found '{v}'",
                                )
                            )

                # Recurse
                scan_dict(v)
        elif isinstance(d, list):
            for item in d:
                scan_dict(item)

    if isinstance(data, (dict, list)):
        scan_dict(data)

    # If it is a forecast model/run in a release artifact, fit_source MUST be declared as live_history
    if isinstance(data, dict) and is_forecast_or_training and is_release:
        # Check if fit_source exists in data or data_provenance
        fit_source = data.get("fit_source")
        if fit_source is None and isinstance(data.get("data_provenance"), dict):
            fit_source = data["data_provenance"].get("fit_source")

        if fit_source is None:
            if not any("fit_source" in v.detail for v in violations):
                violations.append(
                    Violation(
                        gate="gate1_synthetic",
                        path=rel_path,
                        detail="Forecast release artifact missing required 'fit_source' declaration (must be 'live_history')",
                    )
                )
        elif fit_source != "live_history":
            # Already flagged above or flag here
            if not any("fit_source" in v.detail for v in violations):
                violations.append(
                    Violation(
                        gate="gate1_synthetic",
                        path=rel_path,
                        detail=f"Forecast release artifact must declare fit_source='live_history', got '{fit_source}'",
                    )
                )

    return violations


# ==============================================================================
# Gate 2: Temporal Invariant & Anti-Spoofing
# ==============================================================================


def check_gate2_temporal(
    rel_path: str, data: Dict[str, Any], now: Optional[datetime] = None
) -> List[Violation]:
    """
    Enforce temporal consistency on soak evidence and benchmark runs.
      - generated_at >= window.end
      - generated_at <= current_time + 300s (rejects future timestamps)
      - window.end <= current_time + 300s (rejects future soak windows)
      - window.end - window.start matches declared duration within tolerance.
    If status == "SUPERSEDED", requires non-empty superseded_by (referencing valid file)
    and non-empty invalidation_reason. Bypasses live temporal assertions if valid.
    """
    violations: List[Violation] = []
    if not isinstance(data, dict):
        return violations

    # Support artifact invalidation/superseding (ADR-013 F2)
    if data.get("status") == "SUPERSEDED":
        superseded_by = data.get("superseded_by")
        invalidation_reason = data.get("invalidation_reason")
        has_superseded_err = False

        if not isinstance(superseded_by, str) or not superseded_by.strip():
            violations.append(
                Violation(
                    gate="gate2_temporal",
                    path=rel_path,
                    detail="SUPERSEDED artifact missing required non-empty 'superseded_by' declaration",
                )
            )
            has_superseded_err = True
        else:
            base_root = root()
            target_path = base_root / superseded_by.strip()
            if not target_path.exists():
                local_target = (base_root / rel_path).parent / superseded_by.strip()
                if local_target.exists():
                    target_path = local_target

            if not target_path.exists() or not target_path.is_file():
                violations.append(
                    Violation(
                        gate="gate2_temporal",
                        path=rel_path,
                        detail=f"SUPERSEDED artifact references non-existent superseded_by target '{superseded_by}'",
                    )
                )
                has_superseded_err = True

        if not isinstance(invalidation_reason, str) or not invalidation_reason.strip():
            violations.append(
                Violation(
                    gate="gate2_temporal",
                    path=rel_path,
                    detail="SUPERSEDED artifact missing required non-empty 'invalidation_reason' declaration",
                )
            )
            has_superseded_err = True

        if not has_superseded_err:
            # Valid superseded artifact: bypass live temporal generation assertions
            return []
        return violations

    current_utc = now.astimezone(timezone.utc) if now else datetime.now(timezone.utc)
    max_allowed_future = current_utc.timestamp() + 300.0  # 5 min clock skew allowance

    # 1. Locate generated_at timestamp
    gen_ts_str = (
        data.get("generated_at")
        or data.get("generated_at_utc")
        or data.get("timestamp")
        or data.get("created_at")
    )
    gen_dt = parse_iso_timestamp(gen_ts_str)

    # Check for naive local timestamps (missing explicit UTC offset)
    if gen_ts_str and is_naive_iso_timestamp(gen_ts_str):
        violations.append(
            Violation(
                gate="gate2_temporal",
                path=rel_path,
                detail=f"generated_at ({gen_ts_str}) has no UTC offset (naive local time prohibited)",
            )
        )

    if gen_dt:
        if gen_dt.timestamp() > max_allowed_future:
            violations.append(
                Violation(
                    gate="gate2_temporal",
                    path=rel_path,
                    detail=f"generated_at ({gen_ts_str}) is in the future (> current_time + 300s)",
                )
            )

    # 2. Locate window start and end
    start_ts_str = None
    end_ts_str = None
    declared_duration_sec = None

    if isinstance(data.get("window"), dict):
        w = data["window"]
        start_ts_str = w.get("start") or w.get("start_time") or w.get("start_utc")
        end_ts_str = w.get("end") or w.get("end_time") or w.get("end_utc")
        if "duration" in w:
            declared_duration_sec = parse_duration_to_seconds(w["duration"])

    if not start_ts_str:
        start_ts_str = data.get("start_time") or data.get("window_start")
    if not end_ts_str:
        end_ts_str = data.get("end_time") or data.get("window_end")

    if declared_duration_sec is None:
        if "window_hours" in data:
            hours = parse_duration_to_seconds(data["window_hours"])
            if hours is not None:
                declared_duration_sec = hours * 3600.0
        elif "window_days" in data:
            days = parse_duration_to_seconds(data["window_days"])
            if days is not None:
                declared_duration_sec = days * 86400.0
        elif "window_duration" in data:
            declared_duration_sec = parse_duration_to_seconds(data["window_duration"])
        elif "declared_duration" in data:
            declared_duration_sec = parse_duration_to_seconds(data["declared_duration"])

    # 3. Validate window if present
    if start_ts_str and is_naive_iso_timestamp(start_ts_str):
        violations.append(
            Violation(
                gate="gate2_temporal",
                path=rel_path,
                detail=f"window.start ({start_ts_str}) has no UTC offset (naive local time prohibited)",
            )
        )
    if end_ts_str and is_naive_iso_timestamp(end_ts_str):
        violations.append(
            Violation(
                gate="gate2_temporal",
                path=rel_path,
                detail=f"window.end ({end_ts_str}) has no UTC offset (naive local time prohibited)",
            )
        )

    start_dt = parse_iso_timestamp(start_ts_str)
    end_dt = parse_iso_timestamp(end_ts_str)

    if start_dt and end_dt:
        # Check start <= end
        if start_dt > end_dt:
            violations.append(
                Violation(
                    gate="gate2_temporal",
                    path=rel_path,
                    detail=f"window.start ({start_ts_str}) is after window.end ({end_ts_str})",
                )
            )
            return violations

        # Check window.end is not in the future
        if end_dt.timestamp() > max_allowed_future:
            violations.append(
                Violation(
                    gate="gate2_temporal",
                    path=rel_path,
                    detail=f"window.end ({end_ts_str}) is in the future (> current_time + 300s)",
                )
            )

        # Check generated_at >= window.end
        if gen_dt and gen_dt < end_dt:
            violations.append(
                Violation(
                    gate="gate2_temporal",
                    path=rel_path,
                    detail=f"generated_at ({gen_ts_str}) < window.end ({end_ts_str}): cannot observe soak before window concludes",
                )
            )

        # Check duration match
        actual_duration_sec = (end_dt - start_dt).total_seconds()
        if declared_duration_sec is not None and declared_duration_sec > 0:
            delta = abs(actual_duration_sec - declared_duration_sec)
            tolerance = max(60.0, declared_duration_sec * 0.02)  # 2% or 60s tolerance
            if delta > tolerance:
                violations.append(
                    Violation(
                        gate="gate2_temporal",
                        path=rel_path,
                        detail=(
                            f"window duration ({actual_duration_sec:.0f}s) does not match "
                            f"declared duration ({declared_duration_sec:.0f}s)"
                        ),
                    )
                )

    return violations


# ==============================================================================
# Gate 3: Model Weight Integrity
# ==============================================================================


def check_gate3_model(
    rel_path: str,
    file_bytes: bytes,
    min_size: int = 1_048_576,  # 1 MB default
    min_entropy: float = 3.0,
    min_unique_bytes: int = 32,
) -> List[Violation]:
    """
    Assert neural network weights (.bin, .pt, .onnx) are genuine FP16/FP32 models
    rather than empty stub files.
      - File size > min_size (1 MB)
      - Shannon entropy >= 3.0 bits/byte
      - Unique byte diversity >= 32 unique bytes
    """
    violations: List[Violation] = []
    size = len(file_bytes)

    if size < min_size:
        violations.append(
            Violation(
                gate="gate3_model",
                path=rel_path,
                detail=f"Model file size ({size} B) is below 1 MB threshold ({min_size} B). Placeholder stub suspected.",
            )
        )
        return violations

    # Evaluate first 1 MB for entropy and byte diversity
    sample = file_bytes[: 1024 * 1024]
    ent = shannon_entropy(sample)
    unique_cnt = len(set(sample))

    if ent < min_entropy:
        violations.append(
            Violation(
                gate="gate3_model",
                path=rel_path,
                detail=f"Model weights have low Shannon entropy ({ent:.2f} < {min_entropy:.2f} bits/byte). Repetitive byte pattern detected.",
            )
        )

    if unique_cnt < min_unique_bytes:
        violations.append(
            Violation(
                gate="gate3_model",
                path=rel_path,
                detail=f"Model weights lack byte diversity ({unique_cnt} < {min_unique_bytes} unique bytes). Stub pattern detected.",
            )
        )

    # Check first 64 KB chunk for zero-byte or trivial repeating sequences
    chunk_64k = file_bytes[: 64 * 1024]
    if len(chunk_64k) >= 1024:
        chunk_ent = shannon_entropy(chunk_64k)
        if chunk_ent < 1.5:
            violations.append(
                Violation(
                    gate="gate3_model",
                    path=rel_path,
                    detail=f"First 64 KB chunk has near-zero entropy ({chunk_ent:.2f} bits/byte). Null or trivial stub detected.",
                )
            )

    # Check periodicity across first 64 KB (detect periodic repetition of 1 to 4096 byte patterns)
    check_len = min(64 * 1024, size)
    head_block = file_bytes[:check_len]
    for p in (1, 2, 4, 8, 16, 32, 64, 128, 256, 512, 1024, 2048, 4096):
        if p <= len(head_block) // 2:
            repeated = head_block[:p] * (len(head_block) // p)
            if head_block.startswith(repeated):
                violations.append(
                    Violation(
                        gate="gate3_model",
                        path=rel_path,
                        detail=f"Model binary is periodic repetition of {p}-byte pattern. Placeholder stub detected.",
                    )
                )
                break

    # Check midpoint and tail for null padding
    if size >= 128 * 1024:
        mid_start = size // 2
        mid_block = file_bytes[mid_start : mid_start + 64 * 1024]
        if len(mid_block) >= 1024 and shannon_entropy(mid_block) < 1.5:
            violations.append(
                Violation(
                    gate="gate3_model",
                    path=rel_path,
                    detail=f"Model binary midpoint has near-zero entropy ({shannon_entropy(mid_block):.2f} bits/byte). Stub detected.",
                )
            )
        tail_block = file_bytes[-64 * 1024 :]
        if shannon_entropy(tail_block) < 0.2 and len(set(tail_block)) <= 2:
            violations.append(
                Violation(
                    gate="gate3_model",
                    path=rel_path,
                    detail="Model binary tail consists of null/zero padding bytes. Padded stub detected.",
                )
            )

    # Check companion OpenVINO IR XML if inspecting .bin
    if rel_path.endswith(".bin"):
        xml_rel = rel_path[:-4] + ".xml"
        actual_xml_file = root() / xml_rel
        if actual_xml_file.exists():
            try:
                xml_text = actual_xml_file.read_text(encoding="utf-8")
                layer_count = xml_text.count("<layer ")
                if layer_count < 10:
                    violations.append(
                        Violation(
                            gate="gate3_model",
                            path=rel_path,
                            detail=f"Companion OpenVINO IR XML '{xml_rel}' has only {layer_count} layers (< 10). Shallow mock detected.",
                        )
                    )
            except Exception:
                pass

    # Check compressibility via gzip (real float weights have ratio > 0.80; periodic stubs compress to < 0.10)
    compressed_sample = gzip.compress(sample)
    gzip_ratio = len(compressed_sample) / max(1, len(sample))
    if gzip_ratio < 0.60:
        violations.append(
            Violation(
                gate="gate3_model",
                path=rel_path,
                detail=f"Model weights are highly compressible (gzip ratio {gzip_ratio:.3f} < 0.60). Periodic or repetitive stub detected.",
            )
        )

    return violations


# ==============================================================================
# Gate 4: Empirical Proof Artifact Integrity
# ==============================================================================


def check_gate4_proof(
    rel_path: str,
    file_bytes: bytes,
    min_width: int = 320,
    min_height: int = 240,
) -> List[Violation]:
    """
    Assert empirical screenshots and camera proof images (.jpg, .png) are authentic.
      - Non-zero file size.
      - Valid image magic bytes (JPEG or PNG).
      - Minimum resolution >= 320x240 (landscape or portrait).
      - Rejects HTML mocks disguised as images.
    """
    violations: List[Violation] = []
    size = len(file_bytes)

    if size == 0:
        violations.append(
            Violation(
                gate="gate4_proof",
                path=rel_path,
                detail="Proof artifact is an empty (0 bytes) file",
            )
        )
        return violations

    # Check for HTML mock disguised as image
    prefix_str = file_bytes[:100].decode("ascii", errors="ignore").lower()
    if "<!doctype html" in prefix_str or "<html" in prefix_str:
        violations.append(
            Violation(
                gate="gate4_proof",
                path=rel_path,
                detail="Proof artifact is a static HTML mockup disguised as an image file",
            )
        )
        return violations

    fmt, width, height = read_image_dimensions(file_bytes)
    if fmt is None:
        violations.append(
            Violation(
                gate="gate4_proof",
                path=rel_path,
                detail="Invalid image magic bytes (expected JPEG '\\xff\\xd8\\xff' or PNG '\\x89PNG')",
            )
        )
        return violations

    if width == 0 or height == 0:
        violations.append(
            Violation(
                gate="gate4_proof",
                path=rel_path,
                detail=f"Could not parse valid image dimensions for {fmt} artifact",
            )
        )
        return violations

    # Check resolution bounds (supports landscape >= 320x240 or portrait >= 240x320)
    is_landscape_ok = width >= min_width and height >= min_height
    is_portrait_ok = width >= min_height and height >= min_width

    if not (is_landscape_ok or is_portrait_ok):
        violations.append(
            Violation(
                gate="gate4_proof",
                path=rel_path,
                detail=f"Image resolution {width}x{height} is below minimum {min_width}x{min_height} threshold",
            )
        )

    # Check for degenerate solid/blank placeholder images
    try:
        import io
        from PIL import Image

        with Image.open(io.BytesIO(file_bytes)) as im:
            rgb = im.convert("RGB")
            colors = rgb.getcolors(maxcolors=256)
            if colors is not None and len(colors) < 5:
                gray = rgb.convert("L")
                if hasattr(gray, "get_flattened_data"):
                    pixels = list(gray.get_flattened_data())
                else:
                    pixels = list(gray.getdata())
                if pixels:
                    mean = sum(pixels) / len(pixels)
                    variance = sum((p - mean) ** 2 for p in pixels) / len(pixels)
                    if variance < 10.0:
                        violations.append(
                            Violation(
                                gate="gate4_proof",
                                path=rel_path,
                                detail=f"Image is a degenerate solid-color placeholder (unique colors={len(colors)}, variance={variance:.1f})",
                            )
                        )
    except Exception:
        pass

    return violations


# ==============================================================================
# File Dispatcher & Baseline Engine
# ==============================================================================


def check_artifact(
    file_path: Path,
    now: Optional[datetime] = None,
    min_model_size: int = 1_048_576,
    min_width: int = 320,
    min_height: int = 240,
    strict: bool = False,
) -> List[Violation]:
    """Inspect a single artifact file against applicable integrity gates."""
    base_root = root()
    try:
        rel_path = str(file_path.resolve().relative_to(base_root))
    except ValueError:
        rel_path = str(file_path)

    if not file_path.exists():
        return [Violation(gate="file_not_found", path=rel_path, detail="File does not exist")]

    ext = file_path.suffix.lower()
    violations: List[Violation] = []

    # 1. Model Files
    if ext in (".bin", ".pt", ".pth", ".onnx"):
        try:
            content = file_path.read_bytes()
            violations.extend(
                check_gate3_model(
                    rel_path, content, min_size=min_model_size
                )
            )
        except Exception as e:
            violations.append(
                Violation(gate="gate3_model", path=rel_path, detail=f"Failed to read model: {e}")
            )
        return violations

    # 2. Image Proof Artifacts
    if ext in (".jpg", ".jpeg", ".png"):
        try:
            content = file_path.read_bytes()
            violations.extend(
                check_gate4_proof(
                    rel_path, content, min_width=min_width, min_height=min_height
                )
            )
        except Exception as e:
            violations.append(
                Violation(gate="gate4_proof", path=rel_path, detail=f"Failed to read image: {e}")
            )
        return violations

    # 3. JSON Reports and Evidence Manifests
    if ext == ".json":
        try:
            text = file_path.read_text(encoding="utf-8")
        except Exception as e:
            return [Violation(gate="json_parse", path=rel_path, detail=f"Failed to read text: {e}")]

        try:
            parsed = json.loads(text)
        except Exception as e:
            return [Violation(gate="json_parse", path=rel_path, detail=f"Invalid JSON syntax: {e}")]

        # Gate 1: Synthetic Prohibition
        violations.extend(check_gate1_synthetic(rel_path, parsed, text, is_release=True))

        # Gate 2: Temporal Invariant
        if isinstance(parsed, dict):
            violations.extend(check_gate2_temporal(rel_path, parsed, now=now))

        return violations

    # 4. Markdown Evidence Reports
    if ext == ".md":
        try:
            text = file_path.read_text(encoding="utf-8")
            violations.extend(check_gate1_synthetic(rel_path, {}, text, is_release=True))
        except Exception as e:
            violations.append(
                Violation(gate="gate1_synthetic", path=rel_path, detail=f"Failed to read markdown: {e}")
            )
        return violations

    return violations


def load_baseline(baseline_path: Path) -> Set[Violation]:
    """Load known historical baseline violations."""
    if not baseline_path.exists():
        return set()
    violations: Set[Violation] = set()
    for line in baseline_path.read_text(encoding="utf-8").splitlines():
        v = Violation.from_baseline_line(line)
        if v is not None:
            validate_baseline_entry(v, source=baseline_path.name)
            violations.add(v)
    return violations


def save_baseline(baseline_path: Path, violations: List[Violation]) -> None:
    """Save sorted baseline violations to file."""
    lines = [
        "# Evidence-integrity baseline: historical violations grandfathered from Sprint 10.",
        "#",
        "# This is a ratchet, not an exemption list. Fixing a violation means deleting",
        "# its line here in the same PR; a stale line fails the gate, so this file can only shrink.",
        "# Never add a line to silence a regression.",
        "#",
        "# Format: <gate>\\t<path>\\t<detail>",
    ]
    # Sort violations deterministically
    sorted_v = sorted(violations, key=lambda x: (x.gate, x.path, x.detail))
    for v in sorted_v:
        validate_baseline_entry(v, source="save_baseline")
        lines.append(v.to_baseline_line())
    baseline_path.write_text("\n".join(lines) + "\n", encoding="utf-8")


def find_tracked_evidence_files(base_root: Path) -> List[Path]:
    """Locate all release evidence artifacts tracked in git or staged."""
    matched_paths: Set[Path] = set()

    # Query git for tracked and staged files
    try:
        res_tracked = subprocess.run(
            ["git", "ls-files"],
            cwd=base_root,
            capture_output=True,
            text=True,
            check=True,
        )
        tracked_files = res_tracked.stdout.splitlines()

        res_staged = subprocess.run(
            ["git", "diff", "--name-only", "--cached"],
            cwd=base_root,
            capture_output=True,
            text=True,
            check=True,
        )
        staged_files = res_staged.stdout.splitlines()

        all_candidates = set(tracked_files + staged_files)
    except Exception:
        all_candidates = set()

    evidence_prefixes = (
        "docs/reports/",
        "docs/proofs/",
        "tools/eval/results/",
        "services/go-server/data/proofs/",
        "models/",
    )

    if all_candidates:
        for c in all_candidates:
            c_norm = c.replace("\\", "/")
            if c_norm.startswith("docs/reports/") and c_norm.endswith(".json"):
                p = base_root / c
                if p.exists() and p.is_file():
                    matched_paths.add(p)
            elif c_norm.startswith("docs/proofs/") and any(c_norm.endswith(ext) for ext in (".png", ".jpg", ".jpeg", ".json")):
                p = base_root / c
                if p.exists() and p.is_file():
                    matched_paths.add(p)
            elif c_norm.startswith("tools/eval/results/") and c_norm.endswith(".json"):
                p = base_root / c
                if p.exists() and p.is_file():
                    matched_paths.add(p)
            elif c_norm.startswith("services/go-server/data/proofs/") and any(c_norm.endswith(ext) for ext in (".png", ".jpg", ".jpeg", ".json")):
                p = base_root / c
                if p.exists() and p.is_file():
                    matched_paths.add(p)
            elif c_norm.startswith("models/") and any(c_norm.endswith(ext) for ext in (".bin", ".pt", ".onnx")):
                p = base_root / c
                if p.exists() and p.is_file():
                    matched_paths.add(p)

        # In addition, check any local model binaries under models/ directory if present
        models_dir = base_root / "models"
        if models_dir.exists() and models_dir.is_dir():
            for p in models_dir.rglob("*"):
                if p.is_file() and any(p.name.endswith(ext) for ext in (".bin", ".pt", ".onnx")):
                    matched_paths.add(p)
    else:
        # Fallback to filesystem traversal if not in git
        for target_dir, allowed_exts in (
            (base_root / "docs/reports", (".json",)),
            (base_root / "docs/proofs", (".png", ".jpg", ".jpeg", ".json")),
            (base_root / "tools/eval/results", (".json",)),
            (base_root / "services/go-server/data/proofs", (".png", ".jpg", ".jpeg", ".json")),
            (base_root / "models", (".bin", ".pt", ".onnx")),
        ):
            if target_dir.exists():
                for p in target_dir.rglob("*"):
                    if p.is_file() and any(p.name.endswith(ext) for ext in allowed_exts):
                        matched_paths.add(p)

    return sorted(list(matched_paths))


def evaluate_gate(
    files: List[Path],
    now: Optional[datetime] = None,
    strict: bool = False,
    baseline_path: Optional[Path] = None,
    min_model_size: int = 1_048_576,
    min_width: int = 320,
    min_height: int = 240,
) -> GateReport:
    """Evaluate all provided evidence artifacts against gates and baseline."""
    base_root = root()
    baseline_file = baseline_path or (base_root / BASELINE_FILENAME)
    baseline_violations = load_baseline(baseline_file) if not strict else set()

    all_violations: List[Violation] = []
    checked_files: List[str] = []

    for file_path in files:
        try:
            rel = str(file_path.resolve().relative_to(base_root))
        except ValueError:
            rel = str(file_path)
        checked_files.append(rel)
        v_list = check_artifact(
            file_path,
            now=now,
            min_model_size=min_model_size,
            min_width=min_width,
            min_height=min_height,
            strict=strict,
        )
        all_violations.extend(v_list)

    if strict:
        return GateReport(
            valid=len(all_violations) == 0,
            violations=all_violations,
            checked_files=checked_files,
            new_violations=all_violations,
            stale_baseline=[],
        )

    # Ratchet comparison: new violations vs baseline
    curr_set = set(all_violations)
    evaluated_paths = set(checked_files)
    # Only consider baseline entries corresponding to files that were checked
    relevant_baseline = {v for v in baseline_violations if v.path in evaluated_paths}
    new_violations = [v for v in all_violations if v not in relevant_baseline]
    stale_baseline = [v for v in relevant_baseline if v not in curr_set]

    # Stale baseline entries fail CI (demands ratchet cleanup)
    is_valid = len(new_violations) == 0 and len(stale_baseline) == 0

    return GateReport(
        valid=is_valid,
        violations=all_violations,
        checked_files=checked_files,
        new_violations=new_violations,
        stale_baseline=stale_baseline,
    )


# ==============================================================================
# CLI Entrypoint
# ==============================================================================


def build_arg_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        description="Verify evidence integrity, synthetic data prohibition, and temporal invariants."
    )
    parser.add_argument(
        "--verify-all",
        action="store_true",
        help="Check all release evidence artifacts tracked in git or staged.",
    )
    parser.add_argument(
        "--file",
        type=str,
        help="Check a single evidence artifact file.",
    )
    parser.add_argument(
        "--dir",
        type=str,
        help="Check all evidence artifacts under a directory.",
    )
    parser.add_argument(
        "--repo-root",
        type=str,
        default=None,
        help="Path to repository root (defaults to auto-detected repository root).",
    )
    parser.add_argument(
        "--mode",
        type=str,
        choices=["gate", "audit", "strict"],
        default="gate",
        help="Execution mode: gate (enforce baseline ratchet), audit (report only), strict (zero exemptions).",
    )
    parser.add_argument(
        "--strict",
        action="store_true",
        help="Enforce strict mode: ignore baseline ratchet, require 100%% compliance.",
    )
    parser.add_argument(
        "--now",
        type=str,
        help="Override current UTC timestamp (ISO 8601) for deterministic temporal checks.",
    )
    parser.add_argument(
        "--baseline",
        type=str,
        help=f"Path to baseline file (default: {BASELINE_FILENAME}).",
    )
    parser.add_argument(
        "--update-baseline",
        action="store_true",
        help="Rewrite the baseline file with current violations.",
    )
    parser.add_argument(
        "--min-model-size",
        type=int,
        default=1_048_576,
        help="Minimum model file size in bytes (default: 1048576 = 1 MB).",
    )
    parser.add_argument(
        "--min-width",
        type=int,
        default=320,
        help="Minimum image width for empirical proof screenshots (default: 320).",
    )
    parser.add_argument(
        "--min-height",
        type=int,
        default=240,
        help="Minimum image height for empirical proof screenshots (default: 240).",
    )
    parser.add_argument(
        "--json",
        action="store_true",
        help="Output check report in JSON format.",
    )
    parser.add_argument(
        "--verbose",
        "-v",
        action="store_true",
        help="Print verbose diagnostic details.",
    )
    return parser


def main() -> int:
    parser = build_arg_parser()
    args = parser.parse_args()

    if args.repo_root:
        set_root(args.repo_root)

    base_root = root()
    now_dt = parse_iso_timestamp(args.now) if args.now else None
    baseline_path = Path(args.baseline) if args.baseline else (base_root / BASELINE_FILENAME)
    strict_mode = args.strict or (args.mode == "strict")

    files_to_check: List[Path] = []

    if args.file:
        p = Path(args.file)
        if not p.is_absolute():
            p = base_root / p
        files_to_check.append(p)
    elif args.dir:
        d = Path(args.dir)
        if not d.is_absolute():
            d = base_root / d
        if d.exists() and d.is_dir():
            for f in sorted(d.rglob("*")):
                if f.is_file():
                    files_to_check.append(f)
    elif args.verify_all or args.update_baseline or args.mode in ("gate", "audit"):
        files_to_check = find_tracked_evidence_files(base_root)
    else:
        # Default behavior: run verify-all
        files_to_check = find_tracked_evidence_files(base_root)

    if args.update_baseline:
        all_violations: List[Violation] = []
        for f in files_to_check:
            all_violations.extend(
                check_artifact(
                    f,
                    now=now_dt,
                    min_model_size=args.min_model_size,
                    min_width=args.min_width,
                    min_height=args.min_height,
                    strict=True,
                )
            )
        try:
            save_baseline(baseline_path, all_violations)
        except ValueError as e:
            print(f"FAIL: Cannot update baseline: {e}", file=sys.stderr)
            return 1
        print(f"Updated baseline at {baseline_path} with {len(all_violations)} violations.")
        return 0

    try:
        report = evaluate_gate(
            files=files_to_check,
            now=now_dt,
            strict=strict_mode,
            baseline_path=baseline_path,
            min_model_size=args.min_model_size,
            min_width=args.min_width,
            min_height=args.min_height,
        )
    except ValueError as e:
        print(f"FAIL: {e}", file=sys.stderr)
        return 1

    if args.json:
        print(json.dumps(report.to_dict(), indent=2))
        return 0 if report.valid else 1

    print(f"Evidence Integrity CI Gate: evaluated {len(report.checked_files)} artifacts.")

    if args.verbose:
        for cf in report.checked_files:
            print(f"  Checked: {cf}")

    if report.valid:
        print("PASS: All evidence artifacts satisfy integrity, temporal, model, and anti-spoofing gates.")
        return 0

    print("FAIL: Evidence integrity violations detected!")
    if report.new_violations:
        print(f"\nNew Violations ({len(report.new_violations)}):")
        for v in report.new_violations:
            print(f"  [{v.gate}] {v.path}: {v.detail}")

    if report.stale_baseline:
        print(f"\nStale Baseline Entries ({len(report.stale_baseline)}) - Remove these from {baseline_path.name}:")
        for v in report.stale_baseline:
            print(f"  [{v.gate}] {v.path}: {v.detail}")

    if args.mode == "audit":
        return 0
    return 1


if __name__ == "__main__":
    sys.exit(main())
