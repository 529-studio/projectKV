#!/usr/bin/env python3
"""
Unit tests for the motorbike flood stall hazard classifier (Sprint 12 Task 3, Issue #401).

Contract under test: ``docs/sprints/s12_t3_spec.md``
  * §2.1 reference ladder / pixels-per-cm resolution
  * §2.2 per-camera calibration (``calibration.json``)
  * §2.3 inclusive-at-20.0 classification
  * §2.4 avoidance escalation — the false ``FLOODED_SHALLOW`` fix
  * §2.5 de-escalation latch
  * §5.1 ``POST /predict`` wire contract
  * §7 edge cases E1-E8, E10, E15, E16

These tests are written against the specification, not against an implementation.
``hazard_classifier.py`` currently ships as a skeleton whose functions raise
``NotImplementedError``, so a red run here is the expected TDD state — the
assertions already pin the behaviour the implementation must satisfy. The one
test in ``test_server.py`` that has been rewritten (``test_triangulate_flood_verdict_decision_matrix``
case 3) pins the same regression from the verdict side (AC8).

Run from this directory:  ``python3 -m unittest test_hazard_classifier -v``
Run from the repo root:   ``python3 -m unittest discover -s services/cv-engine -p 'test_*.py'``
"""

from __future__ import annotations

import json
import logging
import os
import sys
import tempfile
import unittest
from datetime import date
from pathlib import Path

# Support `python3 -m unittest services/cv-engine/test_hazard_classifier.py` from the
# repository root: this module imports its siblings as top-level modules, which only
# works when the service directory is on sys.path. Discovery from inside the service
# directory already satisfies that, so the insert is a no-op there.
SERVICE_DIR = str(Path(__file__).resolve().parent)
if SERVICE_DIR not in sys.path:
    sys.path.insert(0, SERVICE_DIR)

import hazard_classifier as hc  # noqa: E402
from server import build_flood_assessment, triangulate_flood_verdict  # noqa: E402


def bike(box_bottom_y: float = 100.0, delta_sub: float = 0.0) -> "hc.MotorcycleObservation":
    """Shorthand for one detected motorcycle observation inside the ROI."""
    return hc.MotorcycleObservation(box_bottom_y=box_bottom_y, delta_sub=delta_sub)


def write_calibration(path: Path, payload: dict) -> None:
    path.write_text(json.dumps(payload), encoding="utf-8")


def valid_entry(**overrides) -> dict:
    entry = {
        "scale_multiplier": 1.08,
        "measured_by": "operator-01",
        "measured_at": "2026-11-14",
        "frame_ref": "docs/proofs/s12_t3/calib_cam_dbp_01.jpg",
    }
    entry.update(overrides)
    return entry


class TestLevelFromDepth(unittest.TestCase):
    """§2.3 — ``LEVEL_1_SPLASH < 10``, ``LEVEL_2_WARNING 10-20``, ``LEVEL_3_STALL_RISK >= 20``."""

    def test_classification_matrix(self):
        # The exact table demanded by §8.1.
        cases = [
            (0.0, 1),
            (9.99, 1),
            (10.0, 2),
            (19.99, 2),
            (20.0, 3),
            (20.01, 3),
            (150.0, 3),
        ]
        for depth, expected in cases:
            with self.subTest(depth=depth):
                self.assertEqual(hc.level_from_depth(depth), expected)

    def test_twenty_centimetres_is_inclusive_into_level_3(self):
        # AC3/E2: at exactly 20 cm the exhaust pipe sits at the waterline, so any
        # ripple reaches it. 20.0 must never classify as Level 2.
        self.assertEqual(hc.level_from_depth(hc.EXHAUST_HEIGHT_CM), 3)
        self.assertEqual(hc.level_from_depth(19.999999), 2)

    def test_ten_centimetres_is_inclusive_into_level_2(self):
        self.assertEqual(hc.level_from_depth(hc.LEVEL_2_MIN_CM), 2)
        self.assertEqual(hc.level_from_depth(9.999999), 1)

    def test_classification_never_rounds_first(self):
        # E16: rounding to one decimal before classifying would turn 19.96 into
        # 20.0 and invent a Level 3 stall risk.
        self.assertEqual(hc.level_from_depth(9.949), 1)
        self.assertEqual(hc.level_from_depth(19.949), 2)
        self.assertEqual(hc.level_from_depth(20.049), 3)

    def test_dry_road_is_level_1(self):
        # E3: a measured dry surface is Level 1 with depth 0.0, not "unmeasured".
        self.assertEqual(hc.level_from_depth(0.0), 1)

    def test_reference_constants_are_exported_not_inlined(self):
        # DoD §8.3: constants exported. The ladder is the physical meaning of the bands.
        self.assertEqual(hc.MOTORCYCLE_HEIGHT_CM, 110.0)
        self.assertEqual(hc.EXHAUST_HEIGHT_CM, 20.0)
        self.assertEqual(hc.CURB_HEIGHT_CM, 15.0)
        self.assertEqual(hc.LEVEL_2_MIN_CM, 10.0)
        self.assertEqual(hc.LEVEL_3_MIN_CM, 20.0)


