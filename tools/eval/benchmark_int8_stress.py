#!/usr/bin/env python3
"""
tools/eval/benchmark_int8_stress.py - 60-Camera Concurrent Stream Stress Test & Latency Benchmark.
Issue #555 / Sprint 19 Task 7.

Simulates 60 concurrent municipal CCTV streams submitting frames to `cv-engine` (`POST /predict`),
measuring P50/P90/P95/P99 latency, throughput (FPS), queue shed rate (429), and process RSS memory.
"""

from __future__ import annotations

import argparse
import concurrent.futures
import datetime
import http.server
import io
import json
import logging
import math
import resource
import socketserver
import sys
import threading
import time
import urllib.error
import urllib.request
from pathlib import Path
from typing import Any, Dict, List, Optional, Tuple

logging.basicConfig(level=logging.INFO, format="%(asctime)s [%(levelname)s] %(message)s")
logger = logging.getLogger("benchmark_int8_stress")


def create_minimal_valid_jpeg() -> bytes:
    """Returns a minimal valid JPEG byte sequence (SOI + APP0 + SOF0 + EOI or PIL-rendered frame)."""
    try:
        from PIL import Image

        img = Image.new("RGB", (640, 360), color=(92, 102, 112))
        buf = io.BytesIO()
        img.save(buf, format="JPEG", quality=80)
        return buf.getvalue()
    except ImportError:
        # Minimal valid JPEG binary fallback (1x1 grayscale) when Pillow is absent
        return (
            b"\xff\xd8\xff\xe0\x00\x10JFIF\x00\x01\x01\x01\x00H\x00H\x00\x00"
            b"\xff\xdb\x00C\x00\x03\x02\x02\x02\x02\x02\x03\x02\x02\x02\x03\x03\x03\x03\x04\x06\x04\x04\x04\x04\x04\x08\x06\x06\x05\x06\t\x08\n\n\t\x08\t\t\n\x0c\x0f\x0c\n\x0b\x0e\x0b\t\t\r\x11\r\x0e\x0f\x10\x10\x11\x10\n\x0c\x12\x13\x12\x10\x13\x0f\x10\x10\x10"
            b"\xff\xc0\x00\x0b\x08\x00\x01\x00\x01\x01\x01\x11\x00"
            b"\xff\xc4\x00\x14\x00\x01\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\t"
            b"\xff\xc4\x00\x14\x10\x01\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00"
            b"\xff\xda\x00\x08\x01\x01\x00\x00?\x00T\xdf\xff\xd9"
        )


def compute_percentiles(latencies_ms: List[float]) -> Dict[str, float]:
    """Computes P50, P90, P95, P99, min, max, and mean over latency samples in ms."""
    if not latencies_ms:
        return {"p50": 0.0, "p90": 0.0, "p95": 0.0, "p99": 0.0, "min": 0.0, "max": 0.0, "mean": 0.0}
    sorted_vals = sorted(latencies_ms)
    n = len(sorted_vals)

    def pct(p: float) -> float:
        idx = min(n - 1, max(0, int(math.ceil((p / 100.0) * n)) - 1))
        return round(sorted_vals[idx], 3)

    return {
        "p50": pct(50.0),
        "p90": pct(90.0),
        "p95": pct(95.0),
        "p99": pct(99.0),
        "min": round(sorted_vals[0], 3),
        "max": round(sorted_vals[-1], 3),
        "mean": round(sum(sorted_vals) / n, 3),
    }


def _send_single_frame(
    endpoint: str,
    camera_id: str,
    frame_bytes: bytes,
    timeout_s: float,
    is_corridor: bool,
) -> Tuple[int, float, Optional[str]]:
    """Dispatches one POST /predict HTTP frame and returns (status_code, latency_ms, error)."""
    headers = {
        "Content-Type": "image/jpeg",
        "X-Camera-ID": camera_id,
        "X-Capture-Timestamp": str(time.time()),
        "X-Corridor-Priority": "1" if is_corridor else "0",
    }
    req = urllib.request.Request(endpoint, data=frame_bytes, headers=headers, method="POST")
    t0 = time.perf_counter()
    try:
        with urllib.request.urlopen(req, timeout=timeout_s) as resp:
            _ = resp.read()
            elapsed_ms = (time.perf_counter() - t0) * 1000.0
            return resp.status, elapsed_ms, None
    except urllib.error.HTTPError as err:
        elapsed_ms = (time.perf_counter() - t0) * 1000.0
        return err.code, elapsed_ms, None
    except Exception as exc:
        elapsed_ms = (time.perf_counter() - t0) * 1000.0
        return 0, elapsed_ms, str(exc)


class _LocalBenchmarkServer(socketserver.ThreadingMixIn, http.server.HTTPServer):
    daemon_threads = True
    allow_reuse_address = True
    request_queue_size = 128


def start_local_int8_harness_server() -> Tuple[_LocalBenchmarkServer, str, threading.Thread]:
    """Starts a lightweight concurrent HTTP server simulating INT8 quantized inference latency (~8ms)."""

    class Handler(http.server.BaseHTTPRequestHandler):
        def log_message(self, format: str, *args: Any) -> None:
            return

        def do_POST(self) -> None:
            length = int(self.headers.get("Content-Length", "0"))
            _ = self.rfile.read(length)
            # Simulate realistic INT8 CPU/ORT inference work (~4ms)
            time.sleep(0.004)
            body = json.dumps({
                "status": "ok",
                "precision": "int8",
                "severity": "CLEAR",
                "confidence": 0.92,
                "vehicle_count": 6,
            }).encode("utf-8")
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)

    srv = _LocalBenchmarkServer(("127.0.0.1", 0), Handler)
    port = srv.server_address[1]
    t = threading.Thread(target=srv.serve_forever, daemon=True)
    t.start()
    return srv, f"http://127.0.0.1:{port}/predict", t


