#!/usr/bin/env python3
"""
High-Throughput Bulk Label Approval Pipeline with Cloudflare R2 Atomic Promotion.

Promotes AI-generated candidate labels (e.g., annotator='opencode_auto' or 'harvester/auto')
into human-verified ground-truth labels (default: annotator='thdat-bot') while maintaining
audit provenance, preventing storage bloat by purging obsolete auto keys, and triggering
cache invalidation on the Go backend server.

Usage:
    python3 tools/eval/batch_approve_labels.py --date 2026-10-23 --dry-run --limit 20
    python3 tools/eval/batch_approve_labels.py --date 2026-10-23 --reviewer thdat-bot --concurrency 30
"""

import argparse
import concurrent.futures
import json
import logging
import os
import re
import sys
import time
import urllib.error
import urllib.request
from typing import Any, Dict, List, Optional, Tuple

logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s [%(levelname)s] %(message)s",
    datefmt="%H:%M:%S",
)


def is_auto_annotator(annotator: Optional[str]) -> bool:
    """Returns True if the annotator string represents an automated machine pipeline."""
    if not annotator:
        return False
    val = annotator.strip().lower()
    return (
        val.startswith("harvester/")
        or val.startswith("auto_")
        or val.endswith("_auto")
        or "/auto" in val
        or "opencode" in val
        or val == "auto_harvester"
        or val == "auto"
    )


def compute_approved_key(
    old_key: str, reviewer: str, cam_id: Optional[str] = None, ts: Optional[int] = None
) -> str:
    """
    Computes new R2 object key replacing the automated annotator with reviewer identifier.
    Pattern: labels/{date}/{camera_id}_{annotator}_{timestamp}.json
    """
    clean_reviewer = os.path.basename(reviewer.strip()).replace("/", "_").replace("\\", "_")
    parts = old_key.split("/")
    if len(parts) >= 3 and parts[0] == "labels":
        date = parts[1]
        filename = parts[2]
        dot_idx = filename.rfind(".")
        name_no_ext = filename[:dot_idx] if dot_idx != -1 else filename

        if cam_id and ts:
            clean_cam = os.path.basename(str(cam_id).strip()).replace("/", "_").replace("\\", "_")
            return f"labels/{date}/{clean_cam}_{clean_reviewer}_{ts}.json"

        m = re.match(r"^(.*?)(?:_(?:opencode_auto|auto_harvester|harvester_auto|auto))_(\d+)$", name_no_ext)
        if m:
            detected_cam = m.group(1)
            detected_ts = m.group(2)
            return f"labels/{date}/{detected_cam}_{clean_reviewer}_{detected_ts}.json"

        subparts = name_no_ext.rsplit("_", 2)
        if len(subparts) == 3:
            detected_cam = subparts[0]
            detected_ts = subparts[2]
            return f"labels/{date}/{detected_cam}_{clean_reviewer}_{detected_ts}.json"

    # Fallback pattern if non-standard
    dirname = os.path.dirname(old_key)
    base = os.path.basename(old_key)
    return f"{dirname}/{clean_reviewer}_{base}"


def transform_label_payload(
    payload: Dict[str, Any], reviewer: str, approved_timestamp: Optional[int] = None
) -> Dict[str, Any]:
    """
    Upgrades an AI candidate label payload to human-verified status with provenance audit fields.
    """
    promoted = dict(payload)
    original_annotator = payload.get("annotator", "opencode_auto")
    now_ts = approved_timestamp or int(time.time())
    utc_str = time.strftime("%Y-%m-%d %H:%M:%S UTC", time.gmtime(now_ts))

    promoted["annotator"] = reviewer
    promoted["original_annotator"] = original_annotator
    promoted["approved_by"] = reviewer
    promoted["approved_at"] = now_ts

    existing_notes = str(payload.get("notes") or "").strip()
    approval_note = f"[Approved by {reviewer} on {utc_str} from {original_annotator}]"
    if existing_notes:
        promoted["notes"] = f"{existing_notes} {approval_note}"
    else:
        promoted["notes"] = approval_note

    return promoted


