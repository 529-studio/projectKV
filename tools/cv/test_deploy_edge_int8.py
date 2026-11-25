#!/usr/bin/env python3
"""
Unit tests for tools/cv/deploy_edge_int8.py (Issue #553 / Sprint 19 Task 5).
Verifies Rule 10 weight integrity enforcement (size > 1MB, Shannon entropy >= 3.0,
unique byte diversity) and reproducible SHA-256 manifest generation.
"""

import json
import os
import tempfile
import unittest
from pathlib import Path

from tools.cv.deploy_edge_int8 import (
    build_edge_manifest,
    compute_shannon_entropy,
    verify_model_integrity,
)


class TestDeployEdgeINT8(unittest.TestCase):
    def test_shannon_entropy_bounds(self):
        self.assertEqual(compute_shannon_entropy(b""), 0.0)
        self.assertEqual(compute_shannon_entropy(b"\x00" * 1024), 0.0)
        self.assertGreater(compute_shannon_entropy(os.urandom(65536)), 7.5)

    def test_rejects_small_model_under_1mb(self):
        with tempfile.TemporaryDirectory() as td:
            p = Path(td) / "tiny_int8.onnx"
            p.write_bytes(os.urandom(500_000))
            with self.assertRaisesRegex(ValueError, "fails Rule 10 >1MB threshold"):
                verify_model_integrity(p)

    def test_rejects_zero_filled_stub_model(self):
        with tempfile.TemporaryDirectory() as td:
            p = Path(td) / "stub_int8.onnx"
            p.write_bytes(b"\x00" * 1_200_000)
            with self.assertRaisesRegex(ValueError, "Shannon entropy"):
                verify_model_integrity(p)

    def test_accepts_valid_high_entropy_artifact_and_emits_manifest(self):
        with tempfile.TemporaryDirectory() as td:
            model_path = Path(td) / "yolov8n_640_int8.onnx"
            manifest_path = Path(td) / "edge_int8_manifest.json"
            # Generate 1.2 MB pseudo-random weights satisfying Rule 10 entropy & diversity
            model_path.write_bytes(os.urandom(1_200_000))

            manifest = build_edge_manifest(
                model_path=model_path,
                manifest_out=manifest_path,
                run_smoke_test=False,
            )

            self.assertEqual(manifest["precision"], "int8")
            self.assertEqual(manifest["target_provider"], "CPUExecutionProvider")
            self.assertEqual(manifest["artifact"]["filename"], "yolov8n_640_int8.onnx")
            self.assertGreater(manifest["artifact"]["shannon_entropy"], 7.5)
            self.assertEqual(len(manifest["artifact"]["sha256"]), 64)
            self.assertTrue(manifest_path.is_file())

            loaded = json.loads(manifest_path.read_text(encoding="utf-8"))
            self.assertEqual(loaded["artifact"]["sha256"], manifest["artifact"]["sha256"])


if __name__ == "__main__":
    unittest.main()
