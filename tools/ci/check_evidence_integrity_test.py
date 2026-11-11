#!/usr/bin/env python3
"""
Unit tests for tools/ci/check_evidence_integrity.py.

Tests all four integrity gates:
  Gate 1: Synthetic Data Prohibition & Prohibited Fixture References
  Gate 2: Temporal Invariants, Window Verification & Anti-Spoofing
  Gate 3: Model Weight Integrity, Entropy & Stub Rejection
  Gate 4: Empirical Proof Artifact Integrity & Magic Byte Validation
  Plus Baseline Ratchet Engine, Clock-Dependent Baseline Rejection,
  Superseded Artifact Handling, and CLI Arguments.
"""

from __future__ import annotations

import json
import math
import os
import struct
import sys
import tempfile
import unittest
from datetime import datetime, timedelta, timezone
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parents[2]
if str(REPO_ROOT) not in sys.path:
    sys.path.insert(0, str(REPO_ROOT))

from tools.ci.check_evidence_integrity import (
    BASELINE_FILENAME,
    Violation,
    check_artifact,
    check_gate1_synthetic,
    check_gate2_temporal,
    check_gate3_model,
    check_gate4_proof,
    evaluate_gate,
    find_tracked_evidence_files,
    is_clock_dependent_violation,
    load_baseline,
    parse_duration_to_seconds,
    parse_iso_timestamp,
    read_image_dimensions,
    reset_root,
    root,
    save_baseline,
    set_root,
    shannon_entropy,
    validate_baseline_entry,
)


def create_mock_png(width: int, height: int) -> bytes:
    """Construct minimal valid PNG bytes with IHDR chunk."""
    header = b"\x89PNG\r\n\x1a\n"
    # IHDR length = 13 (0x0D), type = b'IHDR'
    ihdr_data = struct.pack(">IIBBBBB", width, height, 8, 2, 0, 0, 0)
    ihdr_chunk = struct.pack(">I", len(ihdr_data)) + b"IHDR" + ihdr_data + b"\x00\x00\x00\x00"
    return header + ihdr_chunk + b"\x00\x00\x00\x00IEND\xaeB`\x82"


def create_mock_jpeg(width: int, height: int) -> bytes:
    """Construct minimal valid JPEG bytes with SOF0 marker."""
    header = b"\xff\xd8\xff\xe0\x00\x10JFIF\x00\x01\x01\x00\x00\x01\x00\x01\x00\x00"
    # SOF0 marker (0xFF, 0xC0), length = 17, precision = 8
    sof0_len = 17
    sof0 = struct.pack(">BBHBHHI", 0xFF, 0xC0, sof0_len, 8, height, width, 0x03011100)
    eoi = b"\xff\xd9"
    return header + sof0 + eoi


