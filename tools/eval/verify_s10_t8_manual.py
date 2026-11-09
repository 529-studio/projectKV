#!/usr/bin/env python3
"""
verify_s10_t8_manual.py - Autonomous Manual Verification Scenarios for S10-T8 (#379).

Enforces AGENTS.md Rule 10 with empirical proof, in four scenarios:

  Scenario 1: Multi-load contention matrix. Validates the reference matrix
              (p50/p95, CPU, RAM, SLA) and runs a live local contention probe that
              measures CPU starvation and the resolution compute delta on this host.
  Scenario 2: Cgroup boundary isolation. Validates core pinning, CPU quota, memory
              ceilings, and proves the validator detects a tampered configuration.
  Scenario 3: Off-peak nocturnal cron gate. Proves 02:00 ICT is allowed and 14:00 ICT
              is rejected, including the CLI exit codes a cron wrapper relies on.
  Scenario 4: Adaptive load-shedding and hysteresis. Drives the policy through a
              contention/recovery sequence and verifies cv-engine honours
              X-Target-Resolution through its production helper.

Every scenario asserts. The process exits 1 on the first failed assertion.
"""

from __future__ import annotations

import argparse
import copy
import importlib.util
import json
import logging
import multiprocessing as mp
import os
import statistics
import subprocess
import sys
import threading
import time
import urllib.request
from datetime import datetime, timedelta, timezone
from io import BytesIO
from pathlib import Path
from typing import Any, Dict, List

import numpy as np
from PIL import Image

REPO_ROOT = Path(__file__).resolve().parent.parent.parent
if str(REPO_ROOT) not in sys.path:
    sys.path.insert(0, str(REPO_ROOT))

from tools.eval.benchmark_cotenancy import (
    CGROUP_BOUNDARIES,
    OFFPEAK_WINDOWS,
    WORKER_HARDWARE,
    evaluate_loadshedding_policy,
    expand_cpuset,
    is_within_offpeak_window,
    physical_core_ids,
    run_all_benchmarks,
    validate_cron_schedule,
    verify_cgroup_configuration,
)

logging.basicConfig(level=logging.INFO, format="%(asctime)s [%(levelname)s] %(message)s")
logger = logging.getLogger("s10-t8-manual-verifier")

ICT = timezone(timedelta(hours=7))
BENCHMARK_SCRIPT = REPO_ROOT / "tools" / "eval" / "benchmark_cotenancy.py"
CV_ENGINE_SERVER = REPO_ROOT / "services" / "cv-engine" / "server.py"


def check_invariant(cond: bool, msg: str) -> None:
    """Fail a verification scenario unless `cond` holds.

    ``assert`` is compiled out under ``python3 -O``, which would turn this verifier
    into a script that prints PASSED without checking anything. Raising is not
    optimizable, so the evidence it writes always reflects executed checks.
    """
    if not cond:
        raise RuntimeError(msg)


# ---------------------------------------------------------------------------
# Live measurement helpers
# ---------------------------------------------------------------------------

def _synthetic_frame(width: int = 1920, height: int = 1080) -> Image.Image:
    """Deterministic frame with enough entropy to defeat resize fast paths."""
    rng = np.random.default_rng(379)
    return Image.fromarray(rng.integers(0, 256, size=(height, width, 3), dtype=np.uint8))


def _measure_preprocess(frame: Image.Image, resolution: int, iterations: int = 120) -> Dict[str, float]:
    """Measure cv-engine-shaped preprocessing at one input resolution.

    The loop mirrors the service path: PIL resize, float conversion, /255, HWC->CHW.
    Wall and process CPU time are both recorded: wall time exposes scheduling stalls,
    process CPU time exposes the intrinsic compute cost of the pixel count.
    """
    for _ in range(10):
        frame.resize((resolution, resolution), Image.BILINEAR)

    latencies: List[float] = []
    wall_start = time.perf_counter()
    cpu_start = time.process_time()
    for _ in range(iterations):
        t0 = time.perf_counter()
        resized = frame.resize((resolution, resolution), Image.BILINEAR)
        array = np.asarray(resized, dtype=np.float32) / 255.0
        _ = array.transpose(2, 0, 1)
        latencies.append((time.perf_counter() - t0) * 1000.0)
    wall_s = time.perf_counter() - wall_start
    cpu_s = time.process_time() - cpu_start

    latencies.sort()
    return {
        "resolution": resolution,
        "iterations": iterations,
        "p50_wall_ms": round(statistics.median(latencies), 3),
        "p95_wall_ms": round(latencies[int(len(latencies) * 0.95)], 3),
        "wall_s": round(wall_s, 4),
        "cpu_s": round(cpu_s, 4),
        "cpu_share": round(cpu_s / wall_s, 3) if wall_s > 0 else 0.0,
        "pixels": resolution * resolution,
    }


