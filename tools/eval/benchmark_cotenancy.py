#!/usr/bin/env python3
"""
benchmark_cotenancy.py - Inference Worker Co-Tenancy Benchmark & Load-Shedding Harness.

Part of Sprint 10 Issue #379 (ADR-012 §3 (S10-T8) / §1.2 (F5)).
Models inference worker capacity (AMD Ryzen 7 6800HS, 8C/16T, 3.8 GB RAM) under
concurrent loads:

  1. baseline        - Hotset sweep only (57 cameras, 416 px).
  2. cotenancy_sam   - Hotset sweep + MobileSAM batch pre-labeling (4 workers).
  3. cotenancy_full  - Hotset sweep + MobileSAM batch (4 workers) + CI build runner.
  4. loadshed_320    - Adaptive degradation to 320 px under Level 2 contention.

The latency/cpu readings are a *reference capacity profile* adopted for the target
hardware and reported as planning figures. They are not re-measured by this script: it
validates the policy envelope (SLA arithmetic, cgroup invariants, cron gate, hysteresis
state machine). `tools/eval/verify_s10_t8_manual.py` runs the live mechanisms on the host
it executes on. No claim of a fresh measurement is made here.

Zero-emoji, 100% English, adheres to AGENTS.md.
"""

from __future__ import annotations

import argparse
import copy
import json
import logging
import math
import platform
import sys
from datetime import datetime, timezone, timedelta
from pathlib import Path
from typing import Any, Dict, List, Optional, Sequence, Set, Tuple

logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s [%(levelname)s] %(name)s: %(message)s",
    handlers=[logging.StreamHandler(sys.stdout)],
)
logger = logging.getLogger("benchmark-cotenancy")

# Hardware & SLA Constants (ADR-012 §1.2 F5 / Issue #379)
WORKER_HARDWARE = {
    "cpu": "AMD Ryzen 7 6800HS with Radeon Graphics (8 Cores / 16 Threads, AVX2 enabled)",
    "physical_cores": 8,
    "logical_threads": 16,
    "base_freq_ghz": 3.2,
    "boost_freq_ghz": 4.7,
    "total_ram_mb": 3840.0,
    "hotset_cameras": 57,
    "cycle_budget_s": 30.0,
    "p95_sla_ceiling_ms": 25.0,
    "degrade_cpu_threshold_pct": 85.0,
    "recover_p95_threshold_ms": 20.0,
    "recover_cpu_threshold_pct": 70.0,
}

# Cgroup Resource Boundaries (ADR-012 §1.2 F5 mitigation).
#
# CPU affinity is expressed with SMT siblings included so each slice owns whole
# physical cores: threads t belong to physical core (t % 8) on this 8C/16T part.
#   cv_engine     -> physical cores 0-3
#   sam_prelabel  -> physical cores 4-7
#   ci_runner     -> child of the sam_prelabel slice, bounded by its 1.5 GB ceiling
CGROUP_BOUNDARIES: Dict[str, Dict[str, Any]] = {
    "cv_engine": {
        "description": "Latency-critical real-time inference loop",
        "parent_slice": "root",
        "cpuset_cpus": "0-3,8-11",
        "cpu_weight": 1000,
        "cpu_quota_pct": None,
        "memory_limit_mb": 2048.0,
    },
    "sam_prelabel": {
        "description": "MobileSAM batch pre-labeling offline toolchain",
        "parent_slice": "root",
        "cpuset_cpus": "4-7,12-15",
        "cpu_weight": 200,
        "cpu_quota_pct": 50.0,
        "memory_limit_mb": 1536.0,
    },
    "ci_runner": {
        "description": "GitHub Actions runner / compilation jobs",
        "parent_slice": "sam_prelabel",
        "cpuset_cpus": "4-7,12-15",
        "cpu_weight": 100,
        "cpu_quota_pct": 30.0,
        "memory_limit_mb": 1024.0,
    },
}

# Static contract the policy must satisfy. validate_cgroup_configuration() reads the
# live CGROUP_BOUNDARIES mapping against these expectations, so tampering fails loudly.
CGROUP_EXPECTATIONS = {
    "cv_engine": {
        "physical_cores": {0, 1, 2, 3},
        "parent_slice": "root",
        "cpu_quota_pct": None,
        "memory_limit_mb": 2048.0,
    },
    "sam_prelabel": {
        "physical_cores": {4, 5, 6, 7},
        "parent_slice": "root",
        "cpu_quota_pct": 50.0,
        "memory_limit_mb": 1536.0,
    },
    "ci_runner": {
        "physical_cores": {4, 5, 6, 7},
        "parent_slice": "sam_prelabel",
        "cpu_quota_pct": 30.0,
        "memory_limit_mb": 1024.0,
    },
}

# Off-Peak Temporal Window: 01:00 to 05:00 ICT (UTC+7) -> 18:00 to 22:00 UTC.
# The window is end-exclusive: 05:00 ICT is already peak traffic.
OFFPEAK_WINDOWS = {
    "ict": (1, 5),
    "utc": (18, 22),
}
ICT = timezone(timedelta(hours=7))

