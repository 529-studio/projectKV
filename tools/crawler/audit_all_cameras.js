const fs = require('fs');
const path = require('path');

const CONCURRENCY = 15;
const TIMEOUT_MS = 5000;
const RETRIES = 2;

const PNG_MAGIC = Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]);
const GHOST_SIZE = 2638;

function isGhostImage(buffer) {
  if (!buffer || buffer.length === 0) return true;
  if (buffer.length === GHOST_SIZE) return true;
  if (buffer.length >= 8 && buffer.subarray(0, 8).equals(PNG_MAGIC)) return true;
  return false;
}

async function probeCamera(camId) {
  for (let attempt = 1; attempt <= RETRIES + 1; attempt++) {
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), TIMEOUT_MS);
    const start = Date.now();
    const url = `https://giaothong.hochiminhcity.gov.vn/render/ImageHandler.ashx?id=${camId}&t=${Date.now()}`;

    try {
      const res = await fetch(url, {
        signal: controller.signal,
        headers: {
          'User-Agent': 'Mozilla/5.0 (compatible; CameraViewProxy/1.0)',
          'Referer': 'https://giaothong.hochiminhcity.gov.vn/',
          'Accept': 'image/jpeg,image/*,*/*;q=0.8'
        }
      });
      clearTimeout(timer);

      if (!res.ok) {
        if (attempt <= RETRIES && res.status >= 500) {
          await new Promise(r => setTimeout(r, 300 * attempt));
          continue;
        }
        return {
          status: `HTTP_${res.status}`,
          durationMs: Date.now() - start,
          error: `HTTP ${res.status} ${res.statusText}`
        };
      }

      const arrayBuf = await res.arrayBuffer();
      const buffer = Buffer.from(arrayBuf);
      const durationMs = Date.now() - start;

      if (buffer.length === 0) {
        return { status: 'EMPTY_BODY', durationMs, size: 0 };
      }

      if (isGhostImage(buffer)) {
        return { status: 'GHOST_PLACEHOLDER', durationMs, size: buffer.length };
      }

      const ct = res.headers.get('content-type') || '';
      return { status: 'SUCCESS', durationMs, size: buffer.length, contentType: ct };
    } catch (err) {
      clearTimeout(timer);
      const isTimeout = err.name === 'AbortError' || err.message.includes('timeout') || err.message.includes('aborted');
      if (attempt <= RETRIES) {
        await new Promise(r => setTimeout(r, 300));
        continue;
      }
      return {
        status: isTimeout ? 'TIMEOUT' : 'NETWORK_ERROR',
        durationMs: Date.now() - start,
        error: err.message
      };
    }
  }
}

async function main() {
  const projectRoot = path.join(__dirname, '../../');
  const catalogPath = path.join(projectRoot, 'cameras_enriched.json');
  const cameras = JSON.parse(fs.readFileSync(catalogPath, 'utf8'));

  const total = cameras.length;
  console.log(`Starting camera audit for ${total} cameras (Concurrency: ${CONCURRENCY}, Timeout: ${TIMEOUT_MS}ms, Retries: ${RETRIES})...`);

  const results = [];
  let currentIndex = 0;
  let completed = 0;
  const startTime = Date.now();

  const stats = {
    SUCCESS: 0,
    TIMEOUT: 0,
    NETWORK_ERROR: 0,
    GHOST_PLACEHOLDER: 0,
    EMPTY_BODY: 0,
    HTTP_ERROR: 0
  };

  async function worker() {
    while (true) {
      const idx = currentIndex++;
      if (idx >= total) break;

      const cam = cameras[idx];
      const probeRes = await probeCamera(cam.id);

      const record = {
        id: cam.id,
        displayName: cam.displayName || cam.title,
        title: cam.title,
        catalogIsOnline: cam.isOnline,
        catalogStatus: cam.status,
        latitude: cam.latitude,
        longitude: cam.longitude,
        road_name: cam.road_name,
        road_key: cam.road_key,
        ...probeRes
      };

      results.push(record);

      if (stats[probeRes.status] !== undefined) {
        stats[probeRes.status]++;
      } else if (probeRes.status.startsWith('HTTP_')) {
        stats.HTTP_ERROR++;
      } else {
        stats[probeRes.status] = 1;
      }

      completed++;
      if (completed % 25 === 0 || completed === total) {
        const elapsedSec = ((Date.now() - startTime) / 1000).toFixed(1);
        process.stdout.write(
          `\rProgress: [${String(completed).padStart(3, ' ')}/${total}] | Success: ${stats.SUCCESS} | Timeout: ${stats.TIMEOUT} | Ghost: ${stats.GHOST_PLACEHOLDER} | HTTP Err: ${stats.HTTP_ERROR} | Elapsed: ${elapsedSec}s`
        );
      }
    }
  }

  const workers = Array.from({ length: CONCURRENCY }, () => worker());
  await Promise.all(workers);

  console.log('\n\n--- AUDIT SUMMARY ---');
  console.log(`Total cameras scanned: ${total}`);
  console.log(`Duration: ${((Date.now() - startTime) / 1000).toFixed(1)}s`);
  console.log(JSON.stringify(stats, null, 2));

  // Cameras marked isOnline=true in the catalog that are actually failing
  // upstream, whether via a placeholder frame (GHOST_PLACEHOLDER), an empty
  // body, a timeout, a network error, or an HTTP error response.
  const FAILING_STATUSES = new Set(['GHOST_PLACEHOLDER', 'EMPTY_BODY', 'TIMEOUT', 'NETWORK_ERROR']);
  const isFailingStatus = (status) => FAILING_STATUSES.has(status) || status.startsWith('HTTP_');

  const brokenOnlineCams = results.filter(
    r => r.catalogIsOnline && isFailingStatus(r.status)
  );
  // Cameras marked isOnline=false in the catalog that are now serving live frames.
  const recoveredCams = results.filter(
    r => !r.catalogIsOnline && r.status === 'SUCCESS'
  );
  console.log(`\nCameras marked isOnline: true in catalog but FAILING upstream: ${brokenOnlineCams.length}`);
  console.log(`Cameras marked isOnline: false in catalog but RECOVERED upstream: ${recoveredCams.length}`);

  const outReportPath = path.join(projectRoot, 'tools/crawler/audit_results.json');
  fs.writeFileSync(
    outReportPath,
    JSON.stringify(
      {
        scannedAt: new Date().toISOString(),
        total,
        stats,
        brokenOnlineCamerasCount: brokenOnlineCams.length,
        brokenOnlineCameras: brokenOnlineCams,
        recoveredCamerasCount: recoveredCams.length,
        recoveredCameras: recoveredCams,
        allResults: results
      },
      null,
      2
    )
  );
  console.log(`Detailed audit report written to: ${outReportPath}`);
}

main().catch(err => {
  console.error('Audit failed:', err);
  process.exit(1);
});