def _cpu_spin(stop_event: Any, counter: Any, progress: Any = None, slot: int = 0) -> None:
    """Saturate one core until told to stop; used only by the contention probe.

    ``progress[slot]`` is published after every completed spin loop so the parent can
    prove this worker is actually executing on a core before it starts measuring.
    """
    value = 0.0
    loops = 0
    while not stop_event.is_set():
        for i in range(50_000):
            value = (value + i * 1.000001) % 1000.0
        loops += 1
        if progress is not None:
            progress[slot] = loops
        counter.value += 1


def _worker_count() -> int:
    cores = os.cpu_count() or 2
    # Two runnable spinners per core guarantees the probe process is descheduled,
    # which is what a co-tenanted worker experiences when batch jobs are unbounded.
    return max(4, cores * 2)


def _mp_context() -> Any:
    try:
        return mp.get_context("fork")
    except ValueError:  # pragma: no cover - platforms without fork
        return mp.get_context("spawn")


def _await_contention(progress: Any, timeout_s: float = 10.0) -> int:
    """Block until every background spinner has completed a loop, or fail loudly.

    Starting a process only means ``fork()`` returned; it does not mean the child has
    been scheduled onto a core. Measuring before that would compare the probe against
    a partly idle machine and could report a contention drop that never happened (or
    miss the contention that was about to happen). The barrier makes the probe
    measurement conditional on real load being established.
    """
    deadline = time.time() + timeout_s
    while time.time() < deadline and min(progress) < 1:
        time.sleep(0.02)
    ready = sum(1 for loops in progress if loops >= 1)
    if ready < len(progress):
        raise RuntimeError(
            f"Only {ready}/{len(progress)} contention workers executed a spin loop "
            f"within {timeout_s:.0f}s; refusing to report a contention result"
        )
    return ready


def _run_contention_probe(frame: Image.Image) -> Dict[str, Any]:
    """Measure preprocessing under idle conditions and under real CPU contention.

    The asserted signal is the probe's CPU share (process CPU seconds per wall second),
    not wall-clock throughput. Wall time is contaminated by DVFS and scheduler
    placement on a laptop; a share drop directly proves the process was descheduled
    because the background spinners held the cores. Wall slowdown is still recorded as
    informational evidence.
    """
    baseline = _measure_preprocess(frame, 416)
    context = _mp_context()
    stop_event = context.Event()
    counter = context.Value("l", 0)
    worker_count = _worker_count()
    progress = context.Array("l", worker_count)
    workers = [
        context.Process(
            target=_cpu_spin,
            args=(stop_event, counter, progress, slot),
            daemon=True,
        )
        for slot in range(worker_count)
    ]
    try:
        for worker in workers:
            worker.start()
        ready = _await_contention(progress)
        contended = _measure_preprocess(frame, 416)
    finally:
        stop_event.set()
        for worker in workers:
            worker.join(timeout=5)
            if worker.is_alive():  # pragma: no cover - defensive
                worker.terminate()

    slowdown = round(contended["wall_s"] / baseline["wall_s"], 3) if baseline["wall_s"] else 0.0
    return {
        "worker_processes": len(workers),
        "worker_processes_ready": ready,
        "worker_loops_completed": int(counter.value),
        "baseline": baseline,
        "contended": contended,
        "wall_slowdown_factor": slowdown,
    }


def _load_cv_engine_module() -> Any:
    """Load services/cv-engine/server.py as an isolated module for production-path checks."""
    spec = importlib.util.spec_from_file_location("cv_engine_server_s10_t8", CV_ENGINE_SERVER)
    if spec is None or spec.loader is None:  # pragma: no cover - defensive
        raise RuntimeError(f"Cannot load {CV_ENGINE_SERVER}")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


