#!/usr/bin/env python3
"""
Storm-Burst Disk I/O Benchmark & Simulation Tool for local-homesrv.

Simulates concurrent multi-tenant storage workloads on Debian/Docker hosts:
  1. Archiver: Sequential burst writing of camera JPEG frames (50KB - 100KB).
  2. SQLite WAL: Synchronous transactional verdict updates with fsync.
  3. Valhalla: Random tile block reads (64KB - 256KB).
  4. CV Worker: Snapshot frame reading for inference pipeline.

Measures throughput (MB/s), IOPS, and P50/P90/P95/P99 latency across
Normal Baseline and Peak Storm-Burst conditions.
"""

import argparse
import json
import os
import sqlite3
import statistics
import tempfile
import threading
import time
from typing import Any, Dict, List


class WorkerMetrics:
    def __init__(self, name: str):
        self.name = name
        self.latencies_ms: List[float] = []
        self.bytes_transferred: int = 0
        self.operations_count: int = 0
        self.errors_count: int = 0
        self.lock = threading.Lock()

    def record(self, latency_ms: float, byte_count: int):
        with self.lock:
            self.latencies_ms.append(latency_ms)
            self.bytes_transferred += byte_count
            self.operations_count += 1

    def record_error(self):
        with self.lock:
            self.errors_count += 1

    def summarize(self, duration_sec: float) -> Dict[str, Any]:
        with self.lock:
            if not self.latencies_ms:
                return {
                    "operations": 0,
                    "errors": self.errors_count,
                    "throughput_mb_s": 0.0,
                    "iops": 0.0,
                    "latency_p50_ms": 0.0,
                    "latency_p90_ms": 0.0,
                    "latency_p95_ms": 0.0,
                    "latency_p99_ms": 0.0,
                    "latency_max_ms": 0.0,
                }
            sorted_lat = sorted(self.latencies_ms)
            n = len(sorted_lat)

            def pct(p: float) -> float:
                idx = min(int(n * p), n - 1)
                return sorted_lat[idx]

            throughput_mb = (self.bytes_transferred / (1024 * 1024)) / max(duration_sec, 0.001)
            iops = self.operations_count / max(duration_sec, 0.001)

            return {
                "operations": self.operations_count,
                "errors": self.errors_count,
                "throughput_mb_s": round(throughput_mb, 2),
                "iops": round(iops, 1),
                "latency_p50_ms": round(pct(0.50), 2),
                "latency_p90_ms": round(pct(0.90), 2),
                "latency_p95_ms": round(pct(0.95), 2),
                "latency_p99_ms": round(pct(0.99), 2),
                "latency_max_ms": round(sorted_lat[-1], 2),
            }


def run_archiver_worker(target_dir: str, duration_sec: float, interval_sec: float, frame_size_kb: int, metrics: WorkerMetrics, stop_event: threading.Event):
    """Simulates snapshot archiver writing JPEG files with periodic sync."""
    frame_data = b"X" * (frame_size_kb * 1024)
    arch_dir = os.path.join(target_dir, "archiver")
    os.makedirs(arch_dir, exist_ok=True)
    idx = 0

    end_time = time.time() + duration_sec
    while not stop_event.is_set() and time.time() < end_time:
        t0 = time.perf_counter()
        file_path = os.path.join(arch_dir, f"frame_{idx:06d}.jpg")
        try:
            with open(file_path, "wb") as f:
                f.write(frame_data)
                f.flush()
                os.fsync(f.fileno())
            t1 = time.perf_counter()
            metrics.record((t1 - t0) * 1000.0, len(frame_data))
        except Exception:
            metrics.record_error()
        idx += 1
        if interval_sec > 0:
            time.sleep(interval_sec)


