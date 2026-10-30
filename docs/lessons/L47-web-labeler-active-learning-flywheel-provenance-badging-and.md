## Lesson 47: Web Labeler Active Learning Flywheel, Provenance Badging, and Candidate Fast-Approval

- **Incident & Tracking:** Issue #264, Kanban **[KV-12]**, Sprint 7 Task S7-T7d (`feat/s7-t7d-web-labeler-filter`).
- **Symptom:**
  As automated data harvesters began continuously generating candidate evaluation frames and pseudo-labels on Cloudflare R2, human annotators in `apps/web/src/app/labeler` had no mechanism to distinguish pseudo-labeled frames from certified gold-standard labels or unlabeled frames. Reviewing candidates required re-entering or re-submitting every parameter from scratch, slowing down model evaluation and active learning flywheel turnarounds.
- **Root Cause:**
  1. Flat Queue Presentation: `apps/web/src/app/labeler/page.tsx` loaded all partition snapshots into a single unpartitioned sequential queue without status or provenance indicators.
  2. Missing Fast-Approval Ergonomics: Annotators reviewing auto-harvested labels had to manually press `Enter` after re-verifying every field rather than having a dedicated single-hotkey approval flow that atomically updates annotator attribution to the human reviewer and advances the queue.
  3. Provenance Ambiguity: UI did not surface whether an existing label originated from autonomous heuristics (`harvester/auto`) or human domain experts.
- **Mandatory Takeaways & Engineering Resolutions:**
  1. **Tri-State Status Filtering (`apps/web/src/utils/labelerUtils.ts`)**:
     - Added `LabelFilterStatus` type (`all`, `pending_auto`, `approved_human`) and helper functions `isAutoHarvested(label)` and `filterSnapshots(snapshots, labels, filter)`.
     - Filter pills in the labeler control bar display live count chips: `[Tất cả (N)]`, `[Chờ duyệt (Auto) (M)]`, `[Đã duyệt (Human) (K)]`.
  2. **Monochromatic Provenance Badging (Rule 1 Compliant)**:
     - Rendered high-contrast, zero-emoji provenance badges in the snapshot metadata bar above the frame view:
       - Auto-harvested candidate: `[Tự động: Harvester]` (vi) / `[Auto: Harvester]` (en).
       - Human-certified label: `[Người duyệt: <tên>]` (vi) / `[Reviewer: <name>]` (en).
       - Unlabeled frame: `[Chưa gán nhãn]` (vi) / `[Unlabeled]` (en).
  3. **Fast-Approval Hotkey Ergonomics (`Shift + Enter`)**:
     - Extended `handleHotkey` with `HotkeyMeta` support detecting `Shift + Enter` (`action: 'approve'`).
     - Directly promotes candidate parameters to gold standard, stamping `annotator` with current human reviewer nickname, persisting to `/api/archiver/labels`, and advancing the frame queue.
     - In `pending_auto` filter mode, approved frames cleanly transition to `approved_human`, seamlessly advancing to the next pending candidate frame without manual index skipping.
