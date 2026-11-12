#!/usr/bin/env python3
"""
image_geometry.py — Pure NumPy image geometry helpers for SAM pre-labeling.

Implements the mask-to-polygon path (connected components, boundary tracing,
Ramer-Douglas-Peucker simplification), the YOLO letterbox transform, and the
heuristic puddle-candidate proposer. No OpenCV, no scikit-image: the toolchain
stays on numpy + Pillow so it runs inside constrained CPU environments.

Adheres to AGENTS.md Rules:
- Rule 1: zero-emoji, 100% English code and logs.
- Rule 8: no secrets and no configuration in this module.
"""

from typing import Dict, List, Optional, Sequence, Tuple

import numpy as np
from numpy.lib.stride_tricks import sliding_window_view

# Boundary tracing neighbor order: E, SE, S, SW, W, NW, N, NE (clockwise).
_NEIGHBORS: Tuple[Tuple[int, int], ...] = (
    (0, 1), (1, 1), (1, 0), (1, -1), (0, -1), (-1, -1), (-1, 0), (-1, 1),
)
# Reverse lookup for Moore tracing: grid offset -> clockwise neighbor index.
_DIR_INDEX: Dict[Tuple[int, int], int] = {
    offset: i for i, offset in enumerate(_NEIGHBORS)
}

# Puddle proposer bounds: a standing-water candidate must be a small-ish,
# well-formed region; anything larger is more likely roadway shadow or sky.
PUDDLE_MIN_AREA_FRAC = 0.001
PUDDLE_MAX_AREA_FRAC = 0.08
PUDDLE_MAX_ASPECT = 6.0
# Below this mean luminance (0-255) specular/water discrimination is
# unreliable (ADR-009 section 4.3); no puddle proposals are emitted.
PUDDLE_MIN_MEAN_LUMA = 40.0


def letterbox(img: np.ndarray, size: int) -> Tuple[np.ndarray, float, Tuple[int, int]]:
    """
    Resizes an HWC RGB uint8 image to a size x size canvas with gray padding,
    preserving aspect ratio (YOLOv8 inference convention).

    Returns (canvas float32 RGB in [0, 1] shape (size, size, 3), scale factor,
    (pad_x, pad_y) offsets in canvas pixels).
    """
    h, w = img.shape[:2]
    scale = min(size / h, size / w)
    new_h, new_w = int(round(h * scale)), int(round(w * scale))
    resized = np.asarray(
        _resize_bilinear(img, new_h, new_w), dtype=np.float32
    )
    canvas = np.full((size, size, 3), 114.0 / 255.0, dtype=np.float32)
    pad_x = (size - new_w) // 2
    pad_y = (size - new_h) // 2
    canvas[pad_y:pad_y + new_h, pad_x:pad_x + new_w, :] = resized / 255.0
    return canvas, scale, (pad_x, pad_y)


def _resize_bilinear(img: np.ndarray, new_h: int, new_w: int) -> np.ndarray:
    """Bilinear resize of an HWC image to (new_h, new_w) without cv2/PIL."""
    h, w = img.shape[:2]
    if h == new_h and w == new_w:
        return img
    row_linspace = np.linspace(0, h - 1, new_h, dtype=np.float32)
    col_linspace = np.linspace(0, w - 1, new_w, dtype=np.float32)
    y0 = np.clip(np.floor(row_linspace).astype(np.int64), 0, h - 1)
    y1 = np.clip(y0 + 1, 0, h - 1)
    x0 = np.clip(np.floor(col_linspace).astype(np.int64), 0, w - 1)
    x1 = np.clip(x0 + 1, 0, w - 1)
    wy = (row_linspace - y0)[:, None, None]
    wx = (col_linspace - x0)[None, :, None]
    rows_y0, rows_y1 = y0[:, None], y1[:, None]
    cols_x0, cols_x1 = x0[None, :], x1[None, :]
    top = img[rows_y0, cols_x0] * (1 - wx) + img[rows_y0, cols_x1] * wx
    bottom = img[rows_y1, cols_x0] * (1 - wx) + img[rows_y1, cols_x1] * wx
    return top * (1 - wy) + bottom * wy


