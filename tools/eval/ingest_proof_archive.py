#!/usr/bin/env python3
"""
ingest_proof_archive.py - Replay proof-archive frames into hotset_verdict_history.

Sprint 11 / Issue #396 (S11-T2):
  * Discovers historical proof frames stored as
    ``<archive_dir>/<camera_id>/<timestamp>_<event>.jpg``.
  * Validates JPEG magic bytes and structural integrity before analysis.
  * Extracts frame-wide and directional PCU / saturation-index verdicts from the
    local CV engine when reachable, from an existing Draft-07 label manifest when
    available, or from a deterministic image-content heuristic.
  * Persists observations into SQLite ``hotset_verdict_history`` using migrations
    0007 and 0008 (append-only, direction-aware, idempotent inserts).
  * Emits a replay manifest that records every processed frame, its provenance,
    and the verdicts derived from it.

The tool is deliberately standard-library first. Pillow is used only when it is
already installed; the deterministic fallback works from JPEG structure and byte
statistics when Pillow is unavailable.

Adheres strictly to AGENTS.md:
  * Rule 1: English-only, zero-emoji code, docstrings, and logs.
  * Rule 2: Invalid frames, duplicate rows, partial archives, and unreachable
    services are handled explicitly instead of being silently coerced.
  * Rule 8: No credentials are stored in code. Optional environment values are
    loaded from the process environment or a local .env file.
  * Rule 10: The manifest records ``provenance="replayed"`` and the archive path
    for every frame so evidence remains auditable.

Example:
    python3 tools/eval/ingest_proof_archive.py \
        --archive-dir services/go-server/data/proofs/2026-11-03 \
        --db-path services/go-server/data/camera-view.db \
        --manifest-out docs/proofs/sprint11/proof_archive_manifest.json
"""

from __future__ import annotations

import argparse
import hashlib
import json
import logging
import math
import os
import re
import sqlite3
import sys
import urllib.error
import urllib.request
from collections import Counter
from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path
from typing import Any, Dict, List, Mapping, Optional, Sequence, Tuple
from uuid import uuid4

try:
    from PIL import Image, ImageChops, ImageDraw, ImageFilter
except ImportError:  # Pillow is optional; the byte-statistics fallback remains active.
    Image = None  # type: ignore[assignment]
    ImageChops = None  # type: ignore[assignment]
    ImageDraw = None  # type: ignore[assignment]
    ImageFilter = None  # type: ignore[assignment]

PIL_AVAILABLE = Image is not None

LOG = logging.getLogger("proof_archive.ingest")

REPO_ROOT = Path(__file__).resolve().parents[2]

DEFAULT_ARCHIVE_DIR = REPO_ROOT / "services" / "go-server" / "data" / "proofs" / "2026-11-03"
DEFAULT_DB_PATH = REPO_ROOT / "services" / "go-server" / "data" / "camera-view.db"
DEFAULT_MANIFEST_PATH = REPO_ROOT / "docs" / "proofs" / "sprint11" / "proof_archive_manifest.json"
DEFAULT_CAMERAS_FILE = REPO_ROOT / "cameras_enriched.json"
DEFAULT_ROADWAY_REGIONS_DIR = REPO_ROOT / "data" / "roadway_regions"
DEFAULT_CV_ENGINE_URL = "http://127.0.0.1:8000"

TABLE_NAME = "hotset_verdict_history"
TIMESTAMP_FIXED_LAYOUT = "%Y-%m-%dT%H:%M:%S"
TIMESTAMP_SUFFIX = ".%06d000Z"
DEFAULT_BATCH_SIZE = 500

# PCU weights conforming to ADR-005 T6 / services/go-server/internal/cvclient.
PCU_WEIGHTS: Dict[str, float] = {
    "motorcycle": 0.3,
    "car": 1.0,
    "bus": 2.5,
    "truck": 2.0,
    "container": 2.0,
}
CAPACITY_FLOOR = 15.0

# Five-class operational vocabulary requested by S11-T2. The Go server and the
# forecast feature store use the canonical four-class vocabulary, so every
# record also carries the canonical value persisted to SQLite.
SEVERITY_LEVELS: Tuple[str, ...] = ("clear", "slight", "moderate", "heavy", "severe")
SEVERITY_BANDS: Dict[str, Tuple[float, float]] = {
    "clear": (0.0, 0.15),
    "slight": (0.15, 0.30),
    "moderate": (0.30, 0.60),
    "heavy": (0.60, 0.80),
    "severe": (0.80, 1.50),
}
CANONICAL_SEVERITY: Dict[str, str] = {
    "clear": "CLEAR",
    "slight": "CLEAR",
    "moderate": "MODERATE",
    "heavy": "HEAVY",
    "severe": "CONGESTED",
}
CANONICAL_REPRESENTATIVE_SI: Dict[str, float] = {
    "CLEAR": 0.10,
    "MODERATE": 0.45,
    "HEAVY": 0.70,
    "CONGESTED": 0.90,
}

IMAGE_EDGE_FLOOR = 0.045
IMAGE_EDGE_SPAN = 0.30
IMAGE_MIN_WIDTH = 160
# Minimum accepted frame resolution. 1x1 or placeholder frames cannot carry a
# defensible traffic verdict, so they are rejected before analysis.
FRAME_MIN_WIDTH = 320
FRAME_MIN_HEIGHT = 240
FRAME_ROAD_CROP_TOP = 0.35


# ---------------------------------------------------------------------------
# Environment and timestamp helpers
# ---------------------------------------------------------------------------


def load_dotenv(candidates: Optional[Sequence[Path]] = None) -> None:
    """Load simple KEY=VALUE pairs from local .env files without overriding env."""
    paths = list(candidates) if candidates else [Path.cwd() / ".env", REPO_ROOT / ".env"]
    for env_path in paths:
        try:
            if not env_path.is_file():
                continue
            with env_path.open("r", encoding="utf-8") as handle:
                for raw_line in handle:
                    line = raw_line.strip()
                    if not line or line.startswith("#") or "=" not in line:
                        continue
                    key, value = line.split("=", 1)
                    key = key.strip()
                    if not key or key in os.environ:
                        continue
                    os.environ[key] = value.strip().strip("'\"")
            return
        except OSError:
            continue


def format_timestamp(value: datetime) -> str:
    """Format an aware datetime as the fixed-width UTC layout used by Go."""
    utc_value = value.astimezone(timezone.utc)
    return utc_value.strftime(TIMESTAMP_FIXED_LAYOUT) + (TIMESTAMP_SUFFIX % utc_value.microsecond)


def to_rfc3339nano(timestamp: int) -> str:
    """Convert Unix epoch seconds to a fixed-width RFC3339Nano UTC string."""
    return format_timestamp(datetime.fromtimestamp(int(timestamp), tz=timezone.utc))


def safe_observed_at(timestamp: int) -> Optional[str]:
    """Format an epoch timestamp, returning None when it is outside the calendar range."""
    try:
        return to_rfc3339nano(timestamp)
    except (OverflowError, OSError, ValueError) as exc:
        LOG.warning("Skipping frame with out-of-range timestamp %s: %s", timestamp, exc)
        return None


def utc_now_timestamp() -> str:
    """Return the current UTC time in the fixed-width RFC3339Nano layout."""
    return format_timestamp(datetime.now(timezone.utc))


def parse_timestamp(value: Any) -> Optional[datetime]:
    """Parse the timestamp encodings used by SQLite and Go into aware UTC."""
    if value is None:
        return None
    if isinstance(value, datetime):
        parsed = value
    else:
        text = str(value).strip()
        if not text:
            return None
        text = text.replace("Z", "+00:00")
        if text.endswith(" UTC"):
            text = text[: -len(" UTC")] + "+00:00"
        try:
            parsed = datetime.fromisoformat(text)
        except ValueError:
            for layout in (
                "%Y-%m-%d %H:%M:%S.%f %z %Z",
                "%Y-%m-%d %H:%M:%S %z %Z",
                "%Y-%m-%d %H:%M:%S.%f %z",
                "%Y-%m-%d %H:%M:%S %z",
                "%Y-%m-%d %H:%M:%S.%f",
                "%Y-%m-%d %H:%M:%S",
            ):
                try:
                    parsed = datetime.strptime(text, layout)
                    break
                except ValueError:
                    continue
            else:
                return None
    if parsed.tzinfo is None:
        parsed = parsed.replace(tzinfo=timezone.utc)
    return parsed.astimezone(timezone.utc)


