# Lesson 76: Inference Worker Co-Tenancy, Adaptive Load-Shedding, and Policy That Proves Itself

## Context
Sprint 10 S10-T8 (Issue #379, ADR-012 §3 (S10-T8) / §1.2 (F5)) addressed risk F5: one AVX2 worker (AMD Ryzen 7
6800HS, 8C/16T, 3.8 GB allocable RAM) co-hosts the real-time `cv-engine` hotset sweep,
MobileSAM batch pre-labeling, and the GitHub Actions runner. The 57-camera hotset must
complete every 30 seconds and keep p95 inference latency at or below 25 ms. The 14.8 ms
p95 quoted in the ADR was measured with no concurrent load, so it was never a serving
guarantee for the co-tenanted host.

The first cut of the tooling looked complete: a four-scenario benchmark table, cgroup
boundaries, an off-peak cron window, and a 320 px fallback. Almost none of it could fail.
The benchmark printed a capacity story, `verify_cgroup_configuration()` returned
`verified: True` as a literal, the cron check logged `FORBIDDEN` and still exited 0, and
the load-shedding branch lived inline in the HTTP handler where no test could call it.

## Problem & Root Cause
1. **A verifier that could not observe a violation.** The cgroup "verification" returned a
   constant. Changing the batch quota from 50% to 100%, moving `cv-engine` off cores 0-3,
   or overcommitting the 3.8 GB ceiling would all still report success. This is the same
   failure shape as L66: the test asserted on a copy of the intended state instead of
   recomputing the invariant from the configuration.
2. **A benchmark that presented planning figures as a measurement.** The four scenario
   rows were reference values adopted for the target hardware, but the report and console
   output implied a live run. On a different host the numbers cannot be reproduced, and no
   reader could tell which claims were measured.
3. **An off-peak guard that did not guard.** The runbook said the cron wrapper exits `1`
   outside 01:00-05:00 ICT. The implementation only logged the word FORBIDDEN, so a batch
   job would proceed through daytime peak traffic. The window was also ambiguous between
   the ICT policy clock and the UTC host crontab.
4. **A load-shedding decision hidden in the request handler.** Header parsing, environment
   override, model-shape fallback and the default were interleaved inside `do_POST`. Any
   test of "320 px degradation is honoured" had to rebuild that ladder and would keep
   passing after production precedence changed.
5. **Hysteresis existed only as prose.** Degradation at p95 > 25 ms or CPU > 85% and
   recovery at p95 <= 20 ms and CPU <= 70% were described, but no executable state machine
   proved that the worker holds 320 px in the band between the two thresholds instead of
   oscillating every cycle.
6. **The response reported the requested resolution, not the tensor served.** The handler
   letterboxed to the configured target and wrote that same pair into `input_resolution`.
   A compiled graph with a static spatial shape cannot execute the requested 320 px tensor:
   it either raises at `infer()` or executes its baked 416 px shape while the payload still
   claims 320. Nothing in the test suite recorded what was actually fed, so the squashed
   resize stayed invisible. Directional region assignment had the same class of defect:
   bottom-center anchors were normalized without clamping, so a letterbox overhang box
   produced coordinates outside [0.0, 1.0] and could be assigned to the wrong polygon.

## Solution
1. **Recompute every cgroup invariant from the mapping.** `verify_cgroup_configuration()`
   expands each cpuset, maps thread ids onto physical cores (`t % 8` on this SMT topology),
   and checks: `cv-engine` owns physical cores 0-3; batch and CI own 4-7; batch quota is
   50%; batch memory is 1536 MB; the CI runner is nested under the batch slice and cannot
   exceed it; root slices total 3584 MB of the 3840 MB ceiling; and CPU weights are ordered
   `cv_engine > sam_prelabel > ci_runner`. It returns a check list and a violations list,
   and the test suite tampers with a deep copy in three ways to prove each one is caught.
2. **Separate the reference profile from live measurement.** The matrix is explicitly
   tagged `measurement_source: "reference-profile"`, and the report opens with a provenance
   note stating the values are planning figures adopted for the target hardware and are
   not re-measured by the script. `verify_s10_t8_manual.py` performs the live measurements instead, so the
   two kinds of evidence are never conflated.
3. **Make the cron gate an exit code, with an explicit clock.** `--check-cron-window`
   returns 1 outside the window, and `--validate-cron` validates both the ICT policy window
   (01:00-05:00) and the UTC host crontab window (18:00-22:00). The window is end-exclusive,
   so 05:00 ICT is already peak. Cron hour expansion handles `*`, ranges, lists, and the
   `a/n` form correctly: `2/3` means 02, 05, 08 ... 23 and is therefore rejected. The same
   exit code now gates the batch entry point: `tools/seg/prelabel_sam.py` calls
   `enforce_offpeak_guard()` immediately after the AVX2 host guard, so a daytime batch
   refuses before weights are resolved, `--dry-run` still plans offline, `--allow-daytime`
   is a logged deliberate override, and the guard fails closed when its policy module
   cannot be imported.
