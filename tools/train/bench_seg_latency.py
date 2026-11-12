#!/usr/bin/env python3
"""
bench_seg_latency.py — AVX2 worker latency benchmark for the segmentation IR (S12-T5 / Issue #411).

A latency number is only evidence if it was measured on the hardware that will
serve the request. 13.7 ms p95 on an Apple M4 says nothing about the x86-64
AVX2 inference worker, so this harness refuses to run anywhere else and writes
no evidence file when it refuses (spec §2.9 / AC17).

Protocol:
  read_model -> reshape([-1, 3, -1, -1]) -> compile_model("CPU") at default
  thread settings; 10 warm-ups; 200 timed runs x 3 repeats at 416 px, then the
  same at 320 px (the cv-engine load-shedding size). The gate number is the
  median of the per-repeat p95 values at 416 px; the worst repeat is disclosed
  beside it so a noisy host cannot hide behind a lucky median.

Gate: median-of-repeats p95 at 416 px <= 25.0 ms.

Usage:
  python3 tools/train/bench_seg_latency.py \
      --ir models/yolov8n_seg_openvino_416 \
      --frame data/proofs/proofs/2026-11-08/<camera>/<ts>_sample.jpg \
      --out docs/reports/s12_t5_latency_avx2.json

Exit codes: 0 gate passed, 1 gate failed, 2 refused (host or untrained head).

Contract tests: tools/train/bench_seg_latency_test.py
"""

from __future__ import annotations

import argparse
import json
import logging
import math
import os
import platform
import statistics
import sys
import time
from datetime import datetime, timedelta, timezone
from pathlib import Path
from typing import Any, Dict, List, Optional, Sequence, Tuple

logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s [%(levelname)s] %(message)s",
    datefmt="%H:%M:%S",
)

# ---------------------------------------------------------------------------
# Constants
# ---------------------------------------------------------------------------

EXIT_OK = 0
EXIT_GATE_FAILED = 1
EXIT_REFUSED = 2

GATE_P95_MS = 25.0
TIMED_RUNS = 200
REPEATS = 3
WARMUP_RUNS = 10

PRIMARY_RESOLUTION = 416
SECONDARY_RESOLUTION = 320
ACCEPTED_MACHINES = {"x86_64", "amd64", "x64"}
HOST_CPUINFO_PATH = Path("/proc/cpuinfo")
EVIDENCE_VERSION = "1.0"


# ---------------------------------------------------------------------------
# Errors
# ---------------------------------------------------------------------------


class HostRefusedError(RuntimeError):
    """The host cannot produce gate-eligible latency evidence."""


class UntrainedHeadError(RuntimeError):
    """The IR was exported from an untrained class head; benchmarking it is meaningless."""


# ---------------------------------------------------------------------------
# Host guard
# ---------------------------------------------------------------------------


def _read_cpuinfo(cpuinfo_path: Optional[Path]) -> str:
    path = Path(cpuinfo_path) if cpuinfo_path is not None else HOST_CPUINFO_PATH
    try:
        return path.read_text(encoding="utf-8", errors="replace")
    except OSError:
        return ""


def _cpu_model(cpuinfo: str) -> str:
    for line in cpuinfo.splitlines():
        if line.lower().startswith("model name"):
            return line.split(":", 1)[1].strip()
    return "unknown"


def _has_avx2(cpuinfo: str) -> bool:
    """True when the CPU flag list advertises avx2.

    The flag list wraps across continuation lines in /proc/cpuinfo, so the check
    runs over the whole payload instead of one ``flags`` line.
    """
    import re

    return bool(re.search(r"\bavx2\b", cpuinfo))


def probe_host(
    system: Optional[str] = None,
    machine: Optional[str] = None,
    cpuinfo_path: Optional[Path] = None,
) -> Dict[str, Any]:
    """Require Linux + x86_64 + AVX2, or refuse the host by name."""
    system = system if system is not None else platform.system()
    machine = machine if machine is not None else platform.machine()

    if system != "Linux":
        raise HostRefusedError(
            f"host refusal: {system} is not gate-eligible; Linux x86_64 with AVX2 is "
            f"required for worker latency evidence (got {system}/{machine})"
        )
    if str(machine).lower() not in ACCEPTED_MACHINES:
        raise HostRefusedError(
            f"host refusal: machine {machine} is not gate-eligible; x86_64 is required "
            f"for the AVX2 worker benchmark"
        )

    cpuinfo = _read_cpuinfo(cpuinfo_path)
    if not cpuinfo:
        raise HostRefusedError(
            f"host refusal: avx2 cannot be verified because {cpuinfo_path or HOST_CPUINFO_PATH} "
            f"is unreadable; a host without proven AVX2 produces no gate-eligible latency"
        )
    if not _has_avx2(cpuinfo):
        raise HostRefusedError(
            "host refusal: /proc/cpuinfo does not advertise avx2; a non-AVX2 host runs the "
            "FP16 IR on a fallback path and its latency is not gate-eligible"
        )

    return {
        "system": system,
        "machine": str(machine),
        "avx2": True,
        "cpu_model": _cpu_model(cpuinfo),
    }


