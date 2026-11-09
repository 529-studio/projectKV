#!/usr/bin/env python3
"""
Unit tests for tools/eval/benchmark_cotenancy.py.

Verifies latency percentiles, cgroup boundary invariants (including tamper
detection), off-peak cron window rules in both ICT and UTC, adaptive
load-shedding thresholds with hysteresis, and the benchmark matrix invariants.
"""

import copy
import sys
import unittest
import unittest.mock
from datetime import datetime, timezone, timedelta
from pathlib import Path

TOOLS_EVAL_DIR = Path(__file__).resolve().parent
if str(TOOLS_EVAL_DIR) not in sys.path:
    sys.path.insert(0, str(TOOLS_EVAL_DIR))

import benchmark_cotenancy
from benchmark_cotenancy import (
    CGROUP_BOUNDARIES,
    WORKER_HARDWARE,
    calculate_percentiles,
    cgroup_validation_environment_notice,
    evaluate_loadshedding_policy,
    expand_cpuset,
    format_markdown_report,
    is_within_offpeak_window,
    physical_core_ids,
    run_all_benchmarks,
    run_benchmark_scenario,
    validate_benchmark_matrix,
    validate_cron_schedule,
    verify_cgroup_configuration,
)


class TestLatencyPercentiles(unittest.TestCase):
    def test_interpolated_percentiles(self):
        p50, p95, p99 = calculate_percentiles([10.0, 20.0])
        self.assertAlmostEqual(p50, 15.0)
        self.assertAlmostEqual(p95, 19.5)
        self.assertAlmostEqual(p99, 19.9)

    def test_single_sample(self):
        self.assertEqual(calculate_percentiles([7.5]), (7.5, 7.5, 7.5))

    def test_empty_sample(self):
        self.assertEqual(calculate_percentiles([]), (0.0, 0.0, 0.0))

    def test_percentiles_of_unsorted_sample(self):
        p50, p95, p99 = calculate_percentiles([30.0, 10.0, 20.0])
        self.assertAlmostEqual(p50, 20.0)
        self.assertAlmostEqual(p95, 29.0)
        self.assertAlmostEqual(p99, 29.8)


class TestOffPeakWindow(unittest.TestCase):
    def test_ict_window_boundaries(self):
        ict = timezone(timedelta(hours=7))
        allowed = [
            datetime(2026, 11, 7, 1, 0, tzinfo=ict),
            datetime(2026, 11, 7, 2, 30, tzinfo=ict),
            datetime(2026, 11, 7, 4, 59, tzinfo=ict),
        ]
        rejected = [
            datetime(2026, 11, 7, 0, 59, tzinfo=ict),
            datetime(2026, 11, 7, 5, 0, tzinfo=ict),
            datetime(2026, 11, 7, 14, 0, tzinfo=ict),
            datetime(2026, 11, 7, 23, 59, tzinfo=ict),
        ]
        for ts in allowed:
            self.assertTrue(is_within_offpeak_window(ts), ts.isoformat())
        for ts in rejected:
            self.assertFalse(is_within_offpeak_window(ts), ts.isoformat())

    def test_utc_window_is_the_same_instant(self):
        # 02:00 ICT == 19:00 UTC the previous day.
        ts = datetime(2026, 11, 6, 19, 0, tzinfo=timezone.utc)
        self.assertTrue(is_within_offpeak_window(ts, tz_mode="utc"))
        self.assertTrue(is_within_offpeak_window(ts, tz_mode="ict"))
        # 14:00 ICT == 07:00 UTC.
        daytime = datetime(2026, 11, 7, 7, 0, tzinfo=timezone.utc)
        self.assertFalse(is_within_offpeak_window(daytime, tz_mode="utc"))
        self.assertFalse(is_within_offpeak_window(daytime, tz_mode="ict"))

    def test_unknown_timezone_mode(self):
        with self.assertRaises(ValueError):
            is_within_offpeak_window(None, tz_mode="pst")


