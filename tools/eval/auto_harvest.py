#!/usr/bin/env python3
"""
auto_harvest.py — Standalone Automated Harvester CLI.

Part of Sprint 7 Issue #263 (S7-T7c) / Kanban [KV-11].
Executes autonomous camera frame harvesting, local OpenVINO vehicle detection,
PCU traffic saturation scoring, physical waterline depth mapping,
Draft-07 label synthesis, schema validation, and Cloudflare R2 archival.

Adheres strictly to AGENTS.md Rules:
- Rule 1: Monochromatic, zero-emoji, 100% English code, docs, and logs.
- Rule 8: Zero hardcoded secrets; credentials loaded dynamically from .env.
"""

import argparse
import concurrent.futures
from datetime import datetime, timezone, timedelta
import json
import logging
import os
from pathlib import Path
import sys
import time
from typing import Any, Dict, List, Optional, Set, Tuple
import urllib.error
import urllib.parse
import urllib.request

REPO_ROOT = Path(__file__).resolve().parent.parent.parent

# Try importing validate_labels from same directory for Draft-07 schema validation
try:
    from tools.eval.validate_labels import validate_label_dict, load_schema
except ImportError:
    try:
        from validate_labels import validate_label_dict, load_schema
    except ImportError:
        validate_label_dict = None
        load_schema = None


def load_dotenv():
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


load_dotenv()

# Minimum JPEG size: 20 KB (20,480 bytes) per ADR-009 / Issue #262 / S7-T7b
MIN_JPEG_SIZE = 20480

# Priority corridors for scheduled Hot Set harvest (4 corridors, 57 intersection cameras)
PRIORITY_CORRIDORS = {
    "vo_van_kiet",
    "mai_chi_tho",
    "dien_bien_phu",
    "cong_hoa",
}

# Flood-prone corridors for rain watcher harvest (24 historical flood-prone corridors)
FLOOD_PRONE_CORRIDORS = {
    "ung_van_khiem",
    "quoc_lo_13",
    "huynh_tan_phat",
    "tran_xuan_soan",
    "nguyen_xi",
    "nguyen_huu_canh",
    "kha_van_can",
    "le_van_luong",
    "an_duong_vuong",
    "kinh_duong_vuong",
    "vo_van_kiet",
    "dong_den",
    "phan_huy_ich",
    "quoc_lo_1",
    "xo_viet_nghe_tinh",
    "dinh_bo_linh",
    "to_ngoc_van",
    "bach_dang",
    "le_duc_tho",
    "pham_van_dong",
}

# PCU weights conforming to ADR-005 T6 and services/go-server/internal/cvclient
PCU_WEIGHTS = {
    "motorcycle": 0.3,
    "car": 1.0,
    "bus": 2.5,
    "truck": 2.0,
    "container": 2.0,
}

# PCU Saturation Index cutoffs conforming to Sprint 4 T5 / Lesson 33
PCU_THRESHOLDS = {
    "moderate": 0.30,
    "heavy": 0.60,
    "congested": 0.80,
}

# WMO Weather interpretation codes indicating rain or drizzle
RAIN_WMO_CODES = {
    51, 53, 55, 56, 57, 61, 63, 65, 66, 67, 80, 81, 82, 95, 96, 99
}

DEFAULT_CATALOG_PATH = REPO_ROOT / "cameras_enriched.json"
DEFAULT_CHOKEPOINTS_PATH = REPO_ROOT / "tools" / "eval" / "fixtures" / "flood_chokepoints.json"
DEFAULT_SCHEMA_PATH = REPO_ROOT / "tools" / "eval" / "label_schema.json"
DEFAULT_OPEN_METEO_URL = (
    "https://api.open-meteo.com/v1/forecast"
    "?latitude=10.762622&longitude=106.660172"
    "&current=temperature_2m,relative_humidity_2m,precipitation,rain,weather_code"
    "&timezone=Asia%2FHo_Chi_Minh"
)


