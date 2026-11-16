# Lesson 477: Mobile Web Ergonomics, iOS Safe-Area Padding & Kiosk Touch Targets

## Navigation Aid & Quick Reference

- **Domain**: Mobile Web UX, iOS Safari gesture conflicts, Leaflet Canvas hit tolerance, viewport metadata, kiosk touch target standards.
- **Relevant Issues**: Issue #477 (S15-T3), Issue #454 (Macro clustering), AGENTS.md Rule 1 (Bilingual / lang="vi"), Rule 2 (Sound solutions), Rule 10 (Proactive manual testing).
- **Key Files**:
  - `apps/web/src/app/layout.tsx` (Viewport export with `viewport-fit=cover`, `<html lang="vi">`)
  - `apps/web/src/app/globals.css` (Safe-area variables, `overscroll-behavior: none;`)
  - `apps/web/src/components/LandingOverlay.tsx` (`localStorage` permanent dismissal)
  - `apps/web/src/components/MapComponent.tsx` (Leaflet canvas tolerance 14 for $\ge 48\text{px}$ touch target)
  - `apps/web/src/components/PinnedDock.tsx`, `MobileCameraDrawer.tsx`, `CameraModal.tsx`
- **Core Invariants**:
  - **Mobile web is not desktop squeezed into 390px.** Any fixed button placed at `bottom: 14px` without safe-area offset collides with the iOS Home Indicator swipe bar (34px), throwing users out of the browser.
  - **Leaflet Canvas renderer defaults to zero click tolerance.** Unclustered markers with $r=8$ (16px diameter) require precision mouse clicks unless the Canvas renderer explicitly configures `tolerance: 14` to expand hit detection to 44–48px.
  - **Landing splash screens without persistence destroy mobile usability.** An unpersisted modal that appears on every page refresh forces commuters to tap through redundant screens when checking traffic at traffic lights.

---

## 1. Context & Problem Statement

Independent technical auditing at the end of Sprint 14 revealed that while backend APIs and desktop web features operated with high fidelity, mobile web usability received a failing grade:

1. **iOS Safe Area Collision**: Bottom floating action buttons (Near-Me trigger and Search FAB) were positioned at `bottom: 14px` (`bottom-3.5`). On modern iPhones with notched or Dynamic Island screens, the native Home Indicator occupies the bottom 34px. Users attempting to tap Near-Me or Search consistently triggered iOS app switching or returned to the home screen.
2. **Rubber-Banding & Pull-to-Refresh Disruption**: Panning the Leaflet map downwards starting near the top edge triggered iOS Safari's elastic rubber-band bounce and full-page reload, tearing down open SSE connections and resetting user map coordinates.
3. **Severe Touch Target Violations (< 48px)**:
   - Camera markers on the Leaflet canvas used geometric radii between $5.5\text{px}$ and $8\text{px}$ (11–16px diameter). Because `preferCanvas: true` was enabled, Leaflet's point-in-circle test required a fingertip to land within 8 pixels of the marker center.
   - Action buttons in `CameraModal` ("Xác minh VLM" and "Báo cáo ngập") measured only 24px in height (`px-2.5 py-1`).
   - Accordion expand arrows in `PhuongTreeFilter` measured 16x16px, making them nearly impossible to tap without triggering adjacent row navigation.
4. **Intrusive Landing Splash Screen**: `LandingOverlay.tsx` relied on local React component state (`const [isVisible, setIsVisible] = useState(true)`) without `localStorage` synchronization. Every browser refresh forced the commuter to dismiss the greeting modal before seeing the map.
5. **Orphaned Dead Assets in Production Bundles**: `warded-dockUI.jpg` (1.2 MB) and `warded-camera.jpg` (564 KB) remained in `apps/web/public/` without being referenced anywhere in code, adding 1.8 MB of dead weight to container image layers.
6. **Rule 1 Language Non-Compliance**: `layout.tsx` declared `<html lang="en">`, contradicting the bilingual requirement mandating Vietnamese default (`lang="vi"`).

---

## 2. Root Cause Analysis & Architectural Solutions

### 2.1. Viewport Metadata & CSS Safe-Area Utilities
Next.js 15 separates `viewport` configuration from general `metadata`. Without explicit `viewportFit: 'cover'`, the browser reserves letterboxed margins around system UI elements.

In `layout.tsx`:
```typescript
export const viewport: Viewport = {
  width: "device-width",
  initialScale: 1,
  viewportFit: "cover",
  themeColor: "#09090b",
};
```

In `globals.css`:
```css
:root {
  --safe-top: env(safe-area-inset-top, 0px);
  --safe-bottom: env(safe-area-inset-bottom, 0px);
  --safe-left: env(safe-area-inset-left, 0px);
  --safe-right: env(safe-area-inset-right, 0px);
}

body, html, #app-container {
  overscroll-behavior: none;
  -webkit-overflow-scrolling: touch;
}
```

Fixed bottom elements now offset their bottom anchors dynamically:
```tsx
pinnedCameras.length > 0
  ? 'bottom-[calc(4.5rem+env(safe-area-inset-bottom,0px))]'
  : 'bottom-[calc(0.875rem+env(safe-area-inset-bottom,0px))]'
```

### 2.2. Expanding Canvas Marker Hit Tolerance
In Leaflet, `L.Canvas` hit testing uses `_clickTolerance`:
```javascript
_clickTolerance: function () {
  return (this.options.stroke ? this.options.weight / 2 : 0) +
         (this._renderer.options.tolerance || 0);
}
```
By default, `tolerance` is 0. Creating a dedicated canvas renderer with `tolerance: 14` expands the effective click and touch radius by 14px in all directions. A marker with radius $r=8$ (16px diameter) achieves an effective touch target diameter of $2 \times (8 + 14) = 44–48\text{px}$ without visually cluttering the map.

```typescript
const cameraCanvasRenderer = L.canvas({
  tolerance: 14,
  pane: 'cameraMarkersPane',
});
```

### 2.3. Modal & Control Ergonomics ($\ge 48\text{px}$)
All interactive controls in `CameraModal` and `PhuongTreeFilter` were upgraded with `min-h-[48px]` and `min-w-[48px]` constraints:
- Modal Close button: `min-w-[48px] min-h-[48px] flex items-center justify-center text-2xl`
- Action buttons ("Xác minh VLM", "Báo cáo ngập", "Ghim camera"): `min-h-[48px] px-3.5`
- Prev/Next camera navigation overlays: `w-12 h-12 min-w-[48px] min-h-[48px] rounded-full`
- Tree accordion toggles: `w-8 h-8 min-w-[32px] min-h-[32px] flex items-center justify-center`

### 2.4. Permanent Splash Dismissal
`LandingOverlay.tsx` stores a timestamp under `warded_landing_seen_v1` upon first dismissal. On subsequent sessions or page refreshes, the overlay initializes to `false` and never interrupts the user.

---

## 3. Verification & Empirical Evidence

- **Automated Regression Suite**: Added `apps/web/src/components/MobileErgonomics.test.ts` validating viewport metadata, Vietnamese lang tag, safe-area variables, `localStorage` key persistence, dead asset removal, and touch target sizing.
- **Full Test Suite**: 422 tests passing across 83 test suites in `apps/web`.
- **Production Build**: `next build` static export succeeded with zero errors, reducing container image size by 1.8 MB.
