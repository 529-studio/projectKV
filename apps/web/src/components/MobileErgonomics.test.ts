import assert from 'node:assert/strict';
import { test, describe } from 'node:test';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { CLUSTER_MIN_DIAMETER_PX } from '../utils/mapClustering.ts';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const webRoot = path.resolve(__dirname, '../..');

describe('S15-T3: Mobile Ergonomics, Safe Area & Touch Target Verification', () => {
  test('Scenario 1: Root layout enforces Vietnamese lang and viewport-fit=cover', () => {
    const layoutPath = path.join(webRoot, 'src/app/layout.tsx');
    const layoutContent = fs.readFileSync(layoutPath, 'utf8');

    // Rule 1 compliance
    assert.match(layoutContent, /<html\s+lang="vi">/, 'Layout must specify lang="vi" as default per Rule 1');
    assert.match(layoutContent, /viewportFit:\s*["']cover["']/, 'Layout must export viewport with viewportFit: cover');
    assert.match(layoutContent, /export\s+const\s+viewport:\s*Viewport/, 'Layout must explicitly export Viewport object');
  });

  test('Scenario 2: Global CSS defines safe-area insets and overscroll-behavior: none', () => {
    const cssPath = path.join(webRoot, 'src/app/globals.css');
    const cssContent = fs.readFileSync(cssPath, 'utf8');

    assert.match(cssContent, /--safe-bottom:\s*env\(safe-area-inset-bottom,\s*0px\);/, 'Must define --safe-bottom CSS variable');
    assert.match(cssContent, /--safe-top:\s*env\(safe-area-inset-top,\s*0px\);/, 'Must define --safe-top CSS variable');
    assert.match(cssContent, /overscroll-behavior:\s*none;/, 'Must prevent rubber-banding and pull-to-refresh reload');
    assert.match(cssContent, /-webkit-overflow-scrolling:\s*touch;/, 'Must enable smooth iOS momentum scrolling');
  });

  test('Scenario 3: Landing overlay implements permanent dismissal persistence via localStorage', () => {
    const overlayPath = path.join(webRoot, 'src/components/LandingOverlay.tsx');
    const overlayContent = fs.readFileSync(overlayPath, 'utf8');

    assert.match(overlayContent, /warded_landing_seen_v1/, 'Must check and store warded_landing_seen_v1 in localStorage');
    assert.match(overlayContent, /min-h-\[48px\]/, 'Landing CTA button must meet 48px touch target standard');
  });

  test('Scenario 4: Deleted orphaned heavy assets from public directory (1.8 MB reduction)', () => {
    const dockUIPath = path.join(webRoot, 'public/warded-dockUI.jpg');
    const cameraPath = path.join(webRoot, 'public/warded-camera.jpg');

    assert.equal(fs.existsSync(dockUIPath), false, 'warded-dockUI.jpg must be deleted');
    assert.equal(fs.existsSync(cameraPath), false, 'warded-camera.jpg must be deleted');
  });

  test('Scenario 5: Mobile touch target sizing adheres to kiosk minimum 48px', () => {
    assert.ok(CLUSTER_MIN_DIAMETER_PX >= 48, 'Map cluster diameter floor must be at least 48px');

    const modalPath = path.join(webRoot, 'src/components/CameraModal.tsx');
    const modalContent = fs.readFileSync(modalPath, 'utf8');

    // Verify modal action buttons and close button have min-h-[48px]
    assert.match(modalContent, /handleVerifyFlood[\s\S]*?min-h-\[48px\]/, 'VLM verify button must have min-h-[48px]');
    assert.match(modalContent, /handleReportFlood[\s\S]*?min-h-\[48px\]/, 'Report flood button must have min-h-[48px]');
    assert.match(modalContent, /min-w-\[48px\][\s\S]*?min-h-\[48px\][\s\S]*?onClose|onClose[\s\S]*?min-w-\[48px\][\s\S]*?min-h-\[48px\]/, 'Close button must have min 48px bounding box');
  });

  test('Scenario 6 (Issue #514): MyRouteCard and MyRouteSetupModal enforce kiosk 48px touch targets and safe area', () => {
    const cardPath = path.join(webRoot, 'src/components/MyRouteCard.tsx');
    const cardContent = fs.readFileSync(cardPath, 'utf8');

    // Direction switch button
    assert.match(cardContent, /onToggleDirection[\s\S]*?min-h-\[48px\]\s+min-w-\[48px\]/, 'Direction switch button must meet 48px kiosk standard');
    // Change route (gear) button
    assert.match(cardContent, /onOpenSetup[\s\S]*?min-w-\[48px\]\s+min-h-\[48px\]/, 'Route setup button must meet 48px kiosk standard');
    // Collapse and Close buttons
    assert.match(cardContent, /setIsCollapsed[\s\S]*?min-w-\[48px\]\s+min-h-\[48px\]/, 'Card collapse toggle must meet 48px kiosk standard');
    assert.match(cardContent, /onClose[\s\S]*?min-w-\[48px\]\s+min-h-\[48px\]/, 'Card close button must meet 48px kiosk standard');
    // Detour button
    assert.match(cardContent, /onSelectDetour[\s\S]*?min-h-\[48px\]/, 'Detour button must meet 48px kiosk standard');
    // Telegram CTA
    assert.match(cardContent, /telegramDeepLink[\s\S]*?min-h-\[48px\]/, 'Telegram subscribe CTA link must meet 48px kiosk standard');

    const modalPath = path.join(webRoot, 'src/components/MyRouteSetupModal.tsx');
    const modalContent = fs.readFileSync(modalPath, 'utf8');

    // Safe area on modal container
    assert.match(modalContent, /env\(safe-area-inset-top\)/, 'Modal must respect top safe area');
    assert.match(modalContent, /env\(safe-area-inset-bottom\)/, 'Modal must respect bottom safe area');
    // Modal close button
    assert.match(modalContent, /onClose[\s\S]*?min-w-\[48px\]\s+min-h-\[48px\]/, 'Modal close button must meet 48px kiosk standard');
    // Endpoint inputs
    assert.match(modalContent, /home-endpoint-input[\s\S]*?min-h-\[48px\]/, 'Home endpoint input must have min-h-[48px]');
    assert.match(modalContent, /work-endpoint-input[\s\S]*?min-h-\[48px\]/, 'Work endpoint input must have min-h-[48px]');
    // Corridor selection checkbox rows
    assert.match(modalContent, /min-h-\[44px\][\s\S]*?toggleCorridor/, 'Corridor checkbox row must meet 44px touch target');
    // Footer actions
    assert.match(modalContent, /handleSave[\s\S]*?min-h-\[48px\]\s+min-w-\[48px\]/, 'Save CTA button must meet 48px kiosk standard');
  });

  test('Scenario 7 (Issue #514): HourlyTimelineScrubber enforces 48px hitboxes and touch momentum scrolling', () => {
    const scrubberPath = path.join(webRoot, 'src/components/HourlyTimelineScrubber.tsx');
    const scrubberContent = fs.readFileSync(scrubberPath, 'utf8');

    // Reset button
    assert.match(scrubberContent, /onSelectHour\(0\)[\s\S]*?min-h-\[48px\]\s+min-w-\[48px\]/, 'Scrubber reset button must meet 48px kiosk standard');
    // Expand/collapse button
    assert.match(scrubberContent, /setIsExpanded[\s\S]*?min-w-\[48px\]\s+min-h-\[48px\]/, 'Scrubber expand toggle must meet 48px kiosk standard');
    // Hour tick buttons
    assert.match(scrubberContent, /min-w-\[84px\][\s\S]*?min-h-\[52px\]/, 'Timeline hour buttons must exceed 48px');
    // Smooth touch scrolling & overscroll containment
    assert.match(scrubberContent, /overscroll-x-contain/, 'Scrubber track must contain horizontal overscroll');
    assert.match(scrubberContent, /touch-pan-x/, 'Scrubber track must declare touch-pan-x');
  });

  test('Scenario 8 (Issue #514): Safe-area insets applied to mobile floating controls and bottom stack', () => {
    const pagePath = path.join(webRoot, 'src/app/page.tsx');
    const pageContent = fs.readFileSync(pagePath, 'utf8');

    // Mobile top header safe-area
    assert.match(pageContent, /top-\[calc\(0\.75rem\+env\(safe-area-inset-top,0px\)\)\]/, 'Mobile header must respect safe-area-inset-top');
    assert.match(pageContent, /left-\[calc\(0\.75rem\+env\(safe-area-inset-left,0px\)\)\]/, 'Mobile header must respect safe-area-inset-left');
    assert.match(pageContent, /right-\[calc\(0\.75rem\+env\(safe-area-inset-right,0px\)\)\]/, 'Mobile header must respect safe-area-inset-right');

    // Mobile bottom stack safe-area when dock is empty
    assert.match(pageContent, /pb-\[calc\(0\.5rem\+env\(safe-area-inset-bottom,0px\)\)\]/, 'Bottom stack must pad for safe-area-inset-bottom');

    // Mobile FAB buttons safe-area
    assert.match(pageContent, /right-\[calc\(0\.75rem\+env\(safe-area-inset-right,0px\)\)\]\s+z-\[830\]/, 'Mobile search FAB must respect safe-area-inset-right');
    assert.match(pageContent, /left-\[calc\(0\.75rem\+env\(safe-area-inset-left,0px\)\)\]\s+z-\[840\]/, 'Mobile near-me FAB must respect safe-area-inset-left');
  });

  test('Scenario 9 (Issue #514): VehicleProfileSelector contains narrow viewport overflow', () => {
    const selectorPath = path.join(webRoot, 'src/components/VehicleProfileSelector.tsx');
    const selectorContent = fs.readFileSync(selectorPath, 'utf8');

    // Max width bound on dropdown menu
    assert.match(selectorContent, /max-w-\[calc\(100vw-1\.5rem\)\]/, 'Dropdown menu must prevent horizontal overflow on 360px viewports');
    // Trigger button has 48px
    assert.match(selectorContent, /min-h-\[48px\]/, 'Vehicle selector trigger button must meet 48px kiosk standard');
    // Dropdown options have 48px
    assert.match(selectorContent, /min-h-\[48px\][\s\S]*?onVehicleChange/, 'Vehicle selector options must have min-h-[48px]');
  });
});
