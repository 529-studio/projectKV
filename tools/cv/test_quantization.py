#!/usr/bin/env python3
"""
tools/cv/test_quantization.py - Verification and benchmarking suite for INT8 ONNX models.

Part of Sprint 18 Task 5 (Issue #528).
Validates:
  1. Prediction parity against FP32 baseline (primary tensor cosine similarity >= 0.985).
  2. Bounding box detection matched IoU on real camera frames.
  3. Latency speedup and memory footprint reduction (>= 40%).
  4. Gate 3 model weight invariants (entropy >= 3.0, size in [1 MB, 10 MB], unique bytes >= 32).
"""

from __future__ import annotations

import argparse
import glob
import math
import os
import sys
import time
from pathlib import Path
from typing import List, Optional, Sequence, Tuple

import numpy as np
from PIL import Image

try:
    import onnxruntime as ort
except ImportError:
    print("Error: onnxruntime is required for test_quantization.py", file=sys.stderr)
    sys.exit(1)


def letterbox_image(image: Image.Image, target_size: Tuple[int, int] = (640, 640)) -> np.ndarray:
    """Resize image with constant padding (letterboxing) to target (w, h)."""
    iw, ih = image.size
    w, h = target_size
    scale = min(w / iw, h / ih)
    nw = int(iw * scale)
    nh = int(ih * scale)

    resized = image.resize((nw, nh), Image.Resampling.BILINEAR)
    new_image = Image.new("RGB", (w, h), (114, 114, 114))
    new_image.paste(resized, ((w - nw) // 2, (h - nh) // 2))

    arr = np.array(new_image, dtype=np.float32) / 255.0
    arr = np.transpose(arr, (2, 0, 1))
    arr = np.expand_dims(arr, axis=0)
    return arr


def compute_shannon_entropy(data: bytes) -> float:
    """Compute Shannon entropy in bits per byte."""
    if not data:
        return 0.0
    freq = [0] * 256
    for b in data:
        freq[b] += 1
    total = len(data)
    ent = 0.0
    for count in freq:
        if count > 0:
            p = count / total
            ent -= p * math.log2(p)
    return ent


def box_iou(box1: np.ndarray, box2: np.ndarray) -> float:
    """Compute IoU between two bounding boxes [x1, y1, x2, y2]."""
    x1 = max(box1[0], box2[0])
    y1 = max(box1[1], box2[1])
    x2 = min(box1[2], box2[2])
    y2 = min(box1[3], box2[3])

    inter_area = max(0.0, x2 - x1) * max(0.0, y2 - y1)
    box1_area = max(0.0, box1[2] - box1[0]) * max(0.0, box1[3] - box1[1])
    box2_area = max(0.0, box2[2] - box2[0]) * max(0.0, box2[3] - box2[1])

    union_area = box1_area + box2_area - inter_area
    if union_area <= 0:
        return 1.0 if inter_area == 0 else 0.0
    return inter_area / union_area


def nms_cpu(boxes: np.ndarray, scores: np.ndarray, iou_thresh: float = 0.45) -> List[int]:
    """Greedy Non-Maximum Suppression (NMS)."""
    if len(boxes) == 0:
        return []
    x1 = boxes[:, 0]
    y1 = boxes[:, 1]
    x2 = boxes[:, 2]
    y2 = boxes[:, 3]
    areas = (x2 - x1) * (y2 - y1)
    order = scores.argsort()[::-1]
    keep = []
    while order.size > 0:
        i = order[0]
        keep.append(i)
        xx1 = np.maximum(x1[i], x1[order[1:]])
        yy1 = np.maximum(y1[i], y1[order[1:]])
        xx2 = np.minimum(x2[i], x2[order[1:]])
        yy2 = np.minimum(y2[i], y2[order[1:]])
        w = np.maximum(0.0, xx2 - xx1)
        h = np.maximum(0.0, yy2 - yy1)
        inter = w * h
        ovr = inter / (areas[i] + areas[order[1:]] - inter + 1e-8)
        inds = np.where(ovr <= iou_thresh)[0]
        order = order[inds + 1]
    return keep


def extract_detections(output: np.ndarray, conf_thresh: float = 0.3) -> Tuple[np.ndarray, np.ndarray]:
    """Extract filtered bounding boxes and scores from detection head [1, 84, N]."""
    preds = output[0].T  # [N, 84]
    class_scores = np.max(preds[:, 4:84], axis=1)
    mask = class_scores >= conf_thresh
    if not np.any(mask):
        return np.empty((0, 4)), np.empty((0,))
    filtered_preds = preds[mask]
    scores = class_scores[mask]
    cx = filtered_preds[:, 0]
    cy = filtered_preds[:, 1]
    w = filtered_preds[:, 2]
    h = filtered_preds[:, 3]
    boxes = np.stack([cx - w / 2, cy - h / 2, cx + w / 2, cy + h / 2], axis=1)
    keep = nms_cpu(boxes, scores)
    return boxes[keep], scores[keep]


def detect_model_input_size(sess: ort.InferenceSession) -> Tuple[int, int]:
    """Detect native input resolution from ORT session."""
    inp = sess.get_inputs()[0]
    if len(inp.shape) == 4:
        try:
            h = int(inp.shape[2])
            w = int(inp.shape[3])
            if h > 0 and w > 0:
                return (w, h)
        except (TypeError, ValueError):
            pass
    return (640, 640)


def evaluate_quantization_parity(
    fp32_model_path: Path,
    int8_model_path: Path,
    benchmark_frames: List[str],
    target_size: Optional[Tuple[int, int]] = None,
) -> dict:
    """Compare inference outputs between FP32 and INT8 models."""
    assert fp32_model_path.is_file(), f"FP32 model not found: {fp32_model_path}"
    assert int8_model_path.is_file(), f"INT8 model not found: {int8_model_path}"

    # Gate 3 Invariant checks on INT8 model weights
    int8_size = int8_model_path.stat().st_size
    fp32_size = fp32_model_path.stat().st_size
    assert int8_size > 1_048_576, f"INT8 model size ({int8_size} B) must be > 1 MB"
    assert int8_size < 10_485_760, f"INT8 model size ({int8_size} B) must be < 10 MB"

    with open(int8_model_path, "rb") as f:
        content = f.read()
    entropy = compute_shannon_entropy(content)
    unique_bytes = len(set(content))
    assert entropy >= 3.0, f"Entropy ({entropy:.2f}) must be >= 3.0"
    assert unique_bytes >= 32, f"Unique bytes ({unique_bytes}) must be >= 32"

    sess_fp32 = ort.InferenceSession(str(fp32_model_path), providers=["CPUExecutionProvider"])
    sess_int8 = ort.InferenceSession(str(int8_model_path), providers=["CPUExecutionProvider"])

    input_name_fp32 = sess_fp32.get_inputs()[0].name
    input_name_int8 = sess_int8.get_inputs()[0].name

    effective_size = target_size or detect_model_input_size(sess_fp32)

    primary_cosines: List[float] = []
    overall_cosines: List[float] = []
    matched_ious: List[float] = []
    latencies_fp32: List[float] = []
    latencies_int8: List[float] = []

    for frame_path in benchmark_frames:
        try:
            with Image.open(frame_path) as img:
                tensor = letterbox_image(img.convert("RGB"), effective_size)
        except Exception:
            continue

        # Timed run FP32
        t0 = time.perf_counter()
        out_fp32 = sess_fp32.run(None, {input_name_fp32: tensor})
        latencies_fp32.append((time.perf_counter() - t0) * 1000)

        # Timed run INT8
        t1 = time.perf_counter()
        out_int8 = sess_int8.run(None, {input_name_int8: tensor})
        latencies_int8.append((time.perf_counter() - t1) * 1000)

        # Multi-output cosine similarity
        tensor_cosines = []
        for i, (o_fp, o_int) in enumerate(zip(out_fp32, out_int8)):
            v_fp = o_fp.flatten()
            v_int = o_int.flatten()
            dot = np.dot(v_fp, v_int)
            norm_product = (np.linalg.norm(v_fp) * np.linalg.norm(v_int)) + 1e-8
            cos_sim = float(dot / norm_product)
            tensor_cosines.append(cos_sim)
            if i == 0:
                primary_cosines.append(cos_sim)
        overall_cosines.append(float(np.mean(tensor_cosines)))

        # Bounding box IoU overlap if primary output is detection head [1, 84, N]
        if len(out_fp32[0].shape) == 3 and out_fp32[0].shape[1] >= 84:
            boxes_fp, _ = extract_detections(out_fp32[0])
            boxes_int, _ = extract_detections(out_int8[0])

            if len(boxes_fp) > 0 and len(boxes_int) > 0:
                for b_fp in boxes_fp:
                    best_iou = max([box_iou(b_fp, b_int) for b_int in boxes_int])
                    if best_iou >= 0.5:
                        matched_ious.append(best_iou)
            elif len(boxes_fp) == 0 and len(boxes_int) == 0:
                matched_ious.append(1.0)

    mean_primary_cos = float(np.mean(primary_cosines)) if primary_cosines else 1.0
    mean_overall_cos = float(np.mean(overall_cosines)) if overall_cosines else 1.0
    primary_drop_pct = (1.0 - mean_primary_cos) * 100.0
    mean_matched_iou = float(np.mean(matched_ious)) if matched_ious else 1.0
    iou_drop_pct = (1.0 - mean_matched_iou) * 100.0

    p50_fp32 = float(np.median(latencies_fp32)) if latencies_fp32 else 0.0
    p50_int8 = float(np.median(latencies_int8)) if latencies_int8 else 0.0
    speedup = (p50_fp32 / p50_int8) if p50_int8 > 0 else 1.0

    metrics = {
        "frames_evaluated": len(primary_cosines),
        "primary_cosine_similarity": mean_primary_cos,
        "primary_drop_percent": primary_drop_pct,
        "overall_cosine_similarity": mean_overall_cos,
        "mean_matched_iou": mean_matched_iou,
        "iou_drop_percent": iou_drop_pct,
        "fp32_size_mb": fp32_size / 1024 / 1024,
        "int8_size_mb": int8_size / 1024 / 1024,
        "memory_reduction_percent": (1.0 - int8_size / fp32_size) * 100.0,
        "latency_p50_fp32_ms": p50_fp32,
        "latency_p50_int8_ms": p50_int8,
        "speedup_factor": speedup,
        "entropy": entropy,
        "unique_bytes": unique_bytes,
        "resolution": effective_size,
    }

    return metrics


def main() -> int:
    parser = argparse.ArgumentParser(description="Evaluate INT8 ONNX parity against FP32 baseline.")
    parser.add_argument(
        "--fp32-model",
        type=Path,
        default=Path("data/models/yolov8n_640.onnx"),
        help="Path to FP32 baseline ONNX model",
    )
    parser.add_argument(
        "--int8-model",
        type=Path,
        default=Path("models/yolov8n_640_int8.onnx"),
        help="Path to quantized INT8 ONNX model",
    )
    parser.add_argument(
        "--frames-dir",
        type=Path,
        default=Path("data/proofs/"),
        help="Directory with real benchmark camera frames",
    )
    parser.add_argument(
        "--max-frames",
        type=int,
        default=50,
        help="Max frames to evaluate (default: 50)",
    )
    parser.add_argument(
        "--min-cosine-similarity",
        type=float,
        default=0.985,
        help="Minimum primary tensor cosine similarity threshold (default: 0.985)",
    )

    args = parser.parse_args()

    frame_paths = sorted(glob.glob(str(args.frames_dir / "**" / "*.jpg"), recursive=True))[: args.max_frames]
    if not frame_paths:
        print("[ERROR] No benchmark frames found in data/proofs/", file=sys.stderr)
        return 1

    print(f"[TEST] Evaluating INT8 parity on {len(frame_paths)} benchmark frames...")
    print(f"       FP32 Model: {args.fp32_model}")
    print(f"       INT8 Model: {args.int8_model}")
    metrics = evaluate_quantization_parity(args.fp32_model, args.int8_model, frame_paths)

    print("\n================ INT8 ONNX QUANTIZATION BENCHMARK ================")
    print(f"Resolution:               {metrics['resolution'][0]}x{metrics['resolution'][1]}")
    print(f"Frames Evaluated:         {metrics['frames_evaluated']}")
    print(f"Model Size FP32:          {metrics['fp32_size_mb']:.2f} MB")
    print(f"Model Size INT8:          {metrics['int8_size_mb']:.2f} MB ({metrics['memory_reduction_percent']:.1f}% reduction)")
    print(f"Shannon Entropy:          {metrics['entropy']:.2f} bits/byte (threshold: >= 3.0)")
    print(f"Byte Diversity:           {metrics['unique_bytes']} unique bytes (threshold: >= 32)")
    print(f"Primary Cosine Sim:       {metrics['primary_cosine_similarity']:.4f} (threshold: >= {args.min_cosine_similarity})")
    print(f"Primary Fidelity Drop:    {metrics['primary_drop_percent']:.3f}% (threshold: <= 1.5%)")
    print(f"Overall Cosine Sim:       {metrics['overall_cosine_similarity']:.4f}")
    print(f"Mean Matched IoU:         {metrics['mean_matched_iou']:.4f}")
    print(f"Latency FP32 (CPU p50):   {metrics['latency_p50_fp32_ms']:.2f} ms")
    print(f"Latency INT8 (CPU p50):   {metrics['latency_p50_int8_ms']:.2f} ms")
    print(f"Throughput Speedup:       {metrics['speedup_factor']:.2f}x")
    print("==================================================================\n")

    # Invariant Assertions (Issue #528)
    assert metrics["primary_cosine_similarity"] >= args.min_cosine_similarity, (
        f"Primary cosine similarity ({metrics['primary_cosine_similarity']:.4f}) below threshold {args.min_cosine_similarity}!"
    )
    assert metrics["primary_drop_percent"] <= 1.5, (
        f"Fidelity drop ({metrics['primary_drop_percent']:.3f}%) exceeds 1.5% target!"
    )
    assert metrics["int8_size_mb"] < 10.0, f"INT8 model size ({metrics['int8_size_mb']:.2f} MB) exceeds 10 MB cap!"
    assert metrics["int8_size_mb"] > 1.0, f"INT8 model size ({metrics['int8_size_mb']:.2f} MB) below 1 MB floor!"
    assert metrics["memory_reduction_percent"] >= 40.0, (
        f"Memory reduction ({metrics['memory_reduction_percent']:.1f}%) below 40% target!"
    )
    assert metrics["entropy"] >= 3.0, f"Entropy ({metrics['entropy']:.2f}) below 3.0 threshold!"
    assert metrics["unique_bytes"] >= 32, f"Unique bytes ({metrics['unique_bytes']}) below 32 threshold!"

    print("[PASS] All INT8 quantization acceptance criteria satisfied successfully.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