class TestReferenceLadder(unittest.TestCase):
    """§2.1 — depth is measured against the ladder, never inferred from ``water_ratio``."""

    def test_motorcycle_110cm_reference_is_the_scale_source(self):
        # A 220 px box whose bottom sits 110 px below the waterline: delta_sub = 0.5,
        # so the water reaches half the 110 cm motorcycle -> 55 cm.
        delta_sub = hc.compute_delta_sub(box_bottom_y=400.0, waterline_y=290.0, box_h_px=220.0)
        self.assertAlmostEqual(delta_sub, 0.5, places=6)
        self.assertAlmostEqual(hc.estimate_depth_cm(delta_sub), 55.0, places=6)

    def test_delta_sub_is_clamped_to_the_unit_interval(self):
        # A waterline below the box bottom means no submersion at all, not negative depth.
        self.assertEqual(
            hc.compute_delta_sub(box_bottom_y=100.0, waterline_y=400.0, box_h_px=200.0), 0.0
        )
        # A waterline above the box top means the whole bike is under water.
        self.assertEqual(
            hc.compute_delta_sub(box_bottom_y=400.0, waterline_y=0.0, box_h_px=200.0), 1.0
        )

    def test_curb_15cm_reference(self):
        # 90 px of curb = 15 cm, so 6 px/cm. 90 px of water over the road is 15 cm and
        # 120 px is 20 cm — the stall threshold measured with no vehicle in frame.
        self.assertAlmostEqual(hc.estimate_depth_from_curb_cm(depth_px=90.0, curb_px=90.0), 15.0)
        self.assertAlmostEqual(hc.estimate_depth_from_curb_cm(depth_px=120.0, curb_px=90.0), 20.0)

    def test_camera_override_multiplier_is_hand_computed(self):
        # §2.2: depth_cm = delta_sub x 110 x multiplier. Hand-computed reference:
        # 0.5 x 110 x 1.08 = 59.4 cm, and 15 cm behind a 15 cm curb becomes 16.2 cm.
        self.assertAlmostEqual(hc.estimate_depth_cm(0.5, scale_multiplier=1.08), 59.4, places=6)
        self.assertAlmostEqual(
            hc.estimate_depth_from_curb_cm(depth_px=90.0, curb_px=90.0, scale_multiplier=1.08),
            16.2,
            places=6,
        )

    def test_zero_multiplier_is_identity(self):
        self.assertAlmostEqual(hc.estimate_depth_cm(0.25, scale_multiplier=1.0), 27.5, places=6)

    def test_measured_dry_surface_estimates_zero_not_none(self):
        # E3: a measured 0.0 must stay a number so the UI can print "0 cm".
        self.assertEqual(hc.estimate_depth_cm(0.0), 0.0)

    def test_calibration_ref_resolution_priority(self):
        # §2.1 "first available wins", read together with item 3: CAMERA_OVERRIDE is
        # "the §2.2 multiplier applied on top of (1) or (2)". It is therefore a
        # modifier, not a third base source, so it is reported whenever a multiplier
        # actually scaled the number — otherwise CAMERA_OVERRIDE could only ever
        # appear with no scale to apply, which contradicts the ladder.
        self.assertEqual(
            hc.resolve_calibration_ref(
                has_motorcycle_reference=True,
                has_curb_reference=False,
                has_camera_override=False,
            ),
            hc.CALIBRATION_REF_MOTORCYCLE,
        )
        self.assertEqual(
            hc.resolve_calibration_ref(
                has_motorcycle_reference=False,
                has_curb_reference=True,
                has_camera_override=False,
            ),
            hc.CALIBRATION_REF_CURB,
        )
        self.assertEqual(
            hc.resolve_calibration_ref(
                has_motorcycle_reference=True,
                has_curb_reference=True,
                has_camera_override=False,
            ),
            hc.CALIBRATION_REF_MOTORCYCLE,
        )
        self.assertEqual(
            hc.resolve_calibration_ref(
                has_motorcycle_reference=False,
                has_curb_reference=True,
                has_camera_override=True,
            ),
            hc.CALIBRATION_REF_OVERRIDE,
        )

    def test_no_reference_source_is_none_never_zero_depth(self):
        # E1: an override with no base reference is not a reference. Without one,
        # depth is not estimated at all.
        self.assertEqual(
            hc.resolve_calibration_ref(
                has_motorcycle_reference=False,
                has_curb_reference=False,
                has_camera_override=True,
            ),
            hc.CALIBRATION_REF_NONE,
        )
        self.assertEqual(
            hc.resolve_calibration_ref(
                has_motorcycle_reference=False,
                has_curb_reference=False,
                has_camera_override=False,
            ),
            hc.CALIBRATION_REF_NONE,
        )

    def test_multiplier_band_is_strictly_inside_one_plus_minus_a_quarter(self):
        # §8.1 pins 1.08 accepted and 1.25 rejected, so the upper bound is exclusive;
        # the lower bound is symmetric (a 0.75 multiplier would understate depth by a
        # quarter, the same error in the other direction).
        for multiplier in (1.08, 1.0, 0.7501, 1.2499):
            with self.subTest(multiplier=multiplier):
                self.assertTrue(hc.is_valid_scale_multiplier(multiplier))
        for multiplier in (1.25, 0.75, 0.5, 1.4, 0.0, -1.2, float("nan"), "1.08", None):
            with self.subTest(multiplier=multiplier):
                self.assertFalse(hc.is_valid_scale_multiplier(multiplier))


