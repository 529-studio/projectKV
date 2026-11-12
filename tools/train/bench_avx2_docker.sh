#!/usr/bin/env bash
#
# bench_avx2_docker.sh — run the segmentation latency gate in the pinned x86-64
# AVX2 container (S12-T5 §2.9 / S13-T1 #438).
#
# A latency number is evidence only when it was measured on the hardware that
# serves inference. This wrapper refuses to measure anywhere else:
#
#   1. the container runtime must be reachable and must report an x86-64
#      architecture, so Docker Desktop's linux/amd64 emulation on Apple Silicon
#      is refused before an image is built;
#   2. inside the pinned image, bench_avx2_host_probe.py re-runs the Linux /
#      x86_64 / AVX2 guard and rejects QEMU/TCG CPU models, which can advertise
#      AVX2 without being the worker;
#   3. the container clock must be within 300 s of the host clock, so evidence
#      cannot be future-dated by a drifting VM clock;
#   4. the protocol is fixed at 200 timed runs x 3 repeats after 10 warm-ups at
#      416 px. There is no flag to shorten it — a short run is not the gate;
#   5. after the run, bench_avx2_evidence_check.py re-derives the 600
#      measurements, the 25.0 ms verdict and every timestamp from the evidence
#      JSON itself.
#
# Refusals exit 2 and write no evidence file, matching the harness. A completed
# run exits 0 when the gate passed, 1 when it failed (evidence is still written
# and validated), and 3 when the evidence document fails validation.
#
# Usage:
#   tools/train/bench_avx2_docker.sh \
#       --ir models/yolov8n_seg_openvino_416 \
#       --frame data/proofs/proofs/2026-11-08/<camera>/<ts>_sample.jpg \
#       --out docs/reports/s13_t1_latency_avx2.json
#
#   # Guard only: build if needed, then prove this runtime is gate-eligible.
#   tools/train/bench_avx2_docker.sh --check
#
# Requirements: docker (or podman) with a native x86-64 AVX2 daemon, and the
# trained IR directory containing yolov8n_seg.xml, yolov8n_seg.bin and
# metadata.yaml with head_status: trained.

set -Eeuo pipefail

readonly EXIT_OK=0
readonly EXIT_GATE_FAILED=1
readonly EXIT_REFUSED=2
readonly EXIT_EVIDENCE_REJECTED=3
readonly EXIT_UNREADABLE=1

readonly RUNS=200
readonly WARMUP=10
readonly REPEATS=3
readonly PLATFORM="linux/amd64"
readonly DEFAULT_IMAGE="projectkv/bench-seg-avx2:2026.4.1"
readonly DOCKERFILE="deploy/benchmark/Dockerfile.avx2"
readonly HOST_PROBE="/opt/bench/bench_avx2_host_probe.py"
readonly EVIDENCE_CHECK="/opt/bench/bench_avx2_evidence_check.py"
readonly CLOCK_SKEW_SECONDS=300

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
REPO_ROOT="$(cd -- "$SCRIPT_DIR/../.." && pwd -P)"

log() {
    printf '[bench-avx2] %s\n' "$*" >&2
}

refuse() {
    log "REFUSED: $*"
    exit "$EXIT_REFUSED"
}

die() {
    log "ERROR: $*"
    exit "$EXIT_UNREADABLE"
}

usage() {
    cat <<'EOF'
Run the segmentation latency gate (3 x 200 runs at 416 px) inside a pinned
x86-64 AVX2 container. Refuses emulated or non-x86-64 runtimes.

Usage:
  tools/train/bench_avx2_docker.sh --ir DIR --frame FILE --out FILE [options]

Required:
  --ir DIR           OpenVINO IR directory (yolov8n_seg.xml/.bin, metadata.yaml)
  --frame FILE       Real proof frame used for inference
  --out FILE         Evidence JSON destination (created, parent directories too)

Options:
  --metadata FILE    metadata.yaml override (default: <ir>/metadata.yaml)
  --image TAG        Image tag (default: projectkv/bench-seg-avx2:2026.4.1)
  --runtime CMD      Container runtime command (default: docker, then podman)
  --check            Run the host guard only; no measurement, no evidence
  --no-build         Use the local image as-is; fail when it is absent
  --rebuild          Rebuild the image before running
  -h, --help         Show this help

Exit codes:
  0 gate passed      1 evidence rejected or runtime failure
  2 host refused     3 evidence document failed validation
EOF
}

