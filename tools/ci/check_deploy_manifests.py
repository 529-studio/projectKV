#!/usr/bin/env python3
"""check_deploy_manifests.py - Static deployment manifest invariant validator.

Enforces structural and security invariants across deploy/ and compose manifests:
- k8s CronJob: Forbid concurrency, Asia/Ho_Chi_Minh timeZone, non-root securityContext,
  resource limits, activeDeadlineSeconds, no bare :latest tag, no hardcoded secrets.
- systemd timer/service: OnCalendar matches CronJob slots, Persistent=true, Type=oneshot,
  NoNewPrivileges, ProtectSystem=strict.
- docker-compose.stg.yml sidecars: forecast worker profile/user/read_only/healthcheck,
  the daily SQLite backup sidecar, and the volume-ownership init container. The
  removed streaming-replication sidecar must not come back.
- docker-compose.cv.yml: the CV engine joins core-network and publishes no host
  port, so consumers reach it by service name only.

Exit code 0 on pass, 1 on violations.
"""

from __future__ import annotations

import argparse
import re
import sys
from pathlib import Path
from typing import NamedTuple

REPO_ROOT = Path(__file__).resolve().parent.parent.parent


class Violation(NamedTuple):
    file_path: Path
    rule: str
    message: str


def check_k8s_cronjob(manifest_path: Path) -> list[Violation]:
    """Validate invariants for deploy/k8s/forecast-refit-cronjob.yaml."""
    violations: list[Violation] = []
    if not manifest_path.exists():
        return [Violation(manifest_path, "existence", "Manifest file does not exist")]

    text = manifest_path.read_text(encoding="utf-8")

    # 1. Concurrency policy must be Forbid
    if not re.search(r"concurrencyPolicy:\s*Forbid", text):
        violations.append(
            Violation(manifest_path, "concurrency", "Expected concurrencyPolicy: Forbid")
        )

    # 2. Timezone must be Asia/Ho_Chi_Minh
    if not re.search(r'timeZone:\s*"?Asia/Ho_Chi_Minh"?', text):
        violations.append(
            Violation(manifest_path, "timezone", "Expected timeZone: Asia/Ho_Chi_Minh")
        )

    # 3. Schedule must be 0 2,8,14,20 * * *
    if not re.search(r'schedule:\s*"?0\s+2,8,14,20\s+\*\s+\*\s+\*"?', text):
        violations.append(
            Violation(
                manifest_path,
                "schedule",
                "Expected schedule: 0 2,8,14,20 * * * (matching 6-hour off-peak slots)",
            )
        )

    # 4. Security context must enforce non-root and read-only rootfs
    if not re.search(r"runAsNonRoot:\s*true", text):
        violations.append(
            Violation(manifest_path, "security", "Expected runAsNonRoot: true")
        )
    if not re.search(r"readOnlyRootFilesystem:\s*true", text):
        violations.append(
            Violation(manifest_path, "security", "Expected readOnlyRootFilesystem: true")
        )
    if not re.search(r"drop:\s*-\s*ALL", text, re.DOTALL):
        violations.append(
            Violation(manifest_path, "security", "Expected capabilities drop: [ALL]")
        )

    # 5. Limits must be present
    if not re.search(r"limits:\s*cpu:", text) or not re.search(r"memory:", text):
        violations.append(
            Violation(manifest_path, "resources", "Expected resource requests and limits")
        )

    # 6. activeDeadlineSeconds must be present
    if not re.search(r"activeDeadlineSeconds:\s*\d+", text):
        violations.append(
            Violation(manifest_path, "deadline", "Expected activeDeadlineSeconds to prevent runaway jobs")
        )

    # 7. Disallow bare ':latest' image tag
    for match in re.finditer(r"image:\s*([^\s]+)", text):
        img = match.group(1).strip("\"'")
        if img.endswith(":latest"):
            violations.append(
                Violation(manifest_path, "tag", f"Prohibited bare ':latest' tag in production manifest: {img}")
            )

    return violations