def normalize_timestamp(value: Any) -> str:
    """Normalize a stored timestamp to the fixed-width layout when possible."""
    parsed = parse_timestamp(value)
    if parsed is None:
        return str(value)
    return format_timestamp(parsed)


# ---------------------------------------------------------------------------
# Archive discovery and JPEG validation
# ---------------------------------------------------------------------------


@dataclass(frozen=True)
class FrameSource:
    """One proof frame discovered on disk."""

    camera_id: str
    timestamp: int
    event: str
    path: Path


_FRAME_NAME = re.compile(r"^(?P<timestamp>\d{10,})_(?P<event>[A-Za-z0-9][A-Za-z0-9_-]*)\.(?P<suffix>jpe?g)$", re.IGNORECASE)


def parse_frame_filename(name: str) -> Optional[Tuple[int, str]]:
    """Parse ``<timestamp>_<event>.jpg`` into timestamp and event tokens."""
    try:
        match = _FRAME_NAME.match(name)
        if match is None:
            return None
        timestamp = int(match.group("timestamp"))
    except (TypeError, ValueError, OverflowError) as exc:
        LOG.warning("Cannot parse frame filename %r: %s", name, exc)
        return None
    return timestamp, match.group("event")


def discover_frames(archive_dir: Path) -> Tuple[List[FrameSource], List[Dict[str, str]]]:
    """Discover proof frames and report files whose names do not match the contract."""
    if not archive_dir.is_dir():
        raise FileNotFoundError(f"archive directory not found: {archive_dir}")

    frames: List[FrameSource] = []
    unrecognized: List[Dict[str, str]] = []
    seen: set[Path] = set()

    for camera_dir in sorted(archive_dir.iterdir(), key=lambda item: item.name):
        if not camera_dir.is_dir() or camera_dir.name.startswith("."):
            continue
        camera_id = camera_dir.name
        for candidate in sorted(camera_dir.iterdir(), key=lambda item: item.name):
            if not candidate.is_file():
                continue
            if candidate.suffix.lower() not in (".jpg", ".jpeg"):
                continue
            try:
                parsed = parse_frame_filename(candidate.name)
            except (TypeError, ValueError, OverflowError) as exc:
                LOG.warning("Skipping malformed frame filename %s: %s", candidate, exc)
                unrecognized.append({"path": str(candidate.relative_to(archive_dir)), "reason": "name_parse_error"})
                continue
            if parsed is None:
                unrecognized.append({"path": str(candidate.relative_to(archive_dir)), "reason": "name_pattern"})
                continue
            timestamp, event = parsed
            resolved = candidate.resolve()
            if resolved in seen:
                continue
            seen.add(resolved)
            frames.append(FrameSource(camera_id=camera_id, timestamp=timestamp, event=event, path=candidate))

    frames.sort(key=lambda item: (item.timestamp, item.camera_id, item.event, str(item.path)))
    return frames, unrecognized


def validate_jpeg_bytes(data: bytes) -> Tuple[bool, str]:
    """Validate JPEG SOI/EOI markers and enforce a usable frame resolution."""
    if not data:
        return False, "empty_file"
    if len(data) < 4:
        return False, "truncated_header"
    if data[:3] != b"\xff\xd8\xff":
        return False, "invalid_magic"
    eoi_index = data.rfind(b"\xff\xd9")
    if eoi_index < 0:
        return False, "missing_eoi"
    if eoi_index < len(data) - 64:
        return False, "unexpected_trailing_data"
    dimensions = read_jpeg_dimensions(data)
    if dimensions is None:
        return False, "dimensions_unreadable"
    width, height = dimensions
    if width < FRAME_MIN_WIDTH or height < FRAME_MIN_HEIGHT:
        return False, "resolution_too_small"
    return True, ""


def read_jpeg_dimensions(data: bytes) -> Optional[Tuple[int, int]]:
    """Read JPEG width and height from the first Start-Of-Frame marker."""
    if len(data) < 4 or data[:3] != b"\xff\xd8\xff":
        return None
    index = 2
    length = len(data)
    while index < length:
        if data[index] != 0xFF:
            index += 1
            continue
        if index + 1 >= length:
            return None
        marker = data[index + 1]
        index += 2
        if marker in (
            0xC0,
            0xC1,
            0xC2,
            0xC3,
            0xC5,
            0xC6,
            0xC7,
            0xC9,
            0xCA,
            0xCB,
            0xCD,
            0xCE,
            0xCF,
        ):
            if index + 7 > length:
                return None
            height = int.from_bytes(data[index + 3 : index + 5], "big")
            width = int.from_bytes(data[index + 5 : index + 7], "big")
            if width > 0 and height > 0:
                return width, height
            return None
        if marker in (0xD9, 0xDA):
            return None
        if index + 2 > length:
            return None
        segment_length = int.from_bytes(data[index : index + 2], "big")
        if segment_length < 2:
            return None
        index += segment_length
    return None


# ---------------------------------------------------------------------------
# Roadway regions and camera capacities
# ---------------------------------------------------------------------------


@dataclass(frozen=True)
class RoadwayRegion:
    """One directional roadway polygon registered for a camera."""

    region_id: str
    direction: str
    capacity_share: float
    polygon: Tuple[Tuple[float, float], ...]

    def as_meta(self) -> Dict[str, Any]:
        return {
            "region_id": self.region_id,
            "direction": self.direction,
            "capacity_share": self.capacity_share,
            "polygon": [list(point) for point in self.polygon],
        }


def _parse_polygon(raw: Any) -> Tuple[Tuple[float, float], ...]:
    points: List[Tuple[float, float]] = []
    if not isinstance(raw, list):
        return ()
    for item in raw:
        if not isinstance(item, (list, tuple)) or len(item) < 2:
            continue
        try:
            x = float(item[0])
            y = float(item[1])
        except (TypeError, ValueError):
            continue
        points.append((min(1.0, max(0.0, x)), min(1.0, max(0.0, y))))
    return tuple(points)


def load_roadway_regions(camera_id: str, regions_dir: Path) -> List[RoadwayRegion]:
    """Load validated directional polygons for a camera, returning an empty list if absent."""
    path = regions_dir / f"{camera_id}.json"
    if not path.is_file():
        return []
    try:
        with path.open("r", encoding="utf-8") as handle:
            payload = json.load(handle)
    except (OSError, json.JSONDecodeError) as exc:
        LOG.warning("Cannot read roadway regions for camera %s: %s", camera_id, exc)
        return []

    raw_regions: Any = payload
    if isinstance(payload, dict):
        raw_regions = payload.get("regions", [])
    if not isinstance(raw_regions, list) or not raw_regions:
        return []

    regions: List[RoadwayRegion] = []
    seen_directions: set[str] = set()
    for raw in raw_regions:
        if not isinstance(raw, dict):
            continue
        region_id = str(raw.get("region_id") or "").strip()
        direction = str(raw.get("direction") or "").strip().lower()
        if not direction and region_id:
            match = re.search(r"(?:^|_)dir_([A-Za-z0-9]+)$", region_id)
            if match:
                direction = match.group(1).lower()
        if not direction or direction in seen_directions:
            continue
        try:
            share = float(raw.get("capacity_share", 0.5))
        except (TypeError, ValueError):
            share = 0.5
        if share <= 0.0 or share > 1.0:
            share = 0.5
        polygon = _parse_polygon(raw.get("polygon"))
        if not region_id:
            region_id = f"{camera_id}_dir_{direction}"
        regions.append(
            RoadwayRegion(
                region_id=region_id,
                direction=direction,
                capacity_share=share,
                polygon=polygon,
            )
        )
        seen_directions.add(direction)

    regions.sort(key=lambda item: item.direction)
    return regions


