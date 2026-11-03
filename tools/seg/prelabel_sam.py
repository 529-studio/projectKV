#!/usr/bin/env python3
"""
prelabel_sam.py — SAM-Assisted Polygon Pre-Labeling Batch Toolchain (S9-T2).

Streams real camera frames from Cloudflare R2, runs a compact promptable
segmentation model (MobileSAM class, ONNX) plus the existing YOLOv8 detector
(ONNX) entirely offline on a capable CPU host, and writes candidate polygon
labels (ADR-011 D1 contract, annotator/source "sam_prelabel") back to R2 for
human bulk approval. No automatic promotion: every candidate requires human
verification in /labeler.

Heuristics (issue #298):
- roadway: largest stable central region below the horizon (box prompt with
  a point-grid consensus fallback).
- water_puddle: proposal-only candidates from smooth dark regions; specular
  wet asphalt is indistinguishable from standing water without human
  judgment (ADR-009 section 4.3).
- vehicles: bounding boxes from the existing detection model, refined into
  polygons via box prompts.

Operational guardrails: token-bucket rate limiting, exponential backoff,
--dry-run, --limit, --date filters, and a host guard refusing non-AVX2
machines (the Celeron ingress host is architecturally disqualified and the
live 30s hot-set loop must never host annotation workloads; ADR-011 D3).

Usage:
    python3 tools/seg/prelabel_sam.py --date 2026-10-25 --limit 50 --upload-r2
    python3 tools/seg/prelabel_sam.py --date 2026-10-25 --dry-run

Weight provisioning (AGENTS.md Rule 8 — never hardcoded):
    SAM_PRELABEL_ENCODER_ONNX, SAM_PRELABEL_DECODER_ONNX,
    SAM_PRELABEL_DETECTOR_ONNX (see docs/runbooks/sam_prelabeling.md).

Adheres to AGENTS.md Rules:
- Rule 1: monochromatic, zero-emoji, 100% English code and logs.
- Rule 8: credentials and weight paths loaded dynamically from .env only.
"""

import argparse
import concurrent.futures
from datetime import datetime, timezone, timedelta
import json
import logging
import os
from pathlib import Path
import platform
import subprocess
import sys
import threading
import time
from typing import Any, Callable, Dict, List, Optional, Set, Tuple
import urllib.error

import numpy as np

REPO_ROOT = Path(__file__).resolve().parent.parent.parent

# Script execution (python3 tools/seg/prelabel_sam.py) starts with tools/seg/
# on sys.path; prepend the repo root so the tools.* namespace imports resolve
# inside worker threads as well.
if str(REPO_ROOT) not in sys.path:
    sys.path.insert(0, str(REPO_ROOT))

try:
    from tools.seg import contract as seg_contract
    from tools.seg import image_geometry as geom
    from tools.seg import sam_runtime
except ImportError:  # Direct script execution from tools/seg/.
    import contract as seg_contract  # type: ignore
    import image_geometry as geom  # type: ignore
    import sam_runtime  # type: ignore

logger = logging.getLogger("prelabel_sam")

# Minimum JPEG size: 20 KB (20,480 bytes) per ADR-009 / S7-T7b.
MIN_JPEG_SIZE = 20480

# PCU weights and traffic cutoffs mirroring tools/eval/batch_auto_label.py so
# prelabel candidates agree with the existing auto-label classification.
PCU_WEIGHTS = {
    "motorcycle": 0.3,
    "car": 1.0,
    "bus": 2.5,
    "truck": 2.5,
    "container": 3.5,
}
TRAFFIC_LEVELS = ("clear", "moderate", "heavy", "jam")

# Roadway prompt geometry (normalized frame coordinates).
ROADWAY_AREA_FRAC_RANGE = (0.12, 0.92)
ROADWAY_POINT_GRID = [
    (0.35, 0.60), (0.50, 0.60), (0.65, 0.60),
    (0.35, 0.75), (0.50, 0.75), (0.65, 0.75),
    (0.40, 0.90), (0.60, 0.90),
]
# A point prompt mask is "stable" when it overlaps the reference mask enough.
STABILITY_IOU = 0.5

# Vehicle polygon bounds: reject degenerate SAM refinements.
VEHICLE_MIN_MASK_FRAC = 0.0002
VEHICLE_MAX_MASK_FRAC = 0.6


