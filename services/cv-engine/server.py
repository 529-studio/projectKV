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
import threading
import time
from http.server import ThreadingHTTPServer, BaseHTTPRequestHandler
from io import BytesIO
from typing import Any, Dict, List, Optional, Sequence, Tuple

import hazard_classifier as hc
from hazard_classifier import HazardAssessment, MotorcycleObservation

logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s [%(levelname)s] %(name)s: %(message)s",
    handlers=[logging.StreamHandler(sys.stdout)]
)
logger = logging.getLogger("cv-engine")

PORT = int(os.environ.get("CV_PORT", os.environ.get("MODEL_PORT", 8000)))
MODEL_DIR = os.environ.get("MODEL_DIR", "/app/models/yolov8n_openvino_320")

# Inference backend selection. `openvino` is the production default and must keep
# working unchanged; `ort` runs the identical YOLOv8n graph through ONNX Runtime,
# preferring CUDA and falling back to CPU. The switch only chooses how the graph is
# executed — preprocessing, post-processing and the HTTP contract are shared.
SUPPORTED_BACKENDS: Tuple[str, ...] = ("openvino", "ort")
DEFAULT_BACKEND = "openvino"

SUPPORTED_PRECISIONS: Tuple[str, ...] = ("auto", "int8", "fp16", "fp32")
DEFAULT_PRECISION = "auto"


def resolve_precision(raw: Optional[str]) -> str:
    """Return the precision named by ``CV_PRECISION``, defaulting to auto.

    An unrecognised value is logged with a warning and defaults to 'auto'.
    """
    candidate = (raw or "").strip().lower()
    if not candidate:
        return DEFAULT_PRECISION
    if candidate in SUPPORTED_PRECISIONS:
        return candidate
    logger.warning(
        "Ignoring CV_PRECISION=%r: allowed values are %s; falling back to %s",
        raw,
        ", ".join(SUPPORTED_PRECISIONS),
        DEFAULT_PRECISION,
    )
    return DEFAULT_PRECISION


CV_PRECISION = resolve_precision(os.environ.get("CV_PRECISION"))


def resolve_backend(raw: Optional[str]) -> str:
    """Return the backend named by ``CV_BACKEND``, defaulting to OpenVINO.

    An unrecognised value must never silently become the new serving path: it is
    logged and the documented default is used instead, so a typo cannot change what
    the inference host runs.
    """
    candidate = (raw or "").strip().lower()
    if not candidate:
        return DEFAULT_BACKEND
    if candidate in SUPPORTED_BACKENDS:
        return candidate
    logger.warning(
        "Ignoring CV_BACKEND=%r: allowed values are %s; falling back to %s",
        raw,
        ", ".join(SUPPORTED_BACKENDS),
        DEFAULT_BACKEND,
    )
    return DEFAULT_BACKEND


CV_BACKEND = resolve_backend(os.environ.get("CV_BACKEND"))
# Explicit ORT weight path. ORT mode never scans for a `.onnx` file: the class row
# layout the post-processor indexes into depends on which model was exported, so a
# guess could silently serve a graph with the wrong channel count.
CV_ONNX_MODEL = (os.environ.get("CV_ONNX_MODEL") or "").strip() or os.path.join(
    MODEL_DIR, "yolov8n.onnx"
)


def resolve_onnx_model_path(
    explicit_path: Optional[str] = None,
    precision: Optional[str] = None,
    model_dir: Optional[str] = None,
) -> Tuple[str, str, bool]:
    """Resolve the ONNX model path, target precision, and quantization status.

    Precedence:
      1. If explicit_path is provided:
         - If precision is 'int8' or 'auto', check if an int8 quantized file exists
           (e.g., explicit_path with '_int8.onnx' suffix, or explicit_path if already int8).
         - Otherwise use explicit_path.
      2. If explicit_path is not provided or does not exist:
         - In search directories (model_dir, 'models', 'data/models'), look for INT8
           candidates if precision is 'int8' or 'auto':
           'yolov8n_640_int8.onnx', 'yolov8n_seg_int8.onnx', 'yolov8n_int8.onnx'.
         - If not found, look for baseline FP32/FP16 models:
           'yolov8n_640.onnx', 'yolov8n_seg.onnx', 'yolov8n.onnx'.

    Returns:
      (resolved_model_path, effective_precision, is_quantized)
    """
    req_precision = resolve_precision(precision)
    base_dir = model_dir or MODEL_DIR

    # 1. Explicit path given
    if explicit_path and explicit_path.strip():
        cand = explicit_path.strip()
        if "int8" in os.path.basename(cand).lower() or "quant" in os.path.basename(cand).lower():
            return cand, "int8", True
        if req_precision in ("int8", "auto"):
            stem, ext = os.path.splitext(cand)
            int8_peer = f"{stem}_int8{ext}"
            if os.path.isfile(int8_peer):
                return int8_peer, "int8", True
        if os.path.isfile(cand):
            return cand, ("fp16" if req_precision == "fp16" else "fp32"), False
        return cand, ("int8" if req_precision == "int8" else "fp32"), (req_precision == "int8")

    # 2. Search in search_dirs
    search_dirs = [base_dir, "models", "data/models"]
    if req_precision in ("int8", "auto"):
        for d in search_dirs:
            for name in ("yolov8n_640_int8.onnx", "yolov8n_seg_int8.onnx", "yolov8n_int8.onnx"):
                p = os.path.join(d, name)
                if os.path.isfile(p):
                    return p, "int8", True

    for d in search_dirs:
        for name in ("yolov8n_640.onnx", "yolov8n_seg.onnx", "yolov8n.onnx"):
            p = os.path.join(d, name)
            if os.path.isfile(p):
                return p, ("fp16" if req_precision == "fp16" else "fp32"), False

    default_p = os.path.join(base_dir, "yolov8n.onnx")
    return default_p, ("int8" if req_precision == "int8" else "fp32"), (req_precision == "int8")


# Attempt to import optional OpenVINO and imaging libraries
OPENVINO_AVAILABLE = False
PIL_AVAILABLE = False
core = None
compiled_model = None
# How the loaded model treats its spatial input, and the compiled input shape.
# "dynamic" means the graph accepts a per-request spatial size; "static" means the
# shape baked into the IR is the only shape it can execute; "unknown" means nothing
# is loaded yet. The handler reads these instead of assuming every model is dynamic.
resolution_mode = "unknown"
compiled_input_shape = None
# Providers the ORT session actually resolved, in execution order. Empty when no
# ORT session is loaded. Reported by /info so an operator can see whether CUDA was
# really available instead of inferring it from the requested list.
ort_providers: List[str] = []
effective_precision: str = "fp32"
is_model_quantized: bool = False

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


# Preferred weight filenames, in precedence order. The fine-tuned segmentation model
# must win over the generic detector: they produce different output shapes and only
# the segmentation model emits masks, so picking the wrong one silently downgrades
# the service to bounding boxes.
SEG_MODEL_XML = "yolov8n_seg.xml"
DET_MODEL_XML = "yolov8n.xml"

# Load-shedding contract (ADR-012 §1.2 F5 / Issue #379).
#
# The request header is reachable by any client that can talk to the port, so it is
# held to the two sizes the serving loop is dimensioned for. Allowing 640 px from a
# header would be a CPU-amplification vector: one caller could quadruple the pixel
# count relative to 320 px for every request. The operator-controlled environment
# override keeps 640 available for a deliberate, restart-scoped capacity change.
HEADER_INPUT_RESOLUTIONS: Tuple[int, ...] = (320, 416)
ENV_INPUT_RESOLUTIONS: Tuple[int, ...] = (320, 416, 640)
DEFAULT_INPUT_RESOLUTION = 416