def load_camera_capacities(catalog_path: Path) -> Dict[str, float]:
    """Load per-camera base capacity from the enriched camera catalog."""
    if not catalog_path.is_file():
        LOG.warning("Camera catalog not found, using capacity floor %.1f: %s", CAPACITY_FLOOR, catalog_path)
        return {}
    try:
        with catalog_path.open("r", encoding="utf-8") as handle:
            payload = json.load(handle)
    except (OSError, json.JSONDecodeError) as exc:
        LOG.warning("Cannot read camera catalog %s: %s", catalog_path, exc)
        return {}

    items: Any = payload
    if isinstance(payload, dict):
        items = payload.get("cameras", [])
    if not isinstance(items, list):
        return {}

    capacities: Dict[str, float] = {}
    for item in items:
        if not isinstance(item, dict):
            continue
        camera_id = str(item.get("id") or "").strip()
        if not camera_id:
            continue
        try:
            capacity = float(item.get("base_capacity", item.get("capacity", CAPACITY_FLOOR)))
        except (TypeError, ValueError):
            capacity = CAPACITY_FLOOR
        if capacity <= 0.0:
            capacity = CAPACITY_FLOOR
        capacities[camera_id] = capacity
    return capacities


# ---------------------------------------------------------------------------
# Verdict construction
# ---------------------------------------------------------------------------


@dataclass(frozen=True)
class VerdictRecord:
    """One frame-wide or directional observation ready for persistence."""

    camera_id: str
    direction: str
    region_id: str
    pcu_total: float
    saturation_index: float
    severity: str
    severity_class: str
    confidence: float
    source: str
    observed_at: str
    method: str

    def as_db_row(self) -> Tuple[Any, ...]:
        return (
            self.camera_id,
            self.direction,
            self.region_id,
            round(max(0.0, self.pcu_total), 4),
            round(max(0.0, self.saturation_index), 6),
            self.severity,
            round(min(1.0, max(0.0, self.confidence)), 4),
            self.source,
            self.observed_at,
        )

    def as_manifest(self) -> Dict[str, Any]:
        return {
            "direction": self.direction,
            "region_id": self.region_id,
            "pcu_total": round(self.pcu_total, 4),
            "saturation_index": round(self.saturation_index, 6),
            "severity": self.severity,
            "severity_class": self.severity_class,
            "confidence": round(self.confidence, 4),
            "method": self.method,
        }


def _clamp(value: float, low: float, high: float) -> float:
    return max(low, min(high, value))


def classify_severity(saturation_index: float) -> str:
    """Map a saturation index to the five-class S11-T2 vocabulary."""
    value = max(0.0, float(saturation_index))
    if value < SEVERITY_BANDS["slight"][0]:
        return "clear"
    if value < SEVERITY_BANDS["moderate"][0]:
        return "slight"
    if value < SEVERITY_BANDS["heavy"][0]:
        return "moderate"
    if value < SEVERITY_BANDS["severe"][0]:
        return "heavy"
    return "severe"


def canonical_severity(severity_class: str) -> str:
    """Map the operational class to the canonical Go server vocabulary."""
    return CANONICAL_SEVERITY.get(severity_class, "CLEAR")


def normalize_label_severity(value: Any) -> Optional[str]:
    """Normalize a Draft-07 traffic label into the operational class vocabulary."""
    text = str(value or "").strip().upper()
    mapping = {
        "CLEAR": "clear",
        "FREE_FLOW": "clear",
        "SLIGHT": "slight",
        "LOW": "slight",
        "MODERATE": "moderate",
        "MEDIUM": "moderate",
        "HEAVY": "heavy",
        "HIGH": "heavy",
        "CONGESTED": "severe",
        "SEVERE": "severe",
        "GRIDLOCK": "severe",
    }
    return mapping.get(text)


def _clip_si_to_class_band(saturation_index: float, severity_class: str) -> float:
    """Clamp a label-authoritative saturation index inside its class band."""
    low, high = SEVERITY_BANDS.get(severity_class, (0.0, 1.5))
    if saturation_index < low:
        return low
    if saturation_index >= high:
        return max(low, high - 0.001)
    return saturation_index


def _make_record(
    camera_id: str,
    direction: str,
    region_id: str,
    capacity: float,
    saturation_index: float,
    observed_at: str,
    source: str,
    method: str,
    confidence: float,
    forced_class: Optional[str] = None,
) -> VerdictRecord:
    effective_capacity = max(1.0, float(capacity))
    si = max(0.0, float(saturation_index))
    if forced_class is not None:
        canonical = canonical_severity(forced_class)
        si = _clip_si_to_class_band(si, forced_class)
        severity_class = forced_class
    else:
        severity_class = classify_severity(si)
        canonical = canonical_severity(severity_class)
    pcu_total = si * effective_capacity
    return VerdictRecord(
        camera_id=camera_id,
        direction=direction,
        region_id=region_id,
        pcu_total=pcu_total,
        saturation_index=si,
        severity=canonical,
        severity_class=severity_class,
        confidence=confidence,
        source=source,
        observed_at=observed_at,
        method=method,
    )


def compute_pcu(counts: Mapping[str, Any]) -> float:
    """Compute PCU total from a class-count mapping."""
    total = 0.0
    for key, weight in PCU_WEIGHTS.items():
        try:
            total += max(0.0, float(counts.get(key, 0) or 0)) * weight
        except (TypeError, ValueError):
            continue
    return total


def _counts_total(counts: Mapping[str, Any]) -> int:
    total = 0
    for value in counts.values():
        try:
            total += int(value)
        except (TypeError, ValueError):
            continue
    return max(0, total)


def _compute_saturation(
    counts: Mapping[str, Any],
    fallback_total: int,
    capacity: float,
) -> float:
    effective_capacity = max(1.0, float(capacity))
    pcu_total = compute_pcu(counts)
    if pcu_total <= 0.0 and fallback_total > 0:
        pcu_total = float(fallback_total) * PCU_WEIGHTS["car"]
    return pcu_total / effective_capacity


# ---------------------------------------------------------------------------
# Deterministic image-content heuristic
# ---------------------------------------------------------------------------


def _edge_fraction(image: Any, polygon: Optional[Sequence[Tuple[float, float]]]) -> Optional[float]:
    """Return the dense-edge fraction for a road crop or normalized polygon."""
    if Image is None or ImageChops is None or ImageDraw is None or ImageFilter is None:
        return None

    width, height = image.size
    if width <= 0 or height <= 0:
        return None
    scale = IMAGE_MIN_WIDTH / float(width)
    small_width = max(IMAGE_MIN_WIDTH, int(width * scale))
    small_height = max(1, int(height * scale))
    small = image.resize((small_width, small_height))

    if polygon and len(polygon) >= 3:
        mask = Image.new("L", small.size, 0)
        points = [(int(x * small_width), int(y * small_height)) for x, y in polygon]
        ImageDraw.Draw(mask).polygon(points, fill=255)
        mask_pixels = int(mask.histogram()[255])
        if mask_pixels < max(64, int(small_width * small_height * 0.02)):
            return None
        edges = small.filter(ImageFilter.FIND_EDGES)
        masked = ImageChops.multiply(edges, mask)
        histogram = masked.histogram()
        return float(sum(histogram[96:])) / float(mask_pixels)

    crop_top = int(small_height * FRAME_ROAD_CROP_TOP)
    road = small.crop((0, crop_top, small_width, small_height))
    if road.width <= 0 or road.height <= 0:
        return None
    edges = road.filter(ImageFilter.FIND_EDGES)
    histogram = edges.histogram()
    total = max(1, sum(histogram))
    return float(sum(histogram[96:])) / float(total)


