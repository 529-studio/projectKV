# L480 — Automated Disaster Recovery Drills, Webhook Alerting, and Idempotent Systemd Units

**Issue**: [#480](https://github.com/529-studio/projectKV/issues/480) · **Sprint**: 15 · **Date**: 2026-11-16

## 1. An untested backup is only a hypothesis

Disaster recovery plans often rot because drills rely on manual developer execution.
In Sprint 14, ProjectKV introduced Litestream replication streaming SQLite WAL
frames to Cloudflare R2 and delivered the `restore_drill.py` tool. While capable of
verifying integrity pragmas (`integrity_check`, `quick_check`, `foreign_key_check`)
and measuring Recovery Time Objective (RTO <= 60s) and Recovery Point Objective (RPO <= 10s),
drills remained ad-hoc.

**Guardrail**: Disaster recovery verification must run autonomously on a scheduled
cadence. Systemd timer units (`projectkv-restore-drill.timer`) schedule weekly
execution during off-peak traffic windows (Sunday 03:00 UTC). When a drill runs,
empirical metrics (wall-clock restore duration, replica lag, record count deltas)
must be measured directly against the restored file, never estimated or assumed.

## 2. Webhook alerting must be structured, universal, and fail-safe

Automated jobs that run headless must report both victories and failures directly
to operational communication channels (Telegram, Slack, Discord, or internal HTTP
receivers). A crash during drill execution or a corrupted database must never fail
silently in journal logs.

**Guardrail**: `restore_drill.py` accepts `--webhook-url` and `RESTORE_DRILL_WEBHOOK_URL`
(with optional Telegram chat ID via `--chat-id` or `RESTORE_DRILL_CHAT_ID`).
The payload is multi-format:
- For Telegram Bot API (`api.telegram.org`), it emits `{ "chat_id": ..., "text": ... }`
  formatted in monochromatic, zero-emoji plain text.
- For Slack and Discord, it populates `text` and `content` fields.
- For generic HTTP collectors, it provides structured top-level metadata (`event`,
  `status`, `rto`, `rpo`, `verifications`, `counts`, `failures`, and `runbook`).
- Delivery employs exponential backoff retries for transient HTTP errors (5xx, 429)
  and network timeouts, while failing fast without wasteful retries on 4xx client errors.
  Webhook failures never abort or hide drill results.

## 3. Sandboxed systemd privilege isolation

Backup restoration requires reading replication configurations and writing temporary
restoration artifacts into sandboxes. Granting root privileges or running drills inside
the production database directory creates severe operational blast radius risks.

**Guardrail**: The systemd service (`projectkv-restore-drill.service`) runs under the
unprivileged `projectkv:projectkv` user with systemd hardening directives:
`NoNewPrivileges=true`, `PrivateTmp=true`, `ProtectSystem=strict`, `ProtectHome=true`,
and explicit `ReadWritePaths` limited to `/opt/projectkv/data /run/projectkv /tmp`.
Any temporary database corruption or failed restore cannot escape into live system
paths or tamper with running daemon state.