def scale_boxes_back(
    boxes: Sequence[Sequence[float]],
    scale: float,
    pads: Tuple[int, int],
    orig_h: int,
    orig_w: int,
) -> np.ndarray:
    """
    Maps letterboxed cx, cy, w, h boxes back to original-image xyxy pixels.
    """
    out = np.zeros((len(boxes), 4), dtype=np.float32)
    pad_x, pad_y = pads
    for i, b in enumerate(boxes):
        cx, cy, bw, bh = float(b[0]), float(b[1]), float(b[2]), float(b[3])
        x1 = (cx - bw / 2.0 - pad_x) / scale
        y1 = (cy - bh / 2.0 - pad_y) / scale
        x2 = (cx + bw / 2.0 - pad_x) / scale
        y2 = (cy + bh / 2.0 - pad_y) / scale
        out[i] = (
            min(max(x1, 0.0), orig_w - 1),
            min(max(y1, 0.0), orig_h - 1),
            min(max(x2, 0.0), orig_w - 1),
            min(max(y2, 0.0), orig_h - 1),
        )
    return out


def nms_xyxy(boxes: np.ndarray, scores: np.ndarray, iou_thresh: float = 0.45) -> List[int]:
    """Standard greedy NMS over xyxy boxes; returns kept indices."""
    if len(boxes) == 0:
        return []
    order = np.argsort(-scores)
    keep: List[int] = []
    while order.size > 0:
        i = int(order[0])
        keep.append(i)
        if order.size == 1:
            break
        rest = order[1:]
        xx1 = np.maximum(boxes[i, 0], boxes[rest, 0])
        yy1 = np.maximum(boxes[i, 1], boxes[rest, 1])
        xx2 = np.minimum(boxes[i, 2], boxes[rest, 2])
        yy2 = np.minimum(boxes[i, 3], boxes[rest, 3])
        inter = np.maximum(0.0, xx2 - xx1) * np.maximum(0.0, yy2 - yy1)
        areas = (boxes[:, 2] - boxes[:, 0]) * (boxes[:, 3] - boxes[:, 1])
        union = areas[i] + areas[rest] - inter
        iou = np.where(union > 0, inter / np.maximum(union, 1e-9), 0.0)
        order = rest[iou <= iou_thresh]
    return keep


def largest_component(mask: np.ndarray) -> np.ndarray:
    """
    Returns the largest 8-connected component of a boolean mask.

    Row-run based union-find labeling: each horizontal run becomes a node and
    runs on adjacent rows are merged when their column spans are at most one
    pixel apart (8-connectivity, so diagonal-only contact still joins). The
    per-row run detection is vectorized, so 1080p masks stay fast in pure
    NumPy/Python.
    """
    m = np.ascontiguousarray(mask.astype(bool))
    h, w = m.shape
    parent: List[int] = []

    def find(x: int) -> int:
        root = x
        while parent[root] != root:
            root = parent[root]
        while parent[x] != root:
            parent[x], x = root, parent[x]
        return root

    def union(a: int, b: int) -> None:
        ra, rb = find(a), find(b)
        if ra != rb:
            parent[max(ra, rb)] = min(ra, rb)

    run_nodes = np.full((h, w), -1, dtype=np.int64)
    for y in range(h):
        row = m[y]
        if not row.any():
            continue
        diffs = np.diff(np.concatenate(([False], row, [False])).astype(np.int8))
        starts = np.where(diffs == 1)[0]
        ends = np.where(diffs == -1)[0]
        for s, e in zip(starts, ends):
            node = len(parent)
            parent.append(node)
            run_nodes[y, s:e] = node
            if y > 0:
                # 8-connectivity: a run touches runs on the row above when
                # their column spans are at most one pixel apart, including
                # diagonal-only contact. A plain [s:e] slice would implement
                # 4-connectivity and silently split one-pixel diagonal
                # antennas off the component they are attached to.
                lo = max(0, s - 1)
                above = run_nodes[y - 1, lo:e + 1]
                for prev in np.unique(above[above >= 0]):
                    union(node, int(prev))

    best_root, best_size = -1, -1
    size_by_root: Dict[int, int] = {}
    for node, n in enumerate(parent):
        root = find(node)
        size_by_root[root] = size_by_root.get(root, 0) + 1
        if size_by_root[root] > best_size:
            best_size = size_by_root[root]
            best_root = root
    if best_root < 0:
        return np.zeros_like(m)
    keep = {node for node in range(len(parent)) if find(node) == best_root}
    return np.isin(run_nodes, np.fromiter(keep, dtype=np.int64)) & m