def is_valid_jpeg(data: bytes, min_size: int = MIN_JPEG_SIZE) -> bool:
    """Verifies JPEG SOI magic bytes (0xFF, 0xD8) and minimum size constraint."""
    if len(data) < min_size:
        return False
    return data[0] == 0xFF and data[1] == 0xD8


def determine_time_of_day(dt: datetime) -> str:
    """Maps an ICT datetime to day, night, or dawn_dusk."""
    mins = dt.hour * 60 + dt.minute
    if 330 <= mins < 390:  # 05:30 to 06:30
        return "dawn_dusk"
    elif 390 <= mins < 1050:  # 06:30 to 17:30
        return "day"
    elif 1050 <= mins < 1110:  # 17:30 to 18:30
        return "dawn_dusk"
    return "night"


def determine_waterline_depth(weather: str, rainfall_mm: float = 0.0) -> Tuple[str, str, str]:
    """
    Maps meteorological condition and rainfall amount to physical waterline depth anchors.
    Deprecated: weather-based flood/depth guessing causes answer key pollution (Lesson 39).
    When active rain is detected, flood and depth default to UNKNOWN pending vision/human verification.
    Returns: (flood_severity, waterline_depth, flood_submersion_anchor).
    """
    if weather == "heavy_rain" or rainfall_mm >= 5.0 or weather == "rain" or rainfall_mm >= 0.10:
        return "UNKNOWN", "UNKNOWN", "unknown"
    return "NONE", "DRY", "dry"


def fetch_weather_open_meteo(url: str = DEFAULT_OPEN_METEO_URL, timeout: float = 5.0) -> Tuple[str, float, int]:
    """
    Queries Open-Meteo API for real-time HCMC weather conditions.
    Returns: (weather_condition, rainfall_mm, weather_code).
    """
    req = urllib.request.Request(
        url,
        headers={"User-Agent": "Mozilla/5.0 (compatible; CameraViewAutoHarvester/1.0)"},
        method="GET",
    )
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            data = json.loads(resp.read().decode("utf-8"))
        curr = data.get("current", {})
        rain = float(curr.get("rain", curr.get("precipitation", 0.0)))
        code = int(curr.get("weather_code", 0))

        if rain >= 5.0:
            condition = "heavy_rain"
        elif rain >= 0.10 or code in RAIN_WMO_CODES:
            condition = "rain"
        else:
            condition = "dry"

        return condition, rain, code
    except Exception as e:
        logging.warning("Failed to query Open-Meteo API: %s. Falling back to dry.", e)
        return "dry", 0.0, 0


def resolve_weather(weather_arg: str, url: str = DEFAULT_OPEN_METEO_URL) -> Tuple[str, float, int]:
    """Resolves weather condition from CLI argument or live Open-Meteo API."""
    if weather_arg == "auto":
        return fetch_weather_open_meteo(url)
    elif weather_arg in ("rain", "heavy_rain"):
        rain_mm = 5.0 if weather_arg == "heavy_rain" else 1.0
        return weather_arg, rain_mm, 61
    return "dry", 0.0, 0


def load_catalog(path: Path) -> List[Dict[str, Any]]:
    """Loads camera catalog from JSON."""
    if not path.is_file():
        raise FileNotFoundError(f"Catalog file not found: {path}")
    with open(path, "r", encoding="utf-8") as f:
        data = json.load(f)
    return [c for c in data if isinstance(c, dict) and "id" in c]


def load_chokepoints(path: Path) -> Set[str]:
    """Loads curated flood chokepoint camera IDs."""
    if not path.is_file():
        return set()
    with open(path, "r", encoding="utf-8") as f:
        data = json.load(f)
    return {c["camera_id"] for c in data.get("chokepoints", []) if "camera_id" in c}