def resolve_input_resolution(
    header_value: Optional[str],
    env_value: Optional[str],
    model_shape: Optional[Sequence[int]] = None,
) -> Tuple[int, int]:
    """Resolve the (width, height) used for inference preprocessing.

    Precedence, highest first:
      1. ``X-Target-Resolution`` request header (per-request load shedding, 320/416).
      2. ``CV_INPUT_RESOLUTION`` environment override (operator degradation, 320/416/640).
      3. The loaded OpenVINO model's static NCHW input shape.
      4. ``DEFAULT_INPUT_RESOLUTION`` (416 px).

    Each source has its own whitelist: the request header cannot select 640 px, while
    the environment override can. An out-of-contract header or environment value is
    ignored with a warning so the effective resolution is never silently different
    from the documented contract.
    """
    sources = (
        ("X-Target-Resolution header", header_value, HEADER_INPUT_RESOLUTIONS),
        ("CV_INPUT_RESOLUTION", env_value, ENV_INPUT_RESOLUTIONS),
    )
    for source, raw, allowed in sources:
        if raw is None:
            continue
        candidate = raw.strip()
        if not candidate:
            continue
        if candidate.isascii() and candidate.isdigit() and int(candidate) in allowed:
            size = int(candidate)
            return size, size
        logger.warning(
            "Ignoring %s=%r: allowed values are %s",
            source,
            raw,
            ", ".join(str(v) for v in allowed),
        )

    if model_shape is not None and len(model_shape) == 4:
        try:
            height = int(model_shape[2])
            width = int(model_shape[3])
        except (TypeError, ValueError):
            height = width = 0
        if height > 0 and width > 0:
            return width, height

    return DEFAULT_INPUT_RESOLUTION, DEFAULT_INPUT_RESOLUTION


def spatial_dims_are_dynamic(model_shape: Optional[Sequence[int]]) -> bool:
    """Return True when a 4-D NCHW shape has no fixed spatial height/width."""
    if model_shape is None or len(model_shape) != 4:
        return True
    for dim in model_shape[2:]:
        if dim is None:
            return True
        try:
            if int(dim) <= 0:
                return True
        except (TypeError, ValueError):
            return True
    return False


def resolve_feed_resolution(
    target_w: int,
    target_h: int,
    mode: str,
    model_shape: Optional[Sequence[int]],
) -> Tuple[int, int]:
    """Return the ``(width, height)`` the compiled model can actually be fed.

    A dynamic graph accepts the requested target size. A static graph only executes
    the shape baked into the IR, so feeding the requested size would raise at
    ``infer()`` time; the request is served at the model shape instead and the
    downgrade is logged. The handler reports whichever pair this returns, so the
    response can never claim a resolution that was not fed to the model.
    """
    if mode == "dynamic" or spatial_dims_are_dynamic(model_shape):
        return target_w, target_h

    try:
        model_h = int(model_shape[2])
        model_w = int(model_shape[3])
    except (TypeError, ValueError, IndexError):
        return target_w, target_h
    if model_h <= 0 or model_w <= 0:
        return target_w, target_h

    if (model_w, model_h) != (target_w, target_h):
        logger.warning(
            "Loaded OpenVINO model is static (%dx%d); cannot execute at requested %dx%d. "
            "Running at model shape.",
            model_w,
            model_h,
            target_w,
            target_h,
        )
    return model_w, model_h


def model_input_metadata(
    model: Any,
) -> Tuple[str, Optional[Sequence[int]]]:
    """Return the resolution mode and input shape for the loaded compiled model.

    Production stores both in module globals when ``load_model()`` runs. A test
    double may expose the same information on the model object, which takes
    precedence so the handler can be exercised against a recording fake without
    mutating process state.
    """
    mode = getattr(model, "resolution_mode", None) or resolution_mode
    shape = getattr(model, "input_shape", None)
    if shape is None:
        shape = compiled_input_shape
    if shape is None:
        try:
            shape = model.input(0).shape
        except Exception as exc:
            logger.warning("Could not read model input shape, using default resolution: %s", exc)
    return mode, shape


def box_polygon_anchor(
    cx: float,
    cy: float,
    w: float,
    h: float,
    img_w: int,
    img_h: int,
) -> Tuple[float, float]:
    """Bottom-center anchor of a detection box in normalized image coordinates.

    Region membership is tested in the same normalized space the polygon contract
    uses, against the resolution actually fed to the model. Clamping keeps the anchor
    inside [0.0, 1.0] for boxes that extend past the frame edge, so an out-of-frame
    detection cannot be assigned to a region by an arithmetic accident.
    """
    if img_w <= 0 or img_h <= 0:
        return 0.0, 0.0
    norm_cx = min(1.0, max(0.0, float(cx) / float(img_w)))
    norm_by = min(1.0, max(0.0, (float(cy) + float(h) / 2.0) / float(img_h)))
    return norm_cx, norm_by


def select_model_xml(model_dir: str) -> Optional[str]:
    """Return the OpenVINO XML to load from `model_dir`, or None if none is mounted.

    Precedence: segmentation weights, then detection weights, then any single XML in
    the directory. Extracted from load_model() so the decision that determines which
    weights the service actually runs is directly testable; keeping it inline is what
    allowed a test to re-implement the loop and assert against its own copy.
    """
    for name in (SEG_MODEL_XML, DET_MODEL_XML):
        candidate = os.path.join(model_dir, name)
        if os.path.isfile(candidate):
            return candidate

    if not os.path.isdir(model_dir):
        logger.info("Model directory %s does not exist. Ready for dynamic model mounting.", model_dir)
        return None

    xml_files = sorted(f for f in os.listdir(model_dir) if f.endswith(".xml"))
    if not xml_files:
        logger.info("No OpenVINO model XML found in %s. Ready for dynamic model mounting.", model_dir)
        return None

    if len(xml_files) > 1:
        # os.listdir order is filesystem-dependent. Sorting makes the choice
        # reproducible, and the warning makes an unexpected extra weight visible.
        logger.warning(
            "Multiple unrecognised OpenVINO XML files in %s; loading %s (sorted order)",
            model_dir,
            xml_files[0],
        )
    return os.path.join(model_dir, xml_files[0])


class ORTInferenceResult:
    """Read-only view of the ORT output arrays under the keys the handler probes.

    The handler unwraps results through ``.get(0)``/``[0]`` and tests
    ``len()``. A raw ``dict`` of ndarrays is not equivalent: the handler's
    ``results.get(0) or results.get("output0")`` chain would evaluate an ndarray's
    truth value and raise. This mapping supplies the same lookups without that
    behaviour, so the untouched post-processing path receives the raw output.
    """

    def __init__(self, outputs: Sequence["np.ndarray"]) -> None:
        self._outputs = list(outputs)

    def get(self, key: Any, default: Any = None) -> Any:
        if isinstance(key, int) and 0 <= key < len(self._outputs):
            return self._outputs[key]
        return default

    def __getitem__(self, key: Any) -> Any:
        if isinstance(key, slice):
            return self._outputs[key]
        return self._outputs[int(key)]

    def __len__(self) -> int:
        return len(self._outputs)


class ORTInferRequest:
    """Single-shot inference handle mirroring ``CompiledModel.create_infer_request``."""

    def __init__(self, model: "ORTModel") -> None:
        self._model = model

    def infer(self, feeds: Dict[Any, "np.ndarray"]) -> ORTInferenceResult:
        tensor = feeds.get(0)
        if tensor is None and len(feeds) == 1:
            tensor = next(iter(feeds.values()))
        if tensor is None:
            raise ValueError("ORT infer() requires the image tensor under key 0")
        return ORTInferenceResult(self._model.run(tensor))


