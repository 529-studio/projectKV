# L66: Tests That Rebuild the Code Prove Nothing, and Silent Fallbacks Hide the Path That Ran

## Context & Problem

A review of the 2026-11-03 merges found three defects that passed every gate. None was a
logic error in the happy path; each was a failure that the surrounding machinery was
structurally unable to see.

1. **Self-referential tests (`services/cv-engine/test_server.py`).** `load_model()`
   selected the OpenVINO XML to load inside the request handler's module, so the two
   tests written for it could not call it. They rebuilt the candidate list and the
   precedence loop inside the test body and asserted on the test's own local variable.
   48 of the 50 added test lines were this shape. The suite stayed green when the
   precedence was reversed by hand. The decision under test — segmentation weights
   versus the generic detector — is the single highest-stakes branch in the service:
   choosing the detector silently drops mask output for every frame.

2. **Silent discard in a defensive fallback (`cvclient/cloudvision.go`).** Gemini
   sometimes returns a JSON array where the prompt asked for an object. The fallback
   parsed the array and took `arr[0]`. An array with two contradictory verdicts
   persisted the first as ground truth with no log line, no error and no metric.

3. **Silent semantic change in a recursive fallback (`tools/eval/export_yolo_seg.py`).**
   When a labels directory had no top-level JSON, the exporter fell back to
   `rglob("*.json")`. Pointing `--labels-dir` at a tree that already contained
   `train/` and `val/` therefore collected both, re-shuffled every frame with
   `seed`, and wrote a *different* split — a frame that had lived in `val/` was
   exported into `train/`, silently invalidating comparability with weights trained
   on the original split. Reproduced with `seed=0`: four frames, `val=['f2']`, the
   original val frame `f3` moved to train.

All three share one shape: **the code did something defensible-looking, and no signal
distinguished it from the intended behaviour.**

## Key Decisions

1. **A test must call production code, or production code must be extracted until it
   can.** `candidate_xmls` became `select_model_xml(model_dir)`, and the
   `flood_assessment` dict became `build_flood_assessment(...)`. `load_model()` and
   the request handler are now thin callers. If a decision matters to the product, it
   is a function with a name, not a local variable inside a handler.

2. **Assert delegation, not just output.** `TestPredictPayload` drives a real
   `POST /predict` (with a fake compiled model) and always sends one variant with
   `build_flood_assessment` patched to a sentinel. If the handler ever re-inlines the
   dict, that test fails even though the dict contents still look right.

3. **Shared thresholds are constants, not literals.** `WHEEL_SUBMERSION_DELTA` and
   `FLOOD_DEPTH_CM` are read by both `triangulate_flood_verdict()` and
   `build_flood_assessment()`, so `wheel_submersion_detected` cannot contradict a
   `FLOODED_DEEP` verdict produced by the same signal.

4. **A fallback must be narrow, announced, or refused.** The array fallback now
   accepts exactly one element and returns an error for any other count, because
   nothing in the response says which verdict applies; the caller degrades to
   `UNKNOWN` rather than recording an arbitrary label. The exporter scans only the
   directory it was given, skips `roadway_regions*` sidecars by name, and raises
   `ValueError` naming the reason when it finds nothing — instead of widening the
   search and changing what the data means.

5. **Case counts in errors, not silence.** `gemini returned 2 verdicts for a single
   image; refusing to pick one` is actionable in a log; `arr[0]` is not.

## Verification & Impact

- `services/cv-engine`: 27 tests pass, including a real HTTP round trip through
  `CVRequestHandler` that asserts the emitted `flood_assessment` keys and the
  delegation sentinel.
- `cvclient`: multi-verdict rejection covered by
  `TestCloudVisionAnalyzer_Analyze_ArrayWrappedJSON_MultipleVerdicts_Rejected`,
  which also asserts the fail-closed `UNKNOWN`/`UNKNOWN` verdict.
- `tools/eval`: 3 regression tests; the two directory-discovery cases were run against
  the pre-fix module and reproduce the silent re-split and the sidecar ingestion
  (`total_labels=3` with a parse error versus `2` with none).
- Deliberately out of scope: duplicating each sidecar into two files (Finding 4) and
  the `docs/lessons/INDEX.md` ceiling that forces row erosion (Finding 6).