class TestUtilityHelpers(unittest.TestCase):
    """Test mathematical and parsing utility functions."""

    def test_shannon_entropy(self):
        # Empty
        self.assertEqual(shannon_entropy(b""), 0.0)
        # Single repeated byte -> entropy = 0.0
        self.assertEqual(shannon_entropy(b"\x00" * 1000), 0.0)
        # Two alternating bytes with equal frequency -> entropy = 1.0
        two_bytes = (b"\x00\x01") * 500
        self.assertAlmostEqual(shannon_entropy(two_bytes), 1.0, places=4)
        # Four alternating bytes with equal frequency -> entropy = 2.0
        four_bytes = (b"ABCD") * 250
        self.assertAlmostEqual(shannon_entropy(four_bytes), 2.0, places=4)
        # High entropy random data (close to 8.0)
        high_entropy = bytes(range(256)) * 4
        self.assertAlmostEqual(shannon_entropy(high_entropy), 8.0, places=4)

    def test_parse_iso_timestamp(self):
        # Standard Z
        dt = parse_iso_timestamp("2026-11-06T10:23:18.434225Z")
        self.assertIsNotNone(dt)
        self.assertEqual(dt.tzinfo, timezone.utc)
        self.assertEqual(dt.year, 2026)
        self.assertEqual(dt.month, 11)
        self.assertEqual(dt.day, 6)

        # 9-digit RFC3339Nano
        dt_nano = parse_iso_timestamp("2026-11-06T10:23:18.434225000Z")
        self.assertIsNotNone(dt_nano)
        self.assertEqual(dt_nano.tzinfo, timezone.utc)
        self.assertEqual(dt_nano.microsecond, 434225)

        # Offset +07:00
        dt7 = parse_iso_timestamp("2026-11-07T06:00:00+07:00")
        self.assertIsNotNone(dt7)
        self.assertEqual(dt7.tzinfo, timezone.utc)
        # 06:00 +07:00 is 23:00 Nov 6 UTC
        self.assertEqual(dt7.day, 6)
        self.assertEqual(dt7.hour, 23)

        # Space separated fallback
        dt_space = parse_iso_timestamp("2026-10-09 04:11:30")
        self.assertIsNotNone(dt_space)
        self.assertEqual(dt_space.year, 2026)

        # Invalid
        self.assertIsNone(parse_iso_timestamp("not-a-timestamp"))
        self.assertIsNone(parse_iso_timestamp(""))
        self.assertIsNone(parse_iso_timestamp(12345))

    def test_parse_duration_to_seconds(self):
        # Numeric hours
        self.assertEqual(parse_duration_to_seconds(72), 72.0)
        # Hours string
        self.assertEqual(parse_duration_to_seconds("72h"), 72 * 3600.0)
        self.assertEqual(parse_duration_to_seconds("48 hours"), 48 * 3600.0)
        # Days string
        self.assertEqual(parse_duration_to_seconds("14d"), 14 * 86400.0)
        self.assertEqual(parse_duration_to_seconds("2 days"), 2 * 86400.0)
        # Minutes string
        self.assertEqual(parse_duration_to_seconds("30m"), 30 * 60.0)
        self.assertEqual(parse_duration_to_seconds("15 minutes"), 15 * 60.0)
        # Seconds string
        self.assertEqual(parse_duration_to_seconds("3600s"), 3600.0)
        # Invalid
        self.assertIsNone(parse_duration_to_seconds("invalid"))
        self.assertIsNone(parse_duration_to_seconds(None))

    def test_read_image_dimensions(self):
        # PNG
        png_bytes = create_mock_png(640, 480)
        fmt, w, h = read_image_dimensions(png_bytes)
        self.assertEqual(fmt, "PNG")
        self.assertEqual(w, 640)
        self.assertEqual(h, 480)

        # JPEG
        jpeg_bytes = create_mock_jpeg(1280, 720)
        fmt, w, h = read_image_dimensions(jpeg_bytes)
        self.assertEqual(fmt, "JPEG")
        self.assertEqual(w, 1280)
        self.assertEqual(h, 720)

        # Corrupt / non-image
        fmt, w, h = read_image_dimensions(b"plain text data")
        self.assertIsNone(fmt)
        self.assertEqual(w, 0)
        self.assertEqual(h, 0)


class TestGate1SyntheticData(unittest.TestCase):
    """Test Gate 1: Synthetic Data Prohibition & Prohibited Fixture References."""

    def test_detect_prohibited_fixture_reference(self):
        v = check_gate1_synthetic(
            "docs/reports/test.json",
            {},
            '{"input": "tools/forecast/fixtures/synthetic_congestion_v2_5m.csv"}',
        )
        self.assertEqual(len(v), 1)
        self.assertIn("synthetic_congestion_v2_5m.csv", v[0].detail)

        v2 = check_gate1_synthetic(
            "docs/reports/test2.json",
            {},
            '{"input": "tools/forecast/fixtures/synthetic_congestion_5m.csv"}',
        )
        self.assertEqual(len(v2), 1)
        self.assertIn("synthetic_congestion_5m.csv", v2[0].detail)

    def test_reject_synthetic_field_values(self):
        cases = [
            {"source": "synthetic"},
            {"fit_source": "synthetic"},
            {"dataset_source": "synthetic"},
            {"data_source": "synthetic_fixture"},
            {"provenance": "synthetic"},
        ]
        for data in cases:
            v = check_gate1_synthetic("docs/reports/test.json", data, json.dumps(data))
            self.assertTrue(any("synthetic" in vi.detail.lower() for vi in v))

    def test_forecast_fit_source_requirement(self):
        # Forecast release artifact with fit_source != live_history
        bad_forecast = {
            "forecast_model": "ridge_v2",
            "fit_source": "offline_sim",
        }
        v = check_gate1_synthetic("docs/reports/forecast_run.json", bad_forecast, json.dumps(bad_forecast))
        self.assertTrue(any("live_history" in vi.detail for vi in v))

        # Valid forecast with live_history
        good_forecast = {
            "forecast_model": "ridge_v2",
            "fit_source": "live_history",
        }
        v_good = check_gate1_synthetic("docs/reports/forecast_run.json", good_forecast, json.dumps(good_forecast))
        self.assertEqual(len(v_good), 0)


