#!/usr/bin/env python3
"""Unit tests for check_deploy_manifests.py."""

from __future__ import annotations

import tempfile
import unittest
from pathlib import Path

from tools.ci import check_deploy_manifests as cdm

VALID_LITESTREAM = """
dbs:
  - path: /app/data/camera-view.db
    replicas:
      - type: s3
        name: r2
        bucket: ${R2_BUCKET_NAME}
        path: db-replicas/camera-view.db
        endpoint: ${R2_ENDPOINT}
        region: ${R2_REGION}
        access-key-id: ${LITESTREAM_ACCESS_KEY_ID}
        secret-access-key: ${LITESTREAM_SECRET_ACCESS_KEY}
        sync-interval: 1s
        snapshot-interval: 24h
        retention: 168h
"""

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

VALID_LITESTREAM_SERVICE = """
  warded-litestream:
    image: litestream/litestream:0.3.13
    restart: unless-stopped
    user: "10001:10001"
    depends_on:
      warded-init-vol:
        condition: service_completed_successfully
    volumes:
      - warded-stg-data:/app/data
    entrypoint: ["/bin/sh", "-c"]
    command:
      - |
        cat > /tmp/litestream.yml <<'LITESTREAM_CONFIG'
        dbs:
          - path: /app/data/camera-view.db
            replicas:
              - type: s3
                name: r2
                bucket: ${R2_BUCKET_NAME}
                path: db-replicas/camera-view.db
                endpoint: ${R2_ENDPOINT}
                region: ${R2_REGION}
                access-key-id: $${LITESTREAM_ACCESS_KEY_ID}
                secret-access-key: $${LITESTREAM_SECRET_ACCESS_KEY}
                sync-interval: 1s
                snapshot-interval: 24h
                retention: 168h
        LITESTREAM_CONFIG
        exec litestream replicate -config /tmp/litestream.yml
    environment:
      - R2_ENDPOINT=${R2_ENDPOINT:?R2_ENDPOINT must be provided for Litestream replication}
      - LITESTREAM_ACCESS_KEY_ID=${LITESTREAM_ACCESS_KEY_ID:-${R2_ACCESS_KEY_ID:-}}
      - LITESTREAM_SECRET_ACCESS_KEY=${LITESTREAM_SECRET_ACCESS_KEY:-${R2_SECRET_ACCESS_KEY:-}}
    read_only: true
    cap_drop:
      - ALL
    deploy:
      resources:
        limits:
          cpus: "0.20"
          memory: 64M
"""


