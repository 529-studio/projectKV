#!/usr/bin/env python3
"""Unit tests for check_deploy_manifests.py."""

from __future__ import annotations

import tempfile
import unittest
from pathlib import Path

from tools.ci import check_deploy_manifests as cdm

VALID_INIT_VOL = """
  warded-init-vol:
    image: alpine:3.20
    restart: "no"
    user: "0:0"
    volumes:
      - warded-stg-data:/app/data
    command:
      - sh
      - -c
      - mkdir -p /app/data/locks /app/data/forecast /app/data/proofs && chown -R 10001:10001 /app/data && chmod -R 775 /app/data
"""

VALID_BACKUP_SERVICE = """
  warded-backup:
    image: alpine:3.20
    container_name: warded-backup
    restart: unless-stopped
    group_add:
      - "10001"
    networks:
      - core-network
    depends_on:
      warded-init-vol:
        condition: service_completed_successfully
    volumes:
      - warded-stg-data:/app/data
      - warded-backup-staging:/backup
    environment:
      - RCLONE_CONFIG_R2_TYPE=s3
      - BACKUP_BUCKET_NAME=${BACKUP_BUCKET_NAME:-homelab-backup}
      - KEEP_DAYS=${BACKUP_KEEP_DAYS:-14}
    entrypoint: ["/bin/sh", "-c"]
    command:
      - |
        sqlite3 /app/data/camera-view.db ".backup '/backup/camera-view.db'"
    tmpfs:
      - /tmp:rw,noexec,nosuid,size=32m,mode=1777
    security_opt:
      - no-new-privileges:true
    cap_drop:
      - ALL
    deploy:
      resources:
        limits:
          cpus: "0.10"
          memory: 128M
"""

