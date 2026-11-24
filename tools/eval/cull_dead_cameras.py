#!/usr/bin/env python3
"""
Automated Feed Health Audit Tool: Dead Camera Synchronization & Culling
Issue #531 / Sprint 18 Task 8

Scans the municipal traffic camera catalog against historical harvest feeds
and outage records. Flags persistent dead feeds as 'stale_culled' while enforcing
arterial corridor coverage floors to prevent coverage voids.
"""

import argparse
import datetime
import json
import logging
import os
import sys
from pathlib import Path
from typing import Any, Dict, List, Optional, Set, Tuple

logging.basicConfig(level=logging.INFO, format="%(asctime)s [%(levelname)s] %(message)s")
logger = logging.getLogger("cull_dead_cameras")


def load_catalog(path: Path) -> List[Dict[str, Any]]:
    """Loads and validates the camera catalog JSON."""
    if not path.is_file():
        raise FileNotFoundError(f"Catalog file not found: {path}")
    with open(path, "r", encoding="utf-8") as f:
        data = json.load(f)
    if not isinstance(data, list):
        raise ValueError(f"Catalog root must be a list of camera objects, got {type(data)}")
    return data


def save_catalog(path: Path, cameras: List[Dict[str, Any]]) -> None:
    """Saves the camera catalog JSON formatted with standard indent."""
    with open(path, "w", encoding="utf-8") as f:
        json.dump(cameras, f, indent=2, ensure_ascii=False)
        f.write("\n")


def build_corridor_index(cameras: List[Dict[str, Any]]) -> Dict[str, List[str]]:
    """Builds a map of road_key -> list of camera IDs."""
    corridors: Dict[str, List[str]] = {}
    for cam in cameras:
        cid = cam.get("id")
        if not cid:
            continue
        road_keys = cam.get("road_keys") or []
        if not road_keys and cam.get("road_key"):
            road_keys = [cam["road_key"]]
        for rk in road_keys:
            rk = rk.strip().lower()
            if rk:
                corridors.setdefault(rk, []).append(cid)
    return corridors


def find_last_seen_timestamp(
    camera_id: str,
    history_dir: Optional[Path],
    now: datetime.datetime,
) -> Optional[datetime.datetime]:
    """Finds the last seen timestamp of a camera from history files or proofs."""
    if not history_dir or not history_dir.is_dir():
        return None

    # Search for camera snapshots matching <camera_id>*.jpg
    latest_mtime: Optional[float] = None
    try:
        for p in history_dir.glob(f"{camera_id}*.*"):
            if p.is_file():
                mtime = p.stat().st_mtime
                if latest_mtime is None or mtime > latest_mtime:
                    latest_mtime = mtime
    except Exception as e:
        logger.debug("Error scanning history for %s: %v", camera_id, e)

    if latest_mtime is not None:
        return datetime.datetime.fromtimestamp(latest_mtime, datetime.timezone.utc)
    return None


