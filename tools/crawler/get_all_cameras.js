const fs = require('fs');
const path = require('path');

const BASE_URL = 'https://giaothong.hochiminhcity.gov.vn';

async function getAllCameras() {
  console.log('========================================================');
  console.log(' [1/3] Đang kết nối tới Cổng thông tin giao thông TP.HCM...');
  console.log('========================================================');

  // 1. Lấy session cookies từ Map.aspx
  const mapRes = await fetch(`${BASE_URL}/Map.aspx`, {
    headers: {
      'User-Agent': 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36',
      'Accept': 'text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8'
    }
  });

  const rawCookies = mapRes.headers.getSetCookie ? mapRes.headers.getSetCookie() : [];
  const cookieStr = rawCookies.map(c => c.split(';')[0]).join('; ');
  console.log(' -> Đã nhận session cookie thành công.');

  // 2. Gọi AjaxPro RPC để lấy toàn bộ danh sách camera
  console.log('\n [2/3] Đang truy vấn danh sách toàn bộ camera thành phố...');
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
        searchKey: '', // Rỗng để lấy tất cả
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

  // Khởi tạo đối tượng giả lập DataTable của AjaxPro
  global.Ajax = {
    Web: {
      DataTable: function (cols, rows) {
        this.cols = cols;
        this.rows = rows;
      }
    }
  };

  const evalResult = eval('(' + text + ')');
  const dataTable = evalResult.value[1][1];
  const rows = dataTable.rows;

  console.log(` -> Tổng số camera tìm thấy trong hệ thống: ${rows.length}`);

  // 3. Chuẩn hóa dữ liệu camera
  const cameras = rows.map(r => {
    const id = r[2];
    const title = r[0];
    const displayName = r[13] || title;
    const status = r[10]; // 'UP' hoặc 'NOT_IMAGE'
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

  // 4. Lưu ra file JSON
  const projectRoot = path.join(__dirname, '../../');
  const outputPath = path.join(projectRoot, 'cameras_all.json');
  fs.writeFileSync(outputPath, JSON.stringify(cameras, null, 2), 'utf8');
  console.log(` -> Đã lưu toàn bộ danh sách vào: ${outputPath}`);

  // Thống kê nhanh
  const onlineCams = cameras.filter(c => c.isOnline);
  const offlineCams = cameras.filter(c => !c.isOnline);

  console.log('\n========================================================');
  console.log(' THỐNG KÊ TỔNG QUAN:');
  console.log(` - Tổng số camera: ${cameras.length}`);
  console.log(` - Đang hoạt động (UP): ${onlineCams.length} camera`);
  console.log(` - Mất tín hiệu/Bảo trì: ${offlineCams.length} camera`);
  console.log('========================================================');

  // 5. Thử nghiệm tải 3 camera ngẫu nhiên đang hoạt động để kiểm tra chất lượng ảnh
  console.log('\n [3/3] Tải thử nghiệm 3 ảnh snapshot mới nhất từ camera đang online...');
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
      console.log(`     - Tọa độ: ${cam.latitude}, ${cam.longitude}`);
      console.log(`     - Dung lượng ảnh: ${(buffer.byteLength / 1024).toFixed(1)} KB`);
      console.log(`     - Đã lưu tại: snapshots/${filename}\n`);
    } catch (err) {
      console.error(` [✗] Lỗi tải ảnh camera ${cam.id}:`, err.message);
    }
  }

  console.log('========================================================');
  console.log(' HOÀN TẤT THỬ NGHIỆM!');
  console.log(` - File dữ liệu: ${outputPath}`);
  console.log(` - Thư mục ảnh mẫu: ${snapshotDir}`);
  console.log('========================================================');
}

getAllCameras().catch(err => {
  console.error('Lỗi khi thực thi:', err);
});