class TestAvoidanceSignals(unittest.TestCase):
    """§2.4 — the three primitives that decide why the lane is empty."""

    def test_through_traffic_needs_wheels_visibly_in_water(self):
        self.assertTrue(hc.has_through_traffic([bike(delta_sub=0.12)]))
        self.assertTrue(hc.has_through_traffic([bike(delta_sub=0.09)]))  # inclusive
        self.assertFalse(hc.has_through_traffic([bike(delta_sub=0.089)]))
        self.assertFalse(hc.has_through_traffic([]))

    def test_upstream_stop_is_a_bike_above_the_pool_that_barely_touches_it(self):
        stopped = bike(box_bottom_y=100.0, delta_sub=0.02)
        self.assertTrue(hc.has_upstream_stop([stopped], pool_top_y=200.0))
        # Slightly deeper than a stop-and-wait posture is not an upstream stop.
        self.assertFalse(hc.has_upstream_stop([bike(box_bottom_y=100.0, delta_sub=0.06)], 200.0))
        # Below the pool top the bike is already inside the water, not stopping before it.
        self.assertFalse(hc.has_upstream_stop([stopped], pool_top_y=50.0))
        # Without a pool outline there is no upstream to talk about.
        self.assertFalse(hc.has_upstream_stop([stopped], pool_top_y=None))
        self.assertFalse(hc.has_upstream_stop([], pool_top_y=200.0))

    def test_escalation_requires_a_pool_that_blocks_the_lane(self):
        # Inclusive thresholds: 0.30 and 400 px.
        self.assertTrue(
            hc.should_escalate(
                water_ratio=0.30, pool_area_px=400.0, motorcycles_observed=0
            )
        )
        self.assertFalse(
            hc.should_escalate(
                water_ratio=0.2999, pool_area_px=5200.0, motorcycles_observed=0
            )
        )

    def test_the_area_floor_rejects_wet_asphalt_reflections(self):
        # E6: a 399 px patch is a reflection the segmentation model misreads as water.
        self.assertFalse(
            hc.should_escalate(water_ratio=0.45, pool_area_px=399.0, motorcycles_observed=0)
        )

    def test_empty_road_escalates_because_a_blocked_lane_is_a_hazard(self):
        # E7: the reason for the emptiness does not change the danger.
        self.assertTrue(
            hc.should_escalate(water_ratio=0.34, pool_area_px=5200.0, motorcycles_observed=0)
        )

    def test_upstream_stop_escalates(self):
        self.assertTrue(
            hc.should_escalate(
                water_ratio=0.32,
                pool_area_px=1200.0,
                motorcycles_observed=3,
                observations=[bike(box_bottom_y=120.0, delta_sub=0.02)],
                pool_top_y=220.0,
            )
        )

    def test_through_traffic_suppresses_escalation(self):
        # E5/AC9: a rider crossing at rim depth is proof of passability at Level 2.
        self.assertFalse(
            hc.should_escalate(
                water_ratio=0.32,
                pool_area_px=5200.0,
                motorcycles_observed=3,
                observations=[bike(box_bottom_y=120.0, delta_sub=0.02), bike(delta_sub=0.12)],
                pool_top_y=220.0,
            )
        )

    def test_traffic_inside_the_pool_is_not_an_upstream_stop(self):
        # Three bikes waiting *inside* the water says nothing about avoidance.
        self.assertFalse(
            hc.should_escalate(
                water_ratio=0.34,
                pool_area_px=5200.0,
                motorcycles_observed=3,
                observations=[bike(box_bottom_y=300.0, delta_sub=0.02)],
                pool_top_y=200.0,
            )
        )

    def test_level_from_avoidance_is_three_or_nothing(self):
        escalated = hc.level_from_avoidance(
            water_ratio=0.34, pool_area_px=5200.0, motorcycles_observed=0
        )
        self.assertEqual(escalated, 3)
        quiet = hc.level_from_avoidance(
            water_ratio=0.29, pool_area_px=5200.0, motorcycles_observed=0
        )
        self.assertIsNone(quiet)


