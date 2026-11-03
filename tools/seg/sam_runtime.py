#!/usr/bin/env python3
"""
sam_runtime.py — ONNX Runtime wrappers for the SAM pre-labeling toolchain.

Encapsulates three model runtimes so the pipeline logic in prelabel_sam.py
stays testable with duck-typed fake sessions:
- SamOnnxModel: MobileSAM-class image encoder + promptable mask decoder
  (standard segment-anything ONNX interface; Acly/MobileSAM export).
- YoloOnnxModel: YOLOv8 detection ONNX export for vehicle bounding boxes.

Weight paths are resolved from the environment per AGENTS.md Rule 8; this
module never embeds filesystem defaults pointing at real weights and never
downloads anything.

Adheres to AGENTS.md Rules:
- Rule 1: zero-emoji, 100% English code and logs.
- Rule 8: weights loaded from environment-provided paths only.
"""

import logging
import os
from typing import Any, Dict, List, Optional, Sequence, Tuple

import numpy as np

logger = logging.getLogger(__name__)

# SAM preprocessing constants (segment-anything convention).
SAM_INPUT_SIZE = 1024
SAM_PIXEL_MEAN = np.array([123.675, 116.28, 103.53], dtype=np.float32)
SAM_PIXEL_STD = np.array([58.395, 57.12, 57.375], dtype=np.float32)

# Point prompt label codes (segment-anything ONNX decoder convention).
POINT_LABEL_EXCLUDE = -1
POINT_LABEL_INCLUDE = 1
POINT_LABEL_BOX_TOPLEFT = 2
POINT_LABEL_BOX_BOTTOMRIGHT = 3

# Environment variables holding weight paths (AGENTS.md Rule 8).
ENV_ENCODER = "SAM_PRELABEL_ENCODER_ONNX"
ENV_DECODER = "SAM_PRELABEL_DECODER_ONNX"
ENV_DETECTOR = "SAM_PRELABEL_DETECTOR_ONNX"


def resolve_weight_path(env_key: str, arg_path: Optional[str] = None) -> str:
    """
    Resolves a model weight path from an explicit CLI argument or the
    environment. Returns "" when unset (caller must fail fast per Rule 8).
    """
    if arg_path:
        return arg_path
    return os.getenv(env_key, "")


