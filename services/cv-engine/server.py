#!/usr/bin/env python3
"""
ProjectKV CV Engine — Standalone Computer Vision Inference Microservice.
Evaluated and validated in Spike #104 (docs/spike-cv-runtime.md) for non-AVX hardware.
Runs Intel OpenVINO CPU plugin with SSE4.2 vector instructions.
"""

from __future__ import annotations

import json
import logging
import os
import sys
import time
from http.server import ThreadingHTTPServer, BaseHTTPRequestHandler
from io import BytesIO
from typing import Any, Dict, List, Optional, Tuple

logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s [%(levelname)s] %(name)s: %(message)s",
    handlers=[logging.StreamHandler(sys.stdout)]
)
logger = logging.getLogger("cv-engine")

PORT = int(os.environ.get("CV_PORT", os.environ.get("MODEL_PORT", 8000)))
MODEL_DIR = os.environ.get("MODEL_DIR", "/app/models/yolov8n_openvino_320")

# Attempt to import optional OpenVINO and imaging libraries
OPENVINO_AVAILABLE = False
PIL_AVAILABLE = False
core = None
compiled_model = None

try:
    import openvino as ov
    core = ov.Core()
    OPENVINO_AVAILABLE = True
    logger.info("OpenVINO Core initialized successfully (Version: %s)", ov.__version__)
except ImportError:
    logger.warning("OpenVINO package not installed. Running in standby/mock mode.")

try:
    from PIL import Image
    import numpy as np
    PIL_AVAILABLE = True
except ImportError:
    logger.warning("Pillow/NumPy not installed. Image decoding in standby mode.")


def load_model():
    """Attempt to load and compile OpenVINO model if model files exist."""
    global compiled_model
    if not OPENVINO_AVAILABLE or not core:
        return

    # Look for segmentation model first, then detection model
    candidate_xmls = [
        os.path.join(MODEL_DIR, "yolov8n_seg.xml"),
        os.path.join(MODEL_DIR, "yolov8n.xml"),
    ]
    model_xml = None
    for candidate in candidate_xmls:
        if os.path.isfile(candidate):
            model_xml = candidate
            break

    if not model_xml:
        # Look for any .xml file in MODEL_DIR
        if os.path.isdir(MODEL_DIR):
            xml_files = [f for f in os.listdir(MODEL_DIR) if f.endswith(".xml")]
            if xml_files:
                model_xml = os.path.join(MODEL_DIR, xml_files[0])
            else:
                logger.info("No OpenVINO model XML found in %s. Ready for dynamic model mounting.", MODEL_DIR)
                return
        else:
            logger.info("Model directory %s does not exist. Ready for dynamic model mounting.", MODEL_DIR)
            return

    try:
        start_t = time.perf_counter()
        logger.info("Loading OpenVINO model from %s...", model_xml)
        model = core.read_model(model=model_xml)
        compiled_model = core.compile_model(model=model, device_name="CPU")
        duration = (time.perf_counter() - start_t) * 1000
        logger.info("OpenVINO model compiled successfully on CPU in %.1f ms", duration)
    except Exception as exc:
        logger.error("Failed to compile OpenVINO model: %s", exc)


def letterbox(image, target_size=(416, 416), fill_color=(114, 114, 114)):
    """Resizes image with aspect-ratio preserving padding (letterboxing)."""
    w, h = image.size
    target_w, target_h = target_size
    scale = min(target_w / w, target_h / h)
    nw, nh = int(round(w * scale)), int(round(h * scale))
    resample_filter = getattr(Image, "Resampling", Image).BILINEAR
    image_resized = image.resize((nw, nh), resample_filter)
    new_image = Image.new("RGB", target_size, fill_color)
    pad_x = (target_w - nw) // 2
    pad_y = (target_h - nh) // 2
    new_image.paste(image_resized, (pad_x, pad_y))
    return new_image, scale, pad_x, pad_y