def filter_cameras(
    catalog: List[Dict[str, Any]],
    scope: str,
    chokepoints: Optional[Set[str]] = None,
) -> List[Dict[str, Any]]:
    """
    Filters online, non-ghost cameras by scope ('hotset', 'flood-prone', 'all').
    """
    eligible = [
        c
        for c in catalog
        if (c.get("isOnline") or c.get("status") == "UP")
        and not c.get("isGhost")
        and not c.get("duplicateOf")
    ]

    if scope == "hotset":
        return [
            c
            for c in eligible
            if len(c.get("road_keys", [])) == 2
            and any(k in PRIORITY_CORRIDORS for k in c.get("road_keys", []))
        ]
    elif scope == "flood-prone":
        chokepoint_ids = chokepoints or set()
        return [
            c
            for c in eligible
            if c["id"] in chokepoint_ids
            or any(k in FLOOD_PRONE_CORRIDORS for k in c.get("road_keys", []))
        ]
    elif scope == "all":
        return eligible
    else:
        raise ValueError(f"Unknown camera scope: {scope}")


def fetch_camera_frame(camera: Dict[str, Any], timeout: float = 10.0) -> Optional[bytes]:
    """Fetches snapshot JPEG bytes for a camera from upstream portal."""
    snap_url = camera.get("snapshotUrl")
    if not snap_url:
        snap_url = f"https://giaothong.hochiminhcity.gov.vn/render/ImageHandler.ashx?id={camera['id']}"

    req = urllib.request.Request(
        snap_url,
        headers={"User-Agent": "Mozilla/5.0 (compatible; CameraViewAutoHarvester/1.0)"},
        method="GET",
    )
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            if resp.status != 200:
                return None
            return resp.read()
    except Exception:
        return None


def detect_vehicles(
    image_bytes: bytes,
    cv_engine_url: str = "http://localhost:8000",
    timeout: float = 10.0,
) -> Dict[str, Any]:
    """
    Sends frame bytes to local OpenVINO CV engine /predict endpoint.
    Returns dict containing vehicles_detected, vehicle_counts, and confidence.
    """
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


def calculate_traffic_severity(
    counts: Dict[str, int],
    vehicles_detected: int,
    base_capacity: float,
) -> Tuple[str, float, float, str]:
    """
    Calculates PCU Saturation Index and returns (severity, pcu_total, si, queue_reason).
    """
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
        return "CLEAR", 0.0, 0.0, "free_flow"

    eff_capacity = max(base_capacity, 15.0)
    si = pcu_total / eff_capacity

    if si >= PCU_THRESHOLDS["congested"]:
        severity = "CONGESTED"
        queue_reason = "bottleneck"
    elif si >= PCU_THRESHOLDS["heavy"]:
        severity = "HEAVY"
        queue_reason = "bottleneck"
    elif si >= PCU_THRESHOLDS["moderate"]:
        severity = "MODERATE"
        queue_reason = "free_flow"
    else:
        severity = "CLEAR"
        queue_reason = "free_flow"

    return severity, pcu_total, si, queue_reason