# Canonical scenario order. The matrix validator depends on it.
SCENARIO_IDS: Tuple[str, ...] = ("baseline", "cotenancy_sam", "cotenancy_full", "loadshed_320")

# Reference capacity profile adopted for the target host (planning figures, not a live run).
# Keys are intentionally explicit: a reader must not mistake these for a fresh measurement.
REFERENCE_SCENARIO_PROFILES: Dict[str, Dict[str, Any]] = {
    "baseline": {
        "name": "Scenario 1: Baseline (Hotset Sweep Only)",
        "description": "cv-engine running 57-camera hotset sweep at 416 px resolution, zero background contention",
        "resolution": 416,
        "p50_ms": 11.9,
        "p95_ms": 14.2,
        "p99_ms": 16.8,
        "fps": 70.4,
        "cpu_util_pct": 18.2,
        "rss_ram_mb": 124.0,
        "active_batch_workers": 0,
        "ci_runner_active": False,
    },
    "cotenancy_sam": {
        "name": "Scenario 2: Co-Tenancy Level 1 (Hotset + SAM Batch)",
        "description": "cv-engine running 57-camera hotset sweep (416 px) + concurrent MobileSAM batch (4 workers)",
        "resolution": 416,
        "p50_ms": 16.5,
        "p95_ms": 21.8,
        "p99_ms": 24.5,
        "fps": 45.8,
        "cpu_util_pct": 62.4,
        "rss_ram_mb": 682.0,
        "active_batch_workers": 4,
        "ci_runner_active": False,
    },
    "cotenancy_full": {
        "name": "Scenario 3: Co-Tenancy Level 2 (Hotset + SAM + CI Build)",
        "description": "cv-engine running 57-camera hotset sweep (416 px) + MobileSAM batch (4 workers) + CI build runner",
        "resolution": 416,
        "p50_ms": 22.4,
        "p95_ms": 28.6,
        "p99_ms": 34.2,
        "fps": 34.9,
        "cpu_util_pct": 91.8,
        "rss_ram_mb": 1150.0,
        "active_batch_workers": 4,
        "ci_runner_active": True,
    },
    "loadshed_320": {
        "name": "Scenario 4: Adaptive Load-Shedding (320 px under Full Contention)",
        "description": "Automatic degradation policy downgrades resolution from 416 px to 320 px under Level 2 contention",
        "resolution": 320,
        "p50_ms": 9.4,
        "p95_ms": 12.2,
        "p99_ms": 14.6,
        "fps": 82.0,
        "cpu_util_pct": 68.1,
        "rss_ram_mb": 652.0,
        "active_batch_workers": 4,
        "ci_runner_active": True,
    },
}

MEASUREMENT_SOURCE = "reference-profile"
MEASUREMENT_NOTE = (
    "Values are the reference capacity profile adopted for the target AMD Ryzen 7 "
    "6800HS worker and are reported as planning figures. They are not a live "
    "measurement performed by this script. Run tools/eval/verify_s10_t8_manual.py to "
    "exercise the live policy and cgroup invariants on the host that executes it."
)


def calculate_percentiles(latencies_ms: Sequence[float]) -> Tuple[float, float, float]:
    """Calculate p50, p95, and p99 from a sequence of latency measurements."""
    if not latencies_ms:
        return 0.0, 0.0, 0.0
    s = sorted(latencies_ms)
    n = len(s)

    def p(pct: float) -> float:
        k = (n - 1) * (pct / 100.0)
        f = math.floor(k)
        c = math.ceil(k)
        if f == c:
            return s[int(k)]
        d0 = s[int(f)] * (c - k)
        d1 = s[int(c)] * (k - f)
        return d0 + d1

    return round(p(50), 2), round(p(95), 2), round(p(99), 2)


def _window_bounds(tz_mode: str) -> Tuple[int, int]:
    try:
        return OFFPEAK_WINDOWS[tz_mode]
    except KeyError:
        raise ValueError(f"Unknown timezone mode {tz_mode!r}; expected one of {sorted(OFFPEAK_WINDOWS)}")


def is_within_offpeak_window(ts: Optional[datetime] = None, tz_mode: str = "ict") -> bool:
    """Check whether `ts` falls inside the authorized off-peak window.

    `tz_mode` selects how the clock is interpreted: "ict" is the canonical policy
    window (01:00-05:00 ICT), "utc" is the equivalent 18:00-22:00 UTC window used by
    the host crontab. A naive datetime is assumed to already be on the selected clock.
    """
    start, end = _window_bounds(tz_mode)
    if ts is None:
        ts = datetime.now(timezone.utc)
    if tz_mode == "ict":
        dt = ts.astimezone(ICT) if ts.tzinfo is not None else ts
    else:
        dt = ts.astimezone(timezone.utc) if ts.tzinfo is not None else ts
    return start <= dt.hour < end


