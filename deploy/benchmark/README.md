# AVX2 latency soak container (S13-T1 / #438)

Packages Python 3.12, OpenVINO, NumPy, Pillow, PyYAML and
`tools/train/bench_seg_latency.py` into a pinned `linux/amd64` image so the
segmentation gate (`3 x 200 timed runs`, p95 <= 25.0 ms at 416 px) can run on
the x86-64 AVX2 worker without hand-built virtualenvs.

A latency number is only evidence when it comes from the worker that serves
inference (S12-T5 §2.9). Three checks enforce that:

| Check | Where | Refusal |
|---|---|---|
| Container runtime reports an x86-64 architecture | `bench_avx2_docker.sh` | exit 2, before any build |
| Container is Linux x86-64 with `avx2` and a non-QEMU CPU model | `bench_avx2_host_probe.py` inside the image | exit 2, no evidence |
| Container clock within 300 s of the host clock | `bench_avx2_docker.sh` | exit 2, no evidence |

After the run, `bench_avx2_evidence_check.py` re-derives the protocol
(600 measurements), the trained head, the 25.0 ms verdict and every `*_at`
timestamp from the evidence JSON. A future-dated document is rejected with
exit 3.

## Run on the worker

```bash
# Prove this runtime is gate-eligible (builds the image if needed)
make bench-seg-avx2-check

# Measure; the evidence destination defaults to SOAK_OUT
make bench-seg-avx2-docker \
  SEG_IR=models/yolov8n_seg_openvino_416 \
  SEG_FRAME=data/proofs/proofs/2026-11-08/<camera>/<ts>_sample.jpg \
  SOAK_OUT=docs/reports/s13_t1_latency_avx2.json
```

Exit codes: `0` gate passed, `1` gate failed (evidence still written and
validated) or runtime failure, `2` host refused, `3` evidence rejected.

The IR, proof frame and evidence directory are bind-mounted; no model, frame
or secret is baked into the image. The repository mount is read-only and the
evidence directory is the only writable host path.

## Version pins

`OPENVINO_VERSION`, `NUMPY_VERSION`, `PILLOW_VERSION` and `PYYAML_VERSION` are
build arguments defaulting to the versions that produced the local 13.70 ms p95
on the M4 developer host. OpenVINO version is recorded in the evidence
fingerprint: change a pin only when the worker runtime deliberately changes,
and disclose the new version in the sprint report.

## Local verification (non-x86 host)

`make test-train` covers the image contract, both guard modules and the runner
orchestration with a fake container runtime, so the refusal and validation
paths are testable on a macOS/arm64 machine. On such a host the real runner
still refuses with exit 2:

```
$ tools/train/bench_avx2_docker.sh --check
[bench-avx2] REFUSED: container runtime architecture is 'aarch64', not x86-64; ...
```
