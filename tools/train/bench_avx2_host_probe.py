#!/usr/bin/env python3
"""bench_avx2_host_probe.py — container-side host guard for the AVX2 latency soak.

S12-T5 §2.9 / S13-T1 (#438) requires a latency number from the hardware that
serves inference. ``bench_seg_latency.probe_host()`` already refuses anything
that is not Linux x86-64 with an ``avx2`` CPU flag, but a container can pass
that check while still being emulated: Docker Desktop on Apple Silicon builds a
``linux/amd64`` container whose ``/proc/cpuinfo`` is synthesised, and QEMU can
be told to advertise AVX2 (``-cpu max``). Emulated AVX2 is precisely the
"sub-normal CPU" the spec disqualifies, so this probe adds the check the harness
cannot make from inside a container — the CPU model must not name an emulator.

The runner (``tools/train/bench_avx2_docker.sh``) invokes this inside the
pinned image before any measurement, and independently refuses a container
runtime whose own architecture is not x86-64. Together those two checks mean a
gate-eligible number can only come from a native x86-64 AVX2 machine.

Usage:
    python3 bench_avx2_host_probe.py

Exit codes: 0 accepted, 2 refused. A refusal writes the reason to stderr and a
JSON object to stdout, and never writes an evidence file.

Contract tests: tools/train/bench_avx2_docker_test.py
"""

from __future__ import annotations

import json
import sys
import time
from pathlib import Path
from typing import Any, Dict, Optional, Sequence

sys.path.insert(0, str(Path(__file__).resolve().parent))

import bench_seg_latency as bench  # noqa: E402

EXIT_OK = 0
EXIT_REFUSED = 2

# QEMU's TCG targets advertise themselves as "QEMU Virtual CPU version ...",
# Bochs as "Bochs", and older QEMU/TCG builds as "... TCG ...". A physical AMD
# Ryzen or Intel Xeon model string contains none of these.
EMULATION_MARKERS = ("qemu", "bochs", "virtual cpu", "tcg")


def assert_native_x86(cpu_model: Optional[str]) -> None:
    """Refuse a CPU model that names an emulator or cannot be identified."""
    model = (cpu_model or "").strip()
    lowered = model.lower()
    if not model or lowered == "unknown":
        raise bench.HostRefusedError(
            "host refusal: /proc/cpuinfo exposes no CPU model, so an emulated x86 "
            "container cannot be ruled out; gate-eligible latency requires a native "
            "x86-64 AVX2 machine"
        )
    for marker in EMULATION_MARKERS:
        if marker in lowered:
            raise bench.HostRefusedError(
                f"host refusal: CPU model '{model}' identifies an emulator ({marker}); "
                f"AVX2 under emulation is not the worker's latency and is not "
                f"gate-eligible"
            )


def probe() -> Dict[str, Any]:
    """Return the accepted host plus the container clock reading."""
    host = bench.probe_host()
    assert_native_x86(host.get("cpu_model"))
    return {"host": dict(host), "epoch": int(time.time())}


def main(argv: Optional[Sequence[str]] = None) -> int:
    del argv  # no options: the guard is not configurable
    try:
        payload = probe()
    except bench.HostRefusedError as exc:
        print(str(exc), file=sys.stderr)
        print(json.dumps({"refused": str(exc), "exit_code": EXIT_REFUSED}))
        return EXIT_REFUSED
    print(json.dumps(payload, sort_keys=True))
    return EXIT_OK


if __name__ == "__main__":
    sys.exit(main())
