# Lesson 89: Unified SSE Stream Provider, Leaflet Canvas Rendering, and Page Visibility Throttling

## Navigation Aid & Quick Reference
- **Domain**: Frontend network architecture, SSE multiplexing, Leaflet Canvas rendering optimization, Page Visibility lifecycle management.
- **Relevant Issues**: Issue #423 (S12-T3), Issue #64, Issue #106, Issue #251, ADR-003, Lesson L85.
- **Key Files**:
  - `apps/web/src/contexts/LiveStreamContext.ts` (Singleton LiveStreamProvider, single EventSource connection, single JSON deserialization, typed hooks)
  - `apps/web/src/hooks/useTraffic.ts` (Consumes shared live stream; `appEnvRef` decoupling eliminates initial render reconnect loop)
  - `apps/web/src/hooks/useFlood.ts` (Consumes shared live stream; decoupled mock filter ref)
  - `apps/web/src/components/MapComponent.tsx` (`preferCanvas: true` to consolidate 837 circle markers into an HTML5 Canvas)
  - `apps/web/src/hooks/useHotsetStatus.ts` (Page Visibility API integration: pauses 30s interval when hidden, refreshes immediately on focus)
  - `apps/web/src/hooks/useWeather.ts` (Page Visibility API integration: pauses 5m interval when hidden, refreshes immediately on focus)
  - `apps/web/src/app/layout.tsx` (Root client context provider boundary)
- **Core Invariants**:
  - **Single SSE Connection**: Exactly one EventSource connection is established to `/api/live/stream` per browser tab, preserving HTTP/1.1 socket budget.
  - **Zero Duplicate Deserialization**: Incoming SSE JSON payloads are deserialized once in `LiveStreamProvider` and dispatched as parsed objects to all subscribers.
  - **Stable Effect Boundaries**: Environment state (`appEnv`) is mirrored to `useRef`, decoupling it from effect dependency arrays and eliminating the hydration reconnect loop.
  - **Single Canvas per Pane**: Leaflet `preferCanvas: true` draws 837 camera circle markers onto a single hardware-accelerated canvas, eliminating 837 SVG `<path>` DOM mutations during map pans and zooms.
  - **Background Timer Hibernation**: Background polling stops completely when `document.visibilityState === 'hidden'` and triggers a debounced refresh when returning to `visible`.

---

## 1. Context & Problem Statement

As features expanded across Sprint 8 through Sprint 11, multiple frontend network and rendering inefficiencies compounded on `apps/web`:

1. **Dual SSE Connections**:
   Both `useTraffic.ts` and `useFlood.ts` independently established `new EventSource('/api/live/stream')`. In HTTP/1.1 environments (standard for many mobile browsers), each connection consumed one of the 6 allowed domain socket slots. Furthermore, identical `camera_update` events were independently parsed and deserialized in two separate hooks, doubling JSON parsing overhead and doubling connection concurrency on the backend `live.Hub`.
2. **Initial Render Reconnect Loop**:
   In `useTraffic.ts`, `updateVerdict` depended on `appEnv`. When initial traffic status resolved (`fetchInitial()`) and populated `setAppEnv(data.app_env)`, React re-rendered the hook. Because `updateVerdict` was in the `useEffect` dependency array, the effect tore down, closed the active EventSource connection, and immediately opened a second one.
3. **837 SVG DOM Nodes on Map**:
   In `MapComponent.tsx`, Leaflet was initialized without `preferCanvas: true`. Leaflet created 837 separate SVG `<path>` elements inside the DOM overlay pane. Panning, zooming, and corridor filtering forced repeated layout recalculations and DOM mutations across 837 nodes, degrading frame rates on mobile devices.
4. **Unthrottled Background Polling**:
   `useHotsetStatus` (30s) and `useWeather` (5m) continued executing periodic background network requests even when the browser tab was minimized or hidden.

---

## 2. Core Architectural Decisions

### A. Centralized `LiveStreamProvider` & Multiplexed Subscriptions
`apps/web/src/contexts/LiveStreamContext.ts` provides a single, resilient EventSource connection to `/api/live/stream`:
- When an event (`camera_update`, `hex_update`, `flood_hex_update`, `user_flood_report`) arrives, `parseSSEPayload` executes once.
- Subscribed listeners are invoked with the parsed object. If an unhandled exception occurs in a subscriber, other subscribers remain unaffected.
- Exposes typed subscription hooks (`useCameraUpdate`, `useHexUpdate`, `useFloodHexUpdate`, `useUserFloodReport`) that store subscriber callbacks in refs, preventing re-subscription thrashing across renders.
- Provides fallback context (`connected: false`, no-op `subscribe`) when rendered outside the provider, ensuring unit tests and headless environments never crash.

### B. Elimination of Hydration Reconnect Loops via Ref Mirrors
In `useTraffic.ts` and `useFlood.ts`, `appEnv` state is mirrored into `appEnvRef.current`. The `updateVerdict` callback reads `appEnvRef.current` directly, allowing `updateVerdict` to retain an empty dependency array (`[]`). Consequently:
- Initial status fetching (`fetchInitial()`) updates `appEnvRef.current = data.app_env`.
- No effect teardown occurs.
- The EventSource connection remains uninterrupted through initial hydration and environment transitions.

### C. GPU-Accelerated Leaflet Canvas Mode
In `MapComponent.tsx` and `constants.ts`:
- `MAP_CONFIG.PREFER_CANVAS` is set to `true`.
- `L.map` is configured with `preferCanvas: true`.
- Leaflet switches its vector renderer from SVG to an HTML5 `<canvas>` element inside `cameraMarkersPane`.
- All 837 circle markers are batched and rendered using 2D canvas drawing contexts, eliminating 837 DOM elements and preventing layout reflows during zoom animations.

### D. Page Visibility API Lifecycle Integration
Both `useHotsetStatus` and `useWeather` listen to `document.visibilitychange` and window `focus`:
- When `document.visibilityState === 'hidden'`, the active `setInterval` timer is cleared and polling stops.
- When `document.visibilityState === 'visible'`, an immediate fetch executes and the interval timer restarts.
- Focus events within a 2-second debounce window do not trigger redundant network requests if `visibilitychange` already initiated a fetch.

---

## 3. Verification & Invariants

- **Automated Verification**: 270 frontend unit and integration tests passing (`npm test`), including dedicated scenario validations in `LiveStreamContext.test.ts` and `e2e_scenarios.test.ts`.
- **Production Build Validation**: `next build` static export compiles with zero type errors, verifying full App Router SSR and client boundary safety across `/`, `/labeler`, and `/_not-found`.
- **Empirical Resource Reduction**: Exactly 1 SSE connection per tab (50% reduction in backend SSE connection footprint); zero SVG path node overhead on map; zero background polling while minimized.