class SamOnnxModel:
    """
    Promptable segmentation model over MobileSAM-class ONNX weights.

    The encoder accepts a 1024x1024 RGB float32 HWC image (aspect-preserving
    resize + pad done by `preprocess_frame`) and yields image embeddings; the
    decoder consumes embeddings plus point/box prompts and returns boolean
    masks resized to the original frame size.
    """

    def __init__(self, encoder_path: str, decoder_path: str, session_factory=None):
        if not encoder_path or not decoder_path:
            raise RuntimeError(
                "SAM weights not provisioned: set %s and %s in .env (Rule 8: no hardcoded paths)"
                % (ENV_ENCODER, ENV_DECODER)
            )
        if session_factory is None:
            import onnxruntime as ort

            def session_factory(path: str) -> Any:
                return ort.InferenceSession(path, providers=["CPUExecutionProvider"])

        self.encoder = session_factory(encoder_path)
        self.decoder = session_factory(decoder_path)
        self._encoder_input = self.encoder.get_inputs()[0].name
        self._decoder_inputs = {i.name: i for i in self.decoder.get_inputs()}

    def preprocess_frame(self, img_rgb: np.ndarray) -> Tuple[np.ndarray, float, Tuple[int, int]]:
        """
        Aspect-preserving resize of an HWC RGB uint8 frame to the SAM input
        canvas (longest side 1024, zero-pad to square).

        Returns (canvas float32 HWC, scale, (pad_x, pad_y)) so prompt points
        can be mapped into the 1024 input space.
        """
        h, w = img_rgb.shape[:2]
        scale = min(SAM_INPUT_SIZE / h, SAM_INPUT_SIZE / w)
        new_h, new_w = max(1, int(round(h * scale))), max(1, int(round(w * scale)))
        resized = np.asarray(img_rgb, dtype=np.float32)
        from tools.seg.image_geometry import _resize_bilinear

        resized = _resize_bilinear(resized, new_h, new_w)
        canvas = np.zeros((SAM_INPUT_SIZE, SAM_INPUT_SIZE, 3), dtype=np.float32)
        pad_x = (SAM_INPUT_SIZE - new_w) // 2
        pad_y = (SAM_INPUT_SIZE - new_h) // 2
        canvas[pad_y:pad_y + new_h, pad_x:pad_x + new_w, :] = resized
        return canvas, scale, (pad_x, pad_y)

    def encode(self, canvas: np.ndarray) -> np.ndarray:
        """Runs the image encoder over a preprocessed 1024 canvas (HWC float)."""
        out = self.encoder.run(None, {self._encoder_input: canvas})
        return np.asarray(out[0])

    def _decode(
        self,
        embeddings: np.ndarray,
        coords: np.ndarray,
        labels: np.ndarray,
        orig_h: int,
        orig_w: int,
    ) -> Tuple[np.ndarray, np.ndarray]:
        """Runs the mask decoder; returns (masks bool at orig size, iou scores)."""
        n = coords.shape[1]
        feed: Dict[str, Any] = {
            "image_embeddings": embeddings,
            "point_coords": coords.astype(np.float32),
            "point_labels": labels.astype(np.float32),
            "mask_input": np.zeros((1, 1, 256, 256), dtype=np.float32),
            "has_mask_input": np.zeros(1, dtype=np.float32),
            "orig_im_size": np.array([float(orig_h), float(orig_w)], dtype=np.float32),
        }
        # Tolerate decoder exports with extra/renamed optional inputs.
        for name in list(feed):
            if name not in self._decoder_inputs:
                logger.warning("Decoder does not declare input %s; skipping it", name)
                feed.pop(name)
        out = self.decoder.run(None, feed)
        masks = np.asarray(out[0])[0]  # (num_masks, H, W)
        scores = np.asarray(out[1])[0]  # (num_masks,)
        return masks > 0.0, scores

    @staticmethod
    def _to_input_space(
        norm_xy: Sequence[float],
        scale: float,
        pads: Tuple[int, int],
        orig_w: int,
        orig_h: int,
    ) -> Tuple[float, float]:
        """
        Maps a normalized [0, 1] frame point into the padded 1024 canvas:
        stretch by scale into the (new_w, new_h) window, then shift by the
        zero-pad offset.
        """
        x = float(norm_xy[0]) * orig_w * scale + pads[0]
        y = float(norm_xy[1]) * orig_h * scale + pads[1]
        return x, y

    def segment_points(
        self,
        embeddings: np.ndarray,
        points_norm: Sequence[Sequence[float]],
        scale: float,
        pads: Tuple[int, int],
        orig_h: int,
        orig_w: int,
    ) -> Tuple[np.ndarray, float]:
        """
        Segments from include-point prompts (normalized frame coordinates).
        Returns the best (highest-IoU-score) boolean mask at original size.
        """
        coords, labels = [], []
        for p in points_norm:
            coords.append(self._to_input_space(p, scale, pads, orig_w, orig_h))
            labels.append(POINT_LABEL_INCLUDE)
        masks, scores = self._decode(
            embeddings,
            np.array([coords], dtype=np.float32),
            np.array([labels], dtype=np.float32),
            orig_h,
            orig_w,
        )
        best = int(np.argmax(scores))
        return masks[best], float(scores[best])

    def segment_box(
        self,
        embeddings: np.ndarray,
        box_norm: Sequence[float],
        scale: float,
        pads: Tuple[int, int],
        orig_h: int,
        orig_w: int,
    ) -> Tuple[np.ndarray, float]:
        """
        Segments from a box prompt (normalized frame xyxy coordinates).
        Returns the best boolean mask at original size and its IoU score.
        """
        x1, y1, x2, y2 = [float(v) for v in box_norm]
        p1 = self._to_input_space((x1, y1), scale, pads, orig_w, orig_h)
        p2 = self._to_input_space((x2, y2), scale, pads, orig_w, orig_h)
        coords = np.array([[p1, p2]], dtype=np.float32)
        labels = np.array([[POINT_LABEL_BOX_TOPLEFT, POINT_LABEL_BOX_BOTTOMRIGHT]], dtype=np.float32)
        masks, scores = self._decode(embeddings, coords, labels, orig_h, orig_w)
        best = int(np.argmax(scores))
        return masks[best], float(scores[best])


