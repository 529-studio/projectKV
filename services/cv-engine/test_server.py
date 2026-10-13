#!/usr/bin/env python3
"""
Unit and integration tests for services/cv-engine/server.py.
Verifies ThreadingHTTPServer concurrency, healthchecks, and resilient disconnect handling.
"""

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

from server import CVServer, CVRequestHandler, letterbox, nms_cpu


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


if __name__ == "__main__":
    unittest.main()