class _RecordingInput:
    def __init__(self, shape: Any):
        self.shape = tuple(shape)


class _RecordingResults:
    """Stand-in for OpenVINO's OVDict: subscriptable, not a Python dict."""

    def __init__(self, output: Any):
        self._output = output

    def __getitem__(self, key: int) -> Any:
        return self._output

    def __len__(self) -> int:
        return 1


class _RecordingInferRequest:
    def __init__(self, output: Any, recorded_feeds: List[Dict[int, tuple]]):
        self.output = output
        self.recorded_feeds = recorded_feeds

    def infer(self, feeds: Dict[int, Any]) -> _RecordingResults:
        self.recorded_feeds.append(
            {int(key): tuple(np.asarray(value).shape) for key, value in feeds.items()}
        )
        return _RecordingResults(self.output)


class _RecordingCompiledModel:
    """Compiled-model double that records the tensor actually fed to infer()."""

    def __init__(self, shape: Any):
        self.input_shape = tuple(shape)
        self.recorded_feeds: List[Dict[int, tuple]] = []
        self._output = np.zeros((1, 42, 8400), dtype=np.float32)

    @property
    def resolution_mode(self) -> str:
        spatial = self.input_shape[2:]
        fixed = all(isinstance(dim, int) and dim > 0 for dim in spatial)
        return "static" if fixed and len(spatial) == 2 else "dynamic"

    def input(self, index: int) -> _RecordingInput:
        return _RecordingInput(self.input_shape)

    def create_infer_request(self) -> _RecordingInferRequest:
        return _RecordingInferRequest(self._output, self.recorded_feeds)


def _exercise_predict(
    server_module: Any,
    model_shape: Any,
    header_value: str,
) -> Dict[str, Any]:
    """Run one real POST /predict against a recording fake compiled model."""
    model = _RecordingCompiledModel(model_shape)
    frame = BytesIO()
    Image.new("RGB", (64, 64), (90, 90, 90)).save(frame, format="JPEG")

    saved_model = server_module.compiled_model
    server_module.compiled_model = model
    httpd = server_module.CVServer(("127.0.0.1", 0), server_module.CVRequestHandler)
    port = httpd.server_address[1]
    thread = threading.Thread(target=httpd.serve_forever, daemon=True)
    thread.start()
    try:
        request = urllib.request.Request(
            f"http://127.0.0.1:{port}/predict",
            data=frame.getvalue(),
            headers={
                "Content-Type": "image/jpeg",
                "X-Target-Resolution": header_value,
            },
            method="POST",
        )
        with urllib.request.urlopen(request, timeout=10.0) as response:
            status = response.status
            payload = json.loads(response.read().decode("utf-8"))
    finally:
        httpd.shutdown()
        httpd.server_close()
        thread.join(timeout=5)
        server_module.compiled_model = saved_model

    return {"status": status, "payload": payload, "feeds": model.recorded_feeds}


def _verify_predict_feed_resolution(server_module: Any, resolver: Any) -> Dict[str, Any]:
    """Prove the payload reports the shape the graph was actually fed.

    A dynamic graph must execute the requested 320 px tensor; a static 416 px IR must
    ignore the header, feed 416 px, and say so. The 640 px header is an operator-only
    environment value and must fall back to 416 px.
    """
    cases = [
        ("dynamic", (1, 3, -1, -1), "320", (320, 320)),
        ("static", (1, 3, 416, 416), "320", (416, 416)),
        ("header_640_rejected", (1, 3, -1, -1), "640", (416, 416)),
    ]
    results: Dict[str, Any] = {}
    for label, shape, header, expected in cases:
        run = _exercise_predict(server_module, shape, header)
        check_invariant(run["status"] == 200, f"{label}: POST /predict returned {run['status']}")
        reported = tuple(run["payload"].get("input_resolution", ()))
        check_invariant(
            reported == expected,
            f"{label}: payload input_resolution {reported} != {expected}",
        )
        feeds = run["feeds"]
        check_invariant(bool(feeds), f"{label}: model never received an inference feed")
        fed_shape = tuple(feeds[-1][0])
        expected_shape = (1, 3, expected[0], expected[1])
        check_invariant(
            fed_shape == expected_shape,
            f"{label}: fed tensor {fed_shape} != expected {expected_shape}",
        )
        check_invariant(
            list(fed_shape[2:]) == list(reported),
            f"{label}: payload resolution {list(reported)} does not match fed tensor {fed_shape}",
        )
        results[label] = {
            "model_shape": list(shape),
            "header": header,
            "input_resolution": list(reported),
            "fed_shape": list(fed_shape),
        }
        logger.info(
            "  POST /predict [%s] model=%s header=%s -> fed %s, reported %s",
            label,
            list(shape),
            header,
            list(fed_shape),
            list(reported),
        )
    return results