def _compressed_image_fraction(data: bytes) -> float:
    """Estimate visual complexity from JPEG byte statistics when Pillow is absent."""
    width_height = read_jpeg_dimensions(data)
    pixels = float(width_height[0] * width_height[1]) if width_height else 200000.0
    bytes_per_pixel = len(data) / max(1.0, pixels)

    counts = Counter(data)
    total = float(len(data))
    entropy = 0.0
    if total > 0:
        for count in counts.values():
            probability = count / total
            entropy -= probability * math.log2(probability)

    density_from_size = _clamp((bytes_per_pixel - 0.10) / 0.55, 0.0, 1.0)
    density_from_entropy = _clamp((entropy - 6.0) / 2.2, 0.0, 1.0)
    return _clamp(0.65 * density_from_size + 0.35 * density_from_entropy, 0.0, 1.0)


def estimate_image_saturation(
    data: bytes,
    polygon: Optional[Sequence[Tuple[float, float]]] = None,
) -> float:
    """Estimate a deterministic saturation index from actual image content."""
    if PIL_AVAILABLE:
        try:
            from io import BytesIO

            with Image.open(BytesIO(data)) as decoded:  # type: ignore[union-attr]
                grayscale = decoded.convert("L")
                edge_fraction = _edge_fraction(grayscale, polygon)
                if edge_fraction is None:
                    edge_fraction = _edge_fraction(grayscale, None)
            if edge_fraction is not None:
                density = _clamp((edge_fraction - IMAGE_EDGE_FLOOR) / IMAGE_EDGE_SPAN, 0.0, 0.95)
                return density
        except (OSError, ValueError, RuntimeError):
            LOG.debug("Pillow could not decode frame; falling back to byte statistics", exc_info=True)

    return _clamp(_compressed_image_fraction(data) * 0.95, 0.0, 0.95)


# ---------------------------------------------------------------------------
# CV engine client
# ---------------------------------------------------------------------------

# Verdict tokens emitted by the local CV engine contract (traffic and flood).
VALID_CV_STATUSES = frozenset(
    {
        "CLEAR",
        "MODERATE",
        "HEAVY",
        "CONGESTED",
        "SEVERE",
        "DRY",
        "WET",
        "FLOODED_SHALLOW",
        "FLOODED_DEEP",
    }
)

_ERROR_DETAIL_KEYS = ("error", "errors", "detail", "error_detail")


def validate_cv_response(payload: Any) -> Tuple[bool, str]:
    """Validate a /predict payload against the strict local-engine contract.

    Mock/standby sources, transport error details, and payloads without a usable
    verdict status are rejected so they can never be persisted as observations.
    """
    if not isinstance(payload, Mapping):
        return False, "not_a_json_object"

    source = str(payload.get("source", "") or "").strip().lower()
    if source in {"mock", "standby_dummy"}:
        return False, f"rejected_source:{source}"

    for key in _ERROR_DETAIL_KEYS:
        if payload.get(key):
            return False, f"error_payload:{key}"

    status_raw = payload.get("status")
    if status_raw is None or not str(status_raw).strip():
        status_raw = payload.get("traffic_severity")
    if status_raw is None or not str(status_raw).strip():
        status_raw = payload.get("flood_severity")
    status = str(status_raw or "").strip().upper()
    if not status:
        return False, "empty_status"
    if status not in VALID_CV_STATUSES:
        return False, f"invalid_status:{status}"
    return True, ""


class CVEngineClient:
    """Small /predict client that disables itself after a transport-level failure."""

    def __init__(self, url: str, timeout: float = 10.0) -> None:
        self.url = url.rstrip("/")
        self.timeout = max(1.0, float(timeout))
        self.available = bool(self.url)

    def _multipart_body(
        self,
        image_bytes: bytes,
        regions: Sequence[RoadwayRegion],
    ) -> Tuple[bytes, str]:
        boundary = "----ProjectKVProofBoundary" + uuid4().hex
        chunks: List[bytes] = []

        def add_field(name: str, value: bytes, filename: Optional[str] = None, content_type: Optional[str] = None) -> None:
            chunks.append(f"--{boundary}\r\n".encode("ascii"))
            disposition = f'Content-Disposition: form-data; name="{name}"'
            if filename:
                disposition += f'; filename="{filename}"'
            chunks.append((disposition + "\r\n").encode("utf-8"))
            if content_type:
                chunks.append(f"Content-Type: {content_type}\r\n".encode("ascii"))
            chunks.append(b"\r\n")
            chunks.append(value)
            chunks.append(b"\r\n")

        add_field("frame", image_bytes, filename="frame.jpg", content_type="image/jpeg")
        meta = {"regions": [region.as_meta() for region in regions]}
        add_field("meta", json.dumps(meta, ensure_ascii=False).encode("utf-8"), content_type="application/json")
        chunks.append(f"--{boundary}--\r\n".encode("ascii"))
        return b"".join(chunks), f"multipart/form-data; boundary={boundary}"

    def predict(
        self,
        image_bytes: bytes,
        regions: Sequence[RoadwayRegion],
    ) -> Optional[Dict[str, Any]]:
        """Return a parsed /predict response or None when the engine is unavailable."""
        if not self.available or not self.url:
            return None

        if regions:
            body, content_type = self._multipart_body(image_bytes, regions)
        else:
            body, content_type = image_bytes, "application/octet-stream"

        request = urllib.request.Request(
            f"{self.url}/predict",
            data=body,
            headers={
                "Content-Type": content_type,
                "User-Agent": "Mozilla/5.0 (compatible; ProjectKVProofIngest/1.0)",
            },
            method="POST",
        )
        try:
            with urllib.request.urlopen(request, timeout=self.timeout) as response:
                if response.status != 200:
                    LOG.debug("CV engine returned HTTP %s; disabling for this run", response.status)
                    self.available = False
                    return None
                payload = json.loads(response.read().decode("utf-8"))
        except (urllib.error.URLError, TimeoutError, ConnectionError, OSError) as exc:
            LOG.info("CV engine unavailable at %s (%s); using deterministic fallback", self.url, exc)
            self.available = False
            return None
        except (json.JSONDecodeError, UnicodeDecodeError) as exc:
            LOG.warning("CV engine returned an invalid payload: %s", exc)
            return None

        if not isinstance(payload, dict):
            return None

        valid, reason = validate_cv_response(payload)
        if not valid:
            LOG.warning("CV engine response rejected (%s); using deterministic fallback", reason)
            if reason.startswith("rejected_source:"):
                self.available = False
            return None
        return payload


def _normalize_vehicle_counts(raw: Any) -> Dict[str, int]:
    counts: Dict[str, int] = {}
    if not isinstance(raw, Mapping):
        return counts
    for key in PCU_WEIGHTS:
        try:
            counts[key] = max(0, int(raw.get(key, 0) or 0))
        except (TypeError, ValueError):
            counts[key] = 0
    return counts


def _region_lookup(regions: Sequence[RoadwayRegion]) -> Tuple[Dict[str, RoadwayRegion], Dict[str, RoadwayRegion]]:
    by_id: Dict[str, RoadwayRegion] = {}
    by_direction: Dict[str, RoadwayRegion] = {}
    for region in regions:
        by_id[region.region_id] = region
        by_direction[region.direction] = region
    return by_id, by_direction