class YoloOnnxModel:
    """
    YOLOv8 detection ONNX wrapper: letterbox input, decode (1, 4+K, N) head,
    confidence filter + NMS, and COCO-to-taxonomy class mapping.
    """

    def __init__(self, model_path: str, session_factory=None):
        if not model_path:
            raise RuntimeError(
                "Detector weights not provisioned: set %s in .env (Rule 8: no hardcoded paths)"
                % ENV_DETECTOR
            )
        if session_factory is None:
            import onnxruntime as ort

            def session_factory(path: str) -> Any:
                return ort.InferenceSession(path, providers=["CPUExecutionProvider"])

        self.session = session_factory(model_path)
        inp = self.session.get_inputs()[0]
        self.input_name = inp.name
        # Static exports pin (1, 3, S, S); dynamic exports carry strings.
        shape = inp.shape
        self.input_size = int(shape[2]) if isinstance(shape[2], int) else 640

    def detect(
        self, img_rgb: np.ndarray, conf: float = 0.25, iou: float = 0.45
    ) -> List[Dict[str, Any]]:
        """
        Runs detection over an HWC RGB uint8 frame. Returns dicts with keys
        class (canonical taxonomy), box_norm ([x1, y1, x2, y2] in [0, 1]),
        and score.
        """
        from tools.seg.contract import COCO_TO_TAXONOMY
        from tools.seg.image_geometry import (
            letterbox,
            nms_xyxy,
            scale_boxes_back,
        )

        h, w = img_rgb.shape[:2]
        canvas, scale, pads = letterbox(img_rgb, self.input_size)
        blob = canvas.transpose(2, 0, 1)[None].astype(np.float32)
        out = np.asarray(self.session.run(None, {self.input_name: blob})[0])

        # Standard Ultralytics export: (1, 4 + num_classes, N) channels-first.
        # Transpose when the leading axis is the smaller, channel-like one.
        pred = out[0] if out.ndim == 3 else out
        if pred.ndim == 2 and pred.shape[0] < pred.shape[1] and pred.shape[0] >= 5:
            pred = pred.T
        boxes_cxcywh = pred[:, :4]
        class_scores = pred[:, 4:]

        class_ids = np.argmax(class_scores, axis=1)
        confidences = class_scores[np.arange(len(class_ids)), class_ids]
        mask = confidences >= conf
        if not mask.any():
            return []

        kept_raw = np.where(mask)[0]
        boxes_xyxy = scale_boxes_back(boxes_cxcywh[kept_raw], scale, pads, h, w)
        keep = nms_xyxy(boxes_xyxy, confidences[kept_raw], iou_thresh=iou)

        results: List[Dict[str, Any]] = []
        for idx in keep:
            raw_idx = int(kept_raw[idx])
            taxonomy = COCO_TO_TAXONOMY.get(int(class_ids[raw_idx]))
            if taxonomy is None:
                continue
            x1, y1, x2, y2 = boxes_xyxy[idx]
            results.append(
                {
                    "class": taxonomy,
                    "box_norm": [x1 / w, y1 / h, x2 / w, y2 / h],
                    "score": float(confidences[raw_idx]),
                }
            )
        return results
