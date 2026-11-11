#!/usr/bin/env python3
"""
Motorbike flood stall hazard classifier (Issue #401, Sprint 12 Task 3).

Normative source: ``docs/sprints/s12_t3_spec.md`` §2 (decisions), §5 (wire
contracts) and §7 (edge cases).

Why this module exists
----------------------
``server.py`` infers a flood verdict from an abstract area ratio and then, at the
``motorcycles_observed == 0 and water_ratio >= 0.35`` branch, converts evidence of
hazard into a clearance: a rider who stops *upstream* of a large pool behaves
exactly like a rider facing an impassable one. The classifier replaces that
static fallback with an explicit inference about *why* the lane is empty, and
publishes a physical depth in centimetres when — and only when — the frame
contains a reference that can measure one.

Integration contract with ``server.py`` (what the implementation provides)
--------------------------------------------------------------------------
1. ``triangulate_flood_verdict`` keeps its existing positional arguments and
   gains three keyword-only arguments::

       def triangulate_flood_verdict(water_ratio, max_delta_sub, depth_estimated_cm,
                                     motorcycles_observed=0, rain_event=False, *,
                                     pool_area_px=0.0, observations=(),
                                     pool_top_y=None) -> Tuple[str, float]

   The static fallback branch (``motorcycles_observed == 0 and
   water_ratio >= 0.35``) is deleted. When :func:`should_escalate` is true the
   verdict is ``FLOODED_DEEP`` with confidence capped at
   :data:`AVOIDANCE_CONFIDENCE_CAP`. Every legacy branch keeps its current
   behaviour when the new signals are absent.

2. ``build_flood_assessment`` gains one keyword-only argument and four keys::

       def build_flood_assessment(..., *, hazard: Optional[HazardAssessment] = None)

   The block always carries ``hazard_level``, ``hazard_basis``,
   ``depth_confidence`` and ``calibration_ref`` (defaults ``None``/``NONE``/
   ``UNMEASURED``/``NONE`` when ``hazard`` is ``None``); the seven legacy keys
   keep their names, types and meaning.

3. The /predict handler writes ``flood_depth_cm``, ``flood_hazard_level``,
   ``flood_hazard_basis``, ``flood_depth_confidence`` and ``calibration_ref`` at
   the top level from the same ``HazardAssessment``, and serialises an
   unmeasured depth as ``null`` — never ``0.0``.
"""

from __future__ import annotations

import json
import logging
import math
import threading
import time
from dataclasses import dataclass
from datetime import date
from pathlib import Path
from typing import Any, Dict, List, Optional, Sequence, Tuple

# ---------------------------------------------------------------------------
# §2.1 Reference ladder — constants are exported, never inlined (DoD §8.3).
# ---------------------------------------------------------------------------

#: Grip-to-ground height of the HCMC scooter fleet (Wave, Vision, Lead, SH Mode).
MOTORCYCLE_HEIGHT_CM = 110.0
#: Exhaust outlet height above the road: the stall threshold between L2 and L3.
EXHAUST_HEIGHT_CM = 20.0
#: Published HCMC curb top edge, the primary anchor when no vehicle is in water.
CURB_HEIGHT_CM = 15.0

#: Depth bands from §2.3. ``d < 10`` is Level 1, ``10 <= d < 20`` Level 2,
#: ``d >= 20`` Level 3 — 20.0 is inclusive because the pipe sits at the waterline.
LEVEL_2_MIN_CM = 10.0
LEVEL_3_MIN_CM = 20.0

#: Depths above this are not credible: keep the level, refuse the centimetre figure.
MAX_CREDIBLE_DEPTH_CM = 150.0

#: §2.2 — a multiplier outside ``1.0 ± 0.25`` is rejected; the raw scale is used.
SCALE_MULTIPLIER_TOLERANCE = 0.25
#: §2.2 — warn when an operator calibration has not been measured for this long.
CALIBRATION_STALE_DAYS = 180