# ---------------------------------------------------------------------------
# Percentile mathematics
# ---------------------------------------------------------------------------


def percentile(values: Sequence[float], p: float) -> float:
    """Nearest-rank percentile over the ascending sort (no interpolation)."""
    data = sorted(float(value) for value in values)
    if not data:
        raise ValueError("percentile of an empty series is undefined")
    rank = max(0.0, min(100.0, float(p)))
    index = max(0, int(math.ceil(rank / 100.0 * len(data))) - 1)
    return float(data[index])


# Alias used by the task contract.
nearest_rank_percentile = percentile


def summarize_latencies(values: Sequence[float]) -> Dict[str, Any]:
    """p50/p95/p99 plus count and extremes for one measurement series."""
    data = [float(value) for value in values]
    if not data:
        raise ValueError("cannot summarize an empty latency series")
    return {
        "p50_ms": round(percentile(data, 50), 4),
        "p95_ms": round(percentile(data, 95), 4),
        "p99_ms": round(percentile(data, 99), 4),
        "n": len(data),
        "min_ms": round(min(data), 4),
        "max_ms": round(max(data), 4),
    }


def summarize_repeats(repeats: Sequence[Sequence[float]]) -> Dict[str, Any]:
    """Median-of-repeats summary; the p95 median is the gate number."""
    if not repeats:
        raise ValueError("at least one repeat is required")
    summaries = [summarize_latencies(repeat) for repeat in repeats]
    return {
        "repeats": summaries,
        "p50_ms": round(statistics.median(item["p50_ms"] for item in summaries), 4),
        "p95_ms": round(statistics.median(item["p95_ms"] for item in summaries), 4),
        "p99_ms": round(statistics.median(item["p99_ms"] for item in summaries), 4),
        "n": sum(item["n"] for item in summaries),
        "worst_repeat_p95_ms": round(max(item["p95_ms"] for item in summaries), 4),
    }


# ---------------------------------------------------------------------------
# Gate
# ---------------------------------------------------------------------------


def evaluate_latency_gate(
    p95_ms: float, threshold_ms: float = GATE_P95_MS
) -> Tuple[bool, List[str]]:
    """The graph p95 must not exceed the ceiling; 25.0 passes, 25.01 does not."""
    if float(p95_ms) > float(threshold_ms):
        return False, [
            f"p95 latency {float(p95_ms):.2f} ms exceeds the {float(threshold_ms)} ms ceiling "
            f"at {PRIMARY_RESOLUTION} px on the AVX2 worker"
        ]
    return True, []


# ---------------------------------------------------------------------------
# Metadata trust
# ---------------------------------------------------------------------------


def _metadata_block(metadata: Dict[str, Any]) -> Dict[str, Any]:
    if not isinstance(metadata, dict):
        raise UntrainedHeadError("metadata payload is not an object")
    block = metadata.get("projectkv")
    return block if isinstance(block, dict) else metadata


def require_trained_head(metadata: Dict[str, Any]) -> None:
    """Refuse an IR whose class head was re-initialised instead of trained."""
    block = _metadata_block(metadata)
    status = block.get("head_status")
    if status is None:
        raise UntrainedHeadError(
            "metadata does not declare head_status; the exporter writes it from build stats, "
            "so an absent field means the artifact is unverified"
        )
    if status != "trained":
        raise UntrainedHeadError(
            f"IR head_status is '{status}', not 'trained'; re-initialised COCO weights emit "
            f"zero detections and must never be benchmarked or promoted"
        )
    reinitialised = block.get("reinitialised_tensors")
    if reinitialised is None:
        raise UntrainedHeadError(
            "metadata declares head_status 'trained' but records no reinitialised_tensors count; "
            "the claim is unverifiable"
        )
    if int(reinitialised) != 0:
        raise UntrainedHeadError(
            f"metadata declares head_status 'trained' while {reinitialised} tensor(s) were "
            f"re-initialised; the artifact contradicts itself"
        )