def nms_cpu(boxes: np.ndarray, scores: np.ndarray, iou_thresh: float = 0.45):
    """
    Applies standard Non-Maximum Suppression on bounding boxes.
    boxes: np.ndarray of shape (N, 4) in [cx, cy, w, h] format.
    scores: np.ndarray of shape (N,) containing class confidences.
    iou_thresh: float threshold above which overlapping boxes are suppressed.
    """
    if len(boxes) == 0:
        return []

    x1 = boxes[:, 0] - boxes[:, 2] / 2.0
    y1 = boxes[:, 1] - boxes[:, 3] / 2.0
    x2 = boxes[:, 0] + boxes[:, 2] / 2.0
    y2 = boxes[:, 1] + boxes[:, 3] / 2.0
    areas = np.maximum(0.0, x2 - x1) * np.maximum(0.0, y2 - y1)

    order = scores.argsort()[::-1]
    keep = []
    while order.size > 0:
        i = order[0]
        keep.append(int(i))

        xx1 = np.maximum(x1[i], x1[order[1:]])
        yy1 = np.maximum(y1[i], y1[order[1:]])
        xx2 = np.minimum(x2[i], x2[order[1:]])
        yy2 = np.minimum(y2[i], y2[order[1:]])

        w = np.maximum(0.0, xx2 - xx1)
        h = np.maximum(0.0, yy2 - yy1)
        inter = w * h
        union = areas[i] + areas[order[1:]] - inter
        iou = np.where(union > 0, inter / union, 0.0)

        inds = np.where(iou <= iou_thresh)[0]
        order = order[inds + 1]
    return keep


def decode_mask_prototypes(
    mask_coeffs: np.ndarray,
    proto: np.ndarray,
    binarize_thresh: float = 0.5,
) -> np.ndarray:
    """
    Decodes mask prototypes into binary 2D spatial masks using NumPy BLAS.
    mask_coeffs: shape (K, 32) where K is number of candidates.
    proto: shape (32, H_proto, W_proto) or (1, 32, H_proto, W_proto).
    Returns binary mask array of shape (K, H_proto, W_proto).
    """
    if mask_coeffs.size == 0 or proto.size == 0:
        return np.zeros((0, 104, 104), dtype=bool)

    if proto.ndim == 4:
        proto = proto[0]

    num_protos, h_proto, w_proto = proto.shape
    proto_flat = proto.reshape(num_protos, h_proto * w_proto)

    logits = np.matmul(mask_coeffs, proto_flat)
    sigmoids = 1.0 / (1.0 + np.exp(-np.clip(logits, -12.0, 12.0)))
    masks = sigmoids.reshape(-1, h_proto, w_proto)
    return masks > binarize_thresh


def calculate_water_ratio(
    water_mask: np.ndarray,
    road_mask: Optional[np.ndarray] = None,
) -> float:
    """
    Layer 1: Roadway Surface Water Coverage Ratio (ADR-009 §5.1).
    R_water = sum(M_water & M_road) / sum(M_road)
    Fallback: sum(M_water) / total_pixels if road_mask is empty/missing.
    """
    if water_mask is None or water_mask.size == 0:
        return 0.0

    if road_mask is not None and road_mask.size == water_mask.size:
        road_sum = float(np.sum(road_mask))
        if road_sum > 0:
            intersection = np.logical_and(water_mask, road_mask)
            return float(np.sum(intersection)) / road_sum

    total_pixels = float(water_mask.size)
    if total_pixels <= 0:
        return 0.0
    return float(np.sum(water_mask)) / total_pixels


