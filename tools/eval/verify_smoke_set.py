#!/usr/bin/env python3
"""
verify_smoke_set.py — Verification script for known-state smoke-test cameras.

Part of Sprint 4 T7 / ADR-005 Finding B:
Validates AI model inference outputs and PCU saturation classifications against
curated ground-truth baseline cameras (free-flow CLEAR vs chronic bottleneck CONGESTED).

Zero-emoji, minimalist formatting adhering to AGENTS.md Rule 1.
"""

import argparse
import json
import os
import sys
import urllib.error
import urllib.request
from dataclasses import dataclass, asdict
from datetime import datetime, timezone
from pathlib import Path
from typing import Dict, List, Optional, Tuple, Any

DEFAULT_FIXTURE_PATH = Path(__file__).resolve().parent / "fixtures" / "smoke_test_cameras.json"


@dataclass
class SmokeCamera:
    camera_id: str
    camera_name: str
    title: str
    corridor: str
    road_keys: List[str]
    expected_baseline_traffic: str  # CLEAR or CONGESTED
    expected_si_threshold: str
    validation_time_window: Dict[str, Any]
    rationale: str
    latitude: Optional[float] = None
    longitude: Optional[float] = None


@dataclass
class SmokeVerdict:
    camera_id: str
    traffic: str  # CLEAR, MODERATE, HEAVY, CONGESTED
    saturation_index: Optional[float] = None
    timestamp: Optional[str] = None
    raw_response: Optional[Dict[str, Any]] = None


@dataclass
class VerificationResult:
    camera_id: str
    camera_name: str
    corridor: str
    expected_traffic: str
    actual_traffic: Optional[str]
    actual_si: Optional[float]
    status: str  # PASS, FAIL, MISSING
    discrepancy_details: Optional[str] = None


def load_smoke_cameras(fixture_path: Path) -> List[SmokeCamera]:
    """Loads smoke test camera specifications from a JSON fixture file."""
    if not fixture_path.exists():
        raise FileNotFoundError(f"Fixture file not found: {fixture_path}")

    with open(fixture_path, "r", encoding="utf-8") as f:
        data = json.load(f)

    raw_cameras = data.get("cameras", data) if isinstance(data, dict) else data
    if not isinstance(raw_cameras, list):
        raise ValueError(f"Expected list of cameras in fixture: {fixture_path}")

    cameras = []
    for item in raw_cameras:
        coords = item.get("coordinates") or {}
        cameras.append(
            SmokeCamera(
                camera_id=item["camera_id"],
                camera_name=item.get("camera_name") or item.get("displayName") or item["camera_id"],
                title=item.get("title", ""),
                corridor=item.get("corridor", ""),
                road_keys=item.get("road_keys", []),
                expected_baseline_traffic=item["expected_baseline_traffic"],
                expected_si_threshold=item.get("expected_si_threshold", ""),
                validation_time_window=item.get("validation_time_window", {}),
                rationale=item.get("rationale", ""),
                latitude=coords.get("latitude"),
                longitude=coords.get("longitude"),
            )
        )
    return cameras


def parse_verdict_entry(cam_id: str, entry: Any) -> SmokeVerdict:
    """Parses various verdict formats into a normalized SmokeVerdict."""
    if isinstance(entry, str):
        return SmokeVerdict(camera_id=cam_id, traffic=entry.upper())

    if isinstance(entry, dict):
        traffic = (
            entry.get("traffic")
            or entry.get("status")
            or entry.get("traffic_status")
            or entry.get("state")
            or "UNKNOWN"
        ).upper()

        si = entry.get("saturation_index")
        if si is None:
            si = entry.get("pcu_saturation_index")
        if si is None:
            si = entry.get("si")
        if si is not None:
            try:
                si = float(si)
            except (ValueError, TypeError):
                si = None

        return SmokeVerdict(
            camera_id=cam_id,
            traffic=traffic,
            saturation_index=si,
            timestamp=entry.get("timestamp") or entry.get("updated_at"),
            raw_response=entry,
        )

    raise ValueError(f"Unrecognized verdict structure for camera {cam_id}: {entry}")