absolute_dir() {
    ( cd -- "$1" && pwd -P )
}

absolute_output() {
    local target="$1" directory
    directory="$(dirname -- "$target")"
    mkdir -p -- "$directory"
    directory="$(cd -- "$directory" && pwd -P)"
    printf '%s/%s\n' "$directory" "$(basename -- "$target")"
}

is_podman() {
    case "$(basename -- "${RUNTIME:-}")" in
        *podman*) return 0 ;;
        *) return 1 ;;
    esac
}

runtime_arch() {
    local arch=""
    arch="$("$RUNTIME" info --format '{{.Architecture}}' 2>/dev/null || true)"
    case "$arch" in
        ""|"<no value>"|*"can't evaluate"*)
            arch="$("$RUNTIME" info --format '{{.Host.Arch}}' 2>/dev/null || true)"
            ;;
    esac
    printf '%s\n' "$arch" | tr '[:upper:]' '[:lower:]'
}

require_runtime() {
    if ! command -v "$RUNTIME" >/dev/null 2>&1 && [ ! -x "$RUNTIME" ]; then
        refuse "container runtime '$RUNTIME' is not installed; the AVX2 worker image cannot run"
    fi
    if ! "$RUNTIME" info >/dev/null 2>&1; then
        refuse "container runtime '$RUNTIME' is not reachable; start it or point --runtime at the worker's daemon"
    fi
}

require_x86_runtime() {
    local arch
    arch="$(runtime_arch)"
    case "$arch" in
        x86_64|amd64|x64)
            log "Container runtime architecture: $arch"
            ;;
        *)
            refuse "container runtime architecture is '${arch:-unknown}', not x86-64; an emulated linux/amd64 container is not the AVX2 worker and its latency is not gate-eligible"
            ;;
    esac
}

build_image() {
    # No --platform here: require_x86_runtime() already proved the daemon is
    # x86-64, so the build is native and never needs an emulation layer.
    log "Building $IMAGE from $DOCKERFILE"
    if ! ( cd -- "$REPO_ROOT" && "$RUNTIME" build -f "$DOCKERFILE" -t "$IMAGE" . ); then
        die "image build failed; the AVX2 worker image is required before any measurement"
    fi
}

ensure_image() {
    if [ "$NO_BUILD" -eq 0 ]; then
        if [ "$REBUILD" -eq 1 ] || ! "$RUNTIME" image inspect "$IMAGE" >/dev/null 2>&1; then
            build_image
        fi
    fi
    if ! "$RUNTIME" image inspect "$IMAGE" >/dev/null 2>&1; then
        die "image $IMAGE is not present and --no-build was given"
    fi
}

probe_container_host() {
    local output status
    set +e
    output="$("$RUNTIME" run --rm \
        --platform "$PLATFORM" \
        --cap-drop ALL \
        --security-opt no-new-privileges \
        --entrypoint python3 \
        "$IMAGE" "$HOST_PROBE" 2>&1)"
    status=$?
    set -e

    if [ "$status" -ne 0 ]; then
        log "$output"
        refuse "the container-side host guard rejected this runtime (exit $status); no evidence was written"
    fi

    case "$output" in
        *'"avx2": true'*|*'"avx2":true'*) ;;
        *) refuse "the container host probe returned no AVX2 confirmation: $output" ;;
    esac

    CONTAINER_EPOCH="$(printf '%s' "$output" | sed -n 's/.*"epoch"[[:space:]]*:[[:space:]]*\([0-9][0-9]*\).*/\1/p' | head -n 1)"
    if [ -z "$CONTAINER_EPOCH" ]; then
        refuse "the container host probe returned no clock reading; timestamps cannot be trusted"
    fi
}

require_clock_agreement() {
    local host_epoch skew
    host_epoch="$(date +%s)"
    skew=$(( CONTAINER_EPOCH - host_epoch ))
    if [ "$skew" -gt "$CLOCK_SKEW_SECONDS" ] || [ "$skew" -lt $(( 0 - CLOCK_SKEW_SECONDS )) ]; then
        refuse "container clock differs from the host by ${skew}s (limit ${CLOCK_SKEW_SECONDS}s); evidence timestamps would be untrustworthy"
    fi
    log "Container clock delta: ${skew}s (within ${CLOCK_SKEW_SECONDS}s)"
}