# ---------------------------------------------------------------------------
# Scenario 1
# ---------------------------------------------------------------------------

def scenario_1_contention_matrix() -> Dict[str, Any]:
    logger.info("=== SCENARIO 1: Multi-Load Contention Matrix ===")
    data = run_all_benchmarks()

    logger.info(
        "%-14s %6s %9s %9s %9s %9s %10s  %s",
        "scenario", "res", "p50 ms", "p95 ms", "CPU %", "RSS MB", "sweep s", "SLA",
    )
    for scenario in data["scenarios"]:
        logger.info(
            "%-14s %6d %9.1f %9.1f %9.1f %9.1f %10.2f  %s",
            scenario["scenario_id"],
            scenario["resolution"],
            scenario["p50_latency_ms"],
            scenario["p95_latency_ms"],
            scenario["cpu_utilization_pct"],
            scenario["memory_rss_mb"],
            scenario["sweep_duration_s"],
            scenario["sla_status"],
        )

    matrix = data["matrix_validation"]
    check_invariant(matrix["valid"], f"Matrix invariants failed: {matrix['errors']}")
    check_invariant(
        data["cgroup_status"]["verified"],
        f"Cgroup violations: {data['cgroup_status']['violations']}",
    )
    check_invariant(
        data["measurement_source"] == "reference-profile",
        f"Unexpected measurement source: {data['measurement_source']}",
    )

    # The matrix must tell the expected story explicitly, not merely be "valid".
    by_id = {s["scenario_id"]: s for s in data["scenarios"]}
    check_invariant(by_id["baseline"]["sla_passed"], "Baseline must fit the 25 ms p95 ceiling")
    check_invariant(by_id["cotenancy_sam"]["sla_passed"], "SAM-only co-tenancy must fit the SLA")
    check_invariant(
        not by_id["cotenancy_full"]["sla_passed"], "Full contention must breach the SLA"
    )
    check_invariant(by_id["loadshed_320"]["sla_passed"], "320 px shedding must recover the SLA")
    check_invariant(
        by_id["cotenancy_full"]["p95_latency_ms"] > WORKER_HARDWARE["p95_sla_ceiling_ms"],
        "Full contention p95 did not exceed the SLA ceiling",
    )
    check_invariant(
        by_id["loadshed_320"]["p95_latency_ms"] < by_id["cotenancy_full"]["p95_latency_ms"],
        "320 px shedding did not reduce p95 latency versus full contention",
    )
    logger.info("Reference matrix invariants: PASSED")

    # Live proof on this host: 320 px performs less work than 416 px.
    frame = _synthetic_frame()
    live_416 = _measure_preprocess(frame, 416)
    live_320 = _measure_preprocess(frame, 320)
    compute_ratio = round(live_320["cpu_s"] / live_416["cpu_s"], 3) if live_416["cpu_s"] else 0.0
    logger.info(
        "Live preprocessing at 416 px: p50=%.2f ms p95=%.2f ms cpu=%.3f s",
        live_416["p50_wall_ms"], live_416["p95_wall_ms"], live_416["cpu_s"],
    )
    logger.info(
        "Live preprocessing at 320 px: p50=%.2f ms p95=%.2f ms cpu=%.3f s (ratio %.3f)",
        live_320["p50_wall_ms"], live_320["p95_wall_ms"], live_320["cpu_s"], compute_ratio,
    )
    check_invariant(
        live_320["p50_wall_ms"] < live_416["p50_wall_ms"],
        "320 px preprocessing was not faster than 416 px on this host: "
        f"{live_320['p50_wall_ms']} ms vs {live_416['p50_wall_ms']} ms",
    )
    check_invariant(
        compute_ratio < 0.98,
        f"320 px consumed {compute_ratio:.3f} of the 416 px CPU time",
    )

    # Live proof of contention: the probe loses CPU share when background load runs.
    contention = _run_contention_probe(frame)
    base_share = contention["baseline"]["cpu_share"]
    cont_share = contention["contended"]["cpu_share"]
    logger.info(
        "Live contention probe: %d/%d background processes ready, %d spin loops; "
        "cpu_share %.3f -> %.3f, wall x%.2f",
        contention["worker_processes_ready"],
        contention["worker_processes"],
        contention["worker_loops_completed"],
        base_share,
        cont_share,
        contention["wall_slowdown_factor"],
    )
    check_invariant(
        contention["worker_processes_ready"] == contention["worker_processes"],
        f"Only {contention['worker_processes_ready']} of "
        f"{contention['worker_processes']} contention workers were executing",
    )
    check_invariant(contention["worker_loops_completed"] > 0, "Background workers never ran")
    check_invariant(
        base_share > 0.5,
        f"Baseline probe never received meaningful CPU: share {base_share}",
    )
    check_invariant(
        cont_share <= base_share * 0.95,
        f"Contention did not reduce the probe's CPU share: "
        f"{base_share} -> {cont_share} (wall factor {contention['wall_slowdown_factor']})",
    )

    logger.info("SCENARIO 1 PASSED: matrix invariants and live contention measured.\n")
    return {
        "matrix": {
            "measurement_source": data["measurement_source"],
            "scenarios": data["scenarios"],
            "matrix_validation": matrix,
        },
        "live_resolution_compute": {"at_416": live_416, "at_320": live_320, "cpu_ratio_320_over_416": compute_ratio},
        "live_contention": contention,
    }