def _record_from_cv_response(
    camera_id: str,
    base_capacity: float,
    regions: Sequence[RoadwayRegion],
    response: Mapping[str, Any],
    observed_at: str,
    source: str,
) -> List[VerdictRecord]:
    confidence = _clamp(float(response.get("confidence", 0.0) or 0.0), 0.0, 1.0)
    top_counts = _normalize_vehicle_counts(response.get("vehicle_counts"))
    try:
        top_total = max(0, int(response.get("vehicles_detected", 0) or 0))
    except (TypeError, ValueError):
        top_total = 0
    if top_total <= 0:
        top_total = _counts_total(top_counts)

    raw_regions = response.get("regions")
    if top_total <= 0 and isinstance(raw_regions, list):
        aggregated: Dict[str, int] = {key: 0 for key in PCU_WEIGHTS}
        for raw in raw_regions:
            if not isinstance(raw, dict):
                continue
            region_counts = _normalize_vehicle_counts(raw.get("vehicle_counts"))
            for key, value in region_counts.items():
                aggregated[key] += value
        if _counts_total(aggregated) > 0:
            top_counts = aggregated
            top_total = _counts_total(aggregated)

    frame_si = _compute_saturation(top_counts, top_total, base_capacity)
    records = [
        _make_record(
            camera_id=camera_id,
            direction="",
            region_id="",
            capacity=base_capacity,
            saturation_index=frame_si,
            observed_at=observed_at,
            source=source,
            method="cv_engine",
            confidence=confidence,
        )
    ]

    raw_regions = response.get("regions")
    if not isinstance(raw_regions, list):
        return records

    by_id, by_direction = _region_lookup(regions)
    for raw in raw_regions:
        if not isinstance(raw, dict):
            continue
        direction = str(raw.get("direction") or "").strip().lower()
        region_id = str(raw.get("region_id") or "").strip()
        geometry = by_id.get(region_id) or by_direction.get(direction)
        if geometry is not None:
            if not direction:
                direction = geometry.direction
            if not region_id:
                region_id = geometry.region_id
        if not direction:
            continue
        try:
            share = float(raw.get("capacity_share", geometry.capacity_share if geometry else 0.5))
        except (TypeError, ValueError):
            share = geometry.capacity_share if geometry else 0.5
        if share <= 0.0 or share > 1.0:
            share = geometry.capacity_share if geometry else 0.5
        capacity = max(1.0, base_capacity * share)
        counts = _normalize_vehicle_counts(raw.get("vehicle_counts"))
        try:
            fallback_total = max(0, int(raw.get("vehicles_detected", 0) or 0))
        except (TypeError, ValueError):
            fallback_total = 0
        if fallback_total <= 0:
            fallback_total = _counts_total(counts)
        directional_si = _compute_saturation(counts, fallback_total, capacity)
        records.append(
            _make_record(
                camera_id=camera_id,
                direction=direction,
                region_id=region_id or (geometry.region_id if geometry else f"{camera_id}_dir_{direction}"),
                capacity=capacity,
                saturation_index=directional_si,
                observed_at=observed_at,
                source=source,
                method="cv_engine",
                confidence=confidence,
            )
        )
    return records


# ---------------------------------------------------------------------------
# Label manifest fallback
# ---------------------------------------------------------------------------


_NOTE_VEHICLES = re.compile(r"vehicles_detected\s*[=:]\s*(\d+)", re.IGNORECASE)
_NOTE_PCU = re.compile(r"\bpcu(?:_total)?\s*[=:]\s*([0-9]+(?:\.[0-9]+)?)", re.IGNORECASE)
_NOTE_SI = re.compile(r"(?:\bsi\b|saturation[_ ]index)\s*[=:]\s*([0-9]+(?:\.[0-9]+)?)", re.IGNORECASE)


def find_label_path(labels_dir: Optional[Path], archive_dir: Path, camera_id: str, timestamp: int) -> Optional[Path]:
    """Locate a Draft-07 label manifest for a frame."""
    candidates: List[Path] = []
    if labels_dir is not None:
        candidates.append(labels_dir)
    candidates.extend(
        [
            archive_dir.parent / "labels" / archive_dir.name,
            archive_dir / "labels" / archive_dir.name,
        ]
    )
    for directory in candidates:
        if not directory.is_dir():
            continue
        for name in (f"cam_{camera_id}_{timestamp}.json", f"{camera_id}_{timestamp}.json"):
            path = directory / name
            if path.is_file():
                return path
    return None


def load_label_payload(path: Optional[Path]) -> Optional[Mapping[str, Any]]:
    """Read a label manifest, returning None when it is missing or malformed."""
    if path is None:
        return None
    try:
        with path.open("r", encoding="utf-8") as handle:
            payload = json.load(handle)
    except (OSError, json.JSONDecodeError) as exc:
        LOG.debug("Cannot read label manifest %s: %s", path, exc)
        return None
    return payload if isinstance(payload, dict) else None


def _record_from_label(
    camera_id: str,
    base_capacity: float,
    regions: Sequence[RoadwayRegion],
    label: Mapping[str, Any],
    observed_at: str,
    source: str,
) -> Optional[List[VerdictRecord]]:
    labels = label.get("labels")
    if not isinstance(labels, Mapping):
        return None
    notes = str(label.get("notes") or "")

    frame_class = normalize_label_severity(labels.get("traffic"))
    note_pcu = _NOTE_PCU.search(notes)
    note_si = _NOTE_SI.search(notes)
    note_vehicles = _NOTE_VEHICLES.search(notes)

    frame_si: Optional[float] = None
    if note_si is not None:
        try:
            frame_si = max(0.0, float(note_si.group(1)))
        except ValueError:
            frame_si = None
    elif note_pcu is not None:
        try:
            frame_si = max(0.0, float(note_pcu.group(1))) / max(1.0, base_capacity)
        except ValueError:
            frame_si = None
    elif note_vehicles is not None:
        try:
            frame_si = (float(note_vehicles.group(1)) * PCU_WEIGHTS["car"]) / max(1.0, base_capacity)
        except ValueError:
            frame_si = None

    if frame_si is None:
        if frame_class is None:
            return None
        frame_si = CANONICAL_REPRESENTATIVE_SI[canonical_severity(frame_class)]

    records = [
        _make_record(
            camera_id=camera_id,
            direction="",
            region_id="",
            capacity=base_capacity,
            saturation_index=frame_si,
            observed_at=observed_at,
            source=source,
            method="label_manifest",
            confidence=0.75,
            forced_class=frame_class,
        )
    ]

    for region in regions:
        directional_label = labels.get(f"traffic_dir_{region.direction}")
        if directional_label is None:
            directional_label = labels.get(f"traffic_{region.direction}")
        directional_class = normalize_label_severity(directional_label)
        directional_capacity = max(1.0, base_capacity * region.capacity_share)
        if directional_class is not None:
            directional_si = CANONICAL_REPRESENTATIVE_SI[canonical_severity(directional_class)]
            forced_class: Optional[str] = directional_class
        else:
            directional_si = frame_si
            forced_class = None
        records.append(
            _make_record(
                camera_id=camera_id,
                direction=region.direction,
                region_id=region.region_id,
                capacity=directional_capacity,
                saturation_index=directional_si,
                observed_at=observed_at,
                source=source,
                method="label_manifest",
                confidence=0.70,
                forced_class=forced_class,
            )
        )
    return records


# ---------------------------------------------------------------------------
# Image fallback and frame orchestration
# ---------------------------------------------------------------------------


def _image_records(
    camera_id: str,
    base_capacity: float,
    regions: Sequence[RoadwayRegion],
    image_bytes: bytes,
    observed_at: str,
    source: str,
) -> List[VerdictRecord]:
    frame_si = estimate_image_saturation(image_bytes)
    records = [
        _make_record(
            camera_id=camera_id,
            direction="",
            region_id="",
            capacity=base_capacity,
            saturation_index=frame_si,
            observed_at=observed_at,
            source=source,
            method="image_heuristic",
            confidence=0.35,
        )
    ]
    for region in regions:
        directional_si = estimate_image_saturation(image_bytes, region.polygon)
        directional_capacity = max(1.0, base_capacity * region.capacity_share)
        records.append(
            _make_record(
                camera_id=camera_id,
                direction=region.direction,
                region_id=region.region_id,
                capacity=directional_capacity,
                saturation_index=directional_si,
                observed_at=observed_at,
                source=source,
                method="image_heuristic",
                confidence=0.30,
            )
        )
    return records


def _ensure_directional_records(
    records: List[VerdictRecord],
    camera_id: str,
    base_capacity: float,
    regions: Sequence[RoadwayRegion],
    image_bytes: bytes,
    observed_at: str,
    source: str,
) -> List[VerdictRecord]:
    """Complete missing directional observations with the image-content fallback."""
    if not regions:
        return records
    present = {record.direction for record in records if record.direction}
    completed = list(records)
    for region in regions:
        if region.direction in present:
            continue
        directional_si = estimate_image_saturation(image_bytes, region.polygon)
        directional_capacity = max(1.0, base_capacity * region.capacity_share)
        completed.append(
            _make_record(
                camera_id=camera_id,
                direction=region.direction,
                region_id=region.region_id,
                capacity=directional_capacity,
                saturation_index=directional_si,
                observed_at=observed_at,
                source=source,
                method="image_heuristic",
                confidence=0.30,
            )
        )
    completed.sort(key=lambda item: (item.direction != "", item.direction, item.region_id))
    return completed