def load_dotenv() -> None:
    """Dynamically loads key-value pairs from .env without third-party dependencies."""
    for p in [".env", "../../.env", "../.env", str(REPO_ROOT / ".env")]:
        env_file = Path(p)
        if env_file.exists():
            with open(env_file, "r", encoding="utf-8") as f:
                for line in f:
                    line = line.strip()
                    if not line or line.startswith("#") or "=" not in line:
                        continue
                    k, v = line.split("=", 1)
                    k, v = k.strip(), v.strip().strip("'\"")
                    if k not in os.environ:
                        os.environ[k] = v
            break


class TokenBucketRateLimiter:
    """Thread-safe Token Bucket rate limiter for client-side pacing."""

    def __init__(self, rps: float, burst: float):
        self.rps = max(0.1, rps)
        self.burst = max(1.0, burst)
        self.tokens = self.burst
        self.last = time.time()
        self.lock = threading.Lock()

    def acquire(self) -> None:
        while True:
            with self.lock:
                now = time.time()
                elapsed = now - self.last
                self.tokens = min(self.burst, self.tokens + elapsed * self.rps)
                self.last = now
                if self.tokens >= 1.0:
                    self.tokens -= 1.0
                    return
                sleep_sec = (1.0 - self.tokens) / self.rps
            time.sleep(sleep_sec)


def retry_with_backoff(
    fn: Callable[[], Any],
    limiter: Optional[TokenBucketRateLimiter] = None,
    max_retries: int = 4,
    base_delay: float = 0.8,
) -> Any:
    """Runs fn with token-bucket pacing and exponential backoff on failure."""
    last_exc: Optional[BaseException] = None
    for attempt in range(max_retries):
        if limiter:
            limiter.acquire()
        try:
            return fn()
        except Exception as exc:  # noqa: BLE001 - tool boundary logs everything
            last_exc = exc
            if attempt < max_retries - 1:
                delay = base_delay * (2 ** attempt)
                logger.warning("R2 operation failed (attempt %d/%d): %s; retrying in %.1fs",
                               attempt + 1, max_retries, exc, delay)
                time.sleep(delay)
    raise RuntimeError(f"operation failed after {max_retries} retries: {last_exc}")


def host_has_avx2() -> Tuple[bool, str]:
    """
    Detects AVX2 support. Returns (supported, host_description).

    The guard targets ADR-011 D3: SAM annotation workloads are disqualified on
    the non-AVX Celeron ingress host and must stay off the live hot-set loop.
    """
    machine = platform.machine().lower()
    system = platform.system()
    host_desc = f"{system}/{machine}"
    if machine in ("x86_64", "amd64"):
        if system == "Linux":
            try:
                with open("/proc/cpuinfo", "r", encoding="utf-8") as f:
                    for line in f:
                        if line.startswith("flags") and " avx2 " in f" {line.split(':', 1)[1].strip()} ":
                            return True, host_desc
            except OSError:
                return False, host_desc
            return False, host_desc
        if system == "Darwin":
            try:
                out = subprocess.run(
                    ["sysctl", "-n", "machdep.cpu.features"],
                    capture_output=True, text=True, timeout=5, check=False,
                ).stdout
                return "AVX2" in out.upper(), host_desc
            except (OSError, subprocess.SubprocessError):
                return False, host_desc
        return False, host_desc
    # ARM and other architectures never expose AVX2; reported as absent.
    return False, host_desc


def enforce_host_guard(allow_unsafe: bool) -> bool:
    """
    Refuses to run when AVX2 is absent unless the operator passes
    --allow-unsafe-host (a deliberate, loudly-logged override for capable
    non-x86 development machines; production pre-labeling targets dev-box).
    Returns True when execution may proceed.
    """
    supported, host_desc = host_has_avx2()
    if supported:
        return True
    if allow_unsafe:
        logger.warning(
            "HOST GUARD OVERRIDE: host %s has no AVX2; continuing only because "
            "--allow-unsafe-host was passed explicitly. Production pre-labeling "
            "must run on the AVX2 dev-box (ADR-011 D3).", host_desc
        )
        return True
    logger.error(
        "HOST GUARD: refusing to run on %s without AVX2. SAM pre-labeling is "
        "authorized on the dev-box only; pass --allow-unsafe-host to override "
        "on a capable development machine.", host_desc
    )
    return False


