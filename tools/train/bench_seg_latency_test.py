#!/usr/bin/env python3
"""
Contract tests for tools/train/bench_seg_latency.py (S12-T5 / Issue #411).

Written before the implementation (TDD red phase) as the executable
specification of §2.9 and §6.1 of docs/sprints/s12_t5_spec.md.

Expected public contract of ``bench_seg_latency``
-------------------------------------------------
Constants
    EXIT_OK = 0, EXIT_GATE_FAILED = 1, EXIT_REFUSED = 2
    GATE_P95_MS = 25.0
    TIMED_RUNS = 200, REPEATS = 3, WARMUP_RUNS = 10
    HOST_CPUINFO_PATH = Path("/proc/cpuinfo")

Exceptions
    HostRefusedError
    UntrainedHeadError

Functions
    probe_host(system=None, machine=None, cpuinfo_path=None) -> dict
        Requires Linux + x86_64 + "avx2" in /proc/cpuinfo; otherwise raises
        HostRefusedError naming the failed requirement. Returns
        {"system", "machine", "avx2", "cpu_model"}.
    percentile(values, p) -> float
        Nearest-rank over the ascending sort:
        ``index = max(0, ceil(p / 100 * n) - 1)``; empty input raises ValueError.
    summarize_latencies(values) -> {"p50_ms", "p95_ms", "p99_ms", "n",
                                    "min_ms", "max_ms"}
    summarize_repeats(repeats) -> {"repeats": [...], "p50_ms", "p95_ms",
                                   "p99_ms", "n", "worst_repeat_p95_ms"}
        ``p95_ms`` is the median of the per-repeat p95 values: that is the gate
        number. ``worst_repeat_p95_ms`` is disclosed alongside it.
    evaluate_latency_gate(p95_ms, threshold_ms=GATE_P95_MS) -> (bool, [str])
    require_trained_head(metadata) -> None
        Accepts the full metadata.yaml payload or its ``projectkv`` block.
        Raises UntrainedHeadError unless ``head_status == "trained"`` and
        ``reinitialised_tensors == 0``.
    build_payload(*, repeats, host, ir_sha256, head_status, generated_at=None) -> dict
        Returns the evidence document; every ``*_at`` field is a timezone-aware
        ISO-8601 timestamp that is never in the future. It refuses a
        non-trained ``head_status`` with UntrainedHeadError.
    main(argv=None) -> int
        Exits 2 on host refusal or untrained metadata and writes no evidence
        file. CLI: --out PATH --ir DIR --metadata PATH --frame PATH
        [--runs N] [--warmup N] [--repeats N].

Red phase behaviour
-------------------
Until the module exists the tests skip with a reason naming the missing API.
Run ``KV_TDD_STRICT=1 python3 -m unittest tools/train/bench_seg_latency_test.py``
to turn every missing-contract skip into a hard failure.
"""

from __future__ import annotations

import functools
import json
import os
import platform
import shutil
import sys
import tempfile
import unittest
from datetime import datetime, timedelta, timezone
from pathlib import Path
from unittest.mock import patch

TRAIN_DIR = Path(__file__).resolve().parent
REPO_ROOT = TRAIN_DIR.parents[1]
if str(TRAIN_DIR) not in sys.path:
    sys.path.insert(0, str(TRAIN_DIR))

STRICT_RED_PHASE = os.environ.get("KV_TDD_STRICT") == "1"
MODULE_PATH = TRAIN_DIR / "bench_seg_latency.py"
MODULE_NAME = "bench_seg_latency"

try:  # TDD red phase: the module is written by the Lead Coder after this suite.
    import bench_seg_latency as bench  # noqa: E402
except ImportError as exc:  # pragma: no cover - exercised on every red-phase run
    bench = None
    IMPORT_ERROR = f"{type(exc).__name__}: {exc}"
else:
    IMPORT_ERROR = None


