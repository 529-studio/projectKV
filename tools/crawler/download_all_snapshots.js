const fs = require('fs');
const path = require('path');

// Configuration
const CONCURRENCY = 10;
const TIMEOUT_MS = 10000;
const RETRIES = 1;

async function fetchWithRetry(url, retries = RETRIES) {
  for (let attempt = 0; attempt <= retries; attempt++) {
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), TIMEOUT_MS);
    try {
      const res = await fetch(url, {
        signal: controller.signal,
        headers: {
          'User-Agent': 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36',
          'Accept': 'image/avif,image/webp,image/apng,image/svg+xml,image/*,*/*;q=0.8'
        }
      });
      clearTimeout(timer);

      if (!res.ok) {
        throw new Error(`HTTP ${res.status}`);
      }

      const contentType = res.headers.get('content-type') || '';
      if (!contentType.includes('image')) {
        throw new Error(`Unexpected Content-Type: ${contentType}`);
      }

      const arrayBuffer = await res.arrayBuffer();
      const buffer = Buffer.from(arrayBuffer);
      if (buffer.length < 1000) {
        throw new Error(`Image too small (${buffer.length} bytes)`);
      }

      return buffer;
    } catch (err) {
      clearTimeout(timer);
      if (attempt === retries) throw err;
      await new Promise(r => setTimeout(r, 500));
    }
  }
}

async function main() {
  const projectRoot = path.join(__dirname, '../../');
  const catalogPath = path.join(projectRoot, 'cameras_all.json');

  if (!fs.existsSync(catalogPath)) {
    console.error('Error: cameras_all.json not found at', catalogPath);
    process.exit(1);
  }

  const cameras = JSON.parse(fs.readFileSync(catalogPath, 'utf8'));
  const total = cameras.length;

  const now = new Date();
  const timestampStr = now.toISOString().replace(/[:.]/g, '-').slice(0, 19);
  const outDir = path.join(projectRoot, 'snapshots', `crawl_${timestampStr}`);
  fs.mkdirSync(outDir, { recursive: true });

  console.log('========================================================');
  console.log(' CRAWLING FULL HCMC TRAFFIC CAMERA SNAPSHOTS');
  console.log('========================================================');
  console.log(`- Total cameras to fetch: ${total}`);
  console.log(`- Concurrency: ${CONCURRENCY} workers`);
  console.log(`- Output directory: ${outDir}`);
  console.log(`- Start time: ${now.toLocaleString()}`);
  console.log('--------------------------------------------------------');

  const startTime = Date.now();
  let completed = 0;
  let successCount = 0;
  let failedCount = 0;
  let totalBytes = 0;

  const results = new Array(total);
  let currentIndex = 0;

  async function worker(workerId) {
    while (true) {
      const idx = currentIndex++;
      if (idx >= total) break;

      const cam = cameras[idx];
      const fetchTime = Date.now();
      const snapshotUrl = `https://giaothong.hochiminhcity.gov.vn/render/ImageHandler.ashx?id=${cam.id}&t=${fetchTime}`;
      const filename = `${cam.id}.jpg`;
      const filePath = path.join(outDir, filename);

      try {
        const buffer = await fetchWithRetry(snapshotUrl);
        fs.writeFileSync(filePath, buffer);

        totalBytes += buffer.length;
        successCount++;
        results[idx] = {
          id: cam.id,
          title: cam.title,
          displayName: cam.displayName,
          latitude: cam.latitude,
          longitude: cam.longitude,
          status: 'SUCCESS',
          filename: filename,
          fileSizeBytes: buffer.length,
          fetchedAt: fetchTime
        };
      } catch (err) {
        failedCount++;
        results[idx] = {
          id: cam.id,
          title: cam.title,
          displayName: cam.displayName,
          latitude: cam.latitude,
          longitude: cam.longitude,
          status: 'FAILED',
          error: err.message,
          fetchedAt: fetchTime
        };
      } finally {
        completed++;
        if (completed % 50 === 0 || completed === total) {
          const pct = ((completed / total) * 100).toFixed(1);
          const elapsedSec = ((Date.now() - startTime) / 1000).toFixed(1);
          console.log(`[${String(completed).padStart(3, ' ')}/${total}] (${pct}%) | Success: ${successCount}, Failed: ${failedCount} | ${elapsedSec}s elapsed`);
        }
      }
    }
  }

  // Launch workers pool
  const workers = Array.from({ length: CONCURRENCY }, (_, i) => worker(i + 1));
  await Promise.all(workers);

  const durationSec = ((Date.now() - startTime) / 1000).toFixed(2);
  const totalMB = (totalBytes / (1024 * 1024)).toFixed(2);

  // Write manifest
  const manifest = {
    summary: {
      crawlStartedAt: now.toISOString(),
      durationSeconds: parseFloat(durationSec),
      totalCameras: total,
      successfulDownloads: successCount,
      failedDownloads: failedCount,
      totalSizeMB: parseFloat(totalMB),
      outputDirectory: outDir
    },
    cameras: results
  };

  const manifestPath = path.join(outDir, 'manifest.json');
  fs.writeFileSync(manifestPath, JSON.stringify(manifest, null, 2), 'utf8');

  console.log('\n========================================================');
  console.log(' CRAWL COMPLETED!');
  console.log('========================================================');
  console.log(`- Time taken: ${durationSec} seconds`);
  console.log(`- Total downloaded: ${successCount} / ${total} cameras`);
  console.log(`- Failed: ${failedCount} cameras`);
  console.log(`- Total size: ${totalMB} MB`);
  console.log(`- Saved directory: ${outDir}`);
  console.log(`- Manifest: ${manifestPath}`);
  console.log('========================================================\n');
}

main().catch(err => {
  console.error('Fatal error during crawl:', err);
  process.exit(1);
});
