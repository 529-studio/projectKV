import os
import json
from pathlib import Path
from PIL import Image
import imagehash
import numpy as np

def main():
    project_root = Path(__file__).resolve().parent.parent.parent
    snapshot_dir = project_root / "snapshots" / "crawl_2026-09-24T07-48-35"
    catalog_file = project_root / "cameras_all.json"

    with open(catalog_file, "r", encoding="utf-8") as f:
        catalog = json.load(f)
    cam_meta = {c["id"]: c for c in catalog}

    images = list(snapshot_dir.glob("*.jpg"))
    print(f"Found {len(images)} images to analyze.")

    hashes_phash = []
    hashes_dhash = []
    cam_ids = []

    print("Computing perceptual hashes (cropping top 15% OSD banner)...")
    for img_path in images:
        cam_id = img_path.stem
        try:
            with Image.open(img_path) as img:
                # Convert to RGB
                img = img.convert("RGB")
                w, h = img.size
                # Crop top 15% to remove OSD date/time banner
                cropped = img.crop((0, int(h * 0.15), w, h))

                ph = imagehash.phash(cropped, hash_size=8)
                dh = imagehash.dhash(cropped, hash_size=8)

                hashes_phash.append(ph)
                hashes_dhash.append(dh)
                cam_ids.append(cam_id)
        except Exception as e:
            print(f"Error reading {img_path}: {e}")

    n = len(cam_ids)
    print(f"Successfully processed {n} images. Comparing all {n * (n - 1) // 2:,} pairs...")

    duplicates = []
    near_duplicates = []

    # Pairwise comparison
    for i in range(n):
        for j in range(i + 1, n):
            dist_d = hashes_dhash[i] - hashes_dhash[j]
            dist_p = hashes_phash[i] - hashes_phash[j]

            # Max hamming distance for 64-bit hash:
            # dist <= 2: essentially identical scene
            # dist <= 5: very similar scene
            if dist_d == 0 and dist_p == 0:
                duplicates.append((cam_ids[i], cam_ids[j], dist_d, dist_p))
            elif dist_d <= 3 and dist_p <= 3:
                near_duplicates.append((cam_ids[i], cam_ids[j], dist_d, dist_p))

    print("\n========================================================")
    print(f" EXACT DUPLICATE SCENES (Hamming Distance = 0): {len(duplicates)}")
    print("========================================================")
    for cam1, cam2, dd, dp in duplicates:
        m1 = cam_meta.get(cam1, {})
        m2 = cam_meta.get(cam2, {})
        print(f"\n[!] MATCH FOUND (dHash={dd}, pHash={dp}):")
        print(f"  Camera 1: {m1.get('displayName')} (ID: {cam1}, Title: {m1.get('title')})")
        print(f"            GPS: {m1.get('latitude')}, {m1.get('longitude')}")
        print(f"  Camera 2: {m2.get('displayName')} (ID: {cam2}, Title: {m2.get('title')})")
        print(f"            GPS: {m2.get('latitude')}, {m2.get('longitude')}")

    print("\n========================================================")
    print(f" NEAR-DUPLICATE SCENES (Hamming Distance <= 3): {len(near_duplicates)}")
    print("========================================================")
    for cam1, cam2, dd, dp in near_duplicates:
        m1 = cam_meta.get(cam1, {})
        m2 = cam_meta.get(cam2, {})
        print(f"\n[~] NEAR MATCH (dHash={dd}, pHash={dp}):")
        print(f"  Camera 1: {m1.get('displayName')} (ID: {cam1}, Title: {m1.get('title')})")
        print(f"            GPS: {m1.get('latitude')}, {m1.get('longitude')}")
        print(f"  Camera 2: {m2.get('displayName')} (ID: {cam2}, Title: {m2.get('title')})")
        print(f"            GPS: {m2.get('latitude')}, {m2.get('longitude')}")

    # Output JSON summary
    report = {
        "totalImagesAnalyzed": n,
        "exactDuplicates": [
            {
                "cam1": {"id": c1, **cam_meta.get(c1, {})},
                "cam2": {"id": c2, **cam_meta.get(c2, {})},
                "dHashDist": dd,
                "pHashDist": dp
            }
            for c1, c2, dd, dp in duplicates
        ],
        "nearDuplicates": [
            {
                "cam1": {"id": c1, **cam_meta.get(c1, {})},
                "cam2": {"id": c2, **cam_meta.get(c2, {})},
                "dHashDist": dd,
                "pHashDist": dp
            }
            for c1, c2, dd, dp in near_duplicates
        ]
    }

    report_path = snapshot_dir / "duplicate_analysis.json"
    with open(report_path, "w", encoding="utf-8") as f:
        json.dump(report, f, indent=2, ensure_ascii=False)

    print(f"\nReport saved to: {report_path}")

if __name__ == "__main__":
    main()