class TestAvoidanceEscalationOnTheVerdict(unittest.TestCase):
    """§2.4/AC7/AC8 — the regression that named the issue (#401)."""

    def test_stopped_traffic_beside_a_large_pool_is_deep_never_shallow(self):
        # The frame from the issue: 34 % of the roadway is under water, traffic has
        # stopped upstream, no wheel is deep enough to trip the legacy wheel signal.
        verdict, confidence = triangulate_flood_verdict(
            water_ratio=0.34,
            max_delta_sub=0.0,
            depth_estimated_cm=0.0,
            motorcycles_observed=0,
            pool_area_px=5200.0,
            observations=(),
            pool_top_y=220.0,
        )
        self.assertEqual(verdict, "FLOODED_DEEP")
        self.assertNotEqual(verdict, "FLOODED_SHALLOW")
        self.assertLessEqual(confidence, hc.AVOIDANCE_CONFIDENCE_CAP)

    def test_no_path_returns_shallow_for_a_lane_blocking_pool_without_through_traffic(self):
        # AC8 pins the deleted static fallback branch. Sweep everything that used to
        # reach it: any ratio at/above the threshold, any credible area, empty road or
        # bikes stopped upstream.
        stopped_observations = (
            (),
            (bike(box_bottom_y=120.0, delta_sub=0.02),),
        )
        for water_ratio in (0.30, 0.34, 0.50, 0.90):
            for pool_area_px in (400.0, 5200.0):
                for observations in stopped_observations:
                    with self.subTest(
                        water_ratio=water_ratio,
                        pool_area_px=pool_area_px,
                        observations=observations,
                    ):
                        verdict, confidence = triangulate_flood_verdict(
                            water_ratio=water_ratio,
                            max_delta_sub=0.0,
                            depth_estimated_cm=0.0,
                            motorcycles_observed=len(observations),
                            pool_area_px=pool_area_px,
                            observations=observations,
                            pool_top_y=220.0,
                        )
                        self.assertEqual(verdict, "FLOODED_DEEP")
                        self.assertNotEqual(verdict, "FLOODED_SHALLOW")
                        self.assertLessEqual(confidence, hc.AVOIDANCE_CONFIDENCE_CAP)

    def test_large_pool_below_the_ratio_threshold_is_not_escalated(self):
        # 0.29 with no bikes is a wet road, not a proved blockage. It keeps whatever
        # the legacy matrix said; what it must never do is escalate.
        verdict, _ = triangulate_flood_verdict(
            water_ratio=0.29,
            max_delta_sub=0.0,
            depth_estimated_cm=0.0,
            motorcycles_observed=0,
            pool_area_px=5200.0,
        )
        self.assertNotEqual(verdict, "FLOODED_DEEP")

    def test_sub_400px_patch_is_never_escalated(self):
        # E6
        verdict, _ = triangulate_flood_verdict(
            water_ratio=0.60,
            max_delta_sub=0.0,
            depth_estimated_cm=0.0,
            motorcycles_observed=0,
            pool_area_px=399.0,
        )
        self.assertNotEqual(verdict, "FLOODED_DEEP")

    def test_through_traffic_suppresses_escalation_on_the_verdict(self):
        # AC9: over-warning costs the same trust as under-warning.
        verdict, _ = triangulate_flood_verdict(
            water_ratio=0.34,
            max_delta_sub=0.0,
            depth_estimated_cm=0.0,
            motorcycles_observed=2,
            pool_area_px=5200.0,
            observations=(bike(delta_sub=0.12),),
            pool_top_y=220.0,
        )
        self.assertNotEqual(verdict, "FLOODED_DEEP")

    def test_legacy_decision_matrix_is_unchanged_without_avoidance_signals(self):
        # AC12: clients ignoring the new keys must see zero change.
        self.assertEqual(
            triangulate_flood_verdict(
                water_ratio=0.25, max_delta_sub=0.22, depth_estimated_cm=24.2, motorcycles_observed=5
            )[0],
            "FLOODED_DEEP",
        )
        self.assertEqual(
            triangulate_flood_verdict(
                water_ratio=0.20, max_delta_sub=0.08, depth_estimated_cm=8.8, motorcycles_observed=5
            )[0],
            "FLOODED_SHALLOW",
        )
        self.assertEqual(
            triangulate_flood_verdict(
                water_ratio=0.08, max_delta_sub=0.0, depth_estimated_cm=0.0, motorcycles_observed=2
            )[0],
            "WET",
        )
        self.assertEqual(
            triangulate_flood_verdict(
                water_ratio=0.02,
                max_delta_sub=0.0,
                depth_estimated_cm=0.0,
                motorcycles_observed=2,
                rain_event=True,
            )[0],
            "WET",
        )
        verdict, confidence = triangulate_flood_verdict(
            water_ratio=0.01, max_delta_sub=0.0, depth_estimated_cm=0.0, motorcycles_observed=2
        )
        self.assertEqual(verdict, "CLEAR")
        self.assertEqual(confidence, 0.95)

    def test_escalation_does_not_reuse_the_legacy_area_confidence(self):
        # Section 2.4 caps avoidance confidence at 0.78. The legacy "no bikes" branch
        # returned 0.82, which is exactly the over-confident clearance being removed.
        _, confidence = triangulate_flood_verdict(
            water_ratio=0.90,
            max_delta_sub=0.0,
            depth_estimated_cm=0.0,
            motorcycles_observed=0,
            pool_area_px=9000.0,
        )
        self.assertLessEqual(confidence, hc.AVOIDANCE_CONFIDENCE_CAP)


