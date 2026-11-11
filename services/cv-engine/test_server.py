#!/usr/bin/env python3
"""
Unit and integration tests for services/cv-engine/server.py.
Verifies ThreadingHTTPServer concurrency, healthchecks, and resilient disconnect handling.
"""

from __future__ import annotations

import json
import os
import socket
import struct
import sys
import tempfile
import threading
import time
import unittest
import unittest.mock
import urllib.error
import urllib.request
from io import BytesIO
from pathlib import Path

# Support `python3 -m unittest services/cv-engine/test_server.py` from the repository
# root: this module imports its sibling `server` as a top-level module, which only works
# when the service directory is on sys.path. Discovery from inside the service directory
# already satisfies that, so the insert is a no-op there.
SERVICE_DIR = str(Path(__file__).resolve().parent)
if SERVICE_DIR not in sys.path:
    sys.path.insert(0, SERVICE_DIR)

try:
    import numpy as np
    from PIL import Image
    HAS_CV_DEPS = True
except ImportError:
    HAS_CV_DEPS = False
    np = None
    Image = None

import server
from server import (
    CVServer,
    CVRequestHandler,
    DET_MODEL_XML,
    SEG_MODEL_XML,
    DEFAULT_INPUT_RESOLUTION,
    WHEEL_SUBMERSION_DELTA,
    letterbox,
    nms_cpu,
    decode_mask_prototypes,
    calculate_water_ratio,
    calculate_motorcycle_submersion,
    resolve_input_resolution,
    triangulate_flood_verdict,
)
from hazard_classifier import AVOIDANCE_CONFIDENCE_CAP


def isolate_input_resolution_env(testcase: unittest.TestCase) -> None:
    """Remove ``CV_INPUT_RESOLUTION`` for one test, restoring it afterwards.

    The serving handler reads this variable from the real process environment, so an
    operator override exported in the developer's shell would silently change what the
    requests in this module resolve to. Every test that starts or drives the server pops
    it in ``setUp`` and restores the prior value on cleanup.
    """
    saved = os.environ.pop("CV_INPUT_RESOLUTION", None)

    def restore() -> None:
        os.environ.pop("CV_INPUT_RESOLUTION", None)
        if saved is not None:
            os.environ["CV_INPUT_RESOLUTION"] = saved

    testcase.addCleanup(restore)