def _expand_cron_field(field: str, minimum: int, maximum: int) -> Set[int]:
    """Expand a cron field (``*``, ``a-b``, ``a,b``, ``*/n``, ``a-b/n``)."""
    values: Set[int] = set()
    for part in field.split(","):
        part = part.strip()
        if not part:
            raise ValueError("empty field component")
        step = 1
        if "/" in part:
            part, step_raw = part.split("/", 1)
            step = int(step_raw)
            if step <= 0:
                raise ValueError("step must be positive")
        if part == "*":
            start, end = minimum, maximum
        elif "-" in part:
            start_raw, end_raw = part.split("-", 1)
            start, end = int(start_raw), int(end_raw)
        else:
            start = int(part)
            # Cron's "a/n" means "from a to the maximum, every n" (e.g. 2/3 -> 2,5,8...).
            end = maximum if step > 1 else start
        if not (minimum <= start <= maximum and minimum <= end <= maximum):
            raise ValueError(f"value out of range {minimum}-{maximum}")
        if start > end:
            raise ValueError("range start is greater than end")
        values.update(range(start, end + 1, step))
    return values


def validate_cron_schedule(cron_expression: str, tz_mode: str = "ict") -> Tuple[bool, str]:
    """Validate that a 5-part cron expression fires only inside the off-peak window.

    `tz_mode="ict"` validates the canonical 01:00-05:00 ICT policy window;
    `tz_mode="utc"` validates the equivalent 18:00-22:00 UTC host crontab window.
    """
    start, end = _window_bounds(tz_mode)
    parts = cron_expression.strip().split()
    if len(parts) != 5:
        return False, f"Invalid cron format (expected 5 fields, got {len(parts)})"

    minute, hour, _dom, _month, _dow = parts
    if hour.strip() == "*" or hour.strip().startswith("*/"):
        return False, "Wildcard '*' in hour violates off-peak window; runs every hour"

    try:
        hours = _expand_cron_field(hour, 0, 23)
    except ValueError as exc:
        return False, f"Failed to parse hour field '{hour}': {exc}"

    outside = sorted(h for h in hours if not (start <= h < end))
    if outside:
        return False, (
            f"Hour(s) {', '.join(f'{h:02d}:00' for h in outside)} outside authorized "
            f"window ({start:02d}:00-{end:02d}:00 {tz_mode.upper()})"
        )

    # Minutes are not part of the window boundary rule, but an unparseable minute is
    # still a broken schedule and must not be accepted as valid.
    try:
        _expand_cron_field(minute, 0, 59)
    except ValueError as exc:
        return False, f"Failed to parse minute field '{minute}': {exc}"

    return True, f"Valid off-peak schedule ({hour} inside {start:02d}:00-{end:02d}:00 {tz_mode.upper()})"


def evaluate_loadshedding_policy(
    p95_latency_ms: float,
    cpu_util_pct: float,
    current_resolution: int = 416,
) -> Dict[str, Any]:
    """Evaluate the adaptive load-shedding policy for one observation window.

      - If p95 > 25.0 ms or CPU > 85%:
          current 416 -> DEGRADE_RESOLUTION_320
          current 320 -> THROTTLE_BACKGROUND_BATCH
      - Else if p95 <= 20.0 ms and CPU <= 70%:
          current 320 -> RECOVER_RESOLUTION_416
          otherwise   -> NOMINAL
    """
    ceiling = WORKER_HARDWARE["p95_sla_ceiling_ms"]
    degrade_cpu = WORKER_HARDWARE["degrade_cpu_threshold_pct"]
    recover_p95 = WORKER_HARDWARE["recover_p95_threshold_ms"]
    recover_cpu = WORKER_HARDWARE["recover_cpu_threshold_pct"]

    action = "NOMINAL"
    recommended_res = current_resolution
    reason = "Inference latency within nominal SLA budget"

    if p95_latency_ms > ceiling or cpu_util_pct > degrade_cpu:
        if current_resolution == 416:
            action = "DEGRADE_RESOLUTION_320"
            recommended_res = 320
            reason = (
                f"Contention detected: p95 latency {p95_latency_ms:.2f} ms exceeds {ceiling:.1f} ms ceiling "
                f"or CPU {cpu_util_pct:.1f}% > {degrade_cpu:.0f}%. Degrade resolution to 320 px."
            )
        else:
            action = "THROTTLE_BACKGROUND_BATCH"
            recommended_res = 320
            reason = (
                f"Worker saturated even at 320 px (p95={p95_latency_ms:.2f} ms, CPU={cpu_util_pct:.1f}%). "
                f"Throttle or pause background batch workers."
            )
    elif p95_latency_ms <= recover_p95 and cpu_util_pct <= recover_cpu:
        if current_resolution == 320:
            action = "RECOVER_RESOLUTION_416"
            recommended_res = 416
            reason = (
                f"Load eased: p95 latency {p95_latency_ms:.2f} ms <= {recover_p95:.1f} ms and CPU "
                f"{cpu_util_pct:.1f}% <= {recover_cpu:.0f}%. Recover resolution to 416 px."
            )

    return {
        "action": action,
        "current_resolution": current_resolution,
        "recommended_resolution": recommended_res,
        "p95_latency_ms": p95_latency_ms,
        "cpu_util_pct": cpu_util_pct,
        "reason": reason,
    }


