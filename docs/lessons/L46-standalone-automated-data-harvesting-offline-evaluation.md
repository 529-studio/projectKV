## Lesson 46: Standalone Automated Data Harvesting, Offline Evaluation Tooling, and Draft-07 Pseudo-Labeling

- **Incident & Tracking:** Issue #263, Kanban **[KV-11]**, Sprint 7 Task S7-T7c (`tooling/s7-t7c-auto-harvest-cli`).
- **Symptom:**
  While the Go backend ran continuous autonomous data harvesting on staging/production (`internal/harvester`), developers, evaluators, and CI pipelines had no way to execute on-demand harvesting, offline batch pseudo-labeling, or schema validation without booting the full Go server stack and database.
- **Root Cause:**
  Coupling of continuous data collection exclusively to the long-running backend server lifecycle. Model evaluation, benchmark curation, and offline chaos testing required a decoupled, standalone Python CLI mirror adhering to identical Draft-07 schema specifications, camera scoping heuristics, and PCU calculation formulas.
- **Mandatory Takeaways & Engineering Resolutions:**
  1. **Decoupled Standalone CLI Architecture (`tools/eval/auto_harvest.py`)**:
     - Built modular Python CLI replicating `internal/harvester` capabilities with zero runtime dependency on the Go server.
     - Supports `--trigger [scheduled|rain|now]`, `--scope [hotset|flood-prone|all]`, `--weather [auto|dry|rain|heavy_rain]`, `--upload-r2`, `--validate`, and `--dry-run`.
  2. **Standard-Library Resilience & Zero Hardcoded Secrets (Rule 8)**:
     - Implemented `load_dotenv()` and HTTP interactions using standard library `urllib.request` and `json`.
     - Integrated `validate_labels.py` native Draft-07 validator so strict schema validation runs 100% cleanly without external package dependencies.
     - Ensured zero credentials in code; R2 access keys and endpoints are dynamically loaded from environment variables with empty-string fallbacks.
   3. **Provenance & Active Learning Bridge (S7-T7d Alignment)**:
      - Tagged auto-generated manifests with `annotator: "harvester/auto"` and structured metadata (`notes: "Auto-harvested via ...; pcu=...; SI=..."`), creating a clean provenance foundation for downstream Web Labeler human-in-the-loop candidate review (S7-T7d / Issue #264).
