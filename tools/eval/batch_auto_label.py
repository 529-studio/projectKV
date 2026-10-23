#!/usr/bin/env python3
"""
Batch Auto-Labeler for Raw Archiver Snapshots with Rate-Limiting and Resilient Retry.

Inspects raw snapshots on the Go server archiver for a given date,
runs local OpenVINO CV engine inference, and automatically posts completed
labels to the Web Labeler API (annotator="opencode_auto").

Includes:
- Client-side Token Bucket rate limiter (default 8 req/sec) to respect Go backend limits.
- Exponential backoff retry with Retry-After header parsing for HTTP 429.
- Optional --unique-cameras flag to deduplicate and process only latest frame per camera.

Usage:
    python3 tools/eval/batch_auto_label.py --date 2026-10-23 --limit 200
    python3 tools/eval/batch_auto_label.py --unique-cameras --limit 200
"""

import argparse
import concurrent.futures
import json
import logging
import os
import sys
import threading
import time
import urllib.error
import urllib.parse
import urllib.request
from typing import Any, Dict, List, Optional, Set, Tuple

logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s [%(levelname)s] %(message)s",
    datefmt="%H:%M:%S",
)

PCU_WEIGHTS = {
    "motorcycle": 0.3,
    "car": 1.0,
    "bus": 2.5,
    "truck": 2.5,
    "container": 3.5,
}


class TokenBucketRateLimiter:
    """Thread-safe Token Bucket rate limiter for client-side pacing."""

    def __init__(self, rps: float, burst: float):
        self.rps = max(0.1, rps)
        self.burst = max(1.0, burst)
        self.tokens = self.burst
        self.last = time.time()
        self.lock = threading.Lock()

    def acquire(self):
        while True:
            with self.lock:
                now = time.time()
                elapsed = now - self.last
                self.tokens = min(self.burst, self.tokens + elapsed * self.rps)
                self.last = now

                if self.tokens >= 1.0:
                    self.tokens -= 1.0
                    return
                # Calculate sleep duration until next token is available
                needed = 1.0 - self.tokens
                sleep_sec = needed / self.rps
            time.sleep(sleep_sec)


def load_dotenv_fallback():
    """Loads key-value pairs from .env if not already in os.environ."""
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
    """Initializes and returns boto3 S3 client for Cloudflare R2 if available."""
    try:
        import boto3
        from botocore.config import Config
    except ImportError:
        return None

    endpoint = os.getenv("R2_ENDPOINT", "")
    access_key = os.getenv("R2_ACCESS_KEY_ID", "")
    secret_key = os.getenv("R2_SECRET_ACCESS_KEY", "")

    if not endpoint or not access_key or not secret_key:
        return None

    return boto3.client(
        service_name="s3",
        endpoint_url=endpoint,
        aws_access_key_id=access_key,
        aws_secret_access_key=secret_key,
        region_name="auto",
        config=Config(s3={"addressing_style": "path"}, max_pool_connections=50),
    )


def send_http_request_with_retry(
    req: urllib.request.Request,
    limiter: Optional[TokenBucketRateLimiter] = None,
    max_retries: int = 5,
    timeout: float = 60.0,
) -> Tuple[int, bytes, Dict[str, str]]:
    """
    Executes an HTTP request with pacing and exponential backoff retry on HTTP 429.
    Returns (status_code, body_bytes, headers_dict).
    """
    for attempt in range(max_retries):
        if limiter:
            limiter.acquire()

        try:
            with urllib.request.urlopen(req, timeout=timeout) as resp:
                status = resp.status
                body = resp.read()
                headers = dict(resp.getheaders())
                return status, body, headers
        except urllib.error.HTTPError as e:
            if e.code == 429 and attempt < max_retries - 1:
                # Read Retry-After header or compute backoff
                retry_after_str = e.headers.get("Retry-After", "")
                try:
                    retry_sec = float(retry_after_str)
                except ValueError:
                    retry_sec = 1.0 * (1.5**attempt)
                retry_sec = max(0.5, retry_sec)
                time.sleep(retry_sec)
                continue
            raise
        except Exception:
            if attempt < max_retries - 1:
                time.sleep(0.5 * (attempt + 1))
                continue
            raise

    raise RuntimeError(f"Failed after {max_retries} retries")