def expand_cpuset(cpuset: str) -> Set[int]:
    """Expand a Linux cpuset expression such as ``"0-3,8-11"`` into thread ids."""
    threads: Set[int] = set()
    for part in cpuset.split(","):
        part = part.strip()
        if not part:
            continue
        if "-" in part:
            start_raw, end_raw = part.split("-", 1)
            start, end = int(start_raw), int(end_raw)
            if start > end:
                raise ValueError(f"Invalid cpuset range {part!r}")
            threads.update(range(start, end + 1))
        else:
            threads.add(int(part))
    return threads


def physical_core_ids(cpuset: str, physical_cores: int = 8) -> Set[int]:
    """Map a thread cpuset onto physical core ids (thread t belongs to core t % N).

    This is the Linux 8C/16T enumeration used by the AMD Ryzen 7 6800HS: threads
    0-7 are the first SMT sibling per core, threads 8-15 the second.
    """
    threads = expand_cpuset(cpuset)
    if any(t < 0 or t >= physical_cores * 2 for t in threads):
        raise ValueError(f"Thread id outside 0-{physical_cores * 2 - 1} in cpuset {cpuset!r}")
    return {t % physical_cores for t in threads}


CGROUP_FS_ROOT = Path("/sys/fs/cgroup")


def cgroup_validation_environment_notice() -> Optional[str]:
    """Return a notice when live cgroup files cannot describe this host.

    The policy is validated against the schema on any platform, but a macOS or
    container host has no ``/sys/fs/cgroup`` tree to read. Saying so explicitly
    keeps a schema check from being mistaken for a live kernel inspection.
    """
    if sys.platform.startswith("linux") and CGROUP_FS_ROOT.exists():
        return None

    host = {"darwin": "macOS", "win32": "Windows"}.get(sys.platform)
    if host is None:
        host = platform.system() or sys.platform
    return (
        f"Validating cgroup policy configuration (Host: {host}, cgroups not present "
        "locally; container cgroup boundaries verified against schema)"
    )


def verify_cgroup_configuration(
    boundaries: Optional[Dict[str, Dict[str, Any]]] = None,
    hardware: Optional[Dict[str, Any]] = None,
) -> Dict[str, Any]:
    """Validate the cgroup boundaries against the policy contract.

    Every invariant is recomputed from the mapping, not asserted as a constant, so a
    tampered boundary (wrong core set, dropped quota, inflated memory) is reported as
    a violation instead of passing silently (Lesson 66).
    """
    boundaries = boundaries if boundaries is not None else CGROUP_BOUNDARIES
    hardware = hardware if hardware is not None else WORKER_HARDWARE
    physical_cores = int(hardware.get("physical_cores", 8))
    total_ram_mb = float(hardware["total_ram_mb"])

    checks: List[Dict[str, Any]] = []

    def check(name: str, condition: bool, detail: str) -> None:
        checks.append({"name": name, "passed": bool(condition), "detail": detail})

    for slice_name, expected in CGROUP_EXPECTATIONS.items():
        boundary = boundaries.get(slice_name)
        if boundary is None:
            check(f"{slice_name}.present", False, f"Missing cgroup slice {slice_name!r}")
            continue
        try:
            cores = physical_core_ids(str(boundary["cpuset_cpus"]), physical_cores)
        except (KeyError, ValueError) as exc:
            check(f"{slice_name}.cpuset", False, f"Unreadable cpuset: {exc}")
            cores = set()
        check(
            f"{slice_name}.physical_cores",
            cores == expected["physical_cores"],
            f"physical cores {sorted(cores)} != required {sorted(expected['physical_cores'])}",
        )
        check(
            f"{slice_name}.parent_slice",
            boundary.get("parent_slice") == expected["parent_slice"],
            f"parent slice {boundary.get('parent_slice')!r} != {expected['parent_slice']!r}",
        )
        check(
            f"{slice_name}.cpu_quota_pct",
            boundary.get("cpu_quota_pct") == expected["cpu_quota_pct"],
            f"cpu quota {boundary.get('cpu_quota_pct')!r} != {expected['cpu_quota_pct']!r}",
        )
        check(
            f"{slice_name}.memory_limit_mb",
            boundary.get("memory_limit_mb") == expected["memory_limit_mb"],
            f"memory limit {boundary.get('memory_limit_mb')!r} MB != {expected['memory_limit_mb']!r} MB",
        )

    # The latency-critical slice must outrank every background slice.
    cv_weight = boundaries.get("cv_engine", {}).get("cpu_weight", -1)
    for slice_name in ("sam_prelabel", "ci_runner"):
        other_weight = boundaries.get(slice_name, {}).get("cpu_weight", -1)
        check(
            f"priority.cv_engine_over_{slice_name}",
            cv_weight > other_weight,
            f"cpu_weight cv_engine={cv_weight} must exceed {slice_name}={other_weight}",
        )

    # Background slices must not escape the batch core group.
    batch_cores = CGROUP_EXPECTATIONS["sam_prelabel"]["physical_cores"]
    for slice_name in ("sam_prelabel", "ci_runner"):
        boundary = boundaries.get(slice_name)
        if not boundary:
            continue
        try:
            cores = physical_core_ids(str(boundary["cpuset_cpus"]), physical_cores)
        except (KeyError, ValueError):
            continue
        check(
            f"{slice_name}.within_batch_group",
            cores <= batch_cores,
            f"physical cores {sorted(cores)} escape batch group {sorted(batch_cores)}",
        )

    # ci_runner is a child of the batch slice: its memory ceiling may not exceed its
    # parent's, and the two root slices must fit inside the allocable ceiling.
    ci_limit = boundaries.get("ci_runner", {}).get("memory_limit_mb")
    sam_limit = boundaries.get("sam_prelabel", {}).get("memory_limit_mb")
    if isinstance(ci_limit, (int, float)) and isinstance(sam_limit, (int, float)):
        check(
            "memory.ci_within_sam_slice",
            ci_limit <= sam_limit,
            f"ci_runner {ci_limit} MB exceeds parent sam_prelabel {sam_limit} MB",
        )
    root_limits = []
    for name in ("cv_engine", "sam_prelabel"):
        if name in boundaries:
            mem = boundaries[name].get("memory_limit_mb")
            if isinstance(mem, (int, float)):
                root_limits.append(float(mem))
            else:
                check(f"{name}.memory_limit_mb.present", False, f"missing memory_limit_mb on slice {name}")
    root_total = sum(root_limits)
    check(
        "memory.root_slices_fit_ceiling",
        root_total <= total_ram_mb,
        f"root slice limits total {root_total:.0f} MB > allocable {total_ram_mb:.0f} MB",
    )

    violations = [c for c in checks if not c["passed"]]
    return {
        "verification_mode": "static-policy",
        "cgroup_version": 2,
        "slices_configured": list(boundaries.keys()),
        "boundaries": copy.deepcopy(boundaries),
        "checks": checks,
        "violations": violations,
        "root_memory_total_mb": root_total,
        "allocable_memory_mb": total_ram_mb,
        "verified": not violations,
    }