def load_dotenv_fallback():
    """Loads key-value pairs from .env into os.environ if missing."""
    env_paths = [".env", "../../.env", "../.env"]
    for path in env_paths:
        if os.path.isfile(path):
            with open(path, "r", encoding="utf-8") as f:
                for line in f:
                    line = line.strip()
                    if not line or line.startswith("#") or "=" not in line:
                        continue
                    k, v = line.split("=", 1)
                    k, v = k.strip(), v.strip().strip("\"'")
                    if k not in os.environ:
                        os.environ[k] = v
            break


def get_r2_client():
    """Initializes and returns boto3 S3 client for Cloudflare R2."""
    try:
        import boto3
        from botocore.config import Config
    except ImportError:
        logging.error("boto3 is required for direct Cloudflare R2 operations. Install via: pip install boto3")
        sys.exit(1)

    endpoint = os.getenv("R2_ENDPOINT", "")
    access_key = os.getenv("R2_ACCESS_KEY_ID", "")
    secret_key = os.getenv("R2_SECRET_ACCESS_KEY", "")

    if not endpoint or not access_key or not secret_key:
        logging.error("Cloudflare R2 credentials (R2_ENDPOINT, R2_ACCESS_KEY_ID, R2_SECRET_ACCESS_KEY) missing from environment.")
        sys.exit(1)

    cfg = Config(
        max_pool_connections=50,
        retries={"max_attempts": 5, "mode": "standard"},
    )
    return boto3.client(
        "s3",
        endpoint_url=endpoint,
        aws_access_key_id=access_key,
        aws_secret_access_key=secret_key,
        config=cfg,
        region_name="auto",
    )


def invalidate_backend_cache(api_url: str, dev_token: str) -> bool:
    """Sends invalidation trigger to Go server archiver cache."""
    url = f"{api_url.rstrip('/')}/api/archiver/cache/invalidate"
    headers = {"Content-Type": "application/json"}
    if dev_token:
        headers["X-Dev-Token"] = dev_token

    req = urllib.request.Request(url, data=b"", headers=headers, method="POST")
    try:
        with urllib.request.urlopen(req, timeout=10.0) as resp:
            if resp.status in (200, 204):
                logging.info(f"Successfully invalidated Go server cache at {url}")
                return True
    except Exception as e:
        logging.warning(f"Failed to invalidate Go server cache at {url}: {e}")
    return False


