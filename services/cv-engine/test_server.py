#!/usr/bin/env python3
"""
Unit and integration tests for services/cv-engine/server.py.
Verifies ThreadingHTTPServer concurrency, healthchecks, and resilient disconnect handling.
"""

from __future__ import annotations

import socket
import struct
import sys
import threading
import time
import unittest
import urllib.request
import urllib.error
import json

try:
    import numpy as np
    from PIL import Image
    HAS_CV_DEPS = True
except ImportError:
    HAS_CV_DEPS = False
    np = None
    Image = None

from server import (
    CVServer,
    CVRequestHandler,
    letterbox,
    nms_cpu,
    decode_mask_prototypes,
    calculate_water_ratio,
    calculate_motorcycle_submersion,
    triangulate_flood_verdict,
)


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

    def test_healthz_endpoint(self):
        req = urllib.request.Request(f"{self.base_url}/healthz")
        with urllib.request.urlopen(req, timeout=5.0) as resp:
            self.assertEqual(resp.status, 200)
            data = json.loads(resp.read().decode("utf-8"))
            self.assertEqual(data.get("status"), "healthy")
            self.assertEqual(data.get("service"), "cv-engine")

    def test_info_endpoint(self):
        req = urllib.request.Request(f"{self.base_url}/info")
        with urllib.request.urlopen(req, timeout=5.0) as resp:
            self.assertEqual(resp.status, 200)
            data = json.loads(resp.read().decode("utf-8"))
            self.assertEqual(data.get("service"), "projectkv-cv-engine")

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

        # 3. Static fallback: no motorcycles observed but water_ratio >= 0.35
        verdict_f, conf_f = triangulate_flood_verdict(
            water_ratio=0.40, max_delta_sub=0.0, depth_estimated_cm=0.0, motorcycles_observed=0
        )
        self.assertEqual(verdict_f, "FLOODED_SHALLOW")

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


if __name__ == "__main__":
    unittest.main()