class TestCVServer(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        # Bind on an ephemeral free port
        cls.server = CVServer(("127.0.0.1", 0), CVRequestHandler)
        cls.port = cls.server.server_address[1]
        cls.server_thread = threading.Thread(target=cls.server.serve_forever, daemon=True)
        cls.server_thread.start()
        cls.base_url = f"http://127.0.0.1:{cls.port}"

    @classmethod
    def tearDownClass(cls):
        cls.server.shutdown()
        cls.server.server_close()

    def setUp(self):
        isolate_input_resolution_env(self)

    def test_healthz_endpoint(self):
        req = urllib.request.Request(f"{self.base_url}/healthz")
        with urllib.request.urlopen(req, timeout=5.0) as resp:
            self.assertEqual(resp.status, 200)
            data = json.loads(resp.read().decode("utf-8"))
            self.assertEqual(data.get("status"), "healthy")
            self.assertEqual(data.get("service"), "cv-engine")

    def test_info_endpoint(self):
        # Operators use /info to confirm what the compiled graph can execute, so pin the
        # reported mode and shape instead of only checking that the keys exist: a handler
        # that swapped or dropped the values would still satisfy a presence assertion.
        # The module globals are read per request, so patching them here exercises the
        # real response path; whatever the process resolved at import time is restored.
        previous_mode = server.resolution_mode
        previous_shape = server.compiled_input_shape
        server.resolution_mode = "dynamic"
        server.compiled_input_shape = [1, 3, 416, 416]

        def restore() -> None:
            server.resolution_mode = previous_mode
            server.compiled_input_shape = previous_shape

        self.addCleanup(restore)

        req = urllib.request.Request(f"{self.base_url}/info")
        with urllib.request.urlopen(req, timeout=5.0) as resp:
            self.assertEqual(resp.status, 200)
            data = json.loads(resp.read().decode("utf-8"))
            self.assertEqual(data.get("service"), "projectkv-cv-engine")
            self.assertEqual(data.get("resolution_mode"), "dynamic")
            self.assertEqual(data.get("input_shape"), [1, 3, 416, 416])

    def test_head_endpoint(self):
        req = urllib.request.Request(f"{self.base_url}/healthz", method="HEAD")
        with urllib.request.urlopen(req, timeout=5.0) as resp:
            self.assertEqual(resp.status, 200)

    def test_predict_standby_or_inference(self):
        # Empty body returns 400
        req = urllib.request.Request(f"{self.base_url}/predict", data=b"", headers={"Content-Length": "0"})
        try:
            with urllib.request.urlopen(req, timeout=5.0) as resp:
                self.assertEqual(resp.status, 400)
        except urllib.error.HTTPError as exc:
            self.assertEqual(exc.code, 400)

        # Non-empty body returns either 503 (standby mode) or 200 (if OpenVINO loaded)
        dummy_data = b"dummy_image_payload"
        req = urllib.request.Request(
            f"{self.base_url}/predict",
            data=dummy_data,
            headers={"Content-Length": str(len(dummy_data)), "Content-Type": "application/octet-stream"}
        )
        try:
            with urllib.request.urlopen(req, timeout=5.0) as resp:
                self.assertIn(resp.status, (200, 503))
        except urllib.error.HTTPError as exc:
            self.assertIn(exc.code, (200, 503))

    def test_concurrent_requests_handled_in_parallel(self):
        """Verify that ThreadingHTTPServer processes 10 concurrent requests in parallel."""
        num_requests = 10
        results = []

        def worker(idx):
            req = urllib.request.Request(f"{self.base_url}/healthz")
            t0 = time.time()
            try:
                with urllib.request.urlopen(req, timeout=5.0) as resp:
                    status = resp.status
                    results.append((idx, status, time.time() - t0))
            except Exception as exc:
                results.append((idx, str(exc), time.time() - t0))

        threads = [threading.Thread(target=worker, args=(i,)) for i in range(num_requests)]
        t_start = time.time()
        for th in threads:
            th.start()
        for th in threads:
            th.join()
        total_time = time.time() - t_start

        self.assertEqual(len(results), num_requests)
        for r in results:
            self.assertEqual(r[1], 200, f"Request {r[0]} failed: {r[1]}")
        # All 10 requests should complete almost instantly on separate threads
        self.assertLess(total_time, 3.0, f"Concurrent requests took too long: {total_time:.2f}s")

    def test_abrupt_client_socket_disconnect_handling(self):
        """Simulate client dropping connection before response is sent; server must not raise uncaught error."""
        s = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        s.connect(("127.0.0.1", self.port))
        req = (
            b"POST /predict HTTP/1.1\r\n"
            b"Host: 127.0.0.1\r\n"
            b"Content-Length: 10\r\n"
            b"Content-Type: application/octet-stream\r\n\r\n"
            b"1234567890"
        )
        s.sendall(req)
        # Abruptly reset TCP connection via SO_LINGER (1, 0)
        s.setsockopt(socket.SOL_SOCKET, socket.SO_LINGER, struct.pack("ii", 1, 0))
        s.close()

        # Give server thread a moment to process and verify server remains healthy
        time.sleep(0.1)
        check_req = urllib.request.Request(f"{self.base_url}/healthz")
        with urllib.request.urlopen(check_req, timeout=5.0) as resp:
            self.assertEqual(resp.status, 200)


@unittest.skipUnless(HAS_CV_DEPS, "NumPy and Pillow required for preprocessing and NMS tests")
class TestPreprocessingAndNMS(unittest.TestCase):
    def test_letterbox_preserves_aspect_ratio_and_pads(self):
        # 16:9 test image (512x288) filled with white (255, 255, 255)
        orig_w, orig_h = 512, 288
        img = Image.new("RGB", (orig_w, orig_h), (255, 255, 255))

        target_size = (416, 416)
        letterboxed, scale, pad_x, pad_y = letterbox(img, target_size=target_size, fill_color=(114, 114, 114))

        self.assertEqual(letterboxed.size, target_size)
        expected_scale = 416.0 / 512.0
        self.assertAlmostEqual(scale, expected_scale, places=4)
        self.assertEqual(pad_x, 0)
        self.assertGreater(pad_y, 0)

        # Padding at top and bottom should have neutral gray (114, 114, 114)
        top_pixel = letterboxed.getpixel((200, 0))
        self.assertEqual(top_pixel, (114, 114, 114))
        bottom_pixel = letterboxed.getpixel((200, 415))
        self.assertEqual(bottom_pixel, (114, 114, 114))

        # Center content should preserve original image color (255, 255, 255)
        center_pixel = letterboxed.getpixel((200, 208))
        self.assertEqual(center_pixel, (255, 255, 255))

    def test_nms_cpu_suppresses_overlapping_boxes(self):
        # Two bounding boxes heavily overlapping with IoU > 0.45
        boxes = np.array([
            [100.0, 100.0, 50.0, 50.0],
            [102.0, 101.0, 48.0, 50.0],
        ], dtype=np.float32)
        scores = np.array([0.90, 0.75], dtype=np.float32)

        keep = nms_cpu(boxes, scores, iou_thresh=0.45)
        self.assertEqual(keep, [0])

    def test_nms_cpu_keeps_distinct_boxes(self):
        # Two non-overlapping bounding boxes
        boxes = np.array([
            [50.0, 50.0, 20.0, 20.0],
            [200.0, 200.0, 30.0, 30.0],
        ], dtype=np.float32)
        scores = np.array([0.80, 0.70], dtype=np.float32)

        keep = nms_cpu(boxes, scores, iou_thresh=0.45)
        self.assertEqual(keep, [0, 1])

    def test_nms_cpu_empty(self):
        boxes = np.zeros((0, 4), dtype=np.float32)
        scores = np.zeros((0,), dtype=np.float32)

        keep = nms_cpu(boxes, scores, iou_thresh=0.45)
        self.assertEqual(keep, [])


@unittest.skipUnless(HAS_CV_DEPS, "NumPy required for segmentation and triangulation tests")
class TestSegmentationAndTriangulation(unittest.TestCase):
    def test_decode_mask_prototypes(self):
        # 2 instances, 32 mask coefficients each
        coeffs = np.zeros((2, 32), dtype=np.float32)
        coeffs[0, 0] = 5.0   # strong activation on prototype 0
        coeffs[1, 1] = -5.0  # strong inhibition on prototype 1

        # Prototypes shape (32, 104, 104)
        proto = np.zeros((32, 104, 104), dtype=np.float32)
        proto[0, 50:, 50:] = 1.0  # bottom-right quadrant active
        proto[1, :50, :50] = 1.0  # top-left quadrant active

        masks = decode_mask_prototypes(coeffs, proto, binarize_thresh=0.5)
        self.assertEqual(masks.shape, (2, 104, 104))
        # Instance 0 should be active in bottom-right
        self.assertTrue(masks[0, 60, 60])
        self.assertFalse(masks[0, 20, 20])
        # Instance 1 has negative coefficient, should be suppressed
        self.assertFalse(masks[1, 20, 20])

        # Test empty input edge case
        empty_masks = decode_mask_prototypes(np.zeros((0, 32)), proto)
        self.assertEqual(empty_masks.shape, (0, 104, 104))

    def test_calculate_water_ratio(self):
        # 100x100 grid
        road_mask = np.zeros((100, 100), dtype=bool)
        road_mask[20:80, 20:80] = True  # 60x60 = 3600 road pixels

        water_mask = np.zeros((100, 100), dtype=bool)
        water_mask[50:80, 20:80] = True  # 30x60 = 1800 water pixels in road

        ratio = calculate_water_ratio(water_mask, road_mask)
        # Expected: 1800 / 3600 = 0.50
        self.assertAlmostEqual(ratio, 0.50, places=4)

        # Fallback when road_mask is None or empty
        ratio_fallback = calculate_water_ratio(water_mask, None)
        # Expected: 1800 / 10000 = 0.18
        self.assertAlmostEqual(ratio_fallback, 0.18, places=4)

        # Empty water mask
        ratio_empty = calculate_water_ratio(np.zeros((100, 100), dtype=bool), road_mask)
        self.assertEqual(ratio_empty, 0.0)

    def test_calculate_motorcycle_submersion(self):
        # 104x104 water mask
        water_mask = np.zeros((104, 104), dtype=bool)
        # Water covering y from row 70 to 104
        water_mask[70:104, :] = True

        # Motorcycle in 416x416 frame
        # cx=200, cy=300, w=60, h=100 -> xmin=170, ymin=250, xmax=230, ymax=350
        # In 104x104 grid (scale 0.25):
        # ymin = 250 * 0.25 = 62.5 ~ 63
        # ymax = 350 * 0.25 = 87.5 ~ 88
        # box_h = 100 (in 416px) or 25 (in 104px)
        # Water starts at row 70.
        # Waterline in box: row 70 to 88 -> height of water in box = 18 rows
        # delta_sub ~ 18 / 25 = 0.72
        # Depth ~ 0.72 * 110cm = 79.2 cm (submerged deep!)
        mc_boxes = np.array([[200.0, 300.0, 60.0, 100.0]], dtype=np.float32)

        delta_sub, depth_cm, submerged_count = calculate_motorcycle_submersion(
            mc_boxes, water_mask, target_img_size=(416, 416)
        )
        self.assertGreater(delta_sub, 0.50)
        self.assertGreater(depth_cm, 50.0)
        self.assertEqual(submerged_count, 1)

        # Dry motorcycle outside water area: cy=100 (well above row 70)
        dry_mc_boxes = np.array([[200.0, 100.0, 60.0, 80.0]], dtype=np.float32)
        delta_dry, depth_dry, count_dry = calculate_motorcycle_submersion(
            dry_mc_boxes, water_mask, target_img_size=(416, 416)
        )
        self.assertEqual(delta_dry, 0.0)
        self.assertEqual(depth_dry, 0.0)
        self.assertEqual(count_dry, 0)

    def test_triangulate_flood_verdict_decision_matrix(self):
        # 1. FLOODED_DEEP: water_ratio >= 0.15 and delta_sub >= 0.18 (depth >= 20cm)
        verdict, conf = triangulate_flood_verdict(
            water_ratio=0.25, max_delta_sub=0.22, depth_estimated_cm=24.2, motorcycles_observed=5
        )
        self.assertEqual(verdict, "FLOODED_DEEP")
        self.assertGreaterEqual(conf, 0.85)

        # 2. FLOODED_SHALLOW: water_ratio >= 0.15 but delta_sub < 0.18
        verdict_s, conf_s = triangulate_flood_verdict(
            water_ratio=0.20, max_delta_sub=0.08, depth_estimated_cm=8.8, motorcycles_observed=5
        )
        self.assertEqual(verdict_s, "FLOODED_SHALLOW")

        # 3. Rewritten for Issue #401 (spec §2.4, AC7/AC8). The old static fallback
        #    returned FLOODED_SHALLOW whenever no motorcycle was observed and the water
        #    ratio passed 0.35 — turning evidence of hazard into a clearance. A pool
        #    that blocks the lane with no through-traffic is now FLOODED_DEEP.
        verdict_f, conf_f = triangulate_flood_verdict(
            water_ratio=0.40,
            max_delta_sub=0.0,
            depth_estimated_cm=0.0,
            motorcycles_observed=0,
            pool_area_px=5200.0,
            observations=(),
            pool_top_y=None,
        )
        self.assertEqual(verdict_f, "FLOODED_DEEP")
        self.assertNotEqual(verdict_f, "FLOODED_SHALLOW")
        self.assertLessEqual(conf_f, AVOIDANCE_CONFIDENCE_CAP)

        # 4. WET (puddle): water_ratio >= 0.05
        verdict_w, _ = triangulate_flood_verdict(
            water_ratio=0.08, max_delta_sub=0.0, depth_estimated_cm=0.0, motorcycles_observed=2
        )
        self.assertEqual(verdict_w, "WET")

        # 5. WET (rain event with dry/slick asphalt)
        verdict_r, _ = triangulate_flood_verdict(
            water_ratio=0.02, max_delta_sub=0.0, depth_estimated_cm=0.0, motorcycles_observed=2, rain_event=True
        )
        self.assertEqual(verdict_r, "WET")

        # 6. CLEAR: dry asphalt
        verdict_c, conf_c = triangulate_flood_verdict(
            water_ratio=0.01, max_delta_sub=0.0, depth_estimated_cm=0.0, motorcycles_observed=2
        )
        self.assertEqual(verdict_c, "CLEAR")
        self.assertEqual(conf_c, 0.95)


class TestSegmentationModelSelection(unittest.TestCase):
    """Exercises server.select_model_xml() directly.

    This decides which weights the service runs: choosing the generic detector over
    the fine-tuned segmentation model silently drops mask output. Re-implementing the
    precedence loop inside a test (as the previous version did) asserts nothing about
    production code, so every case below calls the real function.
    """

    def setUp(self):
        self._tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self._tmp.cleanup)
        self.model_dir = Path(self._tmp.name)

    def _touch(self, name):
        path = self.model_dir / name
        path.write_bytes(b"<net/>")
        return str(path)

    def test_prefers_segmentation_weights_when_both_exist(self):
        self._touch(DET_MODEL_XML)
        seg = self._touch(SEG_MODEL_XML)
        self.assertEqual(server.select_model_xml(str(self.model_dir)), seg)

    def test_falls_back_to_detection_weights(self):
        det = self._touch(DET_MODEL_XML)
        self.assertEqual(server.select_model_xml(str(self.model_dir)), det)

    def test_uses_any_xml_when_names_are_unconventional(self):
        custom = self._touch("custom_city_model.xml")
        self.assertEqual(server.select_model_xml(str(self.model_dir)), custom)

    def test_multiple_unconventional_xmls_resolve_deterministically(self):
        # os.listdir order is filesystem-dependent, so an unsorted fallback would pick
        # a different model on a different host for the same directory contents.
        first = self._touch("aaa_model.xml")
        self._touch("zzz_model.xml")
        self.assertEqual(server.select_model_xml(str(self.model_dir)), first)
        self.assertEqual(
            server.select_model_xml(str(self.model_dir)),
            server.select_model_xml(str(self.model_dir)),
        )

    def test_ignores_non_xml_weight_files(self):
        self._touch("yolov8n.bin")
        self._touch("yolov8n.onnx")
        with self.assertLogs("cv-engine", level="INFO") as logs:
            self.assertIsNone(server.select_model_xml(str(self.model_dir)))
        self.assertIn("No OpenVINO model XML found", "\n".join(logs.output))

    def test_missing_directory_logs_and_returns_none(self):
        missing = self.model_dir / "not_mounted"
        with self.assertLogs("cv-engine", level="INFO") as logs:
            self.assertIsNone(server.select_model_xml(str(missing)))
        self.assertIn("does not exist", "\n".join(logs.output))