class TestCronSchedule(unittest.TestCase):
    def test_valid_ict_schedules(self):
        for expression in ("0 2 * * *", "30 1-4 * * *", "0 1,3 * * *", "*/10 4 * * *"):
            ok, message = validate_cron_schedule(expression)
            self.assertTrue(ok, f"{expression}: {message}")

    def test_valid_utc_host_crontab(self):
        ok, message = validate_cron_schedule("0 19 * * *", tz_mode="utc")
        self.assertTrue(ok, message)
        ok, message = validate_cron_schedule("0 18-21 * * *", tz_mode="utc")
        self.assertTrue(ok, message)

    def test_daytime_schedule_rejected(self):
        ok, message = validate_cron_schedule("0 14 * * *")
        self.assertFalse(ok)
        self.assertIn("outside authorized window", message)

        ok, message = validate_cron_schedule("0 7 * * *", tz_mode="utc")
        self.assertFalse(ok)
        self.assertIn("outside authorized window", message)

    def test_window_end_is_exclusive(self):
        ok, message = validate_cron_schedule("0 5 * * *")
        self.assertFalse(ok, message)
        ok, message = validate_cron_schedule("0 22 * * *", tz_mode="utc")
        self.assertFalse(ok, message)

    def test_wildcard_hour_rejected(self):
        for expression in ("* * * * *", "0 */2 * * *"):
            ok, message = validate_cron_schedule(expression)
            self.assertFalse(ok, expression)
            self.assertIn("Wildcard", message)

    def test_start_slash_step_expands_to_end_of_day(self):
        # "2/3" is 02,05,08,...,23, so it escapes the window and must be rejected.
        ok, message = validate_cron_schedule("0 2/3 * * *")
        self.assertFalse(ok, message)
        self.assertIn("05:00", message)

    def test_malformed_schedules_rejected(self):
        ok, message = validate_cron_schedule("0 2 * *")
        self.assertFalse(ok)
        self.assertIn("expected 5 fields", message)

        ok, message = validate_cron_schedule("0 2 * * * *")
        self.assertFalse(ok)

        ok, message = validate_cron_schedule("0 x * * *")
        self.assertFalse(ok)
        self.assertIn("Failed to parse hour", message)

        ok, message = validate_cron_schedule("70 2 * * *")
        self.assertFalse(ok)
        self.assertIn("Failed to parse minute", message)


class TestLoadSheddingPolicy(unittest.TestCase):
    def test_nominal_state(self):
        result = evaluate_loadshedding_policy(14.2, 18.0, current_resolution=416)
        self.assertEqual(result["action"], "NOMINAL")
        self.assertEqual(result["recommended_resolution"], 416)

    def test_exact_thresholds_do_not_degrade(self):
        # The policy is strict: > 25 ms and > 85%.
        self.assertEqual(evaluate_loadshedding_policy(25.0, 85.0, 416)["action"], "NOMINAL")
        self.assertEqual(evaluate_loadshedding_policy(10.0, 85.0, 416)["action"], "NOMINAL")
        self.assertEqual(evaluate_loadshedding_policy(25.0, 10.0, 416)["action"], "NOMINAL")

    def test_latency_breach_degrades(self):
        result = evaluate_loadshedding_policy(25.01, 50.0, current_resolution=416)
        self.assertEqual(result["action"], "DEGRADE_RESOLUTION_320")
        self.assertEqual(result["recommended_resolution"], 320)

    def test_cpu_breach_degrades(self):
        result = evaluate_loadshedding_policy(10.0, 85.01, current_resolution=416)
        self.assertEqual(result["action"], "DEGRADE_RESOLUTION_320")
        self.assertEqual(result["recommended_resolution"], 320)

    def test_saturation_at_320_throttles_batch(self):
        result = evaluate_loadshedding_policy(29.0, 95.0, current_resolution=320)
        self.assertEqual(result["action"], "THROTTLE_BACKGROUND_BATCH")
        self.assertEqual(result["recommended_resolution"], 320)

    def test_hysteresis_recovery_requires_both_conditions(self):
        self.assertEqual(evaluate_loadshedding_policy(12.0, 60.0, 320)["action"], "RECOVER_RESOLUTION_416")
        self.assertEqual(evaluate_loadshedding_policy(20.0, 70.0, 320)["action"], "RECOVER_RESOLUTION_416")
        # Just past either recovery threshold: stay degraded, no oscillation.
        self.assertEqual(evaluate_loadshedding_policy(20.01, 70.0, 320)["action"], "NOMINAL")
        self.assertEqual(evaluate_loadshedding_policy(12.0, 70.01, 320)["action"], "NOMINAL")
        self.assertEqual(evaluate_loadshedding_policy(12.0, 60.0, 320)["recommended_resolution"], 416)

    def test_recovery_does_not_fire_at_416(self):
        result = evaluate_loadshedding_policy(12.0, 60.0, current_resolution=416)
        self.assertEqual(result["action"], "NOMINAL")
        self.assertEqual(result["recommended_resolution"], 416)


