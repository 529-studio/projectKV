# Lesson 476: Commuter Route Polyline & Hazard Layer + Corridor Overview Card

## Navigation Aid & Quick Reference

- **Domain**: Commuter UX, Leaflet multi-segment polylines, WGS84 GeoJSON rendering, floating corridor overview cards, Telegram deep linking.
- **Relevant Issues**: Issue #476 (S15-T2), Issue #475 (S15-T1), Issue #477 (Mobile ergonomics S15-T3), Issue #484 (Address search S15-T7).
- **Key Files**:
  - `apps/web/src/types/corridor.ts` (Corridor route response and GeoJSON schemas)
  - `apps/web/src/hooks/useCorridorRoute.ts` (On-demand route caching with 30s TTL)
  - `apps/web/src/components/CorridorRouteLayer.tsx` (Leaflet canvas polyline with outer glow casing & checkpoints)
  - `apps/web/src/components/CorridorOverviewCard.tsx` (Ergonomic floating card with passability verdict and Telegram CTA)
  - `apps/web/src/components/CorridorRouteAndCard.test.ts` (Empirical automated verification for Rule 10 scenarios)
- **Core Invariants**:
  - **Commuters think in arterial corridors, not individual camera pins.** When planning a commute across the city, riders need an immediate answer to passability: "Can I take Vo Van Kiet right now, where is the flood, and how many minutes of delay?"
  - **Leaflet polylines require high-contrast double-stroke casing for outdoor readability.** A single thin 3px polyline washes out against dark base tiles under direct sunlight. Rendering a 12px outer glow casing with 35% opacity beneath a solid 6px core polyline guarantees clear visibility.
  - **Differentiating passability verdicts prevents vehicle damage.** Passability verdicts (`PASSABLE` in emerald, `CAUTION` in amber, `IMPASSABLE` in pulsing crimson) give clear guidance on low-clearance vehicles and deep water risk ($\ge 20\text{ cm}$).

---

## 1. Context & Problem Statement

Prior to Sprint 15, the web application focused on camera-centric monitoring: users navigated through ward trees, selected individual camera pins, or inspected H3 hexagon heatmaps. While valuable for traffic control operators, daily commuters faced significant friction:
- No visual corridor polyline showing the continuous route from origin to destination.
- No aggregated passability verdict indicating whether an arterial route was submerged or blocked.
- No quick-glance travel delay estimate or peak water depth measurement.
- Disconnected alerting requiring users to search for Telegram bots manually.

This task delivers the commuter corridor visualization in `apps/web`, consuming the Go corridor engine endpoints shipped in PR #482 (`/api/roads/:road_key/route` and `/api/corridors/overview`).

---

## 2. Root Cause Analysis & Architectural Solutions

### 2.1. On-Demand Route Fetching & Caching (`useCorridorRoute`)
To prevent redundant network requests when switching between corridors, `useCorridorRoute` implements an in-memory cache with a 30-second TTL:

```typescript
export async function fetchCorridorRoute(roadKey: string): Promise<CorridorRouteResponse | null> {
  const normKey = roadKey.trim().toLowerCase();
  const cached = getCachedCorridorRoute(normKey);
  if (cached) return cached;

  const url = getApiUrl(API_ENDPOINTS.ROAD_ROUTE(normKey));
  const res = await fetch(url);
  if (!res.ok) {
    if (res.status === 404) return null;
    throw new Error(`Failed to fetch route for ${normKey}: ${res.status}`);
  }
  const data: CorridorRouteResponse = await res.json();
  setCachedCorridorRoute(normKey, data);
  return data;
}
```

### 2.2. Sunlight-Readable Polyline Rendering (`CorridorRouteLayer`)
In `CorridorRouteLayer.tsx`, a dedicated Leaflet pane (`corridorRoutePane`, `zIndex: 560`) prevents polylines from occluding interactive camera markers (`zIndex: 600`).
The polyline renders a two-layer stroke:
1. Outer glow polyline (`weight: 12`, `opacity: 0.35`) in `summary.status_color`.
2. Core solid line (`weight: 6`, `opacity: 0.95`).
3. Point features for cameras along the corridor with dynamic circle markers and water depth indicators.
4. Automatic viewport framing (`map.fitBounds`) smoothly flies the camera to show the complete corridor bounds upon selection.

### 2.3. Floating Ergonomic Overview Card (`CorridorOverviewCard`)
Positioned above the mobile safe area (`bottom: calc(var(--safe-bottom, 0px) + 64px)`), the card provides:
- Corridor title and origin-destination description.
- Passability verdict badge (`THÔNG THOÁNG`, `CẢNH BÁO XE GẦM THẤP`, `NGUY CƠ CHẾT MÁY / THỦY KÍCH`).
- Travel duration vs free-flow standard, with delay in minutes.
- Peak water depth in centimeters.
- Checkpoint camera strip with live snapshot previews and click-to-open interaction.
- 1-click Telegram subscription deep link: `https://t.me/<bot>?start=sub_corridor_<road_key>`.

---

## 3. Verification & Operational Evidence

1. **Automated Unit & Scenario Tests**:
   - `useCorridorRoute.test.ts`: Verifies route fetching, cache hits, 404 handling, and overviews array.
   - `corridorUtils.test.ts`: Validates `toRoadKey` accent stripping and snake_case mapping across all 10 canonical corridors.
   - `CorridorRouteAndCard.test.ts`: Executes 5 Rule 10 manual testing scenarios verifying polyline coordinate parsing, crimson flood styling, checkpoint camera clicks, and Telegram start payloads.
2. **Production Build Clean**:
   - `npm run build` succeeds with zero errors across all 6 static pages.
   - 430/430 unit and contract tests passing.