def load_verdicts_from_file(filepath: Path) -> Dict[str, SmokeVerdict]:
    """Loads verdicts from a JSON file (dict or list format)."""
    if not filepath.exists():
        raise FileNotFoundError(f"Verdicts file not found: {filepath}")

    with open(filepath, "r", encoding="utf-8") as f:
        data = json.load(f)

    verdicts: Dict[str, SmokeVerdict] = {}

    if isinstance(data, dict):
        # Could be {"<cam_id>": {...}} or {"verdicts": [...]} or {"predictions": [...]}
        if "verdicts" in data and isinstance(data["verdicts"], list):
            items = data["verdicts"]
        elif "predictions" in data and isinstance(data["predictions"], list):
            items = data["predictions"]
        elif "cameras" in data and isinstance(data["cameras"], list):
            items = data["cameras"]
        else:
            # Key-value map of camera_id -> verdict
            for k, v in data.items():
                if isinstance(v, (dict, str)):
                    verdicts[k] = parse_verdict_entry(k, v)
            return verdicts
    elif isinstance(data, list):
        items = data
    else:
        raise ValueError(f"Unsupported JSON format in {filepath}")

    for item in items:
        if isinstance(item, dict):
            cid = item.get("camera_id") or item.get("id") or item.get("cameraId")
            if cid:
                verdicts[cid] = parse_verdict_entry(cid, item)

    return verdicts


def fetch_verdicts_from_backend(backend_url: str) -> Dict[str, SmokeVerdict]:
    """Fetches camera status directly from running backend API."""
    base = backend_url.rstrip("/")
    endpoints = [
        f"{base}/api/cameras",
        f"{base}/api/roads",
    ]

    verdicts: Dict[str, SmokeVerdict] = {}

    # Try /api/cameras first
    try:
        url = f"{base}/api/cameras"
        req = urllib.request.Request(url, headers={"User-Agent": "ProjectKV-SmokeVerifier/1.0"})
        with urllib.request.urlopen(req, timeout=10) as resp:
            data = json.loads(resp.read().decode("utf-8"))
            if isinstance(data, list):
                for item in data:
                    cid = item.get("id") or item.get("camera_id")
                    if cid and "status" in item:
                        verdicts[cid] = parse_verdict_entry(cid, item)
            elif isinstance(data, dict) and "cameras" in data:
                for item in data["cameras"]:
                    cid = item.get("id") or item.get("camera_id")
                    if cid:
                        verdicts[cid] = parse_verdict_entry(cid, item)
    except Exception as e:
        sys.stderr.write(f"Warning: Failed to fetch {base}/api/cameras: {e}\n")

    return verdicts


def generate_mock_verdicts(
    cameras: List[SmokeCamera], mismatch_id: Optional[str] = None
) -> Dict[str, SmokeVerdict]:
    """Generates mock verdicts matching the baseline (or with an intentional mismatch)."""
    verdicts: Dict[str, SmokeVerdict] = {}
    now_iso = datetime.now(timezone.utc).isoformat()

    for cam in cameras:
        traffic = cam.expected_baseline_traffic
        si = 0.20 if traffic == "CLEAR" else 0.95

        if mismatch_id and cam.camera_id == mismatch_id:
            # Invert traffic for testing discrepancy detection
            traffic = "CONGESTED" if cam.expected_baseline_traffic == "CLEAR" else "CLEAR"
            si = 0.98 if traffic == "CONGESTED" else 0.15

        verdicts[cam.camera_id] = SmokeVerdict(
            camera_id=cam.camera_id,
            traffic=traffic,
            saturation_index=si,
            timestamp=now_iso,
        )

    return verdicts


