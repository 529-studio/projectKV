#!/usr/bin/env python3
"""
bench_solution1.py — Comprehensive Benchmark Suite for ProjectKV cv-engine (Solution 1).
Evaluates latency distribution, max throughput (RPS), concurrency scalability,
and resource consumption under production workload conditions.
"""

import argparse
import concurrent.futures
import json
import os
import sys
import time
import urllib.request
import urllib.error
from statistics import mean, median

def load_image(path):
    with open(path, "rb") as f:
        return f.read()

def send_request(url, img_bytes, timeout=10):
    start = time.perf_counter()
    req = urllib.request.Request(
        f"{url}/predict",
        data=img_bytes,
        headers={"Content-Type": "application/octet-stream"}
    )
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            data = resp.read()
            duration = (time.perf_counter() - start) * 1000.0
            parsed = json.loads(data.decode("utf-8"))
            return {
                "ok": True,
                "status": resp.status,
                "duration_ms": duration,
                "model_latency_ms": parsed.get("latency_ms", 0),
                "vehicles": parsed.get("vehicles_detected", 0),
                "error": None
            }
    except Exception as e:
        duration = (time.perf_counter() - start) * 1000.0
        return {
            "ok": False,
            "status": getattr(e, "code", 500),
            "duration_ms": duration,
            "model_latency_ms": 0,
            "vehicles": 0,
            "error": str(e)
        }

def run_sequential_bench(url, img_bytes, num_requests=50):
    print(f"\n[1] Running Sequential Baseline ({num_requests} requests)...")
    results = []
    for i in range(num_requests):
        r = send_request(url, img_bytes)
        results.append(r)
        if (i + 1) % 10 == 0:
            print(f"  Processed {i+1}/{num_requests} requests...")
    
    durations = [r["duration_ms"] for r in results if r["ok"]]
    model_latencies = [r["model_latency_ms"] for r in results if r["ok"]]
    successes = sum(1 for r in results if r["ok"])
    
    durations.sort()
    return {
        "total": num_requests,
        "success": successes,
        "error_rate_pct": (num_requests - successes) / num_requests * 100.0,
        "p50_ms": durations[int(len(durations) * 0.50)] if durations else 0,
        "p90_ms": durations[int(len(durations) * 0.90)] if durations else 0,
        "p95_ms": durations[int(len(durations) * 0.95)] if durations else 0,
        "p99_ms": durations[int(len(durations) * 0.99)] if durations else 0,
        "min_ms": min(durations) if durations else 0,
        "max_ms": max(durations) if durations else 0,
        "avg_ms": mean(durations) if durations else 0,
        "avg_model_latency_ms": mean(model_latencies) if model_latencies else 0,
    }

def run_concurrent_bench(url, img_bytes, concurrency_levels=[1, 2, 5, 10, 20], duration_sec=5):
    print(f"\n[2] Running Concurrency Scalability & Throughput Test...")
    summary = {}
    for c in concurrency_levels:
        print(f"  Testing Concurrency = {c} workers for {duration_sec}s...")
        stop_time = time.time() + duration_sec
        results = []
        
        def worker():
            local_res = []
            while time.time() < stop_time:
                res = send_request(url, img_bytes)
                local_res.append(res)
            return local_res
        
        t0 = time.perf_counter()
        with concurrent.futures.ThreadPoolExecutor(max_workers=c) as executor:
            futures = [executor.submit(worker) for _ in range(c)]
            for f in concurrent.futures.as_completed(futures):
                results.extend(f.result())
        total_time = time.perf_counter() - t0
        
        durations = [r["duration_ms"] for r in results if r["ok"]]
        successes = sum(1 for r in results if r["ok"])
        total_reqs = len(results)
        durations.sort()
        
        rps = successes / total_time if total_time > 0 else 0
        summary[c] = {
            "concurrency": c,
            "total_requests": total_reqs,
            "successful_requests": successes,
            "duration_sec": round(total_time, 2),
            "throughput_rps": round(rps, 2),
            "p50_ms": round(durations[int(len(durations) * 0.50)], 2) if durations else 0,
            "p90_ms": round(durations[int(len(durations) * 0.90)], 2) if durations else 0,
            "p99_ms": round(durations[int(len(durations) * 0.99)], 2) if durations else 0,
            "error_rate_pct": round((total_reqs - successes) / total_reqs * 100.0, 2) if total_reqs > 0 else 0
        }
        print(f"    -> Concurrency {c}: {rps:.1f} RPS | P50: {summary[c]['p50_ms']}ms | P90: {summary[c]['p90_ms']}ms | Errors: {summary[c]['error_rate_pct']}%")
    return summary

def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--url", default="http://localhost:8005", help="CV engine base URL")
    parser.add_argument("--image", default="tools/eval/fixtures/frames/fixture_01_clear_dry_day.jpg")
    parser.add_argument("--output", default="docs/reports/bench_solution1_report.json")
    args = parser.parse_args()

    if not os.path.exists(args.image):
        print(f"Image not found: {args.image}")
        sys.exit(1)

    img_bytes = load_image(args.image)
    print(f"Loaded benchmark image: {args.image} ({len(img_bytes)} bytes)")
    print(f"Target URL: {args.url}")

    # Check healthz
    try:
        with urllib.request.urlopen(f"{args.url}/healthz", timeout=5) as resp:
            data = json.loads(resp.read().decode())
            print(f"Health check OK: {data}")
    except Exception as e:
        print(f"Health check failed: {e}")
        sys.exit(1)

    seq_results = run_sequential_bench(args.url, img_bytes, num_requests=50)
    conc_results = run_concurrent_bench(args.url, img_bytes, concurrency_levels=[1, 2, 5, 10, 15, 20], duration_sec=5)

    report = {
        "timestamp": time.strftime("%Y-%m-%d %H:%M:%S"),
        "service": "projectkv-cv-engine",
        "runtime": "Python 3.11 + OpenVINO IR 2026",
        "model": "yolov8n_openvino_320 (FP16)",
        "endpoint": args.url,
        "sequential_baseline": seq_results,
        "concurrency_sweep": conc_results,
    }

    os.makedirs(os.path.dirname(args.output), exist_ok=True)
    with open(args.output, "w") as f:
        json.dump(report, f, indent=2)
    print(f"\n[✓] Benchmark completed. Report saved to {args.output}")

if __name__ == "__main__":
    main()
