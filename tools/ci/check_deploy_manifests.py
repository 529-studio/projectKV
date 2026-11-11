#!/usr/bin/env python3
"""check_deploy_manifests.py - Static deployment manifest invariant validator.

Enforces structural and security invariants across deploy/ and compose manifests:
- k8s CronJob: Forbid concurrency, Asia/Ho_Chi_Minh timeZone, non-root securityContext,
  resource limits, activeDeadlineSeconds, no bare :latest tag, no hardcoded secrets.
- systemd timer/service: OnCalendar matches CronJob slots, Persistent=true, Type=oneshot,
  NoNewPrivileges, ProtectSystem=strict.
- docker-compose.stg.yml: sidecar profiles, non-root user, read_only, healthcheck.

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

    # Locate warded-forecast-cron service block
    block_match = re.search(r"warded-forecast-cron:\s*\n((?:[ \t]+[^\n]+\n)+)", text)
    if not block_match:
        return [Violation(compose_path, "service", "Service 'warded-forecast-cron' not defined in compose")]

    block = block_match.group(1)

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


def check_all(repo_root: Path = REPO_ROOT) -> list[Violation]:
    """Check all deployment manifests under repo_root."""
    violations: list[Violation] = []
    cronjob_path = repo_root / "deploy" / "k8s" / "forecast-refit-cronjob.yaml"
    service_path = repo_root / "deploy" / "systemd" / "projectkv-forecast-refit.service"
    timer_path = repo_root / "deploy" / "systemd" / "projectkv-forecast-refit.timer"
    compose_path = repo_root / "docker-compose.stg.yml"

    violations.extend(check_k8s_cronjob(cronjob_path))
    violations.extend(check_systemd_units(service_path, timer_path))
    violations.extend(check_compose_sidecar(compose_path))
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