def requires(*attrs):
    """Skip (or fail under KV_TDD_STRICT) when the implementation is absent."""

    def decorator(func):
        @functools.wraps(func)
        def wrapper(self, *args, **kwargs):
            if bench is None:
                missing = list(attrs)
                reason = (
                    f"TDD red phase: {MODULE_PATH.relative_to(REPO_ROOT)} is not implemented "
                    f"(missing contract: {', '.join(missing)}; import error: {IMPORT_ERROR})"
                )
            else:
                missing = [name for name in attrs if not hasattr(bench, name)]
                reason = f"TDD red phase: {MODULE_NAME} does not expose {', '.join(missing)}"
            if missing:
                if STRICT_RED_PHASE:
                    raise AssertionError(reason)
                raise unittest.SkipTest(reason)
            return func(self, *args, **kwargs)

        return wrapper

    return decorator


AVX2_CPUINFO = """
processor\t: 0
vendor_id\t: AuthenticAMD
cpu family\t: 25
model\t\t: 68
model name\t: AMD Ryzen 7 6800HS Creator Edition
flags\t\t: fpu vme de pse tsc msr pae mce cx8 apic sep sse sse2 ssse3 sse4_1 sse4_2
\t\t: avx avx2 fma bmi1 bmi2
""".strip()

NO_AVX2_CPUINFO = """
processor\t: 0
vendor_id\t: GenuineIntel
model name\t: Intel(R) Xeon(R) CPU E5-2670
flags\t\t: fpu vme de pse tsc msr pae mce cx8 apic sep sse sse2 ssse3 sse4_1 sse4_2
\t\t: avx fma
""".strip()


