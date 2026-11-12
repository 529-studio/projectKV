## Lesson 425: Decomposing Monolithic Go Composition Root into Modular Bootstrap Components

- **Issue & Tracking:** Issue #425 (`[backend] Decompose monolithic cmd/server/main.go into modular bootstrap components`), Sprint 12 (PR targeting `dev`).
- **Symptom:**
  `services/go-server/cmd/server/main.go` grew to **1,778 lines**, creating significant cognitive overhead and maintenance drag. Multiple concerns were commingled in a single source file:
  1. Dependency Inversion Principle (DIP) adapter structs and mapping functions spanning hundreds of lines.
  2. Inline background lifecycle tickers (cache eviction, 30-day raw data retention purger, stale flood hex cell reaper).
  3. Large route and middleware registration blocks.
  4. Obsolete static file route registrations (`styles.css` and `app.js`) referencing dead legacy vanilla JS assets rather than the Next.js static export structure.
- **Root Cause:**
  1. **Accretion in the Composition Root**: As new leaf modules were added in accordance with the leaf modulith architecture (`forecast`, `harvester`, `feedback`, `notify`, `telegrambot`), adapters and subsystem wiring were repeatedly appended directly to `cmd/server/main.go` rather than factored into cohesive domain files within the `main` package.
  2. **Coupled Lifecycle Loops**: Background goroutines were inlined in `func main()` with ad-hoc channels, making shutdown coordination difficult to trace.
  3. **Dead Route Accumulation**: Legacy asset routes (`styles.css`, `app.js`) from the early vanilla JS prototype remained bound in Gin's static routing table despite the web UI having migrated fully to a Next.js application (`apps/web/out`).
- **Mandatory Takeaways & Engineering Resolutions:**
  1. **Separation of Concerns within `package main`**:
     - `cmd/server/adapters.go`: Encapsulates all adapter structs bridging internal leaf modules (`verdictStoreAdapter`, `forecastHistoryAdapter`, `forecastCameraAdapter`, `subscriptionTaxonomyAdapter`, `telegramBotStoreAdapter`, `telegramBotTaxonomyAdapter`, `harvesterSnapshotFetcher`, `feedbackCameraChecker`, `feedbackSnapshotFetcher`, `harvesterPredictorAdapter`, `hotsetCoverageAdapter`) alongside subsystem constructors (`initAnalyzer`, `initStorageDriver`, `initHarvesterCoordinator`, `initFeedbackHandler`).
     - `cmd/server/jobs.go`: Centralizes background goroutine lifecycle loops and execution callbacks (`startProxyCachePurge`, `startRetentionPurge`, `startFloodHexReaper`, `preseedHotsetFromStore`, `buildHotsetOnVerdict`, `startHotsetScheduler`, `startHarvesterJob`).
     - `cmd/server/routes.go`: Consolidates Gin router instantiation, global middleware attachments, endpoint registrations, and static Next.js asset serving.
  2. **Purge Dead Vanilla JS Static Routes**:
     - Removed obsolete `router.StaticFile("/styles.css", ...)` and `router.StaticFile("/app.js", ...)`. Static serving is strictly targeted at `index.html`, Next.js bundles under `/_next`, and dynamic client-side SPA fallback.
  3. **Streamlined Composition Root (< 250 Lines)**:
     - `cmd/server/main.go` was reduced from 1,778 lines to **229 lines** (< 250 lines acceptance criterion), serving strictly as the declarative orchestrator for config loading, subsystem instantiation, and graceful shutdown.
  4. **Package-Level Test Invocation**:
     - When decomposing `package main` across multiple files, integration tests invoking `exec.Command("go", "run", "./main.go")` only compile `main.go` and fail with undefined symbol errors. Updated all integration test harnesses (`server_test.go`, `abuse_guard_test.go`) to invoke `exec.Command("go", "run", ".")` to compile the entire directory package.