#: §2.5 — a latch entry older than the verdict TTL is ignored.
LATCH_TTL_SECONDS = 180.0

#: §2.4 thresholds.
POOL_BLOCKS_LANE_MIN_RATIO = 0.30
POOL_BLOCKS_LANE_MIN_AREA_PX = 400.0
THROUGH_TRAFFIC_MIN_DELTA_SUB = 0.09
UPSTREAM_STOP_MAX_DELTA_SUB = 0.05
AVOIDANCE_CONFIDENCE_CAP = 0.78

#: §2.1 "pixels-per-cm resolution" source names (wire: ``calibration_ref``).
CALIBRATION_REF_MOTORCYCLE = "MOTORCYCLE_110CM"
CALIBRATION_REF_CURB = "CURB_15CM"
CALIBRATION_REF_OVERRIDE = "CAMERA_OVERRIDE"
CALIBRATION_REF_NONE = "NONE"
CALIBRATION_REFS: Tuple[str, ...] = (
    CALIBRATION_REF_MOTORCYCLE,
    CALIBRATION_REF_CURB,
    CALIBRATION_REF_OVERRIDE,
    CALIBRATION_REF_NONE,
)

#: §5.1 ``flood_hazard_basis`` vocabulary.
BASIS_DEPTH_MEASURED = "DEPTH_MEASURED"
BASIS_AVOIDANCE_INFERRED = "AVOIDANCE_INFERRED"
BASIS_DEPTH_AND_AVOIDANCE = "DEPTH_AND_AVOIDANCE"
BASIS_NONE = "NONE"
HAZARD_BASES: Tuple[str, ...] = (
    BASIS_DEPTH_MEASURED,
    BASIS_AVOIDANCE_INFERRED,
    BASIS_DEPTH_AND_AVOIDANCE,
    BASIS_NONE,
)

#: §5.1 ``flood_depth_confidence`` vocabulary.
CONFIDENCE_MEASURED = "MEASURED"
CONFIDENCE_INFERRED = "INFERRED"
CONFIDENCE_UNMEASURED = "UNMEASURED"
DEPTH_CONFIDENCES: Tuple[str, ...] = (
    CONFIDENCE_MEASURED,
    CONFIDENCE_INFERRED,
    CONFIDENCE_UNMEASURED,
)

DEFAULT_SCALE_MULTIPLIER = 1.0

#: Default operator calibration file, sibling to this module (§2.2).
DEFAULT_CALIBRATION_PATH = Path(__file__).resolve().parent / "calibration.json"

#: Module logger. ``assess_hazard`` accepts an override parameter named ``logger``,
#: so the module-level instance is kept under an unambiguous name.
DEFAULT_LOGGER = logging.getLogger("cv-engine")


# ---------------------------------------------------------------------------
# Value types
# ---------------------------------------------------------------------------


@dataclass(frozen=True)
class MotorcycleObservation:
    """One detected motorcycle inside the ROI, in target-image pixel space.

    ``box_bottom_y`` is the bottom edge of the box (y grows downwards);
    ``delta_sub`` is the measured wheel-submersion ratio in ``[0, 1]``.
    """

    box_bottom_y: float
    delta_sub: float


@dataclass(frozen=True)
class HazardAssessment:
    """The physical hazard verdict for one frame (§5.1 block + top-level fields).

    ``depth_cm`` is ``None`` whenever the depth could not be measured credibly;
    it is ``0.0`` only for a genuinely measured dry surface. ``level`` is ``None``
    when neither a credible depth nor avoidance evidence exists — never 1.
    """

    level: Optional[int]
    basis: str
    depth_cm: Optional[float]
    depth_confidence: str
    calibration_ref: str


@dataclass(frozen=True)
class CameraCalibration:
    """One validated §2.2 operator calibration entry."""

    camera_id: str
    scale_multiplier: float
    measured_by: str
    measured_at: date
    frame_ref: str
    curb_px: Optional[float] = None
    curb_cm: Optional[float] = None


