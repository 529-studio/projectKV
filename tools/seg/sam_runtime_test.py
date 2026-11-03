#!/usr/bin/env python3
"""
sam_runtime_test.py — Unit tests for the ONNX runtime wrappers.

Uses duck-typed fake sessions so no onnxruntime installation or model weights
are required (AGENTS.md Rule 10: deterministic, repeatable verification).
"""

import sys
import unittest
from pathlib import Path

import numpy as np

sys.path.insert(0, str(Path(__file__).resolve().parent.parent.parent))

from tools.seg import sam_runtime  # noqa: E402


class _FakeInput:
    def __init__(self, name, shape=None, type_str="tensor(float)"):
        self.name = name
        self.shape = shape if shape is not None else []
        self.type = type_str


class FakeEncoderSession:
    def __init__(self, embedding_shape=(1, 256, 64, 64)):
        self.embedding_shape = embedding_shape
        self.calls = []

    def get_inputs(self):
        return [_FakeInput("input_image", ["image_height", "image_width", 3])]

    def run(self, outputs, feed):
        self.calls.append(feed)
        return [np.zeros(self.embedding_shape, dtype=np.float32)]


class FakeDecoderSession:
    """Returns a deterministic two-candidate mask set with known scores."""

    def __init__(self, h=100, w=200, declared_all=True):
        self.h, self.w = h, w
        self.declared_all = declared_all
        self.last_feed = None

    def get_inputs(self):
        names = ["image_embeddings", "point_coords", "point_labels", "mask_input",
                 "has_mask_input", "orig_im_size"]
        if self.declared_all:
            return [_FakeInput(n) for n in names]
        return [_FakeInput(n) for n in names[:3]]  # omit optional inputs

    def run(self, outputs, feed):
        self.last_feed = feed
        n = feed["point_coords"].shape[1]
        masks = np.zeros((1, 2, self.h, self.w), dtype=np.float32)
        masks[0, 0, 10:50, 20:80] = 1.0   # small box
        masks[0, 1, 0: self.h, 0: self.w] = 1.0  # full frame
        scores = np.array([[0.42, 0.97]], dtype=np.float32)
        return [masks, scores, np.zeros((1, 2, 256, 256), dtype=np.float32)]


class FakeDetectorSession:
    """Emits a YOLOv8 (1, 84, N) head with two cars (overlapping) and a truck."""

    def __init__(self, size=640):
        self.size = size

    def get_inputs(self):
        return [_FakeInput("images", [1, 3, self.size, self.size])]

    def run(self, outputs, feed):
        s = self.size
        # Channels-first (1, 84, N) with N > 84 like a real export; most
        # candidates carry zero confidence and are filtered out.
        pred = np.zeros((1, 84, 100), dtype=np.float32)
        pred[0, :4, 0] = [s / 2, s / 2, s / 2, s / 4]
        pred[0, 4 + 2, 0] = 0.9   # COCO car
        pred[0, :4, 1] = [s / 2 + 5, s / 2, s / 2, s / 4]
        pred[0, 4 + 2, 1] = 0.8   # COCO car, overlapping candidate 0
        pred[0, :4, 2] = [s / 6, s / 6, s / 8, s / 8]
        pred[0, 4 + 7, 2] = 0.7   # COCO truck
        pred[0, :4, 3] = [s / 2, s / 2, s / 3, s / 5]
        pred[0, 4 + 0, 3] = 0.85  # COCO person: no taxonomy slot, must be dropped
        return [pred]


