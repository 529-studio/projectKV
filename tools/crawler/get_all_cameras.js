const fs = require('fs');
const path = require('path');
const vm = require('vm');

const BASE_URL = 'https://giaothong.hochiminhcity.gov.vn';

async function getAllCameras() {
  console.log('========================================================');
  console.log(' [1/3] Connecting to HCMC Traffic Portal...');
  console.log('========================================================');

  // 1. Retrieve session cookies from Map.aspx
  const mapRes = await fetch(`${BASE_URL}/Map.aspx`, {
    headers: {
      'User-Agent': 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36',
      'Accept': 'text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8'
    }
  });

  const rawCookies = mapRes.headers.getSetCookie ? mapRes.headers.getSetCookie() : [];
  const cookieStr = rawCookies.map(c => c.split(';')[0]).join('; ');
  console.log(' -> Successfully received session cookie.');

  // 2. Invoke AjaxPro RPC to fetch full camera list
  console.log('\n [2/3] Querying full city camera list...');
  const rpcRes = await fetch(
    `${BASE_URL}/ajaxpro/VDMS.Web.Library.AJAX.FolderAjax,VDMS.Web.Library.ashx`,
    {
      method: 'POST',
      headers: {
        'User-Agent': 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36',
        'X-AjaxPro-Method': 'SearchQuery',
        'Content-Type': 'text/plain; charset=utf-8',
        'Cookie': cookieStr
      },
      body: JSON.stringify({
        path: '/root/vdms/tangthu/data/layerdata/camera',
        isInTree: false,
        searchKey: '', // Empty to fetch all
        layer: ['CAMERA'],
        detail: true,
        page: 1,
        limit: 1000,
        filterQuery: null,
        sortby: null,
        returnFields: null
      })
    }
  );

  const text = await rpcRes.text();

  // Sandbox AjaxPro DataTable structure in isolated vm context (CWE-95 remediation)
  const sandbox = {
    Ajax: {
      Web: {
        DataTable: function (cols, rows) {
          this.cols = cols;
          this.rows = rows;
        }
      }
    }
  };

  const script = new vm.Script('(' + text + ')');
  const evalResult = script.runInNewContext(sandbox, { timeout: 2000 });
  const dataTable = evalResult.value[1][1];
  const rows = dataTable.rows;

  console.log(` -> Total cameras found in system: ${rows.length}`);

  // 3. Normalize camera data
  const cameras = rows.map(r => {
    const id = r[2];
    const title = r[0];
    const displayName = r[13] || title;
    const status = r[10]; // 'UP' or 'NOT_IMAGE'
    const rawLoc = r[4]?.rows?.[0]?.[1] || null;

    let latitude = null;
    let longitude = null;

    if (rawLoc) {
      const match = rawLoc.match(/POINT\(([\d.]+)\s+([\d.]+)\)/i);
      if (match) {
        longitude = parseFloat(match[1]);
        latitude = parseFloat(match[2]);
      }
    }

    return {
      id,
      title,
      displayName,
      status,
      isOnline: status === 'UP',
      latitude,
      longitude,
      snapshotUrl: `${BASE_URL}/render/ImageHandler.ashx?id=${id}`
    };
  });

  // 4. Save to JSON file
  const projectRoot = path.join(__dirname, '../../');
  const outputPath = path.join(projectRoot, 'cameras_all.json');
  fs.writeFileSync(outputPath, JSON.stringify(cameras, null, 2), 'utf8');
  console.log(` -> Saved full list to: ${outputPath}`);

  // Quick statistics
  const onlineCams = cameras.filter(c => c.isOnline);
  const offlineCams = cameras.filter(c => !c.isOnline);

  console.log('\n========================================================');
  console.log(' OVERALL STATISTICS:');
  console.log(` - Total cameras: ${cameras.length}`);
  console.log(` - Active (UP): ${onlineCams.length} cameras`);
  console.log(` - Offline/Maintenance: ${offlineCams.length} cameras`);
  console.log('========================================================');

  // 5. Test downloading 3 sample snapshots from online cameras to verify image quality
  console.log('\n [3/3] Downloading 3 test snapshot images from online cameras...');
  const snapshotDir = path.join(projectRoot, 'snapshots');
  if (!fs.existsSync(snapshotDir)) {
    fs.mkdirSync(snapshotDir, { recursive: true });
  }

  const sampleCams = onlineCams.slice(0, 3);
  for (let i = 0; i < sampleCams.length; i++) {
    const cam = sampleCams[i];
    const now = Date.now();
    const liveUrl = `${cam.snapshotUrl}&t=${now}`;

    try {
      const imgRes = await fetch(liveUrl);
      const buffer = await imgRes.arrayBuffer();
      const filename = `cam_${i + 1}_${cam.id}.jpg`;
      const saveFile = path.join(snapshotDir, filename);

      fs.writeFileSync(saveFile, Buffer.from(buffer));
      console.log(` [✓] Camera ${i + 1}: "${cam.displayName}"`);
      console.log(`     - ID: ${cam.id}`);
      console.log(`     - Coordinates: ${cam.latitude}, ${cam.longitude}`);
      console.log(`     - Image Size: ${(buffer.byteLength / 1024).toFixed(1)} KB`);
      console.log(`     - Saved to: snapshots/${filename}\n`);
    } catch (err) {
      console.error(` [✗] Failed to load snapshot for camera ${cam.id}:`, err.message);
    }
  }

  console.log('========================================================');
  console.log(' CRAWL & VERIFICATION COMPLETED!');
  console.log(` - Data file: ${outputPath}`);
  console.log(` - Sample images dir: ${snapshotDir}`);
  console.log('========================================================');
}

getAllCameras().catch(err => {
  console.error('Execution error:', err);
});
