# L70: Semantic Taxonomy Drift and Silent Notification Black Holes

## Context & Problem

In earlier sprints (Sprint 6–8), the flood alert subscription service allowed
users to subscribe to `target_type='zone'`, documented in migration `0006` as
administrative districts (`Quận / Huyện`). However, the camera catalog and
classifier assign cameras to municipal traffic zones (`camera.DefaultZones`,
such as `Khu vực Nội thành TP.HCM`, `Quận 7 - Nhà Bè`, `TP. Thủ Đức`).

When a flood was detected at a chokepoint, the alert dispatch logic queried
subscribers matching `alert.District` (which had been assigned `cam.Zone`).
Consequently, a user subscribing to `Quận 7` or `Quận Bình Thạnh` never matched
`Quận 7 - Nhà Bè` or `Khu vực Nội thành TP.HCM`. The system dropped alerts
silently without throwing an error, creating a notification black hole (Sprint 8 F6,
Sprint 9 F4).

## Root Cause

Two divergent mental models of geography co-existed in the codebase:
administrative districts (from human vernacular and outdated docstrings) and
operational traffic zones (from camera routing and catalog rules). Because the
API lacked strict contract-level validation and taxonomy introspection, free-form
district strings were saved without checking whether matching alerts could ever
fire.

## Solution

Sprint 10 Task 2 (#373, ADR-012 D4) reconciled the taxonomy into two strict,
authoritative tiers:

1. **Two-Tier Geography**:
   - **Operational Zone (`zone`)**: Municipal traffic groupings from
     `camera.DefaultZones` (`Bà Rịa - Vũng Tàu`, `Bình Dương`, `Cụm Cảng Cát Lái`,
     `Quận 7 - Nhà Bè`, `TP. Thủ Đức`, `Bình Tân - Bình Chánh`,
     `Q.12 - Hóc Môn - Củ Chi`, `Khu vực Nội thành TP.HCM`).
   - **Administrative Ward (`ward`)**: Phường/Xã/Thị trấn boundaries matching
     `cam.PhuongName` (`phuong_name` from 2025 boundary GeoJSON).
   - The non-existent "district" tier was removed from contracts and structs.
2. **Contract Validation & Taxonomy Introspection**:
   - `target_type` is restricted to `{all, chokepoint, zone, ward}`, returning
     HTTP 400 with the canonical list on invalid input.
   - Zone targets are normalized via `LegacyZoneAliases` (e.g. `Quận 7` ->
     `Quận 7 - Nhà Bè`, `Quận Bình Thạnh` -> `Khu vực Nội thành TP.HCM`).
   - New endpoint `GET /api/subscriptions/taxonomy` exposes canonical targets.
3. **Database Migration & Quarantine**:
   - Migration `0010_subscription_taxonomy.sql` normalizes existing district
     strings and quarantines invalid or orphan targets into
     `flood_subscriptions_legacy_0010` with reasons without dropping rows.
   - Enforces a `CHECK (target_type IN ('all', 'chokepoint', 'zone', 'ward'))`
     constraint on the active table.
4. **Backend Telemetry Alignment**:
   - Renamed `notify.FloodAlert.District` to `Zone` across the backend.
   - Verification CLI `cmd/subscriptions-audit` provides automated classification
     reports to guarantee zero non-canonical active targets.

## Takeaways

- **Validate against authoritative domains**: Never allow user subscription
  APIs to accept free-form targeting strings without validating them against
  authoritative backend catalog domains.
- **Taxonomy evolution requires migration & quarantine**: When reconciling
  divergent taxonomies, map known aliases automatically and isolate unresolvable
  records into explicit quarantine tables with audit reasons.