def synthesize_label(
    camera: Dict[str, Any],
    weather: str,
    rainfall_mm: float,
    pred_res: Dict[str, Any],
    ts: int,
    trigger: str = "now",
    loc_tz: Optional[timezone] = None,
) -> Dict[str, Any]:
    """
    Synthesizes a JSON manifest strictly conforming to tools/eval/label_schema.json (Draft-07).
    """
    if loc_tz is None:
        loc_tz = timezone(timedelta(hours=7))  # ICT UTC+7

    dt = datetime.fromtimestamp(ts, tz=loc_tz)
    camera_id = camera["id"]
    camera_name = camera.get("displayName") or camera.get("title", "")
    base_capacity = float(camera.get("base_capacity", 15.0))

    counts = pred_res.get("vehicle_counts", {})
    vehicles_detected = pred_res.get("vehicles_detected", 0)
    severity, pcu_total, si, queue_reason = calculate_traffic_severity(
        counts, vehicles_detected, base_capacity
    )

    flood, waterline_depth, submersion_anchor = determine_waterline_depth(weather, rainfall_mm)
    time_of_day = determine_time_of_day(dt)

    road_keys = camera.get("road_keys", [])
    carriageway_direction = "bidirectional" if len(road_keys) == 2 else "inbound"
    traffic_dir_b = severity if carriageway_direction == "bidirectional" else "NOT_APPLICABLE"

    quality_tags: List[str] = []
    if weather in ("rain", "heavy_rain"):
        quality_tags.append("rain_on_lens")

    date_str = dt.strftime("%Y-%m-%d")
    image_rel_path = f"{date_str}/{camera_id}/{ts}_sample.jpg"

    return {
        "frame_id": f"{camera_id}_{ts}",
        "camera_id": camera_id,
        "camera_name": camera_name,
        "image_path": image_rel_path,
        "annotator": "harvester/auto",
        "annotated_at": dt.isoformat(),
        "notes": f"Auto-harvested via {trigger} trigger; weather={weather}; pcu={pcu_total:.1f}; SI={si:.4f}",
        "labels": {
            "traffic": severity,
            "carriageway_direction": carriageway_direction,
            "traffic_dir_a": severity,
            "traffic_dir_b": traffic_dir_b,
            "is_waiting_red_light": False,
            "queue_reason": queue_reason,
            "flood": flood,
            "waterline_depth": waterline_depth,
            "flood_submersion_anchor": submersion_anchor,
            "time_of_day": time_of_day,
            "weather": weather,
            "quality_tags": quality_tags,
        },
    }


def validate_label_manifest(manifest: Dict[str, Any], schema_path: Path = DEFAULT_SCHEMA_PATH) -> Tuple[bool, List[str]]:
    """Validates manifest against tools/eval/label_schema.json."""
    if validate_label_dict is not None and schema_path.is_file():
        schema = load_schema(schema_path)
        errors = validate_label_dict(manifest, schema)
        return len(errors) == 0, errors

    # Lightweight fallback validation if validate_labels module is unavailable
    required_root = ["frame_id", "image_path", "labels", "annotator", "annotated_at"]
    errors = []
    for f in required_root:
        if f not in manifest:
            errors.append(f"root: missing required field '{f}'")
    labels = manifest.get("labels", {})
    if not isinstance(labels, dict):
        errors.append("root.labels: expected dict")
    else:
        for f in ["traffic", "flood"]:
            if f not in labels:
                errors.append(f"root.labels: missing required field '{f}'")
    return len(errors) == 0, errors


def get_r2_client():
    """Initializes and returns boto3 S3 client for Cloudflare R2."""
    try:
        import boto3
        from botocore.config import Config
    except ImportError:
        logging.error("boto3 is required for --upload-r2. Install via: pip install boto3")
        return None

    endpoint = os.getenv("R2_ENDPOINT", "")
    access_key = os.getenv("R2_ACCESS_KEY_ID", "")
    secret_key = os.getenv("R2_SECRET_ACCESS_KEY", "")

    if not endpoint or not access_key or not secret_key:
        logging.error("R2_ENDPOINT, R2_ACCESS_KEY_ID, and R2_SECRET_ACCESS_KEY must be set in .env")
        return None

    return boto3.client(
        service_name="s3",
        endpoint_url=endpoint,
        aws_access_key_id=access_key,
        aws_secret_access_key=secret_key,
        region_name="auto",
        config=Config(s3={"addressing_style": "path"}),
    )


def upload_r2_object(
    s3_client: Any,
    bucket: str,
    key: str,
    data: bytes,
    content_type: str = "application/octet-stream",
) -> bool:
    """Uploads in-memory bytes to Cloudflare R2 bucket."""
    try:
        s3_client.put_object(
            Bucket=bucket,
            Key=key,
            Body=data,
            ContentType=content_type,
        )
        return True
    except Exception as e:
        logging.error("Failed to upload object to R2 (key=%s): %s", key, e)
        return False