def calculate_motorcycle_submersion(
    motorcycle_boxes: np.ndarray,
    water_mask: np.ndarray,
    target_img_size: Tuple[int, int] = (416, 416),
) -> Tuple[float, float, int]:
    """
    Layer 2: Motorcycle Wheel Submersion Ratio & Physical Depth Estimation (ADR-009 §5.2).
    motorcycle_boxes: np.ndarray of shape (N, 4) in [cx, cy, w, h] format.
    water_mask: binary 2D array of shape (H_proto, W_proto), e.g. (104, 104).
    Returns:
      (max_delta_sub, max_depth_cm, submerged_motorcycle_count)
    """
    if motorcycle_boxes is None or len(motorcycle_boxes) == 0 or water_mask is None or water_mask.size == 0:
        return 0.0, 0.0, 0

    h_grid, w_grid = water_mask.shape
    h_img, w_img = target_img_size

    scale_x = w_grid / float(w_img)
    scale_y = h_grid / float(h_img)

    max_delta = 0.0
    submerged_count = 0

    boxes_xyxy = []
    for b in motorcycle_boxes:
        if len(b) != 4:
            continue
        cx, cy, w, h = b[0], b[1], b[2], b[3]
        if w > 0 and h > 0:
            xmin = max(0.0, cx - w / 2.0)
            ymin = max(0.0, cy - h / 2.0)
            xmax = min(float(w_img), cx + w / 2.0)
            ymax = min(float(h_img), cy + h / 2.0)
            boxes_xyxy.append((xmin, ymin, xmax, ymax))

    for xmin, ymin, xmax, ymax in boxes_xyxy:
        box_h = ymax - ymin
        if box_h <= 0:
            continue

        gx1 = int(round(xmin * scale_x))
        gx2 = int(round(xmax * scale_x))
        gy1 = int(round(ymin * scale_y))
        gy2 = int(round(ymax * scale_y))

        gx1 = max(0, min(w_grid - 1, gx1))
        gx2 = max(gx1 + 1, min(w_grid, gx2))
        gy1 = max(0, min(h_grid - 1, gy1))
        gy2 = max(gy1 + 1, min(h_grid, gy2))

        sub_region = water_mask[gy1:gy2, gx1:gx2]

        if np.any(sub_region):
            water_rows, _ = np.where(sub_region)
            min_water_row = np.min(water_rows) + gy1
            y_waterline = min_water_row / scale_y
            delta = (ymax - y_waterline) / box_h
            delta = max(0.0, min(1.0, float(delta)))
            if delta > 0.05:
                submerged_count += 1
            if delta > max_delta:
                max_delta = delta

    max_depth_cm = round(max_delta * 110.0, 1)
    return round(max_delta, 4), max_depth_cm, submerged_count


def triangulate_flood_verdict(
    water_ratio: float,
    max_delta_sub: float,
    depth_estimated_cm: float,
    motorcycles_observed: int = 0,
    rain_event: bool = False,
) -> Tuple[str, float]:
    """
    Dual-Layer Physical Triangulation Decision Engine (ADR-009 §5).
    Layer 1: Road water ratio >= 0.15 activates Layer 2.
    Layer 2: delta_sub >= 0.18 (depth >= 20cm) triggers FLOODED_DEEP.
    Returns (verdict_str, confidence_float).
    """
    if water_ratio >= 0.15 and (max_delta_sub >= 0.18 or depth_estimated_cm >= 20.0):
        conf = min(0.95, 0.85 + (water_ratio * 0.2))
        return "FLOODED_DEEP", round(conf, 2)

    if water_ratio >= 0.15:
        conf = min(0.92, 0.80 + (water_ratio * 0.2))
        return "FLOODED_SHALLOW", round(conf, 2)

    if motorcycles_observed == 0 and water_ratio >= 0.35:
        return "FLOODED_SHALLOW", 0.82

    if water_ratio >= 0.05:
        return "WET", 0.80

    if rain_event:
        return "WET", 0.75

    return "CLEAR", 0.95


class CVServer(ThreadingHTTPServer):
    """Multi-threaded HTTP server with resilient socket disconnect handling."""
    daemon_threads = True

    def handle_error(self, request, client_address):
        # Ignore broken pipe and connection reset errors caused by abrupt client disconnections
        exc_type, exc_val, _ = sys.exc_info()
        if exc_type in (BrokenPipeError, ConnectionResetError):
            logger.warning("Client %s disconnected abruptly: %s", client_address, exc_val)
            return
        super().handle_error(request, client_address)


