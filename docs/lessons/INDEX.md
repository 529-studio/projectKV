# Lessons Index

One row per engineering lesson. The full text lives in the linked file; this index
is the only part an agent should read by default.

| # | Lesson | Mandatory guardrail | File |
| --- | --- | --- | --- |
| 1 | Passing Unit Tests is Never Sufficient | Simulate client workflows end-to-end; unit tests alone are insufficient. | [L01](L01-passing-unit-tests-is-never-sufficient-upstream-waf-header.md) |
| 2 | Sequence Inversion Risk & Downstream Divergence | Document inversions; sequence tasks strictly to avoid divergence. | [L02](L02-sequence-inversion-risk-invisible-downstream-divergence.md) |
| 3 | Completion Claims vs. Production Reality | Verify against live endpoints before claiming completion. | [L03](L03-completion-claims-vs-production-reality-gap.md) |
| 4 | Tooling & Environment Discrepancies | Support standalone binaries and containerized runtimes. | [L04](L04-tooling-environment-discrepancies-on-host-platforms.md) |
| 5 | Zero-Emoji Standard & Telemetry Streams | Zero emojis in UI and backend logs; use monochromatic SVGs and plaintext. | [L05](L05-zero-emoji-standard-telemetry-stream-corruption.md) |
| 6 | Single-Threaded HTTP vs Concurrent Go | Services receiving concurrent requests from Go workers must be thread-safe. | [L06](L06-single-threaded-http-servers-vs-concurrent-go-dispatchers.md) |
| 7 | Upstream Timestamp Mismatch & Mock Blind Spots | Normalize time series to common bins before joining; avoid mock blind spots. | [L07](L07-upstream-timestamp-resolution-mismatch-mock-contract-blind.md) |
| 8 | GitHub Actions Billing & Duplicate Jobs | Steps are free, jobs are not: fold logic into existing jobs as steps. | [L08](L08-github-actions-billing-ceiling-rounding-duplicate.md) |
| 9 | Competing Sources of Truth & Silent Drift | Never hardcode a second copy of state that can be derived. | [L09](L09-competing-sources-of-truth-silent-drift-ghost-camera.md) |
| 10 | CD Pipeline Configuration & Environment Propagation | Never rely solely on ad-hoc UI or host edits on Portainer. | [L10](L10-cd-pipeline-configuration-overwrite-environment-propagation.md) |
| 11 | Scheduled Pipelines vs Proxy Caches | Separate scheduled pipelines and proxy caches with explicit boundaries. | [L11](L11-decoupled-scheduled-pipelines-vs-on-demand-proxy-caches.md) |
| 12 | ML Model Classes vs Domain Formulas | Business scoring formulas must stay in sync with model classes. | [L12](L12-machine-learning-model-classes-vs-domain-business-formulas.md) |
| 13 | Ground-Truth Baselines vs. Spot-Checking | Isolate cameras with predictable, extreme baselines. | [L13](L13-ground-truth-smoke-test-baselines-vs-anecdotal-spot-checking.md) |
| 14 | Active Labeling vs. Passive Data Accumulation | Never fabricate labels or substitute mock frames to pass gates. | [L14](L14-active-labeling-effort-vs-passive-environmental-data.md) |
| 15 | Empirical Auditing & Cold-Start Capacity | Safety-critical evaluation requires authentic non-zero positives. | [L15](L15-empirical-ground-truth-auditing-cold-start-capacity.md) |
| 16 | Heterogeneous Distributed Microservices | Distribute microservices by hardware affinity and compute cost. | [L16](L16-heterogeneous-distributed-microservices-multi-endpoint.md) |
| 17 | CCTV Viewport Limits vs Link Capacity | Calibrate visual saturation floors from physical geometry. | [L17](L17-instantaneous-cctv-camera-viewport-physical-limits-vs.md) |
| 18 | High-Frequency Telemetry vs Cloud VLM | Retain self-hosted OpenVINO for continuous local inference. | [L18](L18-high-frequency-edge-telemetry-vs-cloud-vlm-economics-the.md) |
| 19 | Unit-Test Trap & Calibration Gating | Parameter and formula calibration must be empirical. | [L19](L19-the-unit-test-verification-trap-the-parameter-calibration.md) |
| 20 | VPN SPOF & Decoupled Compute | Local neural compute incurs $0 API cost and fails gracefully. | [L20](L20-vpn-spof-graceful-degradation-decoupling-cloud-daily.md) |
| 21 | S3-Compatible Cloudflare R2 Integration | Set UsePathStyle = true in AWS SDK Go v2 for Cloudflare R2. | [L21](L21-s3-compatible-cloudflare-r2-integration-path-style-routing.md) |
| 22 | Plaintext Credential Exposure Prevention | Zero secret fallbacks; load dynamically from .env or fail-fast. | [L22](L22-plaintext-credential-exposure-prevention-benchmark.md) |
| 23 | YOLO Activation Summation vs Missing NMS | Raw output tensors must not be summed without NMS. | [L23](L23-yolo-anchor-activation-summation-vs-object-counts-missing.md) |
| 24 | Decoupled Multimodal Hybrid Architecture | Retain local inference; guard cloud VLMs with strict quotas. | [L24](L24-decoupled-multimodal-hybrid-architecture-quota-guarded-dual.md) |
| 25 | CI Linter Schema Validation Flakiness | Set verify: false in golangci-lint-action to prevent schema flakiness. | [L25](L25-ci-linter-schema-validation-flakiness-toolchain-version.md) |
| 26 | GitHub Issue Auto-Closing Semantics | PR resolving an issue must start with Close #<number>. | [L26](L26-github-issue-auto-closing-keyword-semantics-vs-descriptive.md) |
| 27 | Centralized Collaborative Team Labeling | Dual-driver List(ctx, prefix) abstraction for local/cloud storage. | [L27](L27-centralized-collaborative-team-labeling-over-local.md) |
| 28 | Zero Unauthenticated Dev Endpoints | Endpoints managing labels/archives require dev token auth. | [L28](L28-zero-unauthenticated-dev-endpoints-fail-closed-developer.md) |
| 29 | Perception Fixes vs Scoring Hacks | Implement standard NMS with letterbox preprocessing. | [L29](L29-perception-layer-fixes-vs-scoring-hacks-empirical.md) |
| 30 | Capacity Floors vs Physical Geometry | Enrich camera catalog with physical corridor capacity. | [L30](L30-uniform-capacity-floors-vs-physical-corridor-geometry.md) |
| 31 | Environmental Triggers & Cloud Sync | Use real-time rain triggers to archive flood frames. | [L31](L31-active-environmental-opportunism-continuous-cloud-label.md) |
| 32 | Affordance Collision & Rate Limits | Brackets reserved for badges; cooldowns prevent multi-clicks. | [L32](L32-affordance-collision-client-side-rate-limit-cooldown-in.md) |
| 33 | Secret Sanitizer & Hysteresis Deadband | SecretSanitizer() wraps responses; hysteresis deadband. | [L33](L33-defense-in-depth-secret-sanitization-hysteresis-filtering.md) |
| 34 | Sound Solutions Precede Estimation | Never anchor effort to convenient shortcuts. | [L34](L34-architectural-psychological-priority-sound-solutions.md) |
| 35 | Holistic Repository Docs Maintenance | Audit and sync all affected docs across repo, not just logs. | [L35](L35-holistic-repository-knowledge-maintenance-vs-isolated.md) |
| 36 | Notification Modulith & Debouncing | internal/notify has zero sibling imports; 30-min debouncing. | [L36](L36-leaf-modulith-notification-infrastructure-30-minute-sliding.md) |
| 37 | Labeling Paradigms & Flood Detection | Distinguish eval from train sets; frame diff fails in glare. | [L37](L37-data-labeling-paradigms-why-frame-diff-fails-in-outdoor.md) |
| 38 | Arterial Saturation & Degradation | Stale metrics degrade to Unknown, never Clear. | [L38](L38-trust-safety-for-arterial-saturation-status-degradation.md) |
| 39 | Heuristic Ground-Truth & Taxonomy | Never generate ground-truth keys with heuristic rules. | [L39](L39-heuristic-ground-truth-pollution-vs-empirical-directional.md) |
| 40 | Harvester Architecture & Watcher | Edge neural compute runtime telemetry with rain cooldowns. | [L40](L40-autonomous-zero-vlm-harvester-architecture-dual-trigger.md) |
| 41 | Airborne Radar vs Ground Inundation | Calibrate RainViewer reflectivity to mm/h; fix alpha blindness. | [L41](L41-airborne-radar-reflectivity-dbz-vs-ground-surface.md) |
| 42 | Map Coordinate Zoning & Collision | System toolbars occupy dedicated row to avoid collisions. | [L42](L42-map-overlay-coordinate-zoning-hierarchical-collision.md) |
| 43 | Tile Server Zoom Ceiling & Canvas | Zoom 7 bounds tile grid; blank tile fallback avoided. | [L43](L43-upstream-tile-server-zoom-ceiling-silent-200-ok-placeholder.md) |
| 44 | Spatial Aggregator TTL Sync & Markers | Sync in-memory aggregator with store TTL to avoid stale markers. | [L44](L44-in-memory-spatial-aggregator-ttl-synchronization-multi-lens.md) |
| 45 | Crowdsourced Ground-Truth & Privacy | Internal/feedback has zero sibling imports; salted SHA-256 IP. | [L45](L45-crowdsourced-ground-truth-ingestion-ip-hashed-privacy-and.md) |
| 46 | Standalone Harvesting CLI & Offline Eval | Auto_harvest.py builds Draft-07 manifests from live feeds. | [L46](L46-standalone-automated-data-harvesting-offline-evaluation.md) |
| 47 | Web Labeler Active Learning Flywheel & Badging | Add LabelFilterStatus in /labeler for candidate approval workflow. | [L47](L47-web-labeler-active-learning-flywheel-provenance-badging-and.md) |
| 48 | Zero-VLM Self-Hosted Flood Segmentation | Validate FP16 IR models within 30s hotset budget. | [L48](L48-zero-vlm-self-hosted-flood-segmentation-openvino-model.md) |
| 49 | Object Enumeration vs S3 Delimiters | ListDirectories uses S3 delimiters to avoid 524 timeouts. | [L49](L49-unbounded-object-enumeration-vs-s3-delimiter-hierarchy.md) |
| 50 | Waterway & Bridge Sensor Hygiene | Whitelist roads, filter waterway nodes at ingest, API, UI. | [L50](L50-waterway-bridge-footing-sensor-hygiene-exclusion-of-non.md) |
| 51 | Partition Scalability & Client Pacing | Thread-safe labelCache with 60s TTL prevents redundant GETs. | [L51](L51-partition-object-scalability-client-pacing-in-active.md) |
| 52 | Go Select Break Fallacy & Cleanup | Labeled loop break in HarvestCameras ensures clean termination. | [L52](L52-go-select-break-concurrency-fallacy-heuristic-ground-truth.md) |
| 53 | Active Learning Queue & Dual-Storage | FilterSnapshots sources frames from local storage and R2. | [L53](L53-active-learning-queue-desync-dual-storage-frame-routing-in.md) |
| 56 | Off-Box Training Guardrails & Manifests | Host guard enforces AVX2 compute for training pipelines. | [L56](L56-off-box-training-guardrails-yolov8-segmentation-dataset.md) |
| 55 | Crowdsource Feedback & Leaf Modulith | Feedback has zero sibling imports; registered in main. | [L55](L55-the-crowdsource-feedback-black-hole-decoupled-leaf-modulith.md) |
| 57 | Bot Token Leaks, Sanitize & Parity | SanitizeBytes scrubs token before logging; parity test prevents drift. | [L57](L57-telegram-bot-token-leak-vectors-word-boundary-regex.md) |
| 58 | Parity Verification & Single-Source Alerts | Extract alert building into exported pure function notify.BuildFloodAlert. | [L58](L58-parity-verification-illusion-single-source-alert-generation.md) |
| 59 | Docs Cross-Reference Rot & Dead Links | Verify_doc_move.py: Moved docs proven byte-identical before reference rewrite. | [L59](L59-documentation-cross-reference-rot-links-to-never-merged-files.md) |
| 60 | Read Frequency Cost & Tiered Docs | Always-loaded <=36KB total, <=16KB/file (INDEX 20KB); on-demand <=25KB. | [L60](L60-read-frequency-is-the-cost-multiplier-documentation-tiering.md) |
| 61 | One Decision, One File: ADR Separation | ADR registers do not append successor decisions; use separate ADR files. | [L61](L61-one-decision-one-file-adr-register-that-embeds-its-successors.md) |
| 62 | Offline SAM Pre-Labeling & Storage | Host guard enforces AVX2 off-box; decoupled local path from R2 snapshot_key. | [L62](L62-offline-sam-prelabeling-provenance-and-dual-layer-storage-parity.md) |
| 63 | VLM Output Variance & Array Unmarshal | Gemini VLM emits arrays; cvclient falls back to []verdictJSON, extracts arr[0]. | [L63](L63-vlm-structured-output-variance-and-array-unmarshal-resilience.md) |
| 64 | First Segmentation Cycle: OpenVINO | water_puddle IoU must beat null baseline before deploy. | [L64](L64-first-segmentation-cycle-openvino-fp16-and-map-gating.md) |
| 65 | Roadway Polygons & Sidecars | roadway_regions.json sidecar per directional split. | [L65](L65-directional-roadway-polygons-sidecar-and-per-direction-ground-truth.md) |
| 66 | Self-Referential Tests & Fallbacks | Extract decisions into pure functions; announce fallbacks. | [L66](L66-self-referential-tests-and-silent-fallbacks-hide-the-live-path.md) |
| 67 | Upsert-Only State Cannot Feed Models | Append-only history with idempotent keys, UTC timestamps, stale exclusion. | [L67](L67-upsert-only-state-tables-cannot-feed-time-series-models.md) |
| 68 | Chronological Splits Leak at Boundaries | Purge training samples crossing cutoff; score on common set. | [L68](L68-chronological-split-horizon-boundary-leakage.md) |
| 69 | Hardcoded Fixtures & Cost of Shortcuts | Dynamic artifact loader; fail-closed path/size guards. | [L69](L69-hardcoded-prototype-fixtures-and-the-cost-of-shortcuts.md) |
| 70 | Semantic Drift & Notification Routing | Reconcile zone/ward taxonomy; alias normalization. | [L70](L70-canonical-zone-and-ward-subscription-taxonomy.md) |
| 71 | Directional Runtime Ingestion & Pinning | Polygon assignment in cv-engine; per-direction PCU. | [L71](L71-directional-runtime-ingestion-and-worst-direction-pinning.md) |
| 72 | Schema Assertions & Batch Retention | Assert migration readiness at boot; chunked retention. | [L72](L72-fail-fast-schema-assertions-and-nonblocking-batch-retention.md) |
| 73 | As-Of Weather Feature Store & Gating | Append-only weather; as-of join with 30m cutoff; ablation gate. | [L73](L73-asof-weather-feature-store-and-ablation-gating.md) |
| 74 | Frontend Forecast Gating & Visualization | Fail-closed checks (ok/stale), 95% CI band, stale dimming. | [L74](L74-frontend-forecast-contract-gating-and-uncertainty-visualization.md) |
| 75 | Segmentation Dataset v2 Group Split | Group split by camera/date; IoU >= 0.612, mAP >= 0.496. | [L75](L75-segmentation-dataset-v2-group-splitting-and-retraining-gates.md) |
| 76 | Inference Worker Co-Tenancy & Tuning | Recompute cgroup invariants; cron gate exits 1; clamp anchors. | [L76](L76-inference-worker-cotenancy-and-adaptive-loadshedding.md) |
| 77 | Evidence Integrity Gate & Synthetic Ban | No synthetic fixtures; authentic timestamps; entropy >= 3.0. | [L77](L77-evidence-integrity-gate-and-synthetic-prohibition.md) |
| 78 | Unattended Soak & Proof Ingestion | 24/7 harvester; 11,905 real-frame replay; process soak. | [L78](L78-unattended-soak-and-proof-archive-ingestion.md) |
| 79 | Roadway Registry Scale-Out & Geometry | 59-camera dual Hot Set; IoU < 0.05; capacity sum 1.0. | [L79](L79-directional-roadway-registry-scaleout.md) |
| 80 | Automated Forecast Refit & Promotion | Stdlib Ridge; chronological gate; atomic promotion. | [L80](L80-automated-forecast-refit-and-live-deployment.md) |
| 81 | Temporal Baseline Ratchet & Gate | No clock-dependent baselines; SUPERSEDED lifecycle. | [L81](L81-temporal-baseline-ratchet-and-approval-gate-concurrency.md) |
| 82 | Authentic Artifacts & Untrained Heads | Real export only; refuse untrained heads; test resolutions. | [L82](L82-authentic-model-artifacts-and-untrained-head-guard.md) |
| 83 | Native GitHub Protection & CI Gate | Verify native controls; migrate checks before delete. | [L83](L83-native-github-protection-and-ci-gate-consolidation.md) |
| 84 | Forecast Refit Daemon & File Locking | fcntl.flock daemon locking; state quarantine; row gating. | [L84](L84-forecast-refit-daemon-and-singleflight-locking.md) |
| 85 | Commuter Geolocation & Privacy Bands | 250m bands; clock-free comparator; zero coordinate leak. | [L85](L85-commuter-geolocation-banded-sorting.md) |
| 86 | RFC3339Nano Timestamps & Truncation | Fixed 9-digit layout; microsecond truncation; UTC invariants. | [L86](L86-rfc3339nano-9digit-timestamps-microsecond-truncation.md) |
| 87 | JWT & CORS Whitelisting for Live Feeds | Verify RS256 & aud via JWKS; whitelist CORS. | [L87](L87-cryptographic-jwt-verification-and-cors-origin-whitelisting.md) |
| 88 | Proxy OOM Bounds & Compression Order | Cap upstream reads via io.LimitReader; order gzip outer. | [L88](L88-proxy-oom-bounds-and-compression-middleware-ordering.md) |
| 89 | Unified SSE, Canvas & Memory Bounds | Single EventSource; ref mirror breaks reconnect loop; preferCanvas. | [L89](L89-unified-sse-stream-provider-canvas-rendering-and-visibility-throttling.md) |
| 90 | Physical Depth Hazard & Monotonic Drops | Avoidance is hazard; physical cm depth; latch drops; no fake levels. | [L90](L90-physical-depth-hazard-classification.md) |
| 91 | Staging Proxy Entrypoint Alignment | Bind proxy to upstream tunnel HTTP entrypoint; avoid phantom certresolvers. | [L91](L91-staging-reverse-proxy-entrypoint-and-tunnel-alignment.md) |
| 92 | Issue-Keyed Lessons & Webhook Routes | Name files after issue; fail-closed webhook without secret; rate-limit. | [402](402-telegram-bot-and-web-subscription.md) |
| 93 | Real Provenance vs Orphaned Labels | Exact timestamp & hash join; purge dead labels; retraining gate. | [411](411-dataset-v3-provenance-and-avx2-evidence.md) |
| 94 | SSE Sanitizer Buffering & Flusher | Streaming content bypasses body buffering; explicit Flusher check. | [420](420-sse-streaming-sanitizer-buffering.md) |
| 95 | Decompose Monolithic Go Composition Root | Factor main.go into adapters, jobs, routes; test go run. | [425](425-decompose-monolithic-cmd-server-main.md) |
| 96 | Moore Boundary Tracing & Jacob's Criterion | State tracking (pixel, backtrack); 8-connected merge; restores SAM yield. | [438](438-moore-boundary-tracing-jacobs-criterion.md) |
| 97 | Dead Camera Isolation & Health Metrics | Isolate 404/5xx feeds from breaker; export 24h health metrics. | [441](441-feed-error-breaker-isolation-and-health-observability.md) |
| 98 | Litestream R2 Replication & Restore Drill | Backups are hypotheses until drill restores SQLite DB; verify. | [442](442-litestream-r2-replication-and-restore-drill.md) |
| 99 | Map Clustering & Worst-Severity Bubble-Up | Cluster markers bubble up deepest hazard; fallback tiles avoid failure. | [443](443-commuter-map-clustering-and-hazard-depth.md) |
| 100 | Forecast Weather Parity & Skill Gating | Mirror Go feature store in Python; require valid skill gating. | [444](444-forecast-weather-parity-and-skill-gating.md) |
| 101 | Cluster Deconfliction & Toolbar Hierarchy | Repel overlapping clusters; dedicate separate row for toolbars. | [452](452-map-cluster-collision-and-toolbar-deconfliction.md) |
| 102 | Adaptive Macro Clustering & Smart Lens | Zoom-adaptive clustering grid; smart lens defaults; eliminate clutter. | [454](454-adaptive-macro-clustering-and-smart-lens.md) |
| 103 | SVG Geometry Transform & Hit Targets | Anchor SVG transform origins; expand transparent hit target. | [456](456-svg-forecast-dot-transform-origin-and-hit-targets.md) |
| 104 | Leaflet Zoom Lifecycle & Reactive Split | Rebuild clusters on zoomend; leap +2 on macro cluster click. | [460](460-macro-cluster-zoomend-reactive-trigger.md) |
| 105 | Lore Verifiable Memory Setup | Ingest git commits into Lore ontology; configure agy/opencode MCP. | [463](463-lore-verifiable-memory-setup-opencode-agy.md) |
| 106 | Doppler Radar Virga Gating & Modal Status | Never suppress convective cores (>=30 dBZ); label THÔNG THOÁNG. | [465](465-radar-virga-suppression-and-traffic-label.md) |
| 107 | Arterial Corridor Hazard & GeoJSON | Aggregate camera congestion along arteries; GeoJSON; in-memory cache. | [475](475-arterial-corridor-hazard-and-route-geojson.md) |
| 108 | Commuter Route Polylines & Cards | Render arterial route polylines; Leaflet deconfliction; bilingual cards. | [476](476-commuter-route-polyline-and-card.md) |
| 109 | Mobile Ergonomics & Viewport dvh | Dynamic 100dvh avoids bar shift; >=48px touch targets; drawer snap. | [477](477-mobile-ergonomics-and-safe-area.md) |
| 110 | Telegram Corridor Hazard Alerts | Corridor subscriptions; 15-min debounce; proactive health alerts. | [478](478-telegram-corridor-hazard-alerts.md) |
| 111 | Adaptive CV Load Shedding & Soak | Shed stale frames (>60s) with 429; 320px downscale; 24h soak profiling. | [479](479-cv-engine-load-shedding-soak.md) |
| 112 | Automated DR Drills & Webhooks | Weekly Sunday 03:00 UTC cron; universal webhook alerting; 4xx fast-fail. | [480](480-automated-restore-drill-webhook.md) |
| 113 | Resilient Address Search Sanitizer | Strip house/alley numbers ("34/55/67"); match street catalog. | [484](484-resilient-address-search-sanitizer.md) |
| 114 | Overlay Stacks & Shared Flow | Overlays need shared flow to reserve space; no magic offsets. | [494](494-corridor-card-overlay-stack.md) |
> Numbering note: Lesson 54 does not exist; numbers are preserved as cited.
