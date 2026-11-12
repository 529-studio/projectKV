# Lesson 438: Moore Boundary Tracing Premature Termination and Jacob's Stopping Criterion

## Context

Sprint 13 Issue #438 (S13-T1) put SAM pre-labeling to work on live camera
frames. The geometry stage is a pure-NumPy chain: SAM proposals are unioned
into one stable mask, `largest_component` keeps the region under test, and
`mask_to_polygon` walks its outer boundary with clockwise Moore-neighbor
tracing before RDP simplification. The input is not a tidy geometric shape.
Road scenes arrive with one-pixel antennas where a pole or sign edge touches
the roadway, clipped corners where a region meets the frame border, and
diagonal-only contacts between a mask and its sliver. The stage reported
**370 polygons over 744 candidate frames**, and the missing 40 % was first
read as weak SAM masks rather than as a defect in the code that was supposed
to describe them.

## Problem & Root Cause

1. **The stopping test was a pixel, not a state.** The trace ended when
   `cur == start and len(path) > 2`. Moore tracing can re-enter the start
   pixel from a different approach two steps after leaving it, which is
   exactly what happens when the start pixel is a protrusion tip: the
   topmost-leftmost pixel of a single-pixel antenna has one foreground
   neighbor, so the contour wraps back onto it almost immediately. The trace
   then "closed" with a three-vertex path covering the tip and nothing else.
2. **A degenerate trace silently deleted the frame.** `mask_to_polygon`
   returns `[]` below three boundary points, and the pre-labeling caller
   drops any candidate whose polygon has fewer than
   `MIN_POLYGON_POINTS`. The failure never surfaced as "bad polygon": it
   surfaced as a frame with no label, indistinguishable from a frame where
   SAM found nothing.
3. **Component labeling was 4-connected.** Runs on adjacent rows were merged
   through a plain `run_nodes[y - 1, s:e]` slice, so a run that touched only
   diagonally was a separate component. An antenna attached at one diagonal
   pixel was split off before tracing began, and `largest_component` kept
   the slab and discarded the antenna. Correcting the trace alone would not
   have restored those shapes.
4. **The tests only contained shapes that cannot fail.** Every existing
   geometry test used smooth rectangles and squares. On a rectangle the
   start pixel is a corner with two orthogonal foreground neighbors, the
   trace moves away along an edge, and the broken condition is never
   exercised. The bug was invisible to a suite that tested geometry rather
   than data.

## Solution

1. **Terminate on the state `(pixel, backtrack)`.** Jacob's stopping
   criterion returns the trace only when it revisits a state it has already
   occupied. The start state is the topmost-leftmost pixel paired with its
   west neighbor, which is background: the row-major scan reaches the start
   from there, so that pair is the closed contour's true entry state. A
   `seen` set records each `(pixel, backtrack)` pair and the loop ends on the
   first repeat, after the full outer boundary has been walked.
2. **Scan clockwise from the backtrack cell.** The neighbor order starts at
   `_DIR_INDEX[backtrack - cur] + 1` and skips the previous pixel, so the
   trace cannot U-turn into itself. The cell scanned immediately before the
   chosen neighbor becomes the next backtrack cell, which is what makes the
   state a function of contour geometry rather than of loop position.
3. **Merge runs across the 8-connected window.** `largest_component` reads
   `run_nodes[y - 1, max(0, s - 1):e + 1]`, so a run overlapping a row above
   within one column joins the component. Diagonal contact now survives
   labeling, and the antenna reaches the tracer instead of being pruned.
4. **Six regression tests pin the shapes that broke it.** A two-pixel
   protrusion tip, a diagonally clipped staircase corner, a one-pixel
   antenna, a diagonally attached antenna at the component level, a closed
   and 8-connected trace, and two slabs joined at a single corner. Each
   asserts a non-degenerate polygon whose bounding box matches the mask, and
   each fails against the old `cur == start` condition.
5. **Measure the yield, not just the trace.** With the state criterion and
   the wider merge window, the same live frame set produced **6,386
   polygons across 744 frames**, up from 370. `python3 -m unittest
   tools/seg/image_geometry_test.py` runs 23 tests, the six new ones
   included, and all pass.

## Prevention & Guardrails

- **Test the ragged shape, not the rectangle.** A geometric primitive is a
  fixture that agrees with the algorithm's assumptions. Every boundary,
  component and contour test needs at least one case with a protrusion, a
  clipped corner, a diagonal-only contact, or a one-pixel appendage, because
  those are the states where start-pixel logic and connectivity rules fail.
- **Verify yield against real frames (Rule 4, Rule 10).** A polygon count is
  an end-to-end measurement: compare emitted labels with candidate frames on
  live captures before accepting a geometry change. A 40 % shortfall is a
  correctness signal, and "the masks were weak" is a hypothesis to test
  against the tracer, never a conclusion to ship on.
- **Make silent emptiness loud.** A helper that returns `[]` for a degenerate
  result hands its caller an easy frame drop. Count drops by cause at the
  call site so a collapsed trace is reported as a geometry failure rather
  than as an empty frame, and so a future regression shows up as a number in
  the run summary instead of a smaller dataset.
