# Lessons Index

One row per engineering lesson. The full text lives in the linked file; this index
is the only part an agent should read by default.

|#|Lesson|Mandatory guardrail|File|
|---|---|---|---|
|1|Passing Unit Tests is Never Sufficient|Simulate client workflows end-to-end.|[L01](L01-passing-unit-tests-is-never-sufficient-upstream-waf-header.md)|
|2|Sequence Inversion Risk|Document inversions; sequence tasks strictly.|[L02](L02-sequence-inversion-risk-invisible-downstream-divergence.md)|
|3|Completion Claims|Verify against live endpoints before completion claims.|[L03](L03-completion-claims-vs-production-reality-gap.md)|
|4|Tooling & Environment Discrepancies|Support standalone binaries and containerized runtimes.|[L04](L04-tooling-environment-discrepancies-on-host-platforms.md)|
|5|Zero-Emoji Standard|Zero emoji in UI and backend logs; monochromatic SVGs.|[L05](L05-zero-emoji-standard-telemetry-stream-corruption.md)|
|6|Single-Threaded HTTP vs Concurrent Go|Concurrent requests from Go workers require thread safety.|[L06](L06-single-threaded-http-servers-vs-concurrent-go-dispatchers.md)|
|7|Upstream Timestamp Mismatch & Mock Blind Spots|Normalize time series before joining; cut mock blind spots.|[L07](L07-upstream-timestamp-resolution-mismatch-mock-contract-blind.md)|
|8|GitHub Actions Billing & Duplicate Jobs|Fold steps into existing jobs; cut separate job overhead.|[L08](L08-github-actions-billing-ceiling-rounding-duplicate.md)|
|9|Competing Sources of Truth & Silent Drift|Never hardcode state that can be derived.|[L09](L09-competing-sources-of-truth-silent-drift-ghost-camera.md)|
|10|CD Pipeline Configuration|Avoid ad-hoc Portainer edits.|[L10](L10-cd-pipeline-configuration-overwrite-environment-propagation.md)|
|11|Scheduled Pipelines|Separate scheduled pipelines and proxy caches.|[L11](L11-decoupled-scheduled-pipelines-vs-on-demand-proxy-caches.md)|
|12|ML Model Classes|Business scoring formulas must mirror model classes.|[L12](L12-machine-learning-model-classes-vs-domain-business-formulas.md)|
|13|Ground-Truth Baselines|Isolate cameras with predictable, extreme baselines.|[L13](L13-ground-truth-smoke-test-baselines-vs-anecdotal-spot-checking.md)|
|14|Active Labeling|Never fabricate labels or substitute mock frames.|[L14](L14-active-labeling-effort-vs-passive-environmental-data.md)|
|15|Empirical Auditing|Safety-critical evaluation requires authentic positives.|[L15](L15-empirical-ground-truth-auditing-cold-start-capacity.md)|
|16|Heterogeneous Distributed Microservices|Distribute microservices by hardware affinity.|[L16](L16-heterogeneous-distributed-microservices-multi-endpoint.md)|
|17|CCTV Viewport Limits|Calibrate visual saturation floors from geometry.|[L17](L17-instantaneous-cctv-camera-viewport-physical-limits-vs.md)|
|18|High-Frequency Telemetry|Retain self-hosted OpenVINO for local inference.|[L18](L18-high-frequency-edge-telemetry-vs-cloud-vlm-economics-the.md)|
|19|Unit-Test Trap|Parameter calibration must be empirical.|[L19](L19-the-unit-test-verification-trap-the-parameter-calibration.md)|
|20|VPN SPOF|Local neural compute incurs $0 API cost; fails gracefully.|[L20](L20-vpn-spof-graceful-degradation-decoupling-cloud-daily.md)|
|21|S3-Compatible Cloudflare R2 Integration|Set UsePathStyle = true in AWS SDK Go v2 for R2.|[L21](L21-s3-compatible-cloudflare-r2-integration-path-style-routing.md)|
|22|Plaintext Credential Exposure Prevention|Zero secret fallbacks; load dynamically or fail fast.|[L22](L22-plaintext-credential-exposure-prevention-benchmark.md)|
|23|YOLO Activation Summation|Raw output tensors require NMS.|[L23](L23-yolo-anchor-activation-summation-vs-object-counts-missing.md)|
|24|Decoupled Multimodal Hybrid Architecture|Retain local inference; guard cloud VLMs with quotas.|[L24](L24-decoupled-multimodal-hybrid-architecture-quota-guarded-dual.md)|
|25|CI Linter Schema Flakiness|Set verify: false in golangci-lint-action.|[L25](L25-ci-linter-schema-validation-flakiness-toolchain-version.md)|
|26|GitHub Issue Auto-Closing Semantics|PR description must start with Close #<number>.|[L26](L26-github-issue-auto-closing-keyword-semantics-vs-descriptive.md)|
|27|Centralized Collaborative Team Labeling|Dual-driver List(ctx, prefix) for local/cloud storage.|[L27](L27-centralized-collaborative-team-labeling-over-local.md)|
|28|Zero Unauthenticated Dev Endpoints|Dev endpoints require dev token auth.|[L28](L28-zero-unauthenticated-dev-endpoints-fail-closed-developer.md)|
|29|Perception Fixes|Standard NMS with letterbox preprocessing.|[L29](L29-perception-layer-fixes-vs-scoring-hacks-empirical.md)|
|30|Capacity Floors|Enrich camera catalog with corridor capacity.|[L30](L30-uniform-capacity-floors-vs-physical-corridor-geometry.md)|
|31|Environmental Triggers|Rain triggers archive flood frames.|[L31](L31-active-environmental-opportunism-continuous-cloud-label.md)|
|32|Affordance Collision|Brackets reserved for badges; click cooldowns.|[L32](L32-affordance-collision-client-side-rate-limit-cooldown-in.md)|
|33|Secret Sanitizer|SecretSanitizer() wraps responses; hysteresis.|[L33](L33-defense-in-depth-secret-sanitization-hysteresis-filtering.md)|
|34|Sound Solutions|Never anchor effort to shortcuts.|[L34](L34-architectural-psychological-priority-sound-solutions.md)|
|35|Holistic Repository Docs Maintenance|Audit and sync all affected docs across repo.|[L35](L35-holistic-repository-knowledge-maintenance-vs-isolated.md)|
|36|Notification Modulith|internal/notify has zero sibling imports; 30m debounce.|[L36](L36-leaf-modulith-notification-infrastructure-30-minute-sliding.md)|
|37|Labeling Paradigms|Distinguish eval from train sets; no frame diff.|[L37](L37-data-labeling-paradigms-why-frame-diff-fails-in-outdoor.md)|
|38|Arterial Saturation|Stale metrics degrade to Unknown, never Clear.|[L38](L38-trust-safety-for-arterial-saturation-status-degradation.md)|
|39|Heuristic Ground-Truth|Never generate ground-truth with heuristic rules.|[L39](L39-heuristic-ground-truth-pollution-vs-empirical-directional.md)|
|40|Harvester|Edge runtime telemetry with rain cooldowns.|[L40](L40-autonomous-zero-vlm-harvester-architecture-dual-trigger.md)|
|41|Airborne Radar|Calibrate radar reflectivity to mm/h; fix alpha.|[L41](L41-airborne-radar-reflectivity-dbz-vs-ground-surface.md)|
|42|Map Coordinate|Toolbars occupy dedicated row to avoid collision.|[L42](L42-map-overlay-coordinate-zoning-hierarchical-collision.md)|
|43|Tile Server|Zoom 7 bounds tile grid; avoid blank fallbacks.|[L43](L43-upstream-tile-server-zoom-ceiling-silent-200-ok-placeholder.md)|
|44|Spatial Aggregator|Sync in-memory aggregator with store TTL.|[L44](L44-in-memory-spatial-aggregator-ttl-synchronization-multi-lens.md)|
|45|Crowdsourced Ground-Truth|internal/feedback zero sibling imports; salted IP.|[L45](L45-crowdsourced-ground-truth-ingestion-ip-hashed-privacy-and.md)|
|46|Standalone Harvesting CLI|auto_harvest.py builds Draft-07 manifests.|[L46](L46-standalone-automated-data-harvesting-offline-evaluation.md)|
|47|Web Labeler Active Learning|LabelFilterStatus in /labeler approval workflow.|[L47](L47-web-labeler-active-learning-flywheel-provenance-badging-and.md)|
|48|Zero-VLM OpenVINO Segmentation|Validate FP16 models within 30s hotset budget.|[L48](L48-zero-vlm-self-hosted-flood-segmentation-openvino-model.md)|
|49|Object Enumeration|ListDirectories uses S3 delimiters.|[L49](L49-unbounded-object-enumeration-vs-s3-delimiter-hierarchy.md)|
|50|Waterway|Whitelist roads, filter waterway nodes.|[L50](L50-waterway-bridge-footing-sensor-hygiene-exclusion-of-non.md)|
|51|Partition|Thread-safe labelCache with 60s TTL.|[L51](L51-partition-object-scalability-client-pacing-in-active.md)|
|52|Go Select|Labeled loop break ensures clean termination.|[L52](L52-go-select-break-concurrency-fallacy-heuristic-ground-truth.md)|
|53|Active Learning|FilterSnapshots sources from local storage and R2.|[L53](L53-active-learning-queue-desync-dual-storage-frame-routing-in.md)|
|56|Off-Box Guardrails|Host guard enforces AVX2 for training.|[L56](L56-off-box-training-guardrails-yolov8-segmentation-dataset.md)|
|55|Crowdsource Feedback Modulith|Feedback has zero sibling imports.|[L55](L55-the-crowdsource-feedback-black-hole-decoupled-leaf-modulith.md)|
|57|Bot Token Leak Defense|SanitizeBytes scrubs token before logging.|[L57](L57-telegram-bot-token-leak-vectors-word-boundary-regex.md)|
|58|Parity Alert Single-Source|Exported pure function notify.BuildFloodAlert.|[L58](L58-parity-verification-illusion-single-source-alert-generation.md)|
|59|Docs Link Rot Prevention|Moved docs proven byte-identical before reference rewrite.|[L59](L59-documentation-cross-reference-rot-links-to-never-merged-files.md)|
|60|Read Tiering & Budget|Always-loaded <=36KB, <=16KB/file (INDEX 20KB); on-demand <=25KB.|[L60](L60-read-frequency-is-the-cost-multiplier-documentation-tiering.md)|
|61|One Decision, One File ADR|Do not append successor decisions; use separate ADR files.|[L61](L61-one-decision-one-file-adr-register-that-embeds-its-successors.md)|
|62|Offline SAM Pre-Labeling Parity|Host guard enforces AVX2; decouple local from R2 snapshot.|[L62](L62-offline-sam-prelabeling-provenance-and-dual-layer-storage-parity.md)|
|63|VLM Array Unmarshal|cvclient falls back to []verdictJSON, extracts arr[0].|[L63](L63-vlm-structured-output-variance-and-array-unmarshal-resilience.md)|
|64|First Segmentation OpenVINO|water_puddle IoU must beat null baseline.|[L64](L64-first-segmentation-cycle-openvino-fp16-and-map-gating.md)|
|65|Roadway Region Sidecars|roadway_regions.json sidecar per split.|[L65](L65-directional-roadway-polygons-sidecar-and-per-direction-ground-truth.md)|
|66|Self-Referential Test Trap|Extract decisions into pure functions.|[L66](L66-self-referential-tests-and-silent-fallbacks-hide-the-live-path.md)|
|67|Upsert-Only State Limitations|Append-only history with idempotent keys, UTC.|[L67](L67-upsert-only-state-tables-cannot-feed-time-series-models.md)|
|68|Chronological Split Boundary Leak|Purge training samples crossing cutoff.|[L68](L68-chronological-split-horizon-boundary-leakage.md)|
|69|Hardcoded Fixture Shortcuts|Dynamic artifact loader; fail-closed guards.|[L69](L69-hardcoded-prototype-fixtures-and-the-cost-of-shortcuts.md)|
|70|Semantic Taxonomy Drift|Reconcile zone/ward taxonomy.|[L70](L70-canonical-zone-and-ward-subscription-taxonomy.md)|
|71|Directional Ingestion & Pinning|Polygon assignment in cv-engine.|[L71](L71-directional-runtime-ingestion-and-worst-direction-pinning.md)|
|72|Schema Assertions|Assert migration readiness at boot.|[L72](L72-fail-fast-schema-assertions-and-nonblocking-batch-retention.md)|
|73|As-Of Weather Feature Store|Append-only weather; as-of join with 30m cutoff.|[L73](L73-asof-weather-feature-store-and-ablation-gating.md)|
|74|Frontend Forecast Gating|Fail-closed checks (ok/stale), 95% CI band.|[L74](L74-frontend-forecast-contract-gating-and-uncertainty-visualization.md)|
|75|Segmentation Dataset Group Split|Group split by camera/date; IoU >= 0.612, mAP >= 0.496.|[L75](L75-segmentation-dataset-v2-group-splitting-and-retraining-gates.md)|
|76|Inference Worker Co-Tenancy|Recompute cgroup invariants; cron gate exits 1; clamp anchors.|[L76](L76-inference-worker-cotenancy-and-adaptive-loadshedding.md)|
|77|Evidence Gate & Synthetic Ban|No synthetic fixtures; authentic timestamps; entropy >= 3.0.|[L77](L77-evidence-integrity-gate-and-synthetic-prohibition.md)|
|78|Unattended Soak Replay Ingestion|24/7 harvester; 11,905 real-frame replay; process soak.|[L78](L78-unattended-soak-and-proof-archive-ingestion.md)|
|79|Roadway Registry Scale-Out|59-camera dual Hot Set; IoU < 0.05; capacity sum 1.0.|[L79](L79-directional-roadway-registry-scaleout.md)|
|80|Automated Refit & Promotion|Stdlib Ridge; chronological gate; atomic promotion.|[L80](L80-automated-forecast-refit-and-live-deployment.md)|
|81|Temporal Baseline Ratchet|No clock-dependent baselines; SUPERSEDED lifecycle.|[L81](L81-temporal-baseline-ratchet-and-approval-gate-concurrency.md)|
|82|Authentic Artifact Protection|Real export only; refuse untrained heads; test resolutions.|[L82](L82-authentic-model-artifacts-and-untrained-head-guard.md)|
|83|Native Protection & CI Gates|Verify native controls; migrate checks before delete.|[L83](L83-native-github-protection-and-ci-gate-consolidation.md)|
|84|Forecast Refit Daemon Locking|fcntl.flock daemon locking; state quarantine; row gating.|[L84](L84-forecast-refit-daemon-and-singleflight-locking.md)|
|85|Commuter Geolocation Bands|250m bands; clock-free comparator; zero coordinate leak.|[L85](L85-commuter-geolocation-banded-sorting.md)|
|86|RFC3339Nano Timestamp Precision|Fixed 9-digit layout; microsecond truncation; UTC invariants.|[L86](L86-rfc3339nano-9digit-timestamps-microsecond-truncation.md)|
|87|JWT & CORS Whitelist|Verify RS256 & aud via JWKS; whitelist CORS.|[L87](L87-cryptographic-jwt-verification-and-cors-origin-whitelisting.md)|
|88|Proxy OOM Bounds & Gzip Order|Cap upstream reads via io.LimitReader; order gzip outer.|[L88](L88-proxy-oom-bounds-and-compression-middleware-ordering.md)|
|89|Unified SSE, Canvas & Memory Bounds|Single EventSource; ref mirror breaks loop; preferCanvas.|[L89](L89-unified-sse-stream-provider-canvas-rendering-and-visibility-throttling.md)|
|90|Physical Depth Monotonic Drops|Avoidance is hazard; physical cm depth; latch drops; no fake levels.|[L90](L90-physical-depth-hazard-classification.md)|
|91|Staging Proxy Tunnel Alignment|Bind proxy to tunnel HTTP entrypoint; avoid phantom certresolvers.|[L91](L91-staging-reverse-proxy-entrypoint-and-tunnel-alignment.md)|
|92|Issue-Keyed Lesson Files|Name files after issue; fail-closed webhook without secret.|[402](402-telegram-bot-and-web-subscription.md)|
|93|Real Provenance Verification|Exact timestamp & hash join; purge dead labels; retrain gate.|[411](411-dataset-v3-provenance-and-avx2-evidence.md)|
|94|SSE Sanitizer Body Buffering Bypass|Streaming content bypasses body buffer; explicit Flusher check.|[420](420-sse-streaming-sanitizer-buffering.md)|
|95|Decompose Monolithic Composition|Factor main.go into adapters, jobs, routes; test go run.|[425](425-decompose-monolithic-cmd-server-main.md)|
|96|Moore Boundary Tracing|Pixel/backtrack state; 8-connected merge; restores SAM yield.|[438](438-moore-boundary-tracing-jacobs-criterion.md)|
|97|Dead Camera Error Isolation|Isolate 404/5xx feeds from breaker; export 24h metrics.|[441](441-feed-error-breaker-isolation-and-health-observability.md)|
|98|Litestream R2 Restore Drill|Backups are hypotheses until drill restores SQLite DB.|[442](442-litestream-r2-replication-and-restore-drill.md)|
|99|Map Clustering Severity Bubble-Up|Clusters bubble up deepest hazard; fallback tiles avoid blanks.|[443](443-commuter-map-clustering-and-hazard-depth.md)|
|100|Forecast Weather Parity Gating|Mirror Go feature store in Python; require skill gating.|[444](444-forecast-weather-parity-and-skill-gating.md)|
|101|Cluster Deconfliction|Repel overlapping clusters; separate toolbar row.|[452](452-map-cluster-collision-and-toolbar-deconfliction.md)|
|102|Adaptive Clustering & Smart Lens|Zoom-adaptive clustering; smart lens defaults; cut clutter.|[454](454-adaptive-macro-clustering-and-smart-lens.md)|
|103|SVG Geometry Origin & Hit Targets|Anchor SVG transform origins; expand transparent hit targets.|[456](456-svg-forecast-dot-transform-origin-and-hit-targets.md)|
|104|Leaflet Zoom Lifecycle Split|Rebuild clusters on zoomend; leap +2 on macro click.|[460](460-macro-cluster-zoomend-reactive-trigger.md)|
|105|Lore Verifiable Ontology Setup|Ingest commits into Lore ontology; configure MCP.|[463](463-lore-verifiable-memory-setup-opencode-agy.md)|
|106|Doppler Radar Virga Gating|Keep >=30 dBZ convective cores; label THÔNG THOÁNG.|[465](465-radar-virga-suppression-and-traffic-label.md)|
|107|Arterial Corridor GeoJSON Routing|Aggregate congestion along arteries; GeoJSON; cache.|[475](475-arterial-corridor-hazard-and-route-geojson.md)|
|108|Route Polylines & Cards|Route polylines; Leaflet deconfliction; bilingual cards.|[476](476-commuter-route-polyline-and-card.md)|
|109|Mobile Dvh & Safe Targets|100dvh avoids shift; >=48px targets; drawer snap.|[477](477-mobile-ergonomics-and-safe-area.md)|
|110|Telegram Corridor Hazard Alerts|Corridor subscriptions; 15m debounce; health alerts.|[478](478-telegram-corridor-hazard-alerts.md)|
|111|Adaptive CV Load Shedding|Shed stale frames (>60s) with 429; 320px downscale; soak.|[479](479-cv-engine-load-shedding-soak.md)|
|112|Automated DR Restore Webhook|Weekly 03:00 UTC cron; universal webhook alerts; 4xx fast-fail.|[480](480-automated-restore-drill-webhook.md)|
|113|Resilient Address Search Sanitizer|Strip house/alley numbers; match street catalog.|[484](484-resilient-address-search-sanitizer.md)|
|114|Overlay Stacks Shared Flow|Overlays need shared flow for space; no magic offsets.|[494](494-corridor-card-overlay-stack.md)|
|115|Vehicle Clearance Hazard|Dynamic thresholding from vehicle clearance; adapt to clearance.|[510](510-vehicle-clearance-profiles-and-dynamic-hazard.md)|
|116|Hourly Tidal Tide & Rain Scrubber|7-constituent tide model + rain; normal flow scrubber.|[511](511-hourly-tidal-and-convective-rain-timeline.md)|
|117|My-Route Personal Glance|3s eval; local-first; automated detour guidance.|[512](512-my-route-journey-glance-and-avoidance.md)|
|118|Rapid Submersion Labeler|Hotkeys 1-5 replace polygons; 20-120x faster.|[513](513-high-speed-submersion-labeler-and-calibration.md)|
|119|Mobile Kiosk Safe Area|>=48px touch targets; safe-area-inset on HUD.|[514](514-mobile-kiosk-touch-targets-and-safe-area.md)|
|120|Polyline Projection & Navigation Hazards|Turn-by-turn alerts along corridor polylines; clearance adaptive.|[523](523-turn-by-turn-navigation-alerts.md)|
|121|Pure Go VAPID & Dead Push|Stdlib VAPID + ECE; prune 410/404 endpoints.|[525](525-web-push-vapid-notifications.md)|
|122|Flood Depth Trends & Tidal Correlation|Corridor aggregation; harmonic tide correlation; bound query hours.|[526](526-corridor-flood-trends.md)|
|123|Crowd Report Corroboration|300m spatial clustering; Bayesian sensor fusion.|[527](527-crowd-report-verification.md)|
|124|INT8 Quantization & FP32 Heads|Static PTQ; keep detection heads FP32; no drift.|[528](528-int8-onnx-quantization.md)|
|125|OpenMetrics Leaf Collectors|Standard /metrics endpoint; acyclic leaf collectors.|[529](529-prometheus-metrics-endpoint.md)|
|126|Prod CORS & Trusted Proxies|Ban wildcard/loopback CORS in prod; SetTrustedProxies.|[530](530-cors-and-trusted-proxies.md)|
|127|Dead Camera Culling Floor|Prune >14d dead feeds; >=50% corridor coverage floor.|[531](531-daily-camera-sync-and-culling.md)|
|128|Healthz vs Readyz Split|/healthz process vs /readyz deps; avoid cascades.|[533](533-healthz-liveness-readiness-split.md)|
|129|CUDA Run Serialization|Mutex guards ONNX CUDA init and concurrent runs.|[535](535-cv-engine-cuda-concurrent-run-serialization.md)|
|130|Transition Push Bypass|StateTracker bypasses debounce on escalation; silent recovery.|[549](549-trigger-webpush-state-transitions.md)|
|131|Push Drawer & Clearance|Local kv prefs; z-[980] drawer; >=48px targets; zero emoji.|[550](550-web-push-preferences-and-route-drawer.md)|
|132|Tidal Sluice Indicators|Map >=1.40m tide to CLOSED_PUMPING; corridor card badges.|[551](551-corridor-tidal-gate-indicators.md)|
|133|Daily Camera Sync CI|02:00 UTC workflow runs tests first; opens PR via env.|[552](552-automated-camera-catalog-sync-workflow.md)|
|134|CPU Edge INT8 Deploy|cv-edge drops CUDA; check >=1MB & entropy>=3.0 preflight.|[553](553-secondary-edge-int8-deployment.md)|
|135|Grafana PromQL Parity|Grafana v10 locked Asia/Ho_Chi_Minh; assert metric names.|[554](554-grafana-prometheus-dashboards.md)|
|136|60-Cam Burst Backlog|CVServer request_queue_size=128; gate P95<=200ms, <1% err.|[555](555-int8-cv-60-camera-stress-benchmark.md)|
|137|SQLite WAL Failover Drill|Isolated VACUUM INTO drill verifies row parity & integrity.|[556](556-automated-sqlite-failover-drill.md)|
> Numbering note: Lesson 54 does not exist; numbers are preserved as cited.