def get_r2_client() -> Optional[Any]:
    """Initializes and returns boto3 S3 client for Cloudflare R2."""
    try:
        import boto3
        from botocore.config import Config
    except ImportError:
        logger.error("boto3 is required. Install via: pip install boto3")
        return None

    endpoint = os.getenv("R2_ENDPOINT", "")
    access_key = os.getenv("R2_ACCESS_KEY_ID", "")
    secret_key = os.getenv("R2_SECRET_ACCESS_KEY", "")
    if not endpoint or not access_key or not secret_key:
        logger.error("R2_ENDPOINT, R2_ACCESS_KEY_ID, and R2_SECRET_ACCESS_KEY must be set in .env")
        return None

    return boto3.client(
        service_name="s3",
        endpoint_url=endpoint,
        aws_access_key_id=access_key,
        aws_secret_access_key=secret_key,
        region_name="auto",
        config=Config(s3={"addressing_style": "path"}, max_pool_connections=20),
    )


def is_valid_jpeg(data: bytes, min_size: int = MIN_JPEG_SIZE) -> bool:
    """Verifies JPEG SOI magic bytes (0xFF, 0xD8) and minimum size constraint."""
    if len(data) < min_size:
        return False
    return data[0] == 0xFF and data[1] == 0xD8


def list_frames(
    s3_client: Any, bucket: str, date: str, limiter: TokenBucketRateLimiter
) -> List[Dict[str, Any]]:
    """
    Lists raw snapshot JPEGs under the date partition (both the harvester
    layout '<date>/' and the archiver layout 'proofs/<date>/'), mirroring the
    key parsing in tools/eval/batch_auto_label.py.
    """
    frames: List[Dict[str, Any]] = []
    paginator = s3_client.get_paginator("list_objects_v2")
    for prefix in [f"{date}/", f"proofs/{date}/"]:
        for page in paginator.paginate(Bucket=bucket, Prefix=prefix):
            for obj in page.get("Contents", []):
                key = obj["Key"]
                if not key.lower().endswith((".jpg", ".jpeg")):
                    continue
                if obj.get("Size", 0) < MIN_JPEG_SIZE:
                    continue
                parts = key.split("/")
                if len(parts) < 3:
                    continue
                cam_id = parts[1] if parts[0] != "proofs" else parts[2]
                name_no_ext = os.path.splitext(parts[-1])[0]
                file_parts = name_no_ext.split("_", 1)
                try:
                    ts = int(file_parts[0])
                except ValueError:
                    ts = 0
                frames.append(
                    {
                        "key": key,
                        "camera_id": cam_id,
                        "timestamp": ts,
                        "reason": file_parts[1] if len(file_parts) > 1 else "sample",
                        "size": obj.get("Size", 0),
                    }
                )
    frames.sort(key=lambda s: s.get("timestamp", 0), reverse=True)
    return frames


def existing_prelabel_keys(
    s3_client: Any, bucket: str, date: str, limiter: TokenBucketRateLimiter
) -> Set[Tuple[str, int]]:
    """
    Collects (camera_id, timestamp) pairs that already carry a sam_prelabel
    candidate so re-runs are idempotent.
    """
    seen: Set[Tuple[str, int]] = set()
    paginator = s3_client.get_paginator("list_objects_v2")
    # Candidate keys are '<camera>_sam_prelabel_<ts>.json'; the provenance
    # token itself contains underscores, so anchor the split on it.
    marker = f"_{seg_contract.SOURCE_SAM_PRELABEL}_"
    for page in paginator.paginate(Bucket=bucket, Prefix=f"labels/{date}/"):
        for obj in page.get("Contents", []):
            name = os.path.splitext(obj["Key"].split("/")[-1])[0]
            idx = name.find(marker)
            if idx <= 0:
                continue
            try:
                seen.add((name[:idx], int(name[idx + len(marker):])))
            except ValueError:
                continue
    return seen


def list_local_frames(root: Path, date: str) -> List[Dict[str, Any]]:
    """
    Lists raw snapshot JPEGs from a local storage root laid out exactly like
    the R2 archive: <root>/<date>/<camera_id>/<ts>_<reason>.jpg.

    This is the offline path for the pre-R2 workflow: frames harvested by the
    Go archiver with STORAGE_DRIVER=local (or auto_harvest.py --output-dir)
    can be pre-labeled immediately; pushing to R2 happens later via
    cmd/sync-proofs once infrastructure credentials are available.
    """
    frames: List[Dict[str, Any]] = []
    base = Path(root) / date
    if not base.is_dir():
        return frames
    for cam_dir in sorted(p for p in base.iterdir() if p.is_dir()):
        for f in sorted(list(cam_dir.glob("*.jpg")) + list(cam_dir.glob("*.jpeg"))):
            size = f.stat().st_size
            if size < MIN_JPEG_SIZE:
                continue
            file_parts = f.stem.split("_", 1)
            try:
                ts = int(file_parts[0])
            except ValueError:
                ts = 0
            try:
                rel_key = f.relative_to(root).as_posix()
            except ValueError:
                rel_key = f"{date}/{cam_dir.name}/{f.name}"
            frames.append(
                {
                    "key": rel_key,
                    "local_path": str(f),
                    "camera_id": cam_dir.name,
                    "timestamp": ts,
                    "reason": file_parts[1] if len(file_parts) > 1 else "sample",
                    "size": size,
                    "local": True,
                }
            )
    frames.sort(key=lambda s: s.get("timestamp", 0), reverse=True)
    return frames