# ---------------------------------------------------------------------------
# Evidence payload
# ---------------------------------------------------------------------------


def _normalized_timestamp(value: Optional[str]) -> str:
    if value is None:
        return datetime.now(timezone.utc).astimezone().isoformat(timespec="seconds")
    parsed = datetime.fromisoformat(str(value).replace("Z", "+00:00"))
    if parsed.tzinfo is None:
        parsed = parsed.replace(tzinfo=timezone.utc)
    if parsed > datetime.now(timezone.utc) + timedelta(seconds=300):
        raise ValueError(f"generated_at {value} is in the future; evidence cannot be pre-dated")
    return parsed.isoformat(timespec="seconds")


def build_payload(
    *,
    repeats: Sequence[Sequence[float]],
    host: Dict[str, Any],
    ir_sha256: str,
    head_status: str,
    generated_at: Optional[str] = None,
    protocol: Optional[Dict[str, Any]] = None,
    resolution_latency: Optional[Dict[str, Any]] = None,
    wall_latency: Optional[Dict[str, Any]] = None,
    frame: Optional[Dict[str, Any]] = None,
    fingerprint: Optional[Dict[str, Any]] = None,
) -> Dict[str, Any]:
    """Build the evidence document, refusing anything that is not a trained head."""
    if str(head_status) != "trained":
        raise UntrainedHeadError(
            f"head_status '{head_status}' cannot be recorded as latency evidence; only a "
            f"trained head produces a meaningful benchmark"
        )

    summary = summarize_repeats(repeats)
    passed, failures = evaluate_latency_gate(summary["p95_ms"])
    payload: Dict[str, Any] = {
        "version": EVIDENCE_VERSION,
        "generated_at": _normalized_timestamp(generated_at),
        "protocol": dict(
            protocol
            or {
                "warmup_runs": WARMUP_RUNS,
                "timed_runs": TIMED_RUNS,
                "repeats": REPEATS,
                "imgsz": PRIMARY_RESOLUTION,
                "gate_resolution": PRIMARY_RESOLUTION,
                "secondary_resolution": SECONDARY_RESOLUTION,
                "compile_device": "CPU",
                "input": "real proof frame, letterboxed",
            }
        ),
        "head_status": str(head_status),
        "ir_sha256": str(ir_sha256),
        "host": dict(host),
        "latency": summary,
        "repeats": summary["repeats"],
        "gate": {
            "metric": "p95_ms",
            "resolution": PRIMARY_RESOLUTION,
            "threshold_ms": GATE_P95_MS,
            "aggregation": "median_of_repeats_p95",
            "verdict": "PASSED" if passed else "FAILED",
            "failures": failures,
        },
    }
    if resolution_latency:
        payload["resolution_latency"] = resolution_latency
    if wall_latency:
        payload["wall_latency"] = wall_latency
    if frame:
        payload["frame"] = frame
    if fingerprint:
        payload["fingerprint"] = fingerprint
    return payload


# ---------------------------------------------------------------------------
# Measurement
# ---------------------------------------------------------------------------


def _load_metadata(path: Path) -> Dict[str, Any]:
    text = Path(path).read_text(encoding="utf-8")
    try:
        import yaml  # type: ignore
    except ImportError:
        return json.loads(text)
    payload = yaml.safe_load(text)
    if not isinstance(payload, dict):
        raise UntrainedHeadError(f"metadata {path} is not a mapping")
    return payload


def _letterbox(image: Any, size: int) -> Any:
    """Resize preserving aspect ratio and pad to a square, like the cv-engine."""
    import numpy as np

    width, height = image.size
    scale = min(size / width, size / height)
    new_width, new_height = max(1, int(round(width * scale))), max(1, int(round(height * scale)))
    resized = image.resize((new_width, new_height))
    canvas = np.full((size, size, 3), 114, dtype=np.uint8)
    top = (size - new_height) // 2
    left = (size - new_width) // 2
    canvas[top : top + new_height, left : left + new_width] = np.asarray(resized)
    return canvas


def _prepare_input(image: Any, size: int) -> Any:
    import numpy as np

    canvas = _letterbox(image, size)
    return (canvas.transpose(2, 0, 1).astype(np.float32) / 255.0)[None, ...]