def run_stress_benchmark(
    endpoint: str,
    concurrency: int = 60,
    total_requests: int = 300,
    timeout_s: float = 5.0,
    frame_bytes: Optional[bytes] = None,
) -> Dict[str, Any]:
    """Executes concurrent camera frame burst and aggregates latency, throughput, and status distribution."""
    if frame_bytes is None:
        frame_bytes = create_minimal_valid_jpeg()

    rss_before_kb = resource.getrusage(resource.RUSAGE_SELF).ru_maxrss
    latencies_ok: List[float] = []
    status_counts: Dict[int, int] = {}
    errors: List[str] = []

    wall_start = time.perf_counter()

    with concurrent.futures.ThreadPoolExecutor(max_workers=concurrency) as pool:
        futures = []
        for idx in range(total_requests):
            cam_id = f"cam_stress_{idx % concurrency:02d}"
            is_corridor = (idx % 4) == 0  # 25% arterial corridor priority
            futures.append(
                pool.submit(_send_single_frame, endpoint, cam_id, frame_bytes, timeout_s, is_corridor)
            )

        for fut in concurrent.futures.as_completed(futures):
            code, lat_ms, err = fut.result()
            status_counts[code] = status_counts.get(code, 0) + 1
            if code == 200:
                latencies_ok.append(lat_ms)
            if err:
                errors.append(err)

    wall_sec = max(0.001, time.perf_counter() - wall_start)
    rss_after_kb = resource.getrusage(resource.RUSAGE_SELF).ru_maxrss
    ok_count = status_counts.get(200, 0)
    shed_count = status_counts.get(429, 0)
    err_count = total_requests - ok_count - shed_count

    return {
        "timestamp": datetime.datetime.now(datetime.timezone.utc).isoformat(),
        "endpoint": endpoint,
        "concurrency": concurrency,
        "total_requests": total_requests,
        "wall_seconds": round(wall_sec, 3),
        "throughput_fps": round(ok_count / wall_sec, 2),
        "status_counts": {str(k): v for k, v in status_counts.items()},
        "ok_count": ok_count,
        "shed_429_count": shed_count,
        "error_count": err_count,
        "error_rate": round(err_count / max(1, total_requests), 4),
        "latency_ms": compute_percentiles(latencies_ok),
        "rss_delta_kb": max(0, rss_after_kb - rss_before_kb),
        "sample_errors": errors[:5],
    }


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--endpoint", default="http://127.0.0.1:8000/predict", help="CV engine /predict endpoint")
    parser.add_argument("--concurrency", type=int, default=60, help="Concurrent camera streams")
    parser.add_argument("--requests", type=int, default=300, help="Total requests across burst")
    parser.add_argument("--p95-threshold-ms", type=float, default=200.0, help="Max acceptable P95 latency (ms)")
    parser.add_argument("--local-sim-server", action="store_true", help="Spin up local HTTP harness server")
    parser.add_argument("--output-json", type=Path, default=None, help="Optional JSON report path")
    args = parser.parse_args()

    srv = None
    endpoint = args.endpoint
    if args.local_sim_server:
        srv, endpoint, _ = start_local_int8_harness_server()
        logger.info("Started local INT8 stress harness server at %s", endpoint)

    try:
        report = run_stress_benchmark(
            endpoint=endpoint,
            concurrency=args.concurrency,
            total_requests=args.requests,
        )
    finally:
        if srv is not None:
            srv.shutdown()
            srv.server_close()

    if args.output_json:
        args.output_json.parent.mkdir(parents=True, exist_ok=True)
        args.output_json.write_text(json.dumps(report, indent=2) + "\n", encoding="utf-8")

    logger.info("60-Camera INT8 Stress Benchmark Results:")
    logger.info("  Total Requests:  %d (concurrency=%d)", report["total_requests"], report["concurrency"])
    logger.info("  Throughput:      %.2f FPS", report["throughput_fps"])
    logger.info("  OK / Shed / Err: %d / %d / %d", report["ok_count"], report["shed_429_count"], report["error_count"])
    logger.info(
        "  Latency (ms):    P50=%.2f | P90=%.2f | P95=%.2f | P99=%.2f",
        report["latency_ms"]["p50"],
        report["latency_ms"]["p90"],
        report["latency_ms"]["p95"],
        report["latency_ms"]["p99"],
    )

    if report["error_rate"] > 0.01:
        logger.error("Error rate %.2f%% exceeds 1%% ceiling", report["error_rate"] * 100.0)
        return 1
    if report["latency_ms"]["p95"] > args.p95_threshold_ms:
        logger.error(
            "P95 latency %.2f ms exceeds %.2f ms SLA threshold",
            report["latency_ms"]["p95"],
            args.p95_threshold_ms,
        )
        return 1

    return 0


if __name__ == "__main__":
    sys.exit(main())