def verify_smoke_set(
    cameras: List[SmokeCamera], verdicts: Dict[str, SmokeVerdict]
) -> Tuple[List[VerificationResult], Dict[str, Any]]:
    """Compares actual verdicts against expected baselines."""
    results: List[VerificationResult] = []
    passed_count = 0
    fail_count = 0
    missing_count = 0

    for cam in cameras:
        verdict = verdicts.get(cam.camera_id)
        if not verdict:
            missing_count += 1
            results.append(
                VerificationResult(
                    camera_id=cam.camera_id,
                    camera_name=cam.camera_name,
                    corridor=cam.corridor,
                    expected_traffic=cam.expected_baseline_traffic,
                    actual_traffic=None,
                    actual_si=None,
                    status="MISSING",
                    discrepancy_details="No verdict provided for camera",
                )
            )
            continue

        actual_traffic = verdict.traffic
        actual_si = verdict.saturation_index

        # Verification check:
        # Expected CLEAR matches only CLEAR (MODERATE/HEAVY/CONGESTED are failures)
        # Expected CONGESTED matches CONGESTED (or HEAVY if tolerant, but strict requires CONGESTED)
        is_pass = False
        discrepancies = []

        if cam.expected_baseline_traffic == "CLEAR":
            if actual_traffic == "CLEAR":
                is_pass = True
            else:
                discrepancies.append(
                    f"Expected CLEAR but got {actual_traffic} (potential false-positive congestion)"
                )
            if actual_si is not None and actual_si >= 0.35:
                discrepancies.append(f"SI {actual_si:.2f} >= 0.35 clear ceiling")
                is_pass = False
        elif cam.expected_baseline_traffic == "CONGESTED":
            if actual_traffic in ("CONGESTED", "HEAVY"):
                # Warning if HEAVY instead of CONGESTED, but pass
                is_pass = True
                if actual_traffic == "HEAVY":
                    discrepancies.append("Warning: Classified as HEAVY instead of CONGESTED")
            else:
                discrepancies.append(
                    f"Expected CONGESTED but got {actual_traffic} (false-negative bottleneck detection)"
                )
            if actual_si is not None and actual_si < 0.70:
                discrepancies.append(f"SI {actual_si:.2f} < 0.70 congestion floor")
                is_pass = False
        else:
            discrepancies.append(f"Unknown expected traffic: {cam.expected_baseline_traffic}")

        status = "PASS" if is_pass else "FAIL"
        if is_pass:
            passed_count += 1
        else:
            fail_count += 1

        results.append(
            VerificationResult(
                camera_id=cam.camera_id,
                camera_name=cam.camera_name,
                corridor=cam.corridor,
                expected_traffic=cam.expected_baseline_traffic,
                actual_traffic=actual_traffic,
                actual_si=actual_si,
                status=status,
                discrepancy_details="; ".join(discrepancies) if discrepancies else None,
            )
        )

    stats = {
        "total_cameras": len(cameras),
        "evaluated_cameras": len(cameras) - missing_count,
        "passed": passed_count,
        "failed": fail_count,
        "missing": missing_count,
        "pass_rate": (passed_count / len(cameras)) * 100.0 if cameras else 0.0,
        "is_all_passed": (passed_count == len(cameras)),
    }

    return results, stats


def format_terminal_report(results: List[VerificationResult], stats: Dict[str, Any]) -> str:
    """Formats verification report with monochromatic plain text."""
    lines = []
    lines.append("=" * 80)
    lines.append("PROJECTKV SMOKE-TEST CAMERA VERIFICATION REPORT (T7)")
    lines.append("=" * 80)
    lines.append(
        f"Summary: {stats['passed']}/{stats['total_cameras']} passed "
        f"({stats['pass_rate']:.1f}%), {stats['failed']} failed, {stats['missing']} missing"
    )
    lines.append("-" * 80)
    lines.append(
        f"{'CAMERA ID':<26} {'STATUS':<9} {'EXPECTED':<11} {'ACTUAL':<11} {'SI':<6} {'CORRIDOR'}"
    )
    lines.append("-" * 80)

    for r in results:
        si_str = f"{r.actual_si:.2f}" if r.actual_si is not None else "-"
        act_str = r.actual_traffic or "NONE"
        lines.append(
            f"{r.camera_id:<26} [{r.status}]   {r.expected_traffic:<11} {act_str:<11} {si_str:<6} {r.corridor}"
        )
        if r.discrepancy_details and r.status != "PASS":
            lines.append(f"  -> Discrepancy: {r.discrepancy_details}")

    lines.append("-" * 80)
    verdict = "PASSED" if stats["is_all_passed"] else "FAILED"
    lines.append(f"OVERALL SMOKE VERIFICATION VERDICT: {verdict}")
    lines.append("=" * 80)
    return "\n".join(lines)


