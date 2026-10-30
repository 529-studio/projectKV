## Lesson 12: Machine Learning Model Classes vs. Domain Business Formulas (The Container Class Gap)

- **Incident & Tracking:** Issue #186, ADR-005 Finding D (`feat/186-resolve-container-class-gap`).
- **Symptom:**
  In `services/go-server/internal/cvclient/pcu.go`, the Passenger Car Unit (PCU) Saturation Index formula defined `Container = 3.0`, distinct from `Truck = 2.0`. However, in production, `services/cv-engine/server.py` permanently returned `container: 0`, meaning the `Container = 3.0` weight was dead code that never influenced traffic congestion scores.
- **Root Cause:**
  Discrepancy between early business domain formulas (specified in ADR-004 before real models were deployed) and the actual capabilities of the chosen inference model. The project adopted stock COCO-pretrained YOLOv8n, whose 80 standard classes include only `car`, `motorcycle`, `bus`, and `truck`. In COCO ground truth, heavy commercial freight vehicles (including container trucks) are labeled under `truck`.
- **Mandatory Takeaway:**
  1. **Model-to-Domain Synchronization**: Business logic and scoring formulas must be strictly synchronized with the output schema of the active machine learning model. Never leave speculative weights or dead code branches in production code.
  2. **Documented Architectural Decision Before Implementation**: Rather than making silent ad-hoc code changes, evaluate tradeoffs explicitly (Option a: fold into parent class; Option b: heuristics; Option c: multi-model complexity) and record the decision in `docs/decision-container-class-gap.md`.
  3. **Domain Justification**: Folding container trucks into `Truck = 2.0` aligns with both COCO labeling practices and HCMC daytime peak-hour heavy freight regulations, eliminating dead code while maintaining API backward compatibility.
