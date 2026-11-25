#!/usr/bin/env python3
"""
tools/cv/deploy_edge_int8.py - Secondary Edge Inference INT8 Artifact Verifier & Packager.
Issue #553 / Sprint 19 Task 5.

Enforces AGENTS.md Rule 10 weight integrity gates (size > 1MB, Shannon entropy H >= 3.0,
unique byte diversity >= 32 per 64KB chunk), computes reproducible SHA-256 manifests,
and validates ONNX Runtime CPUExecutionProvider inference execution prior to edge sync.
"""

from __future__ import annotations

import argparse
import collections
import datetime
import hashlib
import json
import logging
import math
import sys
from pathlib import Path
from typing import Any, Dict, Optional

logging.basicConfig(level=logging.INFO, format="%(asctime)s [%(levelname)s] %(message)s")
logger = logging.getLogger("deploy_edge_int8")

MIN_MODEL_SIZE_BYTES = 1_048_576  # 1 MB floor per Rule 10
MIN_SHANNON_ENTROPY = 3.0         # bits/byte floor per Rule 10
MIN_UNIQUE_BYTES_64K = 32         # diversity floor per 64 KB chunk


def compute_shannon_entropy(data: bytes) -> float:
    """Computes Shannon entropy in bits per byte over a byte buffer."""
    if not data:
        return 0.0
    counts = collections.Counter(data)
    length = len(data)
    entropy = 0.0
    for count in counts.values():
        p = count / length
        entropy -= p * math.log2(p)
    return entropy


def verify_model_integrity(model_path: Path) -> Dict[str, Any]:
    """
    Validates model weight file size, SHA-256 digest, Shannon entropy, and byte diversity.
    Raises ValueError if any integrity gate fails.
    """
    if not model_path.is_file():
        raise FileNotFoundError(f"INT8 model artifact not found: {model_path}")

    size_bytes = model_path.stat().st_size
    if size_bytes <= MIN_MODEL_SIZE_BYTES:
        raise ValueError(
            f"Model size {size_bytes} bytes fails Rule 10 >1MB threshold ({MIN_MODEL_SIZE_BYTES} bytes)"
        )

    sha256 = hashlib.sha256()
    sample_chunks = []
    min_chunk_unique = 256

    with open(model_path, "rb") as f:
        while True:
            chunk = f.read(65536)
            if not chunk:
                break
            sha256.update(chunk)
            if len(chunk) == 65536:
                unique_count = len(set(chunk))
                if unique_count < min_chunk_unique:
                    min_chunk_unique = unique_count
            if len(sample_chunks) < 16:  # sample up to 1 MB for entropy calculation
                sample_chunks.append(chunk)

    sample_bytes = b"".join(sample_chunks)
    entropy = compute_shannon_entropy(sample_bytes)

    if entropy < MIN_SHANNON_ENTROPY:
        raise ValueError(
            f"Model Shannon entropy {entropy:.3f} < {MIN_SHANNON_ENTROPY:.1f} bits/byte (stub/zero-filled detected)"
        )

    if min_chunk_unique < MIN_UNIQUE_BYTES_64K:
        raise ValueError(
            f"Model chunk unique byte diversity {min_chunk_unique} < {MIN_UNIQUE_BYTES_64K}"
        )

    return {
        "filename": model_path.name,
        "size_bytes": size_bytes,
        "size_mb": round(size_bytes / (1024 * 1024), 3),
        "sha256": sha256.hexdigest(),
        "shannon_entropy": round(entropy, 4),
        "min_chunk_unique_bytes": min_chunk_unique,
    }


def smoke_test_ort_cpu(model_path: Path, input_size: int = 640) -> Dict[str, Any]:
    """
    Runs a live ONNX Runtime CPUExecutionProvider load and single dummy tensor pass
    if onnxruntime and numpy are available.
    """
    try:
        import numpy as np
        import onnxruntime as ort
    except ImportError as exc:
        logger.warning("onnxruntime or numpy not installed in current env; skipping runtime smoke test: %s", exc)
        return {"executed": False, "reason": str(exc)}

    opts = ort.SessionOptions()
    opts.intra_op_num_threads = 2
    session = ort.InferenceSession(
        str(model_path),
        sess_options=opts,
        providers=["CPUExecutionProvider"],
    )
    inputs = session.get_inputs()
    input_name = inputs[0].name
    shape = [dim if isinstance(dim, int) and dim > 0 else 1 for dim in inputs[0].shape]
    if len(shape) == 4:
        shape = [1, 3, input_size, input_size]

    dummy = np.zeros(shape, dtype=np.float32)
    outputs = session.run(None, {input_name: dummy})

    return {
        "executed": True,
        "providers": session.get_providers(),
        "input_name": input_name,
        "input_shape": shape,
        "output_count": len(outputs),
    }


def build_edge_manifest(
    model_path: Path,
    manifest_out: Optional[Path] = None,
    run_smoke_test: bool = False,
    input_size: int = 640,
) -> Dict[str, Any]:
    """Verifies INT8 artifact integrity and emits reproducible deployment manifest."""
    metrics = verify_model_integrity(model_path)
    smoke_res = {"executed": False}
    if run_smoke_test:
        smoke_res = smoke_test_ort_cpu(model_path, input_size=input_size)

    manifest = {
        "schema_version": "1.0",
        "generated_at": datetime.datetime.now(datetime.timezone.utc).isoformat(),
        "precision": "int8",
        "target_provider": "CPUExecutionProvider",
        "artifact": metrics,
        "smoke_test": smoke_res,
    }

    if manifest_out:
        manifest_out.parent.mkdir(parents=True, exist_ok=True)
        with open(manifest_out, "w", encoding="utf-8") as f:
            json.dump(manifest, f, indent=2)
            f.write("\n")
        logger.info("Edge manifest written to %s", manifest_out)

    return manifest


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--model",
        type=Path,
        default=Path("models/yolov8n_640_int8.onnx"),
        help="Path to INT8 quantized ONNX model artifact",
    )
    parser.add_argument(
        "--manifest-out",
        type=Path,
        default=Path("models/edge_int8_manifest.json"),
        help="Output path for SHA-256 deployment manifest",
    )
    parser.add_argument(
        "--smoke-test",
        action="store_true",
        help="Run ONNX Runtime CPUExecutionProvider inference smoke test",
    )
    parser.add_argument(
        "--input-size",
        type=int,
        default=640,
        help="Input square tensor resolution",
    )
    args = parser.parse_args()

    try:
        manifest = build_edge_manifest(
            model_path=args.model,
            manifest_out=args.manifest_out,
            run_smoke_test=args.smoke_test,
            input_size=args.input_size,
        )
        logger.info(
            "Verified INT8 artifact %s (%.2f MB, H=%.3f, sha256=%s...)",
            manifest["artifact"]["filename"],
            manifest["artifact"]["size_mb"],
            manifest["artifact"]["shannon_entropy"],
            manifest["artifact"]["sha256"][:16],
        )
        return 0
    except Exception as exc:
        logger.error("Edge INT8 verification failed: %s", exc)
        return 1


if __name__ == "__main__":
    sys.exit(main())