class TestCgroupBoundaries(unittest.TestCase):
    def test_cpuset_expansion(self):
        self.assertEqual(expand_cpuset("0-3,8-11"), {0, 1, 2, 3, 8, 9, 10, 11})
        self.assertEqual(expand_cpuset("4"), {4})
        with self.assertRaises(ValueError):
            expand_cpuset("4-2")

    def test_physical_core_mapping_keeps_smt_siblings_together(self):
        self.assertEqual(physical_core_ids("0-3,8-11"), {0, 1, 2, 3})
        self.assertEqual(physical_core_ids("4-7,12-15"), {4, 5, 6, 7})
        with self.assertRaises(ValueError):
            physical_core_ids("0-16")

    def test_canonical_configuration_is_valid(self):
        status = verify_cgroup_configuration()
        self.assertTrue(status["verified"], status["violations"])
        self.assertEqual(status["violations"], [])
        self.assertEqual(status["root_memory_total_mb"], 3584.0)
        self.assertLessEqual(status["root_memory_total_mb"], WORKER_HARDWARE["total_ram_mb"])
        self.assertEqual(status["slices_configured"], ["cv_engine", "sam_prelabel", "ci_runner"])
        results = {c["name"]: c["passed"] for c in status["checks"]}
        self.assertTrue(results["cv_engine.physical_cores"])
        self.assertTrue(results["sam_prelabel.physical_cores"])
        self.assertTrue(results["sam_prelabel.cpu_quota_pct"])
        self.assertTrue(results["sam_prelabel.memory_limit_mb"])
        self.assertTrue(results["ci_runner.within_batch_group"])
        self.assertTrue(results["memory.ci_within_sam_slice"])

    def test_tampered_quota_is_detected(self):
        tampered = copy.deepcopy(CGROUP_BOUNDARIES)
        tampered["sam_prelabel"]["cpu_quota_pct"] = 100.0
        status = verify_cgroup_configuration(tampered)
        self.assertFalse(status["verified"])
        self.assertIn("sam_prelabel.cpu_quota_pct", [v["name"] for v in status["violations"]])

    def test_tampered_cpuset_is_detected(self):
        tampered = copy.deepcopy(CGROUP_BOUNDARIES)
        tampered["cv_engine"]["cpuset_cpus"] = "4-7,12-15"
        status = verify_cgroup_configuration(tampered)
        self.assertFalse(status["verified"])
        names = [v["name"] for v in status["violations"]]
        self.assertIn("cv_engine.physical_cores", names)

    def test_memory_overcommit_is_detected(self):
        tampered = copy.deepcopy(CGROUP_BOUNDARIES)
        tampered["sam_prelabel"]["memory_limit_mb"] = 2048.0
        status = verify_cgroup_configuration(tampered)
        self.assertFalse(status["verified"])
        names = [v["name"] for v in status["violations"]]
        self.assertIn("memory.root_slices_fit_ceiling", names)
        # ci_runner 1024 MB now equals its parent, which is still allowed.
        self.assertNotIn("memory.ci_within_sam_slice", names)

    def test_missing_memory_limit_is_detected_without_crash(self):
        tampered = copy.deepcopy(CGROUP_BOUNDARIES)
        del tampered["sam_prelabel"]["memory_limit_mb"]
        status = verify_cgroup_configuration(tampered)
        self.assertFalse(status["verified"])
        names = [v["name"] for v in status["violations"]]
        self.assertIn("sam_prelabel.memory_limit_mb.present", names)

    def test_ci_runner_cannot_escape_batch_slice(self):
        tampered = copy.deepcopy(CGROUP_BOUNDARIES)
        tampered["ci_runner"]["parent_slice"] = "root"
        tampered["ci_runner"]["cpuset_cpus"] = "0-3,8-11"
        tampered["ci_runner"]["memory_limit_mb"] = 2048.0
        status = verify_cgroup_configuration(tampered)
        self.assertFalse(status["verified"])
        names = [v["name"] for v in status["violations"]]
        self.assertIn("ci_runner.parent_slice", names)
        self.assertIn("ci_runner.within_batch_group", names)
        self.assertIn("memory.ci_within_sam_slice", names)


