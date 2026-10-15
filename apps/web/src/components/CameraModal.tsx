import { useState, useEffect, useRef, useCallback } from 'react';
import { Camera } from '../hooks/useCameras';
import { TrafficVerdict } from '../hooks/useTraffic';
import { API_ENDPOINTS, FLOOD_REPORT_I18N, FLOOD_VERIFY_I18N, FUNCTIONAL_COLORS, getApiUrl, VERDICT_I18N } from '../config/constants';
import {
  FLOOD_MODAL_I18N,
  isDryRoadSeverity,
  isWetRoadSeverity,
} from '../utils/floodUtils';
import {
  calculateCooldownRemaining,
  getFloodVerifyButtonText,
  isFloodVerifyDisabled,
  getFloodReportButtonText,
  isFloodReportDisabled,
  getCameraReportCooldown,
  setCameraReportCooldown,
} from '../utils/cameraModalUtils';

// Module-level timestamp tracking so 15s cooldown persists across modal closes and reopens (Issue #223)
let lastFloodVerifyTimestamp = 0;

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
  onGhostDetected?: (id: string) => void;
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
  onGhostDetected,
}: CameraModalProps) {
  const [imgError, setImgError] = useState(false);
  const [fetchTimestamp, setFetchTimestamp] = useState<number>(Date.now());
  const [elapsedSeconds, setElapsedSeconds] = useState(0);
  const [osdTime, setOsdTime] = useState<string | null>(null);
  const [isVerifyingFlood, setIsVerifyingFlood] = useState(false);
  const [cooldownRemaining, setCooldownRemaining] = useState<number>(() =>
    calculateCooldownRemaining(lastFloodVerifyTimestamp)
  );
  const [floodVerifyResult, setFloodVerifyResult] = useState<{
    camera_id: string;
    camera_name: string;
    flood_severity: string;
    confidence: number;
    description?: string;
    source: string;
    verified_at: string;
  } | null>(null);
  const [floodVerifyError, setFloodVerifyError] = useState<string | null>(null);
  const [isReportingFlood, setIsReportingFlood] = useState(false);
  const [reportCooldownRemaining, setReportCooldownRemaining] = useState<number>(() =>
    camera?.id ? getCameraReportCooldown(camera.id) : 0
  );
  const [floodReportFeedback, setFloodReportFeedback] = useState<{ text: string; isError?: boolean } | null>(null);
  const mountTimeRef = useRef<number>(Date.now());
  // Camera id whose ghost probe is currently in flight (prevents duplicate probes).
  const probeInFlightRef = useRef<string | null>(null);

  // Active 1-second interval countdown when cooldown is active
  useEffect(() => {
    if (cooldownRemaining <= 0) return;
    const timer = setInterval(() => {
      setCooldownRemaining(prev => {
        if (prev <= 1) {
          clearInterval(timer);
          return 0;
        }
        return prev - 1;
      });
    }, 1000);
    return () => clearInterval(timer);
  }, [cooldownRemaining]);

  // Active 1-second interval countdown for 30s human report cooldown (Issue #248 / S7-T3)
  useEffect(() => {
    if (reportCooldownRemaining <= 0) return;
    const timer = setInterval(() => {
      setReportCooldownRemaining(prev => {
        if (prev <= 1) {
          clearInterval(timer);
          return 0;
        }
        return prev - 1;
      });
    }, 1000);
    return () => clearInterval(timer);
  }, [reportCooldownRemaining]);

  // Auto-dismiss verification error after 4 seconds to maintain clean UI (Option A)
  useEffect(() => {
    if (!floodVerifyError) return;
    const timer = setTimeout(() => {
      setFloodVerifyError(null);
    }, 4000);
    return () => clearTimeout(timer);
  }, [floodVerifyError]);

  // Auto-dismiss flood report feedback chip after 3 seconds (Issue #248 / S7-T3)
  useEffect(() => {
    if (!floodReportFeedback) return;
    const timer = setTimeout(() => {
      setFloodReportFeedback(null);
    }, 3000);
    return () => clearTimeout(timer);
  }, [floodReportFeedback]);

  useEffect(() => {
    mountTimeRef.current = Date.now();
    setImgError(false);
    setFetchTimestamp(Date.now());
    setElapsedSeconds(0);
    setOsdTime(null);
    setIsVerifyingFlood(false);
    setFloodVerifyResult(null);
    setFloodVerifyError(null);
    setCooldownRemaining(calculateCooldownRemaining(lastFloodVerifyTimestamp));
    setIsReportingFlood(false);
    setFloodReportFeedback(null);
    setReportCooldownRemaining(camera?.id ? getCameraReportCooldown(camera.id) : 0);
    if (typeof window !== 'undefined') {
      window.getSelection()?.removeAllRanges();
    }
  }, [camera?.id]);

  const handleVerifyFlood = useCallback(async () => {
    if (!camera?.id || isVerifyingFlood || cooldownRemaining > 0) return;
    lastFloodVerifyTimestamp = Date.now();
    setCooldownRemaining(15);
    setIsVerifyingFlood(true);
    setFloodVerifyError(null);

    try {
      const url = `${getApiUrl(API_ENDPOINTS.FLOOD_VERIFY)}?camera_id=${encodeURIComponent(camera.id)}`;
      const res = await fetch(url, { method: 'POST' });
      if (!res.ok) {
        if (res.status === 429) {
          setFloodVerifyError(lang === 'vi' ? FLOOD_VERIFY_I18N.RATE_LIMITED.vi : FLOOD_VERIFY_I18N.RATE_LIMITED.en);
        } else {
          setFloodVerifyError(lang === 'vi' ? FLOOD_VERIFY_I18N.ERROR_GENERIC.vi : FLOOD_VERIFY_I18N.ERROR_GENERIC.en);
        }
        res.json().then(errJson => {
          if (errJson?.error && typeof window !== 'undefined') {
            console.warn('[flood-verify] failed:', errJson.error);
          }
        }).catch(() => {});
        return;
      }
      const data = await res.json();
      setFloodVerifyResult(data);
    } catch (err) {
      if (typeof window !== 'undefined') {
        console.warn('[flood-verify] network error:', err);
      }
      setFloodVerifyError(lang === 'vi' ? FLOOD_VERIFY_I18N.ERROR_GENERIC.vi : FLOOD_VERIFY_I18N.ERROR_GENERIC.en);
    } finally {
      setIsVerifyingFlood(false);
    }
  }, [camera?.id, isVerifyingFlood, cooldownRemaining, lang]);

  const handleReportFlood = useCallback(async () => {
    if (!camera?.id || isReportingFlood || reportCooldownRemaining > 0) return;
    setCameraReportCooldown(camera.id, Date.now());
    setReportCooldownRemaining(30);
    setIsReportingFlood(true);
    setFloodReportFeedback(null);

    try {
      const url = getApiUrl(API_ENDPOINTS.REPORT_FLOOD(camera.id));
      const res = await fetch(url, { method: 'POST' });
      if (res.ok || res.status === 200 || res.status === 201) {
        setFloodReportFeedback({
          text: lang === 'vi' ? FLOOD_REPORT_I18N.SUCCESS_CHIP.vi : FLOOD_REPORT_I18N.SUCCESS_CHIP.en,
          isError: false,
        });
      } else if (res.status === 429) {
        setFloodReportFeedback({
          text: lang === 'vi' ? FLOOD_REPORT_I18N.RATE_LIMITED.vi : FLOOD_REPORT_I18N.RATE_LIMITED.en,
          isError: false,
        });
      } else {
        setFloodReportFeedback({
          text: lang === 'vi' ? FLOOD_REPORT_I18N.ERROR_GENERIC.vi : FLOOD_REPORT_I18N.ERROR_GENERIC.en,
          isError: true,
        });
      }
    } catch (err) {
      if (typeof window !== 'undefined') {
        console.warn('[report-flood] network error:', err);
      }
      setFloodReportFeedback({
        text: lang === 'vi' ? FLOOD_REPORT_I18N.ERROR_GENERIC.vi : FLOOD_REPORT_I18N.ERROR_GENERIC.en,
        isError: true,
      });
    } finally {
      setIsReportingFlood(false);
    }
  }, [camera?.id, isReportingFlood, reportCooldownRemaining, lang]);

  const handleBackdropClick = (e: React.MouseEvent) => {
    if (e.target !== e.currentTarget) return;
    // Guard against synthetic ghost click dispatched by mobile browser right after opening
    if (Date.now() - mountTimeRef.current < 350) return;
    onClose();
  };

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

  // When the <img> fails to render, probe the proxy once to distinguish a known
  // offline camera (503 GHOST_CAMERA) from a genuine transient network error.
  // A ghost response is promoted into shared client state so the map, sidebar
  // and dock all reflect the real field signal loss immediately.
  const handleImageError = useCallback(() => {
    const id = camera?.id;
    if (!id || !onGhostDetected) {
      setImgError(true);
      return;
    }
    if (probeInFlightRef.current === id) return;
    probeInFlightRef.current = id;
    fetch(`${getApiUrl(API_ENDPOINTS.SNAPSHOT(id))}?probe=${Date.now()}`)
      .then(async (res) => {
        if (res.status === 503) {
          const body = await res.json().catch(() => null);
          if (body && body.is_ghost) {
            onGhostDetected(id);
            return;
          }
        }
        setImgError(true);
      })
      .catch(() => setImgError(true))
      .finally(() => {
        if (probeInFlightRef.current === id) {
          probeInFlightRef.current = null;
        }
      });
  }, [camera?.id, onGhostDetected]);

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
      onClick={handleBackdropClick}
    >
      <div 
        className="bg-zinc-950 border border-zinc-700 shadow-2xl rounded-lg w-full max-w-2xl max-h-[92vh] overflow-hidden flex flex-col font-mono text-zinc-300 select-none"
        onClick={(e) => e.stopPropagation()}
      >
        
        {/* HEADER */}
        <div className="px-4 py-3 bg-zinc-900 border-b border-zinc-800 flex justify-between items-start select-none">
          <div className="flex-1 min-w-0 mr-2">
            <div className="flex items-center gap-2 flex-wrap">
              <h2 className="text-white font-bold text-base sm:text-lg break-words leading-snug">{camera.displayName || camera.title}</h2>
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
                      className="px-2 py-0.5 rounded border border-zinc-600 bg-zinc-900 text-zinc-400 text-xs font-bold inline-flex items-center gap-1.5"
                      title={lang === 'vi' ? 'Dữ liệu phân tích đã cũ (>3 phút hoặc mất cập nhật)' : 'Analysis verdict is stale (>3 min or frame frozen)'}
                    >
                      <svg
                        className="w-3 h-3 shrink-0 text-zinc-400"
                        viewBox="0 0 24 24"
                        fill="none"
                        stroke="currentColor"
                        strokeWidth="2"
                        strokeLinecap="round"
                        strokeLinejoin="round"
                        aria-hidden="true"
                      >
                        <path d="M5 22h14" />
                        <path d="M5 2h14" />
                        <path d="M17 22v-4.172a2 2 0 0 0-.586-1.414L12 12l-4.414 4.414A2 2 0 0 0 7 17.828V22" />
                        <path d="M7 2v4.172a2 2 0 0 0 .586 1.414L12 12l4.414-4.414A2 2 0 0 0 17 6.172V2" />
                      </svg>
                      <span>{lang === 'vi' ? VERDICT_I18N.STALE_BADGE.vi : VERDICT_I18N.STALE_BADGE.en}</span>
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
                  {verdict?.flood_severity && verdict.flood_severity.toUpperCase().trim() !== 'UNKNOWN' && (
                    <>
                      {isDryRoadSeverity(verdict.flood_severity) && (
                        <span
                          className="px-2 py-0.5 rounded border border-emerald-600/70 bg-emerald-950/20 text-emerald-400 text-xs font-bold inline-flex items-center gap-1.5"
                          title={FLOOD_MODAL_I18N.DRY.title[lang]}
                        >
                          <svg className="w-3.5 h-3.5 shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
                            <circle cx="12" cy="12" r="4" />
                            <path d="M12 2v2" />
                            <path d="M12 20v2" />
                            <path d="m4.93 4.93 1.41 1.41" />
                            <path d="m17.66 17.66 1.41 1.41" />
                            <path d="M2 12h2" />
                            <path d="M20 12h2" />
                            <path d="m6.34 17.66-1.41 1.41" />
                            <path d="m19.07 4.93-1.41 1.41" />
                          </svg>
                          <span>{FLOOD_MODAL_I18N.DRY[lang]}</span>
                        </span>
                      )}
                      {isWetRoadSeverity(verdict.flood_severity) && (
                        <span
                          className="px-2 py-0.5 rounded border border-sky-500/80 bg-sky-950/30 text-sky-300 text-xs font-bold inline-flex items-center gap-1.5"
                          title={FLOOD_MODAL_I18N.WET.title[lang]}
                        >
                          <svg className="w-3.5 h-3.5 shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
                            <path d="M4 14.899A7 7 0 1 1 15.71 8h1.79a4.5 4.5 0 0 1 2.5 8.242" />
                            <path d="M16 14v6" />
                            <path d="M8 14v6" />
                            <path d="M12 16v6" />
                          </svg>
                          <span>{FLOOD_MODAL_I18N.WET[lang]}</span>
                        </span>
                      )}
                      {verdict.flood_severity.toUpperCase().trim() === 'FLOODED_SHALLOW' && (
                        <span
                          className="px-2 py-0.5 rounded border border-amber-500/80 bg-amber-950/60 text-amber-300 text-xs font-bold inline-flex items-center gap-1.5"
                          title={FLOOD_MODAL_I18N.FLOODED_SHALLOW.title[lang]}
                        >
                          <svg className="w-3.5 h-3.5 shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
                            <path d="M2 12c1.5 0 3-1 4.5-1s3 1 4.5 1 3-1 4.5-1 3 1 4.5 1 3-1 4.5-1" />
                            <path d="M2 18c1.5 0 3-1 4.5-1s3 1 4.5 1 3-1 4.5-1 3 1 4.5 1 3-1 4.5-1" />
                          </svg>
                          <span>{FLOOD_MODAL_I18N.FLOODED_SHALLOW[lang]}</span>
                        </span>
                      )}
                      {verdict.flood_severity.toUpperCase().trim() === 'FLOODED_DEEP' && (
                        <span
                          className="px-2 py-0.5 rounded border border-rose-500/80 bg-rose-950/60 text-rose-300 text-xs font-bold inline-flex items-center gap-1.5"
                          title={FLOOD_MODAL_I18N.FLOODED_DEEP.title[lang]}
                        >
                          <svg className="w-3.5 h-3.5 shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
                            <path d="m21.73 18-8-14a2 2 0 0 0-3.48 0l-8 14A2 2 0 0 0 4 21h16a2 2 0 0 0 1.73-3Z" />
                            <line x1="12" y1="9" x2="12" y2="13" />
                            <line x1="12" y1="17" x2="12.01" y2="17" />
                          </svg>
                          <span>{FLOOD_MODAL_I18N.FLOODED_DEEP[lang]}</span>
                        </span>
                      )}
                    </>
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

            {/* ROW 2: ACTION CONTROLS & VLM VERIFICATION (Issue #223 / S6-T3) */}
            {isActuallyAvailable && (
              <div className="flex flex-wrap gap-2 mt-2.5 items-center">
                {/* Tactical Monochromatic Action Button (No brackets, high contrast, SVG reticle) */}
                <button
                  type="button"
                  onClick={handleVerifyFlood}
                  disabled={isFloodVerifyDisabled({ isVerifying: isVerifyingFlood, cooldownRemaining })}
                  className={`px-2.5 py-1 rounded text-xs font-mono font-semibold tracking-wide inline-flex items-center gap-1.5 border transition-all select-none shadow-sm ${
                    isFloodVerifyDisabled({ isVerifying: isVerifyingFlood, cooldownRemaining })
                      ? 'bg-zinc-800 text-zinc-500 border-zinc-700 cursor-not-allowed opacity-70 shadow-none'
                      : 'bg-zinc-100 hover:bg-white text-zinc-950 border-zinc-200 hover:border-white active:scale-[0.98] cursor-pointer'
                  }`}
                  title={
                    cooldownRemaining > 0
                      ? (lang === 'vi'
                          ? `Vui lòng đợi ${cooldownRemaining}s trước khi xác minh lại`
                          : `Please wait ${cooldownRemaining}s before verifying again`)
                      : (lang === 'vi'
                          ? 'Xác minh tình trạng ngập nước thực địa qua mô hình VLM'
                          : 'Verify real-time flood status via VLM model')
                  }
                >
                  {/* Crisp SVG reticle / radar scan glyph ('⌖') */}
                  <svg
                    className={`w-3.5 h-3.5 shrink-0 ${isVerifyingFlood ? 'animate-spin' : ''}`}
                    viewBox="0 0 24 24"
                    fill="none"
                    stroke="currentColor"
                    strokeWidth="2"
                    strokeLinecap="round"
                    strokeLinejoin="round"
                    aria-hidden="true"
                  >
                    <circle cx="12" cy="12" r="7" />
                    <line x1="12" y1="1" x2="12" y2="5" />
                    <line x1="12" y1="19" x2="12" y2="23" />
                    <line x1="1" y1="12" x2="5" y2="12" />
                    <line x1="19" y1="12" x2="23" y2="12" />
                    <circle cx="12" cy="12" r="1.5" />
                  </svg>
                  <span>
                    {getFloodVerifyButtonText(
                      { isVerifying: isVerifyingFlood, cooldownRemaining },
                      lang
                    )}
                  </span>
                </button>

                {/* Secondary Tactile Action Button: Human-in-the-Loop Report Flood (Issue #248 / S7-T3) */}
                <button
                  type="button"
                  onClick={handleReportFlood}
                  disabled={isFloodReportDisabled({ isReporting: isReportingFlood, cooldownRemaining: reportCooldownRemaining })}
                  className={`px-2.5 py-1 rounded text-xs font-mono font-medium tracking-wide inline-flex items-center gap-1.5 border transition-all select-none shadow-sm ${
                    isFloodReportDisabled({ isReporting: isReportingFlood, cooldownRemaining: reportCooldownRemaining })
                      ? 'bg-zinc-900/60 text-zinc-600 border-zinc-800 cursor-not-allowed opacity-60 shadow-none'
                      : 'bg-zinc-900 hover:bg-zinc-800 text-zinc-300 hover:text-white border-zinc-700 hover:border-zinc-500 active:scale-[0.98] cursor-pointer'
                  }`}
                  title={
                    reportCooldownRemaining > 0
                      ? (lang === 'vi'
                          ? `Đã ghi nhận báo cáo. Vui lòng đợi ${reportCooldownRemaining}s trước khi gửi lại`
                          : `Report recorded. Please wait ${reportCooldownRemaining}s before reporting again`)
                      : (lang === 'vi'
                          ? 'Báo cáo thực địa ngập nước tại vị trí camera này'
                          : 'Report local street flooding at this camera location')
                  }
                >
                  <svg
                    className="w-3.5 h-3.5 shrink-0 text-zinc-400"
                    viewBox="0 0 24 24"
                    fill="none"
                    stroke="currentColor"
                    strokeWidth="2"
                    strokeLinecap="round"
                    strokeLinejoin="round"
                    aria-hidden="true"
                  >
                    <path d="m21.73 18-8-14a2 2 0 0 0-3.48 0l-8 14A2 2 0 0 0 4 21h16a2 2 0 0 0 1.73-3Z" />
                    <line x1="12" y1="9" x2="12" y2="13" />
                    <line x1="12" y1="17" x2="12.01" y2="17" />
                  </svg>
                  <span>
                    {getFloodReportButtonText(
                      { isReporting: isReportingFlood, cooldownRemaining: reportCooldownRemaining },
                      lang
                    )}
                  </span>
                </button>

                {/* PASSIVE REPORT FEEDBACK CHIP (Auto-dismisses in 3s, subtle monochromatic zinc) */}
                {floodReportFeedback && (
                  <span className="px-2 py-0.5 rounded border border-zinc-700 bg-zinc-900 text-zinc-300 text-xs font-mono select-none">
                    [{floodReportFeedback.text}]
                  </span>
                )}

                {/* PASSIVE VLM VERDICT BADGE */}
                {floodVerifyResult && (
                  <span
                    className={`px-2 py-0.5 rounded border text-xs font-bold inline-flex items-center gap-1.5 ${
                      floodVerifyResult.flood_severity === 'DRY'
                        ? 'border-zinc-500/80 bg-zinc-900/60 text-zinc-200'
                        : floodVerifyResult.flood_severity === 'WET'
                        ? 'border-sky-500/80 bg-sky-950/60 text-sky-300'
                        : floodVerifyResult.flood_severity === 'FLOODED_SHALLOW'
                        ? 'border-amber-500/80 bg-amber-950/60 text-amber-300'
                        : floodVerifyResult.flood_severity === 'FLOODED_DEEP'
                        ? 'border-rose-500/80 bg-rose-950/60 text-rose-300'
                        : 'border-zinc-700 bg-zinc-900 text-zinc-400'
                    }`}
                    title={
                      floodVerifyResult.description
                        ? `${floodVerifyResult.description} (${(floodVerifyResult.confidence * 100).toFixed(0)}%)`
                        : undefined
                    }
                  >
                    <span>
                      [{lang === 'vi' ? FLOOD_VERIFY_I18N.BADGE_PREFIX.vi : FLOOD_VERIFY_I18N.BADGE_PREFIX.en}:{' '}
                      {FLOOD_VERIFY_I18N.SEVERITY[floodVerifyResult.flood_severity as keyof typeof FLOOD_VERIFY_I18N.SEVERITY]?.[lang] ||
                        floodVerifyResult.flood_severity}]
                    </span>
                    {floodVerifyResult.confidence > 0 && (
                      <span className="text-[10px] opacity-75 font-normal">
                        {(floodVerifyResult.confidence * 100).toFixed(0)}%
                      </span>
                    )}
                  </span>
                )}

                {/* PASSIVE ERROR BADGE (Auto-dismisses in 4s, subtle monochromatic zinc) */}
                {floodVerifyError && (
                  <span className="px-2 py-0.5 rounded border border-zinc-700 bg-zinc-900 text-zinc-400 text-xs font-mono select-none">
                    [{floodVerifyError}]
                  </span>
                )}
              </div>
            )}
            {floodVerifyResult?.description && (
              <div className="mt-1.5 text-xs text-zinc-400 font-mono italic">
                &ldquo;{floodVerifyResult.description}&rdquo;
              </div>
            )}
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
              onError={handleImageError}
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
