# Lesson 443: Aggregation Must Bubble Up Danger, and a Basemap Is a Live Dependency

## Context

S14-T3 (`#443`) asked for three things on the commuter map: a high-contrast dark
basemap that survives direct sunlight, marker clustering below zoom 14 that never
hides a severe flood, and physical flood depth on every surface that names a
camera (marker tooltip, pinned-dock tile, mobile camera row). Implementing it
exposed six assumptions that were only true when the issue was written.

## Root causes and what changed

1. **A basemap provider is a live dependency, not a constant.** The issue named
   CARTO Dark Matter and supplied a keyless URL. By implementation time every
   CARTO style (`dark_all`, `dark_nolabels`, `light_all`, `rastertiles/voyager`,
   and the legacy Fastly host) returned HTTP 200 with an "API KEY REQUIRED"
   watermark. The image looks like a dark tile at a glance; only its mean luma
   (40.1) and a visual inspection reveal the placeholder. **Rule:** verify tile
   bytes, not status codes, and record the probe. The proxy now answers with
   server-rendered dark cartography from the keyless OSM upstream
   (`internal/tilecache/darken.go`), CARTO stays a documented opt-in behind
   `MAP_TILE_UPSTREAM_URL` + `MAP_TILE_API_KEY`, and attribution follows the
   provider actually served.
2. **Immutable tile caching outlives the theme.** Tiles are served with
   `Cache-Control: immutable` for 30 days. After switching renderers, the first
   verification run still showed watermarked CARTO tiles from the browser cache
   at the same `/api/tiles/{z}/{x}/{y}.png` URL. The frontend now appends a
   `?v=<theme>` cache key to the tile URL; the proxy ignores it. Bump that token
   whenever the cartography changes.
3. **A provider-agnostic cache directory serves a mixed map.** Old light OSM
   tiles in the same folder as new dark output produce a map where half the
   neighbourhoods are white. The cache directory now carries provider and theme
   (`osm-dark`, `carto-dark_all`), so a switch can never blend.
4. **Cluster aggregation is a safety action, not a performance trick.** A badge
   that only says "62" swallows the one Level 3 flood inside it. The badge now
   bubbles up the worst hazard level: Level 3 red danger ring, Level 2 amber,
   otherwise dark zinc, with the worst hazard in the tooltip and the aria label.
   The flood lens never clusters, a ward/corridor filter never clusters (the
   dimming is the answer the rider asked for), and a camera with an active
   crowdsourced flood report is forced individual so its amber ring is never
   absorbed.
5. **Hazard depth has to be ubiquitous or it is not usable.** The physical
   centimetre figure lived only in the camera modal. It now renders through one
   pure view model (`hazardDisplay.ts`) on the map tooltip, the pinned dock and
   the mobile camera row, including the honest "depth not measurable" state for
   an avoidance-inferred Level 3 and a stale chip instead of a fresh-looking
   reading. `water_ratio` remains forbidden as a depth source.
6. **Touch targets are hit areas, not glyph sizes.** A 20 px unpin `×` on a
   135 px tile is a miss on a moving bus. Dock controls became 48 px buttons
   whose visual glyph stays 20 px, so the layout does not grow but the target
   does. Camera rows, drawer close, search trigger and dock pill all measured
   48 px in a 390 px viewport.

## Secondary traps

- **Tailwind purges dynamic tokens.** The cluster ring and hazard surface class
  strings live in `src/utils/*.ts`; the Tailwind content glob did not include
  `src/utils` or `src/config`, so those classes would have been purged in a
  production build while working in dev. Fixed in `tailwind.config.ts`.
- **An artificial loading floor on an emergency surface is a defect.** The
  `700ms` minimum in `useFlood` delayed the first flood state for no benefit;
  removed.
- **A live SSE tick can overwrite a controlled fixture.** When verifying hazard
  UI against a real backend, the shared camera SSE stream replaces flood
  verdicts; the verification run must pin the stream to isolate the payload
  under test. That is also a reminder that the live feed currently publishes no
  hazard levels, so the hazard surfaces are contract-verified, not
  production-observed (see the verification report).

## Reusable guardrails

- Probe external imagery by decoding it (dimensions, mean luma, visual), never
  by trusting `200 OK`.
- Version the cache key whenever an immutable asset's content contract changes.
- Namespace caches by the inputs that determine their bytes.
- Bubble up severity through every aggregation; force safety-critical items out
  of aggregation instead of hiding them behind a count.
- Expand hit areas without inflating visual chrome.
- Include shared token modules in the CSS scanner, or dynamic styles silently
  disappear in production builds.