class TestGate2TemporalInvariants(unittest.TestCase):
    """Test Gate 2: Temporal Invariants, Window Validation & Anti-Spoofing."""

    def setUp(self):
        self.ref_now = datetime(2026, 11, 10, 12, 0, 0, tzinfo=timezone.utc)

    def test_valid_soak_window(self):
        data = {
            "window": {
                "start": "2026-11-07T10:00:00Z",
                "end": "2026-11-10T10:00:00Z",
                "duration": "72h",
            },
            "generated_at": "2026-11-10T10:15:00Z",
        }
        v = check_gate2_temporal("docs/reports/soak.json", data, now=self.ref_now)
        self.assertEqual(len(v), 0)

    def test_reject_generated_at_before_window_end(self):
        # Spoofed report generated before soak window finished
        data = {
            "window": {
                "start": "2026-11-07T06:00:00Z",
                "end": "2026-11-10T06:00:00Z",
            },
            "generated_at": "2026-11-06T12:00:00Z",
        }
        v = check_gate2_temporal("docs/reports/soak.json", data, now=self.ref_now)
        self.assertTrue(any("cannot observe soak before window concludes" in vi.detail for vi in v))

    def test_reject_future_timestamp(self):
        # generated_at is 1 day in the future relative to ref_now
        data = {
            "generated_at": "2026-11-11T12:00:00Z",
        }
        v = check_gate2_temporal("docs/reports/future.json", data, now=self.ref_now)
        self.assertTrue(any("is in the future" in vi.detail for vi in v))

    def test_reject_future_window_end(self):
        # window.end is in the future relative to ref_now
        data = {
            "start_time": "2026-11-09T00:00:00Z",
            "end_time": "2026-11-12T00:00:00Z",
            "generated_at": "2026-11-12T01:00:00Z",
        }
        v = check_gate2_temporal("docs/reports/future_window.json", data, now=self.ref_now)
        self.assertTrue(any("window.end" in vi.detail and "in the future" in vi.detail for vi in v))

    def test_reject_inverted_window(self):
        data = {
            "window": {
                "start": "2026-11-10T00:00:00Z",
                "end": "2026-11-08T00:00:00Z",
            },
            "generated_at": "2026-11-10T01:00:00Z",
        }
        v = check_gate2_temporal("docs/reports/inverted.json", data, now=self.ref_now)
        self.assertTrue(any("window.start" in vi.detail and "after window.end" in vi.detail for vi in v))

    def test_duration_mismatch(self):
        # Declares 72h but window is only 24h
        data = {
            "window_hours": 72,
            "start_time": "2026-11-08T00:00:00Z",
            "end_time": "2026-11-09T00:00:00Z",
            "generated_at": "2026-11-09T01:00:00Z",
        }
        v = check_gate2_temporal("docs/reports/duration_mismatch.json", data, now=self.ref_now)
        self.assertTrue(any("does not match declared duration" in vi.detail for vi in v))


