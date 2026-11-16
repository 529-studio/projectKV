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
});