# ---------------------------------------------------------------------------
# §2.3 Classification
# ---------------------------------------------------------------------------


def level_from_depth(depth_cm: float) -> int:
    """Map a centimetre depth to hazard level 1-3 (inclusive at 20.0).

    Classifies the **unrounded** value: ``19.99 -> 2``, ``20.0 -> 3``.
    """
    if depth_cm < LEVEL_2_MIN_CM:
        return 1
    if depth_cm < LEVEL_3_MIN_CM:
        return 2
    return 3


def level_from_avoidance(
    *,
    water_ratio: float,
    pool_area_px: float,
    motorcycles_observed: int,
    observations: Sequence[MotorcycleObservation] = (),
    pool_top_y: Optional[float] = None,
) -> Optional[int]:
    """Level implied by upstream avoidance, or ``None`` when nothing escalates."""
    if should_escalate(
        water_ratio=water_ratio,
        pool_area_px=pool_area_px,
        motorcycles_observed=motorcycles_observed,
        observations=observations,
        pool_top_y=pool_top_y,
    ):
        return 3
    return None


# ---------------------------------------------------------------------------
# §2.1 Reference ladder / depth measurement
# ---------------------------------------------------------------------------


def resolve_calibration_ref(
    *,
    has_motorcycle_reference: bool,
    has_curb_reference: bool,
    has_camera_override: bool,
) -> str:
    """Pick the single ``calibration_ref`` source for this observation.

    Priority follows §2.1 "first available wins": a per-camera override (which is
    applied *on top of* a base reference) outranks the base source; a motorcycle
    box outranks a curb segment; an override with no base reference is not a
    reference at all and yields ``NONE``.
    """
    if not has_motorcycle_reference and not has_curb_reference:
        # An override scales a measurement; it cannot create one (E1).
        return CALIBRATION_REF_NONE
    if has_camera_override:
        return CALIBRATION_REF_OVERRIDE
    if has_motorcycle_reference:
        return CALIBRATION_REF_MOTORCYCLE
    return CALIBRATION_REF_CURB


def compute_delta_sub(box_bottom_y: float, waterline_y: float, box_h_px: float) -> float:
    """``(box_bottom_y - waterline_y) / box_h_px`` clamped to ``[0, 1]``."""
    if not box_h_px or box_h_px <= 0:
        return 0.0
    ratio = (float(box_bottom_y) - float(waterline_y)) / float(box_h_px)
    if not math.isfinite(ratio):
        return 0.0
    return max(0.0, min(1.0, ratio))


def estimate_depth_cm(delta_sub: float, scale_multiplier: float = DEFAULT_SCALE_MULTIPLIER) -> float:
    """``delta_sub x MOTORCYCLE_HEIGHT_CM x scale_multiplier`` (never negative)."""
    depth = float(delta_sub) * MOTORCYCLE_HEIGHT_CM * float(scale_multiplier)
    if not math.isfinite(depth):
        return 0.0
    return max(0.0, depth)


def estimate_depth_from_curb_cm(
    depth_px: float,
    curb_px: float,
    scale_multiplier: float = DEFAULT_SCALE_MULTIPLIER,
) -> float:
    """Depth in cm from a curb segment of known height crossing the waterline.

    ``px_per_cm = curb_px / 15.0``, so ``depth_px / px_per_cm`` is the depth.
    """
    if not curb_px or curb_px <= 0:
        # No reference segment: nothing can be measured (never a zero depth).
        return 0.0
    px_per_cm = float(curb_px) / CURB_HEIGHT_CM
    depth = (float(depth_px) / px_per_cm) * float(scale_multiplier)
    if not math.isfinite(depth):
        return 0.0
    return max(0.0, depth)


