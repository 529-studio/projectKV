import os
import json
import re
from pathlib import Path
import cv2
import numpy as np

# Regular expressions for OSD timestamps
TIMESTAMP_PATTERNS = [
    re.compile(r'(\d{1,2}[\.\-\/][A-Za-z]{3,}[\.\-\/]\d{4}\s+\d{2}:\d{2}:\d{2})'), # 24.Sep.2026 14:41:19
    re.compile(r'(\d{1,2}[\.\-\/][A-Za-z]{3,}\s+\d{4}\s+\d{2}:\d{2}:\d{2})'),       # 24.Sep 2026 14:48:35
    re.compile(r'(\d{4}[\-\/]\d{2}[\-\/]\d{2}\s+\d{2}:\d{2}:\d{2})'),               # 2026-09-24 14:48:59
    re.compile(r'(\d{2}:\d{2}:\d{2})')                                               # 14:48:35
]

GHOST_PLACEHOLDER_SIZE = 2638

def inspect_snapshot(img_path):
    """
    Analyze a camera snapshot:
    1. Check if it's a ghost camera (IMAGE NOT AVAILABLE placeholder).
    2. Extract OSD banner region.
    3. Return structured analysis.
    """
    size = os.path.getsize(img_path)
    if size == GHOST_PLACEHOLDER_SIZE:
        return {
            "status": "GHOST",
            "reason": "IMAGE_NOT_AVAILABLE_PLACEHOLDER",
            "fileSize": size,
            "timestamp": None,
            "bannerDetected": False
        }

    img = cv2.imread(str(img_path))
    if img is None:
        return {
            "status": "INVALID_IMAGE",
            "fileSize": size,
            "timestamp": None,
            "bannerDetected": False
        }

    h, w, _ = img.shape
    # Top 30 pixels contains OSD
    banner = img[0:min(32, h), :]

    # Check for presence of dark OSD background
    gray_banner = cv2.cvtColor(banner, cv2.COLOR_BGR2GRAY)
    is_dark = np.mean(gray_banner) < 80

    return {
        "status": "OK",
        "fileSize": size,
        "resolution": f"{w}x{h}",
        "bannerDetected": is_dark
    }

def main():
    project_root = Path(__file__).resolve().parent.parent.parent
    snapshot_dir = project_root / "snapshots" / "crawl_2026-09-24T07-48-35"
    catalog_file = project_root / "cameras_all.json"

    with open(catalog_file, "r", encoding="utf-8") as f:
        catalog = json.load(f)
    cam_meta = {c["id"]: c for c in catalog}

    images = list(snapshot_dir.glob("*.jpg"))
    print(f"Inspecting {len(images)} camera snapshots...")

    ghost_count = 0
    valid_count = 0

    for img_path in images:
        res = inspect_snapshot(img_path)
        if res["status"] == "GHOST":
            ghost_count += 1
            cid = img_path.stem
            meta = cam_meta.get(cid, {})
            print(f"[GHOST DETECTED] ID: {cid} | Name: {meta.get('displayName')}")
        else:
            valid_count += 1

    print("\n========================================================")
    print(" INSPECTION SUMMARY")
    print("========================================================")
    print(f" - Valid Live Cameras: {valid_count}")
    print(f" - Ghost Cameras (2638 bytes): {ghost_count}")
    print("========================================================\n")

if __name__ == "__main__":
    main()
