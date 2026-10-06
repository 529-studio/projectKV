#!/usr/bin/env node
/**
 * sync_catalog_from_audit.js
 *
 * Re-synchronizes the static camera catalog (cameras_all.json and
 * cameras_enriched.json) with the latest upstream probe results produced by
 * audit_all_cameras.js (tools/crawler/audit_results.json).
 *
 * Deterministic upstream outcomes flip the catalog status:
 *   - SUCCESS                          -> isOnline: true,  status: "UP"
 *   - GHOST_PLACEHOLDER / EMPTY_BODY   -> isOnline: false, status: "NOT_IMAGE"
 *   - HTTP 4xx                         -> isOnline: false, status: "NOT_IMAGE"
 *
 * Non-deterministic outcomes (TIMEOUT, NETWORK_ERROR, HTTP 5xx) are treated as
 * transient and leave the catalog entry untouched, avoiding churn caused by
 * temporary network faults.
 *
 * Usage:
 *   node tools/crawler/sync_catalog_from_audit.js --write   # apply changes
 *   node tools/crawler/sync_catalog_from_audit.js --check   # exit 1 on drift
 *
 * Exit codes:
 *   0 - success (no drift in --check mode, or write completed)
 *   1 - drift detected in --check mode
 *   2 - fatal error (missing input files, invalid JSON)
 */

const fs = require('fs');
const path = require('path');

const PROJECT_ROOT = path.join(__dirname, '../../');
const AUDIT_PATH = path.join(__dirname, 'audit_results.json');
const CATALOG_PATHS = [
  path.join(PROJECT_ROOT, 'cameras_all.json'),
  path.join(PROJECT_ROOT, 'cameras_enriched.json'),
];

const UP = 'UP';
const NOT_IMAGE = 'NOT_IMAGE';

// Upstream probe outcomes that deterministically mean "no live frame".
const OFFLINE_STATUSES = new Set(['GHOST_PLACEHOLDER', 'EMPTY_BODY']);
// Upstream probe outcomes that deterministically mean "live frame".
const ONLINE_STATUSES = new Set(['SUCCESS']);

function fail(message) {
  console.error(`[sync] ERROR: ${message}`);
  process.exit(2);
}

/**
 * Derives the desired catalog state for a given audit outcome.
 * Returns null when the outcome is transient and the catalog must stay as-is.
 */
function desiredState(auditStatus) {
  if (ONLINE_STATUSES.has(auditStatus)) {
    return { isOnline: true, status: UP };
  }
  if (OFFLINE_STATUSES.has(auditStatus)) {
    return { isOnline: false, status: NOT_IMAGE };
  }
  // Upstream 4xx responses are non-transient (camera removed / invalid id) and
  // are treated as offline, mirroring proxycache.fetchUpstream, which aborts
  // immediately on 4xx. 5xx, timeouts and network errors remain transient.
  if (/^HTTP_4/.test(auditStatus)) {
    return { isOnline: false, status: NOT_IMAGE };
  }
  return null;
}

function loadAudit(auditPath) {
  if (!fs.existsSync(auditPath)) {
    fail(`audit report not found at ${auditPath}; run audit_all_cameras.js first`);
  }
  const parsed = JSON.parse(fs.readFileSync(auditPath, 'utf8'));
  const records = parsed.allResults;
  if (!Array.isArray(records)) {
    fail(`audit report at ${auditPath} is missing the "allResults" array`);
  }
  const byId = new Map();
  for (const record of records) {
    if (record && record.id) {
      byId.set(record.id, record.status);
    }
  }
  return { parsed, byId };
}

function syncCatalog(catalogPath, auditById) {
  if (!fs.existsSync(catalogPath)) {
    fail(`catalog file not found at ${catalogPath}`);
  }
  const cameras = JSON.parse(fs.readFileSync(catalogPath, 'utf8'));
  if (!Array.isArray(cameras)) {
    fail(`catalog file at ${catalogPath} must contain a JSON array`);
  }

  const flippedToOffline = [];
  const flippedToOnline = [];
  const unchanged = [];
  const transient = [];
  const missingFromAudit = [];

  for (const cam of cameras) {
    const auditStatus = auditById.get(cam.id);
    if (auditStatus === undefined) {
      missingFromAudit.push(cam.id);
      continue;
    }

    const desired = desiredState(auditStatus);
    if (desired === null) {
      transient.push({ id: cam.id, status: auditStatus, catalogIsOnline: cam.isOnline });
      continue;
    }

    const wasOnline = Boolean(cam.isOnline);
    if (wasOnline === desired.isOnline && cam.status === desired.status) {
      unchanged.push(cam.id);
      continue;
    }

    cam.isOnline = desired.isOnline;
    cam.status = desired.status;

    if (desired.isOnline) {
      flippedToOnline.push(cam.id);
    } else {
      flippedToOffline.push(cam.id);
    }
  }

  return {
    cameras,
    summary: { flippedToOffline, flippedToOnline, unchanged, transient, missingFromAudit },
  };
}

function main() {
  const args = process.argv.slice(2);
  const write = args.includes('--write');
  const check = args.includes('--check');

  if (!write && !check) {
    fail('specify either --write (apply changes) or --check (detect drift)');
  }

  const { byId: auditById } = loadAudit(AUDIT_PATH);

  let totalOffline = 0;
  let totalOnline = 0;
  let totalTransient = 0;
  let totalMissing = 0;
  let drift = false;

  for (const catalogPath of CATALOG_PATHS) {
    const result = syncCatalog(catalogPath, auditById);
    const { summary } = result;

    if (summary.flippedToOffline.length > 0 || summary.flippedToOnline.length > 0) {
      drift = true;
    }

    totalOffline += summary.flippedToOffline.length;
    totalOnline += summary.flippedToOnline.length;
    totalTransient += summary.transient.length;
    totalMissing += summary.missingFromAudit.length;

    const relPath = path.relative(PROJECT_ROOT, catalogPath);
    console.log(`[sync] ${relPath}`);
    console.log(`  UP -> NOT_IMAGE : ${summary.flippedToOffline.length}`);
    console.log(`  NOT_IMAGE -> UP : ${summary.flippedToOnline.length}`);
    console.log(`  unchanged       : ${summary.unchanged.length}`);
    console.log(`  transient skip  : ${summary.transient.length}`);
    console.log(`  missing audit    : ${summary.missingFromAudit.length}`);

    if (write && drift) {
      fs.writeFileSync(catalogPath, JSON.stringify(result.cameras, null, 2) + '\n', 'utf8');
      console.log(`  written: ${catalogPath}`);
    }
  }

  console.log('[sync] ---');
  console.log(`[sync] flipped offline: ${totalOffline}, flipped online: ${totalOnline}`,
    `transient skipped: ${totalTransient}, missing from audit: ${totalMissing}`);

  if (check) {
    if (drift) {
      console.log('[sync] catalog is OUT OF SYNC with audit results');
      process.exit(1);
    }
    console.log('[sync] catalog is in sync with audit results');
    process.exit(0);
  }
}

main();