4. **Extract the production decision until it is callable.** `resolve_input_resolution()`
   in `services/cv-engine/server.py` owns the precedence: `X-Target-Resolution` header,
   then `CV_INPUT_RESOLUTION`, then the model's static NCHW shape, then 416 px. Each source
   has its own whitelist: the request header is client-reachable and allows only
   `(320, 416)`, whereas the operator-controlled environment override allows
   `(320, 416, 640)`. An out-of-contract header or environment value is logged and ignored
   rather than silently resizing the model. The request handler is a thin caller, and both
   the service tests and the manual verifier call the same function.
5. **Drive the policy as a state machine in the manual verifier.** A six-step sequence
   proves 416 -> 320 degradation, batch throttling while still saturated at 320, hold at
   320 inside the hysteresis band, and recovery to 416 only when both recovery thresholds
   are met. The verifier also runs a live contention probe: two CPU-bound processes per
   core starve an inference-shaped preprocessing loop, the probe waits until every spinner
   has actually executed a loop before measuring, and the measured CPU share and wall
   slowdown are asserted, not assumed.
6. **Make the fed tensor the source of truth.** `load_model()` now records the compiled
   input shape, derives `resolution_mode` from that shape rather than from the reshape
   request, and logs explicitly whether the graph is dynamic (per-request 320/416 is
   honoured) or static (the baked shape is the only executable one).
   `resolve_feed_resolution()` returns the `(width, height)` the compiled graph can accept,
   the handler letterboxes straight to that pair, and the payload reports `actual_w` and
   `actual_h`. A static 416 IR therefore feeds a `(1, 3, 416, 416)` tensor and reports
   416 even when the header asks for 320. `box_polygon_anchor()` clamps the normalized
   bottom-center into `[0.0, 1.0]`, so an out-of-frame detection cannot be assigned to a
   region by an arithmetic accident. Tests assert the recorded feed shape, not just the
   JSON, and `verify_s10_t8_manual.py` drives a real `POST /predict` against a recording
   fake compiled model for both graph modes plus a rejected 640 px header.

## Verification
- `python3 -m unittest discover -s tools/eval -p "*_test.py"`: 128 tests pass (including 44
  in `benchmark_cotenancy_test.py`) covering tamper detection for quota, cpuset and memory,
  cron ICT/UTC boundaries and exit-worthy schedules, exact degradation thresholds,
  hysteresis recovery, single-scenario subset validation, and the macOS schema-only cgroup
  notice.
- `python3 -m unittest discover -s services/cv-engine -p "test_*.py"` (or
  `pytest services/cv-engine/test_server.py`): 45 tests pass. They assert the recorded
  feed tensor for a dynamic graph `(1, 3, 320, 320)` and a static graph
  `(1, 3, 416, 416)`, that the payload reports the fed shape, that a 640 px header is
  rejected, that `GET /info` always carries `resolution_mode` and `input_shape`, and
  that `box_polygon_anchor` clamps into `[0.0, 1.0]`. The suite pops
  `CV_INPUT_RESOLUTION` in `setUp` and restores it on cleanup, so an override exported
  in the operator's shell cannot change what the tests resolve.
- `python3 tools/eval/benchmark_cotenancy.py --scenario all --strict`: matrix invariants
  pass; full contention is VIOLATED at p95 28.6 ms / CPU 91.8%, and 320 px shedding is
  PASSED at p95 12.2 ms. `--scenario baseline --strict` validates the single row without
  demanding the other three.
- `python3 tools/eval/verify_s10_t8_manual.py`: all four Rule 10 scenarios pass. The
  recorded run (`docs/reports/s10_t8_manual_evidence.json`) measured 320 px preprocessing
  at a `cpu_ratio_320_over_416` of **0.852** (cpu_s 0.4265 -> 0.3634; p50 3.5 ms ->
  3.0 ms). The live contention probe, with two spinners per core, cut the probe's
  `cpu_share` from **0.997** to **0.476** (wall slowdown 2.403x). The asserted signal is
  that share drop, not a DVFS-sensitive wall-clock factor; wall slowdown is recorded as
  secondary evidence. The cron CLI exited 0 at 02:00 ICT and 1 at 14:00 ICT.
  A live `POST /predict` against a recording fake fed `(1, 3, 320, 320)` for a dynamic
  graph and `(1, 3, 416, 416)` for a static graph, and reported the same pair.
- Evidence is recorded in `docs/reports/s10_t8_manual_evidence.json` and the capacity
  envelope in `docs/reports/cotenancy_benchmark_v1.md`.