class ORTModel:
    """Duck-types the subset of the OpenVINO compiled model the handler uses.

    Resolution metadata lives on the object, not only in module globals, so the
    handler can describe the graph it is actually feeding without a second code
    path: ``model_input_metadata()`` already prefers these attributes.
    """

    def __init__(
        self,
        session: Any,
        input_name: str,
        input_shape: Sequence[Optional[int]],
        providers: Sequence[str],
        model_path: str = "",
    ) -> None:
        self.session = session
        self.input_name = input_name
        self.input_shape: List[Optional[int]] = list(input_shape)
        self.providers: List[str] = list(providers)
        self.model_path = model_path
        self.resolution_mode = (
            "dynamic" if spatial_dims_are_dynamic(self.input_shape) else "static"
        )
        # Guards the forward pass of the CUDA provider. ONNX Runtime advertises
        # thread-safe sessions, but on this deployment (Pascal P106-100, CUDA 12.4)
        # two request threads inside ``session.run`` at once abort the process with
        # "CUDA failure 700: an illegal memory access was encountered": the CUDA
        # context dies, the container exits, and every in-flight request is lost.
        # Serializing costs nothing here — YOLOv8n at 320x320 executes in a few
        # milliseconds and the request queue in front of the model bounds throughput
        # — while the same lock makes a rebuild safe, because swapping the session
        # while another thread is inside ``run`` is exactly the illegal state.
        self._run_lock = threading.Lock()
        self._reload_lock = threading.Lock()
        self._generation = 0

    @property
    def generation(self) -> int:
        return self._generation

    @property
    def _serializes_runs(self) -> bool:
        return "CUDAExecutionProvider" in self.providers

    def run(self, tensor: "np.ndarray") -> Any:
        """Execute one forward pass, rebuilding a dead CUDA session once.

        A CUDA context can die while the process keeps running (driver reset, GPU
        power event, long-idle clock change, or the concurrent-run access violation
        above). The session then answers 500 to every request until the container is
        restarted, so rebuild it once and retry; non-CUDA errors propagate unchanged.
        """
        if not self._serializes_runs:
            # CPU execution keeps the previous behaviour: independent threads run in
            # parallel and nothing is gained by serializing them.
            return self._run_or_recover(tensor)
        with self._run_lock:
            return self._run_or_recover(tensor)

    def _run_or_recover(self, tensor: "np.ndarray") -> Any:
        generation = self._generation
        try:
            return self.session.run(None, {self.input_name: tensor})
        except Exception as exc:
            if not is_cuda_runtime_failure(exc):
                raise
            logger.warning(
                "CUDA inference failed (%s); rebuilding the ONNX Runtime session and retrying once",
                exc,
            )
            self.reload_session(generation)
            return self.session.run(None, {self.input_name: tensor})

    def reload_session(self, observed_generation: int) -> None:
        """Replace a dead session once, even when several request threads race."""
        with self._reload_lock:
            if observed_generation != self._generation:
                return
            session, providers = open_ort_session(self.model_path)
            if (
                (os.environ.get("CV_REQUIRE_CUDA") or "").strip() == "1"
                and "CUDAExecutionProvider" not in providers
            ):
                # Rebuilding on CPU would keep the service "working" while the GPU
                # deployment silently loses the GPU. Only a fresh container has been
                # proven to clear a dead CUDA context, so exit loudly and let the
                # restart policy recreate it. os._exit is required here: this runs on
                # a request thread, where sys.exit only ends that thread.
                logger.critical(
                    "Session rebuild resolved only %s while CV_REQUIRE_CUDA=1; "
                    "exiting so the container restarts with a fresh CUDA context",
                    providers,
                )
                logging.shutdown()
                os._exit(1)
            self.session = session
            self.providers = list(providers)
            self.input_name = session.get_inputs()[0].name
            self.input_shape = list(ort_input_shape(session))
            self.resolution_mode = (
                "dynamic" if spatial_dims_are_dynamic(self.input_shape) else "static"
            )
            self._generation += 1
            logger.info(
                "ONNX Runtime session rebuilt (providers=%s, input_shape=%s)",
                self.providers,
                self.input_shape,
            )

    def create_infer_request(self) -> ORTInferRequest:
        return ORTInferRequest(self)


def ort_input_shape(session: Any) -> List[Optional[int]]:
    """Return the session's NCHW input shape, with symbolic dims as ``None``."""
    model_input = session.get_inputs()[0]
    shape: List[Optional[int]] = []
    for dim in model_input.shape:
        if isinstance(dim, int):
            shape.append(dim)
        else:
            try:
                shape.append(int(dim))
            except (TypeError, ValueError):
                shape.append(None)
    return shape


# Markers that identify a failure in the CUDA execution stack rather than a bad
# request. Used to decide whether rebuilding the session can help.
CUDA_FAILURE_MARKERS: Tuple[str, ...] = ("cublas", "cudnn", "cuda")


def is_cuda_runtime_failure(exc: BaseException) -> bool:
    text = str(exc).lower()
    return any(marker in text for marker in CUDA_FAILURE_MARKERS)


def open_ort_session(model_path: str) -> Tuple[Any, Sequence[str]]:
    """Create the CUDA-first ORT session, degrading to CPU only at creation time."""
    import onnxruntime as ort

    requested = ["CUDAExecutionProvider", "CPUExecutionProvider"]
    try:
        session = ort.InferenceSession(model_path, providers=requested)
    except Exception as cuda_exc:
        logger.warning(
            "ORT session with the CUDA provider failed (%s); retrying with CPUExecutionProvider only",
            cuda_exc,
        )
        session = ort.InferenceSession(model_path, providers=["CPUExecutionProvider"])
    return session, list(session.get_providers())