class TestBenchmarkMatrix(unittest.TestCase):
    def test_unknown_scenario_raises(self):
        with self.assertRaises(ValueError):
            run_benchmark_scenario("nonexistent")

    def test_baseline_profile(self):
        base = run_benchmark_scenario("baseline", hotset_cameras=57)
        self.assertEqual(base["resolution"], 416)
        self.assertEqual(base["measurement_source"], "reference-profile")
        self.assertTrue(base["sla_passed"])
        self.assertLess(base["p95_latency_ms"], WORKER_HARDWARE["p95_sla_ceiling_ms"])
        self.assertGreater(base["headroom_pct"], 90.0)

    def test_full_contention_breaches_sla(self):
        cont = run_benchmark_scenario("cotenancy_full", hotset_cameras=57)
        self.assertFalse(cont["sla_passed"])
        self.assertEqual(cont["sla_status"], "VIOLATED")
        self.assertGreater(cont["p95_latency_ms"], WORKER_HARDWARE["p95_sla_ceiling_ms"])
        self.assertGreater(cont["cpu_utilization_pct"], WORKER_HARDWARE["degrade_cpu_threshold_pct"])

    def test_load_shedding_recovers_sla(self):
        shed = run_benchmark_scenario("loadshed_320", hotset_cameras=57)
        self.assertEqual(shed["resolution"], 320)
        self.assertTrue(shed["sla_passed"])
        self.assertLess(shed["p95_latency_ms"], WORKER_HARDWARE["p95_sla_ceiling_ms"])

    def test_full_matrix_is_valid(self):
        data = run_all_benchmarks()
        self.assertEqual(len(data["scenarios"]), 4)
        self.assertTrue(data["matrix_validation"]["valid"], data["matrix_validation"]["errors"])
        self.assertTrue(data["cgroup_status"]["verified"])
        self.assertEqual(data["measurement_source"], "reference-profile")
        self.assertIn("not a live measurement", data["measurement_note"])

    def test_matrix_rejects_contention_that_does_not_breach(self):
        data = run_all_benchmarks()
        scenarios = copy.deepcopy(data["scenarios"])
        for scenario in scenarios:
            if scenario["scenario_id"] == "cotenancy_full":
                scenario["p95_latency_ms"] = 20.0
                scenario["cpu_utilization_pct"] = 60.0
                scenario["sla_passed"] = True
                scenario["sla_status"] = "PASSED"
        result = validate_benchmark_matrix(scenarios)
        self.assertFalse(result["valid"])
        self.assertTrue(any("full contention must breach" in e for e in result["errors"]))

    def test_matrix_rejects_ineffective_shedding(self):
        data = run_all_benchmarks()
        scenarios = copy.deepcopy(data["scenarios"])
        for scenario in scenarios:
            if scenario["scenario_id"] == "loadshed_320":
                scenario["p95_latency_ms"] = 40.0
                scenario["sla_passed"] = False
                scenario["sla_status"] = "VIOLATED"
        result = validate_benchmark_matrix(scenarios)
        self.assertFalse(result["valid"])
        self.assertTrue(any("320 px load-shedding must pass" in e for e in result["errors"]))

    def test_matrix_rejects_missing_scenario_in_complete_set(self):
        data = run_all_benchmarks()
        scenarios = copy.deepcopy(data["scenarios"])
        # Claim completeness (four rows) while replacing one scenario with a duplicate:
        # the missing scenario must still be reported.
        scenarios[-1] = copy.deepcopy(scenarios[0])
        result = validate_benchmark_matrix(scenarios)
        self.assertFalse(result["valid"])
        self.assertTrue(any("Missing scenarios" in e for e in result["errors"]))
        self.assertTrue(any("Duplicate scenario" in e for e in result["errors"]))

    def test_matrix_validates_single_scenario_subset_without_completeness_check(self):
        # --scenario baseline is a legitimate request; it must not be failed for the
        # three scenarios it never claimed to produce.
        result = validate_benchmark_matrix([run_benchmark_scenario("baseline")])
        self.assertTrue(result["valid"], result["errors"])

    def test_matrix_rejects_an_invalid_scenario_in_a_subset(self):
        scenario = run_benchmark_scenario("loadshed_320")
        scenario["sla_passed"] = False
        scenario["sla_status"] = "VIOLATED"
        result = validate_benchmark_matrix([scenario])
        self.assertFalse(result["valid"])
        self.assertTrue(any("320 px load-shedding must pass" in e for e in result["errors"]))

    def test_matrix_rejects_empty_and_unknown_scenarios(self):
        self.assertFalse(validate_benchmark_matrix([])["valid"])
        unknown = run_benchmark_scenario("baseline")
        unknown["scenario_id"] = "not_a_scenario"
        result = validate_benchmark_matrix([unknown])
        self.assertFalse(result["valid"])
        self.assertTrue(any("Unknown scenario" in e for e in result["errors"]))