def is_valid_scale_multiplier(scale_multiplier: Any) -> bool:
    """True when the multiplier lies strictly inside ``1.0 ± 0.25`` (§2.2).

    The band is exclusive: ``1.25`` and ``0.75`` are rejected (M8), ``1.08`` and
    ``1.2499`` are accepted.
    """
    if isinstance(scale_multiplier, bool) or not isinstance(scale_multiplier, (int, float)):
        return False
    value = float(scale_multiplier)
    if not math.isfinite(value):
        return False
    spread = DEFAULT_SCALE_MULTIPLIER * SCALE_MULTIPLIER_TOLERANCE
    return (DEFAULT_SCALE_MULTIPLIER - spread) < value < (DEFAULT_SCALE_MULTIPLIER + spread)


# ---------------------------------------------------------------------------
# §2.4 Avoidance inference (the false-FLOODED_SHALLOW fix)
# ---------------------------------------------------------------------------


def has_through_traffic(observations: Sequence[MotorcycleObservation]) -> bool:
    """True when any rider's wheels are visibly in water (``delta_sub >= 0.09``)."""
    return any(
        obs.delta_sub >= THROUGH_TRAFFIC_MIN_DELTA_SUB for obs in observations or ()
    )


def has_upstream_stop(
    observations: Sequence[MotorcycleObservation],
    pool_top_y: Optional[float],
) -> bool:
    """True when a bike stopped *before* the pool (above it, barely submerged)."""
    if pool_top_y is None:
        # Without a pool outline there is no upstream to talk about.
        return False
    return any(
        obs.box_bottom_y < float(pool_top_y)
        and obs.delta_sub <= UPSTREAM_STOP_MAX_DELTA_SUB
        for obs in observations or ()
    )


def should_escalate(
    *,
    water_ratio: float,
    pool_area_px: float,
    motorcycles_observed: int,
    observations: Sequence[MotorcycleObservation] = (),
    pool_top_y: Optional[float] = None,
) -> bool:
    """Apply the §2.4 rule: pool blocks the lane, nobody is driving through it."""
    pool_blocks_lane = (
        water_ratio >= POOL_BLOCKS_LANE_MIN_RATIO
        and pool_area_px >= POOL_BLOCKS_LANE_MIN_AREA_PX
    )
    if not pool_blocks_lane:
        return False
    if has_through_traffic(observations):
        # A rider crossing at rim depth is proof of passability (AC9).
        return False
    return motorcycles_observed == 0 or has_upstream_stop(observations, pool_top_y)


# ---------------------------------------------------------------------------
# Composition
# ---------------------------------------------------------------------------


