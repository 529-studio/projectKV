#!/usr/bin/env python3
"""Contract tests for the AVX2 Docker soak (S13-T1 / Issue #438).

The gate itself can only run on the x86-64 AVX2 worker, but everything that
decides *whether* it may run is testable anywhere:

* the image must stay pinned, non-root and limited to the benchmark runtime;
* the runner must fix the protocol at 3 x 200 runs and refuse non-x86-64
  container runtimes, missing runtimes and missing inputs;
* the evidence checker must reject a short run, an untrained head, a
  contradictory verdict and a future timestamp;
* the host probe must reject QEMU/TCG CPU models that can advertise AVX2
  without being the worker.

A fake container runtime drives the runner end to end on the developer host:
it answers the architecture query, the host probe and the evidence checker
exactly as the pinned image would, so argument construction and exit-code
mapping are verified without an x86 machine or a Docker daemon.
"""

from __future__ import annotations

import json
import os
import re
import shutil
import stat
import subprocess
import sys
import tempfile
import time
import unittest
from datetime import datetime, timedelta, timezone
from pathlib import Path
from unittest import mock

TRAIN_DIR = Path(__file__).resolve().parent
REPO_ROOT = TRAIN_DIR.parents[1]
DOCKERFILE = REPO_ROOT / "deploy" / "benchmark" / "Dockerfile.avx2"
RUNNER = TRAIN_DIR / "bench_avx2_docker.sh"

if str(TRAIN_DIR) not in sys.path:
    sys.path.insert(0, str(TRAIN_DIR))

import bench_avx2_evidence_check as evidence_check  # noqa: E402
import bench_avx2_host_probe as host_probe  # noqa: E402
import bench_seg_latency as bench  # noqa: E402


FAKE_RUNTIME = '''#!/usr/bin/env python3
"""Minimal stand-in for docker/podman used by bench_avx2_docker_test.py."""
from __future__ import annotations

import json
import os
import subprocess
import sys
import time
from datetime import datetime, timedelta, timezone
from pathlib import Path

REPO = Path(os.environ["FAKE_REPO"])
ARCH = os.environ.get("FAKE_ARCH", "x86_64")
PROBE = "/opt/bench/bench_avx2_host_probe.py"
CHECKER = "/opt/bench/bench_avx2_evidence_check.py"

args = sys.argv[1:]


def option(name: str) -> str:
    if name in args:
        return args[args.index(name) + 1]
    raise SystemExit(f"fake runtime: missing {name}")


if not args:
    raise SystemExit(0)

if args[0] == "info":
    print(ARCH)
    raise SystemExit(0)

if args[0] == "image":
    raise SystemExit(0)

if args[0] == "build":
    print("fake runtime: build skipped", file=sys.stderr)
    raise SystemExit(0)

if args[0] != "run":
    raise SystemExit(0)

if PROBE in args:
    print(json.dumps({
        "epoch": int(time.time()),
        "host": {
            "system": "Linux",
            "machine": "x86_64",
            "avx2": True,
            "cpu_model": os.environ.get("FAKE_CPU_MODEL", "AMD Ryzen 7 6800HS Creator Edition"),
        },
    }))
    raise SystemExit(0)

if CHECKER in args:
    raise SystemExit(subprocess.call([
        sys.executable, str(REPO / "tools/train/bench_avx2_evidence_check.py"),
        "--file", option("--file"),
    ]))

out = option("--out")
sys.path.insert(0, str(REPO / "tools/train"))
import bench_seg_latency as bench

p95 = float(os.environ.get("FAKE_P95", "10.0"))
payload = bench.build_payload(
    repeats=[[p95] * 200 for _ in range(3)],
    host={
        "system": "Linux",
        "machine": "x86_64",
        "avx2": True,
        "cpu_model": "AMD Ryzen 7 6800HS Creator Edition",
    },
    ir_sha256="f" * 64,
    head_status="trained",
    protocol={
        "warmup_runs": int(option("--warmup")),
        "timed_runs": int(option("--runs")),
        "repeats": int(option("--repeats")),
        "imgsz": 416,
        "gate_resolution": 416,
        "secondary_resolution": 320,
        "compile_device": "CPU",
        "input": "real proof frame, letterboxed",
    },
)
if os.environ.get("FAKE_FUTURE") == "1":
    payload["generated_at"] = (
        datetime.now(timezone.utc) + timedelta(hours=1)
    ).isoformat(timespec="seconds")
Path(out).parent.mkdir(parents=True, exist_ok=True)
Path(out).write_text(json.dumps(payload, indent=2) + "\\n", encoding="utf-8")
raise SystemExit(0 if payload["gate"]["verdict"] == "PASSED" else 1)
'''


