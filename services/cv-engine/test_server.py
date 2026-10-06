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

from server import CVServer, CVRequestHandler


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


if __name__ == "__main__":
    unittest.main()