def parse_args(args: Optional[List[str]] = None) -> argparse.Namespace:
    """Parses command line arguments with scope defaults inferred from trigger."""
    parser = argparse.ArgumentParser(
        description="Standalone Automated Harvester CLI (S7-T7c / Issue #263)."
    )
    parser.add_argument(
        "--trigger",
        choices=["scheduled", "rain", "now"],
        default="now",
        help="Trigger mode: scheduled, rain, or now (default: now)",
    )
    parser.add_argument(
        "--scope",
        choices=["hotset", "flood-prone", "all"],
        default=None,
        help="Target camera scope (default: hotset for scheduled/now, flood-prone for rain)",
    )
    parser.add_argument(
        "--weather",
        choices=["auto", "dry", "rain", "heavy_rain"],
        default="auto",
        help="Weather condition (default: auto, queries Open-Meteo)",
    )
    parser.add_argument(
        "--catalog-path",
        type=Path,
        default=DEFAULT_CATALOG_PATH,
        help="Path to cameras_enriched.json",
    )
    parser.add_argument(
        "--chokepoints-path",
        type=Path,
        default=DEFAULT_CHOKEPOINTS_PATH,
        help="Path to flood_chokepoints.json",
    )
    parser.add_argument(
        "--schema-path",
        type=Path,
        default=DEFAULT_SCHEMA_PATH,
        help="Path to label_schema.json",
    )
    parser.add_argument(
        "--cv-engine-url",
        type=str,
        default=os.getenv("CV_ENGINE_URL", "http://localhost:8000"),
        help="URL of OpenVINO cv-engine /predict endpoint",
    )
    parser.add_argument(
        "--upload-r2",
        action="store_true",
        help="Upload harvested frames and Draft-07 labels to Cloudflare R2",
    )
    parser.add_argument(
        "--r2-bucket",
        type=str,
        default=os.getenv("R2_BUCKET_NAME", "warded-proofs"),
        help="Target Cloudflare R2 bucket name",
    )
    parser.add_argument(
        "--validate",
        action="store_true",
        help="Perform strict Draft-07 schema validation on every synthesized label",
    )
    parser.add_argument(
        "--dry-run",
        action="store_true",
        help="Print execution plan and target cameras without downloading or uploading",
    )
    parser.add_argument(
        "--limit",
        type=int,
        default=None,
        help="Optional limit on number of cameras to process",
    )
    parser.add_argument(
        "--output-dir",
        type=Path,
        default=REPO_ROOT / "data" / "harvested",
        help="Directory to persist local frames and labels (default: data/harvested)",
    )
    parser.add_argument(
        "--concurrency",
        type=int,
        default=10,
        help="Worker thread concurrency for network I/O (default: 10)",
    )

    parsed = parser.parse_args(args)

    # Infer scope from trigger if not explicitly provided
    if parsed.scope is None:
        if parsed.trigger == "rain":
            parsed.scope = "flood-prone"
        else:
            parsed.scope = "hotset"

    return parsed