class TestSamOnnxModel(unittest.TestCase):
    def _model(self, declared_all=True, h=100, w=200):
        encoder = FakeEncoderSession()
        decoder = FakeDecoderSession(h=h, w=w, declared_all=declared_all)
        model = sam_runtime.SamOnnxModel(
            "enc.onnx", "dec.onnx",
            session_factory=lambda path: encoder if path == "enc.onnx" else decoder,
        )
        return model, encoder, decoder

    def test_missing_weight_paths_fail_fast(self):
        with self.assertRaises(RuntimeError):
            sam_runtime.SamOnnxModel("", "dec.onnx", session_factory=lambda p: None)
        with self.assertRaises(RuntimeError):
            sam_runtime.SamOnnxModel("enc.onnx", "", session_factory=lambda p: None)

    def test_preprocess_frame_aspect_and_placement(self):
        model, _, _ = self._model()
        img = np.zeros((500, 1000, 3), dtype=np.uint8)
        img[250, 500] = 200
        canvas, scale, pads = model.preprocess_frame(img)
        self.assertEqual(canvas.shape, (1024, 1024, 3))
        self.assertAlmostEqual(scale, 1024 / 1000)
        # Longest side governs: width fills 1024, pad_x = 0, pad_y = (1024-512)/2.
        self.assertEqual(pads, (0, 256))
        y = int(250 * scale) + pads[1]
        x = int(500 * scale) + pads[0]
        # Fractional sampling coordinates interpolate; value stays near 200.
        self.assertAlmostEqual(float(canvas[y, x, 0]), 200.0, delta=10.0)

    def test_segment_box_maps_coords_and_picks_best_mask(self):
        model, encoder, decoder = self._model(h=200, w=400)
        embeddings = np.zeros((1, 256, 64, 64), dtype=np.float32)
        mask, score = model.segment_box(
            embeddings, [0.1, 0.2, 0.5, 0.8], scale=1.0, pads=(0, 0),
            orig_h=200, orig_w=400,
        )
        feed = decoder.last_feed
        np.testing.assert_allclose(
            feed["point_coords"], [[[40.0, 40.0], [200.0, 160.0]]], atol=1e-4
        )
        np.testing.assert_array_equal(feed["point_labels"], [[2.0, 3.0]])
        np.testing.assert_allclose(feed["orig_im_size"], [200.0, 400.0])
        # Best candidate is index 1 (score 0.97) = full-frame mask.
        self.assertAlmostEqual(score, 0.97, places=4)
        self.assertTrue(mask.all())
        self.assertEqual(mask.shape, (200, 400))

    def test_segment_points_all_include_labels(self):
        model, _, decoder = self._model()
        embeddings = np.zeros((1, 256, 64, 64), dtype=np.float32)
        mask, score = model.segment_points(
            embeddings, [(0.5, 0.5), (0.25, 0.75)], scale=1.0, pads=(0, 0),
            orig_h=100, orig_w=100,
        )
        feed = decoder.last_feed
        np.testing.assert_array_equal(feed["point_labels"], [[1.0, 1.0]])
        np.testing.assert_allclose(
            feed["point_coords"], [[[50.0, 50.0], [25.0, 75.0]]], atol=1e-4
        )
        self.assertAlmostEqual(score, 0.97, places=4)

    def test_decode_tolerates_unddeclared_optional_inputs(self):
        model, _, decoder = self._model(declared_all=False)
        embeddings = np.zeros((1, 256, 64, 64), dtype=np.float32)
        mask, _ = model.segment_box(
            embeddings, [0.0, 0.0, 1.0, 1.0], scale=1.0, pads=(0, 0),
            orig_h=100, orig_w=200,
        )
        self.assertIsNotNone(mask)
        self.assertNotIn("mask_input", decoder.last_feed)
        self.assertNotIn("has_mask_input", decoder.last_feed)

    def test_masks_thresholded_to_boolean(self):
        model, _, _ = self._model()
        embeddings = np.zeros((1, 256, 64, 64), dtype=np.float32)
        mask, _ = model.segment_points(
            embeddings, [(0.5, 0.5)], scale=1.0, pads=(0, 0), orig_h=100, orig_w=100,
        )
        self.assertEqual(mask.dtype, bool)


class TestYoloOnnxModel(unittest.TestCase):
    def test_missing_model_path_fail_fast(self):
        with self.assertRaises(RuntimeError):
            sam_runtime.YoloOnnxModel("", session_factory=lambda p: None)

    def test_detect_maps_taxonomy_and_suppresses_overlaps(self):
        detector = sam_runtime.YoloOnnxModel("det.onnx", session_factory=lambda p: FakeDetectorSession(640))
        img = np.zeros((480, 640, 3), dtype=np.uint8)
        results = detector.detect(img, conf=0.3, iou=0.45)
        classes = sorted(r["class"] for r in results)
        # Person (COCO 0) has no taxonomy slot; overlapping car suppressed.
        self.assertEqual(classes, ["car", "truck"])
        for r in results:
            self.assertGreaterEqual(r["score"], 0.3)
            for v in r["box_norm"]:
                self.assertGreaterEqual(v, 0.0)
                self.assertLessEqual(v, 1.0)

    def test_dynamic_input_size_defaults_to_640(self):
        session = FakeDetectorSession()
        session.get_inputs = lambda: [_FakeInput("images", [1, 3, "height", "width"])]
        detector = sam_runtime.YoloOnnxModel("det.onnx", session_factory=lambda p: session)
        self.assertEqual(detector.input_size, 640)


class TestResolveWeightPath(unittest.TestCase):
    def test_explicit_arg_wins(self):
        self.assertEqual(sam_runtime.resolve_weight_path("SOME_KEY", "/a/b.onnx"), "/a/b.onnx")

    def test_env_fallback(self):
        import os

        old = os.environ.get("SOME_KEY")
        os.environ["SOME_KEY"] = "/env/path.onnx"
        try:
            self.assertEqual(sam_runtime.resolve_weight_path("SOME_KEY"), "/env/path.onnx")
        finally:
            if old is None:
                os.environ.pop("SOME_KEY", None)
            else:
                os.environ["SOME_KEY"] = old

    def test_unset_returns_empty_string(self):
        import os

        old = os.environ.pop("SOME_KEY", None)
        try:
            self.assertEqual(sam_runtime.resolve_weight_path("SOME_KEY"), "")
        finally:
            if old is not None:
                os.environ["SOME_KEY"] = old


if __name__ == "__main__":
    unittest.main()
