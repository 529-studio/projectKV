# Sprint 15 Completion, Technical Verification & Reality-Check Report

**Project:** ProjectKV (Ho Chi Minh City Real-Time Urban Traffic & Flood Monitoring)  
**System Name:** Warded (`warded-backend`, `warded-frontend`, `warded-cv-engine`)  
**Document Version:** 1.0.0  
**Date:** 2026-11-17  
**Commit Range:** `c2a2337..2e2fe13` (7 merged PRs, 42 files, +4,225 / -497 lines)

**Primary References & Ground Truth:**
- [`docs/HANDOFF-s14.md`](../HANDOFF-s14.md) and [`docs/HANDOFF-s15.md`](../HANDOFF-s15.md)
- Engineering Lessons: [`docs/lessons/INDEX.md`](../lessons/INDEX.md) (Lessons 107–113 / Issues #475–#484)
- GitHub PR Ledger: PRs #481, #482, #483, #485, #486, #487, #488, #489

---

## Navigation Aid (Table of Contents)

1. [Executive Summary & Quantitative Scorecard](#1-executive-summary--quantitative-scorecard)
2. [Delivered Capabilities (Wave 1 & Wave 2)](#2-delivered-capabilities-wave-1--wave-2)
3. [Field Hardening, Security & Infrastructure Deploys](#3-field-hardening-security--infrastructure-deploys)
4. [Empirical Verification Evidence & Production Proofs](#4-empirical-verification-evidence--production-proofs)
5. [Engineering Lessons Learned (L107–L113)](#5-engineering-lessons-learned-l107l113)
6. [Settled Architecture Decisions & Strategic Bridge to Sprint 16](#6-settled-architecture-decisions--strategic-bridge-to-sprint-16)

---

## 1. Executive Summary & Quantitative Scorecard

Sprint 15 executed all planned initiatives across two waves with 100% completion. The sprint
delivered end-to-end commuter route indicators with arterial corridor status APIs, tactile mobile
touch ergonomics with dynamic viewport geometry, proactive Telegram corridor flooding notifications,
adaptive CV inference load shedding with 24-hour soak profiling, weekly automated SQLite disaster
recovery cron drills with universal webhook alerts, and resilient address sanitization for street queries.
Furthermore, the sprint formalized **AGENTS.md Rule 14** mandating full green CI and zero unresolved
GitHub Advanced Security findings for release eligibility.

### Quantitative Sprint Scorecard

| Target Dimension | Baseline (Sprint 14 End) | Delivered (Sprint 15 End) | Status |
| :--- | :--- | :--- | :--- |
| **Arterial Corridors** | Camera-by-camera isolated pins | Unified corridor status (`/api/corridors`), worst-severity bubble-up, GeoJSON polylines | **Delivered** |
| **Mobile Web UX** | Fixed 100vh overflow, small touch targets | Dynamic 100dvh safe-area, >=48px touch ergonomics, tactile drawer snap states | **Delivered** |
| **Search Usability** | Street names only; house numbers fail | Resilient address sanitizer stripping alleys/house numbers ("34/55/67") | **Delivered** |
| **Corridor Alerts** | Global / ward-level Telegram only | Corridor-specific flood subscriptions, proactive healthcheck alerts | **Delivered** |
| **CV Engine Stability** | Untracked queue drops under burst | Adaptive queue shedding (>60s drop with 429), 320px downscale, 24h soak verified | **Delivered** |
| **Disaster Recovery** | Manual drill execution | Automated weekly systemd timer (Sun 03:00 UTC), universal webhook alert dispatcher | **Delivered** |
| **Security Scanning** | Ad-hoc manual CodeQL reviews | AGENTS.md Rule 14 release gate; zero unresolved CodeQL / secret scan alerts | **Delivered** |

---

## 2. Delivered Capabilities (Wave 1 & Wave 2)

### S15-T1: Arterial Corridor Hazard & Route GeoJSON (PR #482 / Issue #475)
- Implemented `GET /api/corridors` and `GET /api/corridors/:key` in `services/go-server/internal/corridor`.
- Aggregated camera congestion and flood levels along arterial corridors (Cộng Hòa, Điện Biên Phủ, Võ Văn Kiệt, Trường Chinh).
- Embedded GeoJSON linestrings for corridor route polylines with spatial camera indexing.

### S15-T2: Commuter Route Polyline & Arterial Status Card (PR #486 / Issue #476)
- Built interactive commuter route overview drawer displaying corridor saturation and flood warnings.
- Rendered high-contrast arterial polylines on Leaflet map with click-to-focus camera inspection.
- Added bilingual status cards (`vi`/`en`) with live observation timestamps.

### S15-T3: Mobile Touch Ergonomics & Viewport Height Fix (PR #481 / Issue #477)
- Solved mobile address bar occlusion using dynamic CSS viewport units (`100dvh`).
- Enforced minimum 48px tactile touch targets for mobile buttons, search bars, and tabs.
- Optimized drawer touch drag gestures with smooth snapping between collapsed and expanded states.

### S15-T4: Telegram Corridor Hazard Alerts & Healthcheck Alerting (PR #487 / Issue #478)
- Added database migration `0013_add_corridor_subscription.sql` supporting corridor targets.
- Dispatched structured monochromatic corridor alerts via Telegram Bot API with 15-minute debouncing.
- Implemented proactive healthcheck alert worker alerting operators upon consecutive feed drops.

### S15-T5: Adaptive CV Engine Load Shedding & 24h Soak Test (PR #488 / Issue #479)
- Replaced hardcoded curb references with dynamic curb geometry in `services/cv-engine/server.py`.
- Built `InferenceQueueManager`: dropped stale frames (>60s) with 429 backpressure, prioritized arterial corridors, and dynamically degraded resolution to 320px under elevated concurrency.
- Created 24-hour continuous soak harness (`deploy/benchmark/soak_profile.py`); generated verified evidence `docs/reports/s15_cv_soak_evidence.json` (zero crashes, flat RSS memory slope).

### S15-T6: Weekly Automated Litestream Restore Drill Cron & Webhook (PR #489 / Issue #480)
- Enhanced `tools/deploy/restore_drill.py` with universal webhook alerting (`--webhook-url`, `--chat-id`).
- Packaged systemd unit definitions (`projectkv-restore-drill.{service,timer}`) running unprivileged under `projectkv` UID every Sunday at 03:00 UTC.
- Enforced fail-safe retry dispatch with exponential backoff on transient errors and immediate 4xx termination.

### S15-T7: Resilient Address Search Sanitizer & House Number Normalizer (PR #485 / Issue #484)
- Added robust regex pre-processing in search input handling complex Vietnamese house numbers ("34/55/67 Cách Mạng Tháng Tám").
- Stripped alley, lane, and number prefixes to match underlying road catalog without network round-trips.

---

## 3. Field Hardening, Security & Infrastructure Deploys

1. **AGENTS.md Rule 14 (CI & Security Scanning Gate)**: Formalized mandatory policy that no PR or task may be considered complete until all CI checks pass and all GitHub Advanced Security findings are fixed at root cause.
2. **CodeQL URL Sanitization Hardening**: Resolved CodeQL security alert `py/incomplete-url-substring-sanitization` in `restore_drill.py` by replacing substring matching with exact parsed hostname verification (`hostname == "api.telegram.org"`).
3. **CI Runner Concurrency Hardening**: Resolved runner thread starvation in `services/cv-engine/test_server.py` by catching all socket/transport errors and extending join timeouts for 2-vCPU virtualized runners.

---

## 4. Empirical Verification Evidence & Production Proofs

All deliverables were validated with rigorous empirical evidence per Rule 10:
- **CV Engine 24h Soak Artifact**: `docs/reports/s15_cv_soak_evidence.json` qualifies against `tools/ci/check_evidence_integrity.py` (864,000 requests, 10.01 FPS, 0 crashes, peak RSS 194.2 MB, slope 0.012 MB/hr, p95 17.5 ms).
- **Automated Restore Drill Webhook Proofs**: Live local HTTP listener verified `status == "SUCCESS"` (RTO 0.006s, RPO 0.0s, integrity_check ok); simulated budget breach verified `status == "FAILURE"` exit code 1 with failure details.
- **Systemd Unit Syntax**: Unit configuration parsed and validated with configparser tests.
- **Automated Test Suites**:
  - Go backend: `make test-go` passed 100%.
  - CV engine: 129 tests passed in 1.76s.
  - Deploy tools: 47 tests passed in 11.3s.
  - Docs lint: 0 new violations.

---

## 5. Engineering Lessons Learned (L107–L113)

- **L107 (Issue #475)**: Arterial Corridor Hazard Aggregation & In-Memory Polyline Caching
- **L108 (Issue #476)**: Commuter Route Polylines and Map Click Deconfliction
- **L109 (Issue #477)**: Dynamic Viewport Units (`100dvh`) and Mobile Kiosk Ergonomics
- **L110 (Issue #478)**: Telegram Corridor Flooding Subscriptions and Proactive Health Alerts
- **L111 (Issue #479)**: Adaptive Inference Queue Load Shedding and 24h Soak Memory Profiling
- **L112 (Issue #480)**: Automated Disaster Recovery Drills, Webhook Alerting, and Idempotent Systemd Units
- **L113 (Issue #484)**: Resilient Address Search Sanitization for Complex Vietnamese House Numbers

---

## 6. Settled Architecture Decisions & Strategic Bridge to Sprint 16

1. **Corridor Subscription Hierarchy**: Corridor-level alerts augment ward-level and camera-level subscriptions, allowing commuters to monitor entire daily commuting arteries without subscribing to dozens of individual cameras.
2. **Fail-Closed Load Shedding**: CV Engine drops stale frames (>60s) rather than backlogging inference queues, preserving real-time accuracy during high-traffic storm surges.
3. **Automated DR Verification Cadence**: Weekly Sunday 03:00 UTC cron restores prove Litestream replication durability continuously without human toil.
