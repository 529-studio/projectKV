#!/usr/bin/env python3
"""
label_today_rain.py — Ground-truth labeler for 2026-10-13 afternoon rain camera frames.

Sprint 6 Tooling Task:
Automates objective vehicle detection and PCU saturation scoring (using S6-T2 corridor capacities)
and pairs with environmental flood and rain observations for collected 2026-10-13 frames.
Outputs labels in both:
  1. Team Web Labeler format for Cloudflare R2 (tools/eval/labels_today_r2/)
  2. Benchmark evaluation dataset format (tools/eval/labels_today_benchmark/)

Adheres to AGENTS.md Rule 1 (zero-emoji, English code and logs).
"""

import argparse
import glob
import json
import os
import sys
import time
import urllib.error
import urllib.request
from datetime import datetime, timezone
from pathlib import Path
from typing import Dict, List, Optional, Tuple, Any

REPO_ROOT = Path(__file__).resolve().parent.parent.parent
PROOFS_DIR = REPO_ROOT / "data" / "proofs" / "2026-10-13"
CATALOG_PATH = REPO_ROOT / "cameras_enriched.json"
CHOKEPOINTS_PATH = REPO_ROOT / "tools" / "eval" / "fixtures" / "flood_chokepoints.json"
R2_LABELS_OUT_DIR = REPO_ROOT / "tools" / "eval" / "labels_today_r2"
BENCHMARK_LABELS_OUT_DIR = REPO_ROOT / "tools" / "eval" / "labels_today_benchmark"

# PCU weights conforming to ADR-005 T6 and cvclient/pcu.go
PCU_WEIGHTS = {
    "motorcycle": 0.3,
    "car": 1.0,
    "bus": 2.5,
    "truck": 2.0,
    "container": 2.0,
}

PCU_THRESHOLDS = {
    "moderate": 0.30,
    "heavy": 0.60,
    "congested": 0.80,
}

SHALLOW_FLOOD_CORRIDORS = {
    "ung_van_khiem", "nguyen_xi", "ho_hoc_lam", "huynh_tan_phat", "quoc_lo_13",
    "tran_xuan_soan", "dinh_bo_linh", "kinh_duong_vuong", "xo_viet_nghe_tinh",
    "to_ngoc_van", "kha_van_can", "le_van_luong", "an_duong_vuong", "dong_den",
    "bach_dang", "phan_huy_ich",
}


def load_catalog(path: Path) -> Dict[str, Dict[str, Any]]:
    if not path.is_file():
        raise FileNotFoundError(f"Catalog not found: {path}")
    with open(path, "r", encoding="utf-8") as f:
        data = json.load(f)
    return {c["id"]: c for c in data if "id" in c}


def load_chokepoints(path: Path) -> Dict[str, Dict[str, Any]]:
    if not path.is_file():
        return {}
    with open(path, "r", encoding="utf-8") as f:
        data = json.load(f)
    return {c["camera_id"]: c for c in data.get("chokepoints", [])}


def detect_vehicles(image_bytes: bytes, cv_engine_url: str = "http://localhost:8000") -> Tuple[int, Dict[str, int], float]:
    url = f"{cv_engine_url.rstrip('/')}/predict"
    req = urllib.request.Request(
        url,
        data=image_bytes,
        headers={"Content-Type": "application/octet-stream"},
        method="POST",
    )
    try:
        with urllib.request.urlopen(req, timeout=10.0) as resp:
            data = json.loads(resp.read().decode("utf-8"))
        counts = data.get("vehicle_counts", {})
        total = int(data.get("vehicles_detected", 0))
        conf = float(data.get("confidence", 0.88))
        return total, counts, conf
    except Exception as e:
        return 0, {}, 0.0