def run_benchmark_scenario(
    scenario_id: str,
    hotset_cameras: int = 57,
    dry_run: bool = True,
) -> Dict[str, Any]:
    """Return the reference profile for one scenario with SLA arithmetic applied.

    `dry_run` is retained for CLI compatibility; this harness never saturates the host.
    """
    del dry_run
    if scenario_id not in REFERENCE_SCENARIO_PROFILES:
        raise ValueError(
            f"Unknown scenario ID: {scenario_id}. Expected one of {sorted(REFERENCE_SCENARIO_PROFILES)}"
        )

    prof = REFERENCE_SCENARIO_PROFILES[scenario_id]
    sweep_duration_s = round((hotset_cameras / prof["fps"]), 2)
    budget_s = WORKER_HARDWARE["cycle_budget_s"]
    headroom_pct = round(((budget_s - sweep_duration_s) / budget_s) * 100.0, 1)

    ceilings = (
        prof["p95_ms"] <= WORKER_HARDWARE["p95_sla_ceiling_ms"]
        and prof["cpu_util_pct"] <= WORKER_HARDWARE["degrade_cpu_threshold_pct"]
        and sweep_duration_s <= budget_s
    )

    return {
        "scenario_id": scenario_id,
        "name": prof["name"],
        "description": prof["description"],
        "measurement_source": MEASUREMENT_SOURCE,
        "resolution": prof["resolution"],
        "active_batch_workers": prof["active_batch_workers"],
        "ci_runner_active": prof["ci_runner_active"],
        "p50_latency_ms": prof["p50_ms"],
        "p95_latency_ms": prof["p95_ms"],
        "p99_latency_ms": prof["p99_ms"],
        "throughput_fps": prof["fps"],
        "cpu_utilization_pct": prof["cpu_util_pct"],
        "memory_rss_mb": prof["rss_ram_mb"],
        "sweep_duration_s": sweep_duration_s,
        "cycle_budget_s": budget_s,
        "headroom_pct": headroom_pct,
        "sla_passed": ceilings,
        "sla_status": "PASSED" if ceilings else "VIOLATED",
    }


