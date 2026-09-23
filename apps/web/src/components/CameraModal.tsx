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

  return (
    <div className="fixed inset-0 z-[9999] flex items-center justify-center bg-black/80 backdrop-blur-sm p-4">
      <div className="bg-zinc-950 border border-zinc-700 shadow-2xl rounded-lg w-full max-w-2xl overflow-hidden flex flex-col font-mono text-zinc-300">
        
        {/* HEADER */}
        <div className="px-4 py-3 bg-zinc-900 border-b border-zinc-800 flex justify-between items-start">
          <div>
            <h2 className="text-white font-bold text-lg">{camera.displayName || camera.title}</h2>
            <div className="flex gap-2 mt-2 text-xs font-bold">
              <span className={`px-2 py-0.5 rounded border ${camera.isOnline ? 'bg-zinc-800 border-zinc-600 text-white' : 'bg-red-950 border-red-800 text-red-500'}`}>
                [{camera.isOnline ? 'HOẠT ĐỘNG' : 'MẤT TÍN HIỆU'}]
              </span>
              {camera.isOnline && verdict && (
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
          {camera.isOnline ? (
            <img 
              src={`${getApiUrl(API_ENDPOINTS.SNAPSHOT(camera.id))}?t=${Date.now()}`}
              className="w-full h-full object-contain"
              alt="Live Snapshot"
              onError={(e) => { (e.target as HTMLImageElement).src = 'data:image/svg+xml;utf8,<svg xmlns="http://www.w3.org/2000/svg" width="100%" height="100%" fill="none"><rect width="100%" height="100%" fill="%23111"/><text x="50%" y="50%" fill="%23666" font-family="monospace" font-size="14" text-anchor="middle">Lỗi tải ảnh</text></svg>' }}
            />
          ) : (
            <div className="text-zinc-600 font-mono">Camera đang bảo trì hoặc mất kết nối</div>
          )}
        </div>

        {/* FOOTER */}
        <div className="px-4 py-3 bg-zinc-900 border-t border-zinc-800 flex justify-between items-center text-xs">
          <span className="text-zinc-500">
            TỌA ĐỘ: {camera.latitude?.toFixed(5)}, {camera.longitude?.toFixed(5)}
          </span>
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
