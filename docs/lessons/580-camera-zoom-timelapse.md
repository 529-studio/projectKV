# Lesson 580: Mobile Camera Snapshot Tap-to-Zoom & Time-Lapse Compare

**Sprint:** S20
**Issue:** #580
**Author:** AI agent
**Date:** 2026-11-28

---

## Context

Field water-level verification on `390x844` mobile screens needed quick
zoom/pan on the live snapshot inside `CameraModal.tsx` plus an archived
before-flood baseline comparison without pulling users out of the modal.

---

## Key Decisions

1. **Conditional `touch-none` only when zoomed**: `touch-none` is applied to the
   snapshot viewport only while `zoomScale > 1`, so vertical modal scrolling
   stays smooth at `1x` while 2-finger pinch and 1-finger pan work smoothly
   when zoomed (`1x`–`3x`).
2. **Non-blocking archived baseline lookup**: Queries `/api/archiver/snapshots`
   for the camera's earliest frame of the day and falls back gracefully to a
   disabled `Before` / `Compare` state when no archived frame exists.
3. **Kiosk touch targets**: Zoom toggle, reset, and `Live` / `Before` /
   `Compare` controls all enforce `min-h-[48px] min-w-[48px]`.