# ---------------------------------------------------------------------------
# Scenario 2
# ---------------------------------------------------------------------------

def scenario_2_cgroup_isolation() -> Dict[str, Any]:
    logger.info("=== SCENARIO 2: Cgroup Boundary Isolation ===")
    status = verify_cgroup_configuration()
    check_invariant(status["verified"], f"Cgroup violations: {status['violations']}")

    for slice_name, boundary in status["boundaries"].items():
        cores = sorted(physical_core_ids(boundary["cpuset_cpus"]))
        threads = sorted(expand_cpuset(boundary["cpuset_cpus"]))
        logger.info(
            "  [%s] threads=%s physical_cores=%s quota=%s%% mem=%.0f MB parent=%s",
            slice_name,
            threads,
            cores,
            boundary.get("cpu_quota_pct"),
            boundary["memory_limit_mb"],
            boundary["parent_slice"],
        )

    cv = status["boundaries"]["cv_engine"]
    sam = status["boundaries"]["sam_prelabel"]
    ci = status["boundaries"]["ci_runner"]

    # Core pinning: inference owns 0-3, batch owns 4-7, and the SMT siblings follow.
    check_invariant(
        physical_core_ids(cv["cpuset_cpus"]) == {0, 1, 2, 3},
        "cv-engine is not pinned to cores 0-3",
    )
    check_invariant(
        physical_core_ids(sam["cpuset_cpus"]) == {4, 5, 6, 7},
        "SAM batch is not pinned to cores 4-7",
    )
    check_invariant(
        {0, 1, 2, 3}.isdisjoint({4, 5, 6, 7}),
        "cv-engine and batch core sets overlap",
    )
    check_invariant(
        expand_cpuset(cv["cpuset_cpus"]) == {0, 1, 2, 3, 8, 9, 10, 11},
        f"cv-engine threads {sorted(expand_cpuset(cv['cpuset_cpus']))} != 0-3,8-11",
    )
    check_invariant(
        expand_cpuset(sam["cpuset_cpus"]) == {4, 5, 6, 7, 12, 13, 14, 15},
        f"SAM threads {sorted(expand_cpuset(sam['cpuset_cpus']))} != 4-7,12-15",
    )

    # Quota and memory ceilings.
    check_invariant(
        sam["cpu_quota_pct"] == 50.0, f"SAM quota {sam['cpu_quota_pct']} != 50%"
    )
    check_invariant(
        sam["memory_limit_mb"] == 1536.0,
        f"SAM memory {sam['memory_limit_mb']} != 1536 MB",
    )
    check_invariant(cv["cpu_quota_pct"] is None, "cv-engine must not be quota-throttled")
    check_invariant(
        ci["parent_slice"] == "sam_prelabel",
        "CI runner must be nested under the batch slice",
    )
    check_invariant(
        ci["memory_limit_mb"] <= sam["memory_limit_mb"],
        "CI runner memory exceeds its parent slice",
    )
    check_invariant(
        cv["cpu_weight"] > sam["cpu_weight"] > ci["cpu_weight"],
        "CPU priorities are not ordered",
    )
    check_invariant(
        status["root_memory_total_mb"] <= WORKER_HARDWARE["total_ram_mb"],
        "Root slices overcommit RAM",
    )
    logger.info(
        "Root slice limits: %.0f MB / %.0f MB allocable (headroom %.0f MB)",
        status["root_memory_total_mb"],
        WORKER_HARDWARE["total_ram_mb"],
        WORKER_HARDWARE["total_ram_mb"] - status["root_memory_total_mb"],
    )

    # Prove the validator is not a rubber stamp (L66): three tampered configurations
    # must each be rejected with a specific violation.
    tamper_cases = []
    quota_tamper = copy.deepcopy(CGROUP_BOUNDARIES)
    quota_tamper["sam_prelabel"]["cpu_quota_pct"] = 100.0
    tamper_cases.append(("quota 50% -> 100%", quota_tamper, "sam_prelabel.cpu_quota_pct"))

    core_tamper = copy.deepcopy(CGROUP_BOUNDARIES)
    core_tamper["cv_engine"]["cpuset_cpus"] = "4-7,12-15"
    tamper_cases.append(("cv-engine cores 0-3 -> 4-7", core_tamper, "cv_engine.physical_cores"))

    memory_tamper = copy.deepcopy(CGROUP_BOUNDARIES)
    memory_tamper["sam_prelabel"]["memory_limit_mb"] = 2048.0
    tamper_cases.append(("batch 1.5 GB -> 2.0 GB", memory_tamper, "memory.root_slices_fit_ceiling"))

    for label, boundaries, expected_violation in tamper_cases:
        tampered_status = verify_cgroup_configuration(boundaries)
        names = [v["name"] for v in tampered_status["violations"]]
        check_invariant(not tampered_status["verified"], f"Tamper not detected: {label}")
        check_invariant(
            expected_violation in names,
            f"{label}: expected {expected_violation}, got {names}",
        )
        logger.info("  Tamper detected: %s -> %s", label, expected_violation)

    logger.info("SCENARIO 2 PASSED: isolation enforced and tampering detected.\n")
    return {
        "status": {
            "verified": status["verified"],
            "checks": status["checks"],
            "root_memory_total_mb": status["root_memory_total_mb"],
            "allocable_memory_mb": status["allocable_memory_mb"],
        },
        "tamper_cases": [
            {"case": label, "expected_violation": violation}
            for label, _boundaries, violation in tamper_cases
        ],
    }