prepare_inputs() {
    if [ -z "$IR_RAW" ] || [ -z "$FRAME_RAW" ] || [ -z "$OUT_RAW" ]; then
        refuse "--ir, --frame and --out are required unless --check is used"
    fi
    [ -d "$IR_RAW" ] || refuse "--ir '$IR_RAW' is not a directory"
    [ -f "$FRAME_RAW" ] || refuse "proof frame '$FRAME_RAW' is missing"
    if [ -n "$METADATA_RAW" ] && [ ! -f "$METADATA_RAW" ]; then
        refuse "metadata '$METADATA_RAW' is missing; head_status cannot be verified"
    fi
    case "$OUT_RAW" in
        *.json) ;;
        *) refuse "--out '$OUT_RAW' must end in .json" ;;
    esac

    IR="$(absolute_dir "$IR_RAW")"
    FRAME_DIR="$(absolute_dir "$(dirname -- "$FRAME_RAW")")"
    FRAME="$FRAME_DIR/$(basename -- "$FRAME_RAW")"
    if [ -n "$METADATA_RAW" ]; then
        METADATA_DIR="$(absolute_dir "$(dirname -- "$METADATA_RAW")")"
        METADATA="$METADATA_DIR/$(basename -- "$METADATA_RAW")"
    else
        METADATA="$IR/metadata.yaml"
    fi
    OUT="$(absolute_output "$OUT_RAW")"
    OUT_DIR="$(dirname -- "$OUT")"

    local required
    for required in yolov8n_seg.xml yolov8n_seg.bin; do
        [ -f "$IR/$required" ] || refuse "IR pair incomplete: $IR/$required is missing"
    done
    [ -f "$METADATA" ] || refuse "metadata '$METADATA' is missing; head_status cannot be verified"

    mount_for_readonly "$IR"
    mount_for_readonly "$FRAME"
    if [ "$METADATA" != "$IR/metadata.yaml" ]; then
        mount_for_readonly "$METADATA"
    fi
    if [ -d /sys/devices/system/cpu/cpu0/cpufreq ]; then
        EXTRA_MOUNTS+=(-v /sys/devices/system/cpu/cpu0/cpufreq:/sys/devices/system/cpu/cpu0/cpufreq:ro)
    fi
}