def trace_boundary(mask: np.ndarray) -> List[Tuple[float, float]]:
    """
    Traces the outer boundary of the largest component of a boolean mask with
    Moore-neighbor tracing (clockwise, starting from the topmost-leftmost
    boundary pixel). Returns (x, y) pixel-coordinate vertices.

    Termination uses Jacob's stopping criterion: the trace is complete only
    when it revisits the start pixel in the *same* state it started from (same
    backtrack neighbor). Testing just "we are back at the start pixel" is not
    enough: a single-pixel protrusion tip or a diagonally clipped corner is
    re-entered two steps after leaving it, which used to end the trace with a
    three-point path covering only the tip. Tracking the backtrack pixel makes
    the state a function of the contour geometry, so closure is only detected
    after the full outer boundary has been walked.
    """
    comp = largest_component(mask)
    if not comp.any():
        return []
    # One-cell background border so every neighbor lookup stays in bounds.
    grid = np.pad(comp, 1, mode="constant", constant_values=False)
    coords = np.argwhere(grid)
    # Topmost, then leftmost boundary pixel (argwhere is row-major).
    start = (int(coords[0][0]), int(coords[0][1]))
    # The row-major scan reaches `start` from its west neighbor, which is a
    # background cell. That pair is the closed contour's entry state: walking
    # the outer boundary returns to exactly this (pixel, backtrack) pair.
    start_backtrack = (start[0], start[1] - 1)

    path: List[Tuple[int, int]] = [start]
    cur, backtrack = start, start_backtrack
    seen = {(start, start_backtrack)}
    # Safety net only: the state space is 9 * grid.size, and the loop provably
    # stops when a state repeats, so this bound is never the normal exit.
    for _ in range(grid.size * 9):
        # Scan clockwise from the backtrack cell; the previous pixel itself is
        # already foreground and must be skipped to avoid an immediate U-turn.
        base = _DIR_INDEX[(backtrack[0] - cur[0], backtrack[1] - cur[1])]
        next_state: Optional[Tuple[Tuple[int, int], Tuple[int, int]]] = None
        for k in range(1, 9):
            direction = (base + k) % 8
            dy, dx = _NEIGHBORS[direction]
            nxt = (cur[0] + dy, cur[1] + dx)
            if grid[nxt]:
                # The pixel scanned just before `nxt` becomes the next
                # backtrack cell (Moore neighbor bookkeeping).
                by, bx = _NEIGHBORS[(base + k - 1) % 8]
                next_state = (nxt, (cur[0] + by, cur[1] + bx))
                break
        if next_state is None:
            break
        cur, backtrack = next_state
        path.append(cur)
        if next_state in seen:
            # Jacob's stopping criterion, state form: the contour has returned
            # to a (pixel, backtrack) pair it has already occupied, so the
            # trace is a closed loop and every further step would repeat it.
            break
        seen.add(next_state)
    # Convert padded coordinates back to unpadded (x, y) pixel space.
    return [(float(x - 1), float(y - 1)) for y, x in path]