def audit_and_cull_cameras(
    cameras: List[Dict[str, Any]],
    history_dir: Optional[Path] = None,
    threshold_days: int = 14,
    min_corridor_coverage: float = 0.50,
    dry_run: bool = False,
    now: Optional[datetime.datetime] = None,
) -> Tuple[List[Dict[str, Any]], Dict[str, Any]]:
    """Audits camera feeds and applies culling with corridor safety floors."""
    if now is None:
        now = datetime.datetime.now(datetime.timezone.utc)

    corridor_index = build_corridor_index(cameras)

    # Map camera by ID for fast lookup
    cam_map = {c["id"]: c for c in cameras if "id" in c}

    # Count currently active cameras per corridor
    corridor_active: Dict[str, int] = {}
    for rk, cids in corridor_index.items():
        corridor_active[rk] = sum(1 for cid in cids if cam_map.get(cid, {}).get("isOnline", True))

    culled_ids: Set[str] = set()
    protected_by_corridor: List[Dict[str, Any]] = []
    candidates_culled: List[Dict[str, Any]] = []

    cutoff = now - datetime.timedelta(days=threshold_days)

    for cam in cameras:
        cid = cam.get("id", "")
        is_online = cam.get("isOnline", True)
        status = cam.get("status", "UP")

        # Determine if camera feed is stale / dead
        is_candidate = False
        reason = ""

        if status in ("NOT_IMAGE", "DECOMMISSIONED", "DEAD_FEED"):
            is_candidate = True
            reason = f"status_{status.lower()}"
        elif not is_online:
            is_candidate = True
            reason = "marked_offline"
        else:
            # Check last seen timestamp if history is available
            last_seen = find_last_seen_timestamp(cid, history_dir, now)
            if last_seen and last_seen < cutoff:
                is_candidate = True
                days_ago = (now - last_seen).days
                reason = f"inactive_{days_ago}d_exceeds_{threshold_days}d"

        if not is_candidate:
            continue

        # Evaluate corridor coverage constraints
        road_keys = cam.get("road_keys") or []
        if not road_keys and cam.get("road_key"):
            road_keys = [cam["road_key"]]

        corridor_violation = False
        violating_corridor = ""
        projected_ratio = 1.0

        for rk in road_keys:
            rk = rk.strip().lower()
            total_corridor = len(corridor_index.get(rk, []))
            active_corridor = corridor_active.get(rk, 0)

            # If corridor has 2 or more cameras, ensure it doesn't fall below min_corridor_coverage
            if total_corridor >= 2:
                # If this camera is currently active and would be culled
                projected_active = active_corridor - 1 if is_online else active_corridor
                ratio = projected_active / total_corridor
                if ratio < min_corridor_coverage:
                    corridor_violation = True
                    violating_corridor = rk
                    projected_ratio = ratio
                    break

        if corridor_violation:
            protected_by_corridor.append({
                "id": cid,
                "display_name": cam.get("displayName", ""),
                "road_key": violating_corridor,
                "reason": reason,
                "projected_coverage": round(projected_ratio, 2),
                "threshold": min_corridor_coverage,
            })
            continue

        # Mark for culling
        candidates_culled.append({
            "id": cid,
            "title": cam.get("title", ""),
            "display_name": cam.get("displayName", ""),
            "road_key": cam.get("road_key", ""),
            "previous_status": status,
            "previous_is_online": is_online,
            "cull_reason": reason,
        })
        culled_ids.add(cid)

        # Update corridor active count
        if is_online:
            for rk in road_keys:
                rk = rk.strip().lower()
                if rk in corridor_active:
                    corridor_active[rk] = max(0, corridor_active[rk] - 1)

    # Produce updated camera list
    updated_cameras = []
    for cam in cameras:
        cam_copy = dict(cam)
        cid = cam_copy.get("id")
        if cid in culled_ids and not dry_run:
            cam_copy["status"] = "stale_culled"
            cam_copy["isOnline"] = False
        updated_cameras.append(cam_copy)

    audit_summary = {
        "timestamp": now.isoformat(),
        "dry_run": dry_run,
        "threshold_days": threshold_days,
        "min_corridor_coverage": min_corridor_coverage,
        "total_cameras": len(cameras),
        "previously_offline": sum(1 for c in cameras if not c.get("isOnline", True)),
        "culled_count": len(candidates_culled),
        "active_remaining": sum(1 for c in updated_cameras if c.get("isOnline", True)),
        "protected_by_corridor_floor": len(protected_by_corridor),
        "protected_cameras": protected_by_corridor,
        "culled_cameras": candidates_culled,
    }

    return updated_cameras, audit_summary


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--catalog", type=Path, default=Path("cameras_enriched.json"), help="Path to camera catalog JSON")
    parser.add_argument("--history", type=Path, default=Path("data/proofs"), help="Path to snapshot proof history directory")
    parser.add_argument("--threshold-days", type=int, default=14, help="Days of consecutive inactivity before culling")
    parser.add_argument("--min-corridor-coverage", type=float, default=0.50, help="Minimum active camera ratio per corridor")
    parser.add_argument("--dry-run", action="store_true", help="Perform audit without modifying catalog JSON")
    parser.add_argument("--output-report", type=Path, default=Path("docs/reports/camera_cull_audit.json"), help="Output audit report path")
    args = parser.parse_args()

    logger.info("Loading catalog from %s", args.catalog)
    cameras = load_catalog(args.catalog)

    updated_cameras, summary = audit_and_cull_cameras(
        cameras=cameras,
        history_dir=args.history,
        threshold_days=args.threshold_days,
        min_corridor_coverage=args.min_corridor_coverage,
        dry_run=args.dry_run,
    )

    # Ensure output directory exists
    args.output_report.parent.mkdir(parents=True, exist_ok=True)
    with open(args.output_report, "w", encoding="utf-8") as f:
        json.dump(summary, f, indent=2, ensure_ascii=False)

    logger.info("Audit Report Summary:")
    logger.info("  Total Cameras:            %d", summary["total_cameras"])
    logger.info("  Previously Offline:       %d", summary["previously_offline"])
    logger.info("  Culled Dead Cameras:      %d", summary["culled_count"])
    logger.info("  Active Online Remaining:  %d", summary["active_remaining"])
    logger.info("  Protected By Corridor:    %d", summary["protected_by_corridor_floor"])
    logger.info("  Audit Report Written:     %s", args.output_report)

    if not args.dry_run:
        save_catalog(args.catalog, updated_cameras)
        logger.info("Updated catalog written to %s", args.catalog)
    else:
        logger.info("Dry-run mode active: %s left untouched", args.catalog)

    return 0


if __name__ == "__main__":
    sys.exit(main())
