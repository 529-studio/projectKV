import { useState, useEffect } from 'react';
import { Camera } from '../hooks/useCameras';
import { TrafficVerdict } from '../hooks/useTraffic';
import { API_ENDPOINTS, FUNCTIONAL_COLORS, getApiUrl, VERDICT_I18N } from '../config/constants';
import { getFloodSeverityLabel } from '../utils/floodUtils';

interface CameraModalProps {
  camera: Camera | null;
  verdict: TrafficVerdict | undefined;
  onClose: () => void;
  isPinned: boolean;
  onTogglePin: () => void;
  lang?: 'vi' | 'en';
  appEnv?: string;
  analysisEnabled?: boolean;
  pinCapReached?: boolean;
  onPrevCamera?: () => void;
  onNextCamera?: () => void;
  hasPrevCamera?: boolean;
  hasNextCamera?: boolean;
  sequenceLabel?: string;
}

export default function CameraModal({
  camera,
  verdict,
  onClose,
  isPinned,
  onTogglePin,
  lang = 'vi',
  appEnv = 'production',
  analysisEnabled = false,
  pinCapReached = false,
  onPrevCamera,
  onNextCamera,
  hasPrevCamera = false,
  hasNextCamera = false,
  sequenceLabel,
}: CameraModalProps) {
  const [imgError, setImgError] = useState(false);
  const [fetchTimestamp, setFetchTimestamp] = useState<number>(Date.now());
  const [elapsedSeconds, setElapsedSeconds] = useState(0);
  const [osdTime, setOsdTime] = useState<string | null>(null);

  useEffect(() => {
    setImgError(false);
    setFetchTimestamp(Date.now());
    setElapsedSeconds(0);
    setOsdTime(null);
    if (typeof window !== 'undefined') {
      window.getSelection()?.removeAllRanges();
    }
  }, [camera?.id]);

  // Arrow key navigation for previous / next camera in sequence
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'ArrowLeft') {
        if (hasPrevCamera && onPrevCamera) {
          e.preventDefault();
          onPrevCamera();
        }
      } else if (e.key === 'ArrowRight') {
        if (hasNextCamera && onNextCamera) {
          e.preventDefault();
          onNextCamera();
        }
      }
    };
    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [hasPrevCamera, hasNextCamera, onPrevCamera, onNextCamera]);

  useEffect(() => {
    if (!camera?.isOnline || camera?.isGhost) return;
    const interval = setInterval(() => {
      setElapsedSeconds(prev => {
        if (prev >= 15) {
          setImgError(false);
          setFetchTimestamp(Date.now());
          return 0;
        }
        return prev + 1;
      });
    }, 1000);
    return () => clearInterval(interval);
  }, [camera?.id, camera?.isOnline, camera?.isGhost]);

  useEffect(() => {
    if (!camera?.id || !camera.isOnline || camera.isGhost) {
      setOsdTime(null);
      return;
    }
    fetch(getApiUrl(`/api/snapshot/${encodeURIComponent(camera.id)}/osd`))
      .then(res => res.ok ? res.json() : null)
      .then(data => {
        if (data && data.timestamp) {
          setOsdTime(data.timestamp);
        }
      })
      .catch(() => {});
  }, [camera?.id, fetchTimestamp, camera?.isOnline, camera?.isGhost]);

  const handleRefresh = () => {
    setImgError(false);
    setFetchTimestamp(Date.now());
    setElapsedSeconds(0);
  };

  if (!camera) return null;

  const hasRealVerdict = Boolean(verdict && verdict.severity);
  const severity = hasRealVerdict ? verdict!.severity : 'UNKNOWN';
  let severityLabel = lang === 'vi' ? 'BÌNH THƯỜNG' : 'CLEAR';
  let severityColor: string = FUNCTIONAL_COLORS.CLEAR;
  
  if (severity === 'MODERATE') {
    severityLabel = lang === 'vi' ? 'ĐÔNG XE' : 'MODERATE';
    severityColor = FUNCTIONAL_COLORS.MODERATE;
  } else if (severity === 'HEAVY' || severity === 'CONGESTED') {
    severityLabel = lang === 'vi' ? 'ÙN TẮC' : 'CONGESTED';
    severityColor = FUNCTIONAL_COLORS.CONGESTED;
  }

  const isActuallyAvailable = camera.isOnline && !camera.isGhost && !imgError;

  return (
    <div 
      className="fixed inset-0 z-[9999] flex items-center justify-center bg-black/80 backdrop-blur-sm p-3 sm:p-4 select-none"
      onClick={onClose}
    >
      <div 
        className="bg-zinc-950 border border-zinc-700 shadow-2xl rounded-lg w-full max-w-2xl max-h-[92vh] overflow-hidden flex flex-col font-mono text-zinc-300 select-none"
        onClick={(e) => e.stopPropagation()}
      >
        
        {/* HEADER */}
        <div className="px-4 py-3 bg-zinc-900 border-b border-zinc-800 flex justify-between items-start select-none">
          <div className="flex-1 min-w-0 mr-2">
            <div className="flex items-center gap-2 flex-wrap">
              <h2 className="text-white font-bold text-lg truncate">{camera.displayName || camera.title}</h2>
              {sequenceLabel && (
                <span
                  className="px-2 py-0.5 rounded border border-zinc-700 bg-zinc-800 text-zinc-300 text-xs font-bold tracking-wide shrink-0"
                  title={lang === 'vi' ? 'Vị trí trong danh sách camera' : 'Camera sequence position'}
                >
                  [{sequenceLabel}]
                </span>
              )}
            </div>
            <div className="flex flex-wrap items-center gap-2 mt-1 text-xs text-zinc-400">
              {osdTime ? (
                <>
                  <span className="text-emerald-400 font-bold tracking-wide">
                    {lang === 'vi' ? 'THỰC ĐỊA:' : 'LIVE OSD:'} {osdTime}
                  </span>
                  <span className="text-zinc-600">•</span>
                </>
              ) : null}
              {verdict?.observed_at ? (
                <>
                  <span className="text-zinc-400">
                    {lang === 'vi' ? 'QUAN SÁT:' : 'OBSERVED:'}{' '}
                    {new Date(verdict.observed_at).toLocaleTimeString(lang === 'vi' ? 'vi-VN' : 'en-US')}
                  </span>
                  <span className="text-zinc-600">•</span>
                </>
              ) : null}
              <span>
                {lang === 'vi' ? 'TẢI VỀ:' : 'FETCHED:'}{' '}
                {new Date(fetchTimestamp).toLocaleTimeString(lang === 'vi' ? 'vi-VN' : 'en-US')}
              </span>
              <span className="text-zinc-600">•</span>
              <span>
                {elapsedSeconds === 0
                  ? lang === 'vi'
                    ? 'vừa xong'
                    : 'just now'
                  : `${elapsedSeconds}s ${lang === 'vi' ? 'trước' : 'ago'}`}
              </span>
              {(camera.isOnline && !camera.isGhost) && (
                <button
                  onClick={handleRefresh}
                  className="text-zinc-400 hover:text-white underline text-[11px] ml-1"
                  title={lang === 'vi' ? 'Tải lại khung hình mới nhất' : 'Reload latest frame'}
                >
                  [{lang === 'vi' ? '↻ LÀM MỚI' : '↻ REFRESH'}]
                </button>
              )}
            </div>
            <div className="flex flex-wrap gap-2 mt-2 text-xs font-bold items-center">
              {camera.isGhost ? (
                <span className="px-2 py-0.5 rounded border bg-amber-950/70 border-amber-800 text-amber-400">
                  {lang === 'vi' ? '[MẤT TÍN HIỆU THỰC ĐỊA]' : '[NO FIELD SIGNAL]'}
                </span>
              ) : isActuallyAvailable ? (
                <span className="px-2 py-0.5 rounded border bg-zinc-800 border-zinc-600 text-white">
                  {lang === 'vi' ? '[HOẠT ĐỘNG]' : '[ONLINE]'}
                </span>
              ) : (
                <span className="px-2 py-0.5 rounded border bg-red-950/70 border-red-800 text-red-400">
                  {lang === 'vi' ? '[MẤT KẾT NỐI]' : '[OFFLINE]'}
                </span>
              )}

              {camera.duplicateOf && (
                <span
                  className="px-2 py-0.5 rounded border bg-zinc-900 border-zinc-700 text-zinc-400 text-[11px]"
                  title={
                    lang === 'vi'
                      ? `Trùng nguồn video với camera ID: ${camera.duplicateOf}`
                      : `Duplicate video feed with camera ID: ${camera.duplicateOf}`
                  }
                >
                  {lang === 'vi'
                    ? `[TRÙNG NGUỒN VỚI: ${camera.duplicateOf.slice(0, 8)}...]`
                    : `[DUPLICATE OF: ${camera.duplicateOf.slice(0, 8)}...]`}
                </span>
              )}

              {isActuallyAvailable && hasRealVerdict && (
                <>
                  <span className="px-2 py-0.5 rounded border" style={{ borderColor: severityColor, color: severityColor, backgroundColor: `${severityColor}20` }}>
                    {severityLabel}
                  </span>
                  {verdict?.stale && (
                    <span
                      className="px-2 py-0.5 rounded border border-zinc-600 bg-zinc-900 text-zinc-400 text-xs font-bold"
                      title={lang === 'vi' ? 'Dữ liệu phân tích đã cũ (>3 phút hoặc mất cập nhật)' : 'Analysis verdict is stale (>3 min or frame frozen)'}
                    >
                      [{lang === 'vi' ? VERDICT_I18N.STALE_BADGE.vi : VERDICT_I18N.STALE_BADGE.en}]
                    </span>
                  )}
                  {verdict?.source === 'mock' && appEnv === 'dev' && (
                    <span
                      className="px-2 py-0.5 rounded border border-yellow-500/80 bg-yellow-500/10 text-yellow-400 text-xs font-bold tracking-wider"
                      title={lang === 'vi' ? 'Dữ liệu giả lập chạy trong môi trường dev' : 'Simulated verdict in dev mode'}
                    >
                      [{lang === 'vi' ? VERDICT_I18N.DEMO_BADGE.vi : VERDICT_I18N.DEMO_BADGE.en}]
                    </span>
                  )}
                  {verdict?.flood_severity && verdict.flood_severity !== 'DRY' && verdict.flood_severity !== 'UNKNOWN' && (
                    <span
                      className="px-2 py-0.5 rounded border border-sky-500/80 bg-sky-500/20 text-sky-300 text-xs font-bold inline-flex items-center gap-1.5"
                      title={lang === 'vi' ? 'Tình trạng mưa / ngập nước thực địa' : 'Local rain / flood status'}
                    >
                      <svg className="w-3.5 h-3.5 shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
                        <path d="M4 14.899A7 7 0 1 1 15.71 8h1.79a4.5 4.5 0 0 1 2.5 8.242" />
                        <path d="M16 14v6" />
                        <path d="M8 14v6" />
                        <path d="M12 16v6" />
                      </svg>
                      <span>{getFloodSeverityLabel(verdict.flood_severity, lang)}</span>
                    </span>
                  )}
                </>
              )}

              {isActuallyAvailable && !hasRealVerdict && analysisEnabled && (
                <span
                  className="px-2 py-0.5 rounded border border-zinc-700 bg-zinc-900/90 text-zinc-400 text-xs font-mono"
                  title={
                    lang === 'vi'
                      ? 'Camera hoạt động bình thường, đang chờ kết quả phân tích AI'
                      : 'Camera is online, awaiting AI traffic analysis'
                  }
                >
                  [{lang === 'vi' ? 'Chờ phân tích AI' : 'Awaiting AI analysis'}]
                </span>
              )}
            </div>
          </div>
          <button
            onClick={onClose}
            className="text-zinc-500 hover:text-white font-bold text-xl px-2"
            aria-label={lang === 'vi' ? 'Đóng' : 'Close'}
          >
            ×
          </button>
        </div>

        {/* IMAGE WRAPPER */}
        <div className="relative w-full aspect-video bg-black flex items-center justify-center">
          {/* Overlay Navigation Buttons (< and >) */}
          {(hasPrevCamera || hasNextCamera) && (
            <>
              {/* Previous camera button */}
              <button
                type="button"
                onClick={(e) => {
                  e.stopPropagation();
                  onPrevCamera?.();
                }}
                disabled={!hasPrevCamera}
                className={`absolute left-3 top-1/2 -translate-y-1/2 z-20 w-9 h-9 sm:w-10 sm:h-10 rounded-full bg-zinc-950/80 hover:bg-zinc-900 border border-zinc-700/80 text-white flex items-center justify-center shadow-2xl backdrop-blur-md transition-all active:scale-90 ${
                  !hasPrevCamera
                    ? 'opacity-20 pointer-events-none'
                    : 'opacity-85 hover:opacity-100 hover:border-zinc-400 cursor-pointer'
                }`}
                aria-label={lang === 'vi' ? 'Camera trước (Mũi tên trái)' : 'Previous camera (Left arrow)'}
                title={lang === 'vi' ? 'Camera trước (Mũi tên trái)' : 'Previous camera (Left arrow)'}
              >
                <svg className="w-5 h-5 sm:w-6 sm:h-6 text-white" fill="none" stroke="currentColor" strokeWidth="2.5" viewBox="0 0 24 24" aria-hidden="true">
                  <path strokeLinecap="round" strokeLinejoin="round" d="M15 19l-7-7 7-7" />
                </svg>
              </button>

              {/* Next camera button */}
              <button
                type="button"
                onClick={(e) => {
                  e.stopPropagation();
                  onNextCamera?.();
                }}
                disabled={!hasNextCamera}
                className={`absolute right-3 top-1/2 -translate-y-1/2 z-20 w-9 h-9 sm:w-10 sm:h-10 rounded-full bg-zinc-950/80 hover:bg-zinc-900 border border-zinc-700/80 text-white flex items-center justify-center shadow-2xl backdrop-blur-md transition-all active:scale-90 ${
                  !hasNextCamera
                    ? 'opacity-20 pointer-events-none'
                    : 'opacity-85 hover:opacity-100 hover:border-zinc-400 cursor-pointer'
                }`}
                aria-label={lang === 'vi' ? 'Camera kế tiếp (Mũi tên phải)' : 'Next camera (Right arrow)'}
                title={lang === 'vi' ? 'Camera kế tiếp (Mũi tên phải)' : 'Next camera (Right arrow)'}
              >
                <svg className="w-5 h-5 sm:w-6 sm:h-6 text-white" fill="none" stroke="currentColor" strokeWidth="2.5" viewBox="0 0 24 24" aria-hidden="true">
                  <path strokeLinecap="round" strokeLinejoin="round" d="M9 5l7 7-7 7" />
                </svg>
              </button>
            </>
          )}
          {camera.isGhost ? (
            <div className="text-center p-6 space-y-2">
              <div className="text-amber-400 font-bold text-sm">
                {lang === 'vi' ? 'MẤT TÍN HIỆU CAMERA THỰC ĐỊA' : 'FIELD CAMERA SIGNAL LOST'}
              </div>
              <div className="text-zinc-500 text-xs max-w-md">
                {lang === 'vi'
                  ? 'Cổng thông tin trả về ảnh giữ chỗ (Image Not Available). Máy chủ đã tự động đánh dấu để tránh tải dữ liệu vô ích.'
                  : 'Upstream portal returned a placeholder image (Image Not Available). The server has marked this camera to avoid wasteful requests.'}
              </div>
            </div>
          ) : !camera.isOnline ? (
            <div className="text-center p-6 space-y-2">
              <div className="text-zinc-400 font-bold text-sm">
                {lang === 'vi' ? 'CAMERA ĐANG BẢO TRÌ HOẶC MẤT KẾT NỐI' : 'CAMERA MAINTENANCE OR OFFLINE'}
              </div>
              <div className="text-zinc-600 text-xs">
                {lang === 'vi'
                  ? 'Không thể kết nối đến camera tại vị trí này.'
                  : 'Cannot connect to the camera at this location.'}
              </div>
            </div>
          ) : imgError ? (
            <div className="text-center p-6 space-y-3">
              <div className="text-red-400 font-bold text-sm">
                {lang === 'vi' ? 'KHÔNG THỂ TẢI HÌNH ẢNH SNAPSHOT' : 'UNABLE TO LOAD SNAPSHOT IMAGE'}
              </div>
              <div className="text-zinc-500 text-xs max-w-md">
                {lang === 'vi'
                  ? 'Máy chủ proxy báo lỗi 502/503 hoặc mất kết nối tạm thời tới camera.'
                  : 'Proxy server reported 502/503 error or temporary connection timeout to camera.'}
              </div>
              <div>
                <button
                  onClick={handleRefresh}
                  className="px-3 py-1.5 bg-zinc-900 hover:bg-zinc-800 text-zinc-300 hover:text-white border border-zinc-700 hover:border-zinc-500 rounded text-xs font-mono transition-colors"
                >
                  {lang === 'vi' ? '↻ THỬ LẠI KẾT NỐI' : '↻ RETRY CONNECTION'}
                </button>
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
              {lang === 'vi' ? 'TỌA ĐỘ:' : 'COORDINATES:'} {camera.latitude?.toFixed(5)}, {camera.longitude?.toFixed(5)}
            </span>
            {isActuallyAvailable && (
              <span className="hidden sm:inline text-zinc-500">
                • {lang === 'vi' ? 'TỰ ĐỘNG LÀM MỚI:' : 'AUTO REFRESH:'} {15 - elapsedSeconds}s
              </span>
            )}
          </div>
          <button 
            onClick={onTogglePin}
            title={!isPinned && pinCapReached ? (lang === 'vi' ? 'Đã đạt giới hạn ghim (tối đa 12 camera)' : 'Pin limit reached (maximum 12 cameras)') : undefined}
            className={`px-3 py-1.5 border rounded transition ${
              isPinned
                ? 'bg-zinc-200 text-black border-white hover:bg-white'
                : pinCapReached
                ? 'bg-amber-950/80 hover:bg-amber-900 border-amber-700 text-amber-200 cursor-not-allowed'
                : 'bg-zinc-800 hover:bg-zinc-700 border-zinc-600 text-white'
            }`}
          >
            {isPinned
              ? lang === 'vi'
                ? '[- BỎ GHIM]'
                : '[- UNPIN]'
              : pinCapReached
              ? lang === 'vi'
                ? '[12/12 ĐẦY]'
                : '[12/12 FULL]'
              : lang === 'vi'
              ? '[+ GHIM THEO DÕI]'
              : '[+ PIN TO DOCK]'}
          </button>
        </div>

      </div>
    </div>
  );
}