class TestCgroupEnvironmentNotice(unittest.TestCase):
    def test_linux_with_cgroupfs_has_no_notice(self):
        with unittest.mock.patch.object(benchmark_cotenancy.sys, "platform", "linux"), \
                unittest.mock.patch.object(Path, "exists", return_value=True):
            self.assertIsNone(cgroup_validation_environment_notice())

    def test_macos_reports_schema_only_validation(self):
        with unittest.mock.patch.object(benchmark_cotenancy.sys, "platform", "darwin"):
            notice = cgroup_validation_environment_notice()
        self.assertEqual(
            notice,
            "Validating cgroup policy configuration (Host: macOS, cgroups not present "
            "locally; container cgroup boundaries verified against schema)",
        )

    def test_linux_without_cgroupfs_reports_schema_only_validation(self):
        with unittest.mock.patch.object(benchmark_cotenancy.sys, "platform", "linux"), \
                unittest.mock.patch.object(Path, "exists", return_value=False):
            notice = cgroup_validation_environment_notice()
        self.assertIn("cgroups not present locally", notice)


class TestMarkdownReport(unittest.TestCase):
    def test_report_contains_matrix_and_provenance(self):
        md = format_markdown_report(run_all_benchmarks())
        self.assertIn("# Inference Worker Co-Tenancy Benchmark & Capacity Report", md)
        self.assertIn("AMD Ryzen 7 6800HS", md)
        self.assertIn("| Scenario | Resolution |", md)
        self.assertIn("Measurement provenance", md)
        self.assertIn("planning figures", md)
        self.assertIn("VIOLATED", md)
        self.assertIn("PASSED", md)
        self.assertIn("Matrix invariant validation: **PASSED**", md)


if __name__ == "__main__":
    unittest.main()