def validate_benchmark_matrix(scenarios: Sequence[Dict[str, Any]]) -> Dict[str, Any]:
    """Assert the capacity story the matrix is supposed to tell.

    The matrix is only useful if it demonstrates three things: baseline and SAM-only
    co-tenancy fit the budget, full contention breaches it, and 320 px shedding
    restores it. A regression that silently flips any of those must fail here.
    """
    by_id = {s["scenario_id"]: s for s in scenarios}
    errors: List[str] = []

    if not scenarios:
        return {"valid": False, "errors": ["No scenarios provided"]}

    for scenario in scenarios:
        sid = scenario["scenario_id"]
        if sid not in SCENARIO_IDS:
            errors.append(f"Unknown scenario id: {sid}")
    if len(scenarios) != len(by_id):
        errors.append("Duplicate scenario ids in the matrix; each scenario may appear once")

    # A subset is a legitimate request (``--scenario baseline``). Completeness is only
    # a requirement when the caller is presenting the full matrix: four rows for the
    # four theory-of-record scenarios. Half a matrix is validated on its own terms.
    full_matrix_requested = len(scenarios) == len(SCENARIO_IDS)
    if full_matrix_requested:
        missing = [sid for sid in SCENARIO_IDS if sid not in by_id]
        if missing:
            errors.append(f"Missing scenarios: {', '.join(missing)}")
            return {"valid": False, "errors": errors}

    ceiling = WORKER_HARDWARE["p95_sla_ceiling_ms"]
    degrade_cpu = WORKER_HARDWARE["degrade_cpu_threshold_pct"]
    budget = WORKER_HARDWARE["cycle_budget_s"]

    for scenario in scenarios:
        sid = scenario["scenario_id"]
        if not (scenario["p50_latency_ms"] <= scenario["p95_latency_ms"] <= scenario["p99_latency_ms"]):
            errors.append(f"{sid}: percentiles are not monotonic p50 <= p95 <= p99")
        if scenario["resolution"] not in (320, 416, 640):
            errors.append(f"{sid}: resolution {scenario['resolution']} outside the serving contract")
        if scenario["memory_rss_mb"] > WORKER_HARDWARE["total_ram_mb"]:
            errors.append(f"{sid}: RSS {scenario['memory_rss_mb']} MB exceeds worker RAM")
        if scenario["sweep_duration_s"] > budget:
            errors.append(f"{sid}: sweep {scenario['sweep_duration_s']} s exceeds {budget} s budget")
        if scenario["sla_status"] != ("PASSED" if scenario["sla_passed"] else "VIOLATED"):
            errors.append(f"{sid}: sla_status disagrees with sla_passed")
        expected_pass = (
            scenario["p95_latency_ms"] <= ceiling
            and scenario["cpu_utilization_pct"] <= degrade_cpu
            and scenario["sweep_duration_s"] <= budget
        )
        if scenario["sla_passed"] != expected_pass:
            errors.append(f"{sid}: sla_passed disagrees with the measured ceilings")

    # Cross-scenario claims are only meaningful when both sides are present. A
    # single-scenario run validates that scenario's internal consistency above and
    # must not be failed for the four-scenario narrative it never claimed to tell.
    baseline = by_id.get("baseline")
    if baseline is not None and (not baseline["sla_passed"] or baseline["resolution"] != 416):
        errors.append("baseline must pass the SLA at 416 px")

    sam = by_id.get("cotenancy_sam")
    if sam is not None and (not sam["sla_passed"] or sam["active_batch_workers"] != 4):
        errors.append("SAM-only co-tenancy must pass the SLA with 4 batch workers")

    full = by_id.get("cotenancy_full")
    if full is not None:
        if full["sla_passed"]:
            errors.append("full contention must breach the SLA; otherwise the policy is untested")
        if full["p95_latency_ms"] <= ceiling and full["cpu_utilization_pct"] <= degrade_cpu:
            errors.append("full contention must trip at least one degradation threshold")

    shed = by_id.get("loadshed_320")
    if shed is not None:
        if not shed["sla_passed"] or shed["resolution"] != 320:
            errors.append("320 px load-shedding must pass the SLA at 320 px")
        if full is not None:
            if shed["p95_latency_ms"] >= full["p95_latency_ms"]:
                errors.append("320 px shedding must reduce p95 latency versus full contention")
            if shed["active_batch_workers"] != full["active_batch_workers"]:
                errors.append("shedding must not silently remove the background load it is measuring")

    return {"valid": not errors, "errors": errors}


def run_all_benchmarks(dry_run: bool = True) -> Dict[str, Any]:
    """Build the 4-scenario matrix plus cgroup and matrix validation."""
    results = [run_benchmark_scenario(sid, dry_run=dry_run) for sid in SCENARIO_IDS]
    cgroup_status = verify_cgroup_configuration()
    matrix = validate_benchmark_matrix(results)

    return {
        "timestamp": datetime.now(timezone.utc).isoformat(),
        "measurement_source": MEASUREMENT_SOURCE,
        "measurement_note": MEASUREMENT_NOTE,
        "hardware": WORKER_HARDWARE,
        "cgroup_status": cgroup_status,
        "matrix_validation": matrix,
        "scenarios": results,
    }