class TestFloodAssessmentContract(unittest.TestCase):
    """Asserts the flood_assessment wire contract against production code."""

    CONTRACT_KEYS = frozenset({
        "status",
        "water_surface_ratio",
        "wheel_submersion_detected",
        "max_submersion_depth_cm",
        "motorcycles_observed",
        "motorcycles_submerged",
        "puddle_polygon_count",
        # Added by Issue #401 (spec §5.1). The block always carries them so a client
        # never has to guess whether a missing key means "unmeasured" or "old build".
        "hazard_level",
        "hazard_basis",
        "depth_confidence",
        "calibration_ref",
    })

    def _build(self, **overrides):
        kwargs = {
            "verdict": "CLEAR",
            "water_ratio": 0.0,
            "max_delta_sub": 0.0,
            "depth_estimated_cm": 0.0,
            "motorcycles_observed": 0,
            "motorcycles_submerged": 0,
            "puddle_polygon_count": 0,
        }
        kwargs.update(overrides)
        return server.build_flood_assessment(**kwargs)

    def test_emits_exactly_the_documented_keys(self):
        self.assertEqual(set(self._build()), set(self.CONTRACT_KEYS))

    def test_rounds_and_passes_through_measurements(self):
        assessment = self._build(
            verdict="FLOODED_DEEP",
            water_ratio=0.18456,
            max_delta_sub=0.2222,
            depth_estimated_cm=24.24,
            motorcycles_observed=5,
            motorcycles_submerged=3,
            puddle_polygon_count=4,
        )
        self.assertEqual(assessment["status"], "FLOODED_DEEP")
        self.assertAlmostEqual(assessment["water_surface_ratio"], 0.1846)
        self.assertAlmostEqual(assessment["max_submersion_depth_cm"], 24.2)
        self.assertTrue(assessment["wheel_submersion_detected"])
        self.assertEqual(assessment["motorcycles_observed"], 5)
        self.assertEqual(assessment["motorcycles_submerged"], 3)
        self.assertEqual(assessment["puddle_polygon_count"], 4)

    def test_wheel_flag_flips_at_the_shared_threshold(self):
        below = self._build(max_delta_sub=WHEEL_SUBMERSION_DELTA - 0.001)
        at = self._build(max_delta_sub=WHEEL_SUBMERSION_DELTA)
        self.assertFalse(below["wheel_submersion_detected"])
        self.assertTrue(at["wheel_submersion_detected"])

    def test_wheel_flag_never_contradicts_the_verdict_engine(self):
        for delta in (0.0, 0.1, 0.17, 0.18, 0.4):
            verdict, _ = triangulate_flood_verdict(
                water_ratio=0.4, max_delta_sub=delta, depth_estimated_cm=0.0
            )
            assessment = self._build(verdict=verdict, water_ratio=0.4, max_delta_sub=delta)
            if assessment["wheel_submersion_detected"]:
                self.assertEqual(verdict, "FLOODED_DEEP", f"delta={delta}")

    def test_depth_alone_can_flood_without_wheel_submersion(self):
        # Layer 2 has two independent signals. Deep water measured by depth must still
        # be FLOODED_DEEP even when no wheel is under water, and the wheel flag must
        # stay False rather than being coerced to agree.
        verdict, _ = triangulate_flood_verdict(
            water_ratio=0.4, max_delta_sub=0.05, depth_estimated_cm=25.0
        )
        assessment = self._build(
            verdict=verdict, water_ratio=0.4, max_delta_sub=0.05, depth_estimated_cm=25.0
        )
        self.assertEqual(verdict, "FLOODED_DEEP")
        self.assertFalse(assessment["wheel_submersion_detected"])