def rdp_simplify(
    points: Sequence[Tuple[float, float]], epsilon: float
) -> List[Tuple[float, float]]:
    """Ramer-Douglas-Peucker polyline simplification (iterative)."""
    if len(points) < 3:
        return list(points)
    pts = np.asarray(points, dtype=np.float64)
    keep = np.zeros(len(pts), dtype=bool)
    keep[0] = keep[-1] = True
    stack = [(0, len(pts) - 1)]
    while stack:
        lo, hi = stack.pop()
        if hi <= lo + 1:
            continue
        seg = pts[hi] - pts[lo]
        seg_len = float(np.hypot(seg[0], seg[1]))
        rel = pts[lo + 1:hi] - pts[lo]
        if seg_len < 1e-9:
            dist = np.hypot(rel[:, 0], rel[:, 1])
        else:
            dist = np.abs(rel[:, 0] * seg[1] - rel[:, 1] * seg[0]) / seg_len
        max_i = int(np.argmax(dist))
        if dist[max_i] > epsilon:
            idx = lo + 1 + max_i
            keep[idx] = True
            stack.append((lo, idx))
            stack.append((idx, hi))
    return [(float(p[0]), float(p[1])) for p in pts[keep]]


def mask_to_polygon(
    mask: np.ndarray, max_vertices: int = 48, epsilon: float = 2.0
) -> List[Tuple[float, float]]:
    """
    Converts a boolean mask to a simplified boundary polygon in pixel (x, y)
    coordinates, reducing vertex count to max_vertices via RDP with an
    adaptive epsilon.
    """
    boundary = trace_boundary(mask)
    if len(boundary) < 3:
        return []
    # Deduplicate consecutive repeats from the closed trace.
    deduped: List[Tuple[float, float]] = [boundary[0]]
    for pt in boundary[1:]:
        if abs(pt[0] - deduped[-1][0]) > 1e-6 or abs(pt[1] - deduped[-1][1]) > 1e-6:
            deduped.append(pt)
    if len(deduped) > 2:
        first, last = deduped[0], deduped[-1]
        if abs(first[0] - last[0]) < 1e-6 and abs(first[1] - last[1]) < 1e-6:
            deduped.pop()
    poly = rdp_simplify(deduped, epsilon)
    iter_count = 0
    while len(poly) > max_vertices and iter_count < 12:
        epsilon *= 1.6
        poly = rdp_simplify(deduped, epsilon)
        iter_count += 1
    return poly


def normalize_polygon(
    polygon: Sequence[Tuple[float, float]], width: int, height: int
) -> List[List[float]]:
    """Converts pixel (x, y) vertices to [0, 1]-normalized [x, y] pairs."""
    if width <= 0 or height <= 0:
        return []
    return [
        [
            min(1.0, max(0.0, float(x) / width)),
            min(1.0, max(0.0, float(y) / height)),
        ]
        for x, y in polygon
    ]


def mask_area_frac(mask: np.ndarray) -> float:
    """Fraction of True pixels in a boolean mask."""
    if mask.size == 0:
        return 0.0
    return float(np.count_nonzero(mask)) / float(mask.size)


def mask_iou(a: np.ndarray, b: np.ndarray) -> float:
    """IoU between two boolean masks of identical shape."""
    if a.shape != b.shape or a.size == 0:
        return 0.0
    inter = np.count_nonzero(a & b)
    union = np.count_nonzero(a | b)
    return float(inter) / float(union) if union > 0 else 0.0


def box_to_xyxy_norm(
    box: Sequence[float], width: int, height: int
) -> Optional[List[float]]:
    """Clamps a pixel xyxy box to image bounds and normalizes to [0, 1]."""
    if width <= 0 or height <= 0:
        return None
    x1 = min(max(float(box[0]), 0.0), width - 1) / width
    y1 = min(max(float(box[1]), 0.0), height - 1) / height
    x2 = min(max(float(box[2]), 0.0), width - 1) / width
    y2 = min(max(float(box[3]), 0.0), height - 1) / height
    if x2 - x1 < 1e-4 or y2 - y1 < 1e-4:
        return None
    return [x1, y1, x2, y2]