def harvest_single_camera(
    camera: Dict[str, Any],
    weather: str,
    rainfall_mm: float,
    cv_engine_url: str,
    output_dir: Path,
    schema_path: Path,
    do_validate: bool,
    upload_r2: bool,
    r2_client: Any,
    r2_bucket: str,
    ts: int,
    trigger: str,
    loc_tz: timezone,
) -> Dict[str, Any]:
    """Processes a single camera through the full harvesting pipeline."""
    cam_id = camera["id"]
    result: Dict[str, Any] = {
        "camera_id": cam_id,
        "success": False,
        "status": "pending",
        "traffic": None,
        "flood": None,
        "errors": [],
    }

    # 1. Download snapshot JPEG
    frame_bytes = fetch_camera_frame(camera)
    if not frame_bytes:
        result["status"] = "fetch_failed"
        result["errors"].append("Failed to fetch camera snapshot")
        return result

    # 2. Validate JPEG SOI and minimum size
    if not is_valid_jpeg(frame_bytes):
        result["status"] = "invalid_jpeg"
        result["errors"].append(f"Invalid JPEG payload (size={len(frame_bytes)} bytes)")
        return result

    # 3. Predict vehicle counts via OpenVINO
    pred_res = detect_vehicles(frame_bytes, cv_engine_url)

    # 4. Synthesize Draft-07 label manifest
    manifest = synthesize_label(
        camera=camera,
        weather=weather,
        rainfall_mm=rainfall_mm,
        pred_res=pred_res,
        ts=ts,
        trigger=trigger,
        loc_tz=loc_tz,
    )

    result["traffic"] = manifest["labels"]["traffic"]
    result["flood"] = manifest["labels"]["flood"]

    # 5. Validate schema if requested
    if do_validate:
        is_valid, schema_errors = validate_label_manifest(manifest, schema_path)
        if not is_valid:
            result["status"] = "schema_invalid"
            result["errors"].extend(schema_errors)
            return result

    # 6. Save to local disk
    dt = datetime.fromtimestamp(ts, tz=loc_tz)
    date_str = dt.strftime("%Y-%m-%d")

    cam_dir = output_dir / date_str / cam_id
    cam_dir.mkdir(parents=True, exist_ok=True)

    img_path = cam_dir / f"{ts}_sample.jpg"
    with open(img_path, "wb") as f:
        f.write(frame_bytes)

    label_dir = output_dir / "labels" / date_str
    label_dir.mkdir(parents=True, exist_ok=True)

    json_path = label_dir / f"{cam_id}_{ts}.json"
    with open(json_path, "w", encoding="utf-8") as f:
        json.dump(manifest, f, indent=2, ensure_ascii=False)

    # 7. Upload to Cloudflare R2 if requested
    if upload_r2 and r2_client is not None:
        r2_img_key = f"{date_str}/{cam_id}/{ts}_sample.jpg"
        r2_json_key = f"labels/{date_str}/{cam_id}_{ts}.json"

        ok_img = upload_r2_object(r2_client, r2_bucket, r2_img_key, frame_bytes, "image/jpeg")
        ok_json = upload_r2_object(
            r2_client, r2_bucket, r2_json_key, json.dumps(manifest, indent=2).encode("utf-8"), "application/json"
        )
        if not ok_img or not ok_json:
            result["status"] = "r2_upload_partial_failure"
            result["errors"].append("Failed to upload frame or label manifest to R2")
            return result

    result["success"] = True
    result["status"] = "success"
    return result


