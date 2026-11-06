# Lesson 71: Directional Runtime Ingestion & Worst-Direction Pinning

## Context
In Sprint 9 (S9-T7 / Issue #309), directional roadway polygons were introduced to ground-truth labeling and export sidecars (`roadway_regions.json`). However, downstream runtime consumers (`cv-engine`, `internal/hotset`, `internal/cvclient`, `internal/forecast`) did not consume them (Sprint 9 Finding F2). Frame-wide vehicle saturation averaged jammed carriageways with free-flowing ones, effectively masking critical corridor bottlenecks.

In Sprint 10 (S10-T3 / Issue #374 / ADR-012 D1), we built the complete directional runtime pipeline:
1. `tools/eval/build_roadway_registry.py` with `--verify` enforcing strict capacity share constraints ($\sum \text{share} = 1.0 \pm 0.01$).
2. Leaf package `internal/camera` loading and validating per-camera geometry into `camera.Camera`.
3. `services/cv-engine/server.py` ingesting multipart frame and metadata with point-in-polygon vehicle assignment and rasterized water ratio reconciliation.
4. `internal/cvclient` and `internal/hotset` computing directional saturation indices ($SI_{dir} = PCU_{dir} / (BaseCapacity \times share)$) and pinning overall camera severity to the worst direction.
5. Migration `0008_hotset_directional_history.sql` expanding `hotset_verdict_history` with `direction` and `region_id`.

## The Problem: Jam Dilution and Superficial Shortcuts
When measuring traffic across dual-carriageway corridors (e.g. Vo Van Kiet, Dien Bien Phu, Mai Chi Tho), morning and evening peak hours exhibit extreme flow asymmetry: inbound traffic is gridlocked ($SI = 2.75$, `CONGESTED`) while outbound traffic flows freely ($SI = 0.08$, `CLEAR`).
* **Frame-wide averaging dilution**: Calculating a single frame-wide saturation index averages $2.75$ and $0.08$ to $1.41$, or worse, when base capacity is estimated for the whole road, the free carriageway pulls the saturation index down into `MODERATE`, masking the severe jam from automated alerts and routing engines.
* **Superficial shortcuts ("Đừng sợ tốn sức")**: Attempting shortcuts—such as an arbitrary 50/50 division without geometric polygons, or skipping multipart ingestion to avoid parser work—leads to brittle assumptions and silent failure modes. In complex systems, the cost of fixing half-measures and superficial band-aids is far higher than designing a sound, root-cause architecture up front.

## Key Principles & Architectural Guardrails

### 1. Worst-Direction Pinning Prevents Bottleneck Dilution
Overall camera severity must always be pinned to the worst direction:
$$\text{Severity}_{\text{overall}} = \max_{\text{dir} \in \{a, b\}} \left( \text{Severity}_{\text{dir}} \right)$$
If either direction is `CONGESTED`, the camera reports `CONGESTED`. This ensures downstream alerting (`internal/notify`) and emergency dispatch detect directional bottlenecks immediately.

### 2. Strict Capacity Share Validation & Clean Fallbacks
Directional capacity shares must strictly sum to $1.0 \pm 0.01$:
* The registry loader in `internal/camera` validates every file at startup. If $\sum \text{share} < 0.99$ or $> 1.01$, registration is rejected with a warning.
* Unregistered cameras cleanly report `direction_coverage: "none"` and fall back to frame-wide calculation without throwing panics or failing boot. There is no implicit 50/50 split.

### 3. Additive Database Migrations with Dual Writing
Migration 0008 adds `direction` and `region_id` to `hotset_verdict_history` and re-indexes on `(camera_id, direction, observed_at)`.
* Every observation writes one frame-wide row (`direction = ''`) and, if directional geometry exists, one row per direction (`direction = 'a'`, `direction = 'b'`).
* Time-series consumers (such as `extract_features.py` and `forecast.Service`) query `WHERE direction = ''`, preventing duplicate timestamp collisions and maintaining 100% backward compatibility.

### 4. Public Repository Safety Discipline
Because the repository is open source and publicly visible, code, fixtures, and documentation must never contain real infrastructure credentials, internal IP addresses, access tokens, or private endpoints. All verification tools (`scripts/check-public-safe.sh`) must run before commit.