class TestHazardComposition(unittest.TestCase):
    """§2.4 last paragraph — ``max(level_from_depth, level_from_avoidance)``."""

    def test_measured_depth_yields_depth_measured_basis(self):
        hazard = hc.assess_hazard(
            depth_cm=14.5, calibration_ref=hc.CALIBRATION_REF_CURB, water_ratio=0.2
        )
        self.assertEqual(hazard.level, 2)
        self.assertEqual(hazard.basis, hc.BASIS_DEPTH_MEASURED)
        self.assertAlmostEqual(hazard.depth_cm, 14.5)
        self.assertEqual(hazard.depth_confidence, hc.CONFIDENCE_MEASURED)
        self.assertEqual(hazard.calibration_ref, hc.CALIBRATION_REF_CURB)

    def test_depth_at_the_stall_threshold_is_level_3(self):
        hazard = hc.assess_hazard(depth_cm=20.0, calibration_ref=hc.CALIBRATION_REF_MOTORCYCLE)
        self.assertEqual(hazard.level, 3)

    def test_measured_dry_road_keeps_its_zero_centimetres(self):
        # E3: 0.0 is a measurement, so it must survive as 0.0 (the UI prints "0 cm")
        # and must never be reported as UNMEASURED.
        hazard = hc.assess_hazard(depth_cm=0.0, calibration_ref=hc.CALIBRATION_REF_MOTORCYCLE)
        self.assertEqual(hazard.level, 1)
        self.assertEqual(hazard.depth_cm, 0.0)
        self.assertIsNotNone(hazard.depth_cm)
        self.assertEqual(hazard.depth_confidence, hc.CONFIDENCE_MEASURED)

    def test_unmeasured_without_avoidance_is_null_not_level_1(self):
        # "Absence of measurement is not safety" — the single most important assertion
        # in this file. A dry-looking frame with no reference must not read as safe.
        hazard = hc.assess_hazard(depth_cm=None, water_ratio=0.02, pool_area_px=120.0)
        self.assertIsNone(hazard.level)
        self.assertEqual(hazard.basis, hc.BASIS_NONE)
        self.assertIsNone(hazard.depth_cm)
        self.assertEqual(hazard.depth_confidence, hc.CONFIDENCE_UNMEASURED)
        self.assertEqual(hazard.calibration_ref, hc.CALIBRATION_REF_NONE)

    def test_avoidance_escalation_publishes_a_level_and_no_number(self):
        # AC10/E4: the UI renders the avoidance sentence, not a fabricated centimetre.
        hazard = hc.assess_hazard(
            depth_cm=None,
            calibration_ref=hc.CALIBRATION_REF_CURB,
            water_ratio=0.34,
            pool_area_px=5200.0,
            motorcycles_observed=0,
        )
        self.assertEqual(hazard.level, 3)
        self.assertEqual(hazard.basis, hc.BASIS_AVOIDANCE_INFERRED)
        self.assertIsNone(hazard.depth_cm)
        self.assertEqual(hazard.depth_confidence, hc.CONFIDENCE_UNMEASURED)

    def test_depth_and_avoidance_combine_to_the_higher_level(self):
        # §5.1: a measured 14.5 cm pool that traffic is avoiding is Level 3 with the
        # measurement retained — neither signal may erase the other.
        hazard = hc.assess_hazard(
            depth_cm=14.5,
            calibration_ref=hc.CALIBRATION_REF_CURB,
            water_ratio=0.34,
            pool_area_px=5200.0,
            motorcycles_observed=0,
        )
        self.assertEqual(hazard.level, 3)
        self.assertEqual(hazard.basis, hc.BASIS_DEPTH_AND_AVOIDANCE)
        self.assertAlmostEqual(hazard.depth_cm, 14.5)
        self.assertEqual(hazard.depth_confidence, hc.CONFIDENCE_MEASURED)

    def test_shallow_depth_with_avoidance_still_escalates(self):
        hazard = hc.assess_hazard(
            depth_cm=8.0,
            calibration_ref=hc.CALIBRATION_REF_CURB,
            water_ratio=0.31,
            pool_area_px=5200.0,
            motorcycles_observed=0,
        )
        self.assertEqual(hazard.level, 3)
        self.assertEqual(hazard.basis, hc.BASIS_DEPTH_AND_AVOIDANCE)

    def test_through_traffic_keeps_a_measured_pool_at_level_2(self):
        hazard = hc.assess_hazard(
            depth_cm=14.5,
            calibration_ref=hc.CALIBRATION_REF_CURB,
            water_ratio=0.34,
            pool_area_px=5200.0,
            motorcycles_observed=1,
            observations=(bike(delta_sub=0.12),),
            pool_top_y=220.0,
        )
        self.assertEqual(hazard.level, 2)
        self.assertEqual(hazard.basis, hc.BASIS_DEPTH_MEASURED)

    def test_inferred_depth_is_labelled_inferred(self):
        # E12 (the Go fallback shares this vocabulary): a derived depth is not measured.
        hazard = hc.assess_hazard(
            depth_cm=14.5,
            depth_inferred=True,
            calibration_ref=hc.CALIBRATION_REF_MOTORCYCLE,
        )
        self.assertEqual(hazard.level, 2)
        self.assertEqual(hazard.depth_confidence, hc.CONFIDENCE_INFERRED)

    def test_implausible_depth_keeps_level_3_but_drops_the_number(self):
        # E8: 160 cm is not credible. Keep the warning, refuse the figure.
        logger = logging.getLogger("cv-engine.hazard-test.implausible")
        with self.assertLogs(logger, level="WARNING"):
            hazard = hc.assess_hazard(
                depth_cm=160.0,
                calibration_ref=hc.CALIBRATION_REF_MOTORCYCLE,
                logger=logger,
            )
        self.assertEqual(hazard.level, 3)
        self.assertEqual(hazard.depth_confidence, hc.CONFIDENCE_UNMEASURED)
        self.assertIsNone(hazard.depth_cm)

    def test_credible_depth_boundary_is_150_cm(self):
        hazard = hc.assess_hazard(
            depth_cm=150.0, calibration_ref=hc.CALIBRATION_REF_MOTORCYCLE
        )
        self.assertEqual(hazard.level, 3)
        self.assertEqual(hazard.depth_cm, 150.0)
        self.assertEqual(hazard.depth_confidence, hc.CONFIDENCE_MEASURED)