def assess_hazard(
    *,
    depth_cm: Optional[float] = None,
    depth_inferred: bool = False,
    calibration_ref: str = CALIBRATION_REF_NONE,
    water_ratio: float = 0.0,
    pool_area_px: float = 0.0,
    motorcycles_observed: int = 0,
    observations: Sequence[MotorcycleObservation] = (),
    pool_top_y: Optional[float] = None,
    logger: Optional[logging.Logger] = None,
) -> HazardAssessment:
    """Compose the depth and avoidance signals into one assessment (§2.4 last ¶).

    ``final_level = max(level_from_depth, level_from_avoidance)``; when both are
    absent the level is ``None`` with ``basis="NONE"`` — absence of measurement is
    not safety. A depth above :data:`MAX_CREDIBLE_DEPTH_CM` keeps Level 3, reports
    ``UNMEASURED`` and drops the centimetre figure (E8), logging a warning.
    """
    log = logger or DEFAULT_LOGGER
    ref = calibration_ref if calibration_ref in CALIBRATION_REFS else CALIBRATION_REF_NONE

    depth_level: Optional[int] = None
    measured_depth: Optional[float] = None
    depth_confidence = CONFIDENCE_UNMEASURED

    if depth_cm is not None:
        try:
            depth_value = float(depth_cm)
        except (TypeError, ValueError):
            depth_value = float("nan")
        if not math.isfinite(depth_value) or depth_value < 0.0:
            log.warning("Ignoring a non-physical depth reading (%.3f cm)", depth_value)
        elif depth_value > MAX_CREDIBLE_DEPTH_CM:
            # E8: keep the warning, refuse the figure. A 160 cm pool over a scooter
            # lane is a segmentation artefact, not a measurement.
            log.warning(
                "Implausible flood depth %.1f cm exceeds the credible %.0f cm; "
                "keeping the hazard level and dropping the centimetre figure",
                depth_value,
                MAX_CREDIBLE_DEPTH_CM,
            )
            depth_level = 3
        elif depth_value == 0.0 and water_ratio >= 0.05:
            # A zero submersion measured *inside a flooded ROI* means the reference
            # vehicle is standing outside the water — the upstream-avoidance frame
            # from #401 — not that the road is dry. Reporting 0.0 cm here would be a
            # fabricated measurement of the pool (AC10/E3). A genuinely dry frame
            # (water_ratio < 0.05) keeps its 0.0 as a real measurement.
            pass
        else:
            depth_level = level_from_depth(depth_value)
            measured_depth = depth_value
            depth_confidence = (
                CONFIDENCE_INFERRED if depth_inferred else CONFIDENCE_MEASURED
            )

    avoidance_level = level_from_avoidance(
        water_ratio=water_ratio,
        pool_area_px=pool_area_px,
        motorcycles_observed=motorcycles_observed,
        observations=observations,
        pool_top_y=pool_top_y,
    )

    if depth_level is not None and avoidance_level is not None:
        return HazardAssessment(
            level=max(depth_level, avoidance_level),
            basis=BASIS_DEPTH_AND_AVOIDANCE,
            depth_cm=measured_depth,
            depth_confidence=depth_confidence,
            calibration_ref=ref,
        )
    if depth_level is not None:
        return HazardAssessment(
            level=depth_level,
            basis=BASIS_DEPTH_MEASURED,
            depth_cm=measured_depth,
            depth_confidence=depth_confidence,
            calibration_ref=ref,
        )
    if avoidance_level is not None:
        # AC10: publish the level, never a fabricated centimetre value.
        return HazardAssessment(
            level=avoidance_level,
            basis=BASIS_AVOIDANCE_INFERRED,
            depth_cm=None,
            depth_confidence=CONFIDENCE_UNMEASURED,
            calibration_ref=ref,
        )
    # "Absence of measurement is not safety": no level, no number, no default 1.
    return HazardAssessment(
        level=None,
        basis=BASIS_NONE,
        depth_cm=None,
        depth_confidence=CONFIDENCE_UNMEASURED,
        calibration_ref=ref,
    )


# ---------------------------------------------------------------------------
# §2.5 De-escalation latch — the only stateful part
# ---------------------------------------------------------------------------


class HazardLatch:
    """Hold a de-escalation until two consecutive observations agree.

    Escalation is immediate; a drop needs a second agreeing frame, so one passing
    truck cannot erase the pool for a frame. Entries older than ``ttl_seconds``
    are ignored (verdict TTL) and the map is in-process only — cleared on restart.

    The latch stores the whole :class:`HazardAssessment` it held, never just the
    level: a held Level 3 must keep publishing the ``basis``, ``depth_cm``,
    ``depth_confidence`` and ``calibration_ref`` that justified it. Emitting the
    held level alongside the drop frame's fields would publish a self-
    contradictory block such as "Level 3, DEPTH_MEASURED, 5.0 cm, MEASURED".
    """

    def __init__(
        self,
        ttl_seconds: float = LATCH_TTL_SECONDS,
        clock: Optional[Any] = None,
    ) -> None:
        self.ttl_seconds = ttl_seconds
        self._clock = clock
        # camera_id -> (held assessment, observed_at, pending_lower_level)
        self._state: Dict[str, Tuple[HazardAssessment, float, Optional[int]]] = {}
        # cv-engine serves requests on a thread pool; the latch is shared state.
        self._lock = threading.Lock()

    def _now(self, observed_at: Optional[float]) -> float:
        if observed_at is not None:
            return float(observed_at)
        if self._clock is not None:
            return float(self._clock())
        return time.monotonic()

    def apply(
        self,
        camera_id: str,
        assessment: HazardAssessment,
        observed_at: Optional[float] = None,
    ) -> HazardAssessment:
        """Return the assessment that may be published for this observation."""
        level = assessment.level
        now = self._now(observed_at)
        with self._lock:
            previous = self._state.get(camera_id)
            if previous is None or (now - previous[1]) > self.ttl_seconds:
                # First observation, or the previous one aged out of the TTL.
                self._state[camera_id] = (assessment, now, None)
                return assessment

            held, _, pending = previous
            if level is None:
                # An unmeasured frame neither resets nor releases a live hold: it
                # publishes its own honest "no level" and leaves the hold intact.
                self._state[camera_id] = (held, now, pending)
                return assessment
            if held.level is None or level >= held.level:
                # First real level, an agreeing frame, or an escalation: publish now.
                self._state[camera_id] = (assessment, now, None)
                return assessment
            # A drop needs two consecutive agreeing observations (E15).
            if pending == level:
                self._state[camera_id] = (assessment, now, None)
                return assessment
            # Still holding: publish the held assessment with its own companion
            # fields, and remember this level as the pending release.
            self._state[camera_id] = (held, now, level)
            return held

    def reset(self) -> None:
        """Drop all latched state (restart, camera re-aim, test isolation)."""
        with self._lock:
            self._state.clear()