def main():
    parser = argparse.ArgumentParser(
        description="High-Throughput Bulk Label Approval Pipeline with Cloudflare R2 Atomic Promotion"
    )
    parser.add_argument("--date", type=str, default="", help="Partition date (YYYY-MM-DD). Default: today (ICT)")
    parser.add_argument("--reviewer", type=str, default="thdat-bot", help="Reviewer identifier stamped on approved labels (default: thdat-bot)")
    parser.add_argument("--concurrency", type=int, default=25, help="Number of concurrent worker threads (default: 25)")
    parser.add_argument("--limit", type=int, default=0, help="Maximum number of candidate labels to approve (0 = all)")
    parser.add_argument("--dry-run", action="store_true", help="Simulate promotions without modifying R2 objects")
    parser.add_argument("--keep-old-labels", action="store_true", help="Keep obsolete auto-label JSON files in R2 instead of deleting them")
    parser.add_argument("--no-invalidate-cache", action="store_true", help="Skip Go server cache invalidation post-promotion")
    parser.add_argument("--api-url", type=str, default="http://localhost:3000", help="Go backend API URL (default: http://localhost:3000)")
    args = parser.parse_args()

    load_dotenv_fallback()

    date_str = args.date
    if not date_str:
        # Vietnam Standard Time (ICT, UTC+7)
        now_ict = time.gmtime(time.time() + 7 * 3600)
        date_str = time.strftime("%Y-%m-%d", now_ict)

    bucket = os.getenv("R2_BUCKET_NAME", "warded-proofs")
    s3 = get_r2_client()

    prefix = f"labels/{date_str}/"
    logging.info(f"Scanning Cloudflare R2 bucket '{bucket}' under prefix '{prefix}'...")

    paginator = s3.get_paginator("list_objects_v2")
    auto_candidates: List[str] = []
    total_scanned = 0

    for page in paginator.paginate(Bucket=bucket, Prefix=prefix):
        for obj in page.get("Contents", []):
            total_scanned += 1
            key = obj["Key"]
            if not key.endswith(".json"):
                continue

            # Fast key-name check: does filename indicate an auto-annotator?
            filename = os.path.basename(key)
            parts = filename.replace(".json", "").split("_")
            annotator_candidate = parts[-2] if len(parts) >= 3 else ""

            if is_auto_annotator(annotator_candidate) or "opencode" in key or "auto" in key:
                auto_candidates.append(key)

    logging.info(
        f"Scan complete. Found {len(auto_candidates)} AI candidate labels out of {total_scanned} total objects in {date_str}."
    )

    if not auto_candidates:
        logging.info("No AI candidate labels found to approve. Exiting.")
        return

    if args.limit > 0:
        auto_candidates = auto_candidates[: args.limit]
        logging.info(f"Limited processing queue to {len(auto_candidates)} items as requested by --limit.")

    if args.dry_run:
        logging.info(f"[DRY-RUN] Simulating approval of {len(auto_candidates)} candidate labels by '{args.reviewer}'...")
        sample_size = min(5, len(auto_candidates))
        for sample_key in auto_candidates[:sample_size]:
            new_key = compute_approved_key(sample_key, reviewer=args.reviewer)
            logging.info(f"[DRY-RUN] Transformation plan: {sample_key} -> {new_key}")
        logging.info(f"[DRY-RUN] Completed simulation. {len(auto_candidates)} labels would be promoted to '{args.reviewer}'. No changes written.")
        return

    # Execute promotions with ThreadPoolExecutor
    success_count = 0
    fail_count = 0
    processed_count = 0
    total_queue = len(auto_candidates)
    start_time = time.time()

    def process_item(old_key: str) -> bool:
        new_key = compute_approved_key(old_key, reviewer=args.reviewer)
        try:
            # 1. Read existing label
            resp = s3.get_object(Bucket=bucket, Key=old_key)
            raw_data = resp["Body"].read().decode("utf-8")
            payload = json.loads(raw_data)

            # Refine new_key using payload camera_id and timestamp if available
            new_key = compute_approved_key(
                old_key,
                reviewer=args.reviewer,
                cam_id=payload.get("camera_id"),
                ts=payload.get("timestamp"),
            )

            # 2. Transform payload
            promoted_payload = transform_label_payload(payload, reviewer=args.reviewer)
            promoted_bytes = json.dumps(promoted_payload, indent=2).encode("utf-8")

            # 3. Put new approved object
            s3.put_object(
                Bucket=bucket,
                Key=new_key,
                Body=promoted_bytes,
                ContentType="application/json",
            )

            # 4. Delete old auto-label object to prevent storage doubling
            if not args.keep_old_labels and old_key != new_key:
                s3.delete_object(Bucket=bucket, Key=old_key)

            return True
        except Exception as e:
            logging.warning(f"Failed to promote label {old_key} to {new_key}: {e}")
            return False

    logging.info(f"Executing high-throughput bulk promotion with {args.concurrency} worker threads...")

    with concurrent.futures.ThreadPoolExecutor(max_workers=args.concurrency) as executor:
        futures = {executor.submit(process_item, k): k for k in auto_candidates}
        for future in concurrent.futures.as_completed(futures):
            processed_count += 1
            if future.result():
                success_count += 1
            else:
                fail_count += 1

            if processed_count % 200 == 0 or processed_count == total_queue:
                elapsed = max(0.01, time.time() - start_time)
                rate = processed_count / elapsed
                eta_sec = (total_queue - processed_count) / max(0.1, rate)
                pct = (processed_count * 100) // total_queue
                logging.info(
                    f"Progress: {processed_count}/{total_queue} ({pct}%) | "
                    f"Success: {success_count} | Fail: {fail_count} | "
                    f"Rate: {rate:.1f} labels/sec | ETA: {eta_sec:.0f}s"
                )

    total_elapsed = time.time() - start_time
    logging.info(
        f"Promotion finished in {total_elapsed:.1f}s ({success_count} succeeded, {fail_count} failed) "
        f"at an average speed of {success_count / max(0.01, total_elapsed):.1f} labels/sec."
    )

    # Invalidate Go server cache
    if not args.no_invalidate_cache:
        dev_token = os.getenv("DEV_ACCESS_TOKEN", "") or os.getenv("LABELER_DEV_TOKEN", "")
        invalidate_backend_cache(args.api_url, dev_token)


if __name__ == "__main__":
    main()