VALID_CV_COMPOSE = """# CV engine on core-network, no published port.
services:
  warded-cv-engine:
    image: ghcr.io/529-studio/projectkv-model:stg-latest
    container_name: warded-cv-engine
    restart: unless-stopped
    networks:
      - core-network
    environment:
      - PORT=8000
      - CV_REQUIRE_CUDA=1

networks:
  core-network:
    name: ${DOCKER_NETWORK:-core-network}
    external: true
"""


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
        self.cv_compose_path = self.root / "docker-compose.cv.yml"

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
        self.compose_path.write_text(
            "services:\n"
            + VALID_INIT_VOL
            + """
  warded-backend:
    image: ghcr.io/529-studio/projectkv-backend:stg-latest
    depends_on:
      warded-init-vol:
        condition: service_completed_successfully
"""
            + """
  warded-forecast-cron:
    profiles: ["forecast-refit"]
    user: "10001:10001"
    read_only: true
    depends_on:
      warded-init-vol:
        condition: service_completed_successfully
    healthcheck:
      test: ["CMD", "python3", "tools/forecast/train_live_model.py", "--health"]
"""
            + VALID_BACKUP_SERVICE
        )
        self.cv_compose_path.write_text(VALID_CV_COMPOSE)

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

    # --- Backup sidecar invariants ---------------------------------------

    def test_real_backup_sidecar_satisfies_invariants(self) -> None:
        violations = cdm.check_backup_sidecar(cdm.REPO_ROOT / "docker-compose.stg.yml")
        self.assertEqual(violations, [])

    def test_restored_litestream_service_flagged(self) -> None:
        self.compose_path.write_text(
            self.compose_path.read_text()
            + """
  warded-litestream:
    image: litestream/litestream:0.3.13
    restart: unless-stopped
"""
        )
        violations = cdm.check_backup_sidecar(self.compose_path)
        self.assertTrue(any(v.rule == "backup_legacy_replication" for v in violations))

    def test_missing_backup_service_flagged(self) -> None:
        self.compose_path.write_text(
            self.compose_path.read_text().replace(
                VALID_BACKUP_SERVICE, "\n  unrelated-service:\n    image: alpine:3.20\n"
            )
        )
        violations = cdm.check_backup_sidecar(self.compose_path)
        self.assertTrue(any(v.rule == "backup_service" for v in violations))

    def test_backup_read_only_rootfs_flagged(self) -> None:
        self.compose_path.write_text(
            self.compose_path.read_text().replace(
                "    tmpfs:\n      - /tmp:rw,noexec,nosuid,size=32m,mode=1777\n",
                "    read_only: true\n    tmpfs:\n      - /tmp:rw,noexec,nosuid,size=32m,mode=1777\n",
            )
        )
        violations = cdm.check_backup_sidecar(self.compose_path)
        self.assertTrue(any(v.rule == "backup_rootfs" for v in violations))

    def test_backup_missing_staging_volume_flagged(self) -> None:
        self.compose_path.write_text(
            self.compose_path.read_text().replace("      - warded-backup-staging:/backup\n", "")
        )
        violations = cdm.check_backup_sidecar(self.compose_path)
        self.assertTrue(any(v.rule == "backup_staging_volume" for v in violations))

    def test_backup_tmpfs_snapshot_flagged(self) -> None:
        self.compose_path.write_text(
            self.compose_path.read_text().replace(
                "    tmpfs:\n      - /tmp:rw,noexec,nosuid,size=32m,mode=1777\n",
                "    tmpfs:\n"
                "      - /tmp:rw,noexec,nosuid,size=32m,mode=1777\n"
                "      - /backup:rw,size=512m,mode=1777\n",
            )
        )
        violations = cdm.check_backup_sidecar(self.compose_path)
        self.assertTrue(any(v.rule == "backup_tmpfs" for v in violations))

    def test_backup_snapshot_outside_staging_volume_flagged(self) -> None:
        self.compose_path.write_text(
            self.compose_path.read_text().replace(
                '".backup \'/backup/camera-view.db\'"', '".backup \'/tmp/camera-view.db\'"'
            )
        )
        violations = cdm.check_backup_sidecar(self.compose_path)
        self.assertTrue(any(v.rule == "backup_snapshot_path" for v in violations))

    def test_backup_without_limits_flagged(self) -> None:
        self.compose_path.write_text(
            self.compose_path.read_text().replace(
                """    deploy:
      resources:
        limits:
          cpus: "0.10"
          memory: 128M
""",
                "",
            )
        )
        violations = cdm.check_backup_sidecar(self.compose_path)
        self.assertTrue(any(v.rule == "backup_limits" for v in violations))

    def test_backup_missing_group_add_flagged(self) -> None:
        self.compose_path.write_text(
            self.compose_path.read_text().replace('    group_add:\n      - "10001"\n', "")
        )
        violations = cdm.check_backup_sidecar(self.compose_path)
        self.assertTrue(any(v.rule == "backup_group_add" for v in violations))

    def test_backup_missing_data_volume_flagged(self) -> None:
        self.compose_path.write_text(
            self.compose_path.read_text().replace(
                "      - warded-stg-data:/app/data\n",
                "      - warded-stg-data:/data\n",
            )
        )
        violations = cdm.check_backup_sidecar(self.compose_path)
        self.assertTrue(any(v.rule == "backup_volume" for v in violations))

    def test_backup_wrong_bucket_flagged(self) -> None:
        self.compose_path.write_text(
            self.compose_path.read_text().replace("homelab-backup", "warded-proofs")
        )
        violations = cdm.check_backup_sidecar(self.compose_path)
        self.assertTrue(any(v.rule == "backup_bucket" for v in violations))

    def test_backup_raw_file_copy_flagged(self) -> None:
        self.compose_path.write_text(
            self.compose_path.read_text().replace(
                'sqlite3 /app/data/camera-view.db ".backup \'/backup/camera-view.db\'"',
                "cp /app/data/camera-view.db /backup/camera-view.db",
            )
        )
        violations = cdm.check_backup_sidecar(self.compose_path)
        self.assertTrue(any(v.rule == "backup_command" for v in violations))

    def test_backup_must_wait_for_volume_init(self) -> None:
        self.compose_path.write_text(
            self.compose_path.read_text().replace(
                """    depends_on:
      warded-init-vol:
        condition: service_completed_successfully
    volumes:
      - warded-stg-data:/app/data
      - warded-backup-staging:/backup""",
                """    volumes:
      - warded-stg-data:/app/data
      - warded-backup-staging:/backup""",
            )
        )
        violations = cdm.check_backup_sidecar(self.compose_path)
        self.assertTrue(any(v.rule == "backup_depends_on" for v in violations))

    # --- CV compose invariants -------------------------------------------

    def test_real_cv_compose_satisfies_invariants(self) -> None:
        violations = cdm.check_cv_compose(cdm.REPO_ROOT / "docker-compose.cv.yml")
        self.assertEqual(violations, [])

    def test_cv_published_port_flagged(self) -> None:
        self.cv_compose_path.write_text(
            self.cv_compose_path.read_text().replace(
                "    networks:\n      - core-network\n",
                '    ports:\n      - "8000:8000"\n    networks:\n      - core-network\n',
                1,
            )
        )
        violations = cdm.check_cv_compose(self.cv_compose_path)
        self.assertTrue(any(v.rule == "cv_ports" for v in violations))

    def test_cv_without_core_network_flagged(self) -> None:
        self.cv_compose_path.write_text(
            self.cv_compose_path.read_text()
            .replace("    networks:\n      - core-network\n", "")
            .split("networks:\n  core-network:")[0]
        )
        violations = cdm.check_cv_compose(self.cv_compose_path)
        self.assertTrue(any(v.rule == "cv_network" for v in violations))

    def test_cv_compose_missing_file_flagged(self) -> None:
        violations = cdm.check_cv_compose(self.root / "docker-compose.absent.yml")
        self.assertTrue(any(v.rule == "existence" for v in violations))

    # --- Volume init invariants ------------------------------------------

    def test_backend_must_wait_for_volume_init(self) -> None:
        text = self.compose_path.read_text()
        broken = """  warded-backend:
    image: ghcr.io/529-studio/projectkv-backend:stg-latest
"""
        self.compose_path.write_text(
            text.replace(
                broken
                + """    depends_on:
      warded-init-vol:
        condition: service_completed_successfully
""",
                broken,
            )
        )
        violations = cdm.check_compose_ha_stack(self.compose_path)
        self.assertTrue(any(v.rule == "depends_on" for v in violations))

    def test_init_volume_command_must_fix_ownership(self) -> None:
        self.compose_path.write_text(
            self.compose_path.read_text().replace(
                "chown -R 10001:10001 /app/data", "chown -R 0:0 /app/data"
            )
        )
        violations = cdm.check_compose_ha_stack(self.compose_path)
        self.assertTrue(any(v.rule == "init_vol_command" for v in violations))

    def test_init_volume_must_run_once_as_root(self) -> None:
        self.compose_path.write_text(
            self.compose_path.read_text().replace(
                '    restart: "no"\n    user: "0:0"', '    restart: unless-stopped\n    user: "0:0"'
            )
        )
        violations = cdm.check_compose_ha_stack(self.compose_path)
        self.assertTrue(any(v.rule == "init_vol_restart" for v in violations))


if __name__ == "__main__":
    unittest.main()
