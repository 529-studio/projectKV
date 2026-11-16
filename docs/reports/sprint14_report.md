# Sprint 14 Completion, Reality-Check Audit & Technical Verification Report

**Project:** ProjectKV (Ho Chi Minh City Real-Time Urban Traffic & Flood Monitoring)  
**System Name:** Warded (`warded-backend`, `warded-frontend`, `warded-cv-engine`)  
**Document Version:** 1.0.0  
**Date:** 2026-11-16  
**Commit range:** `9418d29..3249eab` (21 merges, 138 files, +12,369 / -619)

**Primary References & Ground Truth:**
- [`docs/HANDOFF-s13.md`](../HANDOFF-s13.md) and [`docs/HANDOFF-s14.md`](../HANDOFF-s14.md)
- Specifications: `docs/sprints/s14_t1_spec.md` through `s14_t4_spec.md`
- Engineering Lessons: [`docs/lessons/INDEX.md`](../lessons/INDEX.md) (Lessons 97–106 / Issues #441–#465)
- GitHub PR ledger: PRs #446–#473

---

## Navigation Aid (Table of Contents)

1. [Executive Summary & Quantitative Scorecard](#1-executive-summary--quantitative-scorecard)
2. [Delivered Capabilities (S14-T1 through S14-T4)](#2-delivered-capabilities-s14-t1-through-s14-t4)
3. [Field Hardening, UI Polish & Infrastructure Deploys](#3-field-hardening-ui-polish--infrastructure-deploys)
4. [Empirical Verification Evidence & Production Proofs](#4-empirical-verification-evidence--production-proofs)
5. [Engineering Lessons Learned (L97–L106)](#5-engineering-lessons-learned-l97l106)
6. [Settled Architecture Decisions & Strategic Bridge to Sprint 15](#6-settled-architecture-decisions--strategic-bridge-to-sprint-15)

---

## 1. Executive Summary & Quantitative Scorecard

Sprint 14 executed the four planned core tasks (S14-T1 through S14-T4) alongside vital field
hardening initiatives. The sprint achieved complete live inference health observability,
streaming SQLite Litestream replication to Cloudflare R2 with an automated verification drill,
a sunlight-readable dark map basemap with multi-scale macro clustering and worst-severity bubble-up,
forecast weather feature parity with a 5% minimum skill promotion gate, and verifiable persistent
memory via Lore integration.

### Quantitative Sprint Scorecard

| Target Dimension | Baseline (Sprint 13 End) | Delivered (Sprint 14 End) | Status |
| :--- | :--- | :--- | :--- |
| **CV Pipeline Coverage** | Unmeasured dropout rates; dark feeds | 24h sliding window coverage metrics; dead feed circuit breaker isolation | **Exceeded** |
| **Database Durability** | Unreplicated local SQLite | Sub-second Litestream WAL replication to R2; automated cron restore drill | **Delivered** |
| **Commuter Map Usability** | Overlapping individual pins; unreadable | Dual-layer clustering (macro zoom 10-12, street 13-18); worst-severity bubble-up | **Exceeded** |
| **Traffic Forecast Gating** | Unvalidated head promotion risk | Mandatory >=5% skill gate over baseline; Go-Python feature store parity | **Delivered** |
| **Radar False Virga** | 30 dBZ virga falsely blanked | Dual-threshold reflectivity gating (>=30 dBZ core immunity, NWP lag guard) | **Resolved** |
| **Knowledge Continuity** | Ephemeral LLM context | Lore verifiable ontology synced with OpenCode and Antigravity | **Delivered** |

---

## 2. Delivered Capabilities (S14-T1 through S14-T4)

### S14-T1: Live Inference Pipeline Darkness, 24h Coverage & Health Observability (PR #446 / Issue #441)
- Isolated upstream 404/5xx dead camera endpoints from tripping global circuit breakers.
- Implemented in-memory sliding 24-hour health buffer recording snapshot fetch latency, inference execution, and HTTP response distributions.
- Exposed `/api/health/coverage` and Prometheus telemetry metrics for uptime transparency.

### S14-T2: High-Availability SQLite Streaming Replication & Automated Restore Drill (PR #447 / Issue #442)
- Integrated Litestream replication daemon streaming WAL frames directly into Cloudflare R2 storage.
- Engineered automated restore drill script (`tools/ops/litestream_restore_drill.sh`) validating database integrity, schema migration compliance, and table non-emptiness.
- Gated container entrypoints with volume readiness checks to prevent zero-byte database split-brain.

### S14-T3: Commuter Map Sunlight Readability, Marker Clustering & Ubiquitous Hazard Depth (PR #448 / Issue #443)
- Migrated default tile layer to CARTO Dark Matter with high-contrast road hierarchy and offline fallback tiles.
- Built Leaflet spatial clustering algorithm bubbling up deepest water levels (in physical cm) and congestion states to parent badges.
- Enhanced bottom pinned dock and status drawer to provide unambiguous hazard telemetry.

### S14-T4: Forecast Weather Feature Parity, Skill Gating & Calibrated Head Promotion (PR #459 / Issue #444)
- Synchronized Go runtime feature engineering (`internal/forecast/features.go`) with Python training pipelines (`tools/forecast/extract_features.py`).
- Enforced strict 5% skill score promotion ratchet over persistence baseline on historical holdout datasets.
- Promoted calibrated ridge regression model heads with atomic file swapping and runtime checksum verification.

---

## 3. Field Hardening, UI Polish & Infrastructure Deploys

Beyond the core tasks, Sprint 14 aggressively eliminated field anomalies and hardened operations:

1. **Macro Clustering & Zoom Lifecycle (PR #455, #461 / Issues #454, #460)**:
   - Implemented zoom-adaptive macro clusters at metropolitan zoom levels (10–12) aggregating up to 201 cameras per cluster.
   - Fixed click zoom stall by leaping zoom by +2 and hooking cluster re-generation to Leaflet `zoomend`.
2. **SVG Forecast Dot Hover Jitter (PR #457 / Issue #456)**:
   - Anchored SVG scale transform origins to circle centers and expanded transparent hit targets.
3. **Radar Virga Gating & Badge Disambiguation (PR #466 / Issue #465)**:
   - Prevented NWP forecast lag from suppressing genuine convective cores (>=30 dBZ).
   - Standardized camera modal traffic badge to `THÔNG THOÁNG` matching `VERDICT_I18N.SEVERITY.CLEAR`.
4. **Verifiable Agent Memory via Lore (PR #464 / Issue #463)**:
   - Ingested git commit history into Lore knowledge ontology.
   - Configured OpenCode and Antigravity MCP servers for semantic rule and lesson recall.
5. **Deployment & CI Hardening (PRs #462, #468–#473)**:
   - Pinned immutable service tags, moved non-secret configuration into compose definitions, and routed Portainer deployments over Tailscale VPN.

---

## 4. Empirical Verification Evidence & Production Proofs

All deliverables were validated empirically against live staging and local development harnesses:

- **Litestream Restore Drill**: Validated against live R2 replica (`s14_t2_litestream_restore_drill.md`); restored 59 cameras, 11,905 frames, 0 corrupt blocks.
- **Map Clustering & Depth Rendering**: Validated with Playwright E2E browser tests under sunny/rainy simulations; captured zero marker collision.
- **Radar Virga Fix**: Live inspection on `https://warded.529studio.site` confirmed 4,608,422 non-zero radar canvas pixels restored.
- **Go Modulith Concurrency**: Server tests executed with dynamic test ports; zero race conditions or goroutine leaks under 100-worker concurrency.

---

## 5. Engineering Lessons Learned (L97–L106)

Sprint 14 contributed 10 critical lessons to [`docs/lessons/INDEX.md`](../lessons/INDEX.md):

- **L97 (Issue #441)**: Isolate dead feeds from breaker; export 24h health coverage metrics.
- **L98 (Issue #442)**: Backups are hypotheses until drill restores SQLite DB; verify non-empty tables.
- **L99 (Issue #443)**: Cluster markers bubble up deepest hazard; fallback tiles avoid dark map failure.
- **L100 (Issue #444)**: Mirror Go feature store in Python; require valid skill gating before promotion.
- **L101 (Issue #452)**: Repel overlapping clusters; dedicate separate row for secondary map toolbars.
- **L102 (Issue #454)**: Zoom-adaptive clustering grid; smart lens defaults; eliminate visual map clutter.
- **L103 (Issue #456)**: Anchor SVG transform origins; expand transparent hit target to prevent jitter.
- **L104 (Issue #460)**: Rebuild clusters on every zoom step in zoomend; leap +2 on macro cluster click.
- **L105 (Issue #463)**: Ingest git commits into Lore ontology; configure agy/opencode MCP with --no-rules.
- **L106 (Issue #465)**: Never suppress convective cores (>=30 dBZ); disambiguate traffic status to THÔNG THOÁNG.

---

## 6. Settled Architecture Decisions & Strategic Bridge to Sprint 15

- **ADR-014 (Streaming Replication Contract)**: SQLite databases must be replicated continuously via Litestream to object storage; weekly automated restore drills are mandatory for release qualification.
- **ADR-015 (Dual-Tier Macro Clustering)**: Web map rendering uses macro clustering at zoom <= 12 and micro density clustering at zoom >= 13 to maintain sub-16ms Leaflet frame budgets.
- **Rule 13 (Persistent Memory Protocol)**: Engineering lessons and git commits must be indexed into Lore for continuous agent knowledge continuity.

### Ready for Sprint 15
The staging system on `dev` (`3249eab`) is exceptionally stable, thoroughly tested, and documented.