def analyze_frame(
    image_bytes: bytes,
    camera_id: str,
    base_capacity: float,
    regions: Sequence[RoadwayRegion],
    observed_at: str,
    cv_client: Optional[CVEngineClient] = None,
    label_payload: Optional[Mapping[str, Any]] = None,
    source: str = "proof_archive",
) -> Tuple[List[VerdictRecord], str]:
    """Derive frame-wide and directional verdicts for one visual frame.

    Resolution order is CV engine, existing label manifest, then deterministic
    image-content heuristic. Missing directional observations are completed from
    the image fallback so dual-carriageway cameras always receive both records.
    """
    if cv_client is not None:
        response = cv_client.predict(image_bytes, regions)
        if response is not None:
            records = _record_from_cv_response(
                camera_id=camera_id,
                base_capacity=base_capacity,
                regions=regions,
                response=response,
                observed_at=observed_at,
                source=source,
            )
            records = _ensure_directional_records(
                records, camera_id, base_capacity, regions, image_bytes, observed_at, source
            )
            return records, "cv_engine"

    if label_payload is not None:
        records = _record_from_label(
            camera_id=camera_id,
            base_capacity=base_capacity,
            regions=regions,
            label=label_payload,
            observed_at=observed_at,
            source=source,
        )
        if records is not None:
            records = _ensure_directional_records(
                records, camera_id, base_capacity, regions, image_bytes, observed_at, source
            )
            return records, "label_manifest"

    records = _image_records(camera_id, base_capacity, regions, image_bytes, observed_at, source)
    return records, "image_heuristic"


# ---------------------------------------------------------------------------
# SQLite schema and persistence
# ---------------------------------------------------------------------------


CREATE_TABLE_SQL = f"""
CREATE TABLE IF NOT EXISTS {TABLE_NAME} (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    camera_id         TEXT NOT NULL,
    pcu_total         REAL,
    saturation_index  REAL,
    severity          TEXT NOT NULL,
    confidence        REAL NOT NULL DEFAULT 0.0,
    source            TEXT NOT NULL DEFAULT '',
    observed_at       TIMESTAMP NOT NULL,
    recorded_at       TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    direction         TEXT NOT NULL DEFAULT '',
    region_id         TEXT NOT NULL DEFAULT '',
    CHECK (pcu_total IS NULL OR pcu_total >= 0.0),
    CHECK (saturation_index IS NULL OR saturation_index >= 0.0)
)
"""


SCHEMA_MIGRATIONS_DDL = """
CREATE TABLE IF NOT EXISTS schema_migrations (
    version     INTEGER PRIMARY KEY,
    applied_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
)
"""

# go-server migration versions whose effects this tool applies itself. Recording
# them prevents the Go migrator from replaying 0007/0008 over an already-created table.
SCHEMA_MIGRATION_VERSIONS = (7, 8)


def configure_connection(connection: sqlite3.Connection, read_only: bool = False) -> None:
    """Apply WAL journaling and a bounded busy timeout to a SQLite connection."""
    connection.execute("PRAGMA busy_timeout = 5000")
    if read_only:
        return
    try:
        connection.execute("PRAGMA journal_mode = WAL")
    except sqlite3.OperationalError as exc:
        LOG.warning("Cannot enable WAL journal mode: %s", exc)


def sync_schema_migrations(connection: sqlite3.Connection) -> List[int]:
    """Mark the migrations this tool already applied as run for the Go migrator."""
    connection.execute(SCHEMA_MIGRATIONS_DDL)
    existing = {
        int(row[0])
        for row in connection.execute("SELECT version FROM schema_migrations").fetchall()
    }
    recorded: List[int] = []
    for version in SCHEMA_MIGRATION_VERSIONS:
        if int(version) in existing:
            continue
        connection.execute(
            "INSERT OR IGNORE INTO schema_migrations (version) VALUES (?)",
            (int(version),),
        )
        recorded.append(int(version))
    return recorded


def _table_columns(connection: sqlite3.Connection) -> List[str]:
    rows = connection.execute(f"PRAGMA table_info({TABLE_NAME})").fetchall()
    return [str(row[1]) for row in rows]


def ensure_history_schema(connection: sqlite3.Connection) -> Dict[str, Any]:
    """Apply migrations 0007 and 0008 to an existing SQLite database.

    Returns a small migration report so callers can log exactly what changed.
    """
    report: Dict[str, Any] = {
        "created_table": False,
        "added_columns": [],
        "dropped_indexes": [],
        "created_indexes": [],
        "synced_migrations": [],
    }

    configure_connection(connection)
    columns = _table_columns(connection)
    if not columns:
        connection.execute(CREATE_TABLE_SQL)
        report["created_table"] = True
        columns = ["camera_id", "pcu_total", "saturation_index", "severity", "confidence", "source", "observed_at", "recorded_at", "direction", "region_id"]
    else:
        if "direction" not in columns:
            connection.execute(f"ALTER TABLE {TABLE_NAME} ADD COLUMN direction TEXT NOT NULL DEFAULT ''")
            report["added_columns"].append("direction")
            columns.append("direction")
        if "region_id" not in columns:
            connection.execute(f"ALTER TABLE {TABLE_NAME} ADD COLUMN region_id TEXT NOT NULL DEFAULT ''")
            report["added_columns"].append("region_id")
            columns.append("region_id")

    # Record every migration this tool applied itself so the Go migrator does not
    # replay 0007/0008 over the now-current table.
    report["synced_migrations"] = sync_schema_migrations(connection)

    required = {"camera_id", "direction", "region_id", "pcu_total", "saturation_index", "severity", "confidence", "source", "observed_at", "recorded_at"}
    missing = required.difference(columns)
    if missing:
        raise sqlite3.DatabaseError(f"{TABLE_NAME} is missing required columns: {sorted(missing)}")

    existing_indexes = {
        str(row[1]): bool(row[2])
        for row in connection.execute(f"PRAGMA index_list({TABLE_NAME})").fetchall()
    }

    def _index_columns(name: str) -> List[str]:
        return [str(row[2]) for row in connection.execute(f"PRAGMA index_info({name})").fetchall()]

    legacy_unique = "uq_hotset_verdict_history_camera_observed"
    if legacy_unique in existing_indexes:
        connection.execute(f"DROP INDEX {legacy_unique}")
        report["dropped_indexes"].append(legacy_unique)
        existing_indexes.pop(legacy_unique, None)

    index_specs = (
        (
            "uq_hotset_verdict_history_cam_dir_observed",
            True,
            ("camera_id", "direction", "observed_at"),
        ),
        (
            "idx_hotset_verdict_history_observed",
            False,
            ("observed_at",),
        ),
        (
            "idx_hotset_verdict_history_camera_recorded",
            False,
            ("camera_id", "recorded_at"),
        ),
        (
            "idx_hotset_verdict_history_dir_observed",
            False,
            ("direction", "observed_at"),
        ),
    )
    for index_name, unique, columns_in_index in index_specs:
        expected = list(columns_in_index)
        if index_name in existing_indexes and _index_columns(index_name) != expected:
            connection.execute(f"DROP INDEX {index_name}")
            report["dropped_indexes"].append(index_name)
            existing_indexes.pop(index_name, None)
        if index_name not in existing_indexes:
            uniqueness = "UNIQUE " if unique else ""
            column_list = ", ".join(columns_in_index)
            connection.execute(
                f"CREATE {uniqueness}INDEX IF NOT EXISTS {index_name} "
                f"ON {TABLE_NAME}({column_list})"
            )
            report["created_indexes"].append(index_name)

    connection.commit()
    return report