def format_markdown_report(benchmark_data: Dict[str, Any]) -> str:
    """Format benchmark results into a GitHub-flavored Markdown table."""
    cgroup = benchmark_data["cgroup_status"]
    lines = [
        "# Inference Worker Co-Tenancy Benchmark & Capacity Report",
        "",
        "Capacity envelope for the single inference worker under multi-tenant resource contention "
        "(Sprint 10 Issue #379 / ADR-012 §3 (S10-T8) / §1.2 (F5)).",
        "",
        "> **Measurement provenance.** The latency, throughput, CPU and RSS values below are the "
        "reference capacity profile adopted for the target AMD Ryzen 7 6800HS worker and are "
        "reported as planning figures, not as a fresh measurement. "
        "`tools/eval/verify_s10_t8_manual.py` exercises the policy, cgroup and cron mechanisms "
        "on the host that runs it.",
        "",
        "---",
        "",
        "## 1. Hardware Profile",
        "",
        f"- **Processor**: {benchmark_data['hardware']['cpu']}",
        f"- **Base / Boost Frequency**: {benchmark_data['hardware']['base_freq_ghz']} GHz / {benchmark_data['hardware']['boost_freq_ghz']} GHz",
        f"- **Allocated Cgroup Memory**: {benchmark_data['hardware']['total_ram_mb']} MB (3.8 GB)",
        f"- **Hot Set Cameras**: {benchmark_data['hardware']['hotset_cameras']} cameras",
        f"- **Cycle SLA Budget**: {benchmark_data['hardware']['cycle_budget_s']} seconds",
        f"- **p95 Latency Ceiling**: {benchmark_data['hardware']['p95_sla_ceiling_ms']} ms",
        "",
        "---",
        "",
        "## 2. Co-Tenancy Contention Benchmark Matrix",
        "",
        "| Scenario | Resolution | p50 Latency | p95 Latency | Throughput | CPU Util | RSS RAM | 57-Cam Sweep | Headroom | SLA Status |",
        "|---|:---:|:---:|:---:|:---:|:---:|:---:|:---:|:---:|:---:|",
    ]

    for s in benchmark_data["scenarios"]:
        lines.append(
            f"| {s['name']} | {s['resolution']} px | {s['p50_latency_ms']} ms | {s['p95_latency_ms']} ms | "
            f"{s['throughput_fps']} FPS | {s['cpu_utilization_pct']}% | {s['memory_rss_mb']} MB | "
            f"{s['sweep_duration_s']} s | {s['headroom_pct']}% | **{s['sla_status']}** |"
        )

    matrix = benchmark_data.get("matrix_validation", {})
    matrix_state = "PASSED" if matrix.get("valid") else "FAILED"
    lines.extend([
        "",
        f"Matrix invariant validation: **{matrix_state}**.",
        "",
        "---",
        "",
        "## 3. Key Findings & Load-Shedding Analysis",
        "",
        "1. **Unchecked contention violates the SLA.** Under Scenario 3 (concurrent SAM batch plus CI compilation), "
        "p95 latency degrades to **28.6 ms**, above the 25.0 ms ceiling, and CPU reaches **91.8%**.",
        "2. **Adaptive 320 px degradation recovers the budget.** At 320 px under the same background load, p95 falls "
        "to **12.2 ms** and the 57-camera sweep completes in **0.70 s**.",
        "3. **Cgroup CPU affinity isolates the latency-critical slice.** CV-engine owns physical cores 0-3; background "
        "work is confined to cores 4-7 behind a 50% quota and a 1.5 GB ceiling, with the CI runner nested beneath it.",
        "",
        "---",
        "",
        "## 4. Operational Boundaries & Policy Rules",
        "",
        f"- **Cgroup invariants**: {'verified' if cgroup['verified'] else 'VIOLATED'} "
        f"({len(cgroup['checks'])} checks, {len(cgroup['violations'])} violations).",
        "- **Off-peak execution only**: SAM batch pre-labeling is restricted to **01:00-05:00 ICT** (18:00-22:00 UTC).",
        "- **Degradation trigger**: p95 > 25.0 ms or CPU > 85% moves cv-engine from 416 px to 320 px "
        "(`X-Target-Resolution: 320`).",
        "- **Hysteresis recovery**: 320 px returns to 416 px only when p95 <= 20.0 ms and CPU <= 70%, so the "
        "worker cannot oscillate at the boundary.",
        "",
        "---",
        "",
        "## 5. Reproduction",
        "",
        "```bash",
        "python3 tools/eval/benchmark_cotenancy.py --scenario all --strict",
        "python3 tools/eval/verify_s10_t8_manual.py",
        "python3 -m unittest tools/eval/benchmark_cotenancy_test.py",
        "```",
    ])

    return "\n".join(lines)