class CVRequestHandler(BaseHTTPRequestHandler):
    """HTTP request handler for CV inference and health monitoring."""

    def _send_json(self, status_code: int, data: dict):
        try:
            body = json.dumps(data).encode("utf-8")
            self.send_response(status_code)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)
        except (BrokenPipeError, ConnectionResetError) as exc:
            logger.warning("Client disconnected before response could be sent: %s", exc)

    def do_GET(self):
        if self.path in ("/healthz", "/health"):
            self._send_json(200, {
                "status": "healthy",
                "service": "cv-engine",
                "openvino_available": OPENVINO_AVAILABLE,
                "model_loaded": compiled_model is not None,
                "timestamp": int(time.time())
            })
            return

        if self.path == "/info":
            devices = core.available_devices if OPENVINO_AVAILABLE and core else []
            self._send_json(200, {
                "service": "projectkv-cv-engine",
                "version": "1.0.0",
                "openvino_available": OPENVINO_AVAILABLE,
                "available_devices": devices,
                "model_dir": MODEL_DIR,
                "model_loaded": compiled_model is not None
            })
            return

        self._send_json(404, {"error": "not found"})

    def do_HEAD(self):
        try:
            if self.path in ("/healthz", "/info"):
                self.send_response(200)
                self.send_header("Content-Type", "application/json")
                self.end_headers()
                return
            self.send_response(404)
            self.end_headers()
        except (BrokenPipeError, ConnectionResetError) as exc:
            logger.warning("Client disconnected during HEAD request: %s", exc)

    def do_POST(self):
        if self.path in ("/predict", "/analyze"):
            content_length = int(self.headers.get("Content-Length", 0))
            if content_length == 0:
                self._send_json(400, {"error": "empty request body"})
                return

            try:
                post_data = self.rfile.read(content_length)
            except (BrokenPipeError, ConnectionResetError) as exc:
                logger.warning("Client disconnected while reading request body: %s", exc)
                return
            start_t = time.perf_counter()

            # If model is loaded and image decoding is available, execute inference
            if compiled_model is not None and PIL_AVAILABLE:
                try:
                    image = Image.open(BytesIO(post_data)).convert("RGB")
                    target_w, target_h = 416, 416
                    if compiled_model is not None:
                        try:
                            in_shape = compiled_model.input(0).shape
                            if len(in_shape) == 4:
                                target_h = int(in_shape[2])
                                target_w = int(in_shape[3])
                        except Exception:
                            pass

                    image_letterboxed, scale, pad_x, pad_y = letterbox(image, target_size=(target_w, target_h))
                    input_tensor = np.expand_dims(np.array(image_letterboxed).transpose((2, 0, 1)), axis=0).astype(np.float32) / 255.0

                    infer_request = compiled_model.create_infer_request()
                    results = infer_request.infer({0: input_tensor})

                    output_data = None
                    proto_data = None
                    if isinstance(results, dict):
                        output_data = results.get(0) or results.get("output0")
                        proto_data = results.get(1) or results.get("output1")
                        if output_data is None and len(results) > 0:
                            vals = list(results.values())
                            output_data = vals[0]
                            if len(vals) > 1:
                                proto_data = vals[1]
                    elif hasattr(results, "__getitem__"):
                        try:
                            output_data = results[0]
                            if len(results) > 1:
                                proto_data = results[1]
                        except Exception:
                            pass
                    if output_data is None and results is not None:
                        output_data = results

                    latency = (time.perf_counter() - start_t) * 1000

                    vehicle_counts = {
                        "motorcycle": 0,
                        "car": 0,
                        "bus": 0,
                        "truck": 0,
                        "container": 0,
                    }

                    CONF_THRESH = 0.25
                    IOU_THRESH = 0.45
                    vehicles = 0
                    kept_motorcycles = []
                    water_ratio = 0.0
                    max_delta_sub = 0.0
                    depth_estimated_cm = 0.0
                    submerged_mc = 0
                    puddle_count = 0
                    model_tag = f"yolov8n_openvino_{target_w}"

                    if output_data is not None and hasattr(output_data, "shape"):
                        if output_data.ndim == 3:
                            if output_data.shape[1] > output_data.shape[2]:
                                data = output_data[0].T
                            else:
                                data = output_data[0]
                        else:
                            data = output_data

                        num_channels = data.shape[0] if data.ndim == 2 else 0

                        if data.ndim == 2 and num_channels == 42:
                            # Fine-tuned 6-class YOLOv8n-seg model (ADR-009):
                            # Classes: 0: roadway, 1: water_puddle, 2: motorcycle, 3: car, 4: bus, 5: truck
                            # Rows: 0..3 box, 4..9 classes, 10..41 (32 mask coeffs)
                            model_tag = f"yolov8n_seg_openvino_{target_w}"
                            seg_classes = {
                                "motorcycle": 6,
                                "car": 7,
                                "bus": 8,
                                "truck": 9,
                            }
                            for cls_name, row_idx in seg_classes.items():
                                scores = data[row_idx, :]
                                valid_mask = scores > CONF_THRESH
                                if np.any(valid_mask):
                                    valid_indices = np.where(valid_mask)[0]
                                    candidate_boxes = np.column_stack([
                                        data[0, valid_indices],
                                        data[1, valid_indices],
                                        data[2, valid_indices],
                                        data[3, valid_indices],
                                    ])
                                    candidate_scores = scores[valid_indices]
                                    keep = nms_cpu(candidate_boxes, candidate_scores, iou_thresh=IOU_THRESH)
                                    vehicle_counts[cls_name] = len(keep)
                                    if cls_name == "motorcycle" and len(keep) > 0:
                                        kept_motorcycles = candidate_boxes[keep]
                                else:
                                    vehicle_counts[cls_name] = 0

                            vehicle_counts["container"] = 0
                            vehicles = sum(vehicle_counts.values())

                            # Segmentation mask decoding & triangulation
                            road_mask = None
                            water_mask = None
                            if proto_data is not None:
                                # Roadway mask (class 0, row 4)
                                road_scores = data[4, :]
                                valid_road = road_scores > CONF_THRESH
                                if np.any(valid_road):
                                    road_indices = np.where(valid_road)[0]
                                    road_coeffs = data[10:42, road_indices].T
                                    road_masks = decode_mask_prototypes(road_coeffs, proto_data, binarize_thresh=0.5)
                                    road_mask = np.any(road_masks, axis=0)

                                # Water puddle mask (class 1, row 5)
                                water_scores = data[5, :]
                                valid_water = water_scores > CONF_THRESH
                                if np.any(valid_water):
                                    water_indices = np.where(valid_water)[0]
                                    puddle_count = len(water_indices)
                                    water_coeffs = data[10:42, water_indices].T
                                    water_masks = decode_mask_prototypes(water_coeffs, proto_data, binarize_thresh=0.5)
                                    water_mask = np.any(water_masks, axis=0)
                                else:
                                    water_mask = np.zeros((104, 104), dtype=bool)

                                water_ratio = calculate_water_ratio(water_mask, road_mask)
                                if len(kept_motorcycles) > 0 and water_mask is not None:
                                    max_delta_sub, depth_estimated_cm, submerged_mc = calculate_motorcycle_submersion(
                                        np.array(kept_motorcycles), water_mask, target_img_size=(target_w, target_h)
                                    )

                        elif data.ndim == 2 and num_channels >= 12:
                            # Standard COCO 80-class model:
                            # Vehicle classes: 2: car (row 6), 3: motorcycle (row 7), 5: bus (row 9), 7: truck (row 11)
                            coco_vehicle_rows = {
                                "car": 6,
                                "motorcycle": 7,
                                "bus": 9,
                                "truck": 11,
                            }
                            for cls_name, row_idx in coco_vehicle_rows.items():
                                scores = data[row_idx, :]
                                valid_mask = scores > CONF_THRESH
                                if np.any(valid_mask):
                                    valid_indices = np.where(valid_mask)[0]
                                    candidate_boxes = np.column_stack([
                                        data[0, valid_indices],
                                        data[1, valid_indices],
                                        data[2, valid_indices],
                                        data[3, valid_indices],
                                    ])
                                    candidate_scores = scores[valid_indices]
                                    keep = nms_cpu(candidate_boxes, candidate_scores, iou_thresh=IOU_THRESH)
                                    vehicle_counts[cls_name] = len(keep)
                                    if cls_name == "motorcycle" and len(keep) > 0:
                                        kept_motorcycles = candidate_boxes[keep]
                                else:
                                    vehicle_counts[cls_name] = 0

                            vehicle_counts["container"] = 0
                            vehicles = sum(vehicle_counts.values())

                        elif data.ndim == 2 and num_channels == 9:
                            # Legacy DatapolisX 5-class model: 0: motorcycle, 1: car, 2: bus, 3: truck, 4: container
                            legacy_vehicle_rows = {
                                "motorcycle": 4,
                                "car": 5,
                                "bus": 6,
                                "truck": 7,
                                "container": 8,
                            }
                            for cls_name, row_idx in legacy_vehicle_rows.items():
                                scores = data[row_idx, :]
                                valid_mask = scores > CONF_THRESH
                                if np.any(valid_mask):
                                    valid_indices = np.where(valid_mask)[0]
                                    candidate_boxes = np.column_stack([
                                        data[0, valid_indices],
                                        data[1, valid_indices],
                                        data[2, valid_indices],
                                        data[3, valid_indices],
                                    ])
                                    candidate_scores = scores[valid_indices]
                                    keep = nms_cpu(candidate_boxes, candidate_scores, iou_thresh=IOU_THRESH)
                                    vehicle_counts[cls_name] = len(keep)
                                else:
                                    vehicle_counts[cls_name] = 0

                            vehicles = sum(vehicle_counts.values())
                        else:
                            vehicles = int(np.sum(output_data > 0.5))
                            vehicle_counts["car"] = vehicles
                    else:
                        vehicles = 0

                    traffic_severity = "CLEAR"
                    if vehicles > 35:
                        traffic_severity = "CONGESTED"
                    elif vehicles > 20:
                        traffic_severity = "HEAVY"
                    elif vehicles > 8:
                        traffic_severity = "MODERATE"

                    flood_verdict, flood_conf = triangulate_flood_verdict(
                        water_ratio=water_ratio,
                        max_delta_sub=max_delta_sub,
                        depth_estimated_cm=depth_estimated_cm,
                        motorcycles_observed=vehicle_counts["motorcycle"],
                    )

                    self._send_json(200, {
                        "service": "projectkv-cv-engine",
                        "vehicles_detected": vehicles,
                        "vehicle_counts": vehicle_counts,
                        "traffic_severity": traffic_severity,
                        "flood_severity": flood_verdict,
                        "water_ratio": round(water_ratio, 4),
                        "depth_estimated_cm": round(depth_estimated_cm, 1),
                        "confidence": round(float(flood_conf if flood_verdict != "CLEAR" else 0.88), 2),
                        "latency_ms": round(latency, 2),
                        "model": model_tag,
                        "flood_assessment": {
                            "status": flood_verdict,
                            "water_surface_ratio": round(water_ratio, 4),
                            "wheel_submersion_detected": max_delta_sub >= 0.18,
                            "max_submersion_depth_cm": round(depth_estimated_cm, 1),
                            "motorcycles_observed": vehicle_counts["motorcycle"],
                            "motorcycles_submerged": submerged_mc,
                            "puddle_polygon_count": puddle_count,
                        },
                    })
                    return
                except Exception as exc:
                    logger.error("Inference execution error: %s", exc)
                    self._send_json(500, {"error": f"inference failed: {str(exc)}"})
                    return

            # Standby mode: no model loaded — return 503 to prevent Go client from
            # treating this as a real inference result (ADR-004 §14 Finding 1).
            latency = (time.perf_counter() - start_t) * 1000
            self._send_json(503, {
                "source": "standby_dummy",
                "confidence": 0.0,
                "latency_ms": round(latency, 2),
                "mode": "standby_ready",
                "note": "OpenVINO engine ready. Model weights mountable at runtime."
            })
            return

        self._send_json(404, {"error": "not found"})

    def log_message(self, format, *args):
        # Silence routine health check access logs to keep terminal noise low
        if len(args) > 0 and isinstance(args[0], str) and "/healthz" in args[0]:
            return
        super().log_message(format, *args)


def run():
    load_model()
    server_address = ("", PORT)
    httpd = CVServer(server_address, CVRequestHandler)
    logger.info("ProjectKV CV Engine running on port %d (ThreadingHTTPServer)...", PORT)
    try:
        httpd.serve_forever()
    except KeyboardInterrupt:
        logger.info("Shutting down CV Engine...")
        httpd.server_close()


if __name__ == "__main__":
    run()