mount_for_readonly() {
    # Repository paths are already covered by the read-only repository mount;
    # anything outside it needs its own bind so the IR and frame stay visible.
    local path="$1"
    case "$path" in
        "$REPO_ROOT"/*) ;;
        *) EXTRA_MOUNTS+=(-v "$path:$path:ro") ;;
    esac
}

run_benchmark() {
    local run_args status repo_mode
    if [ "$OUT_DIR" = "$REPO_ROOT" ]; then
        # Writing evidence at the repository root would collide with the
        # read-only repository mount, so that single mount carries the write.
        repo_mode="rw"
    else
        repo_mode="ro"
    fi
    run_args=(run --rm
        --platform "$PLATFORM"
        --name "projectkv-bench-avx2-$$"
        --cap-drop ALL
        --security-opt no-new-privileges
        --shm-size 1g
        -e "HOME=/tmp"
        -e "TZ=${TZ:-Asia/Ho_Chi_Minh}"
        -w "$REPO_ROOT"
        -v "$REPO_ROOT:$REPO_ROOT:$repo_mode"
    )
    if [ "$OUT_DIR" != "$REPO_ROOT" ]; then
        run_args+=(-v "$OUT_DIR:$OUT_DIR:rw")
    fi
    run_args+=("${USER_ARGS[@]}")
    if [ "${#EXTRA_MOUNTS[@]}" -gt 0 ]; then
        run_args+=("${EXTRA_MOUNTS[@]}")
    fi
    run_args+=(
        "$IMAGE"
        --ir "$IR"
        --frame "$FRAME"
        --out "$OUT"
        --runs "$RUNS"
        --warmup "$WARMUP"
        --repeats "$REPEATS"
    )
    if [ "$METADATA" != "$IR/metadata.yaml" ]; then
        run_args+=(--metadata "$METADATA")
    fi

    log "Running ${REPEATS} x ${RUNS} timed infer() calls at 416 px (${WARMUP} warm-ups) on $IMAGE"
    set +e
    "$RUNTIME" "${run_args[@]}"
    status=$?
    set -e

    case "$status" in
        "$EXIT_GATE_FAILED")
            BENCH_STATUS="$EXIT_GATE_FAILED"
            ;;
        "$EXIT_REFUSED")
            refuse "the benchmark harness refused this run (exit 2); no evidence was written (reason in the log above)"
            ;;
        "$EXIT_OK")
            BENCH_STATUS="$EXIT_OK"
            ;;
        *)
            die "benchmark container exited with status $status"
            ;;
    esac
}

validate_evidence() {
    if [ ! -f "$OUT" ]; then
        die "the benchmark exited but wrote no evidence to $OUT"
    fi
    if ! "$RUNTIME" run --rm \
        --platform "$PLATFORM" \
        --cap-drop ALL \
        --security-opt no-new-privileges \
        -v "$OUT_DIR:$OUT_DIR:ro" \
        --entrypoint python3 \
        "${USER_ARGS[@]}" \
        "$IMAGE" "$EVIDENCE_CHECK" --file "$OUT"; then
        die "evidence at $OUT failed validation; the run cannot close the gate"
    fi
}

# ---------------------------------------------------------------------------
# Arguments
# ---------------------------------------------------------------------------

IR_RAW=""
FRAME_RAW=""
METADATA_RAW=""
OUT_RAW=""
RUNTIME_OVERRIDE=""
IMAGE="$DEFAULT_IMAGE"
CHECK_ONLY=0
NO_BUILD=0
REBUILD=0

need_value() {
    if [ "$#" -lt 2 ] || [ -z "$2" ]; then
        refuse "option $1 requires a value"
    fi
}

while [ "$#" -gt 0 ]; do
    case "$1" in
        --ir) need_value "$@"; IR_RAW="$2"; shift 2 ;;
        --frame) need_value "$@"; FRAME_RAW="$2"; shift 2 ;;
        --metadata) need_value "$@"; METADATA_RAW="$2"; shift 2 ;;
        --out) need_value "$@"; OUT_RAW="$2"; shift 2 ;;
        --image) need_value "$@"; IMAGE="$2"; shift 2 ;;
        --runtime) need_value "$@"; RUNTIME_OVERRIDE="$2"; shift 2 ;;
        --check) CHECK_ONLY=1; shift ;;
        --no-build) NO_BUILD=1; shift ;;
        --rebuild) REBUILD=1; shift ;;
        -h|--help) usage; exit "$EXIT_OK" ;;
        *) refuse "unknown option: $1" ;;
    esac
done

if [ -n "$RUNTIME_OVERRIDE" ]; then
    RUNTIME="$RUNTIME_OVERRIDE"
elif [ -n "${BENCH_RUNTIME:-}" ]; then
    RUNTIME="$BENCH_RUNTIME"
elif command -v docker >/dev/null 2>&1; then
    RUNTIME=docker
elif command -v podman >/dev/null 2>&1; then
    RUNTIME=podman
else
    refuse "no container runtime found (docker or podman); the AVX2 worker image cannot run"
fi

EXTRA_MOUNTS=()
USER_ARGS=()
BENCH_STATUS="$EXIT_OK"

if is_podman; then
    # Rootless podman maps the caller so the bind-mounted evidence directory
    # stays writable; Docker runs as the same numeric uid as the host caller.
    USER_ARGS+=(--userns=keep-id)
else
    USER_ARGS+=(--user "$(id -u):$(id -g)")
fi

# ---------------------------------------------------------------------------
# Guard, measure, validate
# ---------------------------------------------------------------------------

require_runtime
require_x86_runtime

if [ "$CHECK_ONLY" -eq 0 ]; then
    prepare_inputs
fi

ensure_image
probe_container_host
require_clock_agreement

if [ "$CHECK_ONLY" -eq 1 ]; then
    log "Host guard passed: native x86-64 AVX2 runtime is gate-eligible"
    exit "$EXIT_OK"
fi

log "Repository: $REPO_ROOT"
log "IR:         $IR"
log "Frame:      $FRAME"
log "Evidence:   $OUT"

log "Repository: $REPO_ROOT"
log "IR:         $IR"
log "Frame:      $FRAME"
log "Evidence:   $OUT"

run_benchmark
validate_evidence

if [ "$BENCH_STATUS" -eq "$EXIT_GATE_FAILED" ]; then
    log "Gate FAILED: evidence is valid but p95 exceeded the 25.0 ms ceiling; see $OUT"
    exit "$EXIT_GATE_FAILED"
fi

log "Gate PASSED: evidence validated and written to $OUT"
exit "$EXIT_OK"