def _timed_runs(infer_request: Any, tensor: Any, runs: int, warmup: int) -> List[float]:
    for _ in range(warmup):
        infer_request.infer({0: tensor})
    timings: List[float] = []
    for _ in range(runs):
        start = time.perf_counter()
        infer_request.infer({0: tensor})
        timings.append((time.perf_counter() - start) * 1000.0)
    return timings


def _host_fingerprint(openvino_version: str) -> Dict[str, Any]:
    fingerprint: Dict[str, Any] = {
        "kernel": platform.release(),
        "openvino_version": openvino_version,
    }
    governor_path = Path("/sys/devices/system/cpu/cpu0/cpufreq/scaling_governor")
    if governor_path.is_file():
        fingerprint["governor"] = governor_path.read_text(encoding="utf-8").strip()
    lscpu = Path("/usr/bin/lscpu")
    if lscpu.is_file():
        try:
            import subprocess

            completed = subprocess.run(
                [str(lscpu)], capture_output=True, text=True, timeout=15
            )
            if completed.returncode == 0:
                for line in completed.stdout.splitlines():
                    if line.lower().startswith(("model name", "flags")):
                        key, _, value = line.partition(":")
                        fingerprint[key.strip().lower().replace(" ", "_")] = value.strip()
        except Exception:  # noqa: BLE001 - fingerprinting must not fail a run
            pass
    return fingerprint


def run_benchmark(
    xml_path: Path,
    frame_path: Path,
    runs: int = TIMED_RUNS,
    warmup: int = WARMUP_RUNS,
    repeats: int = REPEATS,
) -> Dict[str, Any]:
    """Measure the graph at 416 px (gate) and 320 px (load-shedding)."""
    try:
        import numpy as np  # noqa: F401
        import openvino as ov
        from PIL import Image
    except ImportError as exc:  # pragma: no cover - depends on the worker venv
        raise HostRefusedError(
            f"host refusal: openvino/numpy/Pillow are required for the benchmark: {exc}"
        ) from exc

    core = ov.Core()
    model = core.read_model(str(xml_path))
    model.reshape([-1, 3, -1, -1])
    compiled = core.compile_model(model, "CPU")
    infer_request = compiled.create_infer_request()

    image = Image.open(frame_path).convert("RGB")
    per_resolution: Dict[str, Any] = {}
    gate_repeats: List[List[float]] = []

    for size in (PRIMARY_RESOLUTION, SECONDARY_RESOLUTION):
        tensor = _prepare_input(image, size)
        measured = [
            _timed_runs(infer_request, tensor, runs, warmup) for _ in range(repeats)
        ]
        per_resolution[str(size)] = summarize_repeats(measured)
        if size == PRIMARY_RESOLUTION:
            gate_repeats = measured

    wall_runs: List[float] = []
    for _ in range(max(1, min(runs, 50))):
        start = time.perf_counter()
        tensor = _prepare_input(image, PRIMARY_RESOLUTION)
        infer_request.infer({0: tensor})
        wall_runs.append((time.perf_counter() - start) * 1000.0)

    return {
        "repeats": gate_repeats,
        "resolution_latency": per_resolution,
        "wall_latency": summarize_latencies(wall_runs),
        "fingerprint": _host_fingerprint(getattr(ov, "__version__", "unknown")),
    }


def _sha256(path: Path) -> str:
    import hashlib

    digest = hashlib.sha256()
    with Path(path).open("rb") as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def parse_args(argv: Optional[Sequence[str]] = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="AVX2 worker latency benchmark for the segmentation OpenVINO IR (S12-T5).",
    )
    parser.add_argument("--out", type=Path, required=True, help="Evidence JSON destination")
    parser.add_argument("--ir", type=Path, required=True, help="OpenVINO IR directory")
    parser.add_argument(
        "--metadata", type=Path, default=None, help="metadata.yaml (default: <ir>/metadata.yaml)"
    )
    parser.add_argument("--frame", type=Path, required=True, help="Real proof frame to infer")
    parser.add_argument("--runs", type=int, default=TIMED_RUNS)
    parser.add_argument("--warmup", type=int, default=WARMUP_RUNS)
    parser.add_argument("--repeats", type=int, default=REPEATS)
    return parser.parse_args(argv)


