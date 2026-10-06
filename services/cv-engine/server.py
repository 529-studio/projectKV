#!/usr/bin/env python3
"""
ProjectKV CV Engine — Standalone Computer Vision Inference Microservice.
Evaluated and validated in Spike #104 (docs/spike-cv-runtime.md) for non-AVX hardware.
Runs Intel OpenVINO CPU plugin with SSE4.2 vector instructions.
"""

import json
import logging
import os
import sys
import time
from http.server import ThreadingHTTPServer, BaseHTTPRequestHandler
from io import BytesIO

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

    model_xml = os.path.join(MODEL_DIR, "yolov8n.xml")
    if not os.path.isfile(model_xml):
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
        if self.path == "/predict":
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
                    image_resized = image.resize((320, 320))
                    input_tensor = np.expand_dims(np.array(image_resized).transpose((2, 0, 1)), axis=0).astype(np.float32) / 255.0

                    infer_request = compiled_model.create_infer_request()
                    results = infer_request.infer({0: input_tensor})
                    output_data = results[0]

                    # Parse output detections (YOLO format)
                    latency = (time.perf_counter() - start_t) * 1000

                    vehicle_counts = {
                        "motorcycle": 0,
                        "car": 0,
                        "bus": 0,
                        "truck": 0,
                        "container": 0,
                    }

                    if output_data is not None and hasattr(output_data, "shape"):
                        data = output_data[0] if output_data.ndim == 3 else output_data
                        if data.ndim == 2 and data.shape[0] >= 12:
                            # COCO vehicle classes: 2: car (row 6), 3: motorcycle (row 7), 5: bus (row 9), 7: truck (row 11)
                            vehicle_counts["motorcycle"] = int(np.sum(data[7, :] > 0.45))
                            vehicle_counts["car"] = int(np.sum(data[6, :] > 0.45))
                            vehicle_counts["bus"] = int(np.sum(data[9, :] > 0.45))
                            vehicle_counts["truck"] = int(np.sum(data[11, :] > 0.45))
                            vehicles = sum(vehicle_counts.values())
                        elif data.ndim == 2 and data.shape[0] == 9:
                            # DatapolisX 5-class model: 0: motorcycle, 1: car, 2: bus, 3: truck, 4: container
                            vehicle_counts["motorcycle"] = int(np.sum(data[4, :] > 0.45))
                            vehicle_counts["car"] = int(np.sum(data[5, :] > 0.45))
                            vehicle_counts["bus"] = int(np.sum(data[6, :] > 0.45))
                            vehicle_counts["truck"] = int(np.sum(data[7, :] > 0.45))
                            vehicle_counts["container"] = int(np.sum(data[8, :] > 0.45))
                            vehicles = sum(vehicle_counts.values())
                        else:
                            vehicles = int(np.sum(output_data > 0.5))
                            vehicle_counts["car"] = vehicles
                    else:
                        vehicles = 0

                    severity = "CLEAR"
                    if vehicles > 35:
                        severity = "CONGESTED"
                    elif vehicles > 20:
                        severity = "HEAVY"
                    elif vehicles > 8:
                        severity = "MODERATE"

                    self._send_json(200, {
                        "vehicles_detected": vehicles,
                        "vehicle_counts": vehicle_counts,
                        "traffic_severity": severity,
                        "confidence": 0.88,
                        "latency_ms": round(latency, 2),
                        "model": "yolov8n_openvino_320"
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