def run_sqlite_worker(target_dir: str, duration_sec: float, interval_sec: float, metrics: WorkerMetrics, stop_event: threading.Event):
    """Simulates SQLite WAL transaction updates with synchronous commits."""
    db_path = os.path.join(target_dir, "test_verdicts.db")
    conn = sqlite3.connect(db_path, timeout=10.0)
    cursor = conn.cursor()
    cursor.execute("PRAGMA journal_mode=WAL;")
    cursor.execute("PRAGMA synchronous=NORMAL;")
    cursor.execute("""
        CREATE TABLE IF NOT EXISTS verdicts (
            camera_id TEXT PRIMARY KEY,
            severity TEXT,
            flood_severity TEXT,
            confidence REAL,
            updated_at TIMESTAMP
        );
    """)
    conn.commit()

    camera_ids = [f"cam_{i:04d}" for i in range(100)]
    idx = 0

    end_time = time.time() + duration_sec
    while not stop_event.is_set() and time.time() < end_time:
        cam_id = camera_ids[idx % len(camera_ids)]
        t0 = time.perf_counter()
        try:
            cursor.execute("""
                INSERT OR REPLACE INTO verdicts (camera_id, severity, flood_severity, confidence, updated_at)
                VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP);
            """, (cam_id, "CONGESTED" if idx % 2 == 0 else "MODERATE", "FLOODED_SHALLOW", 0.92))
            conn.commit()
            t1 = time.perf_counter()
            # Approximate WAL row write size ~ 512 bytes
            metrics.record((t1 - t0) * 1000.0, 512)
        except Exception:
            metrics.record_error()
        idx += 1
        if interval_sec > 0:
            time.sleep(interval_sec)

    conn.close()


def run_valhalla_worker(target_dir: str, duration_sec: float, interval_sec: float, metrics: WorkerMetrics, stop_event: threading.Event):
    """Simulates random reading of routing tile chunks from disk."""
    tile_file = os.path.join(target_dir, "routing_tiles.bin")
    chunk_size = 64 * 1024  # 64KB tile
    total_chunks = 200
    if not os.path.exists(tile_file):
        with open(tile_file, "wb") as f:
            f.write(b"T" * (chunk_size * total_chunks))

    end_time = time.time() + duration_sec
    idx = 0
    with open(tile_file, "rb") as f:
        while not stop_event.is_set() and time.time() < end_time:
            offset = ((idx * 37) % total_chunks) * chunk_size
            t0 = time.perf_counter()
            try:
                f.seek(offset)
                data = f.read(chunk_size)
                t1 = time.perf_counter()
                metrics.record((t1 - t0) * 1000.0, len(data))
            except Exception:
                metrics.record_error()
            idx += 1
            if interval_sec > 0:
                time.sleep(interval_sec)


def run_cv_worker(target_dir: str, duration_sec: float, interval_sec: float, metrics: WorkerMetrics, stop_event: threading.Event):
    """Simulates CV inference pipeline reading snapshot image bytes."""
    cv_dir = os.path.join(target_dir, "cv_frames")
    os.makedirs(cv_dir, exist_ok=True)
    frame_size = 75 * 1024
    frame_files = []
    for i in range(20):
        fp = os.path.join(cv_dir, f"sample_{i:02d}.jpg")
        with open(fp, "wb") as f:
            f.write(b"C" * frame_size)
        frame_files.append(fp)

    end_time = time.time() + duration_sec
    idx = 0
    while not stop_event.is_set() and time.time() < end_time:
        fp = frame_files[idx % len(frame_files)]
        t0 = time.perf_counter()
        try:
            with open(fp, "rb") as f:
                data = f.read()
            t1 = time.perf_counter()
            metrics.record((t1 - t0) * 1000.0, len(data))
        except Exception:
            metrics.record_error()
        idx += 1
        if interval_sec > 0:
            time.sleep(interval_sec)