class TestCheckDeployManifests(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.deploy_dir = self.root / "deploy"
        self.k8s_dir = self.deploy_dir / "k8s"
        self.systemd_dir = self.deploy_dir / "systemd"
        self.litestream_dir = self.deploy_dir / "litestream"
        self.k8s_dir.mkdir(parents=True)
        self.systemd_dir.mkdir(parents=True)
        self.litestream_dir.mkdir(parents=True)

        self.cronjob_path = self.k8s_dir / "forecast-refit-cronjob.yaml"
        self.service_path = self.systemd_dir / "projectkv-forecast-refit.service"
        self.timer_path = self.systemd_dir / "projectkv-forecast-refit.timer"
        self.litestream_path = self.litestream_dir / "litestream.yml"
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
        self.litestream_path.write_text(VALID_LITESTREAM)
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
            + VALID_LITESTREAM_SERVICE
        )

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

    # --- Litestream config invariants ------------------------------------

    def test_litestream_missing_config_flagged(self) -> None:
        violations = cdm.check_litestream_config(self.litestream_dir / "absent.yml")
        self.assertTrue(any(v.rule == "existence" for v in violations))

    def test_litestream_wrong_sync_interval_flagged(self) -> None:
        self.litestream_path.write_text(
            self.litestream_path.read_text().replace("sync-interval: 1s", "sync-interval: 60s")
        )
        violations = cdm.check_litestream_config(self.litestream_path)
        self.assertTrue(any(v.rule == "sync_interval" for v in violations))

    def test_litestream_wrong_replica_path_flagged(self) -> None:
        self.litestream_path.write_text(
            self.litestream_path.read_text().replace(
                "path: db-replicas/camera-view.db", "path: db-replicas/other.db"
            )
        )
        violations = cdm.check_litestream_config(self.litestream_path)
        self.assertTrue(any(v.rule == "replica_path" for v in violations))

    def test_litestream_hardcoded_credentials_flagged(self) -> None:
        self.litestream_path.write_text(
            self.litestream_path.read_text().replace(
                "access-key-id: ${LITESTREAM_ACCESS_KEY_ID}", "access-key-id: AKIAIOSFODNN7EXAMPLE"
            )
        )
        violations = cdm.check_litestream_config(self.litestream_path)
        rules = {v.rule for v in violations}
        self.assertIn("hardcoded_secret", rules)

    def test_litestream_accepts_r2_credential_fallback_names(self) -> None:
        text = self.litestream_path.read_text().replace(
            "${LITESTREAM_ACCESS_KEY_ID}", "${R2_ACCESS_KEY_ID}"
        )
        self.litestream_path.write_text(text)
        violations = cdm.check_litestream_config(self.litestream_path)
        self.assertEqual([v for v in violations if v.rule == "hardcoded_secret"], [])

    def test_litestream_missing_bucket_indirection_flagged(self) -> None:
        self.litestream_path.write_text(
            self.litestream_path.read_text().replace(
                "bucket: ${R2_BUCKET_NAME}", "bucket: hardcoded-bucket"
            )
        )
        violations = cdm.check_litestream_config(self.litestream_path)
        self.assertTrue(any(v.rule == "bucket" for v in violations))

    # --- Compose HA stack invariants -------------------------------------

    def test_missing_litestream_service_flagged(self) -> None:
        text = self.compose_path.read_text().replace(
            VALID_LITESTREAM_SERVICE, "\n  unrelated-service:\n    image: alpine:3.20\n"
        )
        self.compose_path.write_text(text)
        violations = cdm.check_compose_ha_stack(self.compose_path)
        self.assertTrue(any(v.rule == "service" for v in violations))

    def test_unpinned_litestream_image_flagged(self) -> None:
        self.compose_path.write_text(
            self.compose_path.read_text().replace(
                "image: litestream/litestream:0.3.13", "image: litestream/litestream:latest"
            )
        )
        violations = cdm.check_compose_ha_stack(self.compose_path)
        rules = {v.rule for v in violations}
        self.assertIn("litestream_image", rules)

    def test_litestream_host_relative_config_mount_flagged(self) -> None:
        self.compose_path.write_text(
            self.compose_path.read_text().replace(
                "      - warded-stg-data:/app/data\n    entrypoint: [\"/bin/sh\", \"-c\"]",
                "      - warded-stg-data:/app/data\n"
                "      - ./deploy/litestream/litestream.yml:/etc/litestream.yml:ro\n"
                "    entrypoint: [\"/bin/sh\", \"-c\"]",
            )
        )
        violations = cdm.check_compose_ha_stack(self.compose_path)
        self.assertTrue(any(v.rule == "litestream_config_mount" for v in violations))

    def test_litestream_missing_inline_config_flagged(self) -> None:
        self.compose_path.write_text(
            self.compose_path.read_text().replace(
                "cat > /tmp/litestream.yml <<'LITESTREAM_CONFIG'",
                "true #",
            )
        )
        violations = cdm.check_compose_ha_stack(self.compose_path)
        self.assertTrue(any(v.rule == "litestream_config" for v in violations))

    def test_litestream_must_wait_for_volume_init(self) -> None:
        text = self.compose_path.read_text()
        marker = """  warded-litestream:
    image: litestream/litestream:0.3.13
    restart: unless-stopped
    user: "10001:10001"
    depends_on:
      warded-init-vol:
        condition: service_completed_successfully
"""
        self.compose_path.write_text(text.replace(marker, marker.split("    depends_on:")[0]))
        violations = cdm.check_compose_ha_stack(self.compose_path)
        self.assertTrue(any(v.rule == "litestream_depends_on" for v in violations))

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

    def test_litestream_endpoint_must_be_required(self) -> None:
        self.compose_path.write_text(
            self.compose_path.read_text().replace(
                "R2_ENDPOINT=${R2_ENDPOINT:?R2_ENDPOINT must be provided for Litestream replication}",
                "R2_ENDPOINT=${R2_ENDPOINT:-}",
            )
        )
        violations = cdm.check_compose_ha_stack(self.compose_path)
        self.assertTrue(any(v.rule == "litestream_endpoint" for v in violations))

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
