#!/usr/bin/env python3
"""
audit_cv_detections.py — Deep perception audit script for CV object detection failure modes.

Sprint 5 Task 2 (Issue #208) / ADR-006 Finding 2:
Diagnoses two primary perception failure modes in YOLOv8n:
  (a) Missed Congestion Queues (CONGESTED reporting SI = 0.00 / 0 vehicles)
  (b) Clear-Road Over-Saturation (CLEAR reporting SI >= 0.51 / false congestion)

Extracts raw tensor activations, bounding-box coordinate clusters, multi-anchor
duplication ratios, aspect-ratio distortion effects, and resolution sensitivity.

Zero-emoji, minimalist formatting adhering to AGENTS.md Rule 1.
"""

import argparse
import json
import os
import sys
from pathlib import Path
from typing import Dict, List, Tuple, Any

import numpy as np
from PIL import Image, ImageDraw, ImageFont
import openvino as ov
from ultralytics import YOLO

# COCO 80 classes mapping
COCO_VEHICLE_CLASSES = {
    2: "car",
    3: "motorcycle",
    5: "bus",
    7: "truck",
}

# OpenVINO tensor row indices (YOLOv8 output shape [1, 84, 2100])
# Row 0: cx, Row 1: cy, Row 2: w, Row 3: h
# Row 4: person, Row 6: car, Row 7: motorcycle, Row 9: bus, Row 11: truck
OV_ROW_TO_NAME = {
    6: "car",
    7: "motorcycle",
    9: "bus",
    11: "truck",
}


def nms_cpu(boxes: np.ndarray, scores: np.ndarray, iou_thresh: float = 0.45) -> List[int]:
    """Applies standard Non-Maximum Suppression on bounding boxes."""
    if len(boxes) == 0:
        return []
    
    x1 = boxes[:, 0] - boxes[:, 2] / 2
    y1 = boxes[:, 1] - boxes[:, 3] / 2
    x2 = boxes[:, 0] + boxes[:, 2] / 2
    y2 = boxes[:, 1] + boxes[:, 3] / 2
    areas = (x2 - x1) * (y2 - y1)
    
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
        iou = inter / (areas[i] + areas[order[1:]] - inter)
        
        inds = np.where(iou <= iou_thresh)[0]
        order = order[inds + 1]
    return keep


def letterbox_image(img: Image.Image, target_size: Tuple[int, int] = (320, 320)) -> Tuple[Image.Image, float, int, int]:
    """Resizes image with aspect-ratio preserving padding (letterboxing)."""
    w, h = img.size
    scale = min(target_size[0] / w, target_size[1] / h)
    nw, nh = int(round(w * scale)), int(round(h * scale))
    img_resized = img.resize((nw, nh), Image.Resampling.BILINEAR)
    pad_x = (target_size[0] - nw) // 2
    pad_y = (target_size[1] - nh) // 2
    new_img = Image.new("RGB", target_size, (114, 114, 114))
    new_img.paste(img_resized, (pad_x, pad_y))
    return new_img, scale, pad_x, pad_y


