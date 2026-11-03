#!/usr/bin/env python3
"""
contract_test.py — Unit tests for the S9-T1 polygon annotation contract mirror.

The rules exercised here mirror Go validateAnnotations in
services/go-server/internal/archiver/handler.go so a label accepted by this
suite is accepted by the archiver SaveLabel handler.
"""

import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent.parent.parent))

from tools.seg import contract  # noqa: E402


def good_polygon(n: int = 4) -> list:
    """A simple normalized square-ish polygon with n vertices."""
    return [[0.1, 0.1], [0.9, 0.1], [0.9, 0.9], [0.1, 0.9]][: max(3, n)]


class TestNormalizePolygonClass(unittest.TestCase):
    def test_canonical_classes_pass_through(self):
        for cls in contract.CANONICAL_CLASSES:
            self.assertEqual(contract.normalize_polygon_class(cls), cls)

    def test_aliases_map_to_canonical(self):
        cases = {
            "Road": "roadway", "ASPHALT": "roadway",
            "puddle": "water_puddle", "Flooded": "water_puddle",
            "motorbike": "motorcycle", "suv": "car", "lorry": "truck",
        }
        for raw, expected in cases.items():
            self.assertEqual(contract.normalize_polygon_class(raw), expected)

    def test_unknown_class_returns_empty(self):
        self.assertEqual(contract.normalize_polygon_class("sky"), "")
        self.assertEqual(contract.normalize_polygon_class(""), "")
        self.assertEqual(contract.normalize_polygon_class(None), "")


class TestValidateAnnotations(unittest.TestCase):
    def test_valid_annotations_pass(self):
        anns = [
            {"class": "roadway", "polygon": good_polygon(), "source": "sam_prelabel"},
            {"class": "van", "polygon": good_polygon(3), "source": "sam_prelabel"},
        ]
        self.assertEqual(contract.validate_annotations(anns), [])

    def test_too_many_annotations_rejected(self):
        anns = [{"class": "car", "polygon": good_polygon()} for _ in range(33)]
        errors = contract.validate_annotations(anns)
        self.assertTrue(any("too many polygon annotations: 33" in e for e in errors))

    def test_unknown_class_rejected(self):
        errors = contract.validate_annotations([{"class": "bicycle", "polygon": good_polygon()}])
        self.assertTrue(any("unknown class" in e for e in errors))

    def test_polygon_vertex_bounds_rejected(self):
        too_few = contract.validate_annotations([{"class": "car", "polygon": [[0, 0], [1, 1]]}])
        self.assertTrue(any("between 3 and 64" in e for e in too_few))
        too_many = contract.validate_annotations(
            [{"class": "car", "polygon": [[0.0, 0.0]] * 65}]
        )
        self.assertTrue(any("between 3 and 64" in e for e in too_many))

    def test_pixel_coordinates_rejected(self):
        anns = [{"class": "car", "polygon": [[10, 20], [30, 40], [50, 60]]}]
        errors = contract.validate_annotations(anns)
        self.assertTrue(any("outside normalized range" in e for e in errors))

    def test_negative_coordinates_rejected(self):
        anns = [{"class": "car", "polygon": [[-0.1, 0.5], [0.5, 0.5], [0.5, 0.6]]}]
        errors = contract.validate_annotations(anns)
        self.assertTrue(any("outside normalized range" in e for e in errors))

    def test_malformed_vertex_rejected(self):
        errors = contract.validate_annotations(
            [{"class": "car", "polygon": [[0.1, 0.1], [0.2, 0.2, 0.3], [0.5, 0.6]]}]
        )
        self.assertTrue(any("must be an [x, y] pair" in e for e in errors))

    def test_long_source_rejected(self):
        errors = contract.validate_annotations(
            [{"class": "car", "polygon": good_polygon(), "source": "x" * 33}]
        )
        self.assertTrue(any("source exceeds 32" in e for e in errors))

    def test_boundary_coordinates_accepted(self):
        anns = [{"class": "roadway", "polygon": [[0.0, 0.0], [1.0, 0.0], [1.0, 1.0], [0.0, 1.0]]}]
        self.assertEqual(contract.validate_annotations(anns), [])


class TestCanonicalizeAnnotations(unittest.TestCase):
    def test_aliases_resolved_and_clamped(self):
        anns = [{"class": "puddle", "polygon": [[-0.01, 0.5], [1.01, 0.5], [0.5, 0.9]]}]
        out = contract.canonicalize_annotations(anns)
        self.assertEqual(len(out), 1)
        self.assertEqual(out[0]["class"], "water_puddle")
        self.assertEqual(out[0]["polygon"][0], [0.0, 0.5])
        self.assertEqual(out[0]["polygon"][1], [1.0, 0.5])
        self.assertEqual(out[0]["source"], contract.SOURCE_SAM_PRELABEL)

    def test_unknown_class_dropped(self):
        out = contract.canonicalize_annotations([{"class": "sky", "polygon": good_polygon()}])
        self.assertEqual(out, [])

    def test_degenerate_polygon_dropped(self):
        out = contract.canonicalize_annotations([{"class": "car", "polygon": [[0.1, 0.1], [0.2, 0.2]]}])
        self.assertEqual(out, [])

    def test_custom_source_preserved_and_truncated(self):
        out = contract.canonicalize_annotations(
            [{"class": "car", "polygon": good_polygon(), "source": "s" * 40}]
        )
        self.assertEqual(len(out[0]["source"]), 32)


class TestValidateLabelPayload(unittest.TestCase):
    def _payload(self, **overrides) -> dict:
        base = {
            "date": "2026-10-27",
            "camera_id": "cam123",
            "snapshot_key": "2026-10-27/cam123/1234_sample.jpg",
            "annotator": "sam_prelabel",
            "annotations": [{"class": "roadway", "polygon": good_polygon()}],
        }
        base.update(overrides)
        return base

    def test_valid_payload_passes(self):
        self.assertEqual(contract.validate_label_payload(self._payload()), [])

    def test_missing_required_fields_rejected(self):
        for field in ("date", "camera_id", "snapshot_key", "annotator"):
            payload = self._payload()
            payload.pop(field)
            errors = contract.validate_label_payload(payload)
            self.assertTrue(any(field in e for e in errors), field)

    def test_invalid_annotations_surfaced(self):
        payload = self._payload(annotations=[{"class": "sky", "polygon": good_polygon()}])
        errors = contract.validate_label_payload(payload)
        self.assertTrue(any("unknown class" in e for e in errors))


if __name__ == "__main__":
    unittest.main()