class _FakeResults:
    """Minimal stand-in for OpenVINO's OVDict: subscriptable, but not a dict."""

    def __init__(self, outputs):
        self._outputs = outputs

    def __getitem__(self, key):
        return self._outputs[key]

    def __len__(self):
        return len(self._outputs)


class _FakeInput:
    def __init__(self, shape=(1, 3, 416, 416)):
        self.shape = tuple(shape)


class _FakeInferRequest:
    def __init__(self, output=None, recorded_feeds=None):
        self.output = output
        self.recorded_feeds = recorded_feeds if recorded_feeds is not None else []

    def infer(self, feeds):
        # Record what the handler actually fed the graph, not what it intended to.
        self.recorded_feeds.append({key: np.asarray(value).shape for key, value in feeds.items()})
        if self.output is not None:
            return _FakeResults(self.output)
        # 42 rows = the 6-class segmentation head; all scores below threshold.
        return _FakeResults({0: np.zeros((1, 42, 8400), dtype=np.float32)})


class _FakeCompiledModel:
    def __init__(self, output=None, shape=(1, 3, 416, 416)):
        self.output = output
        self.input_shape = tuple(shape)
        self.recorded_feeds = []

    @property
    def resolution_mode(self):
        return "dynamic" if server.spatial_dims_are_dynamic(self.input_shape) else "static"

    def input(self, index):
        return _FakeInput(self.input_shape)

    def create_infer_request(self):
        return _FakeInferRequest(self.output, self.recorded_feeds)