def existing_local_prelabel_keys(mirror_dir: Path, date: str) -> Set[Tuple[str, int]]:
    """Local-mirror counterpart of existing_prelabel_keys (idempotent re-runs)."""
    seen: Set[Tuple[str, int]] = set()
    marker = f"_{seg_contract.SOURCE_SAM_PRELABEL}_"
    label_dir = Path(mirror_dir) / date
    if not label_dir.is_dir():
        return seen
    for f in label_dir.glob("*.json"):
        name = f.stem
        idx = name.find(marker)
        if idx <= 0:
            continue
        try:
            seen.add((name[:idx], int(name[idx + len(marker):])))
        except ValueError:
            continue
    return seen


def download_frame(
    s3_client: Any, bucket: str, key_or_path: str, limiter: TokenBucketRateLimiter,
    local: bool = False,
    local_path: Optional[str] = None,
) -> bytes:
    """
    Streams a snapshot JPEG from R2 (or reads it from the local filesystem in
    local-frames mode) with pacing and exponential backoff.
    """
    if local:
        target = local_path if local_path else key_or_path
        return Path(target).read_bytes()

    def _get() -> bytes:
        resp = s3_client.get_object(Bucket=bucket, Key=key_or_path)
        return resp["Body"].read()

    return retry_with_backoff(_get, limiter=limiter)


def upload_label(
    s3_client: Any, bucket: str, key: str, payload: Dict[str, Any],
    limiter: TokenBucketRateLimiter,
) -> bool:
    """Uploads a candidate label JSON to R2 with pacing and backoff."""
    body = json.dumps(payload, indent=2, ensure_ascii=False).encode("utf-8")

    def _put() -> None:
        s3_client.put_object(Bucket=bucket, Key=key, Body=body, ContentType="application/json")

    try:
        retry_with_backoff(_put, limiter=limiter)
        return True
    except Exception as exc:  # noqa: BLE001
        logger.error("Failed to upload label %s: %s", key, exc)
        return False


def decode_image(frame_bytes: bytes) -> Optional[Any]:
    """Decodes JPEG bytes to an RGB array via Pillow (no OpenCV dependency)."""
    try:
        from PIL import Image
        import io

        img = Image.open(io.BytesIO(frame_bytes)).convert("RGB")
        return img
    except Exception as exc:  # noqa: BLE001
        logger.warning("Failed to decode frame JPEG: %s", exc)
        return None


def calculate_traffic_level(counts: Dict[str, int]) -> str:
    """Maps vehicle counts to a coarse traffic level (batch_auto_label parity)."""
    pcu = sum(counts.get(cls, 0) * weight for cls, weight in PCU_WEIGHTS.items())
    if pcu >= 35.0:
        return "jam"
    if pcu >= 22.0:
        return "heavy"
    if pcu >= 10.0:
        return "moderate"
    return "clear"


def determine_time_of_day(ts: int) -> str:
    """Maps an ICT unix timestamp to day, night, or dawn_dusk (auto_harvest parity)."""
    loc_tz = timezone(timedelta(hours=7))
    dt = datetime.fromtimestamp(ts, tz=loc_tz)
    mins = dt.hour * 60 + dt.minute
    if 330 <= mins < 390 or 1050 <= mins < 1110:
        return "dawn_dusk"
    if 390 <= mins < 1050:
        return "day"
    return "night"


