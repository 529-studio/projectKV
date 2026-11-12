#!/usr/bin/env python3
"""
image_geometry_test.py — Unit tests for mask-to-polygon geometry helpers.

All logic is exercised against synthetic masks so the suite runs on the host
stdlib + numpy without any model weights (AGENTS.md Rule 10: deterministic,
repeatable verification).
"""

import sys
import unittest
from pathlib import Path

import numpy as np

sys.path.insert(0, str(Path(__file__).resolve().parent.parent.parent))

from tools.seg import image_geometry as geom  # noqa: E402


class TestLetterbox(unittest.TestCase):
    def test_aspect_preserved_and_scaled_back(self):
        img = np.zeros((600, 800, 3), dtype=np.uint8)
        img[100:200, 200:600] = 255
        canvas, scale, pads = geom.letterbox(img, 640)
        self.assertEqual(canvas.shape, (640, 640, 3))
        self.assertAlmostEqual(scale, 0.8)
        # 800x0.8 = 640 wide, so pad_x = 0; 600x0.8 = 480 high, pad_y = 80.
        self.assertEqual(pads, (0, 80))
        # A bright pixel at original (200, 150) lands at canvas
        # ((200*0.8)+0, (150*0.8)+80) = (160, 200).
        self.assertGreater(canvas[200, 160, 0], 0.9)

    def test_output_range_normalized(self):
        img = np.full((100, 100, 3), 255, dtype=np.uint8)
        canvas, _, _ = geom.letterbox(img, 320)
        self.assertLessEqual(canvas.max(), 1.0)
        self.assertGreaterEqual(canvas.min(), 0.0)


class TestScaleBoxesBack(unittest.TestCase):
    def test_round_trip(self):
        # A box centered at original (500, 250)-(550, 300) in a 600x800 frame:
        # letterbox scale 0.8, pad (0, 80) puts it at canvas center (400, 280).
        scale, pads = 0.8, (0, 80)
        canvas_box = [400.0, 280.0, 80.0, 40.0]  # cx, cy, w, h in canvas px
        back = geom.scale_boxes_back([canvas_box], scale, pads, 600, 800)[0]
        self.assertAlmostEqual(float(back[0]), 450.0, places=1)
        self.assertAlmostEqual(float(back[1]), 225.0, places=1)
        self.assertAlmostEqual(float(back[2]), 550.0, places=1)
        self.assertAlmostEqual(float(back[3]), 275.0, places=1)


class TestNms(unittest.TestCase):
    def test_suppresses_overlapping(self):
        boxes = np.array([[0, 0, 10, 10], [1, 1, 11, 11], [50, 50, 60, 60]], dtype=np.float32)
        scores = np.array([0.9, 0.8, 0.7], dtype=np.float32)
        keep = geom.nms_xyxy(boxes, scores, iou_thresh=0.5)
        self.assertEqual(keep, [0, 2])


class TestLargestComponent(unittest.TestCase):
    def test_picks_largest_and_drops_noise(self):
        mask = np.zeros((100, 100), dtype=bool)
        mask[10:20, 10:20] = True   # 100 px
        mask[40:80, 40:80] = True   # 1600 px (largest)
        mask[90, 0] = True          # 1 px noise
        comp = geom.largest_component(mask)
        self.assertEqual(int(comp.sum()), 1600)
        self.assertTrue(comp[40, 40])
        self.assertFalse(comp[15, 15])
        self.assertFalse(comp[90, 0])

    def test_empty_mask(self):
        self.assertEqual(int(geom.largest_component(np.zeros((10, 10), bool)).sum()), 0)


