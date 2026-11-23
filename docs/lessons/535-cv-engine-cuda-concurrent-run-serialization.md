# Lesson 535: A Thread-Safe Session Is Not a Thread-Safe GPU Context

## Navigation Aid & Quick Reference

- **Domain**: CV inference process stability, CUDA execution provider, retry-vs-crash semantics.
- **Relevant Issues**: Issue #535, Lesson L79-style evidence discipline (observed crash, not inferred).
- **Key Files**:
  - `services/cv-engine/server.py` (`ORTModel.run`, `_run_or_recover`, `reload_session`, `ORTInferRequest.infer`)
  - `services/cv-engine/test_server.py` (`ORTSessionRecoveryTest`)
  - `services/cv-engine/README.md` (backend and concurrency contract)
- **Core Invariants**:
  - **A documented thread-safe API can still be unsafe on a specific execution provider.** ONNX Runtime states that `InferenceSession.run` may be called concurrently; the CUDA provider on Pascal (P106-100, CUDA 12.4) nonetheless tears the context down under concurrent runs.
  - **A retry path only covers exceptions that return to Python.** An access violation that exits the process is invisible to `try/except`, so "rebuild and retry" cannot be the mitigation for it.
  - **A rebuild must hold the same lock as the run it replaces.** Swapping the session while another thread is inside `run` is itself the illegal state.

---

## 1. Context & Problem Statement

`warded-cv-engine` serves with `ThreadingHTTPServer` and `CV_INFERENCE_CONCURRENCY = 4`,
so up to four request threads reach the same `InferenceSession`. Within six minutes of
enabling the ONNX/CUDA backend on `desk-srv-02` the container reached `RestartCount = 5`:

```
CUDA failure 700: an illegal memory access was encountered
```

Each exit dropped the whole in-flight batch upstream (`warded-backend` degraded ~40
cameras to `unknown` per restart) and surfaced one `503` on a deadman `GET /health` probe.

The earlier hardening (#497, #498) had already added a `reload_session` retry for a dead
CUDA context, plus `CV_REQUIRE_CUDA=1` to refuse silently serving on CPU. Neither could
help: the failure killed the process rather than raising, and the retry itself raced the
runs it was firing under.

## 2. Why Serialization Is the Sound Fix, Not a Band-Aid

Rejected alternatives:

- **Lower `CV_INFERENCE_CONCURRENCY` to 1.** Moves the invariant into deployment
  configuration: any stack environment edit, or a future caller that bypasses the queue,
  silently reintroduces the crash. The engine must be safe regardless of how many threads
  the HTTP server admits.
- **One `InferenceSession` per thread.** Multiplies VRAM and model-load cost on a 6 GB
  mining card for a workload measured at ~1.3 req/s.
- **Rely on the retry path.** The process exit never becomes an exception; retry covers
  driver resets, not access violations.

The forward pass is therefore serialized inside the process, but only when
`CUDAExecutionProvider` actually resolved:

```python
def run(self, tensor):
    if not self._serializes_runs:
        return self._run_or_recover(tensor)
    with self._run_lock:
        return self._run_or_recover(tensor)
```

Cost is bounded by the model, not by the queue: YOLOv8n at 320x320 takes a few
milliseconds against a measured `latency_ms = 16.054` HTTP path, while
`InferenceQueueManager` still sheds, prioritizes and downscales at admission. CPU
deployments keep their request-level parallelism, which
`test_cpu_runs_stay_parallel` pins with a `threading.Barrier(2)`.

## 3. What the Tests Must Prove

`test_concurrent_cuda_runs_never_overlap` runs eight threads against a fake CUDA session
that counts concurrent entries and asserts `max_active == 1` with zero errors — the
regression the production crash represents. Its CPU counterpart asserts the lock is not
applied globally, because a fix that serializes every backend would silently halve the
CPU throughput the older soak measured (98 → 113 RPS with concurrency 5).

## 4. Verification Evidence (2026-11-23, desk-srv-02, NVIDIA P106-100, driver 550.163.01)

Throwaway containers on `127.0.0.1:18000/18001` running the identical model mount, env and
GPU reservation; production `warded-cv-engine` and stack 27 untouched (`StartedAt` and
`RestartCount` unchanged across the run).

| | pre-fix image (deployed tag) | fixed branch `a85c1a4` |
| :--- | :--- | :--- |
| 8-way `POST /predict` | exited after **12.0 s** | survived full **180 s** |
| requests | 977 (976 × 200, 1 × 500, 8 conn-reset) | **33,761 — all 200** |
| `CUDA failure 700` lines | 8, then `CV_REQUIRE_CUDA` exit | **0** |
| container | `exited exit=1` | `running restarts=0` |
| latency under load | p50 43.15 / p95 64.36 ms (until crash) | p50 41.91 / p95 47.81 / p99 58.49 ms |
| throughput | 81.4 rps (crashing) | **187.5 rps** |

33,761 requests over 180 s is ~5.3 ms per serialized GPU forward pass: the lock is
saturated, not idle, and the HTTP p50 (41.91 ms) is below the pre-fix 43.15 ms. The
"serialization costs throughput" objection is therefore measured, not argued.

Remaining open item: a stack-27 soak against the real 57-hotset traffic after the image is
published, which proves the upstream workload rather than a synthetic frame.