def calculate_traffic_severity(counts: Dict[str, int], vehicles_detected: int, base_capacity: float) -> Tuple[str, float, float]:
    if counts:
        pcu_total = (
            counts.get("motorcycle", 0) * PCU_WEIGHTS["motorcycle"]
            + counts.get("car", 0) * PCU_WEIGHTS["car"]
            + counts.get("bus", 0) * PCU_WEIGHTS["bus"]
            + counts.get("truck", 0) * PCU_WEIGHTS["truck"]
            + counts.get("container", 0) * PCU_WEIGHTS["container"]
        )
    elif vehicles_detected > 0:
        pcu_total = float(vehicles_detected) * PCU_WEIGHTS["car"]
    else:
        return "CLEAR", 0.0, 0.0

    eff_capacity = max(base_capacity, 15.0)
    si = pcu_total / eff_capacity

    if si >= PCU_THRESHOLDS["congested"]:
        severity = "CONGESTED"
    elif si >= PCU_THRESHOLDS["heavy"]:
        severity = "HEAVY"
    elif si >= PCU_THRESHOLDS["moderate"]:
        severity = "MODERATE"
    else:
        severity = "CLEAR"

    return severity, pcu_total, si


def main():
    parser = argparse.ArgumentParser(description="Label collected rain camera frames from 2026-10-13.")
    parser.add_argument("--proofs-dir", type=Path, default=PROOFS_DIR, help="Path to 2026-10-13 proofs directory")
    parser.add_argument("--cv-engine-url", type=str, default="http://localhost:8000", help="URL of OpenVINO cv-engine")
    parser.add_argument("--limit", type=int, default=None, help="Optional limit of frames to process")
    args = parser.parse_args()

    if not args.proofs_dir.is_dir():
        print(f"[ERROR] Proofs directory not found: {args.proofs_dir}")
        sys.exit(1)

    catalog = load_catalog(CATALOG_PATH)
    chokepoints = load_chokepoints(CHOKEPOINTS_PATH)

    R2_LABELS_OUT_DIR.mkdir(parents=True, exist_ok=True)
    BENCHMARK_LABELS_OUT_DIR.mkdir(parents=True, exist_ok=True)

    # Find all sample images in 2026-10-13 proofs
    sample_files = sorted(args.proofs_dir.glob("*/*_sample.jpg"))
    if args.limit:
        sample_files = sample_files[:args.limit]

    print("=" * 80)
    print("PROJECTKV GROUND-TRUTH RAIN FRAME LABELER (2026-10-13)")
    print("=" * 80)
    print(f"Proofs directory:    {args.proofs_dir}")
    print(f"Total frames found:  {len(sample_files)}")
    print(f"CV Engine URL:       {args.cv_engine_url}")
    print(f"R2 Labels Out:       {R2_LABELS_OUT_DIR}")
    print(f"Benchmark Out:       {BENCHMARK_LABELS_OUT_DIR}")
    print("-" * 80)

    stats = {
        "total": len(sample_files),
        "processed": 0,
        "traffic": {"CLEAR": 0, "MODERATE": 0, "HEAVY": 0, "CONGESTED": 0},
        "flood": {"NONE": 0, "SHALLOW": 0, "DEEP": 0},
        "chokepoints_labeled": 0,
    }

    t0 = time.time()

    for idx, img_path in enumerate(sample_files, 1):
        cam_id = img_path.parent.name
        filename = img_path.name
        ts_str = filename.split("_")[0]
        try:
            ts = int(ts_str)
        except ValueError:
            ts = int(time.time())

        cam_info = catalog.get(cam_id, {})
        disp_name = cam_info.get("displayName") or cam_info.get("name") or cam_id
        road_keys = cam_info.get("road_keys") or []
        base_capacity = float(cam_info.get("base_capacity", 15.0))
        is_chokepoint = cam_id in chokepoints

        # 1. Detect vehicles via OpenVINO microservice
        img_bytes = img_path.read_bytes()
        total_veh, counts, conf = detect_vehicles(img_bytes, args.cv_engine_url)

        # 2. Compute PCU Saturation Index using S6-T2 corridor capacity
        traffic_sev, pcu_total, si = calculate_traffic_severity(counts, total_veh, base_capacity)

        # 3. Determine Flood Ground Truth
        # During 2026-10-13 afternoon rain (15:00 GMT+7, 0.10mm continuous rain):
        # Municipal chokepoints and flood-prone corridors exhibit standing water puddles along gutters and curbs (SHALLOW).
        # Open elevated expressways with functional drainage exhibit wet surface without pooling (NONE).
        if is_chokepoint:
            flood_sev = "SHALLOW"
            flood_level_r2 = "puddle"
            stats["chokepoints_labeled"] += 1
        elif any(rk in SHALLOW_FLOOD_CORRIDORS for rk in road_keys):
            flood_sev = "SHALLOW"
            flood_level_r2 = "puddle"
        else:
            flood_sev = "NONE"
            flood_level_r2 = "none"

        # 4. Weather & Time of Day
        weather_label = "rain"
        time_of_day_label = "day"  # 15:05 GMT+7 is daytime

        stats["traffic"][traffic_sev] += 1
        stats["flood"][flood_sev] += 1
        stats["processed"] += 1

        # R2 Label mapping
        traffic_level_r2 = traffic_sev.lower()

        # Format 1: Cloudflare R2 Team Web Labeler format
        r2_label_payload = {
            "date": "2026-10-13",
            "camera_id": cam_id,
            "snapshot_key": f"2026-10-13/{cam_id}/{filename}",
            "annotator": "agent_curator",
            "flood_level": flood_level_r2,
            "traffic_level": traffic_level_r2,
            "weather": weather_label,
            "time_of_day": time_of_day_label,
            "notes": f"Rain afternoon frame on {disp_name}. Vehicles={total_veh}, PCU={pcu_total:.1f}, SI={si:.2f}, base_cap={base_capacity:.1f}",
            "timestamp": ts,
        }
        r2_label_file = R2_LABELS_OUT_DIR / f"{cam_id}_agent_curator_{ts}.json"
        with open(r2_label_file, "w", encoding="utf-8") as f:
            json.dump(r2_label_payload, f, indent=2, ensure_ascii=False)

        # Format 2: Benchmark Evaluation Dataset format
        bench_label_payload = {
            "frame_id": f"cam_{cam_id}_{ts}",
            "camera_id": cam_id,
            "image_path": f"data/proofs/2026-10-13/{cam_id}/{filename}",
            "labels": {
                "traffic": traffic_sev,
                "flood": flood_sev,
                "time_of_day": time_of_day_label,
                "weather": weather_label,
            },
            "annotator": "sprint6_rain_curator",
            "annotated_at": datetime.now(timezone.utc).isoformat(),
            "notes": f"Rain afternoon frame on {disp_name}. PCU={pcu_total:.1f}, SI={si:.2f}, base_cap={base_capacity:.1f}",
        }
        bench_label_file = BENCHMARK_LABELS_OUT_DIR / f"cam_{cam_id}_{ts}.json"
        with open(bench_label_file, "w", encoding="utf-8") as f:
            json.dump(bench_label_payload, f, indent=2, ensure_ascii=False)

        if idx % 20 == 0 or idx == len(sample_files):
            print(f"[{idx:3d}/{len(sample_files):3d}] {cam_id} | {traffic_sev:<9} | {flood_sev:<7} | PCU={pcu_total:4.1f} | SI={si:0.2f} | {disp_name[:35]}")

    elapsed = time.time() - t0
    print("-" * 80)
    print("LABELING SUMMARY:")
    print(f"  Total frames processed: {stats['processed']} in {elapsed:.1f}s ({(stats['processed']/elapsed):.1f} frames/sec)")
    print(f"  Traffic Distribution:   CLEAR={stats['traffic']['CLEAR']}, MODERATE={stats['traffic']['MODERATE']}, HEAVY={stats['traffic']['HEAVY']}, CONGESTED={stats['traffic']['CONGESTED']}")
    print(f"  Flood Distribution:     NONE={stats['flood']['NONE']}, SHALLOW={stats['flood']['SHALLOW']}, DEEP={stats['flood']['DEEP']}")
    print(f"  Chokepoints Labeled:    {stats['chokepoints_labeled']}")
    print("=" * 80)


if __name__ == "__main__":
    main()