def load_ort_model() -> None:
    """Create the ORT session at ``CV_ONNX_MODEL`` and publish the serving contract.

    Providers are requested CUDA-first with CPU as the guaranteed fallback; ORT drops
    an unavailable provider and logs it, so the service still starts on a host without
    an NVIDIA GPU. The providers that actually resolved are stored and reported by
    ``/info`` rather than the requested list, which cannot distinguish the two.
    """
    global compiled_model, resolution_mode, compiled_input_shape, ort_providers
    global effective_precision, is_model_quantized

    # Imported lazily: the OpenVINO path must not pay ORT's import cost or fail on a
    # host image that only carries the Intel runtime.
    try:
        import onnxruntime as ort
    except ImportError as exc:
        logger.error("CV_BACKEND=ort but onnxruntime is not installed: %s", exc)
        return

    explicit = CV_ONNX_MODEL if os.environ.get("CV_ONNX_MODEL") else None
    resolved_path, target_precision, is_quant = resolve_onnx_model_path(
        explicit_path=explicit,
        precision=CV_PRECISION,
        model_dir=MODEL_DIR,
    )
    model_path = resolved_path
    effective_precision = target_precision
    is_model_quantized = is_quant

    require_cuda = (os.environ.get("CV_REQUIRE_CUDA") or "").strip() == "1"
    if require_cuda and not os.path.exists("/dev/nvidia0"):
        # With pip CUDA libraries present but no device node, ORT can die inside the
        # CUDA provider with a segfault instead of raising, which still crash-loops the
        # container but for no diagnosable reason. Check the device first.
        logger.critical(
            "CV_REQUIRE_CUDA=1 but /dev/nvidia0 is missing; the device reservation "
            "did not reach the container. Refusing to serve on CPU."
        )
        sys.exit(1)
    if not os.path.isfile(model_path):
        # A GPU deployment with no model would sit in standby answering /healthz 200
        # while every /predict fails, which reads as "deployed" to the health gate.
        if require_cuda:
            logger.critical(
                "CV_REQUIRE_CUDA=1 but the ONNX model %s does not exist; refusing to "
                "serve standby",
                model_path,
            )
            sys.exit(1)
        logger.warning(
            "CV_BACKEND=ort but the ONNX model %s does not exist; running in standby mode.",
            model_path,
        )
        return

    start_t = time.perf_counter()
    logger.info(
        "Loading ONNX Runtime model from %s (precision=%s, quantized=%s, providers=%s)...",
        model_path,
        effective_precision,
        is_model_quantized,
        ["CUDAExecutionProvider", "CPUExecutionProvider"],
    )
    try:
        session, providers = open_ort_session(model_path)
        if "CUDAExecutionProvider" not in providers:
            logger.warning(
                "CUDAExecutionProvider is unavailable; ONNX Runtime is executing on %s",
                providers,
            )
            # A GPU deployment that quietly executes on CPU is indistinguishable from a
            # working one at the HTTP layer: /healthz stays 200 and /predict works, just
            # slower. When CUDA is required, exit instead so the restart loop and the
            # deploy health gate both show the failure.
            if (os.environ.get("CV_REQUIRE_CUDA") or "").strip() == "1":
                logger.critical(
                    "CV_REQUIRE_CUDA=1 but the session resolved only %s; refusing to serve on CPU",
                    providers,
                )
                sys.exit(1)
        input_name = session.get_inputs()[0].name
        shape = ort_input_shape(session)
        compiled_model = ORTModel(session, input_name, shape, providers, model_path)
        compiled_input_shape = list(shape)
        resolution_mode = compiled_model.resolution_mode
        ort_providers = providers
        duration = (time.perf_counter() - start_t) * 1000
        logger.info(
            "ONNX Runtime session created in %.1f ms (providers=%s, input=%s %s, mode=%s, precision=%s)",
            duration,
            providers,
            input_name,
            shape,
            resolution_mode,
            effective_precision,
        )
    except Exception as exc:
        if is_model_quantized and CV_PRECISION in ("auto", "int8"):
            baseline_path = model_path.replace("_int8.onnx", ".onnx")
            if os.path.isfile(baseline_path) and baseline_path != model_path:
                logger.warning(
                    "INT8 model %s failed (%s); falling back to baseline %s",
                    model_path,
                    exc,
                    baseline_path,
                )
                try:
                    session, providers = open_ort_session(baseline_path)
                    input_name = session.get_inputs()[0].name
                    shape = ort_input_shape(session)
                    compiled_model = ORTModel(session, input_name, shape, providers, baseline_path)
                    compiled_input_shape = list(shape)
                    resolution_mode = compiled_model.resolution_mode
                    ort_providers = providers
                    effective_precision = "fp32"
                    is_model_quantized = False
                    duration = (time.perf_counter() - start_t) * 1000
                    logger.info(
                        "Fallback ONNX Runtime session created in %.1f ms (providers=%s, input=%s %s, mode=%s)",
                        duration,
                        providers,
                        input_name,
                        shape,
                        resolution_mode,
                    )
                    return
                except Exception as fallback_exc:
                    logger.error("Failed to load fallback ONNX model %s: %s", baseline_path, fallback_exc)
        logger.error("Failed to create ONNX Runtime session for %s: %s", model_path, exc)


def load_model():
    """Load the weights for the configured backend; standby state on failure.

    Both backends publish the same three module globals the handler reads
    (``compiled_model``, ``resolution_mode``, ``compiled_input_shape``), so routing
    and post-processing never branch on the backend. A failed load clears them first:
    a reload must not leave the handler describing a model that is no longer loaded.
    """
    global compiled_model, resolution_mode, compiled_input_shape, ort_providers
    global effective_precision, is_model_quantized

    compiled_model = None
    compiled_input_shape = None
    resolution_mode = "unknown"
    ort_providers = []
    effective_precision = "fp32"
    is_model_quantized = False

    if CV_BACKEND == "ort":
        load_ort_model()
        return

    load_openvino_model()


def load_openvino_model():
    """Attempt to load and compile OpenVINO model if model files exist."""
    global compiled_model, resolution_mode, compiled_input_shape
    if not OPENVINO_AVAILABLE or not core:
        return

    model_xml = select_model_xml(MODEL_DIR)
    if not model_xml:
        return

    try:
        start_t = time.perf_counter()
        logger.info("Loading OpenVINO model from %s...", model_xml)
        model = core.read_model(model=model_xml)
        try:
            # A dynamic spatial reshape lets the serving loop honour load shedding.
            # It is not supported by every IR, and promising it when the graph
            # rejected it is what produced the squashed-resize bug: the handler must
            # know which of the two modes it is actually running in.
            model.reshape([-1, 3, -1, -1])
            resolution_mode = "dynamic"
            logger.info(
                "Dynamic reshape succeeded on %s: model accepts per-request spatial sizes",
                model_xml,
            )
        except Exception as reshape_exc:
            resolution_mode = "static"
            logger.warning(
                "Dynamic reshape not supported on %s (%s); staying static",
                model_xml,
                reshape_exc,
            )
        compiled_model = core.compile_model(model=model, device_name="CPU")
        try:
            compiled_input_shape = list(compiled_model.input(0).shape)
        except Exception as exc:
            compiled_input_shape = None
            logger.warning("Could not read compiled input shape: %s", exc)
        if compiled_input_shape is not None:
            # The compiled graph, not the reshape request, decides what the serving
            # loop can feed. A graph that still exposes fixed spatial dimensions
            # ignores X-Target-Resolution and must be reported as static.
            resolution_mode = (
                "dynamic" if spatial_dims_are_dynamic(compiled_input_shape) else "static"
            )
        duration = (time.perf_counter() - start_t) * 1000
        logger.info("OpenVINO model compiled successfully on CPU in %.1f ms", duration)
        if resolution_mode == "dynamic":
            logger.info(
                "Input resolution mode: dynamic (compiled input shape %s); "
                "per-request X-Target-Resolution 320/416 is honoured",
                compiled_input_shape,
            )
        else:
            logger.info(
                "Input resolution mode: static (compiled input shape %s); "
                "requests are served at the model's baked spatial size",
                compiled_input_shape,
            )
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


def point_in_polygon(px: float, py: float, polygon: List[List[float]]) -> bool:
    """Ray casting algorithm testing if 2D normalized point (px, py) is inside polygon (ADR-012 D1)."""
    inside = False
    n = len(polygon)
    if n < 3:
        return False
    j = n - 1
    for i in range(n):
        xi, yi = polygon[i][0], polygon[i][1]
        xj, yj = polygon[j][0], polygon[j][1]
        if ((yi > py) != (yj > py)) and (px < (xj - xi) * (py - yi) / (yj - yi) + xi):
            inside = not inside
        j = i
    return inside


def rasterize_polygon_mask(polygon: List[List[float]], shape: Tuple[int, int]) -> np.ndarray:
    """Rasterizes a normalized [0, 1] polygon onto a 2D boolean mask of dimensions shape (H, W)."""
    h, w = shape
    if not PIL_AVAILABLE or len(polygon) < 3:
        return np.zeros((h, w), dtype=bool)
    from PIL import ImageDraw
    img = Image.new("L", (w, h), 0)
    pts = [(int(round(pt[0] * w)), int(round(pt[1] * h))) for pt in polygon]
    ImageDraw.Draw(img).polygon(pts, outline=1, fill=1)
    return np.array(img, dtype=bool)


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


# Wheel submersion ratio at which a wheel is considered physically under water, and
# the absolute depth that is unambiguously a flooded road. Both the verdict engine and
# the flood_assessment payload read these, so `wheel_submersion_detected` can never
# contradict a FLOODED_DEEP verdict produced by the wheel-depth signal.
WHEEL_SUBMERSION_DELTA = 0.18
FLOOD_DEPTH_CM = 20.0