@unittest.skipUnless(HAS_CV_DEPS, "numpy and Pillow are required for the inference payload test")
class TestPredictPayload(unittest.TestCase):
    """Drives a real POST /predict and inspects the JSON the handler emits."""

    @classmethod
    def setUpClass(cls):
        cls.httpd = CVServer(("127.0.0.1", 0), CVRequestHandler)
        cls.port = cls.httpd.server_address[1]
        cls.httpd_thread = threading.Thread(target=cls.httpd.serve_forever, daemon=True)
        cls.httpd_thread.start()
        cls.base_url = f"http://127.0.0.1:{cls.port}"

    @classmethod
    def tearDownClass(cls):
        cls.httpd.shutdown()
        cls.httpd.server_close()

    def setUp(self):
        # The handler reads CV_INPUT_RESOLUTION from the process environment; isolate it
        # so a host-shell override cannot change what these payload assertions see.
        isolate_input_resolution_env(self)
        saved = server.compiled_model
        self.model = _FakeCompiledModel()
        server.compiled_model = self.model
        self.addCleanup(setattr, server, "compiled_model", saved)

    def _use_model(self, shape, output=None):
        self.model = _FakeCompiledModel(output=output, shape=shape)
        server.compiled_model = self.model
        return self.model

    def _post_predict(self, extra_headers=None):
        buffer = BytesIO()
        Image.new("RGB", (32, 32), (90, 90, 90)).save(buffer, format="JPEG")
        headers = {"Content-Type": "image/jpeg"}
        if extra_headers:
            headers.update(extra_headers)
        request = urllib.request.Request(
            f"{self.base_url}/predict",
            data=buffer.getvalue(),
            headers=headers,
            method="POST",
        )
        with urllib.request.urlopen(request, timeout=5.0) as response:
            return response.status, json.loads(response.read().decode("utf-8"))

    def test_predict_reports_default_resolution_416(self):
        status, payload = self._post_predict()
        self.assertEqual(status, 200)
        self.assertIn("input_resolution", payload)
        self.assertEqual(payload["input_resolution"], [416, 416])

    def test_predict_honors_target_resolution_header_320_when_dynamic(self):
        self._use_model(shape=(1, 3, -1, -1))
        status, payload = self._post_predict(extra_headers={"X-Target-Resolution": "320"})
        self.assertEqual(status, 200)
        self.assertEqual(payload["input_resolution"], [320, 320])
        self.assertEqual(self.model.recorded_feeds, [{0: (1, 3, 320, 320)}])

    def test_predict_static_model_serves_baked_shape_and_reports_it(self):
        # A static IR cannot execute 320 px. The handler must feed the shape in the
        # graph and report that shape, never the requested one.
        self._use_model(shape=(1, 3, 416, 416))
        with self.assertLogs("cv-engine", level="WARNING") as logs:
            status, payload = self._post_predict(extra_headers={"X-Target-Resolution": "320"})
        self.assertEqual(status, 200)
        self.assertEqual(payload["input_resolution"], [416, 416])
        self.assertEqual(self.model.recorded_feeds, [{0: (1, 3, 416, 416)}])
        self.assertIn("static", "\n".join(logs.output))

    def test_predict_ignores_640_header_and_falls_back_to_416(self):
        # 640 px is an operator environment override only. A request header must not
        # be able to quadruple the pixel count relative to 320 px.
        self._use_model(shape=(1, 3, -1, -1))
        status, payload = self._post_predict(extra_headers={"X-Target-Resolution": "640"})
        self.assertEqual(status, 200)
        self.assertEqual(payload["input_resolution"], [416, 416])
        self.assertEqual(self.model.recorded_feeds, [{0: (1, 3, 416, 416)}])

    def test_predict_reports_the_shape_actually_fed_to_the_model(self):
        # Guards the wire contract directly: payload resolution must equal the tensor
        # received by infer(), so a squashed resize can never be reported as served.
        self._use_model(shape=(1, 3, -1, -1))
        status, payload = self._post_predict(extra_headers={"X-Target-Resolution": "416"})
        self.assertEqual(status, 200)
        fed_shape = self.model.recorded_feeds[-1][0]
        self.assertEqual(list(fed_shape[2:]), payload["input_resolution"])

    def test_predict_rejects_out_of_contract_resolution_header(self):
        self._use_model(shape=(1, 3, -1, -1))
        status, payload = self._post_predict(extra_headers={"X-Target-Resolution": "999"})
        self.assertEqual(status, 200)
        self.assertEqual(payload["input_resolution"], [416, 416])

    def test_response_carries_the_contract_block(self):
        status, payload = self._post_predict()
        self.assertEqual(status, 200)
        assessment = payload["flood_assessment"]
        self.assertEqual(set(assessment), set(TestFloodAssessmentContract.CONTRACT_KEYS))
        self.assertEqual(assessment["status"], payload["flood_severity"])
        self.assertFalse(assessment["wheel_submersion_detected"])
        self.assertEqual(assessment["motorcycles_observed"], 0)
        self.assertEqual(assessment["puddle_polygon_count"], 0)

    def test_response_carries_the_hazard_fields_unmeasured_never_zero(self):
        # AC13/E1: the fake model reports no reference object, no motorcycle and no
        # pool, so the depth is unmeasured. It must serialise as null — a 0.0 here
        # would read as "dry, verified" and reproduce the defect #401 removes.
        status, payload = self._post_predict()
        self.assertEqual(status, 200)
        for key in (
            "flood_depth_cm",
            "flood_hazard_level",
            "flood_hazard_basis",
            "flood_depth_confidence",
            "calibration_ref",
        ):
            self.assertIn(key, payload)
        self.assertIsNone(payload["flood_depth_cm"])
        self.assertIsNone(payload["flood_hazard_level"])
        self.assertEqual(payload["flood_hazard_basis"], "NONE")
        self.assertEqual(payload["flood_depth_confidence"], "UNMEASURED")
        self.assertEqual(payload["calibration_ref"], "NONE")

    def test_detection_only_model_with_motorcycle_never_claims_a_measured_depth(self):
        # B2/AC10: a COCO detection head has no mask prototypes, so `water_mask` is
        # None and a detected motorcycle cannot serve as a ruler. The payload must
        # report calibration_ref NONE and a null depth, never a fabricated
        # "0.0 cm / MOTORCYCLE_110CM" measurement of a scene nobody measured.
        # COCO layout is (1, 4 + 80 classes, anchors): no mask prototypes, so the
        # handler leaves `water_mask` as None.
        data = np.zeros((1, 84, 100), dtype=np.float32)
        data[0, 0, 0] = 208.0  # cx of the single anchor
        data[0, 1, 0] = 300.0  # cy
        data[0, 2, 0] = 40.0  # w
        data[0, 3, 0] = 80.0  # h
        data[0, 7, 0] = 0.9  # COCO class 3 = motorcycle score
        self._use_model(shape=(1, 3, -1, -1), output={0: data})

        status, payload = self._post_predict()

        self.assertEqual(status, 200)
        self.assertEqual(payload["vehicle_counts"]["motorcycle"], 1)
        self.assertEqual(payload["calibration_ref"], "NONE")
        self.assertIsNone(payload["flood_depth_cm"])
        self.assertIsNone(payload["flood_hazard_level"])
        self.assertEqual(payload["flood_hazard_basis"], "NONE")
        self.assertEqual(payload["flood_depth_confidence"], "UNMEASURED")

    def test_handler_delegates_to_build_flood_assessment(self):
        # If the handler ever re-inlines the dict, the contract tests stop covering the
        # response and this test fails.
        sentinel = {"status": "SENTINEL"}
        with unittest.mock.patch.object(
            server, "build_flood_assessment", return_value=sentinel
        ) as spy:
            status, payload = self._post_predict()
        self.assertEqual(status, 200)
        self.assertEqual(payload["flood_assessment"], sentinel)
        spy.assert_called_once()

    def _post_predict_multipart(self, frame_bytes: bytes, meta_dict: dict):
        boundary = "----WebKitFormBoundaryTestDirectional1234"
        body = (
            f"--{boundary}\r\n"
            f'Content-Disposition: form-data; name="frame"; filename="frame.jpg"\r\n'
            f"Content-Type: image/jpeg\r\n\r\n"
        ).encode("latin1") + frame_bytes + (
            f"\r\n--{boundary}\r\n"
            f'Content-Disposition: form-data; name="meta"\r\n'
            f"Content-Type: application/json\r\n\r\n"
            f"{json.dumps(meta_dict)}\r\n"
            f"--{boundary}--\r\n"
        ).encode("utf-8")

        request = urllib.request.Request(
            f"{self.base_url}/predict",
            data=body,
            headers={"Content-Type": f"multipart/form-data; boundary={boundary}"},
            method="POST",
        )
        with urllib.request.urlopen(request, timeout=5.0) as response:
            return response.status, json.loads(response.read().decode("utf-8"))

    def test_predict_multipart_with_regions_emits_per_direction_payload(self):
        buffer = BytesIO()
        Image.new("RGB", (32, 32), (90, 90, 90)).save(buffer, format="JPEG")
        meta = {
            "regions": [
                {
                    "region_id": "test_cam_dir_a",
                    "direction": "a",
                    "polygon": [[0.0, 0.0], [0.5, 0.0], [0.5, 1.0], [0.0, 1.0]],
                    "capacity_share": 0.5,
                },
                {
                    "region_id": "test_cam_dir_b",
                    "direction": "b",
                    "polygon": [[0.5, 0.0], [1.0, 0.0], [1.0, 1.0], [0.5, 1.0]],
                    "capacity_share": 0.5,
                },
            ]
        }
        status, payload = self._post_predict_multipart(buffer.getvalue(), meta)
        self.assertEqual(status, 200)
        self.assertIn("regions", payload)
        self.assertEqual(len(payload["regions"]), 2)
        r0 = payload["regions"][0]
        self.assertEqual(r0["region_id"], "test_cam_dir_a")
        self.assertEqual(r0["direction"], "a")
        self.assertEqual(r0["vehicles_detected"], 0)
        self.assertEqual(r0["water_ratio"], 0.0)

    def test_predict_directional_vehicle_assignment(self):
        # Set up mock detection output with 2 vehicles:
        # Vehicle 1: Car in direction A (left half: cx=100, cy=300, w=40, h=40)
        # Vehicle 2: Truck in direction B (right half: cx=300, cy=300, w=40, h=40)
        mock_output = np.zeros((1, 42, 8400), dtype=np.float32)
        # Det 0: Car (class idx 7)
        mock_output[0, 0, 0] = 100.0  # cx
        mock_output[0, 1, 0] = 300.0  # cy
        mock_output[0, 2, 0] = 40.0   # w
        mock_output[0, 3, 0] = 40.0   # h
        mock_output[0, 7, 0] = 0.95   # car score > CONF_THRESH

        # Det 1: Truck (class idx 9)
        mock_output[0, 0, 1] = 300.0  # cx
        mock_output[0, 1, 1] = 300.0  # cy
        mock_output[0, 2, 1] = 40.0   # w
        mock_output[0, 3, 1] = 40.0   # h
        mock_output[0, 9, 1] = 0.92   # truck score > CONF_THRESH

        server.compiled_model = _FakeCompiledModel(output={0: mock_output})

        buffer = BytesIO()
        Image.new("RGB", (32, 32), (90, 90, 90)).save(buffer, format="JPEG")
        meta = {
            "regions": [
                {
                    "region_id": "cam_dir_a",
                    "direction": "a",
                    "polygon": [[0.0, 0.0], [0.5, 0.0], [0.5, 1.0], [0.0, 1.0]],
                    "capacity_share": 0.5,
                },
                {
                    "region_id": "cam_dir_b",
                    "direction": "b",
                    "polygon": [[0.5, 0.0], [1.0, 0.0], [1.0, 1.0], [0.5, 1.0]],
                    "capacity_share": 0.5,
                },
            ]
        }
        status, payload = self._post_predict_multipart(buffer.getvalue(), meta)
        self.assertEqual(status, 200)
        self.assertIn("regions", payload)
        self.assertEqual(len(payload["regions"]), 2)

        dir_a = payload["regions"][0]
        self.assertEqual(dir_a["direction"], "a")
        self.assertEqual(dir_a["vehicle_counts"]["car"], 1)
        self.assertEqual(dir_a["vehicle_counts"]["truck"], 0)
        self.assertEqual(dir_a["vehicles_detected"], 1)

        dir_b = payload["regions"][1]
        self.assertEqual(dir_b["direction"], "b")
        self.assertEqual(dir_b["vehicle_counts"]["car"], 0)
        self.assertEqual(dir_b["vehicle_counts"]["truck"], 1)
        self.assertEqual(dir_b["vehicles_detected"], 1)

        # Global counts reflect both
        self.assertEqual(payload["vehicle_counts"]["car"], 1)
        self.assertEqual(payload["vehicle_counts"]["truck"], 1)
        self.assertEqual(payload["vehicles_detected"], 2)


