#!/usr/bin/env python3
import json
import os
import tempfile
import unittest
from thermal_monitor import ThermalMonitor


class TestThermalMonitor(unittest.TestCase):
    def setUp(self):
        self.tmpdir = tempfile.TemporaryDirectory()
        self.sys_root = self.tmpdir.name

    def tearDown(self):
        self.tmpdir.cleanup()

    def _create_mock_thermal_zone(self, zone_name: str, temp_milli_c: int, zone_type: str = "x86_pkg_temp"):
        zone_dir = os.path.join(self.sys_root, "sys/class/thermal", zone_name)
        os.makedirs(zone_dir, exist_ok=True)
        with open(os.path.join(zone_dir, "temp"), "w") as f:
            f.write(f"{temp_milli_c}\n")
        with open(os.path.join(zone_dir, "type"), "w") as f:
            f.write(f"{zone_type}\n")

    def _create_mock_cpu_core(self, cpu_name: str, freq_khz: int):
        core_dir = os.path.join(self.sys_root, "sys/devices/system/cpu", cpu_name, "cpufreq")
        os.makedirs(core_dir, exist_ok=True)
        with open(os.path.join(core_dir, "scaling_cur_freq"), "w") as f:
            f.write(f"{freq_khz}\n")

    def test_sample_normal_temperature(self):
        self._create_mock_thermal_zone("thermal_zone0", 52000)
        self._create_mock_cpu_core("cpu0", 1833000)

        monitor = ThermalMonitor(sys_root=self.sys_root)
        report = monitor.sample()

        self.assertEqual(report["status"], "NORMAL")
        self.assertFalse(report["throttling_risk"])
        self.assertEqual(report["max_temperature_c"], 52.0)
        self.assertEqual(len(report["thermal_zones"]), 1)
        self.assertEqual(len(report["cpu_cores"]), 1)
        self.assertEqual(report["cpu_cores"][0]["frequency_mhz"], 1833.0)

    def test_sample_elevated_and_warning_temperature(self):
        self._create_mock_thermal_zone("thermal_zone0", 72000)
        monitor = ThermalMonitor(sys_root=self.sys_root)
        report = monitor.sample()
        self.assertEqual(report["status"], "ELEVATED")
        self.assertFalse(report["throttling_risk"])

        # Update to warning
        with open(os.path.join(self.sys_root, "sys/class/thermal/thermal_zone0/temp"), "w") as f:
            f.write("78000\n")
        report2 = monitor.sample()
        self.assertEqual(report2["status"], "WARNING")
        self.assertTrue(report2["throttling_risk"])

    def test_sample_critical_throttling_temperature(self):
        self._create_mock_thermal_zone("thermal_zone0", 87000)
        monitor = ThermalMonitor(sys_root=self.sys_root)
        report = monitor.sample()
        self.assertEqual(report["status"], "CRITICAL_THROTTLING")
        self.assertTrue(report["throttling_risk"])


if __name__ == "__main__":
    unittest.main()