# ---------------------------------------------------------------------------
# §2.2 Calibration loader
# ---------------------------------------------------------------------------


class CalibrationRegistry:
    """Operator-authored ``calibration.json`` loader (fail-closed, never fatal).

    A malformed file, an entry missing ``measured_by``/``frame_ref``, or a
    ``scale_multiplier`` outside ``1.0 ± 0.25`` is rejected with a logged error and
    the camera falls back to the raw scale. The service must never fail to boot.
    """

    def __init__(
        self,
        path: Optional[Path] = None,
        *,
        logger: Optional[logging.Logger] = None,
        today: Optional[date] = None,
    ) -> None:
        self.path = Path(path) if path is not None else None
        self.logger = logger or logging.getLogger("cv-engine")
        self.today = today
        self._entries: Dict[str, CameraCalibration] = {}
        self._errors: List[str] = []
        self._mtime_ns: Optional[int] = None

    def _today(self) -> date:
        return self.today or date.today()

    @staticmethod
    def _parse_measured_at(raw: Any) -> Optional[date]:
        if isinstance(raw, date):
            return raw
        if not isinstance(raw, str) or not raw.strip():
            return None
        try:
            return date.fromisoformat(raw.strip())
        except ValueError:
            return None

    def _read_mtime_ns(self) -> Optional[int]:
        if self.path is None:
            return None
        try:
            return self.path.stat().st_mtime_ns
        except OSError:
            return None

    def _reject(self, camera_id: str, reason: str) -> None:
        message = f"rejected calibration for camera {camera_id}: {reason}"
        self._errors.append(message)
        self.logger.error("%s", message)

    def _validate_entry(self, camera_id: str, raw: Any) -> Optional[CameraCalibration]:
        if not isinstance(raw, dict):
            self._reject(camera_id, "entry is not an object")
            return None

        multiplier = raw.get("scale_multiplier")
        if not is_valid_scale_multiplier(multiplier):
            self._reject(
                camera_id,
                "scale_multiplier %r is outside 1.0 +/- %.2f"
                % (multiplier, SCALE_MULTIPLIER_TOLERANCE),
            )
            return None

        measured_by = raw.get("measured_by")
        if not isinstance(measured_by, str) or not measured_by.strip():
            self._reject(camera_id, "measured_by is required for an auditable calibration")
            return None

        frame_ref = raw.get("frame_ref")
        if not isinstance(frame_ref, str) or not frame_ref.strip():
            self._reject(camera_id, "frame_ref is required for an auditable calibration")
            return None

        measured_at = self._parse_measured_at(raw.get("measured_at"))
        if measured_at is None:
            self._reject(camera_id, "measured_at is missing or not an ISO date (YYYY-MM-DD)")
            return None

        landmarks = raw.get("landmarks") or {}
        curb_px: Optional[float] = None
        curb_cm: Optional[float] = None
        if isinstance(landmarks, dict):
            raw_curb_px = landmarks.get("curb_px")
            raw_curb_cm = landmarks.get("curb_cm")
            if isinstance(raw_curb_px, (int, float)) and not isinstance(raw_curb_px, bool):
                curb_px = float(raw_curb_px)
            if isinstance(raw_curb_cm, (int, float)) and not isinstance(raw_curb_cm, bool):
                curb_cm = float(raw_curb_cm)

        age_days = (self._today() - measured_at).days
        if age_days > CALIBRATION_STALE_DAYS:
            # A stale measurement is a re-aiming risk, not a reason to stop serving.
            self.logger.warning(
                "calibration for camera %s was measured %d days ago (> %d); "
                "re-measure the frame reference",
                camera_id,
                age_days,
                CALIBRATION_STALE_DAYS,
            )

        return CameraCalibration(
            camera_id=camera_id,
            scale_multiplier=float(multiplier),
            measured_by=measured_by.strip(),
            measured_at=measured_at,
            frame_ref=frame_ref.strip(),
            curb_px=curb_px,
            curb_cm=curb_cm,
        )

    def load(self) -> "CalibrationRegistry":
        """Read and validate the file; always returns ``self``, never raises."""
        self._entries = {}
        self._errors = []
        self._mtime_ns = self._read_mtime_ns()

        if self.path is None:
            return self
        if self._mtime_ns is None:
            # E10: a missing file is not an error, it is "no overrides yet".
            self.logger.info(
                "No calibration file at %s; serving with the raw scale", self.path
            )
            return self

        try:
            document = json.loads(self.path.read_text(encoding="utf-8"))
        except (OSError, ValueError) as exc:
            self._errors.append(f"cannot read calibration file {self.path}: {exc}")
            self.logger.error("Cannot read calibration file %s: %s", self.path, exc)
            return self

        if not isinstance(document, dict) or not isinstance(document.get("cameras"), dict):
            self._errors.append(
                f"calibration file {self.path} must be an object with a 'cameras' object"
            )
            self.logger.error(
                "Calibration file %s is malformed: expected {\"cameras\": {...}}", self.path
            )
            return self

        for camera_id, raw_entry in document["cameras"].items():
            if not isinstance(camera_id, str) or not camera_id.strip():
                self._errors.append("calibration entry has an empty camera id")
                self.logger.error("Calibration entry has an empty camera id; ignored")
                continue
            entry = self._validate_entry(camera_id, raw_entry)
            if entry is not None:
                self._entries[camera_id] = entry

        if self._entries:
            self.logger.info(
                "Loaded %d camera calibration override(s) from %s",
                len(self._entries),
                self.path,
            )
        return self

    def reload_if_changed(self) -> bool:
        """Re-validate when the file's ``mtime`` moved; True when it reloaded."""
        current = self._read_mtime_ns()
        if current == self._mtime_ns:
            return False
        if current is None and self._mtime_ns is None:
            # Nothing was loaded and there is still nothing to load.
            return False
        # A new mtime, or the file was removed: either way the validated view must
        # follow the filesystem, so deleting calibration.json revokes its overrides.
        self.load()
        return True

    def multiplier_for(self, camera_id: str) -> float:
        """Validated multiplier for ``camera_id``, or ``1.0`` (raw scale)."""
        entry = self._entries.get(camera_id)
        if entry is None:
            return DEFAULT_SCALE_MULTIPLIER
        return entry.scale_multiplier

    def entry_for(self, camera_id: str) -> Optional[CameraCalibration]:
        """Validated entry for ``camera_id``, or ``None``."""
        return self._entries.get(camera_id)

    @property
    def errors(self) -> Tuple[str, ...]:
        """Human-readable rejection reasons from the last load, for logging/tests."""
        return tuple(self._errors)
