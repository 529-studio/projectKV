const path = require('path');
const fs = require('fs');
require('dotenv').config({ path: path.join(__dirname, '../../.env') });
const express = require('express');

const app = express();
const PORT = process.env.PORT || 3000;

function resolvePath(rel) {
  const p1 = path.join(__dirname, '../../', rel);
  if (fs.existsSync(p1)) return p1;
  const p2 = path.join(__dirname, rel);
  if (fs.existsSync(p2)) return p2;
  return path.join(process.cwd(), rel);
}

app.use(express.json());

// Cho phép CORS để Frontend dev server (port 3001) gọi API port 3000 mượt mà
app.use((req, res, next) => {
  res.setHeader('Access-Control-Allow-Origin', '*');
  res.setHeader('Access-Control-Allow-Methods', 'GET, POST, OPTIONS');
  res.setHeader('Access-Control-Allow-Headers', 'Content-Type');
  if (req.method === 'OPTIONS') return res.sendStatus(200);
  next();
});

app.use(express.static(resolvePath('apps/web')));

// 1. Tải danh mục camera vào bộ nhớ
const camerasData = JSON.parse(fs.readFileSync(resolvePath('cameras_all.json'), 'utf8'));

// Map tra cứu nhanh camera theo ID
const camerasMap = {};
camerasData.forEach(c => {
  camerasMap[c.id] = c;
});

// Cache trong bộ nhớ RAM cho ảnh Snapshot (10 giây)
const snapshotCache = new Map(); // id -> { buffer, contentType, timestamp }

// Tự động dọn dẹp cache quá hạn mỗi phút
setInterval(() => {
  const now = Date.now();
  for (const [id, item] of snapshotCache.entries()) {
    if (now - item.timestamp > 15000) snapshotCache.delete(id);
  }
}, 60000);

// ==========================================
// API ENDPOINTS
// ==========================================

// 1. Danh sách toàn bộ 837 camera
app.get('/api/cameras', (req, res) => {
  res.json({
    total: camerasData.length,
    onlineCount: camerasData.filter(c => c.isOnline).length,
    cameras: camerasData
  });
});

// 2. Proxy ảnh snapshot camera (kèm Cache 10s chống nghẽn server cổng GT)
app.get('/api/cameras/snapshot/:id', async (req, res) => {
  const cameraId = req.params.id;
  const now = Date.now();

  // Kiểm tra cache trước
  if (snapshotCache.has(cameraId)) {
    const cached = snapshotCache.get(cameraId);
    if (now - cached.timestamp < 10000) {
      res.setHeader('Content-Type', cached.contentType);
      res.setHeader('X-Cache-Status', 'HIT');
      return res.send(cached.buffer);
    }
  }

  try {
    const url = `https://giaothong.hochiminhcity.gov.vn/render/ImageHandler.ashx?id=${cameraId}&t=${now}`;
    const response = await fetch(url, {
      headers: {
        'User-Agent': 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36'
      }
    });

    if (!response.ok) {
      return res.status(response.status).json({ error: 'Failed to fetch camera snapshot' });
    }

    const contentType = response.headers.get('content-type') || 'image/jpeg';
    const arrayBuffer = await response.arrayBuffer();
    const buffer = Buffer.from(arrayBuffer);

    // Lưu cache 10 giây
    snapshotCache.set(cameraId, {
      buffer,
      contentType,
      timestamp: now
    });

    res.setHeader('Content-Type', contentType);
    res.setHeader('Cache-Control', 'public, max-age=10');
    res.setHeader('X-Cache-Status', 'MISS');
    res.send(buffer);
  } catch (err) {
    console.error(`Lỗi tải snapshot camera ${cameraId}:`, err.message);
    res.status(500).json({ error: err.message });
  }
});

app.listen(PORT, () => {
  console.log('================================================================');
  console.log(` [OK] CAMERA VIEW SERVER ĐANG CHẠY`);
  console.log(` [URL] Truy cập: http://localhost:${PORT}`);
  console.log(` [CAM] Đã nạp ${camerasData.length} camera vào bộ nhớ`);
  console.log('================================================================');
});