# ---------------------------------------------------------------------------
# Scenario 3
# ---------------------------------------------------------------------------

def _run_cron_cli(timestamp: str) -> subprocess.CompletedProcess:
    return subprocess.run(
        [sys.executable, str(BENCHMARK_SCRIPT), "--check-cron-window", timestamp],
        capture_output=True,
        text=True,
    )


def scenario_3_offpeak_cron() -> Dict[str, Any]:
    logger.info("=== SCENARIO 3: Off-Peak Nocturnal Cron Gate ===")
    check_invariant(
        OFFPEAK_WINDOWS == {"ict": (1, 5), "utc": (18, 22)},
        f"Off-peak windows changed: {OFFPEAK_WINDOWS}",
    )

    at_0200_ict = datetime(2026, 11, 7, 2, 0, tzinfo=ICT)
    at_1400_ict = datetime(2026, 11, 7, 14, 0, tzinfo=ICT)
    check_invariant(is_within_offpeak_window(at_0200_ict), "02:00 ICT must be authorized")
    check_invariant(not is_within_offpeak_window(at_1400_ict), "14:00 ICT must be rejected")
    logger.info("02:00 ICT -> AUTHORIZED; 14:00 ICT -> REJECTED")

    # The same instants on the UTC host clock.
    check_invariant(
        is_within_offpeak_window(at_0200_ict, tz_mode="utc"),
        "19:00 UTC must be authorized",
    )
    check_invariant(
        not is_within_offpeak_window(at_1400_ict, tz_mode="utc"),
        "07:00 UTC must be rejected",
    )
    logger.info("19:00 UTC -> AUTHORIZED; 07:00 UTC -> REJECTED")

    valid, message = validate_cron_schedule("0 2 * * *")
    check_invariant(valid, message)
    valid, message = validate_cron_schedule("0 19 * * *", tz_mode="utc")
    check_invariant(valid, message)
    for expression in ("0 14 * * *", "0 5 * * *", "30 5 * * *"):
        valid, message = validate_cron_schedule(expression)
        check_invariant(not valid, f"{expression} should be rejected: {message}")
        logger.info("Cron %-14s -> REJECTED: %s", expression, message)
    logger.info("Cron '0 2 * * *' and '0 19 * * *' (UTC) -> ACCEPTED")

    # Empirical proof of the operational guard: the CLI exit code is what cron sees.
    allowed_run = _run_cron_cli(at_0200_ict.isoformat())
    rejected_run = _run_cron_cli(at_1400_ict.isoformat())
    check_invariant(
        allowed_run.returncode == 0,
        f"Allowed run exited {allowed_run.returncode}: {allowed_run.stderr}",
    )
    check_invariant(
        rejected_run.returncode == 1,
        f"Rejected run exited {rejected_run.returncode}: {rejected_run.stderr}",
    )
    check_invariant("AUTHORIZED" in allowed_run.stdout, "Allowed run did not report AUTHORIZED")
    check_invariant("FORBIDDEN" in rejected_run.stdout, "Rejected run did not report FORBIDDEN")
    logger.info("CLI exit codes: 02:00 ICT -> %d, 14:00 ICT -> %d", allowed_run.returncode, rejected_run.returncode)

    logger.info("SCENARIO 3 PASSED: off-peak gate allows 02:00 ICT and rejects 14:00 ICT.\n")
    return {
        "window": {"start_hour_ict": 1, "end_hour_ict_exclusive": 5, "utc_equivalent": "18:00-22:00"},
        "checks": [
            {"timestamp": at_0200_ict.isoformat(), "allowed": True, "cli_exit": allowed_run.returncode},
            {"timestamp": at_1400_ict.isoformat(), "allowed": False, "cli_exit": rejected_run.returncode},
        ],
        "rejected_cron_expressions": ["0 14 * * *", "0 5 * * *", "30 5 * * *"],
    }