def run_openvino_inference(
    compiled_model: Any,
    image_path: str,
    preprocess_mode: str = "stretch",
    conf_thresh: float = 0.45,
    use_nms: bool = False,
    iou_thresh: float = 0.45,
) -> Dict[str, Any]:
    """Runs inference on OpenVINO model with configurable preprocessing and NMS."""
    img = Image.open(image_path).convert("RGB")
    orig_w, orig_h = img.size

    if preprocess_mode == "stretch":
        img_prep = img.resize((320, 320))
        scale_x = orig_w / 320.0
        scale_y = orig_h / 320.0
        pad_x, pad_y = 0, 0
    else:  # letterbox
        img_prep, scale, pad_x, pad_y = letterbox_image(img, (320, 320))
        scale_x = scale_y = 1.0 / scale

    inp = np.expand_dims(np.array(img_prep).transpose((2, 0, 1)), axis=0).astype(np.float32) / 255.0
    infer_request = compiled_model.create_infer_request()
    results = infer_request.infer({0: inp})
    output_data = results[0][0]  # shape (84, 2100)

    detections = []
    class_counts = {c: 0 for c in OV_ROW_TO_NAME.values()}
    raw_anchor_counts = {c: 0 for c in OV_ROW_TO_NAME.values()}

    for row_idx, cls_name in OV_ROW_TO_NAME.items():
        scores = output_data[row_idx, :]
        raw_anchor_counts[cls_name] = int(np.sum(scores > conf_thresh))
        
        valid_indices = np.where(scores > conf_thresh)[0]
        if len(valid_indices) == 0:
            continue

        candidate_boxes = []
        candidate_scores = []
        for idx in valid_indices:
            cx = output_data[0, idx]
            cy = output_data[1, idx]
            w = output_data[2, idx]
            h = output_data[3, idx]

            if preprocess_mode == "stretch":
                x_orig = cx * scale_x
                y_orig = cy * scale_y
                w_orig = w * scale_x
                h_orig = h * scale_y
            else:
                x_orig = (cx - pad_x) * scale_x
                y_orig = (cy - pad_y) * scale_y
                w_orig = w * scale_x
                h_orig = h * scale_y

            candidate_boxes.append([x_orig, y_orig, w_orig, h_orig])
            candidate_scores.append(float(scores[idx]))

        candidate_boxes_np = np.array(candidate_boxes)
        candidate_scores_np = np.array(candidate_scores)

        if use_nms:
            keep = nms_cpu(candidate_boxes_np, candidate_scores_np, iou_thresh)
        else:
            keep = list(range(len(candidate_boxes)))

        class_counts[cls_name] = len(keep)
        for k in keep:
            b = candidate_boxes_np[k]
            x1 = max(0.0, b[0] - b[2] / 2.0)
            y1 = max(0.0, b[1] - b[3] / 2.0)
            x2 = min(float(orig_w), b[0] + b[2] / 2.0)
            y2 = min(float(orig_h), b[1] + b[3] / 2.0)
            detections.append({
                "class": cls_name,
                "score": float(candidate_scores_np[k]),
                "box": [round(float(v), 1) for v in [x1, y1, x2, y2]],
                "center": [round(float(b[0]), 1), round(float(b[1]), 1)],
                "size": [round(float(b[2]), 1), round(float(b[3]), 1)],
            })

    total_vehicles = sum(class_counts.values())
    raw_total = sum(raw_anchor_counts.values())

    return {
        "total_vehicles": total_vehicles,
        "raw_anchors_total": raw_total,
        "class_counts": class_counts,
        "raw_anchor_counts": raw_anchor_counts,
        "detections": detections,
    }


def run_ultralytics_inference(
    yolo_model: YOLO,
    image_path: str,
    imgsz: int = 416,
    conf_thresh: float = 0.25,
    iou_thresh: float = 0.45,
) -> Dict[str, Any]:
    """Runs inference via Ultralytics YOLOv8 with native letterbox and NMS."""
    vehicle_classes = list(COCO_VEHICLE_CLASSES.keys())
    res = yolo_model.predict(
        image_path,
        imgsz=imgsz,
        conf=conf_thresh,
        iou=iou_thresh,
        classes=vehicle_classes,
        verbose=False,
    )[0]

    class_counts = {c: 0 for c in COCO_VEHICLE_CLASSES.values()}
    detections = []
    for b in res.boxes:
        cls_id = int(b.cls[0])
        cls_name = COCO_VEHICLE_CLASSES.get(cls_id, "unknown")
        class_counts[cls_name] += 1
        conf = float(b.conf[0])
        xyxy = [round(float(v), 1) for v in b.xyxy[0].tolist()]
        cx = round((xyxy[0] + xyxy[2]) / 2.0, 1)
        cy = round((xyxy[1] + xyxy[3]) / 2.0, 1)
        w = round(xyxy[2] - xyxy[0], 1)
        h = round(xyxy[3] - xyxy[1], 1)
        detections.append({
            "class": cls_name,
            "score": round(conf, 3),
            "box": xyxy,
            "center": [cx, cy],
            "size": [w, h],
        })

    return {
        "total_vehicles": len(detections),
        "class_counts": class_counts,
        "detections": detections,
    }