class TestGate3ModelIntegrity(unittest.TestCase):
    """Test Gate 3: Model Weight Integrity, Entropy & Stub Rejection."""

    def test_reject_under_one_mb_placeholder(self):
        small_file = b"\x00" * 65536  # 64 KB stub
        v = check_gate3_model("models/yolo.bin", small_file, min_size=1_048_576)
        self.assertTrue(any("below 1 MB threshold" in vi.detail for vi in v))

    def test_reject_padded_null_stub(self):
        padded_nulls = b"\x00" * (2 * 1024 * 1024)  # 2 MB of nulls
        v = check_gate3_model("models/padded.bin", padded_nulls)
        self.assertTrue(any("low Shannon entropy" in vi.detail for vi in v))
        self.assertTrue(any("lack byte diversity" in vi.detail for vi in v))

    def test_reject_repeating_stub_pattern(self):
        repeating = (b"\x00\x01") * (1024 * 1024)  # 2 MB of alternating 0, 1
        v = check_gate3_model("models/alternating.bin", repeating)
        self.assertTrue(any("low Shannon entropy" in vi.detail for vi in v))
        self.assertTrue(any("lack byte diversity" in vi.detail for vi in v))
        self.assertTrue(any("highly compressible" in vi.detail for vi in v))

    def test_reject_long_period_compressible_stub(self):
        # 1024-byte repeated pattern (has high local unique byte count, but repeats)
        pattern = bytes(range(256)) * 4
        repeated = pattern * 1024  # 1 MB
        v = check_gate3_model("models/periodic.bin", repeated)
        self.assertTrue(any("highly compressible" in vi.detail for vi in v))

    def test_accept_genuine_weights(self):
        # Real float tensors have pseudo-random byte distributions
        genuine_weights = os.urandom(2 * 1024 * 1024)
        v = check_gate3_model("models/real.bin", genuine_weights)
        self.assertEqual(len(v), 0)


class TestGate4ProofIntegrity(unittest.TestCase):
    """Test Gate 4: Empirical Proof Artifact Integrity."""

    def test_reject_zero_byte_file(self):
        v = check_gate4_proof("docs/proofs/empty.png", b"")
        self.assertTrue(any("empty (0 bytes)" in vi.detail for vi in v))

    def test_reject_html_disguised_as_image(self):
        html_bytes = b"<!DOCTYPE html><html><body>Mock Screenshot</body></html>"
        v = check_gate4_proof("docs/proofs/mock.png", html_bytes)
        self.assertTrue(any("HTML mockup disguised" in vi.detail for vi in v))

    def test_reject_corrupted_magic_bytes(self):
        bad_magic = b"\x00\x01\x02\x03" + b"\x00" * 1000
        v = check_gate4_proof("docs/proofs/corrupt.jpg", bad_magic)
        self.assertTrue(any("Invalid image magic bytes" in vi.detail for vi in v))

    def test_reject_below_resolution_threshold(self):
        tiny_png = create_mock_png(200, 150)
        v = check_gate4_proof("docs/proofs/tiny.png", tiny_png, min_width=320, min_height=240)
        self.assertTrue(any("below minimum 320x240" in vi.detail for vi in v))

    def test_accept_landscape_proof(self):
        valid_png = create_mock_png(640, 480)
        v = check_gate4_proof("docs/proofs/good.png", valid_png, min_width=320, min_height=240)
        self.assertEqual(len(v), 0)

    def test_accept_portrait_proof(self):
        valid_portrait = create_mock_jpeg(240, 320)
        v = check_gate4_proof("docs/proofs/good_portrait.jpg", valid_portrait, min_width=320, min_height=240)
        self.assertEqual(len(v), 0)