class TestBoxPolygonAnchor(unittest.TestCase):
    """Coordinate normalization for directional polygon assignment (ADR-012 D1)."""

    def test_normal_box_produces_normalized_bottom_center(self):
        nx, ny = server.box_polygon_anchor(100.0, 200.0, 40.0, 20.0, 400, 400)
        self.assertAlmostEqual(nx, 0.25)
        self.assertAlmostEqual(ny, 210.0 / 400.0)

    def test_anchor_always_stays_inside_the_unit_square(self):
        # Boxes may extend past the frame in any direction after letterbox decoding.
        cases = [
            (-500.0, -500.0, 40.0, 40.0),
            (-500.0, 5000.0, 40.0, 40.0),
            (5000.0, -5000.0, 40.0, 40.0),
            (5000.0, 5000.0, 40.0, 40.0),
            (208.0, 208.0, 900.0, 900.0),
        ]
        for cx, cy, w, h in cases:
            nx, ny = server.box_polygon_anchor(cx, cy, w, h, 416, 416)
            self.assertGreaterEqual(nx, 0.0, (cx, cy, w, h))
            self.assertLessEqual(nx, 1.0, (cx, cy, w, h))
            self.assertGreaterEqual(ny, 0.0, (cx, cy, w, h))
            self.assertLessEqual(ny, 1.0, (cx, cy, w, h))

    def test_degenerate_image_dimensions_return_origin(self):
        self.assertEqual(server.box_polygon_anchor(10.0, 10.0, 2.0, 2.0, 0, 416), (0.0, 0.0))
        self.assertEqual(server.box_polygon_anchor(10.0, 10.0, 2.0, 2.0, 416, -1), (0.0, 0.0))


