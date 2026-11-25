#!/usr/bin/env python3
"""
Integration test executing tools/dr/failover_drill.sh (Issue #556 / Sprint 19 Task 8).
"""

import json
import subprocess
import tempfile
import unittest
from pathlib import Path

DRILL_SCRIPT = Path(__file__).resolve().parent / "failover_drill.sh"


class TestFailoverDrill(unittest.TestCase):
    def test_failover_drill_zero_loss_and_cleanup(self):
        self.assertTrue(DRILL_SCRIPT.is_file(), f"Missing drill script at {DRILL_SCRIPT}")

        with tempfile.TemporaryDirectory() as td:
            report_path = Path(td) / "dr_report.json"
            proc = subprocess.run(
                ["bash", str(DRILL_SCRIPT), "--report-json", str(report_path)],
                capture_output=True,
                text=True,
                timeout=30,
            )
            self.assertEqual(
                proc.returncode,
                0,
                f"Failover drill failed (rc={proc.returncode}):\nSTDOUT:\n{proc.stdout}\nSTDERR:\n{proc.stderr}",
            )
            self.assertIn("PRAGMA integrity_check=ok", proc.stdout)
            self.assertTrue(report_path.is_file())

            data = json.loads(report_path.read_text(encoding="utf-8"))
            self.assertEqual(data["status"], "pass")
            self.assertEqual(data["sqlite_integrity"], "ok")
            self.assertTrue(data["zero_data_loss"])
            self.assertEqual(data["primary_counts"], data["secondary_counts"])
            self.assertEqual(data["secondary_counts"]["verdicts"], 51)
            self.assertEqual(data["secondary_counts"]["push_subscriptions"], 10)


if __name__ == "__main__":
    unittest.main()