def propose_roadway_polygon(
    sam: "sam_runtime.SamOnnxModel",
    embeddings: np.ndarray,
    scale: float,
    pads: Tuple[int, int],
    width: int,
    height: int,
    horizon_frac: float,
    max_vertices: int,
) -> Optional[Tuple[List[List[float]], float]]:
    """
    Proposes the roadway polygon: largest stable central region below the
    horizon. Primary prompt is a wide box under the horizon; when the result
    is degenerate, falls back to a central point-grid consensus union.

    Returns (normalized polygon, mask area fraction) or None.
    """
    y_top = min(max(horizon_frac + 0.05, 0.2), 0.8)
    box = (0.08, y_top, 0.92, 0.97)

    def usable(mask: np.ndarray) -> bool:
        frac = geom.mask_area_frac(mask)
        return ROADWAY_AREA_FRAC_RANGE[0] <= frac <= ROADWAY_AREA_FRAC_RANGE[1]

    best_mask: Optional[np.ndarray] = None
    best_score = 0.0
    mask, score = sam.segment_box(embeddings, box, scale, pads, height, width)
    if usable(mask):
        best_mask, best_score = mask, score

    if best_mask is None:
        # Point-grid consensus: keep masks overlapping (IoU >= STABILITY_IOU)
        # with the largest-area candidate, then union the stable set.
        masks: List[Tuple[np.ndarray, float]] = []
        for pt in ROADWAY_POINT_GRID:
            pt_y = min(max(horizon_frac + (1.0 - horizon_frac) * (pt[1] - 0.5) * 2.0, 0.15), 0.98)
            m, s = sam.segment_points(embeddings, [(pt[0], pt_y)], scale, pads, height, width)
            if geom.mask_area_frac(m) > 0.02:
                masks.append((m, s))
        if not masks:
            return None
        reference = max(masks, key=lambda ms: np.count_nonzero(ms[0]))[0]
        stable = [m for m, _ in masks if geom.mask_iou(m, reference) >= STABILITY_IOU]
        union = stable[0]
        for m in stable[1:]:
            union = union | m
        union = geom.largest_component(union)
        if usable(union):
            best_mask, best_score = union, float(np.mean([s for _, s in masks]))

    if best_mask is None:
        return None
    best_mask = geom.largest_component(best_mask)
    polygon = geom.mask_to_polygon(best_mask, max_vertices=max_vertices)
    if len(polygon) < seg_contract.MIN_POLYGON_POINTS:
        return None
    normalized = geom.normalize_polygon(polygon, width, height)
    return normalized, geom.mask_area_frac(best_mask)