class _FakeRuntimeCase(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = Path(tempfile.mkdtemp())
        self.addCleanup(shutil.rmtree, self.tmp, True)
        self.runtime = self.tmp / "fake-docker"
        self.runtime.write_text(FAKE_RUNTIME, encoding="utf-8")
        self.runtime.chmod(self.runtime.stat().st_mode | stat.S_IXUSR)

    def environment(self, **overrides: str) -> dict:
        env = dict(os.environ)
        env.pop("BENCH_RUNTIME", None)
        env["FAKE_REPO"] = str(REPO_ROOT)
        env.update(overrides)
        return env

    def run_runner(self, *arguments: str, env=None, timeout: int = 60):
        return subprocess.run(
            [str(RUNNER), *arguments],
            capture_output=True,
            text=True,
            timeout=timeout,
            env=env or self.environment(),
        )

    def ir(self, head_status: str = "trained", reinitialised: int = 0) -> Path:
        directory = self.tmp / "ir"
        directory.mkdir(parents=True, exist_ok=True)
        (directory / "yolov8n_seg.xml").write_text("<net name='seg'/>", encoding="utf-8")
        (directory / "yolov8n_seg.bin").write_bytes(b"\x00" * 4096)
        (directory / "metadata.yaml").write_text(
            json.dumps(
                {
                    "projectkv": {
                        "head_status": head_status,
                        "reinitialised_tensors": reinitialised,
                    }
                }
            ),
            encoding="utf-8",
        )
        return directory

    def frame(self) -> Path:
        path = self.tmp / "frame.jpg"
        path.write_bytes(b"\xff\xd8\xff" + b"\x00" * 64)
        return path


# ---------------------------------------------------------------------------
# 1. Image contract
# ---------------------------------------------------------------------------


class TestDockerfile(unittest.TestCase):
    @classmethod
    def setUpClass(cls) -> None:
        cls.text = DOCKERFILE.read_text(encoding="utf-8")

    def test_exists_with_pinned_base_and_no_latest(self):
        self.assertTrue(DOCKERFILE.is_file())
        froms = re.findall(r"^FROM\s+(\S+)", self.text, re.MULTILINE)
        self.assertTrue(froms, "Dockerfile has no FROM line")
        for image in froms:
            self.assertNotIn(":latest", image, f"unpinned base image: {image}")
            self.assertTrue(
                re.search(r":\d+\.\d+\.\d+", image),
                f"base image is not pinned to a patch version: {image}",
            )

    def test_installs_the_benchmark_runtime(self):
        for package, variable in (
            ("openvino", "OPENVINO_VERSION"),
            ("numpy", "NUMPY_VERSION"),
            ("pillow", "PILLOW_VERSION"),
            ("PyYAML", "PYYAML_VERSION"),
        ):
            self.assertIn(f'"{package}==${{{variable}}}"', self.text)
        self.assertIn("libgomp1", self.text, "OpenVINO needs its OpenMP runtime")
        self.assertIn("util-linux", self.text, "lscpu is required by the evidence fingerprint")

    def test_copies_harness_and_both_guards(self):
        for module in (
            "bench_seg_latency.py",
            "bench_avx2_host_probe.py",
            "bench_avx2_evidence_check.py",
        ):
            self.assertIn(module, self.text)

    def test_runs_as_non_root(self):
        self.assertRegex(self.text, r"(?m)^USER\s+10001:10001\s*$")

    def test_entrypoint_is_the_harness(self):
        self.assertIn('ENTRYPOINT ["python3", "/opt/bench/bench_seg_latency.py"]', self.text)

    def test_carries_no_credentials(self):
        for pattern in ("AKIA", "-----BEGIN", "password=", "secret=", "token="):
            self.assertNotIn(pattern, self.text)


# ---------------------------------------------------------------------------
# 2. Runner contract
# ---------------------------------------------------------------------------


class TestRunnerStatic(unittest.TestCase):
    @classmethod
    def setUpClass(cls) -> None:
        cls.text = RUNNER.read_text(encoding="utf-8")

    def test_is_executable_and_parses(self):
        self.assertTrue(RUNNER.stat().st_mode & stat.S_IXUSR, "runner must be executable")
        completed = subprocess.run(["bash", "-n", str(RUNNER)], capture_output=True, text=True)
        self.assertEqual(completed.returncode, 0, completed.stderr)

    def test_fails_fast_on_errors(self):
        self.assertIn("set -Eeuo pipefail", self.text)

    def test_protocol_is_not_configurable(self):
        self.assertRegex(self.text, r"(?m)^readonly RUNS=200$")
        self.assertRegex(self.text, r"(?m)^readonly WARMUP=10$")
        self.assertRegex(self.text, r"(?m)^readonly REPEATS=3$")
        usage_text = self.text.split("usage() {", 1)[1].split("EOF", 1)[0]
        for flag in ("--runs", "--warmup", "--repeats"):
            self.assertNotIn(
                flag, usage_text, f"{flag} must not be an operator-tunable knob"
            )

    def test_refuses_non_x86_and_emulated_runtimes(self):
        self.assertIn("require_x86_runtime", self.text)
        self.assertIn("not x86-64", self.text)
        self.assertIn("bench_avx2_host_probe.py", self.text)

    def test_enforces_clock_agreement_and_non_future_evidence(self):
        self.assertIn("CLOCK_SKEW_SECONDS=300", self.text)
        self.assertIn("evidence timestamps would be untrustworthy", self.text)
        self.assertIn("bench_avx2_evidence_check.py", self.text)

    def test_hardens_the_container(self):
        self.assertIn("--cap-drop ALL", self.text)
        self.assertIn("--security-opt no-new-privileges", self.text)

    def test_has_no_bypass_hatch(self):
        for forbidden in ("--allow-unsafe-host", "--allow-non-avx2", "--skip-guard"):
            self.assertNotIn(forbidden, self.text)

    def test_no_credentials(self):
        for pattern in ("AKIA", "-----BEGIN", "password=", "secret="):
            self.assertNotIn(pattern, self.text)


# ---------------------------------------------------------------------------
# 3. Evidence acceptance rule
# ---------------------------------------------------------------------------


def _valid_payload(p95: float = 10.0, repeat_count: int = 3, run_count: int = 200) -> dict:
    return bench.build_payload(
        repeats=[[p95] * run_count for _ in range(repeat_count)],
        host={
            "system": "Linux",
            "machine": "x86_64",
            "avx2": True,
            "cpu_model": "AMD Ryzen 7 6800HS Creator Edition",
        },
        ir_sha256="f" * 64,
        head_status="trained",
    )


class TestEvidenceCheck(unittest.TestCase):
    def test_valid_payload_is_accepted(self):
        self.assertEqual(evidence_check.validate(_valid_payload()), [])

    def test_gate_failure_is_still_valid_evidence(self):
        payload = _valid_payload(p95=30.0)
        self.assertEqual(payload["gate"]["verdict"], "FAILED")
        self.assertEqual(evidence_check.validate(payload), [])

    def test_short_protocol_is_rejected(self):
        payload = _valid_payload()
        payload["protocol"]["timed_runs"] = 50
        failures = evidence_check.validate(payload)
        self.assertTrue(any("timed_runs" in failure for failure in failures), failures)

    def test_two_repeats_are_rejected(self):
        payload = _valid_payload(repeat_count=2)
        failures = evidence_check.validate(payload)
        self.assertTrue(any("repeats" in failure for failure in failures), failures)

    def test_five_hundred_measurements_are_rejected(self):
        payload = _valid_payload(run_count=250)
        failures = evidence_check.validate(payload)
        self.assertTrue(any("latency.n" in failure for failure in failures), failures)

    def test_untrained_head_is_rejected(self):
        payload = _valid_payload()
        payload["head_status"] = "reinitialised_untrained"
        self.assertTrue(evidence_check.validate(payload))

    def test_contradictory_verdict_is_rejected(self):
        payload = _valid_payload(p95=30.0)
        payload["gate"]["verdict"] = "PASSED"
        failures = evidence_check.validate(payload)
        self.assertTrue(any("contradicts" in failure for failure in failures), failures)

    def test_future_timestamp_is_rejected(self):
        payload = _valid_payload()
        timestamp = (datetime.now(timezone.utc) + timedelta(hours=1)).isoformat()
        payload["generated_at"] = timestamp
        failures = evidence_check.validate(payload)
        self.assertTrue(any("future" in failure for failure in failures), failures)

    def test_naive_timestamp_is_rejected(self):
        payload = _valid_payload()
        payload["generated_at"] = "2026-11-12T10:00:00"
        failures = evidence_check.validate(payload)
        self.assertTrue(any("timezone-aware" in failure for failure in failures), failures)

    def test_synthetic_source_claim_is_rejected(self):
        payload = _valid_payload()
        payload["dataset_source"] = "synthetic"
        failures = evidence_check.validate(payload)
        self.assertTrue(any("synthetic" in failure for failure in failures), failures)

    def test_cli_rejects_unreadable_file(self):
        self.assertEqual(evidence_check.main(["--file", "/nonexistent/evidence.json"]), 1)


# ---------------------------------------------------------------------------
# 4. Host probe contract
# ---------------------------------------------------------------------------


class TestHostProbe(unittest.TestCase):
    def test_ryzen_worker_is_accepted(self):
        host_probe.assert_native_x86("AMD Ryzen 7 6800HS Creator Edition")
        host_probe.assert_native_x86("Intel(R) Xeon(R) Platinum 8375C CPU @ 2.90GHz")

    def test_qemu_virtual_cpu_is_refused(self):
        with self.assertRaises(bench.HostRefusedError) as ctx:
            host_probe.assert_native_x86("QEMU Virtual CPU version 2.5+")
        self.assertIn("emulat", str(ctx.exception).lower())

    def test_bochs_is_refused(self):
        with self.assertRaises(bench.HostRefusedError):
            host_probe.assert_native_x86("Bochs")

    def test_unknown_model_is_refused(self):
        with self.assertRaises(bench.HostRefusedError):
            host_probe.assert_native_x86("unknown")

    def test_main_returns_refusal_json(self):
        with mock.patch.object(
            host_probe.bench,
            "probe_host",
            side_effect=bench.HostRefusedError("host refusal: Darwin arm64"),
        ):
            self.assertEqual(host_probe.main([]), 2)

    def test_probe_reports_epoch_and_host(self):
        fake_host = {
            "system": "Linux",
            "machine": "x86_64",
            "avx2": True,
            "cpu_model": "AMD Ryzen 7 6800HS Creator Edition",
        }
        with mock.patch.object(host_probe.bench, "probe_host", return_value=fake_host):
            payload = host_probe.probe()
        self.assertTrue(payload["host"]["avx2"])
        self.assertLessEqual(abs(payload["epoch"] - int(time.time())), 5)


# ---------------------------------------------------------------------------
# 5. End-to-end orchestration with a fake container runtime
# ---------------------------------------------------------------------------


class TestRunnerOrchestration(_FakeRuntimeCase):
    def test_check_mode_accepts_x86_runtime(self):
        completed = self.run_runner("--check", "--runtime", str(self.runtime))
        self.assertEqual(completed.returncode, 0, completed.stderr)
        self.assertIn("gate-eligible", completed.stderr)

    def test_non_x86_runtime_is_refused_without_evidence(self):
        evidence = self.tmp / "evidence.json"
        completed = self.run_runner(
            "--check",
            "--runtime",
            str(self.runtime),
            env=self.environment(FAKE_ARCH="aarch64"),
        )
        self.assertEqual(completed.returncode, 2, completed.stderr)
        self.assertIn("not x86-64", completed.stderr)
        self.assertIn("not gate-eligible", completed.stderr)
        self.assertFalse(evidence.exists())

    def test_missing_runtime_is_refused(self):
        completed = self.run_runner("--check", "--runtime", str(self.tmp / "absent-docker"))
        self.assertEqual(completed.returncode, 2, completed.stderr)
        self.assertIn("not installed", completed.stderr)

    def test_missing_ir_is_refused_before_measuring(self):
        evidence = self.tmp / "out" / "evidence.json"
        completed = self.run_runner(
            "--ir", str(self.tmp / "absent-ir"),
            "--frame", str(self.frame()),
            "--out", str(evidence),
            "--runtime", str(self.runtime),
        )
        self.assertEqual(completed.returncode, 2, completed.stderr)
        self.assertIn("not a directory", completed.stderr)
        self.assertFalse(evidence.exists())

    def test_passing_run_writes_validated_evidence(self):
        evidence = self.tmp / "out" / "evidence.json"
        completed = self.run_runner(
            "--ir", str(self.ir()),
            "--frame", str(self.frame()),
            "--out", str(evidence),
            "--runtime", str(self.runtime),
        )
        self.assertEqual(completed.returncode, 0, completed.stderr)
        self.assertTrue(evidence.is_file())
        payload = json.loads(evidence.read_text(encoding="utf-8"))
        self.assertEqual(payload["latency"]["n"], 600)
        self.assertEqual(payload["protocol"]["timed_runs"], 200)
        self.assertEqual(payload["protocol"]["repeats"], 3)
        self.assertEqual(payload["gate"]["verdict"], "PASSED")
        self.assertIn("EVIDENCE ACCEPTED", completed.stdout)

    def test_gate_failure_keeps_evidence_and_exits_one(self):
        evidence = self.tmp / "out" / "evidence.json"
        completed = self.run_runner(
            "--ir", str(self.ir()),
            "--frame", str(self.frame()),
            "--out", str(evidence),
            "--runtime", str(self.runtime),
            env=self.environment(FAKE_P95="30.0"),
        )
        self.assertEqual(completed.returncode, 1, completed.stderr)
        self.assertTrue(evidence.is_file(), "a failed gate still produces evidence")
        payload = json.loads(evidence.read_text(encoding="utf-8"))
        self.assertEqual(payload["gate"]["verdict"], "FAILED")

    def test_future_dated_evidence_is_rejected(self):
        evidence = self.tmp / "out" / "evidence.json"
        completed = self.run_runner(
            "--ir", str(self.ir()),
            "--frame", str(self.frame()),
            "--out", str(evidence),
            "--runtime", str(self.runtime),
            env=self.environment(FAKE_FUTURE="1"),
        )
        self.assertEqual(completed.returncode, 1, completed.stderr)
        self.assertIn("in the future", completed.stderr)


if __name__ == "__main__":
    unittest.main(verbosity=2)
