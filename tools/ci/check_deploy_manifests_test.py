#!/usr/bin/env python3
"""Unit tests for check_deploy_manifests.py."""

from __future__ import annotations

import tempfile
import unittest
from pathlib import Path

from tools.ci import check_deploy_manifests as cdm


class TestCheckDeployManifests(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.deploy_dir = self.root / "deploy"
        self.k8s_dir = self.deploy_dir / "k8s"
        self.systemd_dir = self.deploy_dir / "systemd"
        self.k8s_dir.mkdir(parents=True)
        self.systemd_dir.mkdir(parents=True)

        self.cronjob_path = self.k8s_dir / "forecast-refit-cronjob.yaml"
        self.service_path = self.systemd_dir / "projectkv-forecast-refit.service"
        self.timer_path = self.systemd_dir / "projectkv-forecast-refit.timer"
        self.compose_path = self.root / "docker-compose.stg.yml"

        # Populate valid templates
        self.cronjob_path.write_text("""
apiVersion: batch/v1
kind: CronJob
spec:
  concurrencyPolicy: Forbid
  timeZone: "Asia/Ho_Chi_Minh"
  schedule: "0 2,8,14,20 * * *"
  jobTemplate:
    spec:
      activeDeadlineSeconds: 900
      template:
        spec:
          securityContext:
            runAsNonRoot: true
          containers:
            - name: forecast-refit
              image: ghcr.io/529-studio/projectkv-backend:stg-latest
              securityContext:
                readOnlyRootFilesystem: true
                capabilities:
                  drop:
                    - ALL
              resources:
                limits:
                  cpu: 500m
                  memory: 512Mi
""")
        self.service_path.write_text("""
[Service]
Type=oneshot
NoNewPrivileges=true
ProtectSystem=strict
TimeoutStartSec=900
""")
        self.timer_path.write_text("""
[Timer]
OnCalendar=*-*-* 02,08,14,20:00:00 Asia/Ho_Chi_Minh
Persistent=true
""")
        self.compose_path.write_text("""
services:
  warded-forecast-cron:
    profiles: ["forecast-refit"]
    user: "10001:10001"
    read_only: true
    healthcheck:
      test: ["CMD", "python3", "tools/forecast/train_live_model.py", "--health"]
""")

    def test_valid_manifests_produce_zero_violations(self) -> None:
        violations = cdm.check_all(self.root)
        self.assertEqual(violations, [])

    def test_k8s_concurrency_violation_flagged(self) -> None:
        self.cronjob_path.write_text(self.cronjob_path.read_text().replace("Forbid", "Allow"))
        violations = cdm.check_k8s_cronjob(self.cronjob_path)
        self.assertTrue(any(v.rule == "concurrency" for v in violations))

    def test_k8s_timezone_violation_flagged(self) -> None:
        self.cronjob_path.write_text(self.cronjob_path.read_text().replace("Asia/Ho_Chi_Minh", "UTC"))
        violations = cdm.check_k8s_cronjob(self.cronjob_path)
        self.assertTrue(any(v.rule == "timezone" for v in violations))

    def test_k8s_prohibited_bare_latest_tag_flagged(self) -> None:
        self.cronjob_path.write_text(
            self.cronjob_path.read_text().replace("backend:stg-latest", "backend:latest")
        )
        violations = cdm.check_k8s_cronjob(self.cronjob_path)
        self.assertTrue(any(v.rule == "tag" for v in violations))

    def test_systemd_schedule_mismatch_flagged(self) -> None:
        self.timer_path.write_text(self.timer_path.read_text().replace("02,08,14,20:00:00", "01,07,13,19:00:00"))
        violations = cdm.check_systemd_units(self.service_path, self.timer_path)
        self.assertTrue(any(v.rule == "schedule_agreement" for v in violations))

    def test_compose_missing_profile_flagged(self) -> None:
        self.compose_path.write_text(self.compose_path.read_text().replace('"forecast-refit"', '"unrelated"'))
        violations = cdm.check_compose_sidecar(self.compose_path)
        self.assertTrue(any(v.rule == "profiles" for v in violations))


if __name__ == "__main__":
    unittest.main()