# Operator calibration file (§2.2) and the in-process de-escalation latch (§2.5).
# Both are module globals so the serving handler shares one validated view per process.
CALIBRATION_PATH = str(os.environ.get("CV_CALIBRATION_PATH") or hc.DEFAULT_CALIBRATION_PATH)
calibration_registry = hc.CalibrationRegistry(CALIBRATION_PATH).load()
hazard_latch = hc.HazardLatch()


def collect_motorcycle_observations(
    motorcycle_boxes: "np.ndarray",
    water_mask: "np.ndarray",
    target_img_size: Tuple[int, int] = (416, 416),
) -> List[MotorcycleObservation]:
    """Per-motorcycle ``(box_bottom_y, delta_sub)`` in target-image pixels (§2.4).

    ``calculate_motorcycle_submersion`` collapses every bike into one aggregate,
    which is enough for the legacy severity ladder but not for the avoidance rule:
    ``through_traffic`` needs to know whether *any* rider crossed the pool, and
    ``upstream_stop`` needs to know whether a rider stopped *before* it. This
    function keeps the geometry the aggregate throws away, using the same
    waterline convention so the two signals never disagree.
    """
    if motorcycle_boxes is None or len(motorcycle_boxes) == 0 or water_mask is None or water_mask.size == 0:
        return []

    h_grid, w_grid = water_mask.shape
    h_img, w_img = target_img_size
    if h_img <= 0 or w_img <= 0:
        return []
    scale_x = w_grid / float(w_img)
    scale_y = h_grid / float(h_img)

    observations: List[MotorcycleObservation] = []
    for b in motorcycle_boxes:
        if len(b) != 4:
            continue
        cx, cy, w, h = b[0], b[1], b[2], b[3]
        if w <= 0 or h <= 0:
            continue
        xmin = max(0.0, cx - w / 2.0)
        ymin = max(0.0, cy - h / 2.0)
        xmax = min(float(w_img), cx + w / 2.0)
        ymax = min(float(h_img), cy + h / 2.0)
        box_h = ymax - ymin
        if box_h <= 0:
            continue

        gx1 = max(0, min(w_grid - 1, int(round(xmin * scale_x))))
        gx2 = max(gx1 + 1, min(w_grid, int(round(xmax * scale_x))))
        gy1 = max(0, min(h_grid - 1, int(round(ymin * scale_y))))
        gy2 = max(gy1 + 1, min(h_grid, int(round(ymax * scale_y))))
        sub_region = water_mask[gy1:gy2, gx1:gx2]

        if not np.any(sub_region):
            # A bike clear of the water is still an observation: it may be stopped
            # upstream of the pool, which is exactly the hazard evidence (#401).
            observations.append(MotorcycleObservation(box_bottom_y=ymax, delta_sub=0.0))
            continue

        water_rows, _ = np.where(sub_region)
        waterline_y = float(np.min(water_rows) + gy1) / scale_y
        delta_sub = hc.compute_delta_sub(ymax, waterline_y, box_h)
        observations.append(MotorcycleObservation(box_bottom_y=ymax, delta_sub=delta_sub))

    return observations


def measure_water_pool(
    water_mask: "np.ndarray",
    target_img_size: Tuple[int, int] = (416, 416),
) -> Tuple[float, Optional[float]]:
    """Return ``(pool_area_px, pool_top_y)`` in target-image space (§2.4).

    The 400 px area floor is expressed in target-image pixels (~4 m² at 416 px
    over a ~20 m field of view), so the segmentation grid must be projected back
    before comparison. ``pool_top_y`` is the uppermost waterline row.
    """
    if water_mask is None or water_mask.size == 0:
        return 0.0, None

    h_grid, w_grid = water_mask.shape
    h_img, w_img = target_img_size
    if h_grid <= 0 or w_grid <= 0 or h_img <= 0 or w_img <= 0:
        return 0.0, None

    area_px = float(np.sum(water_mask)) * (w_img / float(w_grid)) * (h_img / float(h_grid))
    rows = np.where(np.any(water_mask, axis=1))[0]
    if rows.size == 0:
        return 0.0, None
    pool_top_y = float(rows[0]) * (h_img / float(h_grid))
    return area_px, pool_top_y


def triangulate_flood_verdict(
    water_ratio: float,
    max_delta_sub: float,
    depth_estimated_cm: float,
    motorcycles_observed: int = 0,
    rain_event: bool = False,
    *,
    pool_area_px: float = 0.0,
    observations: Sequence[MotorcycleObservation] = (),
    pool_top_y: Optional[float] = None,
) -> Tuple[str, float]:
    """
    Dual-Layer Physical Triangulation Decision Engine (ADR-009 §5, Issue #401 §2.4).
    Layer 1: Road water ratio >= 0.15 activates Layer 2.
    Layer 2: delta_sub >= WHEEL_SUBMERSION_DELTA (depth >= 20cm) triggers FLOODED_DEEP.

    Avoidance escalation replaces the deleted static fallback: a pool that covers
    at least 30 % of the lane with a credible area, and that no rider is crossing,
    is FLOODED_DEEP whether the lane is empty or traffic stopped upstream of it.
    Returns (verdict_str, confidence_float).
    """
    if hc.should_escalate(
        water_ratio=water_ratio,
        pool_area_px=pool_area_px,
        motorcycles_observed=motorcycles_observed,
        observations=observations,
        pool_top_y=pool_top_y,
    ):
        # §2.4: an inferred blockage is real evidence but weaker than a measured
        # wheel depth, so its confidence is capped rather than borrowed from the
        # legacy area branch (which claimed 0.82 for an empty lane).
        return "FLOODED_DEEP", hc.AVOIDANCE_CONFIDENCE_CAP

    if water_ratio >= 0.15 and (
        max_delta_sub >= WHEEL_SUBMERSION_DELTA or depth_estimated_cm >= FLOOD_DEPTH_CM
    ):
        conf = min(0.95, 0.85 + (water_ratio * 0.2))
        return "FLOODED_DEEP", round(conf, 2)

    if water_ratio >= 0.15:
        conf = min(0.92, 0.80 + (water_ratio * 0.2))
        return "FLOODED_SHALLOW", round(conf, 2)

    if water_ratio >= 0.05:
        return "WET", 0.80

    if rain_event:
        return "WET", 0.75

    return "CLEAR", 0.95


def build_flood_assessment(
    verdict: str,
    water_ratio: float,
    max_delta_sub: float,
    depth_estimated_cm: float,
    motorcycles_observed: int,
    motorcycles_submerged: int,
    puddle_polygon_count: int,
    *,
    hazard: Optional[HazardAssessment] = None,
) -> Dict[str, Any]:
    """
    Builds the `flood_assessment` block of the /predict payload.

    Extracted from the request handler so the wire contract can be asserted against
    production code. The previous inline dict had a test that rebuilt the same
    dict and asserted on its own copy, which passes even when this contract changes.

    The four hazard keys are always present (§5.1): a client must never have to
    guess whether a missing key means "unmeasured" or "old build". They default to
    the honest unknown — no level, no basis, unmeasured — never to Level 1.
    """
    return {
        "status": verdict,
        "water_surface_ratio": round(water_ratio, 4),
        "wheel_submersion_detected": max_delta_sub >= WHEEL_SUBMERSION_DELTA,
        "max_submersion_depth_cm": round(depth_estimated_cm, 1),
        "motorcycles_observed": motorcycles_observed,
        "motorcycles_submerged": motorcycles_submerged,
        "puddle_polygon_count": puddle_polygon_count,
        "hazard_level": hazard.level if hazard is not None else None,
        "hazard_basis": hazard.basis if hazard is not None else hc.BASIS_NONE,
        "depth_confidence": (
            hazard.depth_confidence
            if hazard is not None
            else hc.CONFIDENCE_UNMEASURED
        ),
        "calibration_ref": (
            hazard.calibration_ref if hazard is not None else hc.CALIBRATION_REF_NONE
        ),
    }