class TestWireContract(unittest.TestCase):
    """§5.1 — the ``flood_assessment`` block, built by production code."""

    LEGACY_KEYS = (
        "status",
        "water_surface_ratio",
        "wheel_submersion_detected",
        "max_submersion_depth_cm",
        "motorcycles_observed",
        "motorcycles_submerged",
        "puddle_polygon_count",
    )
    HAZARD_KEYS = ("hazard_level", "hazard_basis", "depth_confidence", "calibration_ref")

    def _build(self, hazard=None, **overrides):
        kwargs = {
            "verdict": "FLOODED_DEEP",
            "water_ratio": 0.34,
            "max_delta_sub": 0.0,
            "depth_estimated_cm": 14.5,
            "motorcycles_observed": 0,
            "motorcycles_submerged": 0,
            "puddle_polygon_count": 2,
        }
        kwargs.update(overrides)
        return build_flood_assessment(hazard=hazard, **kwargs)

    def test_legacy_keys_keep_their_values_alongside_the_hazard_keys(self):
        hazard = hc.assess_hazard(
            depth_cm=None,
            calibration_ref=hc.CALIBRATION_REF_CURB,
            water_ratio=0.34,
            pool_area_px=5200.0,
            motorcycles_observed=0,
        )
        block = self._build(hazard=hazard)

        for key in self.LEGACY_KEYS:
            self.assertIn(key, block)
        self.assertEqual(block["status"], "FLOODED_DEEP")
        self.assertAlmostEqual(block["water_surface_ratio"], 0.34)
        self.assertFalse(block["wheel_submersion_detected"])
        # AC12: max_submersion_depth_cm is the legacy field and stays untouched.
        self.assertAlmostEqual(block["max_submersion_depth_cm"], 14.5)
        self.assertEqual(block["motorcycles_observed"], 0)
        self.assertEqual(block["motorcycles_submerged"], 0)
        self.assertEqual(block["puddle_polygon_count"], 2)

        for key in self.HAZARD_KEYS:
            self.assertIn(key, block)
        self.assertEqual(block["hazard_level"], 3)
        self.assertEqual(block["hazard_basis"], hc.BASIS_AVOIDANCE_INFERRED)
        self.assertEqual(block["depth_confidence"], hc.CONFIDENCE_UNMEASURED)
        self.assertEqual(block["calibration_ref"], hc.CALIBRATION_REF_CURB)

    def test_block_defaults_to_unmeasured_when_no_assessment_is_supplied(self):
        block = self._build()
        self.assertIsNone(block["hazard_level"])
        self.assertEqual(block["hazard_basis"], hc.BASIS_NONE)
        self.assertEqual(block["depth_confidence"], hc.CONFIDENCE_UNMEASURED)
        self.assertEqual(block["calibration_ref"], hc.CALIBRATION_REF_NONE)

    def test_measured_dry_keeps_level_1_and_zero(self):
        hazard = hc.assess_hazard(
            depth_cm=0.0, calibration_ref=hc.CALIBRATION_REF_MOTORCYCLE
        )
        block = self._build(hazard=hazard, verdict="DRY", depth_estimated_cm=0.0, water_ratio=0.0)
        self.assertEqual(block["hazard_level"], 1)
        self.assertEqual(block["depth_confidence"], hc.CONFIDENCE_MEASURED)
        self.assertAlmostEqual(block["max_submersion_depth_cm"], 0.0)

    def test_unmeasured_serialises_as_null_never_zero(self):
        # AC13: absent measurement is null. A 0 here would read as "dry, verified".
        hazard = hc.assess_hazard(depth_cm=None)
        block = self._build(hazard=hazard)
        encoded = json.loads(json.dumps(block))
        self.assertIsNone(encoded["hazard_level"])
        self.assertNotEqual(encoded["hazard_level"], 0)

    def test_basis_and_confidence_vocabularies_are_closed(self):
        # §5.1 enumerates the accepted values; nothing else may reach the wire.
        for basis in (
            hc.BASIS_DEPTH_MEASURED,
            hc.BASIS_AVOIDANCE_INFERRED,
            hc.BASIS_DEPTH_AND_AVOIDANCE,
            hc.BASIS_NONE,
        ):
            self.assertIn(basis, hc.HAZARD_BASES)
        for confidence in (
            hc.CONFIDENCE_MEASURED,
            hc.CONFIDENCE_INFERRED,
            hc.CONFIDENCE_UNMEASURED,
        ):
            self.assertIn(confidence, hc.DEPTH_CONFIDENCES)
        for reference in hc.CALIBRATION_REFS:
            self.assertIn(
                reference,
                (
                    "MOTORCYCLE_110CM",
                    "CURB_15CM",
                    "CAMERA_OVERRIDE",
                    "NONE",
                ),
            )