def build_candidate(
    frame: Dict[str, Any],
    frame_bytes: bytes,
    sam: "sam_runtime.SamOnnxModel",
    detector: "sam_runtime.YoloOnnxModel",
    date: str,
    horizon_frac: float,
    max_vertices: int,
    conf: float,
    max_vehicle_polygons: int,
) -> Tuple[Optional[Dict[str, Any]], Dict[str, Any]]:
    """
    Runs the full per-frame pipeline and composes a LabelPayload candidate.

    Returns (label_payload_or_None, telemetry dict).
    """
    telemetry: Dict[str, Any] = {"key": frame["key"], "status": "ok", "annotations": 0}
    img = decode_image(frame_bytes)
    if img is None:
        telemetry["status"] = "decode_failed"
        return None, telemetry
    width, height = img.size
    if width < 64 or height < 64:
        telemetry["status"] = "image_too_small"
        return None, telemetry
    arr = np.asarray(img, dtype=np.uint8)

    t0 = time.perf_counter()
    detections = detector.detect(arr, conf=conf)
    t1 = time.perf_counter()

    canvas, scale, pads = sam.preprocess_frame(arr)
    embeddings = sam.encode(canvas)
    t2 = time.perf_counter()

    annotations: List[Dict[str, Any]] = []
    counts: Dict[str, int] = {}

    # 1. Roadway: largest stable central region below the horizon.
    roadway = propose_roadway_polygon(
        sam, embeddings, scale, pads, width, height, horizon_frac, max_vertices
    )
    if roadway is not None:
        polygon, _ = roadway
        annotations.append(
            {
                "class": "roadway",
                "polygon": polygon,
                "source": seg_contract.SOURCE_SAM_PRELABEL,
            }
        )
    else:
        telemetry["roadway"] = "rejected"

    # 2. Vehicles: detector boxes refined into polygons via SAM box prompts.
    for det in sorted(detections, key=lambda d: -d["score"])[:max_vehicle_polygons]:
        mask, _ = sam.segment_box(
            embeddings, det["box_norm"], scale, pads, height, width
        )
        frac = geom.mask_area_frac(mask)
        if not (VEHICLE_MIN_MASK_FRAC <= frac <= VEHICLE_MAX_MASK_FRAC):
            continue
        polygon = geom.mask_to_polygon(mask, max_vertices=max_vertices)
        if len(polygon) < seg_contract.MIN_POLYGON_POINTS:
            continue
        annotations.append(
            {
                "class": det["class"],
                "polygon": geom.normalize_polygon(polygon, width, height),
                "source": seg_contract.SOURCE_SAM_PRELABEL,
            }
        )
        counts[det["class"]] = counts.get(det["class"], 0) + 1
        if len(annotations) >= seg_contract.MAX_ANNOTATIONS_PER_LABEL:
            break

    # 3. Water puddles: proposal-only candidates needing human verification.
    puddle_boxes = geom.propose_puddle_boxes(arr, horizon_frac, max_boxes=2)
    for box_norm in puddle_boxes:
        if len(annotations) >= seg_contract.MAX_ANNOTATIONS_PER_LABEL:
            break
        mask, _ = sam.segment_box(embeddings, box_norm, scale, pads, height, width)
        frac = geom.mask_area_frac(mask)
        if not (VEHICLE_MIN_MASK_FRAC <= frac <= VEHICLE_MAX_MASK_FRAC):
            continue
        polygon = geom.mask_to_polygon(mask, max_vertices=max_vertices)
        if len(polygon) < seg_contract.MIN_POLYGON_POINTS:
            continue
        annotations.append(
            {
                "class": "water_puddle",
                "polygon": geom.normalize_polygon(polygon, width, height),
                "source": seg_contract.SOURCE_SAM_PRELABEL,
            }
        )

    t3 = time.perf_counter()
    telemetry["detector_ms"] = round((t1 - t0) * 1000, 1)
    telemetry["encoder_ms"] = round((t2 - t1) * 1000, 1)
    telemetry["decoder_ms"] = round((t3 - t2) * 1000, 1)
    telemetry["annotations"] = len(annotations)

    annotations = seg_contract.canonicalize_annotations(annotations)

    ts = frame.get("timestamp") or int(time.time())
    dt = datetime.fromtimestamp(ts, tz=timezone(timedelta(hours=7)))
    cam_id = frame["camera_id"]
    payload = {
        "date": date,
        "camera_id": cam_id,
        "snapshot_key": frame["key"],
        "annotator": seg_contract.SOURCE_SAM_PRELABEL,
        "flood_level": "none",
        "traffic_level": calculate_traffic_level(counts),
        "queue_reason": "free_flow",
        "flood_submersion_anchor": "unknown",
        "weather": "dry",
        "time_of_day": determine_time_of_day(ts),
        "quality_tags": [],
        "annotations": annotations,
        "notes": (
            "SAM prelabel (proposal-only): roadway/vehicle polygons from "
            "MobileSAM + yolov8n; water_puddle polygons are unverified "
            "proposals requiring human review (ADR-009 4.3)."
        ),
        "timestamp": ts,
        "annotated_at": dt.isoformat(),
    }
    errors = seg_contract.validate_label_payload(payload)
    if errors:
        telemetry["status"] = "contract_invalid"
        telemetry["errors"] = errors
        return None, telemetry
    return payload, telemetry


def upload_key_for(frame: Dict[str, Any], date: str, annotator: str) -> str:
    """Builds the R2 label object key (batch_auto_label naming convention)."""
    clean_cam = os.path.basename(str(frame["camera_id"]).strip()).replace("/", "_")
    clean_annotator = os.path.basename(annotator.strip()).replace("/", "_")
    ts = frame.get("timestamp") or int(time.time())
    return f"labels/{date}/{clean_cam}_{clean_annotator}_{ts}.json"