# Known arterial corridor keywords for priority queue scheduling (S15-T5 / ADR-012 D1).
DEFAULT_CORRIDOR_KEYWORDS: Tuple[str, ...] = (
    "cong_hoa",
    "cong-hoa",
    "vo_van_kiet",
    "vo-van-kiet",
    "dien_bien_phu",
    "dien-bien-phu",
    "truong_chinh",
    "truong-chinh",
    "xa_lo_ha_noi",
    "xa-lo-ha-noi",
    "corridor",
    "hotset",
    "arterial",
)


def is_corridor_request(
    camera_id: str,
    headers: Any,
    configured_corridors: Optional[Sequence[str]] = None,
) -> bool:
    """Determine whether an inference request is for an arterial corridor camera."""
    # 1. Explicit priority / corridor headers
    if headers:
        corridor_hdr = headers.get("X-Corridor")
        if corridor_hdr and corridor_hdr.strip().lower() not in ("0", "false", "no"):
            return True
        priority_hdr = (headers.get("X-Priority") or "").strip().lower()
        if priority_hdr in ("high", "critical", "corridor", "hotset", "1"):
            return True
        hotset_hdr = (headers.get("X-Hotset") or "").strip().lower()
        if hotset_hdr in ("true", "1", "yes"):
            return True

    # 2. Camera ID matching
    cid = camera_id.lower().strip()
    if not cid:
        return False

    if configured_corridors:
        for c in configured_corridors:
            if c and c.lower() in cid:
                return True

    for kw in DEFAULT_CORRIDOR_KEYWORDS:
        if kw in cid:
            return True

    return False


def parse_capture_timestamp(headers: Any) -> Optional[float]:
    """Extract and parse frame capture timestamp from request headers."""
    if not headers:
        return None
    raw_ts = (
        headers.get("X-Capture-Timestamp")
        or headers.get("X-Frame-Timestamp")
        or headers.get("X-OSD-Timestamp")
        or headers.get("X-Timestamp")
    )
    if not raw_ts:
        return None
    raw_ts = raw_ts.strip()
    try:
        return float(raw_ts)
    except ValueError:
        pass
    try:
        from datetime import datetime
        dt = datetime.fromisoformat(raw_ts.replace("Z", "+00:00"))
        return dt.timestamp()
    except Exception:
        return None


class InferenceQueueManager:
    """Adaptive inference queue manager with backpressure and load shedding (S15-T5).

    Tracks active and queued inference requests, enforces backpressure when queue
    depth exceeds max_queue_depth, prioritizes hotset arterial corridor cameras,
    drops stale frames, and dynamically signals resolution downscaling under heavy load.
    """

    def __init__(
        self,
        max_queue_depth: int = 10,
        high_load_threshold: int = 5,
        max_frame_staleness_seconds: float = 60.0,
        concurrency_limit: int = 4,
    ) -> None:
        self.max_queue_depth = max_queue_depth
        self.high_load_threshold = high_load_threshold
        self.max_frame_staleness_seconds = max_frame_staleness_seconds
        self.concurrency_limit = concurrency_limit
        self._lock = threading.Lock()
        self._active_count = 0
        self._queued_count = 0
        self._shed_count = 0
        self._processed_count = 0
        self._semaphore = threading.Semaphore(concurrency_limit)

    def check_admissibility(self, is_corridor: bool) -> Tuple[bool, str]:
        with self._lock:
            in_flight = self._active_count + self._queued_count
            if in_flight >= self.max_queue_depth and not is_corridor:
                self._shed_count += 1
                return False, "queue_full_backpressure"
            self._queued_count += 1
            return True, "admitted"

    def acquire_slot(self, timeout: float = 5.0) -> bool:
        acquired = self._semaphore.acquire(timeout=timeout)
        with self._lock:
            self._queued_count = max(0, self._queued_count - 1)
            if acquired:
                self._active_count += 1
            else:
                self._shed_count += 1
        return acquired

    def release_slot(self) -> None:
        with self._lock:
            self._active_count = max(0, self._active_count - 1)
            self._processed_count += 1
        self._semaphore.release()

    def abort_admission(self) -> None:
        with self._lock:
            self._queued_count = max(0, self._queued_count - 1)

    def record_shed(self) -> None:
        with self._lock:
            self._shed_count += 1

    def should_downscale(self) -> bool:
        with self._lock:
            return (self._active_count + self._queued_count) >= self.high_load_threshold

    @property
    def queue_depth(self) -> int:
        with self._lock:
            return self._active_count + self._queued_count

    def stats(self) -> Dict[str, Any]:
        with self._lock:
            return {
                "active_inferences": self._active_count,
                "queued_inferences": self._queued_count,
                "total_in_flight": self._active_count + self._queued_count,
                "max_queue_depth": self.max_queue_depth,
                "high_load_threshold": self.high_load_threshold,
                "concurrency_limit": self.concurrency_limit,
                "max_frame_staleness_seconds": self.max_frame_staleness_seconds,
                "shed_count": self._shed_count,
                "processed_count": self._processed_count,
                "adaptive_downscale_active": (self._active_count + self._queued_count) >= self.high_load_threshold,
            }

    def reset(self) -> None:
        """Reset counters and slots (for test isolation)."""
        with self._lock:
            self._active_count = 0
            self._queued_count = 0
            self._shed_count = 0
            self._processed_count = 0
        self._semaphore = threading.Semaphore(self.concurrency_limit)


CV_QUEUE_MAX = int(os.environ.get("CV_QUEUE_MAX", 10))
CV_HIGH_LOAD_THRESHOLD = int(os.environ.get("CV_HIGH_LOAD_THRESHOLD", 5))
CV_MAX_STALENESS_SECONDS = float(os.environ.get("CV_MAX_STALENESS_SECONDS", 60.0))
CV_INFERENCE_CONCURRENCY = int(os.environ.get("CV_INFERENCE_CONCURRENCY", 4))
CV_CORRIDOR_CAMERAS = tuple(
    c.strip() for c in os.environ.get("CV_CORRIDOR_CAMERAS", "").split(",") if c.strip()
)

queue_manager = InferenceQueueManager(
    max_queue_depth=CV_QUEUE_MAX,
    high_load_threshold=CV_HIGH_LOAD_THRESHOLD,
    max_frame_staleness_seconds=CV_MAX_STALENESS_SECONDS,
    concurrency_limit=CV_INFERENCE_CONCURRENCY,
)