class TestDeEscalationLatch(unittest.TestCase):
    """§2.5 — escalation is immediate, a drop needs two agreeing observations."""

    #: Depth that lands in each band, so a level also carries its own depth.
    LEVEL_DEPTH_CM = {1: 5.0, 2: 15.0, 3: 25.0}

    def setUp(self):
        self.now = 1000.0
        self.latch = hc.HazardLatch(clock=lambda: self.now)

    def measured(self, level):
        """A depth-measured assessment for ``level`` (``None`` = unmeasured)."""
        if level is None:
            return hc.assess_hazard()
        return hc.assess_hazard(depth_cm=self.LEVEL_DEPTH_CM[level])

    def apply(self, camera_id, level):
        return self.latch.apply(camera_id, self.measured(level)).level

    def test_initial_observation_is_accepted(self):
        self.assertEqual(self.apply("cam-a", 3), 3)
        self.assertEqual(self.apply("cam-b", 1), 1)

    def test_escalation_is_immediate(self):
        self.assertEqual(self.apply("cam-a", 1), 1)
        self.assertEqual(self.apply("cam-a", 3), 3)

    def test_de_escalation_is_held_for_one_frame(self):
        # E15: one passing truck must not erase the pool for a frame.
        self.assertEqual(self.apply("cam-a", 3), 3)
        self.assertEqual(self.apply("cam-a", 1), 3)
        # Second consecutive agreeing observation releases the hold.
        self.assertEqual(self.apply("cam-a", 1), 1)

    def test_de_escalation_to_an_intermediate_level_also_needs_two_frames(self):
        self.assertEqual(self.apply("cam-a", 3), 3)
        self.assertEqual(self.apply("cam-a", 2), 3)
        self.assertEqual(self.apply("cam-a", 2), 2)

    def test_non_agreeing_observations_do_not_release_the_hold(self):
        self.assertEqual(self.apply("cam-a", 3), 3)
        self.assertEqual(self.apply("cam-a", 1), 3)  # pending 1
        self.assertEqual(self.apply("cam-a", 2), 3)  # not the same as pending -> hold
        self.assertEqual(self.apply("cam-a", 2), 2)  # two agreeing frames of 2

    def test_a_latched_escalation_does_not_leak_across_cameras(self):
        self.assertEqual(self.apply("cam-a", 3), 3)
        self.assertEqual(self.apply("cam-b", 1), 1)

    def test_previous_observation_older_than_the_ttl_is_ignored(self):
        # E14: the 180 s verdict TTL also bounds the latch state.
        self.assertEqual(self.apply("cam-a", 3), 3)
        self.now += hc.LATCH_TTL_SECONDS + 1.0
        self.assertEqual(self.apply("cam-a", 1), 1)

    def test_observation_within_the_ttl_still_holds(self):
        self.assertEqual(self.apply("cam-a", 3), 3)
        self.now += hc.LATCH_TTL_SECONDS - 1.0
        self.assertEqual(self.apply("cam-a", 1), 3)

    def test_unmeasured_level_does_not_latch_a_level(self):
        # A frame with no credible level must not reset a live escalation.
        self.assertEqual(self.apply("cam-a", 3), 3)
        self.assertIsNone(self.apply("cam-a", None))
        self.assertEqual(self.apply("cam-a", 3), 3)

    def test_reset_clears_all_state(self):
        self.assertEqual(self.apply("cam-a", 3), 3)
        self.latch.reset()
        self.assertEqual(self.apply("cam-a", 1), 1)

    def test_a_held_level_publishes_the_held_companion_fields(self):
        # Escalate from avoidance (Level 3, no measurable depth), then drop to a
        # measured 5 cm frame. The held block must stay fully self-consistent: the
        # drop frame's 5.0 cm / DEPTH_MEASURED must not be grafted onto Level 3.
        held = hc.assess_hazard(
            water_ratio=0.5,
            pool_area_px=5000.0,
            motorcycles_observed=0,
        )
        self.assertEqual(held.level, 3)
        self.assertEqual(self.latch.apply("cam-a", held), held)

        drop = hc.assess_hazard(depth_cm=5.0, calibration_ref=hc.CALIBRATION_REF_MOTORCYCLE)
        published = self.latch.apply("cam-a", drop)
        self.assertIs(published, held)
        self.assertEqual(published.level, 3)
        self.assertEqual(published.basis, hc.BASIS_AVOIDANCE_INFERRED)
        self.assertIsNone(published.depth_cm)
        self.assertEqual(published.depth_confidence, hc.CONFIDENCE_UNMEASURED)
        self.assertEqual(published.calibration_ref, hc.CALIBRATION_REF_NONE)

        # A second agreeing drop frame releases the hold and publishes that frame.
        self.assertIs(self.latch.apply("cam-a", drop), drop)


