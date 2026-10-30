## Lesson 30: Uniform Capacity Floors vs. Physical Corridor Geometry & Persistent Capacity Storage across Redeployments

- **Incident & Tracking:** Issue #222, Pull Request for S6-T2 (`feat/s6-t2-corridor-capacity-sqlite`).
- **Symptom:**
  On wide commuter boulevards such as Võ Văn Kiệt - Ký Con (10–12 physical lanes, Camera ID `56de42f611f398ec0c481287`), staging traffic feeds continuously reported false-positive congestion (`HEAVY` / `[ĐÔNG XE]`) even on completely free-flowing off-peak scenes with only 6 cars and 14 motorcycles (~10.2 PCU). Furthermore, whenever backend containers redeployed, in-memory learned peak capacities were wiped out, reverting the entire city to cold-start floors.
- **Root Cause:**
  1. **Uniform Capacity Floor Fallacy**: Applying a single hardcoded `CapacityFloor = 15.0` PCU uniformly across all 837 cameras. While 15.0 PCU fits narrow 2-lane urban streets (a queue of 4–5 cars per lane), a 10–12 lane arterial boulevard comfortably accommodates 40–50 PCU in free-flowing traffic. At a 15.0 PCU capacity baseline, 10.2 PCU yields a Saturation Index of $0.6800 \ge 0.60$, triggering an erroneous `HEAVY` classification.
  2. **Transient State Loss**: Storing learned peak observations exclusively in in-memory Go maps (`PCUCalculator.capacities`), causing historical peaks to vanish on container restarts, redeployments, or node failovers.
- **Mandatory Takeaway & Engineering Resolution:**
  1. **Geometry-Based Corridor Calibration**: Enriched camera catalog (`cameras_enriched.json`) with physical lane-calibrated `base_capacity`:
     - Expressway / major boulevards (10–12 lanes, e.g. Võ Văn Kiệt, Mai Chí Thọ, Phạm Văn Đồng, Nguyễn Văn Linh, Xa Lộ Hà Nội, Võ Nguyên Giáp): 45.0 PCU.
     - National highways (e.g. QL1): 40.0 PCU.
     - Major arterials (4–6 lanes, e.g. Điện Biên Phủ, Cộng Hòa, Trường Chinh, CMT8, NKKN, NTMK, 3/2, Hoàng Văn Thụ, Huỳnh Tấn Phát, QL13, QL22, QL50): 25.0–30.0 PCU.
     - Narrow / residential streets (2 lanes): 15.0 PCU fallback.
  2. **Empirical Operational Resolution**: On Võ Văn Kiệt - Ký Con with 10.2 PCU:
     - Baseline (uniform floor 15.0): $SI = 10.2 / 15.0 = 0.6800 \ge 0.60 \rightarrow$ `HEAVY` (False Positive).
     - Calibrated (corridor base 45.0): $SI = 10.2 / 45.0 = 0.2267 < 0.30 \rightarrow$ `CLEAR` (Accurate Ground Truth).
  3. **Persistent SQLite Peak Storage (WAL Mode & Zero-ORM)**: Created migration `0005_add_camera_capacities.sql` and `store.CapacityRepo` with raw SQL upsert (`ON CONFLICT(camera_id) DO UPDATE SET learned_max = excluded.learned_max, updated_at = CURRENT_TIMESTAMP`), wired to `PCUCalculator` via asynchronous non-blocking persistence. Restores learned peak capacities across container restarts while guaranteeing race-free thread safety under `go test ./... -race`.