# ---------------------------------------------------------------------------
# Scenario 4
# ---------------------------------------------------------------------------

def scenario_4_load_shedding_hysteresis() -> Dict[str, Any]:
    logger.info("=== SCENARIO 4: Adaptive Load-Shedding and Hysteresis ===")

    # (p95 ms, CPU %, expected action, expected resolution after applying the action)
    sequence = [
        (14.2, 18.2, "NOMINAL", 416),
        (28.6, 91.8, "DEGRADE_RESOLUTION_320", 320),
        (26.4, 88.0, "THROTTLE_BACKGROUND_BATCH", 320),
        (22.0, 78.0, "NOMINAL", 320),
        (18.0, 60.0, "RECOVER_RESOLUTION_416", 416),
        (16.0, 50.0, "NOMINAL", 416),
    ]

    state = 416
    transitions: List[Dict[str, Any]] = []
    for p95, cpu, expected_action, expected_next in sequence:
        decision = evaluate_loadshedding_policy(p95, cpu, current_resolution=state)
        logger.info(
            "  p95=%5.1f ms cpu=%5.1f%% state=%d -> %s -> %d",
            p95, cpu, state, decision["action"], decision["recommended_resolution"],
        )
        check_invariant(
            decision["action"] == expected_action,
            f"p95={p95} cpu={cpu} state={state}: expected {expected_action}, "
            f"got {decision['action']}",
        )
        check_invariant(
            decision["recommended_resolution"] == expected_next,
            f"p95={p95} cpu={cpu} state={state}: expected resolution {expected_next}, "
            f"got {decision['recommended_resolution']}",
        )
        transitions.append({
            "p95_ms": p95,
            "cpu_pct": cpu,
            "from_resolution": state,
            "action": decision["action"],
            "to_resolution": decision["recommended_resolution"],
        })
        state = decision["recommended_resolution"]

    # No oscillation at the hysteresis boundary: a reading that clears degradation
    # (25 ms / 85%) but not recovery (20 ms / 70%) must hold 320 px.
    hold = evaluate_loadshedding_policy(22.0, 78.0, current_resolution=320)
    check_invariant(
        hold["action"] == "NOMINAL" and hold["recommended_resolution"] == 320,
        f"Hysteresis band did not hold 320 px: {hold}",
    )
    logger.info("Hysteresis band holds 320 px at p95=22.0 ms / CPU=78%.")

    # The production cv-engine path, not a copy of it, must honour the header (L66).
    server = _load_cv_engine_module()
    resolver = server.resolve_input_resolution
    check_invariant(resolver("320", None, [1, 3, 416, 416]) == (320, 320), "header 320")
    check_invariant(resolver("416", "320", [1, 3, 416, 416]) == (416, 416), "header beats environment")
    check_invariant(resolver(None, "320", [1, 3, 416, 416]) == (320, 320), "environment fallback")
    check_invariant(resolver(None, None, [1, 3, 640, 640]) == (640, 640), "model shape fallback")
    check_invariant(resolver("999", None, [1, 3, 416, 416]) == (416, 416), "out-of-contract header")
    logger.info("cv-engine resolve_input_resolution honours 320/416 and rejects out-of-contract 999.")

    predict_proof = _verify_predict_feed_resolution(server, resolver)
    logger.info(
        "POST /predict fed %s for a dynamic graph and %s for a static graph.",
        predict_proof["dynamic"]["fed_shape"],
        predict_proof["static"]["fed_shape"],
    )

    logger.info("SCENARIO 4 PASSED: degradation, throttle, hysteresis recovery verified.\n")
    return {
        "transitions": transitions,
        "hysteresis_band_holds_at": {"p95_ms": 22.0, "cpu_pct": 78.0, "resolution": 320},
        "cv_engine_resolution_contract": {
            "header_320": list(resolver("320", None, [1, 3, 416, 416])),
            "header_invalid_999": list(resolver("999", None, [1, 3, 416, 416])),
            "env_320": list(resolver(None, "320", [1, 3, 416, 416])),
        },
        "predict_feed_verification": predict_proof,
    }


