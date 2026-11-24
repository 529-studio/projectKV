# Specification: Automated Crowd-Sourced Hazard Report Verification Against Nearby Camera Feeds

- **Issue**: [#527](https://github.com/529-studio/projectKV/issues/527) (Sprint 18 Task 4)
- **Charter**: Cross-reference crowd-sourced citizen hazard reports with nearby CCTV cameras ($\le 300\text{m}$ radius) and astronomical tidal surge levels, calculate Bayesian corroboration confidence, enforce a monotonic report lifecycle state machine (`SUBMITTED` $\rightarrow$ `CORROBORATED` / `UNVERIFIED` $\rightarrow$ `EXPIRED`), and display a subtle camera-verified badge on the commuter UI.
- **Author**: Antigravity & Vu Thanh Dat
- **Date**: 2026-11-24

---

## 1. Problem Statement & Motivation

Warded enables citizens to submit real-time flood observations at camera locations. However, crowdsourced inputs inherently carry risks of:
1. False alarms or stale reports (e.g., reports submitted long after water has drained).
2. Prank submissions or single-reporter noise.
3. Lack of authoritative confirmation for commuters planning critical travel routes.

By automatically checking CCTV cameras within a 300m spatial radius and combining computer vision verdicts, multiple reporter consensus, and tidal elevation into a Bayesian confidence score, the system can automatically promote corroborated reports to a high-confidence **`CORROBORATED`** ("Xác thực qua camera") status.

---

## 2. Mathematical Model & Scoring

### 2.1 Haversine Spatial Distance
Given report coordinate $(\phi_1, \lambda_1)$ and camera coordinate $(\phi_2, \lambda_2)$:
$$\Delta \phi = \phi_2 - \phi_1, \quad \Delta \lambda = \lambda_2 - \lambda_1$$
$$a = \sin^2\left(\frac{\Delta \phi}{2}\right) + \cos(\phi_1)\cos(\phi_2)\sin^2\left(\frac{\Delta \lambda}{2}\right)$$
$$d = 2 R \cdot \arcsin\left(\sqrt{a}\right), \quad R = 6,371,000 \text{ m}$$
Cameras with $d \le 300 \text{ m}$ qualify as spatial corroboration candidates.

### 2.2 Bayesian Corroboration Score
$$C = w_1 \cdot S_{\text{CV}} + w_2 \cdot S_{\text{Reports}} + w_3 \cdot S_{\text{Tide}}$$
Where:
- Weights: $w_1 = 0.50$, $w_2 = 0.30$, $w_3 = 0.20$ ($\sum w_i = 1.0$).
- $S_{\text{CV}}$:
  - `FLOODED_DEEP` ($\ge 20\text{cm}$): $1.0$
  - `FLOODED_SHALLOW` ($10\text{--}20\text{cm}$): $0.8$
  - `WET` ($< 10\text{cm}$): $0.5$
  - `DRY` / `UNKNOWN` / no nearby camera: $0.0$
- $S_{\text{Reports}}$:
  - $\ge 3$ distinct reports: $1.0$
  - $2$ distinct reports: $0.7$
  - $1$ report: $0.4$
  - $0$ reports: $0.0$
- $S_{\text{Tide}}$:
  - $\ge 1.60\text{m}$ (Alert Level 3): $1.0$
  - $1.50\text{m} \le H < 1.60\text{m}$ (Alert Level 2): $0.7$
  - $1.40\text{m} \le H < 1.50\text{m}$ (Alert Level 1): $0.4$
  - $< 1.40\text{m}$ (Normal): $0.1$

### 2.3 Verification Decision Boundary
- **`CORROBORATED`**: $C \ge 0.55$ AND ($S_{\text{CV}} \ge 0.5$ OR ($S_{\text{Reports}} \ge 0.7 \text{ and } S_{\text{Tide}} \ge 0.7$)).
- **`UNVERIFIED`**: Nearby cameras exist and are verified `DRY` ($S_{\text{CV}} == 0.0$), and $C < 0.40$.
- **`SUBMITTED`**: Default initial state while awaiting corroborating signals.
- **`EXPIRED`**: Monotonic terminal state when $T \ge T_{\text{submit}} + 90\text{ minutes}$.

---

## 3. Monotonic State Machine

```
              ┌───────────────────────────┐
              │         SUBMITTED         │
              └──────┬─────────────┬──────┘
                     │             │
        (C >= 0.55   │             │  (Nearby camera DRY
         & wet cam)  │             │   & C < 0.40)
                     ▼             ▼
          ┌──────────────┐    ┌──────────────┐
          │ CORROBORATED │    │  UNVERIFIED  │
          └──────┬───────┘    └──────┬───────┘
                 │                   │
                 │   (TTL > 90m)     │ (TTL > 90m)
                 └─────────┬─────────┘
                           ▼
                    ┌─────────────┐
                    │   EXPIRED   │ (Terminal)
                    └─────────────┘
```

- Invalid backwards transitions (e.g. `CORROBORATED` $\rightarrow$ `SUBMITTED`, `EXPIRED` $\rightarrow$ `SUBMITTED`) return `ErrInvalidStateTransition`.

---

## 4. Architectural Boundaries (Leaf Modulith)

- Package: `services/go-server/internal/reports/`
- Dependency Inversion (`deps.go`):
  ```go
  type CameraLocator interface {
      FindCamerasWithin(lat, lng float64, radiusMeters float64) []NearbyCamera
      GetCamera(id string) (NearbyCamera, bool)
  }

  type VerdictReader interface {
      GetVerdict(cameraID string) (*hotset.Verdict, bool)
  }

  type TideReader interface {
      PredictTide(station string, t time.Time) float64
  }
  ```
- Composition root (`cmd/server/adapters.go` & `main.go`) wires `camera.Catalog`, `hotset.Store`, and `tide.PredictTide`.

---

## 5. User Interface & Commuter Ergonomics

1. **Map Markers (`MapComponent.tsx`)**:
   - For citizen-reported cameras with `status == CORROBORATED`, display a badge and tooltip:
     `[Xác thực qua camera · 85% tin cậy]` (VI) / `[Camera verified · 85% confidence]` (EN).
   - Monochromatic styling with emerald/cyan accent; zero emoji.
2. **Camera Modal (`CameraModal.tsx`)**:
   - Prominently indicate camera corroboration status and remaining valid minutes.
