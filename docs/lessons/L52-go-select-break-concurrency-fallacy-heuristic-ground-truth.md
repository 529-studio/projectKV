## Lesson 52: Go `select` Break Concurrency Fallacy & Heuristic Ground-Truth Deprecation

- **Incident & Tracking:** Issue #287, Kanban **[KV-17]** (`fix/s8-t5-harvester-semaphore-deadlock`, PR targeting `dev`).
- **Symptom:**
  1. During server shutdown or batch timeout in `services/go-server/internal/harvester/pipeline.go`, the harvester risked a permanent hang / deadlock on `wg.Wait()`.
  2. When meteorological rain occurred ($\ge 5.0\text{ mm/h}$), all 207 corridor cameras automatically received auto-generated ground-truth labels marked as `flood: "SHALLOW"` and `waterline_depth: "TIRE_QUARTER_SUBMERGED"`, regardless of whether the physical road surface was submerged or elevated and dry.
- **Root Cause:**
  1. **The Go `select` Break Trap**: In Go, a bare `break` inside a `select` statement terminates only the `select`, NOT the enclosing `for` loop. When `<-ctx.Done()` was selected, the loop continued directly to `wg.Add(1)` and spawned a goroutine whose defer executed `<-sem; wg.Done()`. Because `sem <- struct{}{}` was never executed, `<-sem` drained a token belonging to another active worker or blocked indefinitely on an empty channel, preventing `wg.Done()` from ever being called and deadlocking `wg.Wait()`.
  2. **Weather-Based Ground-Truth Pollution**: `DetermineWaterlineDepth` in `pipeline.go` and `auto_harvest.py` used an uncalibrated weather forecast lookup table instead of computer vision, creating synthetic answer key pollution and violating Lesson 39.
- **Mandatory Takeaways & Engineering Resolutions:**
  1. **Labeled Loop Break in Worker Pools**:
     - Upgraded `HarvestCameras` with a labeled loop break:
       ```go
       harvestLoop:
       for _, cam := range cameras {
           select {
           case sem <- struct{}{}:
           case <-ctx.Done():
               break harvestLoop
           }
           wg.Add(1)
           go func(c Camera) {
               defer func() { <-sem; wg.Done() }()
               ...
           }(cam)
       }
       ```
     - Goroutines are never spawned when context is cancelled, completely eliminating semaphore token theft and shutdown deadlocks.
  2. **Ground-Truth Honesty & `UNKNOWN` Enum Support**:
     - Updated `DetermineWaterlineDepth` in `pipeline.go` and `tools/eval/auto_harvest.py` to set `flood: "UNKNOWN"`, `waterline_depth: "UNKNOWN"`, and `flood_submersion_anchor: "unknown"` whenever rain is detected without direct computer vision verification.
     - Updated `tools/eval/label_schema.json` and `services/go-server/internal/harvester/schema.go` to support `UNKNOWN` across Draft-07 validation.
  3. **Concurrency Context Cancellation Unit Test**:
     - Implemented `TestHarvester_ContextCancellationCleanShutdown` in `internal/harvester/harvester_test.go` verifying under `-race` that cancelling context exits gracefully in $< 100\text{ ms}$ with zero goroutine leaks.