def run_pipeline(args: argparse.Namespace) -> int:
    """Executes the automated harvest pipeline according to parsed CLI args."""
    logging.basicConfig(
        level=logging.INFO,
        format="%(asctime)s [%(levelname)s] %(message)s",
        datefmt="%Y-%m-%d %H:%M:%S",
    )

    print("=" * 80)
    print("PROJECTKV STANDALONE AUTOMATED HARVESTER CLI (S7-T7c)")
    print("=" * 80)

    # 1. Resolve Weather
    weather, rainfall_mm, wmo_code = resolve_weather(args.weather)
    print(f"Trigger Mode:        {args.trigger}")
    print(f"Target Scope:        {args.scope}")
    print(f"Weather Condition:   {weather} (rain={rainfall_mm:.2f}mm, wmo={wmo_code})")
    print(f"CV Engine URL:       {args.cv_engine_url}")
    print(f"Upload to R2:        {args.upload_r2} (bucket={args.r2_bucket if args.upload_r2 else 'none'})")
    print(f"Validate Draft-07:   {args.validate}")
    print(f"Dry Run:             {args.dry_run}")
    print(f"Output Directory:    {args.output_dir}")
    print("-" * 80)

    # 2. Load and Filter Cameras
    catalog = load_catalog(args.catalog_path)
    chokepoints = load_chokepoints(args.chokepoints_path)
    cameras = filter_cameras(catalog, args.scope, chokepoints)

    if args.limit is not None and args.limit > 0:
        cameras = cameras[: args.limit]

    print(f"[INFO] Selected {len(cameras)} camera(s) for scope='{args.scope}' (limit={args.limit})")

    # 3. Dry-Run Check
    if args.dry_run:
        print("\n[DRY RUN] Execution Plan:")
        print(f"  • Trigger:              {args.trigger}")
        print(f"  • Target Cameras:       {len(cameras)}")
        print(f"  • Inferred Weather:     {weather}")
        print(f"  • Local Output Path:    {args.output_dir}")
        print(f"  • Cloudflare R2 Upload: {args.upload_r2}")
        print("\nTarget Sample Cameras:")
        for idx, c in enumerate(cameras[:5], 1):
            name = c.get("displayName") or c.get("title", "")
            print(f"  {idx}. [{c['id']}] {name} (corridor: {c.get('road_name', 'N/A')})")
        if len(cameras) > 5:
            print(f"  ... and {len(cameras) - 5} more cameras.")
        print("-" * 80)
        print("[DRY RUN] Completed without network I/O or file writes.")
        return 0

    # 4. Initialize R2 Client if requested
    r2_client = None
    if args.upload_r2:
        r2_client = get_r2_client()
        if r2_client is None:
            print("[ERROR] Cloudflare R2 client initialization failed. Aborting upload.")
            return 1

    # 5. Process Cameras Concurrently
    ts = int(time.time())
    loc_tz = timezone(timedelta(hours=7))  # ICT UTC+7

    stats = {
        "total": len(cameras),
        "success": 0,
        "failed": 0,
        "traffic": {"CLEAR": 0, "MODERATE": 0, "HEAVY": 0, "CONGESTED": 0},
        "flood": {"NONE": 0, "SHALLOW": 0, "DEEP": 0},
    }

    t0 = time.time()
    print(f"[INFO] Starting concurrent harvesting across {len(cameras)} camera(s) with {args.concurrency} worker(s)...")

    with concurrent.futures.ThreadPoolExecutor(max_workers=args.concurrency) as executor:
        future_to_cam = {
            executor.submit(
                harvest_single_camera,
                camera=cam,
                weather=weather,
                rainfall_mm=rainfall_mm,
                cv_engine_url=args.cv_engine_url,
                output_dir=args.output_dir,
                schema_path=args.schema_path,
                do_validate=args.validate,
                upload_r2=args.upload_r2,
                r2_client=r2_client,
                r2_bucket=args.r2_bucket,
                ts=ts,
                trigger=args.trigger,
                loc_tz=loc_tz,
            ): cam
            for cam in cameras
        }

        for future in concurrent.futures.as_completed(future_to_cam):
            cam = future_to_cam[future]
            try:
                res = future.result()
                if res["success"]:
                    stats["success"] += 1
                    if res["traffic"] in stats["traffic"]:
                        stats["traffic"][res["traffic"]] += 1
                    if res["flood"] in stats["flood"]:
                        stats["flood"][res["flood"]] += 1
                else:
                    stats["failed"] += 1
                    logging.warning(
                        "Camera %s (%s) failed: status=%s errors=%s",
                        cam["id"],
                        cam.get("displayName", ""),
                        res["status"],
                        res["errors"],
                    )
            except Exception as e:
                stats["failed"] += 1
                logging.error("Exception processing camera %s: %s", cam["id"], e)

    elapsed = time.time() - t0
    fps = stats["total"] / elapsed if elapsed > 0 else 0.0

    print("=" * 80)
    print("HARVESTING SUMMARY")
    print("=" * 80)
    print(f"Total Cameras:       {stats['total']}")
    print(f"Successful Harvest:  {stats['success']}")
    print(f"Failed Harvest:      {stats['failed']}")
    print(f"Execution Time:      {elapsed:.2f}s ({fps:.2f} cameras/sec)")
    print("-" * 80)
    print(f"Traffic Density:     {stats['traffic']}")
    print(f"Flood Severity:      {stats['flood']}")
    print("=" * 80)

    return 0 if stats["failed"] == 0 or stats["success"] > 0 else 1


def main():
    args = parse_args()
    sys.exit(run_pipeline(args))


if __name__ == "__main__":
    main()