def check_systemd_units(service_path: Path, timer_path: Path) -> list[Violation]:
    """Validate invariants for systemd timer and service units."""
    violations: list[Violation] = []

    # Service checks
    if not service_path.exists():
        violations.append(Violation(service_path, "existence", "Service unit does not exist"))
    else:
        text = service_path.read_text(encoding="utf-8")
        if not re.search(r"Type\s*=\s*oneshot", text):
            violations.append(Violation(service_path, "type", "Expected Type=oneshot"))
        if not re.search(r"NoNewPrivileges\s*=\s*true", text):
            violations.append(Violation(service_path, "security", "Expected NoNewPrivileges=true"))
        if not re.search(r"ProtectSystem\s*=\s*strict", text):
            violations.append(Violation(service_path, "security", "Expected ProtectSystem=strict"))
        if not re.search(r"TimeoutStartSec\s*=\s*\d+", text):
            violations.append(Violation(service_path, "timeout", "Expected TimeoutStartSec"))

    # Timer checks
    if not timer_path.exists():
        violations.append(Violation(timer_path, "existence", "Timer unit does not exist"))
    else:
        text = timer_path.read_text(encoding="utf-8")
        if not re.search(r"Persistent\s*=\s*true", text):
            violations.append(Violation(timer_path, "persistence", "Expected Persistent=true"))
        # Verify 6-hour calendar schedule agreement with CronJob slots (02,08,14,20:00:00 Asia/Ho_Chi_Minh)
        if not re.search(r"OnCalendar\s*=\s*\*-?\*-?\*\s+02,08,14,20:00:00\s+Asia/Ho_Chi_Minh", text):
            violations.append(
                Violation(
                    timer_path,
                    "schedule_agreement",
                    "Expected OnCalendar=*-*-* 02,08,14,20:00:00 Asia/Ho_Chi_Minh (matching CronJob slots and timezone)",
                )
            )

    return violations


def check_compose_sidecar(compose_path: Path) -> list[Violation]:
    """Validate invariants for warded-forecast-cron in docker-compose.stg.yml."""
    violations: list[Violation] = []
    if not compose_path.exists():
        return [Violation(compose_path, "existence", "Compose file does not exist")]

    text = compose_path.read_text(encoding="utf-8")
    block = _service_block(text, "warded-forecast-cron")
    if block is None:
        return [Violation(compose_path, "service", "Service 'warded-forecast-cron' not defined in compose")]

    # 1. Profiles must include forecast-refit
    if not re.search(r"profiles:\s*\[[^\]]*forecast-refit", block):
        violations.append(
            Violation(compose_path, "profiles", "Expected profiles to include 'forecast-refit'")
        )

    # 2. Non-root user
    if not re.search(r'user:\s*"10001:10001"', block):
        violations.append(
            Violation(compose_path, "user", 'Expected non-root user: "10001:10001"')
        )

    # 3. Read-only rootfs
    if not re.search(r"read_only:\s*true", block):
        violations.append(
            Violation(compose_path, "security", "Expected read_only: true")
        )

    # 4. Healthcheck must be configured
    if not re.search(r"healthcheck:\s*\n", block):
        violations.append(
            Violation(compose_path, "healthcheck", "Expected healthcheck definition")
        )

    return violations


def _service_block(text: str, service: str) -> str | None:
    """Return the indented body of one compose service, or None if absent."""
    match = re.search(
        rf"^  {re.escape(service)}:\s*\n((?:[ \t]+[^\n]*\n?)+)",
        text,
        re.MULTILINE,
    )
    return match.group(1) if match else None


def _depends_on_init_volume(block: str) -> bool:
    """True when a service gates on the init container completing successfully."""
    if not re.search(r"depends_on:\s*\n", block):
        return False
    tail = block.split("depends_on:", 1)[1]
    if "warded-init-vol:" not in tail:
        return False
    init_section = tail.split("warded-init-vol:", 1)[1]
    return bool(re.search(r"condition:\s*service_completed_successfully", init_section))