def detect_vehicles(
    image_bytes: bytes, cv_engine_url: str = "http://localhost:8000", timeout: float = 10.0
) -> Dict[str, Any]:
    url = f"{cv_engine_url.rstrip('/')}/predict"
    req = urllib.request.Request(
        url,
        data=image_bytes,
        headers={"Content-Type": "application/octet-stream"},
        method="POST",
    )
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            data = json.loads(resp.read().decode("utf-8"))
        return {
            "vehicles_detected": int(data.get("vehicles_detected", 0)),
            "vehicle_counts": data.get("vehicle_counts", {}),
            "confidence": float(data.get("confidence", 0.0)),
        }
    except Exception as e:
        return {
            "vehicles_detected": 0,
            "vehicle_counts": {},
            "confidence": 0.0,
            "error": str(e),
        }


def calculate_traffic_level(counts: Dict[str, int], vehicles_detected: int) -> str:
    pcu = 0.0
    if counts:
        pcu = (
            counts.get("motorcycle", 0) * PCU_WEIGHTS["motorcycle"]
            + counts.get("car", 0) * PCU_WEIGHTS["car"]
            + counts.get("bus", 0) * PCU_WEIGHTS["bus"]
            + counts.get("truck", 0) * PCU_WEIGHTS["truck"]
            + counts.get("container", 0) * PCU_WEIGHTS["container"]
        )
    else:
        pcu = vehicles_detected * PCU_WEIGHTS["car"]

    if pcu >= 35.0:
        return "jam"
    elif pcu >= 22.0:
        return "heavy"
    elif pcu >= 10.0:
        return "moderate"
    return "clear"