class TestMaskToPolygon(unittest.TestCase):
    def test_square_mask_yields_quadrilateral(self):
        mask = np.zeros((200, 200), dtype=bool)
        mask[50:150, 60:160] = True
        poly = geom.mask_to_polygon(mask, max_vertices=8, epsilon=1.0)
        self.assertGreaterEqual(len(poly), 3)
        self.assertLessEqual(len(poly), 8)
        xs = [p[0] for p in poly]
        ys = [p[1] for p in poly]
        # Boundary must stay within the mask extent (plus small tolerance).
        self.assertGreaterEqual(min(xs), 58)
        self.assertLessEqual(max(xs), 162)
        self.assertGreaterEqual(min(ys), 48)
        self.assertLessEqual(max(ys), 152)

    def test_empty_mask_yields_no_polygon(self):
        self.assertEqual(geom.mask_to_polygon(np.zeros((50, 50), bool)), [])

    def test_max_vertices_enforced(self):
        rng = np.random.default_rng(42)
        noise = rng.random((128, 128)) > 0.5
        mask = geom.largest_component(noise)
        if mask.sum() == 0:
            self.skipTest("random mask produced no component")
        poly = geom.mask_to_polygon(mask, max_vertices=20, epsilon=1.0)
        self.assertLessEqual(len(poly), 20)


class TestTraceBoundaryTermination(unittest.TestCase):
    """
    Regression tests for the Moore-trace stopping condition (S13, premature
    three-point termination).

    The old termination test was "current pixel == start pixel and the path has
    more than two points". A protrusion tip or a diagonally clipped corner is
    re-entered two steps after the trace leaves it, so the trace stopped with a
    three-point path (a single edge) even though the component was large. Every
    case below fails against that condition and passes with Jacob's criterion.
    """

    @staticmethod
    def _bbox(points):
        return (
            min(p[0] for p in points),
            max(p[0] for p in points),
            min(p[1] for p in points),
            max(p[1] for p in points),
        )

    @staticmethod
    def _mask_bbox(mask):
        ys, xs = np.where(mask)
        return (float(xs.min()), float(xs.max()), float(ys.min()), float(ys.max()))

    def assert_polygon_covers_mask(self, mask):
        poly = geom.mask_to_polygon(mask, max_vertices=48, epsilon=1.5)
        self.assertGreaterEqual(
            len(poly), 3, "boundary trace collapsed to a degenerate polygon"
        )
        for got, want in zip(self._bbox(poly), self._mask_bbox(mask)):
            self.assertAlmostEqual(got, want, delta=1.0)

    def test_protrusion_tip_traces_whole_component(self):
        # A two-pixel-wide protrusion tip sitting on a large slab: the tip is
        # the topmost-leftmost pixel, and the trace sees the start pixel again
        # after only three steps.
        mask = np.zeros((12, 12), dtype=bool)
        mask[0, 1] = True
        mask[1, 0:2] = True
        mask[2:9, 0:8] = True
        trace = geom.trace_boundary(mask)
        self.assertGreater(len(trace), 4)
        self.assert_polygon_covers_mask(mask)

    def test_clipped_corner_traces_whole_component(self):
        # Diagonal staircase: the top-left corner is clipped, so the start
        # pixel has a single diagonal continuation and the trace wraps back to
        # it immediately.
        mask = np.zeros((12, 12), dtype=bool)
        for y in range(9):
            mask[y, max(0, 4 - y):9] = True
        trace = geom.trace_boundary(mask)
        self.assertGreater(len(trace), 4)
        self.assert_polygon_covers_mask(mask)

    def test_single_pixel_antenna_retained(self):
        # A one-pixel antenna is the topmost pixel, so it is the start pixel.
        mask = np.zeros((20, 20), dtype=bool)
        mask[2:15, 3:17] = True
        mask[0:2, 5] = True
        self.assert_polygon_covers_mask(mask)

    def test_diagonal_antenna_kept_in_component(self):
        # A diagonally attached antenna only exists under 8-connectivity; the
        # component labeling must not split it off before tracing.
        mask = np.zeros((20, 20), dtype=bool)
        mask[6:16, 4:15] = True
        for i in range(5):
            mask[5 - i, 4 - i] = True
        comp = geom.largest_component(mask)
        self.assertTrue(comp[1, 0])
        self.assert_polygon_covers_mask(mask)

    def test_trace_is_closed_and_eight_connected(self):
        mask = np.zeros((24, 24), dtype=bool)
        mask[3, 10] = True
        mask[4, 10] = True
        mask[5:20, 5:20] = True
        trace = geom.trace_boundary(mask)
        self.assertGreaterEqual(len(trace), 4)
        self.assertEqual(trace[0], trace[-1])
        for (x1, y1), (x2, y2) in zip(trace, trace[1:]):
            self.assertLessEqual(abs(x2 - x1), 1)
            self.assertLessEqual(abs(y2 - y1), 1)
        self.assertNotEqual((x1, y1), (x2, y2))

    def test_corner_touching_blocks_both_traced(self):
        # Two slabs joined at a single diagonal corner: the outer contour must
        # pass through both lobes, not close around the first one.
        mask = np.zeros((20, 20), dtype=bool)
        mask[2:8, 2:8] = True
        mask[8:14, 8:14] = True
        self.assert_polygon_covers_mask(mask)


