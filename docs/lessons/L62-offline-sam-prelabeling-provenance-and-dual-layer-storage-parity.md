## Lesson 62: Offline SAM Pre-Labeling Provenance & Dual-Layer Storage Parity

- **Incident & Tracking:** Sprint 9 Wave 1 / Issue #298 (`feat/s9-t2-sam-prelabel-toolchain`).
- **Symptom:**
  1. Hand-drawing 500+ segmentation masks for YOLOv8-seg is infeasible manually, but running promptable segmentation on the Celeron ingress server (`ingress host`) risks starving the live 30-second camera polling loop.
  2. Pre-labeling from local frame caches originally populated `snapshot_key` with local filesystem paths (`data/proofs/...`), causing broken image links (404) when candidate labels were fetched through the Go server API (`/api/archiver/snapshots/raw`) against Cloudflare R2.
- **Root Cause:**
  1. **Hardware Boundary Inversion:** Running heavyweight ViT image encoders and prompt decoders on the low-power ingress host violates operational isolation boundaries (ADR-011 D3).
  2. **Storage Key vs Local Path Conflation:** Coupling the local file reading path directly to the `snapshot_key` attribute instead of normalizing to the R2 object partition (`<date>/<camera_id>/<file>`) broke dual-layer parity between local mirrors and R2.
- **Mandatory Takeaways & Engineering Resolutions:**
  1. **Strict Host Guardrails:** Implemented an explicit CPU feature guard in `tools/seg/prelabel_sam.py` rejecting non-AVX2 hosts on Linux x86_64 to enforce off-box execution on the `inference worker`, with an explicit `--allow-unsafe-host` override for development workstations.
  2. **Canonical Storage Key Parity:** Decoupled `local_path` from `snapshot_key` in `tools/seg/prelabel_sam.py`, guaranteeing all generated label payloads reference R2-canonical keys regardless of whether frames were streamed from R2 or read from disk.
  3. **Mandatory Human-in-the-Loop Provenance:** Enforced `annotator: "sam_prelabel"` and `source: "sam_prelabel"` on all candidates, surfacing proposals in `/labeler` with the `[Tự động: SAM]` badge without automatic promotion to ground truth.