# ---------------------------------------------------------------------------

def main() -> int:
    parser = argparse.ArgumentParser(description="S10-T8 manual verification scenarios.")
    parser.add_argument("--output-json", type=Path, default=None, help="Optional evidence JSON path")
    args = parser.parse_args()

    logger.info("STARTING S10-T8 MANUAL VERIFICATION SCENARIOS (RULE 10)")
    evidence: Dict[str, Any] = {
        "issue": "#379",
        "task": "S10-T8",
        "timestamp": datetime.now(timezone.utc).isoformat(),
        "host": {
            "platform": sys.platform,
            "python": sys.version.split()[0],
            "logical_cpus": os.cpu_count(),
        },
        "scenarios": {},
    }

    try:
        evidence["scenarios"]["scenario_1_contention_matrix"] = scenario_1_contention_matrix()
        evidence["scenarios"]["scenario_2_cgroup_isolation"] = scenario_2_cgroup_isolation()
        evidence["scenarios"]["scenario_3_offpeak_cron"] = scenario_3_offpeak_cron()
        evidence["scenarios"]["scenario_4_load_shedding"] = scenario_4_load_shedding_hysteresis()
    except (RuntimeError, AssertionError) as exc:
        logger.error("VERIFICATION FAILED: %s", exc)
        return 1

    evidence["result"] = "PASSED"
    if args.output_json:
        args.output_json.parent.mkdir(parents=True, exist_ok=True)
        with open(args.output_json, "w", encoding="utf-8") as fh:
            json.dump(evidence, fh, indent=2)
            fh.write("\n")
        logger.info("Wrote evidence to %s", args.output_json)

    logger.info("ALL 4 MANUAL VERIFICATION SCENARIOS PASSED WITH VERIFIABLE PROOF.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