def parse_args(args: Optional[List[str]] = None) -> argparse.Namespace:
    """Parses command line arguments."""
    parser = argparse.ArgumentParser(
        description="SAM-Assisted Polygon Pre-Labeling Batch Toolchain (S9-T2 / Issue #298)."
    )
    parser.add_argument("--date", type=str, default=time.strftime("%Y-%m-%d"),
                        help="Date partition to stream frames from (YYYY-MM-DD)")
    parser.add_argument("--local-frames", type=Path, default=None,
                        help="Read frames from a local storage root (<root>/<date>/<cam>/*.jpg) "
                             "instead of R2; labels mirror locally (upload deferred unless --upload-r2)")
    parser.add_argument("--limit", type=int, default=None,
                        help="Maximum frames to process (default: all)")
    parser.add_argument("--dry-run", action="store_true",
                        help="List the execution plan without network inference or writes")
    parser.add_argument("--upload-r2", action="store_true",
                        help="Upload candidate labels to Cloudflare R2 (default: local mirror only)")
    parser.add_argument("--rps", type=float, default=4.0,
                        help="Token bucket rate limit for R2 requests (req/sec, default 4)")
    parser.add_argument("--concurrency", type=int, default=2,
                        help="Inference worker threads (default: 2; CPU-bound)")
    parser.add_argument("--horizon-frac", type=float, default=0.4,
                        help="Estimated horizon as a fraction of frame height (default 0.4)")
    parser.add_argument("--max-vertices", type=int, default=48,
                        help="Maximum polygon vertices per annotation (default 48, contract max 64)")
    parser.add_argument("--conf", type=float, default=0.25,
                        help="Detector confidence threshold (default 0.25)")
    parser.add_argument("--max-vehicle-polygons", type=int, default=12,
                        help="Maximum vehicle polygons per frame (default 12)")
    parser.add_argument("--allow-unsafe-host", action="store_true",
                        help="Override the AVX2 host guard on capable non-x86 dev machines")
    parser.add_argument("--encoder-path", type=str, default=None,
                        help="SAM image encoder ONNX path (default: $%s)" % sam_runtime.ENV_ENCODER)
    parser.add_argument("--decoder-path", type=str, default=None,
                        help="SAM mask decoder ONNX path (default: $%s)" % sam_runtime.ENV_DECODER)
    parser.add_argument("--detector-path", type=str, default=None,
                        help="YOLO detection ONNX path (default: $%s)" % sam_runtime.ENV_DETECTOR)
    parser.add_argument("--output-dir", type=Path, default=REPO_ROOT / "data" / "seg_prelabels",
                        help="Local mirror directory for candidate labels (default data/seg_prelabels)")
    return parser.parse_args(args)