def format_markdown_report(results: List[VerificationResult], stats: Dict[str, Any]) -> str:
    """Formats verification report in standard Markdown."""
    lines = []
    lines.append("# Smoke-Test Camera Verification Report")
    lines.append("")
    lines.append(f"- **Generated At**: {datetime.now(timezone.utc).isoformat()}")
    lines.append(f"- **Total Cameras**: {stats['total_cameras']}")
    lines.append(f"- **Passed**: {stats['passed']}")
    lines.append(f"- **Failed**: {stats['failed']}")
    lines.append(f"- **Missing**: {stats['missing']}")
    lines.append(f"- **Pass Rate**: {stats['pass_rate']:.1f}%")
    verdict = "PASS" if stats["is_all_passed"] else "FAIL"
    lines.append(f"- **Overall Verdict**: **{verdict}**")
    lines.append("")
    lines.append("## Detailed Results")
    lines.append("")
    lines.append("| Camera ID | Name | Corridor | Expected | Actual | SI | Status | Details |")
    lines.append("|---|---|---|:---:|:---:|:---:|:---:|---|")

    for r in results:
        si_str = f"{r.actual_si:.2f}" if r.actual_si is not None else "-"
        act_str = r.actual_traffic or "NONE"
        details = r.discrepancy_details or "-"
        lines.append(
            f"| `{r.camera_id}` | {r.camera_name} | `{r.corridor}` | {r.expected_traffic} | {act_str} | {si_str} | **{r.status}** | {details} |"
        )

    lines.append("")
    return "\n".join(lines)


def main(argv: Optional[List[str]] = None) -> int:
    parser = argparse.ArgumentParser(
        description="Verify AI model or backend output against known-state smoke cameras."
    )
    parser.add_argument(
        "--fixtures",
        type=Path,
        default=DEFAULT_FIXTURE_PATH,
        help="Path to smoke_test_cameras.json fixture file",
    )
    parser.add_argument(
        "--verdicts",
        "--predictions",
        type=Path,
        dest="verdicts_file",
        help="Path to JSON file containing camera verdicts/predictions",
    )
    parser.add_argument(
        "--backend-url",
        type=str,
        help="Base URL of running Go server (e.g. http://localhost:3000)",
    )
    parser.add_argument(
        "--mock",
        action="store_true",
        help="Generate synthetic mock verdicts matching baselines",
    )
    parser.add_argument(
        "--mock-mismatch",
        type=str,
        help="Camera ID to intentionally inject mismatch in mock mode (for testing)",
    )
    parser.add_argument(
        "--strict",
        action="store_true",
        help="Exit with non-zero code if any check fails or camera is missing",
    )
    parser.add_argument(
        "--output-markdown",
        type=Path,
        help="Write Markdown report to specified file path",
    )
    parser.add_argument(
        "--output-json",
        type=Path,
        help="Write machine-readable JSON metrics to specified file path",
    )

    args = parser.parse_args(argv)

    # 1. Load fixtures
    try:
        cameras = load_smoke_cameras(args.fixtures)
    except Exception as e:
        sys.stderr.write(f"Error loading smoke camera fixtures: {e}\n")
        return 1

    # 2. Acquire verdicts
    verdicts: Dict[str, SmokeVerdict] = {}
    if args.mock:
        verdicts = generate_mock_verdicts(cameras, mismatch_id=args.mock_mismatch)
    elif args.verdicts_file:
        try:
            verdicts = load_verdicts_from_file(args.verdicts_file)
        except Exception as e:
            sys.stderr.write(f"Error loading verdicts file: {e}\n")
            return 1
    elif args.backend_url:
        verdicts = fetch_verdicts_from_backend(args.backend_url)
    else:
        sys.stderr.write(
            "Error: Must specify one of --verdicts <file>, --backend-url <url>, or --mock\n"
        )
        return 1

    # 3. Verify
    results, stats = verify_smoke_set(cameras, verdicts)

    # 4. Output reports
    print(format_terminal_report(results, stats))

    if args.output_markdown:
        args.output_markdown.parent.mkdir(parents=True, exist_ok=True)
        with open(args.output_markdown, "w", encoding="utf-8") as f:
            f.write(format_markdown_report(results, stats))

    if args.output_json:
        args.output_json.parent.mkdir(parents=True, exist_ok=True)
        out_data = {
            "stats": stats,
            "results": [asdict(r) for r in results],
        }
        with open(args.output_json, "w", encoding="utf-8") as f:
            json.dump(out_data, f, indent=2, ensure_ascii=False)

    if args.strict and not stats["is_all_passed"]:
        return 1

    return 0


if __name__ == "__main__":
    sys.exit(main())