def main() -> int:
    parser = argparse.ArgumentParser(
        description="Inference Worker Co-Tenancy Benchmark & Load-Shedding Harness (S10-T8)."
    )
    parser.add_argument(
        "--scenario",
        type=str,
        default="all",
        choices=["all", *SCENARIO_IDS],
        help="Benchmark scenario to build (default: all)",
    )
    parser.add_argument("--output-json", type=Path, default=None, help="Optional path for JSON results")
    parser.add_argument("--output-markdown", type=Path, default=None, help="Optional path for the Markdown report")
    parser.add_argument("--verify-cgroups", action="store_true", help="Validate cgroup boundary invariants")
    parser.add_argument(
        "--check-cron-window",
        type=str,
        default=None,
        help="Timestamp, or 'now', checked against the off-peak window. Exits 1 when outside.",
    )
    parser.add_argument(
        "--cron-tz",
        type=str,
        default="ict",
        choices=sorted(OFFPEAK_WINDOWS),
        help="Clock used by --check-cron-window (default: ict)",
    )
    parser.add_argument(
        "--validate-cron",
        type=str,
        default=None,
        help="Validate a 5-field cron expression against the off-peak window and exit.",
    )
    parser.add_argument(
        "--strict",
        action="store_true",
        help="Exit 1 when cgroup invariants or matrix invariants fail.",
    )
    parser.add_argument(
        "--dry-run",
        action="store_true",
        default=True,
        help="Accepted for compatibility; the harness never saturates the host.",
    )
    args = parser.parse_args()

    if args.validate_cron:
        ok, message = validate_cron_schedule(args.validate_cron, tz_mode=args.cron_tz)
        log = logger.info if ok else logger.error
        log("Cron schedule %r: %s", args.validate_cron, message)
        return 0 if ok else 1

    if args.check_cron_window:
        if args.check_cron_window.lower() == "now":
            dt = datetime.now(timezone.utc)
        else:
            dt = datetime.fromisoformat(args.check_cron_window)
        allowed = is_within_offpeak_window(dt, tz_mode=args.cron_tz)
        status_str = "AUTHORIZED (Off-Peak)" if allowed else "FORBIDDEN (Daytime / Peak Traffic)"
        logger.info("Cron window check for %s: %s", dt.isoformat(), status_str)
        if not allowed:
            return 1

    cgroup = verify_cgroup_configuration()
    if args.verify_cgroups:
        environment_notice = cgroup_validation_environment_notice()
        if environment_notice:
            # Printed verbatim so the operator sees the validation scope before the
            # structured log lines, including on hosts without a cgroup filesystem.
            print(f"[INFO] {environment_notice}", flush=True)
        if cgroup["verified"]:
            logger.info(
                "Cgroup configuration verified: %d checks passed, root memory %.0f/%.0f MB",
                len(cgroup["checks"]),
                cgroup["root_memory_total_mb"],
                cgroup["allocable_memory_mb"],
            )
        else:
            logger.error("Cgroup configuration VIOLATED: %d violation(s)", len(cgroup["violations"]))
            for violation in cgroup["violations"]:
                logger.error("  %s: %s", violation["name"], violation["detail"])
        for slice_name, boundary in cgroup["boundaries"].items():
            logger.info(
                "  [%s] cpuset: %s, quota: %s%%, memory: %.1f MB",
                slice_name,
                boundary["cpuset_cpus"],
                boundary.get("cpu_quota_pct"),
                boundary["memory_limit_mb"],
            )

    if args.scenario == "all":
        data = run_all_benchmarks()
    else:
        results = [run_benchmark_scenario(args.scenario)]
        data = {
            "timestamp": datetime.now(timezone.utc).isoformat(),
            "measurement_source": MEASUREMENT_SOURCE,
            "measurement_note": MEASUREMENT_NOTE,
            "hardware": WORKER_HARDWARE,
            "cgroup_status": cgroup,
            "matrix_validation": validate_benchmark_matrix(results),
            "scenarios": results,
        }

    logger.info("=== CO-TENANCY BENCHMARK RESULTS SUMMARY ===")
    for s in data["scenarios"]:
        logger.info(
            "  [%s] Res: %d px | p50: %.1f ms | p95: %.1f ms | FPS: %.1f | CPU: %.1f%% | RAM: %.1f MB | SLA: %s",
            s["scenario_id"], s["resolution"], s["p50_latency_ms"], s["p95_latency_ms"],
            s["throughput_fps"], s["cpu_utilization_pct"], s["memory_rss_mb"], s["sla_status"],
        )
    logger.info("Measurement source: %s", MEASUREMENT_SOURCE)
    matrix = data["matrix_validation"]
    if matrix["valid"]:
        logger.info("Matrix invariants: PASSED")
    else:
        for error in matrix["errors"]:
            logger.error("Matrix invariant failed: %s", error)

    if args.output_json:
        args.output_json.parent.mkdir(parents=True, exist_ok=True)
        with open(args.output_json, "w", encoding="utf-8") as fh:
            json.dump(data, fh, indent=2)
            fh.write("\n")
        logger.info("Wrote JSON results to %s", args.output_json)

    if args.output_markdown:
        args.output_markdown.parent.mkdir(parents=True, exist_ok=True)
        with open(args.output_markdown, "w", encoding="utf-8") as fh:
            fh.write(format_markdown_report(data))
            fh.write("\n")
        logger.info("Wrote Markdown report to %s", args.output_markdown)

    if args.strict and (not cgroup["verified"] or not matrix["valid"]):
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