def run_pipeline(args: argparse.Namespace) -> int:
    """Executes the pre-labeling pipeline according to parsed CLI args."""
    logging.basicConfig(
        level=logging.INFO,
        format="%(asctime)s [%(levelname)s] %(message)s",
        datefmt="%Y-%m-%d %H:%M:%S",
    )
    load_dotenv()

    print("=" * 80)
    print("PROJECTKV SAM PRE-LABELING TOOLCHAIN (S9-T2 / Issue #298)")
    print("=" * 80)

    if not enforce_host_guard(args.allow_unsafe_host):
        return 1

    encoder_path = sam_runtime.resolve_weight_path(sam_runtime.ENV_ENCODER, args.encoder_path)
    decoder_path = sam_runtime.resolve_weight_path(sam_runtime.ENV_DECODER, args.decoder_path)
    detector_path = sam_runtime.resolve_weight_path(sam_runtime.ENV_DETECTOR, args.detector_path)
    if args.dry_run:
        print("Dry Run:             True (no inference, no writes)")
        print(f"Date Partition:      {args.date}")
        print(f"Limit:               {args.limit if args.limit is not None else 'none'}")
        print(f"Upload to R2:        {args.upload_r2}")
        print(f"Weights configured:  encoder={bool(encoder_path)} decoder={bool(decoder_path)} "
              f"detector={bool(detector_path)}")
        print(f"Host AVX2:           {host_has_avx2()[0]}")
        print("-" * 80)
        print("[DRY RUN] Completed without network I/O, inference, or file writes.")
        return 0

    if not encoder_path or not decoder_path:
        logger.error("Missing %s / %s in environment (Rule 8). See "
                     "docs/runbooks/sam_prelabeling.md for provisioning.",
                     sam_runtime.ENV_ENCODER, sam_runtime.ENV_DECODER)
        return 1
    if not detector_path:
        logger.error("Missing %s in environment (Rule 8).", sam_runtime.ENV_DETECTOR)
        return 1
    for path, name in [(encoder_path, "encoder"), (decoder_path, "decoder"), (detector_path, "detector")]:
        if not Path(path).is_file():
            logger.error("Weight file for %s does not exist: %s", name, path)
            return 1

    s3_client = None
    bucket = ""
    # R2 access is required for listing (R2 mode) and for uploading; the pure
    # local-frames path runs fully offline.
    if args.upload_r2 or args.local_frames is None:
        s3_client = get_r2_client()
        if s3_client is None:
            return 1
        bucket = os.getenv("R2_BUCKET_NAME", "")
        if not bucket:
            logger.error("R2_BUCKET_NAME must be set in .env (Rule 8: no hardcoded bucket fallback)")
            return 1

    limiter = TokenBucketRateLimiter(rps=args.rps, burst=max(4.0, args.rps))
    sam = sam_runtime.SamOnnxModel(encoder_path, decoder_path)
    detector = sam_runtime.YoloOnnxModel(detector_path)
    logger.info("Loaded SAM encoder=%s decoder=%s detector=%s",
                os.path.basename(encoder_path), os.path.basename(decoder_path),
                os.path.basename(detector_path))

    if args.local_frames is not None:
        frames = list_local_frames(args.local_frames, args.date)
        already = existing_local_prelabel_keys(args.output_dir, args.date)
        source_desc = f"local root {args.local_frames}"
    else:
        frames = list_frames(s3_client, bucket, args.date, limiter)
        already = existing_prelabel_keys(s3_client, bucket, args.date, limiter)
        source_desc = f"R2 bucket {bucket}"
    pending = [f for f in frames if (f["camera_id"], f["timestamp"]) not in already]
    if args.limit is not None and args.limit > 0:
        pending = pending[: args.limit]

    print(f"Frame source:         {source_desc}")
    print(f"Date Partition:      {args.date}")
    print(f"Frames listed:       {len(frames)}")
    print(f"Already pre-labeled: {len(already)} (idempotent skip)")
    print(f"Pending frames:      {len(pending)}")
    print(f"Upload to R2:        {args.upload_r2} (bucket={bucket})")
    print(f"Local mirror:        {args.output_dir}")
    print("-" * 80)

    if not pending:
        print("[INFO] Nothing to pre-label. Exiting.")
        return 0

    stats = {"success": 0, "failed": 0, "skipped": 0}
    infer_ms_total = [0.0]
    lock = threading.Lock()
    t_start = time.time()

    def process(frame: Dict[str, Any]) -> str:
        try:
            frame_bytes = download_frame(
                s3_client, bucket, frame["key"], limiter,
                local=frame.get("local", False),
                local_path=frame.get("local_path"),
            )
            if not is_valid_jpeg(frame_bytes):
                logger.warning("Skip invalid JPEG: %s", frame["key"])
                return "skipped"
            payload, telemetry = build_candidate(
                frame, frame_bytes, sam, detector, args.date,
                horizon_frac=args.horizon_frac,
                max_vertices=args.max_vertices,
                conf=args.conf,
                max_vehicle_polygons=args.max_vehicle_polygons,
            )
            if payload is None:
                logger.warning("Skip %s: status=%s errors=%s",
                               frame["key"], telemetry.get("status"), telemetry.get("errors"))
                return "skipped"

            key = upload_key_for(frame, args.date, seg_contract.SOURCE_SAM_PRELABEL)
            out_dir = args.output_dir / args.date
            out_dir.mkdir(parents=True, exist_ok=True)
            with open(out_dir / os.path.basename(key), "w", encoding="utf-8") as f:
                json.dump(payload, f, indent=2, ensure_ascii=False)

            if args.upload_r2:
                if not upload_label(s3_client, bucket, key, payload, limiter):
                    return "failed"
            logger.info(
                "Pre-labeled %s (%d annotations, detector=%sms encoder=%sms decoder=%sms)",
                frame["key"], telemetry["annotations"], telemetry.get("detector_ms", 0),
                telemetry.get("encoder_ms", 0), telemetry.get("decoder_ms", 0),
            )
            with lock:
                infer_ms_total[0] += (
                    telemetry.get("detector_ms", 0)
                    + telemetry.get("encoder_ms", 0)
                    + telemetry.get("decoder_ms", 0)
                )
            return "success"
        except Exception as exc:  # noqa: BLE001
            logger.error("Frame %s failed: %s", frame.get("key"), exc)
            return "failed"

    with concurrent.futures.ThreadPoolExecutor(max_workers=args.concurrency) as executor:
        for i, outcome in enumerate(executor.map(process, pending), 1):
            stats[outcome] += 1
            if i % 25 == 0 or i == len(pending):
                elapsed = max(0.001, time.time() - t_start)
                logger.info("Progress: %d/%d (%.1f frames/sec)", i, len(pending), i / elapsed)

    elapsed = time.time() - t_start
    with lock:
        avg_infer_ms = infer_ms_total[0] / max(1, stats["success"])

    print("=" * 80)
    print("PRE-LABELING SUMMARY")
    print("=" * 80)
    print(f"Total frames:        {len(pending)}")
    print(f"Pre-labeled:         {stats['success']}")
    print(f"Skipped:             {stats['skipped']}")
    print(f"Failed:              {stats['failed']}")
    print(f"Wall time:           {elapsed:.1f}s ({len(pending) / max(0.001, elapsed):.2f} frames/sec)")
    print(f"Avg inference/frame: {avg_infer_ms:.0f} ms")
    print(f"Upload to R2:        {args.upload_r2}")
    print("=" * 80)

    return 0 if stats["failed"] == 0 else 1


def main() -> None:
    args = parse_args()
    sys.exit(run_pipeline(args))


if __name__ == "__main__":
    main()