def check_compose_ha_stack(compose_path: Path) -> list[Violation]:
    """Validate the volume-ownership init container and its dependents."""
    violations: list[Violation] = []
    if not compose_path.exists():
        return [Violation(compose_path, "existence", "Compose file does not exist")]

    text = compose_path.read_text(encoding="utf-8")

    # --- warded-init-vol --------------------------------------------------
    init_vol = _service_block(text, "warded-init-vol")
    if init_vol is None:
        violations.append(
            Violation(compose_path, "service", "Service 'warded-init-vol' not defined in compose")
        )
    else:
        if not re.search(r"^\s+image:\s*alpine:3\.20\s*$", init_vol, re.MULTILINE):
            violations.append(
                Violation(compose_path, "init_vol_image", "Expected image alpine:3.20")
            )
        if not re.search(r'user:\s*"0:0"', init_vol):
            violations.append(
                Violation(compose_path, "init_vol_user", 'Expected root user "0:0" for chown')
            )
        if not re.search(r'restart:\s*"?no"?', init_vol):
            violations.append(
                Violation(compose_path, "init_vol_restart", 'Expected restart: "no" (run once)')
            )
        if not re.search(r"warded-stg-data:/app/data\s*$", init_vol, re.MULTILINE):
            violations.append(
                Violation(
                    compose_path,
                    "init_vol_volume",
                    "Expected warded-stg-data mounted at /app/data",
                )
            )
        for required in (
            "/app/data/locks",
            "/app/data/forecast",
            "/app/data/proofs",
            "chown -R 10001:10001 /app/data",
            "chmod -R 775 /app/data",
        ):
            if required not in init_vol:
                violations.append(
                    Violation(
                        compose_path,
                        "init_vol_command",
                        f"Expected the init command to include: {required}",
                    )
                )

    # --- gating on the init container ------------------------------------
    for service in ("warded-backend", "warded-forecast-cron"):
        block = _service_block(text, service)
        if block is None:
            violations.append(
                Violation(compose_path, "service", f"Service '{service}' not defined in compose")
            )
        elif not _depends_on_init_volume(block):
            violations.append(
                Violation(
                    compose_path,
                    "depends_on",
                    f"'{service}' must wait for warded-init-vol: service_completed_successfully",
                )
            )
    return violations


def check_backup_sidecar(compose_path: Path) -> list[Violation]:
    """Validate the daily SQLite backup sidecar in docker-compose.stg.yml.

    These are the invariants that decide whether tonight's backup reaches R2 at
    all: the service exists, it is resource-bounded on a Celeron-class host, it
    can read the staging volume, it stages the snapshot on a real volume rather
    than RAM, and it targets the homelab-backup bucket rather than the
    per-environment proofs bucket. The streaming-replication sidecar it replaced
    must stay gone.
    """
    violations: list[Violation] = []
    if not compose_path.exists():
        return [Violation(compose_path, "existence", "Compose file does not exist")]

    text = compose_path.read_text(encoding="utf-8")
    block = _service_block(text, "warded-backup")
    if block is None:
        return [
            Violation(compose_path, "backup_service", "Service 'warded-backup' not defined in compose")
        ]

    if _service_block(text, "warded-litestream") is not None:
        violations.append(
            Violation(
                compose_path,
                "backup_legacy_replication",
                "Service 'warded-litestream' must not exist: it was replaced by 'warded-backup'",
            )
        )

    # The package install needs root and a writable rootfs; a read_only rootfs
    # would make `apk add sqlite rclone tzdata` fail on every start.
    if re.search(r"read_only:\s*true", block):
        violations.append(
            Violation(
                compose_path,
                "backup_rootfs",
                "Prohibited read_only: true: the apk install needs a writable rootfs",
            )
        )

    if not re.search(r"limits:\s*\n\s+cpus:", block) or not re.search(r"memory:", block):
        violations.append(
            Violation(
                compose_path,
                "backup_limits",
                "Expected deploy.resources.limits with cpus and memory (cgroup limits are mandatory)",
            )
        )

    if not re.search(r"^\s+-\s+warded-stg-data:/app/data\s*$", block, re.MULTILINE):
        violations.append(
            Violation(
                compose_path,
                "backup_volume",
                "Expected warded-stg-data mounted at /app/data so the database can be snapshotted",
            )
        )

    if not re.search(r"^\s+-\s+warded-backup-staging:/backup\s*$", block, re.MULTILINE):
        violations.append(
            Violation(
                compose_path,
                "backup_staging_volume",
                "Expected warded-backup-staging mounted at /backup: the database is too large "
                "for a tmpfs and tmpfs memory counts against the container limit",
            )
        )

    tmpfs_section = re.search(r"^    tmpfs:\n((?:      -[^\n]*\n)+)", block, re.MULTILINE)
    if tmpfs_section:
        for entry in re.findall(r"^      -\s*([^\s:]+)", tmpfs_section.group(1), re.MULTILINE):
            if not entry.startswith("/tmp"):
                violations.append(
                    Violation(
                        compose_path,
                        "backup_tmpfs",
                        f"Prohibited tmpfs mount {entry}: the snapshot must live on "
                        "warded-backup-staging, not in RAM",
                    )
                )

    if not re.search(r"/backup/camera-view\.db", block):
        violations.append(
            Violation(
                compose_path,
                "backup_snapshot_path",
                "Expected the sqlite3 .backup snapshot to be written to /backup/camera-view.db",
            )
        )

    if not re.search(r"homelab-backup", block):
        violations.append(
            Violation(
                compose_path,
                "backup_bucket",
                "Expected the backup target bucket to default to homelab-backup",
            )
        )

    if not re.search(r"\.backup", block):
        violations.append(
            Violation(
                compose_path,
                "backup_command",
                "Expected 'sqlite3 .backup' (WAL-safe snapshot API), not a raw file copy",
            )
        )

    if not _depends_on_init_volume(block):
        violations.append(
            Violation(
                compose_path,
                "backup_depends_on",
                "Expected depends_on warded-init-vol: service_completed_successfully",
            )
        )

    if not re.search(r"cap_drop:\s*\n\s*-\s*ALL", block):
        violations.append(
            Violation(compose_path, "backup_security", "Expected capabilities drop: [ALL]")
        )
    if not re.search(r"no-new-privileges:true", block):
        violations.append(
            Violation(compose_path, "backup_security", "Expected security_opt no-new-privileges:true")
        )
    if re.search(r"^\s+ports:\s*$", block, re.MULTILINE):
        violations.append(
            Violation(compose_path, "backup_ports", "The backup sidecar must not publish host ports")
        )
    if re.search(r"(AKIA|ASIA)[0-9A-Z]{16}", block):
        violations.append(
            Violation(compose_path, "hardcoded_secret", "AWS-style access key literal detected")
        )
    return violations