def execute_workload(mode: str, duration_sec: float, base_dir: str) -> Dict[str, Any]:
    """Executes either 'baseline' or 'storm-burst' multi-threaded workload."""
    stop_event = threading.Event()
    metrics = {
        "archiver": WorkerMetrics("archiver"),
        "sqlite": WorkerMetrics("sqlite"),
        "valhalla": WorkerMetrics("valhalla"),
        "cv_worker": WorkerMetrics("cv_worker"),
    }

    if mode == "baseline":
        # Normal operating conditions: gentle intervals
        threads = [
            threading.Thread(target=run_archiver_worker, args=(base_dir, duration_sec, 2.0, 60, metrics["archiver"], stop_event)),
            threading.Thread(target=run_sqlite_worker, args=(base_dir, duration_sec, 1.0, metrics["sqlite"], stop_event)),
            threading.Thread(target=run_valhalla_worker, args=(base_dir, duration_sec, 1.5, metrics["valhalla"], stop_event)),
            threading.Thread(target=run_cv_worker, args=(base_dir, duration_sec, 2.0, metrics["cv_worker"], stop_event)),
        ]
    else:  # storm-burst
        # Peak storm concurrent flood load: heavy concurrent threads, tight loop
        threads = [
            threading.Thread(target=run_archiver_worker, args=(base_dir, duration_sec, 0.05, 80, metrics["archiver"], stop_event)),
            threading.Thread(target=run_archiver_worker, args=(base_dir, duration_sec, 0.05, 80, metrics["archiver"], stop_event)),
            threading.Thread(target=run_sqlite_worker, args=(base_dir, duration_sec, 0.02, metrics["sqlite"], stop_event)),
            threading.Thread(target=run_sqlite_worker, args=(base_dir, duration_sec, 0.02, metrics["sqlite"], stop_event)),
            threading.Thread(target=run_valhalla_worker, args=(base_dir, duration_sec, 0.03, metrics["valhalla"], stop_event)),
            threading.Thread(target=run_valhalla_worker, args=(base_dir, duration_sec, 0.03, metrics["valhalla"], stop_event)),
            threading.Thread(target=run_cv_worker, args=(base_dir, duration_sec, 0.1, metrics["cv_worker"], stop_event)),
        ]

    start_t = time.time()
    for t in threads:
        t.start()
    for t in threads:
        t.join()
    actual_duration = time.time() - start_t

    summary = {
        "mode": mode,
        "duration_sec": round(actual_duration, 2),
        "workers": {name: m.summarize(actual_duration) for name, m in metrics.items()},
    }

    total_ops = sum(w["operations"] for w in summary["workers"].values())
    total_mb_s = sum(w["throughput_mb_s"] for w in summary["workers"].values())
    total_iops = sum(w["iops"] for w in summary["workers"].values())

    summary["aggregate"] = {
        "total_operations": total_ops,
        "total_throughput_mb_s": round(total_mb_s, 2),
        "total_iops": round(total_iops, 1),
    }

    return summary


def main():
    parser = argparse.ArgumentParser(description="Simulate local-homesrv Disk I/O Baseline & Storm-Burst")
    parser.add_argument("--mode", choices=["baseline", "storm-burst", "both"], default="both", help="Workload scenario to run")
    parser.add_argument("--duration", type=float, default=10.0, help="Duration in seconds per scenario")
    parser.add_argument("--dir", type=str, default="", help="Directory for test files (defaults to tempdir)")
    parser.add_argument("--out", type=str, default="", help="Path to write JSON report output")
    args = parser.parse_args()

    results: Dict[str, Any] = {
        "timestamp": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
        "host_hardware": {
            "chassis": "ZOTAC ZBOX-CI320NANO (Fanless, 7.5W TDP)",
            "cpu": "Intel Celeron N2930 (4C/4T @ 1.83GHz)",
            "ram": "8GB DDR3L",
            "storage": "250GB 5400RPM HDD (SATA 3Gb/s / 2.5\")",
            "os": "Debian 13 (Docker Engine)",
        },
        "scenarios": {},
    }

    modes = ["baseline", "storm-burst"] if args.mode == "both" else [args.mode]

    for m in modes:
        print(f"\n[+] Executing scenario: {m.upper()} (duration: {args.duration}s)...")
        if args.dir:
            test_dir = os.path.join(args.dir, m)
            os.makedirs(test_dir, exist_ok=True)
            res = execute_workload(m, args.duration, test_dir)
        else:
            with tempfile.TemporaryDirectory(prefix=f"storm_io_{m}_") as tmp:
                res = execute_workload(m, args.duration, tmp)

        results["scenarios"][m] = res
        print(f"    Total Ops: {res['aggregate']['total_operations']}")
        print(f"    Aggregate Throughput: {res['aggregate']['total_throughput_mb_s']} MB/s")
        print(f"    Aggregate IOPS: {res['aggregate']['total_iops']}")
        for w_name, w_data in res["workers"].items():
            print(f"      - {w_name:10s}: {w_data['operations']:4d} ops | {w_data['throughput_mb_s']:5.2f} MB/s | p50: {w_data['latency_p50_ms']:5.2f}ms | p95: {w_data['latency_p95_ms']:5.2f}ms | max: {w_data['latency_max_ms']:5.2f}ms")

    if args.out:
        os.makedirs(os.path.dirname(os.path.abspath(args.out)), exist_ok=True)
        with open(args.out, "w") as f:
            json.dump(results, f, indent=2)
        print(f"\n[✓] Results saved to: {args.out}")


if __name__ == "__main__":
    main()