class CVServer(ThreadingHTTPServer):
    """Multi-threaded HTTP server with resilient socket disconnect handling."""
    daemon_threads = True
    allow_reuse_address = True
    request_queue_size = 128

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
                "backend": CV_BACKEND,
                "precision": effective_precision if CV_BACKEND == "ort" else "fp32",
                "quantized": is_model_quantized if CV_BACKEND == "ort" else False,
                "providers": list(ort_providers) if CV_BACKEND == "ort" else [],
                "openvino_available": OPENVINO_AVAILABLE,
                "available_devices": devices,
                "model_dir": MODEL_DIR,
                "onnx_model": CV_ONNX_MODEL if CV_BACKEND == "ort" else None,
                "model_loaded": compiled_model is not None,
                "resolution_mode": resolution_mode,
                "input_shape": compiled_input_shape,
                "queue_stats": queue_manager.stats(),
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

            # §2.2: re-validate the operator calibration when the file's mtime moved.
            # The loader never raises, so a bad file cannot take the endpoint down.
            calibration_registry.reload_if_changed()
            camera_id = (self.headers.get("X-Camera-ID") or "").strip()
            is_corridor = is_corridor_request(camera_id, self.headers, CV_CORRIDOR_CAMERAS)

            # S15-T5: Frame staleness detection and load shedding
            capture_ts = parse_capture_timestamp(self.headers)
            if capture_ts is not None:
                age_seconds = time.time() - capture_ts
                if age_seconds > queue_manager.max_frame_staleness_seconds:
                    if not is_corridor:
                        queue_manager.record_shed()
                        logger.warning(
                            "Load shedding: dropped stale frame for camera %s (age=%.1fs > %.1fs)",
                            camera_id,
                            age_seconds,
                            queue_manager.max_frame_staleness_seconds,
                        )
                        self._send_json(429, {
                            "status": "shed",
                            "reason": "stale_frame_dropped",
                            "camera_id": camera_id,
                            "age_seconds": round(age_seconds, 2),
                        })
                        return

            # S15-T5: Queue backpressure and admission control
            admitted, shed_reason = queue_manager.check_admissibility(is_corridor=is_corridor)
            if not admitted:
                logger.warning(
                    "Load shedding: queue backpressure shed request for camera %s (queue_depth=%d)",
                    camera_id,
                    queue_manager.queue_depth,
                )
                self._send_json(429, {
                    "status": "shed",
                    "reason": shed_reason,
                    "camera_id": camera_id,
                    "queue_depth": queue_manager.queue_depth,
                })
                return

            slot_acquired = queue_manager.acquire_slot(timeout=5.0)
            if not slot_acquired:
                logger.warning(
                    "Load shedding: queue timeout acquiring inference slot for camera %s",
                    camera_id,
                )
                self._send_json(503, {
                    "status": "shed",
                    "reason": "queue_timeout",
                    "camera_id": camera_id,
                })
                return

            try:
                self._process_predict(camera_id, is_corridor, post_data, start_t)
            finally:
                queue_manager.release_slot()
            return

        self._send_json(404, {"error": "not found"})

    def _process_predict(
        self, camera_id: str, is_corridor: bool, post_data: bytes, start_t: float
    ) -> None:
        content_type = self.headers.get("Content-Type", "")
        regions: List[Dict[str, Any]] = []
        image_bytes = post_data

        # Ingest multipart/form-data containing frame and optional meta (ADR-012 D1)
        if "multipart/form-data" in content_type:
            try:
                from email.parser import BytesParser
                from email.policy import default

                msg = BytesParser(policy=default).parsebytes(
                    b"Content-Type: " + content_type.encode("latin1", errors="replace") + b"\r\n\r\n" + post_data
                )
                found_image = False
                for part in msg.iter_parts():
                    name = part.get_param("name", header="content-disposition")
                    if name in ("frame", "image", "file", "snapshot") or (not found_image and part.get_filename()):
                        payload = part.get_payload(decode=True)
                        if payload:
                            image_bytes = payload
                            found_image = True
                    elif name in ("meta", "metadata", "regions"):
                        raw_meta = part.get_payload(decode=True)
                        if raw_meta:
                            try:
                                meta_json = json.loads(raw_meta.decode("utf-8"))
                                if isinstance(meta_json, dict) and "regions" in meta_json:
                                    regions = meta_json["regions"]
                                elif isinstance(meta_json, list):
                                    regions = meta_json
                            except Exception as meta_exc:
                                logger.warning("Failed to parse multipart meta JSON: %s", meta_exc)
            except Exception as mp_exc:
                logger.warning("Failed to parse multipart payload: %s", mp_exc)

        # If model is loaded and image decoding is available, execute inference
        if compiled_model is not None and PIL_AVAILABLE:
            try:
                image = Image.open(BytesIO(image_bytes)).convert("RGB")
                # Target resolution with load-shedding support (Issue #379 / ADR-012 §1.2 F5 / S15-T5).
                # The decision lives in resolve_input_resolution() so it is unit-testable.
                mode, model_shape = model_input_metadata(compiled_model)
                target_w, target_h = resolve_input_resolution(
                    self.headers.get("X-Target-Resolution"),
                    os.environ.get("CV_INPUT_RESOLUTION"),
                    model_shape,
                )
                load_shed_downscaled = False
                if queue_manager.should_downscale() and mode == "dynamic":
                    target_w, target_h = 320, 320
                    load_shed_downscaled = True
                # A static graph can only execute the shape in its IR. Letterbox
                # straight to the size that will really be fed: an intermediate
                # resize to the requested size would distort the aspect ratio
                # without changing what the model sees.
                actual_w, actual_h = resolve_feed_resolution(
                    target_w, target_h, mode, model_shape
                )
                image_letterboxed, scale, pad_x, pad_y = letterbox(
                    image, target_size=(actual_w, actual_h)
                )

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
                kept_vehicles: List[Tuple[str, np.ndarray]] = []
                water_ratio = 0.0
                max_delta_sub = 0.0
                depth_estimated_cm = 0.0
                submerged_mc = 0
                puddle_count = 0
                model_tag = f"yolov8n_{CV_BACKEND}_{actual_w}"
                road_mask = None
                water_mask = None

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
                        model_tag = f"yolov8n_seg_{CV_BACKEND}_{actual_w}"
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
                                for k in keep:
                                    kept_vehicles.append((cls_name, candidate_boxes[k]))
                                if cls_name == "motorcycle" and len(keep) > 0:
                                    kept_motorcycles = candidate_boxes[keep]
                            else:
                                vehicle_counts[cls_name] = 0

                        vehicle_counts["container"] = 0
                        vehicles = sum(vehicle_counts.values())

                        # Segmentation mask decoding & triangulation
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
                                    np.array(kept_motorcycles), water_mask, target_img_size=(actual_w, actual_h)
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
                                for k in keep:
                                    kept_vehicles.append((cls_name, candidate_boxes[k]))
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
                                for k in keep:
                                    kept_vehicles.append((cls_name, candidate_boxes[k]))
                            else:
                                vehicle_counts[cls_name] = 0

                        vehicles = sum(vehicle_counts.values())
                    else:
                        vehicles = int(np.sum(output_data > 0.5))
                        vehicle_counts["car"] = vehicles
                else:
                    vehicles = 0

                # §2.1/§2.4 frame geometry: per-bike observations, the pool's
                # projected area and its top edge, and the measured depth through
                # whatever reference the frame actually contains.
                motorcycle_boxes = (
                    np.array(kept_motorcycles) if len(kept_motorcycles) > 0 else None
                )
                motorcycle_observations = collect_motorcycle_observations(
                    motorcycle_boxes, water_mask, target_img_size=(actual_w, actual_h)
                )
                pool_area_px, pool_top_y = measure_water_pool(
                    water_mask, target_img_size=(actual_w, actual_h)
                )
                # A motorcycle box is only a ruler when the segmentation head
                # produced a water mask to measure against it. A detection-only
                # model (or a frame with no water proto) has `water_mask is None`,
                # and treating its bikes as a reference would publish a
                # "0.0 cm, MEASURED, MOTORCYCLE_110CM" reading that was never
                # measured (AC10/AC13).
                has_motorcycle_reference = (
                    motorcycle_boxes is not None
                    and water_mask is not None
                    and getattr(water_mask, "size", 0) > 0
                )
                calib_entry = (
                    calibration_registry.entry_for(camera_id) if camera_id else None
                )
                scale_multiplier = (
                    calib_entry.scale_multiplier if calib_entry else 1.0
                )
                water_present = (
                    water_mask is not None
                    and getattr(water_mask, "size", 0) > 0
                    and (pool_area_px > 0 or water_ratio > 0)
                )
                has_curb_reference = bool(
                    calib_entry is not None
                    and (
                        (calib_entry.curb_px is not None and calib_entry.curb_px > 0)
                        or (calib_entry.curb_polygon is not None and len(calib_entry.curb_polygon) >= 2)
                    )
                    and water_present
                )
                has_camera_override = (
                    (has_motorcycle_reference or has_curb_reference)
                    and scale_multiplier != 1.0
                )
                hazard_calibration_ref = hc.resolve_calibration_ref(
                    has_motorcycle_reference=has_motorcycle_reference,
                    has_curb_reference=has_curb_reference,
                    has_camera_override=has_camera_override,
                )

                hazard_depth_cm: Optional[float] = None
                if has_motorcycle_reference:
                    hazard_depth_cm = hc.estimate_depth_cm(max_delta_sub, scale_multiplier)
                elif has_curb_reference and calib_entry is not None:
                    # Dynamic curb reference depth calculation (S15-T5 / §2.1):
                    rows = (
                        np.where(np.any(water_mask, axis=1))[0]
                        if water_mask is not None
                        else np.array([])
                    )
                    if rows.size > 0:
                        h_grid = water_mask.shape[0]
                        water_depth_px = float(rows[-1] - rows[0]) * (actual_h / float(h_grid))
                    else:
                        water_depth_px = 0.0
                    curb_px_ref = (
                        calib_entry.curb_px
                        if (calib_entry.curb_px and calib_entry.curb_px > 0)
                        else 90.0
                    )
                    hazard_depth_cm = hc.estimate_depth_from_curb_cm(
                        water_depth_px, curb_px_ref, scale_multiplier
                    )
                    depth_estimated_cm = hazard_depth_cm
                    logger.info(
                        "Camera %s using curb reference: calibration_ref=%s, depth=%.1f cm (curb_px=%.1f, scale=%.2f)",
                        camera_id,
                        hazard_calibration_ref,
                        hazard_depth_cm,
                        curb_px_ref,
                        scale_multiplier,
                    )

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
                    pool_area_px=pool_area_px,
                    observations=motorcycle_observations,
                    pool_top_y=pool_top_y,
                )

                # Physical hazard layer (§2.1-§2.5). Depth is measured only when a
                # reference object is in the frame; otherwise it stays None and the
                # level can only come from avoidance. The latch holds a drop for one
                # frame so a single passing vehicle cannot erase a pool.
                hazard_assessment = hc.assess_hazard(
                    depth_cm=hazard_depth_cm,
                    calibration_ref=hazard_calibration_ref,
                    water_ratio=water_ratio,
                    pool_area_px=pool_area_px,
                    motorcycles_observed=vehicle_counts["motorcycle"],
                    observations=motorcycle_observations,
                    pool_top_y=pool_top_y,
                    logger=logger,
                )
                published_assessment = hazard_assessment
                if camera_id:
                    # §2.5: latch state is per camera. A headerless caller has no
                    # identity to key on, so it must not share one bucket. The latch
                    # returns the *held* assessment when a drop is suppressed, so the
                    # published level keeps its own basis, depth, confidence and
                    # calibration reference instead of the drop frame's.
                    published_assessment = hazard_latch.apply(camera_id, hazard_assessment)

                # Directional polygon assignment & water reconciliation (ADR-012 D1)
                region_results: List[Dict[str, Any]] = []
                if regions:
                    for reg in regions:
                        reg_id = reg.get("region_id", "")
                        direction = reg.get("direction", "")
                        poly = reg.get("polygon", [])
                        share = reg.get("capacity_share", 0.5)

                        reg_counts = {
                            "motorcycle": 0,
                            "car": 0,
                            "bus": 0,
                            "truck": 0,
                            "container": 0,
                        }
                        for cls_name, box in kept_vehicles:
                            cx, cy, w, h = box[0], box[1], box[2], box[3]
                            norm_cx, norm_by = box_polygon_anchor(
                                cx, cy, w, h, actual_w, actual_h
                            )
                            if point_in_polygon(norm_cx, norm_by, poly):
                                reg_counts[cls_name] = reg_counts.get(cls_name, 0) + 1

                        reg_water_ratio = 0.0
                        if water_mask is not None and water_mask.size > 0:
                            reg_mask = rasterize_polygon_mask(poly, water_mask.shape)
                            if road_mask is not None and road_mask.size == water_mask.size:
                                reg_road = np.logical_and(road_mask, reg_mask)
                                reg_road_sum = float(np.sum(reg_road))
                                if reg_road_sum > 0:
                                    reg_water = np.logical_and(water_mask, reg_road)
                                    reg_water_ratio = float(np.sum(reg_water)) / reg_road_sum
                            elif np.sum(reg_mask) > 0:
                                reg_water = np.logical_and(water_mask, reg_mask)
                                reg_water_ratio = float(np.sum(reg_water)) / float(np.sum(reg_mask))

                        region_results.append({
                            "region_id": reg_id,
                            "direction": direction,
                            "capacity_share": share,
                            "vehicle_counts": reg_counts,
                            "vehicles_detected": sum(reg_counts.values()),
                            "water_ratio": round(reg_water_ratio, 4),
                        })

                response_payload = {
                    "service": "projectkv-cv-engine",
                    "vehicles_detected": vehicles,
                    "vehicle_counts": vehicle_counts,
                    "traffic_severity": traffic_severity,
                    "flood_severity": flood_verdict,
                    "water_ratio": round(water_ratio, 4),
                    "depth_estimated_cm": round(depth_estimated_cm, 1),
                    # §5.1 additive hazard contract. An unmeasured depth is null,
                    # never 0.0: 0 means "dry, verified" (AC13).
                    "flood_depth_cm": (
                        None
                        if published_assessment.depth_cm is None
                        else round(published_assessment.depth_cm, 1)
                    ),
                    "flood_hazard_level": published_assessment.level,
                    "flood_hazard_basis": published_assessment.basis,
                    "flood_depth_confidence": published_assessment.depth_confidence,
                    "calibration_ref": published_assessment.calibration_ref,
                    "confidence": round(float(flood_conf if flood_verdict != "CLEAR" else 0.88), 2),
                    "latency_ms": round(latency, 2),
                    "model": model_tag,
                    "input_resolution": [actual_w, actual_h],
                    "flood_assessment": build_flood_assessment(
                        verdict=flood_verdict,
                        water_ratio=water_ratio,
                        max_delta_sub=max_delta_sub,
                        depth_estimated_cm=depth_estimated_cm,
                        motorcycles_observed=vehicle_counts["motorcycle"],
                        motorcycles_submerged=submerged_mc,
                        puddle_polygon_count=puddle_count,
                        hazard=published_assessment,
                    ),
                }
                if region_results:
                    response_payload["regions"] = region_results
                if load_shed_downscaled:
                    response_payload["load_shedding_downscale"] = True

                self._send_json(200, response_payload)
                return
            except Exception as exc:
                logger.error("Inference execution error: %s", exc)
                self._send_json(500, {"error": f"inference failed: {str(exc)}"})
                return

        # Standby mode: no model loaded — return 503 to prevent Go client from
        # treating this as a real inference result (ADR-004 §14 Finding 1).
        latency = (time.perf_counter() - start_t) * 1000
        standby_payload = {
            "source": "standby_dummy",
            "confidence": 0.0,
            "latency_ms": round(latency, 2),
            "mode": "standby_ready",
            "note": f"{CV_BACKEND} engine ready. Model weights mountable at runtime."
        }
        if regions:
            standby_payload["regions"] = [
                {
                    "region_id": r.get("region_id", ""),
                    "direction": r.get("direction", ""),
                    "capacity_share": r.get("capacity_share", 0.5),
                    "vehicle_counts": {"motorcycle": 0, "car": 0, "bus": 0, "truck": 0, "container": 0},
                    "vehicles_detected": 0,
                    "water_ratio": 0.0,
                }
                for r in regions
            ]
        self._send_json(503, standby_payload)
        return

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