def check_cv_compose(compose_path: Path) -> list[Violation]:
    """Validate docker-compose.cv.yml reaches the engine over core-network only.

    The CV engine moved next to the stack that calls it, so a published host
    port would be dead surface: consumers address `warded-cv-engine:8000` on the
    shared external network.
    """
    violations: list[Violation] = []
    if not compose_path.exists():
        return [Violation(compose_path, "existence", "Compose file does not exist")]

    text = compose_path.read_text(encoding="utf-8")

    if re.search(r"^\s+ports:\s*$", text, re.MULTILINE):
        violations.append(
            Violation(
                compose_path,
                "cv_ports",
                "The CV compose must not publish host ports; the engine is reached by service name",
            )
        )

    if not re.search(r"^\s+-\s+core-network\s*$", text, re.MULTILINE):
        violations.append(
            Violation(compose_path, "cv_network", "Expected the service to join core-network")
        )

    if not re.search(
        r"^networks:\s*\n\s+core-network:\s*\n\s+name:\s*\$\{DOCKER_NETWORK:-core-network\}\s*\n\s+external:\s*true\s*$",
        text,
        re.MULTILINE,
    ):
        violations.append(
            Violation(
                compose_path,
                "cv_network",
                "Expected a file-level external core-network keyed on ${DOCKER_NETWORK:-core-network}",
            )
        )

    return violations


def check_all(repo_root: Path = REPO_ROOT) -> list[Violation]:
    """Check all deployment manifests under repo_root."""
    violations: list[Violation] = []
    cronjob_path = repo_root / "deploy" / "k8s" / "forecast-refit-cronjob.yaml"
    service_path = repo_root / "deploy" / "systemd" / "projectkv-forecast-refit.service"
    timer_path = repo_root / "deploy" / "systemd" / "projectkv-forecast-refit.timer"
    compose_path = repo_root / "docker-compose.stg.yml"
    cv_compose_path = repo_root / "docker-compose.cv.yml"

    violations.extend(check_k8s_cronjob(cronjob_path))
    violations.extend(check_systemd_units(service_path, timer_path))
    violations.extend(check_compose_sidecar(compose_path))
    violations.extend(check_compose_ha_stack(compose_path))
    violations.extend(check_backup_sidecar(compose_path))
    violations.extend(check_cv_compose(cv_compose_path))
    return violations


def main() -> int:
    parser = argparse.ArgumentParser(description="Static deployment manifest validator")
    parser.add_argument("--repo-root", type=Path, default=REPO_ROOT, help="Path to repository root")
    args = parser.parse_args()

    violations = check_all(args.repo_root)
    if violations:
        print(f"DEPLOY LINT FAILED: {len(violations)} violation(s) found:\n", file=sys.stderr)
        for v in violations:
            rel = v.file_path.relative_to(args.repo_root) if v.file_path.is_relative_to(args.repo_root) else v.file_path
            print(f"  - [{v.rule}] {rel}: {v.message}", file=sys.stderr)
        return 1

    print("DEPLOY LINT PASSED: All deployment manifests satisfy security and scheduling invariants.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