class TestBaselineAndRatchet(unittest.TestCase):
    """Test Baseline Ratchet Engine and CLI operations."""

    def test_ratchet_accepts_known_violation_and_rejects_new(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            tmproot = Path(tmpdir)
            set_root(tmproot)
            try:
                # Create a known bad file
                bad_file = tmproot / "docs/reports/bad.json"
                bad_file.parent.mkdir(parents=True, exist_ok=True)
                bad_file.write_text(json.dumps({"source": "synthetic"}), encoding="utf-8")

                # Baseline with bad.json
                baseline_file = tmproot / BASELINE_FILENAME
                known_v = Violation(
                    gate="gate1_synthetic",
                    path="docs/reports/bad.json",
                    detail="Field 'source' has prohibited synthetic value 'synthetic'",
                )
                save_baseline(baseline_file, [known_v])

                # Without strict: should PASS because violation is in baseline
                report = evaluate_gate([bad_file], baseline_path=baseline_file, strict=False)
                self.assertTrue(report.valid)
                self.assertEqual(len(report.new_violations), 0)

                # With strict: should FAIL
                strict_report = evaluate_gate([bad_file], baseline_path=baseline_file, strict=True)
                self.assertFalse(strict_report.valid)
                self.assertEqual(len(strict_report.new_violations), 1)

                # Add a second violation not in baseline: should FAIL
                new_bad_file = tmproot / "docs/reports/new_bad.json"
                new_bad_file.write_text(json.dumps({"fit_source": "synthetic"}), encoding="utf-8")

                report2 = evaluate_gate([bad_file, new_bad_file], baseline_path=baseline_file, strict=False)
                self.assertFalse(report2.valid)
                self.assertEqual(len(report2.new_violations), 1)
                self.assertEqual(report2.new_violations[0].path, "docs/reports/new_bad.json")
            finally:
                reset_root()

    def test_stale_baseline_entry_fails_ratchet(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            tmproot = Path(tmpdir)
            set_root(tmproot)
            try:
                # File is clean
                clean_file = tmproot / "docs/reports/clean.json"
                clean_file.parent.mkdir(parents=True, exist_ok=True)
                clean_file.write_text(json.dumps({"fit_source": "live_history"}), encoding="utf-8")

                # Baseline claims clean.json had a violation
                baseline_file = tmproot / BASELINE_FILENAME
                stale_v = Violation(
                    gate="gate1_synthetic",
                    path="docs/reports/clean.json",
                    detail="Old fixed bug",
                )
                save_baseline(baseline_file, [stale_v])

                report = evaluate_gate([clean_file], baseline_path=baseline_file, strict=False)
                self.assertFalse(report.valid)
                self.assertEqual(len(report.stale_baseline), 1)
            finally:
                reset_root()


class TestClockDependentBaselineProhibition(unittest.TestCase):
    """Test Task 2.1: dynamic clock-dependent baseline entries are strictly rejected."""

    def test_prohibit_clock_dependent_entries_in_load_baseline(self):
        with tempfile.NamedTemporaryFile("w", suffix=".txt", delete=False) as tf:
            tf.write(
                "gate2_temporal\tdocs/proofs/sprint10/harvester_observation.json\t"
                "window.end (2026-11-10T06:00:00+07:00) is in the future (> current_time + 300s)\n"
            )
            tf_path = Path(tf.name)
        try:
            with self.assertRaises(ValueError) as ctx:
                load_baseline(tf_path)
            self.assertIn("Dynamic clock-dependent baseline entry prohibited", str(ctx.exception))
        finally:
            tf_path.unlink()

    def test_prohibit_clock_dependent_entries_in_save_baseline(self):
        with tempfile.NamedTemporaryFile("w", suffix=".txt", delete=False) as tf:
            tf_path = Path(tf.name)
        try:
            v = Violation(
                gate="gate2_temporal",
                path="docs/test.json",
                detail="generated_at is in the future (> current_time + 300s)",
            )
            with self.assertRaises(ValueError) as ctx:
                save_baseline(tf_path, [v])
            self.assertIn("Dynamic clock-dependent baseline entry prohibited", str(ctx.exception))
        finally:
            if tf_path.exists():
                tf_path.unlink()

    def test_accept_static_baseline_entries(self):
        with tempfile.NamedTemporaryFile("w", suffix=".txt", delete=False) as tf:
            tf.write(
                "gate1_synthetic\tdocs/reports/test.json\tField 'source' has prohibited synthetic value 'synthetic'\n"
            )
            tf_path = Path(tf.name)
        try:
            violations = load_baseline(tf_path)
            self.assertEqual(len(violations), 1)
        finally:
            tf_path.unlink()

    def test_evaluate_gate_rejects_clock_dependent_baseline(self):
        with tempfile.TemporaryDirectory() as td:
            base_dir = Path(td)
            baseline = base_dir / BASELINE_FILENAME
            baseline.write_text(
                "gate2_temporal\tdocs/proofs/test.json\tgenerated_at (2026-11-11T00:00:00Z) is in the future (> current_time + 300s)\n",
                encoding="utf-8",
            )
            with self.assertRaises(ValueError) as ctx:
                evaluate_gate([], baseline_path=baseline)
            self.assertIn("Dynamic clock-dependent baseline entry prohibited", str(ctx.exception))


class TestSupersededArtifactHandling(unittest.TestCase):
    """Test Task 2.2: support artifact invalidation/superseding."""

    def test_valid_superseded_artifact_bypasses_temporal_checks(self):
        with tempfile.TemporaryDirectory() as td:
            root_p = Path(td)
            set_root(root_p)
            try:
                superseding_file = root_p / "docs/proofs/sprint11/new_soak.json"
                superseding_file.parent.mkdir(parents=True, exist_ok=True)
                superseding_file.write_text("{}", encoding="utf-8")

                data = {
                    "status": "SUPERSEDED",
                    "superseded_by": "docs/proofs/sprint11/new_soak.json",
                    "invalidation_reason": "Sprint 10 prototype simulation superseded by authentic soak",
                    "window": {
                        "start": "2026-11-07T06:00:00Z",
                        "end": "2026-11-10T06:00:00Z",
                    },
                    "generated_at": "2026-11-06T10:00:00Z",  # normally fails generated_at < window.end
                }
                violations = check_gate2_temporal("docs/proofs/sprint10/old.json", data)
                self.assertEqual(violations, [])
            finally:
                reset_root()

    def test_superseded_artifact_missing_superseded_by(self):
        data = {
            "status": "SUPERSEDED",
            "invalidation_reason": "Superseded by something",
        }
        violations = check_gate2_temporal("docs/proofs/old.json", data)
        self.assertTrue(any("missing required non-empty 'superseded_by'" in v.detail for v in violations))

    def test_superseded_artifact_nonexistent_superseded_by_target(self):
        data = {
            "status": "SUPERSEDED",
            "superseded_by": "docs/proofs/non_existent_file.json",
            "invalidation_reason": "Superseded by something",
        }
        violations = check_gate2_temporal("docs/proofs/old.json", data)
        self.assertTrue(any("references non-existent superseded_by target" in v.detail for v in violations))

    def test_superseded_artifact_missing_invalidation_reason(self):
        with tempfile.TemporaryDirectory() as td:
            root_p = Path(td)
            set_root(root_p)
            try:
                target_file = root_p / "docs/new.json"
                target_file.parent.mkdir(parents=True, exist_ok=True)
                target_file.write_text("{}", encoding="utf-8")
                data = {
                    "status": "SUPERSEDED",
                    "superseded_by": "docs/new.json",
                    "invalidation_reason": "",
                }
                violations = check_gate2_temporal("docs/proofs/old.json", data)
                self.assertTrue(any("missing required non-empty 'invalidation_reason'" in v.detail for v in violations))
            finally:
                reset_root()


class TestCheckArtifactDispatcher(unittest.TestCase):
    """Test check_artifact routing to different file formats and error handlers."""

    def test_file_not_found(self):
        v = check_artifact(Path("non_existent_file_xyz.json"))
        self.assertEqual(len(v), 1)
        self.assertEqual(v[0].gate, "file_not_found")

    def test_invalid_json_syntax(self):
        with tempfile.NamedTemporaryFile("w", suffix=".json", delete=False) as tf:
            tf.write("{unclosed_json: true")
            tf_path = Path(tf.name)
        try:
            v = check_artifact(tf_path)
            self.assertEqual(len(v), 1)
            self.assertEqual(v[0].gate, "json_parse")
        finally:
            tf_path.unlink()

    def test_markdown_with_synthetic_fixture(self):
        with tempfile.NamedTemporaryFile("w", suffix=".md", delete=False) as tf:
            tf.write("Evaluation based on synthetic_congestion_v2_5m.csv fixture")
            tf_path = Path(tf.name)
        try:
            v = check_artifact(tf_path)
            self.assertTrue(any("synthetic_congestion_v2_5m.csv" in vi.detail for vi in v))
        finally:
            tf_path.unlink()

    def test_unsupported_extension_returns_empty(self):
        with tempfile.NamedTemporaryFile("w", suffix=".txt", delete=False) as tf:
            tf.write("Just some text")
            tf_path = Path(tf.name)
        try:
            v = check_artifact(tf_path)
            self.assertEqual(len(v), 0)
        finally:
            tf_path.unlink()


class TestViolationSerialization(unittest.TestCase):
    """Test Violation dataclass baseline formatting and parsing."""

    def test_roundtrip_serialization(self):
        v = Violation(gate="gate1_synthetic", path="docs/reports/run.json", detail="Prohibited fixture")
        line = v.to_baseline_line()
        self.assertEqual(line, "gate1_synthetic\tdocs/reports/run.json\tProhibited fixture")

        parsed = Violation.from_baseline_line(line)
        self.assertEqual(parsed, v)

    def test_empty_and_comment_lines(self):
        self.assertIsNone(Violation.from_baseline_line(""))
        self.assertIsNone(Violation.from_baseline_line("   "))
        self.assertIsNone(Violation.from_baseline_line("# This is a comment"))

    def test_two_part_line(self):
        parsed = Violation.from_baseline_line("gate3_model\tmodels/yolo.bin")
        self.assertIsNotNone(parsed)
        self.assertEqual(parsed.gate, "gate3_model")
        self.assertEqual(parsed.path, "models/yolo.bin")
        self.assertEqual(parsed.detail, "")


class TestCLIExecution(unittest.TestCase):
    """Test CLI entrypoint main() invocations."""

    def test_cli_file_flag(self):
        import sys
        from unittest.mock import patch
        from io import StringIO
        from tools.ci.check_evidence_integrity import main

        with tempfile.NamedTemporaryFile("w", suffix=".json", delete=False) as tf:
            tf.write(json.dumps({"fit_source": "live_history"}))
            tf_path = tf.name
        try:
            with patch.object(sys, "argv", ["check_evidence_integrity.py", "--file", tf_path, "--strict"]):
                with patch("sys.stdout", new=StringIO()):
                    code = main()
                    self.assertEqual(code, 0)
        finally:
            Path(tf_path).unlink()

    def test_cli_json_output(self):
        import sys
        from unittest.mock import patch
        from io import StringIO
        from tools.ci.check_evidence_integrity import main

        with tempfile.NamedTemporaryFile("w", suffix=".json", delete=False) as tf:
            tf.write(json.dumps({"fit_source": "live_history"}))
            tf_path = tf.name
        try:
            with patch.object(sys, "argv", ["check_evidence_integrity.py", "--file", tf_path, "--json", "--strict"]):
                buf = StringIO()
                with patch("sys.stdout", new=buf):
                    code = main()
                    self.assertEqual(code, 0)
                    out = json.loads(buf.getvalue())
                    self.assertTrue(out["valid"])
                    self.assertEqual(out["violations_count"], 0)
        finally:
            Path(tf_path).unlink()

    def test_cli_dir_flag(self):
        import sys
        from unittest.mock import patch
        from io import StringIO
        from tools.ci.check_evidence_integrity import main

        with tempfile.TemporaryDirectory() as td:
            p = Path(td) / "good.json"
            p.write_text(json.dumps({"fit_source": "live_history"}), encoding="utf-8")
            with patch.object(sys, "argv", ["check_evidence_integrity.py", "--dir", td, "--strict", "--verbose"]):
                with patch("sys.stdout", new=StringIO()):
                    code = main()
                    self.assertEqual(code, 0)

    def test_cli_mode_and_repo_root_flags(self):
        import sys
        from unittest.mock import patch
        from io import StringIO
        from tools.ci.check_evidence_integrity import main

        with patch.object(sys, "argv", ["check_evidence_integrity.py", "--repo-root", str(REPO_ROOT), "--mode", "gate"]):
            with patch("sys.stdout", new=StringIO()):
                code = main()
                self.assertEqual(code, 0)


class TestAdversarialIntegrityVectors(unittest.TestCase):
    """
    Adversarial test vectors from independent auditor review (scratch/opencode_s11_t1_review.md).
    Asserts that deception attempts fail closed.
    """

    def test_adversarial_naive_timestamp_rejection(self):
        # Local naive timestamp without UTC offset
        payload = {
            "window": {
                "start": "2026-11-07T06:00:00",
                "end": "2026-11-10T06:00:00",
            },
            "generated_at": "2026-11-10T06:00:00",
        }
        violations = check_gate2_temporal("docs/reports/soak.json", payload)
        self.assertTrue(any("naive local time" in v.detail for v in violations))

    def test_adversarial_reference_profile_rejection(self):
        payload = {
            "measurement_source": "reference-profile",
            "scenario": "cotenancy_full",
        }
        violations = check_gate1_synthetic("docs/reports/bench.json", payload, json.dumps(payload))
        self.assertTrue(any("prohibited synthetic value 'reference-profile'" in v.detail for v in violations))

    def test_adversarial_periodic_model_stub(self):
        # 4-byte pattern repeated to 2 MB (passes simple size check > 1 MB)
        pattern = b"\x00\x00\x01\x00" * (512 * 1024)
        violations = check_gate3_model("models/yolov8.bin", pattern)
        self.assertTrue(any("periodic repetition" in v.detail for v in violations))

    def test_adversarial_padded_zero_tail_model(self):
        # 128 KB random bytes + 1 MB zeros (padded stub)
        tail_padded = bytes(range(256)) * 512 + b"\x00" * (1024 * 1024)
        violations = check_gate3_model("models/padded.bin", tail_padded)
        self.assertTrue(any("tail consists of null/zero padding" in v.detail for v in violations))

    def test_adversarial_missing_fit_source_forecast(self):
        # Forecast report declaring success but omitting fit_source
        payload = {
            "model": "ridge_regression",
            "target": "congestion_forecast",
            "gate": {"passed": True},
        }
        violations = check_gate1_synthetic("docs/reports/forecast_v3.json", payload, json.dumps(payload), is_release=True)
        self.assertTrue(any("missing required 'fit_source'" in v.detail for v in violations))

    def test_adversarial_degenerate_solid_screenshot(self):
        # Solid white 961x579 image
        try:
            import io
            from PIL import Image
        except ImportError:
            self.skipTest("Pillow not installed in current test runner environment")

        im = Image.new("RGB", (961, 579), color=(255, 255, 255))
        buf = io.BytesIO()
        im.save(buf, format="PNG")
        png_bytes = buf.getvalue()

        violations = check_gate4_proof("docs/proofs/blank_white.png", png_bytes)
        self.assertTrue(any("degenerate solid-color placeholder" in v.detail for v in violations))


class TestLiveRepoEvidenceIntegrity(unittest.TestCase):
    """Verify repository evidence artifacts against current baseline."""

    def test_repo_satisfies_baseline_ratchet(self):
        repo_root = root()
        files = find_tracked_evidence_files(repo_root)
        self.assertGreater(len(files), 0, "Should locate evidence files in repo")
        report = evaluate_gate(files, baseline_path=repo_root / BASELINE_FILENAME, strict=False)
        self.assertTrue(
            report.valid,
            f"Repository must pass evidence integrity gate against baseline: {report.new_violations}",
        )
        self.assertEqual(len(report.new_violations), 0)
        self.assertEqual(len(report.stale_baseline), 0)


if __name__ == "__main__":
    unittest.main()