def draw_bounding_boxes(
    image_path: str,
    output_path: str,
    detections: List[Dict[str, Any]],
    title: str = "",
    color_map: Dict[str, str] = None,
) -> None:
    """Draws visual bounding boxes with class labels and confidence on snapshot."""
    if color_map is None:
        color_map = {
            "car": "#00FF00",
            "motorcycle": "#00FFFF",
            "bus": "#FF00FF",
            "truck": "#FFA500",
        }

    img = Image.open(image_path).convert("RGB")
    draw = ImageDraw.Draw(img)

    for det in detections:
        box = det["box"]
        cls_name = det["class"]
        score = det["score"]
        col = color_map.get(cls_name, "#FFFFFF")

        draw.rectangle(box, outline=col, width=2)
        label_text = f"{cls_name[:3]} {score:.2f}"
        draw.text((box[0] + 2, max(0, box[1] - 12)), label_text, fill=col)

    if title:
        draw.rectangle([0, 0, img.width, 22], fill="#000000")
        draw.text((6, 4), title, fill="#FFFFFF")

    os.makedirs(os.path.dirname(output_path), exist_ok=True)
    img.save(output_path, quality=90)


def calculate_pcu_and_si(class_counts: Dict[str, int], corridor: str) -> Tuple[float, float, str]:
    """Calculates PCU, Saturation Index, and Severity following Go backend formula."""
    # PCU weights: motorcycle=0.2, car=1.0, bus=2.0, truck=2.5
    pcu = (
        class_counts.get("motorcycle", 0) * 0.2 +
        class_counts.get("car", 0) * 1.0 +
        class_counts.get("bus", 0) * 2.0 +
        class_counts.get("truck", 0) * 2.5
    )

    # Corridor capacities from internal/analyzer/analyzer.go
    capacities = {
        "cong_hoa": 41.0,
        "mai_chi_tho": 41.2,
        "vo_van_kiet": 41.0,
        "dien_bien_phu": 25.0,
    }
    cap = capacities.get(corridor, 30.0)
    si = min(1.0, round(pcu / cap, 4))

    # Severity classification:
    # CLEAR: SI < 0.35, MODERATE: 0.35 <= SI < 0.70, HEAVY: 0.70 <= SI < 0.90, CONGESTED: SI >= 0.90
    if si >= 0.90:
        severity = "CONGESTED"
    elif si >= 0.70:
        severity = "HEAVY"
    elif si >= 0.35:
        severity = "MODERATE"
    else:
        severity = "CLEAR"

    return pcu, si, severity