def connect_database(path: Path, read_only: bool = False) -> sqlite3.Connection:
    """Open the observation database with WAL journaling and a bounded busy timeout."""
    if read_only:
        if not path.is_file():
            raise FileNotFoundError(f"SQLite database not found: {path}")
        connection = sqlite3.connect(f"file:{path}?mode=ro", uri=True, timeout=10.0)
    else:
        path.parent.mkdir(parents=True, exist_ok=True)
        connection = sqlite3.connect(str(path), timeout=10.0)
    configure_connection(connection, read_only=read_only)
    return connection


INSERT_SQL = f"""
INSERT OR IGNORE INTO {TABLE_NAME}
    (camera_id, direction, region_id, pcu_total, saturation_index, severity, confidence, source, observed_at, recorded_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
"""


def insert_verdict_batch(
    connection: sqlite3.Connection,
    records: Sequence[VerdictRecord],
    batch_size: int = DEFAULT_BATCH_SIZE,
) -> Tuple[int, int]:
    """Insert records in bounded transactions. Returns (attempted, inserted)."""
    if not records:
        return 0, 0
    size = max(1, int(batch_size))
    attempted = 0
    inserted = 0
    for start in range(0, len(records), size):
        chunk = records[start : start + size]
        # recorded_at must match Go's TimestampFixedLayout, so it is written
        # explicitly instead of relying on SQLite's CURRENT_TIMESTAMP layout.
        recorded_at = utc_now_timestamp()
        rows = [record.as_db_row() + (recorded_at,) for record in chunk]
        before = connection.total_changes
        with connection:
            connection.executemany(INSERT_SQL, rows)
        inserted += connection.total_changes - before
        attempted += len(rows)
    return attempted, inserted


# ---------------------------------------------------------------------------
# Verification and manifest helpers
# ---------------------------------------------------------------------------


def load_existing_keys(connection: sqlite3.Connection) -> set[Tuple[str, str, str]]:
    """Load existing (camera, direction, observed_at) keys from history."""
    rows = connection.execute(
        f"SELECT camera_id, direction, observed_at FROM {TABLE_NAME}"
    ).fetchall()
    keys: set[Tuple[str, str, str]] = set()
    for camera_id, direction, observed_at in rows:
        keys.add((str(camera_id), str(direction or ""), normalize_timestamp(observed_at)))
    return keys


def verify_records(
    connection: sqlite3.Connection,
    records: Sequence[VerdictRecord],
) -> Dict[str, Any]:
    """Compare generated records with persisted rows without writing anything."""
    table_row = connection.execute(
        "SELECT name FROM sqlite_master WHERE type='table' AND name=?",
        (TABLE_NAME,),
    ).fetchone()
    if table_row is None:
        return {
            "table_present": False,
            "expected_records": len(records),
            "present_records": 0,
            "missing_records": len(records),
            "missing_samples": [],
        }

    try:
        existing = load_existing_keys(connection)
    except sqlite3.OperationalError as exc:
        return {
            "table_present": False,
            "expected_records": len(records),
            "present_records": 0,
            "missing_records": len(records),
            "missing_samples": [],
            "error": str(exc),
        }
    missing: List[Tuple[str, str, str]] = []
    present = 0
    for record in records:
        key = (record.camera_id, record.direction, normalize_timestamp(record.observed_at))
        if key in existing:
            present += 1
        else:
            missing.append(key)
    return {
        "table_present": True,
        "expected_records": len(records),
        "present_records": present,
        "missing_records": len(missing),
        "missing_samples": [list(key) for key in missing[:20]],
    }


def write_json_atomic(path: Path, payload: Mapping[str, Any]) -> None:
    """Write JSON through a temporary file so readers never observe a partial file."""
    path.parent.mkdir(parents=True, exist_ok=True)
    temporary = path.with_name(path.name + ".tmp")
    with temporary.open("w", encoding="utf-8") as handle:
        json.dump(payload, handle, indent=2, sort_keys=False)
        handle.write("\n")
    os.replace(temporary, path)


def _resolve_labels_dir(explicit: Optional[Path], archive_dir: Path) -> Optional[Path]:
    if explicit is not None:
        return explicit if explicit.is_dir() else None
    candidate = archive_dir.parent / "labels" / archive_dir.name
    if candidate.is_dir():
        return candidate
    candidate = archive_dir / "labels" / archive_dir.name
    if candidate.is_dir():
        return candidate
    return None


def _relative_path(path: Path, root: Path) -> str:
    try:
        return str(path.relative_to(root))
    except ValueError:
        return str(path)


# ---------------------------------------------------------------------------
# Ingestion pipeline
# ---------------------------------------------------------------------------