class TestCalibrationLoader(unittest.TestCase):
    """§2.2 / E9 / E10 — fail-closed, never fatal, never silently bad."""

    def setUp(self):
        self.tmpdir = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmpdir.cleanup)
        self.path = Path(self.tmpdir.name) / "calibration.json"
        self.logger = logging.getLogger("cv-engine.hazard-test.calibration")

    def _registry(self, **kwargs):
        return hc.CalibrationRegistry(self.path, logger=self.logger, **kwargs)

    def test_valid_entry_is_loaded_and_applied(self):
        write_calibration(
            self.path,
            {"version": 1, "cameras": {"cam_dbp_01": valid_entry()}},
        )
        registry = self._registry().load()
        self.assertAlmostEqual(registry.multiplier_for("cam_dbp_01"), 1.08)
        entry = registry.entry_for("cam_dbp_01")
        self.assertIsNotNone(entry)
        self.assertEqual(entry.measured_by, "operator-01")
        self.assertEqual(entry.frame_ref, "docs/proofs/s12_t3/calib_cam_dbp_01.jpg")
        self.assertEqual(entry.measured_at, date(2026, 11, 14))

    def test_unlisted_camera_falls_back_to_the_raw_scale(self):
        write_calibration(self.path, {"version": 1, "cameras": {"cam_dbp_01": valid_entry()}})
        registry = self._registry().load()
        self.assertEqual(registry.multiplier_for("cam_other"), 1.0)
        self.assertIsNone(registry.entry_for("cam_other"))

    def test_multiplier_outside_the_band_is_rejected_and_logged(self):
        # E9 / M8: 1.4 must not be applied, and the rejection must be visible.
        write_calibration(
            self.path,
            {"version": 1, "cameras": {"cam_dbp_01": valid_entry(scale_multiplier=1.4)}},
        )
        with self.assertLogs(self.logger, level="ERROR"):
            registry = self._registry().load()
        self.assertEqual(registry.multiplier_for("cam_dbp_01"), 1.0)
        self.assertIsNone(registry.entry_for("cam_dbp_01"))
        self.assertTrue(registry.errors)

    def test_upper_band_boundary_is_rejected(self):
        # §8.1 pins 1.25 as rejected; accepting it would let a bad calibration through.
        write_calibration(
            self.path,
            {"version": 1, "cameras": {"cam_dbp_01": valid_entry(scale_multiplier=1.25)}},
        )
        with self.assertLogs(self.logger, level="ERROR"):
            registry = self._registry().load()
        self.assertEqual(registry.multiplier_for("cam_dbp_01"), 1.0)

    def test_provenance_is_mandatory(self):
        # "an entry without measured_by/frame_ref is rejected" — auditable or nothing.
        for missing in ("measured_by", "frame_ref"):
            with self.subTest(missing=missing):
                entry = valid_entry()
                entry.pop(missing)
                write_calibration(self.path, {"version": 1, "cameras": {"cam_dbp_01": entry}})
                with self.assertLogs(self.logger, level="ERROR"):
                    registry = self._registry().load()
                self.assertEqual(registry.multiplier_for("cam_dbp_01"), 1.0)

    def test_malformed_json_boots_with_defaults_and_logs(self):
        # E10: the service never fails to boot over a bad calibration file.
        self.path.write_text("{ this is not json", encoding="utf-8")
        with self.assertLogs(self.logger, level="ERROR"):
            registry = self._registry().load()
        self.assertEqual(registry.multiplier_for("cam_dbp_01"), 1.0)
        self.assertTrue(registry.errors)

    def test_missing_file_boots_with_defaults(self):
        registry = self._registry().load()
        self.assertEqual(registry.multiplier_for("cam_dbp_01"), 1.0)
        self.assertEqual(tuple(registry.errors), ())

    def test_no_path_configured_boots_with_defaults(self):
        registry = hc.CalibrationRegistry(None, logger=self.logger).load()
        self.assertEqual(registry.multiplier_for("cam_dbp_01"), 1.0)

    def test_stale_calibration_warns_but_is_still_applied(self):
        # A 180-day-old measurement is a re-aiming risk, not a reason to stop serving.
        write_calibration(
            self.path,
            {
                "version": 1,
                "cameras": {
                    "cam_dbp_01": valid_entry(measured_at="2026-01-01", scale_multiplier=1.02)
                },
            },
        )
        with self.assertLogs(self.logger, level="WARNING") as logs:
            registry = self._registry(today=date(2026, 11, 11)).load()
        self.assertAlmostEqual(registry.multiplier_for("cam_dbp_01"), 1.02)
        self.assertIn("cam_dbp_01", "\n".join(logs.output))

    def test_reload_happens_only_when_mtime_moves(self):
        write_calibration(
            self.path,
            {"version": 1, "cameras": {"cam_dbp_01": valid_entry(scale_multiplier=1.02)}},
        )
        registry = self._registry().load()
        self.assertAlmostEqual(registry.multiplier_for("cam_dbp_01"), 1.02)
        self.assertFalse(registry.reload_if_changed())

        write_calibration(
            self.path,
            {"version": 1, "cameras": {"cam_dbp_01": valid_entry(scale_multiplier=1.05)}},
        )
        os.utime(self.path, (2_000_000_000, 2_000_000_000))
        self.assertTrue(registry.reload_if_changed())
        self.assertAlmostEqual(registry.multiplier_for("cam_dbp_01"), 1.05)

    def test_reload_revalidates_and_can_revoke_a_multiplier(self):
        write_calibration(
            self.path,
            {"version": 1, "cameras": {"cam_dbp_01": valid_entry(scale_multiplier=1.05)}},
        )
        registry = self._registry().load()
        self.assertAlmostEqual(registry.multiplier_for("cam_dbp_01"), 1.05)

        write_calibration(
            self.path,
            {"version": 1, "cameras": {"cam_dbp_01": valid_entry(scale_multiplier=1.4)}},
        )
        os.utime(self.path, (2_000_000_100, 2_000_000_100))
        with self.assertLogs(self.logger, level="ERROR"):
            self.assertTrue(registry.reload_if_changed())
        self.assertEqual(registry.multiplier_for("cam_dbp_01"), 1.0)

    def test_reload_survives_a_file_that_turns_into_garbage(self):
        write_calibration(
            self.path,
            {"version": 1, "cameras": {"cam_dbp_01": valid_entry(scale_multiplier=1.03)}},
        )
        registry = self._registry().load()
        self.path.write_text("<<<not json>>>", encoding="utf-8")
        os.utime(self.path, (2_000_000_200, 2_000_000_200))
        with self.assertLogs(self.logger, level="ERROR"):
            registry.reload_if_changed()
        self.assertEqual(registry.multiplier_for("cam_dbp_01"), 1.0)


class TestUpstreamReferenceIsNotADryRoad(unittest.TestCase):
    """#401 — a bike stopping outside the water measures the bike, not the pool.

    The MOTORCYCLE_110CM ladder measures submersion at a vehicle. When no vehicle
    is in the water, ``delta_sub`` is 0 for every bike in the frame, and reporting
    that as a measured ``0.0 cm`` pool would be a fabricated measurement (AC10):
    E3's "measured dry road" is a dry frame, not a dry reference standing next to
    a flooded lane.
    """

    def test_zero_submersion_inside_a_flooded_roi_is_unmeasured(self):
        hazard = hc.assess_hazard(
            depth_cm=0.0,
            calibration_ref=hc.CALIBRATION_REF_MOTORCYCLE,
            water_ratio=0.34,
            pool_area_px=5200.0,
            motorcycles_observed=2,
            observations=(bike(box_bottom_y=200.0, delta_sub=0.0),),
            pool_top_y=220.0,
        )
        self.assertEqual(hazard.level, 3)
        self.assertEqual(hazard.basis, hc.BASIS_AVOIDANCE_INFERRED)
        self.assertIsNone(hazard.depth_cm)
        self.assertEqual(hazard.depth_confidence, hc.CONFIDENCE_UNMEASURED)

    def test_zero_submersion_on_a_dry_road_is_still_a_measurement(self):
        hazard = hc.assess_hazard(
            depth_cm=0.0,
            calibration_ref=hc.CALIBRATION_REF_MOTORCYCLE,
            water_ratio=0.02,
        )
        self.assertEqual(hazard.level, 1)
        self.assertEqual(hazard.depth_cm, 0.0)
        self.assertEqual(hazard.depth_confidence, hc.CONFIDENCE_MEASURED)


if __name__ == "__main__":
    unittest.main()