def main(argv: Optional[Sequence[str]] = None) -> int:
    args = parse_args(argv)

    # 1. Host guard first: a refusal must leave no evidence file and no evidence tree.
    try:
        host = probe_host()
    except HostRefusedError as exc:
        logging.error("%s", exc)
        return EXIT_REFUSED

    logging.info(
        "Host accepted: %s %s (%s) with AVX2", host["system"], host["machine"], host["cpu_model"]
    )

    ir_dir = Path(args.ir)
    xml_path = ir_dir / "yolov8n_seg.xml"
    bin_path = ir_dir / "yolov8n_seg.bin"
    metadata_path = Path(args.metadata) if args.metadata else ir_dir / "metadata.yaml"
    frame_path = Path(args.frame)

    # 2. Refuse an untrained head before touching the graph.
    if not metadata_path.is_file():
        logging.error(
            "metadata not found at %s; head_status cannot be verified, so no benchmark may run",
            metadata_path,
        )
        return EXIT_REFUSED
    try:
        metadata = _load_metadata(metadata_path)
        require_trained_head(metadata)
    except (UntrainedHeadError, ValueError, OSError) as exc:
        logging.error("%s", exc)
        return EXIT_REFUSED

    if not xml_path.is_file() or not bin_path.is_file():
        logging.error("IR pair missing under %s; nothing to benchmark", ir_dir)
        return EXIT_REFUSED
    if not frame_path.is_file():
        logging.error("proof frame missing: %s", frame_path)
        return EXIT_REFUSED

    # 3. Measure.
    try:
        measured = run_benchmark(
            xml_path=xml_path,
            frame_path=frame_path,
            runs=max(1, int(args.runs)),
            warmup=max(0, int(args.warmup)),
            repeats=max(1, int(args.repeats)),
        )
    except HostRefusedError as exc:
        logging.error("%s", exc)
        return EXIT_REFUSED
    except Exception as exc:  # noqa: BLE001 - a benchmark crash is a refusal, not a result
        logging.error("benchmark failed before producing a gate-eligible number: %s", exc)
        return EXIT_REFUSED

    payload = build_payload(
        repeats=measured["repeats"],
        host=host,
        ir_sha256=_sha256(bin_path),
        head_status="trained",
        protocol={
            "warmup_runs": max(0, int(args.warmup)),
            "timed_runs": max(1, int(args.runs)),
            "repeats": max(1, int(args.repeats)),
            "imgsz": PRIMARY_RESOLUTION,
            "gate_resolution": PRIMARY_RESOLUTION,
            "secondary_resolution": SECONDARY_RESOLUTION,
            "compile_device": "CPU",
            "input": "real proof frame, letterboxed",
        },
        resolution_latency=measured["resolution_latency"],
        wall_latency=measured["wall_latency"],
        frame={
            "path": str(frame_path),
            "sha256": _sha256(frame_path),
            "bytes": frame_path.stat().st_size,
        },
        fingerprint=dict(
            measured["fingerprint"],
            xml_sha256=_sha256(xml_path),
            metadata_sha256=_sha256(metadata_path),
        ),
    )

    out_path = Path(args.out)
    out_path.parent.mkdir(parents=True, exist_ok=True)
    temporary = out_path.with_suffix(out_path.suffix + ".tmp")
    temporary.write_text(json.dumps(payload, indent=2) + "\n", encoding="utf-8")
    os.replace(temporary, out_path)

    latency = payload["latency"]
    logging.info(
        "Latency at %d px: p50=%.2f ms p95=%.2f ms p99=%.2f ms (n=%d, worst repeat p95=%.2f ms)",
        PRIMARY_RESOLUTION,
        latency["p50_ms"],
        latency["p95_ms"],
        latency["p99_ms"],
        latency["n"],
        latency["worst_repeat_p95_ms"],
    )
    if payload.get("wall_latency"):
        wall = payload["wall_latency"]
        logging.info(
            "Wall (decode + letterbox + infer) p50=%.2f ms p95=%.2f ms p99=%.2f ms",
            wall["p50_ms"],
            wall["p95_ms"],
            wall["p99_ms"],
        )
    logging.info("Evidence written to %s", out_path)

    if payload["gate"]["verdict"] != "PASSED":
        for failure in payload["gate"]["failures"]:
            logging.error("%s", failure)
        return EXIT_GATE_FAILED
    logging.info("Latency gate PASSED at %.2f ms p95 (ceiling %.1f ms)", latency["p95_ms"], GATE_P95_MS)
    return EXIT_OK


if __name__ == "__main__":
    sys.exit(main())
