#!/usr/bin/env python3
"""
tools/cv/test_quantize_unit.py - Unit tests for quantize_int8.py and test_quantization.py.

Part of Sprint 18 Task 5 (Issue #528).
Hermetic tests covering math helpers, data reader, and argument validation.
"""

from __future__ import annotations

import math
import os
import tempfile
import unittest
from pathlib import Path

import numpy as np
from PIL import Image

from tools.cv.quantize_int8 import (
    CCTVCalibrationDataReader,
    compute_shannon_entropy,
    letterbox_image,
    collect_calibration_images,
)
from tools.cv.test_quantization import box_iou, nms_cpu


class TestQuantizationUnit(unittest.TestCase):
    def test_compute_shannon_entropy(self):
        # Empty data has 0 entropy
        self.assertEqual(compute_shannon_entropy(b""), 0.0)

        # Constant bytes have 0 entropy
        self.assertEqual(compute_shannon_entropy(b"\x00" * 100), 0.0)

        # Uniform distribution across 256 bytes has 8.0 entropy
        uniform_bytes = bytes(range(256)) * 4
        entropy = compute_shannon_entropy(uniform_bytes)
        self.assertAlmostEqual(entropy, 8.0, places=2)

    def test_letterbox_image(self):
        # Create dummy RGB image 200x100
        img = Image.new("RGB", (200, 100), color=(255, 0, 0))
        tensor = letterbox_image(img, target_size=(640, 640))

        # Shape should be [1, 3, 640, 640]
        self.assertEqual(tensor.shape, (1, 3, 640, 640))
        self.assertEqual(tensor.dtype, np.float32)

        # Values should be normalized in [0, 1]
        self.assertGreaterEqual(float(np.min(tensor)), 0.0)
        self.assertLessEqual(float(np.max(tensor)), 1.0)

    def test_box_iou(self):
        # Identical boxes have IoU 1.0
        b1 = np.array([10.0, 10.0, 50.0, 50.0])
        self.assertAlmostEqual(box_iou(b1, b1), 1.0)

        # Non-overlapping boxes have IoU 0.0
        b2 = np.array([100.0, 100.0, 150.0, 150.0])
        self.assertAlmostEqual(box_iou(b1, b2), 0.0)

        # Partial overlap (50% area overlap)
        # b1: [0, 0, 10, 10], area = 100
        # b3: [0, 0, 10, 5], area = 50, inter = 50, union = 100 -> IoU = 0.5
        b_full = np.array([0.0, 0.0, 10.0, 10.0])
        b_half = np.array([0.0, 0.0, 10.0, 5.0])
        self.assertAlmostEqual(box_iou(b_full, b_half), 0.5)

    def test_nms_cpu(self):
        boxes = np.array([
            [10.0, 10.0, 50.0, 50.0],
            [12.0, 12.0, 48.0, 48.0],  # Heavy overlap with box 0
            [100.0, 100.0, 200.0, 200.0],  # Disjoint
        ])
        scores = np.array([0.9, 0.8, 0.7])
        keep = nms_cpu(boxes, scores, iou_thresh=0.5)
        self.assertEqual(keep, [0, 2])

    def test_cctv_calibration_data_reader(self):
        with tempfile.TemporaryDirectory() as td:
            img_path = os.path.join(td, "test_calib.jpg")
            img = Image.new("RGB", (100, 100), color=(128, 128, 128))
            img.save(img_path)

            reader = CCTVCalibrationDataReader([img_path], input_name="images", target_size=(320, 320), max_samples=1)
            batch = reader.get_next()
            self.assertIsNotNone(batch)
            self.assertIn("images", batch)
            self.assertEqual(batch["images"].shape, (1, 3, 320, 320))

            # Next call returns None (exhausted)
            self.assertIsNone(reader.get_next())

            # Rewind resets iteration
            reader.rewind()
            batch2 = reader.get_next()
            self.assertIsNotNone(batch2)

    def test_collect_calibration_images(self):
        with tempfile.TemporaryDirectory() as td:
            sub = os.path.join(td, "subdir")
            os.makedirs(sub, exist_ok=True)
            for i in range(3):
                p = os.path.join(sub, f"frame_{i}.jpg")
                Image.new("RGB", (10, 10)).save(p)

            found = collect_calibration_images([Path(td)], max_samples=2)
            self.assertEqual(len(found), 2)


if __name__ == "__main__":
    unittest.main()