def main():
    load_dotenv_fallback()

    parser = argparse.ArgumentParser(description="Resilient Batch Auto-Labeler for Archiver Snapshots")
    parser.add_argument(
        "--date",
        type=str,
        default=time.strftime("%Y-%m-%d"),
        help="Date partition (YYYY-MM-DD)",
    )
    parser.add_argument(
        "--limit",
        type=int,
        default=200,
        help="Maximum snapshots to fetch and label (default: 200)",
    )
    parser.add_argument(
        "--api-url",
        type=str,
        default=os.getenv("API_URL", "http://localhost:3000"),
        help="Go Backend API base URL",
    )
    parser.add_argument(
        "--cv-engine-url",
        type=str,
        default=os.getenv("CV_ENGINE_URL", "http://localhost:8000"),
        help="OpenVINO inference server URL",
    )
    parser.add_argument(
        "--dev-token",
        type=str,
        default=os.getenv("DEV_ACCESS_TOKEN", ""),
        help="Developer access token for API",
    )
    parser.add_argument(
        "--annotator",
        type=str,
        default="opencode_auto",
        help="Annotator signature (default: opencode_auto, or 'harvester/auto')",
    )
    parser.add_argument(
        "--concurrency",
        type=int,
        default=4,
        help="Parallel inference workers (default: 4)",
    )
    parser.add_argument(
        "--rps",
        type=float,
        default=8.0,
        help="Client-side request rate limit (req/sec) to prevent HTTP 429 (default: 8.0)",
    )
    parser.add_argument(
        "--unique-cameras",
        action="store_true",
        help="Deduplicate by camera_id, labeling only the latest snapshot per camera",
    )
    parser.add_argument(
        "--all",
        action="store_true",
        help="Process ALL remaining unlabeled snapshots for the date (equivalent to --limit 0)",
    )
    parser.add_argument(
        "--direct-r2",
        action="store_true",
        default=True,
        help="Use direct Cloudflare R2 listing, streaming, and uploading when credentials are present (default: True)",
    )
    parser.add_argument(
        "--no-direct-r2",
        action="store_false",
        dest="direct_r2",
        help="Force using Go Backend HTTP API instead of direct Cloudflare R2",
    )

    args = parser.parse_args()
    if args.all:
        args.limit = 0

    headers = {}
    if args.dev_token:
        headers["Authorization"] = f"Bearer {args.dev_token}"

    limiter = TokenBucketRateLimiter(rps=args.rps, burst=15.0)

    s3_client = get_r2_client() if args.direct_r2 else None
    bucket = os.getenv("R2_BUCKET", "warded-proofs")

    if s3_client:
        logging.info(f"Direct Cloudflare R2 acceleration active (bucket={bucket})...")
    else:
        logging.info(f"Connecting to API at {args.api_url} for date {args.date} (Pacing: {args.rps} RPS)...")

    # 1. Fetch existing labels
    labels_url = f"{args.api_url.rstrip('/')}/api/archiver/labels?date={args.date}"
    req = urllib.request.Request(labels_url, headers=headers)
    existing_labeled_keys: Set[str] = set()
    try:
        _, body, _ = send_http_request_with_retry(req, limiter=limiter, timeout=30.0)
        data = json.loads(body.decode("utf-8"))
        for lbl in data.get("labels", []):
            key = lbl.get("snapshot_key")
            if key:
                existing_labeled_keys.add(key)
    except Exception as e:
        logging.warning(f"Could not fetch existing labels via API: {e}")

    logging.info(f"Found {len(existing_labeled_keys)} already labeled snapshots for {args.date}.")

    # 2. Fetch raw snapshots
    snapshots: List[Dict[str, Any]] = []
    if s3_client:
        logging.info(f"Listing raw snapshots directly from Cloudflare R2 under prefix '{args.date}/'...")
        paginator = s3_client.get_paginator("list_objects_v2")
        for prefix in [f"{args.date}/", f"proofs/{args.date}/"]:
            for page in paginator.paginate(Bucket=bucket, Prefix=prefix):
                for obj in page.get("Contents", []):
                    k = obj["Key"]
                    if not k.lower().endswith((".jpg", ".jpeg", ".png")):
                        continue
                    parts = k.split("/")
                    if len(parts) >= 3:
                        cam_id = parts[1] if parts[0] != "proofs" else parts[2]
                        filename = parts[-1]
                        name_no_ext = os.path.splitext(filename)[0]
                        file_parts = name_no_ext.split("_", 1)
                        ts = int(file_parts[0]) if file_parts[0].isdigit() else int(time.time())
                        reason = file_parts[1] if len(file_parts) > 1 else "sample"
                        snapshots.append({
                            "key": k,
                            "camera_id": cam_id,
                            "timestamp": ts,
                            "reason": reason,
                            "size": obj["Size"],
                        })
        snapshots.sort(key=lambda s: s.get("timestamp", 0), reverse=True)
    else:
        snaps_url = f"{args.api_url.rstrip('/')}/api/archiver/snapshots?date={args.date}&limit={args.limit if args.limit > 0 else 50000}"
        req = urllib.request.Request(snaps_url, headers=headers)
        try:
            _, body, _ = send_http_request_with_retry(req, limiter=limiter, timeout=60.0)
            data = json.loads(body.decode("utf-8"))
            snapshots = data.get("snapshots", [])
        except Exception as e:
            logging.error(f"Failed to fetch snapshots from {snaps_url}: {e}")
            sys.exit(1)

    unlabeled = [s for s in snapshots if s.get("key") not in existing_labeled_keys]

    if args.unique_cameras:
        seen_cams = set()
        deduped = []
        for s in unlabeled:
            cam_id = s.get("camera_id")
            if cam_id and cam_id not in seen_cams:
                seen_cams.add(cam_id)
                deduped.append(s)
        logging.info(
            f"Deduplicated to {len(deduped)} unique camera snapshot(s) from {len(unlabeled)} candidates."
        )
        unlabeled = deduped

    if args.limit > 0:
        unlabeled = unlabeled[:args.limit]

    logging.info(
        f"Retrieved {len(snapshots)} snapshots ({len(unlabeled)} pending labeling, {len(snapshots) - len(unlabeled)} already labeled)."
    )

    if not unlabeled:
        logging.info("All selected snapshots are already labeled. Nothing to do!")
        return

    # 3. Process unlabeled snapshots with rate pacing and automatic retry
    post_url = f"{args.api_url.rstrip('/')}/api/archiver/labels"
    success_count = 0
    fail_count = 0
    processed_count = 0
    total_unlabeled = len(unlabeled)

    def process_snapshot(snap: Dict[str, Any]) -> bool:
        nonlocal processed_count
        key = snap.get("key", "")
        cam_id = snap.get("camera_id", "")
        if not key or not cam_id:
            return False

        # Download raw frame
        if s3_client:
            frame_bytes = None
            for attempt in range(3):
                try:
                    resp = s3_client.get_object(Bucket=bucket, Key=key)
                    frame_bytes = resp["Body"].read()
                    break
                except Exception as e:
                    if attempt == 2:
                        logging.warning(f"Failed to read image {key} from R2: {e}")
                        return False
                    time.sleep(0.5 * (attempt + 1))
        else:
            raw_url = f"{args.api_url.rstrip('/')}/api/archiver/snapshots/raw?key={urllib.parse.quote(key)}"
            req_raw = urllib.request.Request(raw_url, headers=headers)
            try:
                _, frame_bytes, _ = send_http_request_with_retry(req_raw, limiter=limiter)
            except Exception as e:
                logging.warning(f"Failed to fetch raw image {key}: {e}")
                return False

        # Send to OpenVINO CV engine
        pred = detect_vehicles(frame_bytes, cv_engine_url=args.cv_engine_url)
        traffic_level = calculate_traffic_level(pred["vehicle_counts"], pred["vehicles_detected"])

        # Determine time of day based on current hour
        cur_hour = time.localtime().tm_hour
        tod = "day" if 6 <= cur_hour < 18 else "night"

        ts = snap.get("timestamp") or int(time.time())
        payload = {
            "date": args.date,
            "camera_id": cam_id,
            "snapshot_key": key,
            "annotator": args.annotator,
            "flood_level": "none",
            "traffic_level": traffic_level,
            "traffic_dir_a": traffic_level,
            "traffic_dir_b": "not_applicable",
            "queue_reason": "free_flow",
            "flood_submersion_anchor": "dry",
            "weather": "dry",
            "time_of_day": tod,
            "notes": f"Auto-labeled by {args.annotator} (OpenVINO detected {pred['vehicles_detected']} vehicles)",
            "timestamp": ts,
        }

        # Save label
        if s3_client:
            clean_cam = os.path.basename(cam_id.strip()).replace("/", "_").replace("\\", "_")
            clean_annotator = os.path.basename(args.annotator.strip()).replace("/", "_").replace("\\", "_")
            label_key = f"labels/{args.date}/{clean_cam}_{clean_annotator}_{ts}.json"
            label_bytes = json.dumps(payload, indent=2).encode("utf-8")
            for attempt in range(3):
                try:
                    s3_client.put_object(
                        Bucket=bucket,
                        Key=label_key,
                        Body=label_bytes,
                        ContentType="application/json",
                    )
                    return True
                except Exception as e:
                    if attempt == 2:
                        logging.warning(f"Failed to upload label to R2 for {key}: {e}")
                        return False
                    time.sleep(0.5 * (attempt + 1))
        else:
            req_post = urllib.request.Request(
                post_url,
                data=json.dumps(payload).encode("utf-8"),
                headers={**headers, "Content-Type": "application/json"},
                method="POST",
            )
            try:
                status, _, _ = send_http_request_with_retry(req_post, limiter=limiter)
                return status in (200, 201)
            except Exception as e:
                logging.warning(f"Failed to save label for {key}: {e}")
                return False

    start_time = time.time()
    with concurrent.futures.ThreadPoolExecutor(max_workers=args.concurrency) as executor:
        futures = {executor.submit(process_snapshot, s): s for s in unlabeled}
        for future in concurrent.futures.as_completed(futures):
            processed_count += 1
            if future.result():
                success_count += 1
            else:
                fail_count += 1

            if processed_count % 100 == 0 or processed_count == total_unlabeled:
                elapsed_now = max(0.01, time.time() - start_time)
                rate = processed_count / elapsed_now
                eta_sec = (total_unlabeled - processed_count) / max(0.1, rate)
                logging.info(
                    f"Progress: {processed_count}/{total_unlabeled} ({processed_count*100//total_unlabeled}%) | "
                    f"Speed: {rate:.1f} snaps/sec | ETA: {int(eta_sec//60)}m{int(eta_sec%60):02d}s | "
                    f"Success: {success_count}, Failed: {fail_count}"
                )

    elapsed = time.time() - start_time
    logging.info(
        f"Completed: {success_count} snapshots auto-labeled successfully, {fail_count} failed in {elapsed:.2f}s "
        f"({success_count / max(0.01, elapsed):.1f} snaps/sec)."
    )


if __name__ == "__main__":
    main()