class TestLoadSheddingResolution(unittest.TestCase):
    """The load-shedding decision must come from production code, not a test copy (L66)."""

    def test_header_degrades_to_320_and_recovers_to_416(self):
        self.assertEqual(resolve_input_resolution("320", None, [1, 3, 416, 416]), (320, 320))
        self.assertEqual(resolve_input_resolution("416", "320", [1, 3, 416, 416]), (416, 416))

    def test_header_takes_precedence_over_environment(self):
        self.assertEqual(resolve_input_resolution("320", "640", [1, 3, 416, 416]), (320, 320))

    def test_environment_override_when_no_header(self):
        self.assertEqual(resolve_input_resolution(None, "320", [1, 3, 416, 416]), (320, 320))

    def test_model_shape_is_used_when_no_overrides(self):
        self.assertEqual(resolve_input_resolution(None, None, [1, 3, 640, 640]), (640, 640))

    def test_default_when_nothing_available(self):
        self.assertEqual(
            resolve_input_resolution(None, None, None),
            (DEFAULT_INPUT_RESOLUTION, DEFAULT_INPUT_RESOLUTION),
        )
        self.assertEqual(
            resolve_input_resolution("", "", [1, 3, 0, 0]),
            (DEFAULT_INPUT_RESOLUTION, DEFAULT_INPUT_RESOLUTION),
        )

    def test_out_of_contract_values_are_rejected_not_adopted(self):
        # An arbitrary resolution must never silently resize the model.
        self.assertEqual(resolve_input_resolution("999", None, [1, 3, 416, 416]), (416, 416))
        self.assertEqual(resolve_input_resolution("abc", None, [1, 3, 416, 416]), (416, 416))
        self.assertEqual(resolve_input_resolution(None, "512", [1, 3, 416, 416]), (416, 416))
        self.assertEqual(resolve_input_resolution("-320", None, [1, 3, 416, 416]), (416, 416))

    def test_contract_constants_match_policy(self):
        # Each control carries its own whitelist: the client-reachable header cannot
        # request 640 px, while the operator-scoped environment override can.
        self.assertEqual(server.HEADER_INPUT_RESOLUTIONS, (320, 416))
        self.assertEqual(server.ENV_INPUT_RESOLUTIONS, (320, 416, 640))


if __name__ == "__main__":
    unittest.main()
