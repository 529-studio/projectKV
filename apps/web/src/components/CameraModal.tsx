import { useState, useEffect } from 'react';
import { Camera } from '../hooks/useCameras';
import { TrafficVerdict } from '../hooks/useTraffic';
import { API_ENDPOINTS, FUNCTIONAL_COLORS, getApiUrl } from '../config/constants';

interface CameraModalProps {
  camera: Camera | null;
  verdict: TrafficVerdict | undefined;
  onClose: () => void;
  isPinned: boolean;
  onTogglePin: () => void;
}

export default function CameraModal({ camera, verdict, onClose, isPinned, onTogglePin }: CameraModalProps) {
  const [imgError, setImgError] = useState(false);
  const [fetchTimestamp, setFetchTimestamp] = useState<number>(Date.now());
  const [elapsedSeconds, setElapsedSeconds] = useState(0);

  useEffect(() => {
    setImgError(false);
    setFetchTimestamp(Date.now());
    setElapsedSeconds(0);
  }, [camera?.id]);

  useEffect(() => {
    if (!camera?.isOnline || camera?.isGhost) return;
    const interval = setInterval(() => {
      setElapsedSeconds(prev => {
        if (prev >= 15) {
          setFetchTimestamp(Date.now());
          return 0;
        }
        return prev + 1;
      });
    }, 1000);
    return () => clearInterval(interval);
  }, [camera?.id, camera?.isOnline, camera?.isGhost]);

  const handleRefresh = () => {
    setFetchTimestamp(Date.now());
    setElapsedSeconds(0);
  };

  if (!camera) return null;

  const severity = verdict?.severity || 'UNKNOWN';
  let severityLabel = 'BÌNH THƯỜNG';
  let severityColor: string = FUNCTIONAL_COLORS.CLEAR;
  
  if (severity === 'MODERATE') {
    severityLabel = 'ĐÔNG XE';
    severityColor = FUNCTIONAL_COLORS.MODERATE;
  } else if (severity === 'HEAVY' || severity === 'CONGESTED') {
    severityLabel = 'ÙN TẮC';
    severityColor = FUNCTIONAL_COLORS.CONGESTED;
  }

  const isActuallyAvailable = camera.isOnline && !camera.isGhost && !imgError;

  return (
    <div className="fixed inset-0 z-[9999] flex items-center justify-center bg-black/80 backdrop-blur-sm p-4">
      <div className="bg-zinc-950 border border-zinc-700 shadow-2xl rounded-lg w-full max-w-2xl overflow-hidden flex flex-col font-mono text-zinc-300">
        
        {/* HEADER */}
        <div className="px-4 py-3 bg-zinc-900 border-b border-zinc-800 flex justify-between items-start">
          <div>
            <h2 className="text-white font-bold text-lg">{camera.displayName || camera.title}</h2>
            <div className="flex items-center gap-2 mt-1 text-xs text-zinc-400">
              <span>CẬP NHẬT: {new Date(fetchTimestamp).toLocaleTimeString('vi-VN')}</span>
              <span className="text-zinc-600">•</span>
              <span>{elapsedSeconds === 0 ? 'vừa xong' : `${elapsedSeconds}s trước`}</span>
              {isActuallyAvailable && (
                <button
                  onClick={handleRefresh}
                  className="text-zinc-400 hover:text-white underline text-[11px] ml-1"
                  title="Tải lại khung hình mới nhất"
                >
                  [↻ LÀM MỚI]
                </button>
              )}
            </div>
            <div className="flex flex-wrap gap-2 mt-2 text-xs font-bold items-center">
              {camera.isGhost ? (
                <span className="px-2 py-0.5 rounded border bg-amber-950/70 border-amber-800 text-amber-400">
                  [MẤT TÍN HIỆU THỰC ĐỊA]
                </span>
              ) : isActuallyAvailable ? (
                <span className="px-2 py-0.5 rounded border bg-zinc-800 border-zinc-600 text-white">
                  [HOẠT ĐỘNG]
                </span>
              ) : (
                <span className="px-2 py-0.5 rounded border bg-red-950/70 border-red-800 text-red-400">
                  [MẤT KẾT NỐI]
                </span>
              )}

              {camera.duplicateOf && (
                <span className="px-2 py-0.5 rounded border bg-zinc-900 border-zinc-700 text-zinc-400 text-[11px]" title={`Trùng nguồn video với camera ID: ${camera.duplicateOf}`}>
                  [TRÙNG NGUỒN VỚI: {camera.duplicateOf.slice(0, 8)}...]
                </span>
              )}

              {isActuallyAvailable && verdict && (
                <span className="px-2 py-0.5 rounded border" style={{ borderColor: severityColor, color: severityColor, backgroundColor: `${severityColor}20` }}>
                  {severityLabel}
                </span>
              )}
            </div>
          </div>
          <button onClick={onClose} className="text-zinc-500 hover:text-white font-bold text-xl px-2">×</button>
        </div>

        {/* IMAGE WRAPPER */}
        <div className="relative w-full aspect-video bg-black flex items-center justify-center">
          {camera.isGhost ? (
            <div className="text-center p-6 space-y-2">
              <div className="text-amber-400 font-bold text-sm">
                MẤT TÍN HIỆU CAMERA THỰC ĐỊA
              </div>
              <div className="text-zinc-500 text-xs max-w-md">
                Cổng thông tin trả về ảnh giữ chỗ (Image Not Available). Máy chủ đã tự động đánh dấu để tránh tải dữ liệu vô ích.
              </div>
            </div>
          ) : !camera.isOnline ? (
            <div className="text-center p-6 space-y-2">
              <div className="text-zinc-400 font-bold text-sm">
                CAMERA ĐANG BẢO TRÌ HOẶC MẤT KẾT NỐI
              </div>
              <div className="text-zinc-600 text-xs">
                Không thể kết nối đến camera tại vị trí này.
              </div>
            </div>
          ) : imgError ? (
            <div className="text-center p-6 space-y-2">
              <div className="text-red-400 font-bold text-sm">
                KHÔNG THỂ TẢI HÌNH ẢNH SNAPSHOT
              </div>
              <div className="text-zinc-500 text-xs">
                Máy chủ proxy báo lỗi 503 hoặc mất kết nối tới camera.
              </div>
            </div>
          ) : (
            <img 
              key={fetchTimestamp}
              src={`${getApiUrl(API_ENDPOINTS.SNAPSHOT(camera.id))}?t=${fetchTimestamp}`}
              className="w-full h-full object-contain"
              alt="Live Snapshot"
              onError={() => setImgError(true)}
            />
          )}
        </div>

        {/* FOOTER */}
        <div className="px-4 py-3 bg-zinc-900 border-t border-zinc-800 flex justify-between items-center text-xs">
          <div className="flex items-center gap-3 text-zinc-500">
            <span>
              TỌA ĐỘ: {camera.latitude?.toFixed(5)}, {camera.longitude?.toFixed(5)}
            </span>
            {isActuallyAvailable && (
              <span className="hidden sm:inline text-zinc-500">
                • TỰ ĐỘNG LÀM MỚI: {15 - elapsedSeconds}s
              </span>
            )}
          </div>
          <button 
            onClick={onTogglePin}
            className={`px-3 py-1.5 border rounded transition ${isPinned ? 'bg-zinc-200 text-black border-white hover:bg-white' : 'bg-zinc-800 hover:bg-zinc-700 border-zinc-600 text-white'}`}
          >
            {isPinned ? '[- BỎ GHIM]' : '[+ GHIM THEO DÕI]'}
          </button>
        </div>

      </div>
    </div>
  );
}