class _TmpCase(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = Path(tempfile.mkdtemp())
        self.addCleanup(shutil.rmtree, self.tmp, True)

    def cpuinfo(self, content: str) -> Path:
        path = self.tmp / "cpuinfo"
        path.write_text(content + "\n", encoding="utf-8")
        return path

    def jpeg(self, name: str = "frame.jpg") -> Path:
        import io

        from PIL import Image

        path = self.tmp / name
        buffer = io.BytesIO()
        Image.new("RGB", (640, 480), (32, 64, 96)).save(buffer, format="JPEG")
        path.write_bytes(buffer.getvalue())
        return path

    def metadata(self, head_status: str, reinitialised: int) -> Path:
        """Write metadata.yaml as JSON: JSON is valid YAML, so no PyYAML needed."""
        directory = self.tmp / "ir"
        directory.mkdir(parents=True, exist_ok=True)
        path = directory / "metadata.yaml"
        path.write_text(
            json.dumps(
                {
                    "projectkv": {
                        "class_count": 6,
                        "output_channels": 42,
                        "head_status": head_status,
                        "reinitialised_tensors": reinitialised,
                    }
                }
            ),
            encoding="utf-8",
        )
        (directory / "yolov8n_seg.xml").write_text("<net name='seg'/>", encoding="utf-8")
        (directory / "yolov8n_seg.bin").write_bytes(b"\x00" * 1024)
        return path


# ---------------------------------------------------------------------------
# 1. Host guard: Linux + x86_64 + avx2, otherwise exit 2 with no evidence
# ---------------------------------------------------------------------------


class TestHostGuard(_TmpCase):
    @requires("probe_host", "HostRefusedError")
    def test_darwin_arm64_is_refused(self):
        with self.assertRaises(bench.HostRefusedError) as ctx:
            bench.probe_host(system="Darwin", machine="arm64")
        message = str(ctx.exception).lower()
        self.assertIn("linux", message)
        self.assertIn("darwin", message)

    @requires("probe_host", "HostRefusedError")
    def test_linux_arm64_is_refused(self):
        with self.assertRaises(bench.HostRefusedError) as ctx:
            bench.probe_host(system="Linux", machine="aarch64",
                             cpuinfo_path=self.cpuinfo(AVX2_CPUINFO))
        self.assertIn("x86_64", str(ctx.exception))

    @requires("probe_host", "HostRefusedError")
    def test_linux_x86_64_without_avx2_is_refused(self):
        with self.assertRaises(bench.HostRefusedError) as ctx:
            bench.probe_host(system="Linux", machine="x86_64",
                             cpuinfo_path=self.cpuinfo(NO_AVX2_CPUINFO))
        self.assertIn("avx2", str(ctx.exception).lower())

    @requires("probe_host")
    def test_linux_x86_64_avx2_worker_is_accepted(self):
        host = bench.probe_host(system="Linux", machine="x86_64",
                                cpuinfo_path=self.cpuinfo(AVX2_CPUINFO))
        self.assertTrue(host["avx2"])
        self.assertEqual(host["system"], "Linux")
        self.assertEqual(host["machine"], "x86_64")
        self.assertIn("Ryzen 7 6800HS", host["cpu_model"])

    @requires("probe_host", "main")
    def test_cli_refusal_exits_2_and_writes_no_evidence(self):
        evidence = self.tmp / "evidence" / "s12_t5_latency_avx2.json"

        def refuse(**_kwargs):
            raise bench.HostRefusedError("Darwin arm64 is not gate-eligible; Linux x86_64 required")

        with patch.object(bench, "probe_host", side_effect=refuse):
            code = bench.main(
                [
                    "--out", str(evidence),
                    "--ir", str(self.tmp / "ir"),
                    "--frame", str(self.jpeg()),
                ]
            )

        self.assertEqual(code, 2)
        self.assertFalse(evidence.exists(), "a refused host must not write evidence")
        self.assertFalse(evidence.parent.exists(), "refusal must not create the evidence tree")

    @requires("probe_host", "main")
    def test_non_avx2_linux_host_refusal_exits_2_without_evidence(self):
        evidence = self.tmp / "evidence" / "latency.json"
        cpuinfo = self.cpuinfo(NO_AVX2_CPUINFO)
        real_probe = bench.probe_host

        def linux_without_avx2(**_kwargs):
            return real_probe(system="Linux", machine="x86_64", cpuinfo_path=cpuinfo)

        with patch.object(bench, "probe_host", side_effect=linux_without_avx2):
            code = bench.main(
                [
                    "--out", str(evidence),
                    "--ir", str(self.tmp / "ir"),
                    "--frame", str(self.jpeg()),
                ]
            )

        self.assertEqual(code, 2)
        self.assertFalse(evidence.exists())
        self.assertFalse(evidence.parent.exists())

    @unittest.skipUnless(
        platform.system() == "Darwin", "real-host refusal assertion only runs on macOS"
    )
    @requires("main")
    def test_real_darwin_host_is_refused_without_evidence(self):
        evidence = self.tmp / "s12_t5_latency_avx2.json"

        code = bench.main(
            [
                "--out", str(evidence),
                "--ir", str(self.tmp / "ir"),
                "--frame", str(self.jpeg()),
            ]
        )

        self.assertEqual(code, 2)
        self.assertFalse(evidence.exists())
        self.assertEqual(list(self.tmp.glob("*.json")), [])


# ---------------------------------------------------------------------------
# 2. Percentile mathematics
# ---------------------------------------------------------------------------


class TestPercentiles(_TmpCase):
    @requires("percentile")
    def test_nearest_rank_percentiles_on_a_known_series(self):
        values = list(range(1, 101))

        self.assertEqual(bench.percentile(values, 50), 50.0)
        self.assertEqual(bench.percentile(values, 95), 95.0)
        self.assertEqual(bench.percentile(values, 99), 99.0)
        self.assertEqual(bench.percentile(values, 100), 100.0)
        self.assertEqual(bench.percentile(values, 0), 1.0)

    @requires("percentile")
    def test_percentiles_on_unsorted_small_series(self):
        values = [5.0, 1.0, 3.0, 2.0, 4.0]  # sorted: 1 2 3 4 5

        self.assertEqual(bench.percentile(values, 50), 3.0)
        self.assertEqual(bench.percentile(values, 95), 5.0)
        self.assertEqual(bench.percentile(values, 99), 5.0)
        self.assertEqual(bench.percentile([10.0], 95), 10.0)

    @requires("percentile")
    def test_empty_series_is_rejected(self):
        with self.assertRaises(ValueError):
            bench.percentile([], 95)

    @requires("summarize_latencies")
    def test_summary_reports_p50_p95_p99_and_count(self):
        summary = bench.summarize_latencies(list(range(1, 101)))

        self.assertEqual(summary["n"], 100)
        self.assertEqual(summary["p50_ms"], 50.0)
        self.assertEqual(summary["p95_ms"], 95.0)
        self.assertEqual(summary["p99_ms"], 99.0)
        self.assertEqual(summary["min_ms"], 1.0)
        self.assertEqual(summary["max_ms"], 100.0)

    @requires("summarize_repeats")
    def test_gate_number_is_the_median_of_repeat_p95_values(self):
        repeats = [
            [float(i) for i in range(1, 101)],        # p95 = 95
            [float(i) for i in range(2, 102)],        # p95 = 96
            [float(i) for i in range(3, 103)],        # p95 = 97
        ]

        summary = bench.summarize_repeats(repeats)

        self.assertEqual(summary["n"], 300)
        self.assertEqual(summary["p95_ms"], 96.0)
        self.assertEqual(summary["worst_repeat_p95_ms"], 97.0)
        self.assertEqual(len(summary["repeats"]), 3)


# ---------------------------------------------------------------------------
# 3. The 25.0 ms gate boundary
# ---------------------------------------------------------------------------


class TestLatencyGate(_TmpCase):
    @requires("evaluate_latency_gate")
    def test_exactly_twenty_five_milliseconds_passes(self):
        passed, failures = bench.evaluate_latency_gate(25.0)
        self.assertTrue(passed)
        self.assertEqual(failures, [])

    @requires("evaluate_latency_gate")
    def test_twenty_five_point_zero_one_milliseconds_fails(self):
        passed, failures = bench.evaluate_latency_gate(25.01)
        self.assertFalse(passed)
        self.assertEqual(len(failures), 1)
        self.assertIn("25.01", failures[0])
        self.assertIn("25.0", failures[0])

    @requires("evaluate_latency_gate")
    def test_just_under_the_ceiling_passes(self):
        passed, failures = bench.evaluate_latency_gate(24.999)
        self.assertTrue(passed, failures)


# ---------------------------------------------------------------------------
# 4. Untrained metadata refusal
# ---------------------------------------------------------------------------


class TestTrainedHeadRefusal(_TmpCase):
    @requires("require_trained_head", "UntrainedHeadError")
    def test_reinitialised_head_is_refused(self):
        metadata = {
            "projectkv": {
                "head_status": "reinitialised_untrained",
                "reinitialised_tensors": 36,
            }
        }
        with self.assertRaises(bench.UntrainedHeadError) as ctx:
            bench.require_trained_head(metadata)
        self.assertIn("reinitialised_untrained", str(ctx.exception))

    @requires("require_trained_head", "UntrainedHeadError")
    def test_flat_trained_metadata_is_accepted(self):
        bench.require_trained_head({"head_status": "trained", "reinitialised_tensors": 0})

    @requires("require_trained_head", "UntrainedHeadError")
    def test_trained_label_with_non_zero_reinitialised_tensors_is_contradictory(self):
        with self.assertRaises(bench.UntrainedHeadError):
            bench.require_trained_head({"head_status": "trained", "reinitialised_tensors": 3})

    @requires("main", "probe_host", "UntrainedHeadError")
    def test_cli_refuses_untrained_metadata_before_touching_the_graph(self):
        metadata_path = self.metadata("reinitialised_untrained", 36)
        evidence = self.tmp / "latency.json"
        fake_host = {
            "system": "Linux",
            "machine": "x86_64",
            "avx2": True,
            "cpu_model": "AMD Ryzen 7 6800HS Creator Edition",
        }

        with patch.object(bench, "probe_host", return_value=fake_host):
            code = bench.main(
                [
                    "--out", str(evidence),
                    "--ir", str(metadata_path.parent),
                    "--frame", str(self.jpeg()),
                ]
            )

        self.assertEqual(code, 2)
        self.assertFalse(evidence.exists())


# ---------------------------------------------------------------------------
# 5. Evidence payload: schema, gate verdict, non-future timestamps
# ---------------------------------------------------------------------------


def _walk_timestamps(payload, prefix: str = "") -> list[tuple[str, str]]:
    found: list[tuple[str, str]] = []
    if isinstance(payload, dict):
        for key, value in payload.items():
            path = f"{prefix}.{key}" if prefix else str(key)
            if isinstance(value, str) and (key.endswith("_at") or key.endswith("_timestamp")):
                found.append((path, value))
            else:
                found.extend(_walk_timestamps(value, path))
    elif isinstance(payload, list):
        for index, value in enumerate(payload):
            found.extend(_walk_timestamps(value, f"{prefix}[{index}]"))
    return found


class TestEvidencePayload(_TmpCase):
    def _host(self) -> dict:
        return {
            "system": "Linux",
            "machine": "x86_64",
            "avx2": True,
            "cpu_model": "AMD Ryzen 7 6800HS Creator Edition",
        }

    @requires("build_payload")
    def test_payload_reports_protocol_gate_and_repeats(self):
        repeats = [
            [float(i) for i in range(1, 101)],
            [float(i) for i in range(2, 102)],
            [float(i) for i in range(3, 103)],
        ]
        payload = bench.build_payload(
            repeats=repeats,
            host=self._host(),
            ir_sha256="f" * 64,
            head_status="trained",
            generated_at="2026-11-12T10:00:00+07:00",
        )

        self.assertEqual(payload["head_status"], "trained")
        self.assertEqual(payload["ir_sha256"], "f" * 64)
        self.assertEqual(payload["latency"]["p95_ms"], 96.0)
        self.assertEqual(payload["latency"]["n"], 300)
        self.assertGreaterEqual(payload["latency"]["worst_repeat_p95_ms"],
                                payload["latency"]["p95_ms"])
        self.assertEqual(len(payload["repeats"]), 3)
        self.assertEqual(payload["gate"]["threshold_ms"], 25.0)
        self.assertEqual(payload["gate"]["verdict"], "FAILED")
        self.assertTrue(payload["gate"]["failures"])
        self.assertEqual(payload["host"]["machine"], "x86_64")

    @requires("build_payload")
    def test_a_passing_run_is_verdict_passed(self):
        repeats = [[10.0] * 200, [11.0] * 200, [12.0] * 200]
        payload = bench.build_payload(
            repeats=repeats,
            host=self._host(),
            ir_sha256="a" * 64,
            head_status="trained",
            generated_at="2026-11-12T10:00:00+07:00",
        )
        self.assertEqual(payload["gate"]["verdict"], "PASSED")
        self.assertEqual(payload["gate"]["failures"], [])

    @requires("build_payload")
    def test_no_timestamp_in_the_payload_is_in_the_future(self):
        repeats = [[10.0] * 200, [11.0] * 200, [12.0] * 200]
        payload = bench.build_payload(
            repeats=repeats,
            host=self._host(),
            ir_sha256="b" * 64,
            head_status="trained",
            generated_at="2026-11-12T10:00:00+07:00",
        )

        now = datetime.now(timezone.utc)
        stamps = _walk_timestamps(payload)
        self.assertTrue(stamps, "payload must carry a generated_at timestamp")
        for path, value in stamps:
            parsed = datetime.fromisoformat(value.replace("Z", "+00:00"))
            self.assertIsNotNone(parsed.tzinfo, f"{path} must be timezone aware")
            self.assertLessEqual(parsed, now + timedelta(seconds=5), f"{path} is future dated")

    @requires("build_payload")
    def test_default_generated_at_is_now_and_not_future(self):
        payload = bench.build_payload(
            repeats=[[10.0] * 200],
            host=self._host(),
            ir_sha256="c" * 64,
            head_status="trained",
        )
        generated = datetime.fromisoformat(payload["generated_at"].replace("Z", "+00:00"))
        now = datetime.now(timezone.utc)
        self.assertLessEqual(generated, now + timedelta(seconds=5))
        self.assertLess(abs((now - generated).total_seconds()), 300)

    @requires("build_payload")
    def test_untrained_head_cannot_be_recorded_as_evidence(self):
        with self.assertRaises(bench.UntrainedHeadError):
            bench.build_payload(
                repeats=[[10.0] * 200],
                host=self._host(),
                ir_sha256="d" * 64,
                head_status="reinitialised_untrained",
                generated_at="2026-11-12T10:00:00+07:00",
            )


if __name__ == "__main__":
    unittest.main(verbosity=2)
