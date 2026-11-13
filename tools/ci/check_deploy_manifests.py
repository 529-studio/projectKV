#!/usr/bin/env python3
"""check_deploy_manifests.py - Static deployment manifest invariant validator.

Enforces structural and security invariants across deploy/ and compose manifests:
- k8s CronJob: Forbid concurrency, Asia/Ho_Chi_Minh timeZone, non-root securityContext,
  resource limits, activeDeadlineSeconds, no bare :latest tag, no hardcoded secrets.
- systemd timer/service: OnCalendar matches CronJob slots, Persistent=true, Type=oneshot,
  NoNewPrivileges, ProtectSystem=strict.
- docker-compose.stg.yml sidecars: forecast worker profile/user/read_only/healthcheck,
  the Litestream replication sidecar, and the volume-ownership init container.
- deploy/litestream/litestream.yml: WAL streaming target, 1s sync interval, env-only
  credentials, bounded retention.

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


def check_litestream_config(litestream_path: Path) -> list[Violation]:
    """Validate the Litestream replication target for camera-view.db.

    These are the invariants that decide whether a restore is possible at all:
    the database path inside the container, the replica object key, the sync
    interval that bounds RPO, and credentials that only ever come from the
    environment.
    """
    violations: list[Violation] = []
    if not litestream_path.exists():
        return [Violation(litestream_path, "existence", "Litestream config does not exist")]

    text = litestream_path.read_text(encoding="utf-8")

    if not re.search(r"^dbs:\s*$", text, re.MULTILINE):
        violations.append(Violation(litestream_path, "database", "Expected a top-level 'dbs:' block"))
    if not re.search(r"^\s*-\s*path:\s*/app/data/camera-view\.db\s*$", text, re.MULTILINE):
        violations.append(
            Violation(
                litestream_path,
                "database",
                "Expected database path /app/data/camera-view.db",
            )
        )

    replica_match = re.search(r"^\s*replicas:\s*$", text, re.MULTILINE)
    if not replica_match:
        return violations + [
            Violation(litestream_path, "replica", "Expected a 'replicas:' block")
        ]

    replica_lines = []
    replica_indent = len(replica_match.group(0)) - len(replica_match.group(0).lstrip())
    for line in text[replica_match.end() :].splitlines():
        if not line.strip():
            continue
        indent = len(line) - len(line.lstrip())
        if indent <= replica_indent:
            break
        replica_lines.append(line)
    replica = "\n".join(replica_lines)

    if not re.search(r"^\s*-\s*type:\s*s3\s*$", replica, re.MULTILINE):
        violations.append(Violation(litestream_path, "replica", "Expected an s3 replica type"))
    if not re.search(r"^\s*path:\s*db-replicas/camera-view\.db\s*$", replica, re.MULTILINE):
        violations.append(
            Violation(
                litestream_path,
                "replica_path",
                "Expected replica path db-replicas/camera-view.db",
            )
        )
    if not re.search(r"^\s*bucket:\s*\$\{R2_BUCKET_NAME\}\s*$", replica, re.MULTILINE):
        violations.append(
            Violation(litestream_path, "bucket", "Expected bucket: ${R2_BUCKET_NAME}")
        )
    if not re.search(r"^\s*endpoint:\s*\$\{R2_ENDPOINT\}\s*$", replica, re.MULTILINE):
        violations.append(
            Violation(litestream_path, "endpoint", "Expected endpoint: ${R2_ENDPOINT}")
        )
    if not re.search(r"^\s*sync-interval:\s*1s\s*$", replica, re.MULTILINE):
        violations.append(
            Violation(litestream_path, "sync_interval", "Expected sync-interval: 1s (bounded RPO)")
        )
    if not re.search(r"^\s*(snapshot-interval|retention):", replica, re.MULTILINE):
        violations.append(
            Violation(
                litestream_path,
                "retention",
                "Expected snapshot-interval/retention so replica history stays bounded",
            )
        )

    key_rules = (
        ("access-key-id", "LITESTREAM_ACCESS_KEY_ID", "R2_ACCESS_KEY_ID"),
        ("secret-access-key", "LITESTREAM_SECRET_ACCESS_KEY", "R2_SECRET_ACCESS_KEY"),
    )
    for field_name, primary, fallback in key_rules:
        match = re.search(rf"^\s*{field_name}:\s*(\S+)\s*$", replica, re.MULTILINE)
        if not match:
            violations.append(
                Violation(litestream_path, "credentials", f"Expected {field_name} from the environment")
            )
            continue
        value = match.group(1).strip("'\"")
        allowed = {f"${{{primary}}}", f"${{{fallback}}}"}
        if value not in allowed:
            violations.append(
                Violation(
                    litestream_path,
                    "hardcoded_secret",
                    f"{field_name} must reference {primary} or {fallback}, never a literal value",
                )
            )

    if re.search(r"(AKIA|ASIA)[0-9A-Z]{16}", text):
        violations.append(
            Violation(litestream_path, "hardcoded_secret", "AWS-style access key literal detected")
        )
    return violations


def check_compose_ha_stack(compose_path: Path) -> list[Violation]:
    """Validate the Litestream sidecar and volume-ownership init container."""
    violations: list[Violation] = []
    if not compose_path.exists():
        return [Violation(compose_path, "existence", "Compose file does not exist")]

    text = compose_path.read_text(encoding="utf-8")

    # --- warded-litestream ------------------------------------------------
    litestream = _service_block(text, "warded-litestream")
    if litestream is None:
        violations.append(
            Violation(compose_path, "service", "Service 'warded-litestream' not defined in compose")
        )
    else:
        if not re.search(r"^\s+image:\s*litestream/litestream:0\.3\.13\s*$", litestream, re.MULTILINE):
            violations.append(
                Violation(
                    compose_path,
                    "litestream_image",
                    "Expected pinned image litestream/litestream:0.3.13",
                )
            )
        if re.search(r"^\s+image:\s*\S+:latest\s*$", litestream, re.MULTILINE):
            violations.append(
                Violation(compose_path, "litestream_image", "Prohibited floating ':latest' tag")
            )
        if not re.search(r'user:\s*"10001:10001"', litestream):
            violations.append(
                Violation(compose_path, "litestream_user", 'Expected non-root user: "10001:10001"')
            )
        if not re.search(r"warded-stg-data:/app/data\s*$", litestream, re.MULTILINE):
            violations.append(
                Violation(
                    compose_path,
                    "litestream_volume",
                    "Expected warded-stg-data mounted at /app/data",
                )
            )
        if not re.search(
            r"\./deploy/litestream/litestream\.yml:/etc/litestream\.yml:ro\s*$",
            litestream,
            re.MULTILINE,
        ):
            violations.append(
                Violation(
                    compose_path,
                    "litestream_config",
                    "Expected read-only mount of deploy/litestream/litestream.yml",
                )
            )
        if not re.search(r"\breplicate\b", litestream) or not re.search(
            r"/etc/litestream\.yml", litestream
        ):
            violations.append(
                Violation(
                    compose_path,
                    "litestream_command",
                    "Expected command to run 'replicate' with /etc/litestream.yml",
                )
            )
        if not _depends_on_init_volume(litestream):
            violations.append(
                Violation(
                    compose_path,
                    "litestream_depends_on",
                    "Expected depends_on warded-init-vol: service_completed_successfully",
                )
            )
        if not re.search(r"read_only:\s*true", litestream):
            violations.append(
                Violation(compose_path, "litestream_security", "Expected read_only: true")
            )
        if not re.search(r"cap_drop:\s*\n\s*-\s*ALL", litestream):
            violations.append(
                Violation(compose_path, "litestream_security", "Expected capabilities drop: [ALL]")
            )
        if re.search(r"^\s+ports:\s*$", litestream, re.MULTILINE):
            violations.append(
                Violation(compose_path, "litestream_ports", "Litestream must not publish host ports")
            )
        for field_name in ("LITESTREAM_ACCESS_KEY_ID", "LITESTREAM_SECRET_ACCESS_KEY"):
            if not re.search(rf"\$\{{{field_name}:-", litestream):
                violations.append(
                    Violation(
                        compose_path,
                        "litestream_credentials",
                        f"Expected {field_name} injected from the environment with an explicit fallback",
                    )
                )
        if not re.search(r"-\s*R2_ENDPOINT=\$\{R2_ENDPOINT:\?", litestream):
            violations.append(
                Violation(
                    compose_path,
                    "litestream_endpoint",
                    "Expected R2_ENDPOINT to be required (:?) so a missing endpoint cannot "
                    "silently authenticate against AWS S3 with R2 credentials",
                )
            )
        if re.search(r"(AKIA|ASIA)[0-9A-Z]{16}", litestream):
            violations.append(
                Violation(compose_path, "hardcoded_secret", "AWS-style access key literal detected")
            )

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


def check_all(repo_root: Path = REPO_ROOT) -> list[Violation]:
    """Check all deployment manifests under repo_root."""
    violations: list[Violation] = []
    cronjob_path = repo_root / "deploy" / "k8s" / "forecast-refit-cronjob.yaml"
    service_path = repo_root / "deploy" / "systemd" / "projectkv-forecast-refit.service"
    timer_path = repo_root / "deploy" / "systemd" / "projectkv-forecast-refit.timer"
    compose_path = repo_root / "docker-compose.stg.yml"
    litestream_path = repo_root / "deploy" / "litestream" / "litestream.yml"

    violations.extend(check_k8s_cronjob(cronjob_path))
    violations.extend(check_systemd_units(service_path, timer_path))
    violations.extend(check_compose_sidecar(compose_path))
    violations.extend(check_litestream_config(litestream_path))
    violations.extend(check_compose_ha_stack(compose_path))
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