def main():
    parser = argparse.ArgumentParser(description="Object Detection Perception Failure Audit")
    parser.add_argument("--ov-model", default="yolov8n_openvino_model/yolov8n.xml", help="Path to OpenVINO XML")
    parser.add_argument("--pt-model", default="yolov8n.pt", help="Path to YOLOv8 PyTorch weights")
    parser.add_argument("--out-dir", default="docs/reports", help="Output directory for reports and overlays")
    args = parser.parse_args()

    out_dir = Path(args.out_dir)
    overlay_dir = out_dir / "audit_overlays"
    overlay_dir.mkdir(parents=True, exist_ok=True)

    print("Loading models...")
    core = ov.Core()
    ov_model = core.read_model(args.ov_model)
    compiled_ov = core.compile_model(ov_model, "CPU")
    yolo_pt = YOLO(args.pt_model)
    print("Models loaded successfully.")

    # Target test frames representing the core failure modes
    audit_targets = [
        {
            "id": "failure_a_cong_hoa",
            "name": "Cộng Hòa - Trường Chinh",
            "corridor": "cong_hoa",
            "camera_id": "586e1f18f9fab7001111b0a5",
            "frame_id": "cam_586e1f18f9fab7001111b0a5_1791453029",
            "image_path": "data/proofs/2026-10-08/586e1f18f9fab7001111b0a5/1791453029_sample.jpg",
            "failure_mode": "Failure Mode (a): Missed Congestion Queue",
            "expected_state": "CONGESTED",
        },
        {
            "id": "failure_a_mai_chi_tho_dvc2",
            "name": "Mai Chí Thọ - Đồng Văn Cống 2",
            "corridor": "mai_chi_tho",
            "camera_id": "56de42f611f398ec0c481282",
            "frame_id": "cam_56de42f611f398ec0c481282_1791453015",
            "image_path": "data/proofs/2026-10-08/56de42f611f398ec0c481282/1791453015_sample.jpg",
            "failure_mode": "Failure Mode (a): Smoke-Set Baseline Discrepancy",
            "expected_state": "CONGESTED",
        },
        {
            "id": "failure_b_mai_chi_tho_nct1",
            "name": "Mai Chí Thọ - Nguyễn Cơ Thạch 1",
            "corridor": "mai_chi_tho",
            "camera_id": "5822b5ededeb6c0012a2d66a",
            "frame_id": "cam_5822b5ededeb6c0012a2d66a_1791437508",
            "image_path": "data/proofs/2026-10-08/5822b5ededeb6c0012a2d66a/1791437508_state_change.jpg",
            "failure_mode": "Failure Mode (b): Multi-Anchor Clear Saturation",
            "expected_state": "CLEAR",
        },
        {
            "id": "failure_b_mai_chi_tho_to_huu",
            "name": "Mai Chí Thọ - Tố Hữu (C6)",
            "corridor": "mai_chi_tho",
            "camera_id": "5a10c79f02eb490011a0b0dd",
            "frame_id": "cam_5a10c79f02eb490011a0b0dd_1791437567",
            "image_path": "data/proofs/2026-10-08/5a10c79f02eb490011a0b0dd/1791437567_state_change.jpg",
            "failure_mode": "Failure Mode (b): Multi-Class / Multi-Anchor Saturation",
            "expected_state": "CLEAR",
        },
        {
            "id": "failure_b_vo_van_kiet_ky_con",
            "name": "Võ Văn Kiệt - Ký Con",
            "corridor": "vo_van_kiet",
            "camera_id": "56de42f611f398ec0c481287",
            "frame_id": "cam_56de42f611f398ec0c481287_1791437537",
            "image_path": "data/proofs/2026-10-08/56de42f611f398ec0c481287/1791437537_state_change.jpg",
            "failure_mode": "Failure Mode (b): Multi-Anchor Clear Saturation",
            "expected_state": "CLEAR",
        },
        {
            "id": "failure_b_dien_bien_phu_pasteur",
            "name": "Điện Biên Phủ - Pasteur",
            "corridor": "dien_bien_phu",
            "camera_id": "5deb576d1dc17d7c5515ad02",
            "frame_id": "cam_5deb576d1dc17d7c5515ad02_1791437567",
            "image_path": "data/proofs/2026-10-08/5deb576d1dc17d7c5515ad02/1791437567_state_change.jpg",
            "failure_mode": "Failure Mode (b): Multi-Anchor Clear Saturation",
            "expected_state": "CLEAR",
        },
    ]

    audit_results = []

    print("\nRunning comprehensive comparative audit...")
    for target in audit_targets:
        img_path = target["image_path"]
        name = target["name"]
        corridor = target["corridor"]
        expected = target["expected_state"]
        print(f"\nProcessing {name} ({target['failure_mode']})...")

        # Config 1: Production Baseline (OpenVINO 320 stretch, no NMS, th=0.45)
        c1 = run_openvino_inference(compiled_ov, img_path, preprocess_mode="stretch", conf_thresh=0.45, use_nms=False)
        pcu1, si1, sev1 = calculate_pcu_and_si(c1["class_counts"], corridor)

        # Config 2: OpenVINO 320 stretch + NMS (th=0.25, iou=0.45)
        c2 = run_openvino_inference(compiled_ov, img_path, preprocess_mode="stretch", conf_thresh=0.25, use_nms=True, iou_thresh=0.45)
        pcu2, si2, sev2 = calculate_pcu_and_si(c2["class_counts"], corridor)

        # Config 3: OpenVINO 320 letterbox + NMS (th=0.25, iou=0.45)
        c3 = run_openvino_inference(compiled_ov, img_path, preprocess_mode="letterbox", conf_thresh=0.25, use_nms=True, iou_thresh=0.45)
        pcu3, si3, sev3 = calculate_pcu_and_si(c3["class_counts"], corridor)

        # Config 4: YOLOv8n 416 letterbox + NMS (th=0.25, iou=0.45)
        c4 = run_ultralytics_inference(yolo_pt, img_path, imgsz=416, conf_thresh=0.25, iou_thresh=0.45)
        pcu4, si4, sev4 = calculate_pcu_and_si(c4["class_counts"], corridor)

        # Config 5: YOLOv8n 640 letterbox + NMS (th=0.25, iou=0.45)
        c5 = run_ultralytics_inference(yolo_pt, img_path, imgsz=640, conf_thresh=0.25, iou_thresh=0.45)
        pcu5, si5, sev5 = calculate_pcu_and_si(c5["class_counts"], corridor)

        # Save overlays:
        # Overlay 1: Production raw boxes (showing anchor explosion or empty)
        ov1_path = overlay_dir / f"{target['id']}_prod_320_raw.jpg"
        draw_bounding_boxes(
            img_path,
            str(ov1_path),
            c1["detections"],
            title=f"PROD 320px Stretch NO NMS (th=0.45): {c1['total_vehicles']} objs | PCU={pcu1:.1f} SI={si1:.2f} ({sev1})",
        )

        # Overlay 2: Proposed 416 letterbox + NMS
        ov2_path = overlay_dir / f"{target['id']}_prop_416_nms.jpg"
        draw_bounding_boxes(
            img_path,
            str(ov2_path),
            c4["detections"],
            title=f"PROPOSED 416px Letterbox + NMS (th=0.25): {c4['total_vehicles']} objs | PCU={pcu4:.1f} SI={si4:.2f} ({sev4})",
        )

        dup_ratio = round(c1["total_vehicles"] / max(1, c2["total_vehicles"]), 2)

        entry = {
            "target": target,
            "metrics": {
                "config_1_prod_320_raw": {
                    "total_vehicles": c1["total_vehicles"],
                    "raw_anchors": c1["raw_anchors_total"],
                    "class_counts": c1["class_counts"],
                    "pcu": pcu1,
                    "si": si1,
                    "severity": sev1,
                    "status": "PASS" if sev1 == expected else "FAIL",
                },
                "config_2_ov_320_stretch_nms": {
                    "total_vehicles": c2["total_vehicles"],
                    "class_counts": c2["class_counts"],
                    "pcu": pcu2,
                    "si": si2,
                    "severity": sev2,
                    "status": "PASS" if sev2 == expected else "FAIL",
                },
                "config_3_ov_320_letterbox_nms": {
                    "total_vehicles": c3["total_vehicles"],
                    "class_counts": c3["class_counts"],
                    "pcu": pcu3,
                    "si": si3,
                    "severity": sev3,
                    "status": "PASS" if sev3 == expected else "FAIL",
                },
                "config_4_yolo_416_letterbox_nms": {
                    "total_vehicles": c4["total_vehicles"],
                    "class_counts": c4["class_counts"],
                    "pcu": pcu4,
                    "si": si4,
                    "severity": sev4,
                    "status": "PASS" if sev4 == expected else "FAIL",
                },
                "config_5_yolo_640_letterbox_nms": {
                    "total_vehicles": c5["total_vehicles"],
                    "class_counts": c5["class_counts"],
                    "pcu": pcu5,
                    "si": si5,
                    "severity": sev5,
                    "status": "PASS" if sev5 == expected else "FAIL",
                },
                "multi_anchor_duplication_ratio": dup_ratio,
            },
            "overlay_prod": str(ov1_path),
            "overlay_proposed": str(ov2_path),
        }
        audit_results.append(entry)

    # Save JSON summary
    report_json_path = out_dir / "cv_detection_error_audit.json"
    with open(report_json_path, "w", encoding="utf-8") as f:
        json.dump(audit_results, f, indent=2)
    print(f"\nAudit completed. Summary saved to {report_json_path}")


if __name__ == "__main__":
    main()