class TestNormalizePolygon(unittest.TestCase):
    def test_pixels_normalized_and_clamped(self):
        poly = geom.normalize_polygon([(0, 0), (100, 50), (200, 100)], 100, 100)
        self.assertEqual(poly[0], [0.0, 0.0])
        self.assertEqual(poly[1], [1.0, 0.5])
        self.assertEqual(poly[2], [1.0, 1.0])

    def test_zero_size_returns_empty(self):
        self.assertEqual(geom.normalize_polygon([(1, 1)], 0, 0), [])


class TestMaskMetrics(unittest.TestCase):
    def test_area_frac_and_iou(self):
        a = np.zeros((10, 10), bool)
        a[0:5, :] = True
        self.assertAlmostEqual(geom.mask_area_frac(a), 0.5)
        b = np.zeros((10, 10), bool)
        b[5:10, :] = True  # disjoint bottom half
        self.assertAlmostEqual(geom.mask_iou(a, a), 1.0)
        self.assertAlmostEqual(geom.mask_iou(a, b), 0.0)
        self.assertAlmostEqual(geom.mask_iou(a, np.zeros((10, 10), bool)), 0.0)


class TestBoxToXYXYNorm(unittest.TestCase):
    def test_clamps_and_normalizes(self):
        box = geom.box_to_xyxy_norm([10, 20, 90, 80], 100, 100)
        self.assertEqual(box, [0.1, 0.2, 0.9, 0.8])

    def test_degenerate_box_rejected(self):
        self.assertIsNone(geom.box_to_xyxy_norm([10, 10, 10.00001, 10.00001], 100, 100))


class TestProposePuddleBoxes(unittest.TestCase):
    def test_dark_smooth_region_proposed(self):
        # Daytime frame with a smooth dark patch (standing water look-alike).
        img = np.full((300, 400, 3), 120, dtype=np.uint8)
        img[220:260, 150:250] = 40
        boxes = geom.propose_puddle_boxes(img, horizon_frac=0.4, max_boxes=2)
        self.assertEqual(len(boxes), 1)
        x1, y1, x2, y2 = boxes[0]
        self.assertLess(x1, 0.45)
        self.assertGreater(x2, 0.55)
        self.assertGreater(y1, 0.6)

    def test_night_frame_suppressed(self):
        img = np.full((300, 400, 3), 20, dtype=np.uint8)
        img[220:260, 150:250] = 5
        self.assertEqual(geom.propose_puddle_boxes(img, horizon_frac=0.4), [])

    def test_no_region_below_horizon(self):
        img = np.full((300, 400, 3), 120, dtype=np.uint8)
        img[10:30, 150:250] = 40  # dark patch above the horizon is ignored
        self.assertEqual(geom.propose_puddle_boxes(img, horizon_frac=0.4), [])


if __name__ == "__main__":
    unittest.main()