def run_ingest(args: argparse.Namespace) -> int:
    """Execute archive validation, analysis, persistence, and manifest emission."""
    archive_dir = Path(args.archive_dir).resolve()
    db_path = Path(args.db_path).resolve()
    manifest_path = Path(args.manifest_out).resolve()
    labels_dir = _resolve_labels_dir(Path(args.labels_dir).resolve() if args.labels_dir else None, archive_dir)
    regions_dir = Path(args.roadway_regions_dir).resolve()
    cameras_file = Path(args.cameras_file).resolve()

    if args.verify and args.dry_run:
        LOG.error("--verify and --dry-run are mutually exclusive")
        return 2

    frames, unrecognized = discover_frames(archive_dir)
    if args.limit is not None:
        frames = frames[: max(0, int(args.limit))]

    capacities = load_camera_capacities(cameras_file)
    cv_client = CVEngineClient(args.cv_engine_url, timeout=args.cv_timeout) if args.cv_engine_url else None

    connection: Optional[sqlite3.Connection] = None
    schema_report: Dict[str, Any] = {}
    if args.verify:
        connection = connect_database(db_path, read_only=True)
    elif not args.dry_run:
        connection = connect_database(db_path, read_only=False)
        schema_report = ensure_history_schema(connection)
        LOG.info(
            "Schema ready: created_table=%s added_columns=%s created_indexes=%d",
            schema_report["created_table"],
            schema_report["added_columns"],
            len(schema_report["created_indexes"]),
        )

    frame_index: List[Dict[str, Any]] = []
    all_records: List[VerdictRecord] = []
    buffered_records: List[VerdictRecord] = []
    attempted_records = 0
    inserted_records = 0
    valid_frames = 0
    invalid_frames = 0
    analyzed_frames = 0
    analysis_errors = 0
    method_counts: Counter[str] = Counter()
    invalid_reasons: Counter[str] = Counter()
    valid_cameras: set[str] = set()
    timestamp_values: List[int] = []

    try:
        for position, frame in enumerate(frames, start=1):
            observed_at = safe_observed_at(frame.timestamp)
            if observed_at is None:
                invalid_frames += 1
                invalid_reasons["timestamp_out_of_range"] += 1
                frame_index.append(
                    {
                        "camera_id": frame.camera_id,
                        "timestamp": frame.timestamp,
                        "observed_at": "",
                        "event": frame.event,
                        "path": _relative_path(frame.path, archive_dir),
                        "valid": False,
                        "size_bytes": 0,
                        "sha256": "",
                        "method": "",
                        "verdicts": [],
                        "reason": "timestamp_out_of_range",
                    }
                )
                continue

            entry: Dict[str, Any] = {
                "camera_id": frame.camera_id,
                "timestamp": frame.timestamp,
                "observed_at": observed_at,
                "event": frame.event,
                "path": _relative_path(frame.path, archive_dir),
                "valid": False,
                "size_bytes": 0,
                "sha256": "",
                "method": "",
                "verdicts": [],
            }

            try:
                image_bytes = frame.path.read_bytes()
            except OSError as exc:
                invalid_frames += 1
                invalid_reasons["read_error"] += 1
                entry["reason"] = f"read_error: {exc}"
                frame_index.append(entry)
                continue

            entry["size_bytes"] = len(image_bytes)
            entry["sha256"] = hashlib.sha256(image_bytes).hexdigest()
            valid, reason = validate_jpeg_bytes(image_bytes)
            if not valid:
                invalid_frames += 1
                invalid_reasons[reason] += 1
                entry["reason"] = reason
                frame_index.append(entry)
                continue

            valid_frames += 1
            valid_cameras.add(frame.camera_id)
            timestamp_values.append(frame.timestamp)
            entry["valid"] = True

            regions = load_roadway_regions(frame.camera_id, regions_dir)
            label_path = find_label_path(labels_dir, archive_dir, frame.camera_id, frame.timestamp)
            label_payload = load_label_payload(label_path)
            base_capacity = capacities.get(frame.camera_id, CAPACITY_FLOOR)

            try:
                records, method = analyze_frame(
                    image_bytes=image_bytes,
                    camera_id=frame.camera_id,
                    base_capacity=base_capacity,
                    regions=regions,
                    observed_at=observed_at,
                    cv_client=cv_client,
                    label_payload=label_payload,
                    source="proof_archive",
                )
            except Exception as exc:  # Defensive boundary around per-frame analysis.
                LOG.exception("Analysis failed for %s", frame.path)
                analysis_errors += 1
                entry["reason"] = f"analysis_error: {exc}"
                frame_index.append(entry)
                continue

            analyzed_frames += 1
            method_counts[method] += 1
            all_records.extend(records)
            buffered_records.extend(records)
            entry["method"] = method
            entry["verdicts"] = [record.as_manifest() for record in records]
            frame_index.append(entry)

            if connection is not None and not args.verify and not args.dry_run:
                if len(buffered_records) >= max(1, int(args.batch_size)):
                    attempted, inserted = insert_verdict_batch(connection, buffered_records, args.batch_size)
                    attempted_records += attempted
                    inserted_records += inserted
                    buffered_records.clear()

            if position % 500 == 0:
                LOG.info(
                    "Processed %d/%d frames (valid=%d invalid=%d records=%d)",
                    position,
                    len(frames),
                    valid_frames,
                    invalid_frames,
                    len(all_records),
                )

        if connection is not None and not args.verify and not args.dry_run and buffered_records:
            attempted, inserted = insert_verdict_batch(connection, buffered_records, args.batch_size)
            attempted_records += attempted
            inserted_records += inserted
            buffered_records.clear()

        verification: Optional[Dict[str, Any]] = None
        if args.verify and connection is not None:
            verification = verify_records(connection, all_records)
            LOG.info(
                "Verification: expected=%d present=%d missing=%d",
                verification["expected_records"],
                verification["present_records"],
                verification["missing_records"],
            )
    finally:
        if connection is not None:
            connection.close()

    timestamp_range: Dict[str, Any] = {}
    if timestamp_values:
        earliest = min(timestamp_values)
        latest = max(timestamp_values)
        timestamp_range = {
            "start_unix": earliest,
            "end_unix": latest,
            "start": to_rfc3339nano(earliest),
            "end": to_rfc3339nano(latest),
        }

    manifest: Dict[str, Any] = {
        "schema_version": 1,
        "provenance": "replayed",
        "source": "proof_archive",
        "generated_at": format_timestamp(datetime.now(timezone.utc)),
        "archive_dir": str(archive_dir),
        "labels_dir": str(labels_dir) if labels_dir else "",
        "roadway_regions_dir": str(regions_dir),
        "db_path": str(db_path),
        "mode": "verify" if args.verify else ("dry_run" if args.dry_run else "ingest"),
        "total_frames": len(frames),
        "valid_frames": valid_frames,
        "invalid_frames": invalid_frames,
        "analysis_errors": analysis_errors,
        "analyzed_frames": analyzed_frames,
        "unique_cameras": len(valid_cameras),
        "camera_ids": sorted(valid_cameras),
        "timestamp_range": timestamp_range,
        "records_generated": len(all_records),
        "records_attempted": attempted_records,
        "records_inserted": inserted_records,
        "records_skipped_existing": max(0, attempted_records - inserted_records),
        "directional_records": sum(1 for record in all_records if record.direction),
        "method_counts": dict(sorted(method_counts.items())),
        "invalid_reasons": dict(sorted(invalid_reasons.items())),
        "unrecognized_files": unrecognized[:100],
        "unrecognized_file_count": len(unrecognized),
        "schema_migration": schema_report,
        "verification": verification,
        "frame_index": frame_index,
    }
    write_json_atomic(manifest_path, manifest)

    LOG.info(
        "Manifest written to %s (frames=%d valid=%d invalid=%d records=%d inserted=%d)",
        manifest_path,
        len(frames),
        valid_frames,
        invalid_frames,
        len(all_records),
        inserted_records,
    )

    if args.verify:
        if verification is None or not verification.get("table_present", False):
            LOG.error("Verification failed: %s table is not present", TABLE_NAME)
            return 1
        if verification.get("missing_records", 0) > 0:
            LOG.error("Verification failed: %d records are missing", verification["missing_records"])
            return 1
        LOG.info("Verification passed: all generated records are present")
        return 0

    if valid_frames == 0:
        LOG.error("No valid frames were discovered in %s", archive_dir)
        return 1
    return 0


def build_parser() -> argparse.ArgumentParser:
    """Construct the CLI parser for the proof archive ingester."""
    parser = argparse.ArgumentParser(
        description="Replay proof-archive frames into hotset_verdict_history.",
    )
    parser.add_argument(
        "--archive-dir",
        default=str(DEFAULT_ARCHIVE_DIR),
        help="Proof archive root containing <camera_id>/<timestamp>_<event>.jpg files.",
    )
    parser.add_argument(
        "--db-path",
        default=str(DEFAULT_DB_PATH),
        help="SQLite database path (default: services/go-server/data/camera-view.db).",
    )
    parser.add_argument(
        "--manifest-out",
        default=str(DEFAULT_MANIFEST_PATH),
        help="Replay manifest output path (default: data/proof_archive_manifest.json).",
    )
    parser.add_argument("--limit", type=int, default=None, help="Maximum number of frames to process.")
    parser.add_argument(
        "--batch-size",
        type=int,
        default=DEFAULT_BATCH_SIZE,
        help="SQLite insert batch size (default: 500).",
    )
    parser.add_argument("--verify", action="store_true", help="Verify existing rows without modifying the database.")
    parser.add_argument("--dry-run", action="store_true", help="Parse and validate frames without writing to SQLite.")
    parser.add_argument(
        "--cv-engine-url",
        default=os.environ.get("CV_ENGINE_URL", DEFAULT_CV_ENGINE_URL),
        help="CV engine base URL. Set an empty string to force the deterministic fallback.",
    )
    parser.add_argument("--cv-timeout", type=float, default=10.0, help="CV engine request timeout in seconds.")
    parser.add_argument(
        "--labels-dir",
        default=None,
        help="Optional Draft-07 label directory. Auto-detected under the proof archive when omitted.",
    )
    parser.add_argument(
        "--roadway-regions-dir",
        default=str(DEFAULT_ROADWAY_REGIONS_DIR),
        help="Directional roadway polygon directory.",
    )
    parser.add_argument(
        "--cameras-file",
        default=str(DEFAULT_CAMERAS_FILE),
        help="Camera catalog used for per-camera base capacity.",
    )
    return parser


def main(argv: Optional[Sequence[str]] = None) -> int:
    """CLI entry point."""
    load_dotenv()
    parser = build_parser()
    args = parser.parse_args(argv)
    if args.batch_size <= 0:
        parser.error("--batch-size must be greater than zero")
    if args.limit is not None and args.limit < 0:
        parser.error("--limit must be zero or greater")

    logging.basicConfig(
        level=os.environ.get("LOG_LEVEL", "INFO").upper(),
        format="%(asctime)s [%(levelname)s] %(message)s",
        datefmt="%Y-%m-%d %H:%M:%S",
    )
    try:
        return run_ingest(args)
    except (FileNotFoundError, sqlite3.Error, OSError) as exc:
        LOG.error("Ingestion failed: %s", exc)
        return 1


if __name__ == "__main__":
    sys.exit(main())