def propose_puddle_boxes(
    img: np.ndarray, horizon_frac: float, max_boxes: int = 2
) -> List[List[float]]:
    """
    Heuristic standing-water candidate proposer (proposal-only; ADR-009
    section 4.3): smooth, dark regions in the lower frame become SAM box
    prompts for water_puddle. Returns up to max_boxes normalized xyxy boxes.

    Specular wet asphalt is indistinguishable from standing water without
    human judgment, so candidates always require human verification and are
    suppressed entirely on very dark (night) frames.
    """
    h, w = img.shape[:2]
    y0 = int(h * min(max(horizon_frac, 0.1), 0.8))
    region = img[y0:, :, :].astype(np.float32)
    if region.size == 0:
        return []
    luma = 0.299 * region[:, :, 0] + 0.587 * region[:, :, 1] + 0.114 * region[:, :, 2]
    if float(luma.mean()) < PUDDLE_MIN_MEAN_LUMA:
        return []

    # Downsample for speed; puddle proposals do not need pixel precision.
    scale = max(1.0, max(luma.shape) / 256.0)
    small = _resize_bilinear(luma[:, :, None].repeat(3, axis=2), int(luma.shape[0] / scale), int(luma.shape[1] / scale))[:, :, 0]
    # Smooth with a 5x5 box filter to merge specular speckle.
    kernel = np.ones((5, 5), dtype=np.float32) / 25.0
    padded = np.pad(small, 2, mode="edge")
    windows = sliding_window_view(padded, (5, 5))
    smooth = np.einsum("ijkl,kl->ij", windows, kernel)

    # Contrast-relative threshold: the darkest cluster below one standard
    # deviation of the scene mean, clamped to a sane percentile band. A fixed
    # low percentile would swallow the whole frame when the dark patch covers
    # less than that percentile of pixels.
    mean, std = float(smooth.mean()), float(smooth.std())
    thresh = min(
        max(mean - std, float(np.percentile(smooth, 2.0))),
        float(np.percentile(smooth, 25.0)),
    )
    dark = smooth <= thresh
    boxes: List[List[float]] = []
    # Connected components via the same row-run labeling used for masks.
    labels = _label_runs(dark)
    if labels is None:
        return []
    for comp_id in range(labels.max() + 1 if labels.max() >= 0 else 0):
        ys, xs = np.where(labels == comp_id)
        if len(ys) == 0:
            continue
        area_frac = len(ys) / float(labels.size)
        if not (PUDDLE_MIN_AREA_FRAC <= area_frac <= PUDDLE_MAX_AREA_FRAC):
            continue
        bw, bh = float(xs.max() - xs.min() + 1), float(ys.max() - ys.min() + 1)
        if bw / bh > PUDDLE_MAX_ASPECT or bh / bw > PUDDLE_MAX_ASPECT:
            continue
        # Map back to full-resolution normalized coordinates.
        gx1 = min(float(xs.min()) * scale, w - 1) / w
        gx2 = min((float(xs.max()) + 1) * scale, w) / w
        gy1 = min(float(ys.min()) * scale + y0, h - 1) / h
        gy2 = min((float(ys.max()) + 1) * scale + y0, h) / h
        boxes.append([gx1, gy1, gx2, gy2])
        if len(boxes) >= max_boxes:
            break
    return boxes


def _label_runs(dark: np.ndarray) -> Optional[np.ndarray]:
    """8-connected component labeling for small boolean grids (puddle proposer)."""
    m = dark.astype(bool)
    if not m.any():
        return None
    labels = np.full(m.shape, -1, dtype=np.int64)
    cur = 0
    h, w = m.shape
    for y in range(h):
        for x in range(w):
            if m[y, x] and labels[y, x] < 0:
                stack = [(y, x)]
                labels[y, x] = cur
                while stack:
                    cy, cx = stack.pop()
                    for dy in (-1, 0, 1):
                        for dx in (-1, 0, 1):
                            ny, nx = cy + dy, cx + dx
                            if 0 <= ny < h and 0 <= nx < w and m[ny, nx] and labels[ny, nx] < 0:
                                labels[ny, nx] = cur
                                stack.append((ny, nx))
                cur += 1
    return labels if cur > 0 else None
