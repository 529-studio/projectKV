'use client';

import React, { useState, useEffect, useCallback, useMemo } from 'react';
import Link from 'next/link';
import {
  handleHotkey,
  isLabelComplete,
  formatSnapshotTimestamp,
  isAutoHarvested,
  filterSnapshots,
  LABELER_I18N,
  getStoredDevToken,
  setStoredDevToken,
  clearStoredDevToken,
  buildAuthHeaders,
  type LabelFilterStatus,
  type FloodLevel,
  type TrafficLevel,
  type DirectionalTrafficLevel,
  type FloodSubmersionAnchor,
  type QualityTag,
  type QueueReason,
  type Weather,
  type TimeOfDay,
  type LabelState,
  type LabelPayload,
  type SnapshotItem,
} from '@/utils/labelerUtils';
import { getApiUrl } from '@/config/constants';

export default function LabelerPage() {
  const [locale, setLocale] = useState<'vi' | 'en'>('vi');
  const t = useMemo(() => LABELER_I18N[locale], [locale]);

  const [devToken, setDevToken] = useState<string>('');
  const [authMethod, setAuthMethod] = useState<'cloudflare_access' | 'dev_token' | 'none'>('none');
  const [userEmail, setUserEmail] = useState<string>('');
  const [isAuthenticated, setIsAuthenticated] = useState<boolean>(false);
  const [inputToken, setInputToken] = useState<string>('');
  const [authError, setAuthError] = useState<string | null>(null);
  const [isUnlocking, setIsUnlocking] = useState<boolean>(false);

  const [dates, setDates] = useState<string[]>([]);
  const [selectedDate, setSelectedDate] = useState<string>('');
  const [annotator, setAnnotator] = useState<string>('');
  const [filterStatus, setFilterStatus] = useState<LabelFilterStatus>('all');
  const [snapshots, setSnapshots] = useState<SnapshotItem[]>([]);
  const [currentIndex, setCurrentIndex] = useState<number>(0);
  const [labels, setLabels] = useState<Record<string, LabelPayload>>({});
  const [currentDraft, setCurrentDraft] = useState<LabelState>({});
  const [isLoading, setIsLoading] = useState<boolean>(true);
  const [isSaving, setIsSaving] = useState<boolean>(false);
  const [saveStatus, setSaveStatus] = useState<string | null>(null);

  // Initialize auth on mount: Probe Cloudflare Zero Trust first, fallback to stored dev token
  useEffect(() => {
    let isMounted = true;

    async function checkInitialAuth() {
      // 1. Probe Cloudflare Zero Trust session
      try {
        const probeRes = await fetch(getApiUrl('/api/archiver/auth/me'), {
          credentials: 'include',
        });
        if (probeRes.ok) {
          const authData = await probeRes.json();
          if (isMounted && authData.authenticated) {
            setIsAuthenticated(true);
            setAuthMethod(authData.auth_method || 'cloudflare_access');
            if (authData.user_email && authData.user_email !== 'cf-access-user' && authData.user_email !== 'dev-token-user' && authData.user_email !== 'dev-bypass') {
              setUserEmail(authData.user_email);
              setAnnotator((prev) => prev || authData.user_email);
            }
            return;
          }
        }
      } catch {
        // Probe endpoint unauthenticated; fall through to dev token
      }

      // 2. Fallback: Check if dev token was stored in sessionStorage
      const stored = getStoredDevToken();
      if (stored && isMounted) {
        setDevToken(stored);
        setAuthMethod('dev_token');
        setIsAuthenticated(true);
      }
    }

    checkInitialAuth();
    return () => {
      isMounted = false;
    };
  }, []);

  // Load annotator nickname from localStorage
  useEffect(() => {
    if (typeof window !== 'undefined') {
      const storedAnnotator = localStorage.getItem('labeler_annotator');
      if (storedAnnotator) {
        setAnnotator(storedAnnotator);
      }
    }
  }, []);

  const handleAnnotatorChange = (val: string) => {
    setAnnotator(val);
    if (typeof window !== 'undefined') {
      localStorage.setItem('labeler_annotator', val);
    }
  };

  // Fetch available dates on mount (only when authenticated)
  useEffect(() => {
    if (!isAuthenticated) return;

    async function loadDates() {
      try {
        const res = await fetch(getApiUrl('/api/archiver/dates'), {
          headers: buildAuthHeaders(devToken),
          credentials: 'include',
        });
        if (res.status === 401 || res.status === 403) {
          clearStoredDevToken();
          setIsAuthenticated(false);
          setDevToken('');
          setAuthError(t.devInvalidToken);
          return;
        }
        if (res.ok) {
          const data: string[] = await res.json();
          setDates(data);
          if (data.length > 0 && !selectedDate) {
            setSelectedDate(data[0]);
          }
        }
      } catch (err) {
        console.error('Failed to load dates', err);
      }
    }
    loadDates();
  }, [isAuthenticated, devToken, selectedDate, t.devInvalidToken]);

  // Fetch snapshots and existing labels when selectedDate changes (only when authenticated)
  useEffect(() => {
    if (!isAuthenticated || !selectedDate) return;

    let isSubscribed = true;
    setIsLoading(true);

    async function loadCatalog() {
      try {
        const [snapRes, labelRes] = await Promise.all([
          fetch(getApiUrl(`/api/archiver/snapshots?date=${selectedDate}&limit=200`), {
            headers: buildAuthHeaders(devToken),
            credentials: 'include',
          }),
          fetch(getApiUrl(`/api/archiver/labels?date=${selectedDate}`), {
            headers: buildAuthHeaders(devToken),
            credentials: 'include',
          }),
        ]);

        if (!isSubscribed) return;

        if (snapRes.status === 401 || labelRes.status === 401 || snapRes.status === 403 || labelRes.status === 403) {
          clearStoredDevToken();
          setIsAuthenticated(false);
          setDevToken('');
          setAuthError(t.devInvalidToken);
          return;
        }

        if (snapRes.ok) {
          const snapData = await snapRes.json();
          setSnapshots(snapData.snapshots || []);
          setCurrentIndex(0);
        }

        if (labelRes.ok) {
          const labelData = await labelRes.json();
          const labelMap: Record<string, LabelPayload> = {};
          if (Array.isArray(labelData.labels)) {
            labelData.labels.forEach((lp: LabelPayload) => {
              labelMap[lp.snapshot_key] = lp;
            });
          }
          setLabels(labelMap);
        }
      } catch (err) {
        console.error('Failed to load snapshots or labels', err);
      } finally {
        if (isSubscribed) {
          setIsLoading(false);
        }
      }
    }

    loadCatalog();
    return () => {
      isSubscribed = false;
    };
  }, [isAuthenticated, devToken, selectedDate, t.devInvalidToken]);

  const filteredSnapshots = useMemo(() => {
    return filterSnapshots(snapshots, labels, filterStatus);
  }, [snapshots, labels, filterStatus]);

  const counts = useMemo(() => {
    let pendingAuto = 0;
    let approvedHuman = 0;
    snapshots.forEach((s) => {
      const l = labels[s.key];
      if (l) {
        if (isAutoHarvested(l)) {
          pendingAuto++;
        } else {
          approvedHuman++;
        }
      }
    });
    return {
      all: snapshots.length,
      pendingAuto,
      approvedHuman,
    };
  }, [snapshots, labels]);

  // Clamp currentIndex if filteredSnapshots shrinks or changes
  useEffect(() => {
    if (filteredSnapshots.length > 0 && currentIndex >= filteredSnapshots.length) {
      setCurrentIndex(filteredSnapshots.length - 1);
    }
  }, [filteredSnapshots.length, currentIndex]);

  const handleFilterChange = (status: LabelFilterStatus) => {
    setFilterStatus(status);
    setCurrentIndex(0);
  };

  const currentSnapshot: SnapshotItem | undefined = filteredSnapshots[currentIndex];

  // Sync draft state with existing label or reset on snapshot change
  useEffect(() => {
    if (!currentSnapshot) {
      setCurrentDraft({});
      return;
    }

    const existing = labels[currentSnapshot.key];
    if (existing) {
      setCurrentDraft({
        flood_level: existing.flood_level,
        traffic_level: existing.traffic_level,
        traffic_dir_a: existing.traffic_dir_a,
        traffic_dir_b: existing.traffic_dir_b,
        queue_reason: existing.queue_reason,
        flood_submersion_anchor: existing.flood_submersion_anchor,
        quality_tags: existing.quality_tags,
        weather: existing.weather,
        time_of_day: existing.time_of_day,
        notes: existing.notes || '',
      });
    } else {
      setCurrentDraft({});
    }
    setSaveStatus(null);
  }, [currentSnapshot, labels]);

  // Fast-approve candidate label and advance
  const handleFastApprove = useCallback(async () => {
    if (!currentSnapshot || !selectedDate) return;

    const reviewer = annotator.trim() || 'reviewer';
    const existing = labels[currentSnapshot.key];
    const payload: LabelPayload = {
      date: selectedDate,
      camera_id: currentSnapshot.camera_id,
      snapshot_key: currentSnapshot.key,
      annotator: reviewer,
      flood_level: currentDraft.flood_level || existing?.flood_level || 'none',
      traffic_level: currentDraft.traffic_level || existing?.traffic_level || 'clear',
      traffic_dir_a:
        currentDraft.traffic_dir_a ||
        existing?.traffic_dir_a ||
        (currentDraft.traffic_level as DirectionalTrafficLevel) ||
        'clear',
      traffic_dir_b: currentDraft.traffic_dir_b || existing?.traffic_dir_b || 'not_applicable',
      queue_reason: currentDraft.queue_reason || existing?.queue_reason || 'free_flow',
      flood_submersion_anchor:
        currentDraft.flood_submersion_anchor ||
        existing?.flood_submersion_anchor ||
        (currentDraft.flood_level === 'deep'
          ? 'exhaust_hub_over_15cm'
          : currentDraft.flood_level === 'puddle'
          ? 'tire_base_under_10cm'
          : 'dry'),
      quality_tags: currentDraft.quality_tags || existing?.quality_tags || [],
      weather: currentDraft.weather || existing?.weather || 'clear',
      time_of_day: currentDraft.time_of_day || existing?.time_of_day || 'day',
      notes: currentDraft.notes || existing?.notes || '',
      timestamp: Math.floor(Date.now() / 1000),
    };

    setIsSaving(true);
    try {
      const res = await fetch(getApiUrl('/api/archiver/labels'), {
        method: 'POST',
        headers: buildAuthHeaders(devToken, { 'Content-Type': 'application/json' }),
        credentials: 'include',
        body: JSON.stringify(payload),
      });

      if (res.status === 401 || res.status === 403) {
        clearStoredDevToken();
        setIsAuthenticated(false);
        setDevToken('');
        setAuthError(t.devInvalidToken);
        return;
      }

      if (res.ok) {
        setLabels((prev) => ({
          ...prev,
          [currentSnapshot.key]: payload,
        }));
        setSaveStatus('saved');
        if (filterStatus !== 'pending_auto') {
          if (currentIndex < filteredSnapshots.length - 1) {
            setCurrentIndex((i) => i + 1);
          }
        }
      } else {
        setSaveStatus('error');
      }
    } catch (err) {
      console.error('Failed to fast-approve label', err);
      setSaveStatus('error');
    } finally {
      setIsSaving(false);
    }
  }, [
    currentSnapshot,
    selectedDate,
    annotator,
    labels,
    currentDraft,
    currentIndex,
    filteredSnapshots.length,
    filterStatus,
    devToken,
    t.devInvalidToken,
  ]);

  // Save current label and optionally advance
  const saveCurrent = useCallback(
    async (advance = true) => {
      if (!currentSnapshot || !selectedDate) return;

      const payload: LabelPayload = {
        date: selectedDate,
        camera_id: currentSnapshot.camera_id,
        snapshot_key: currentSnapshot.key,
        annotator: annotator.trim() || 'anonymous',
        flood_level: currentDraft.flood_level || 'none',
        traffic_level: currentDraft.traffic_level || 'clear',
        traffic_dir_a: currentDraft.traffic_dir_a || (currentDraft.traffic_level as DirectionalTrafficLevel) || 'clear',
        traffic_dir_b: currentDraft.traffic_dir_b || 'not_applicable',
        queue_reason: currentDraft.queue_reason || 'free_flow',
        flood_submersion_anchor: currentDraft.flood_submersion_anchor || (currentDraft.flood_level === 'deep' ? 'exhaust_hub_over_15cm' : (currentDraft.flood_level === 'puddle' ? 'tire_base_under_10cm' : 'dry')),
        quality_tags: currentDraft.quality_tags || [],
        weather: currentDraft.weather || 'clear',
        time_of_day: currentDraft.time_of_day || 'day',
        notes: currentDraft.notes || '',
        timestamp: Math.floor(Date.now() / 1000),
      };

      setIsSaving(true);
      try {
        const res = await fetch(getApiUrl('/api/archiver/labels'), {
          method: 'POST',
          headers: buildAuthHeaders(devToken, { 'Content-Type': 'application/json' }),
          credentials: 'include',
          body: JSON.stringify(payload),
        });

        if (res.status === 401 || res.status === 403) {
          clearStoredDevToken();
          setIsAuthenticated(false);
          setDevToken('');
          setAuthError(t.devInvalidToken);
          return;
        }

        if (res.ok) {
          setLabels((prev) => ({
            ...prev,
            [currentSnapshot.key]: payload,
          }));
          setSaveStatus('saved');
          if (advance) {
            if (filterStatus !== 'pending_auto') {
              if (currentIndex < filteredSnapshots.length - 1) {
                setCurrentIndex((i) => i + 1);
              }
            }
          }
        } else {
          setSaveStatus('error');
        }
      } catch (err) {
        console.error('Failed to save label', err);
        setSaveStatus('error');
      } finally {
        setIsSaving(false);
      }
    },
    [
      currentSnapshot,
      selectedDate,
      annotator,
      currentDraft,
      currentIndex,
      filteredSnapshots.length,
      filterStatus,
      devToken,
      t.devInvalidToken,
    ]
  );

  // Global keyboard shortcuts
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      // Do not capture shortcuts when user is typing in form inputs
      const target = e.target as HTMLElement;
      if (
        target &&
        (target.tagName === 'INPUT' ||
          target.tagName === 'TEXTAREA' ||
          target.tagName === 'SELECT' ||
          target.isContentEditable)
      ) {
        return;
      }

      const res = handleHotkey(e.key, currentDraft, { shiftKey: e.shiftKey });
      if (res.action === 'prev') {
        e.preventDefault();
        setCurrentIndex((i) => Math.max(0, i - 1));
      } else if (res.action === 'next') {
        e.preventDefault();
        setCurrentIndex((i) => Math.min(filteredSnapshots.length - 1, i + 1));
      } else if (res.action === 'approve') {
        e.preventDefault();
        handleFastApprove();
      } else if (res.action === 'save') {
        e.preventDefault();
        saveCurrent(true);
      } else if (res.state !== currentDraft) {
        e.preventDefault();
        setCurrentDraft(res.state);
      }
    };

    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [currentDraft, filteredSnapshots.length, saveCurrent, handleFastApprove]);

  const handleExportJson = () => {
    const exportData = {
      date: selectedDate,
      annotator: annotator.trim() || 'anonymous',
      exported_at: new Date().toISOString(),
      total_labels: Object.keys(labels).length,
      labels: Object.values(labels),
    };
    const blob = new Blob([JSON.stringify(exportData, null, 2)], {
      type: 'application/json',
    });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `labels_${selectedDate || 'all'}.json`;
    a.click();
    URL.revokeObjectURL(url);
  };

  const handleUnlock = async (e: React.FormEvent) => {
    e.preventDefault();
    const token = inputToken.trim();
    if (!token) return;

    setIsUnlocking(true);
    setAuthError(null);
    try {
      const res = await fetch(getApiUrl('/api/archiver/dates'), {
        headers: buildAuthHeaders(token),
        credentials: 'include',
      });

      if (res.ok) {
        setStoredDevToken(token);
        setDevToken(token);
        setAuthMethod('dev_token');
        setIsAuthenticated(true);
        setAuthError(null);
        const data: string[] = await res.json();
        setDates(data);
        if (data.length > 0 && !selectedDate) {
          setSelectedDate(data[0]);
        }
      } else if (res.status === 401 || res.status === 403) {
        setAuthError(t.devInvalidToken);
      } else if (res.status === 503) {
        setAuthError(
          locale === 'vi'
            ? 'Backend chưa cấu hình DEV_ACCESS_TOKEN trên production/staging (503)'
            : 'Backend unconfigured: DEV_ACCESS_TOKEN is missing on production/staging (503)'
        );
      } else {
        setAuthError(
          locale === 'vi'
            ? `Lỗi máy chủ (${res.status}): Không thể xác thực token`
            : `Server error (${res.status}): Failed to verify token`
        );
      }
    } catch {
      setAuthError(
        locale === 'vi'
          ? `Lỗi kết nối: Không thể kết nối tới Backend tại ${getApiUrl('/api/archiver/dates')}`
          : `Connection error: Cannot reach Backend at ${getApiUrl('/api/archiver/dates')}`
      );
    } finally {
      setIsUnlocking(false);
    }
  };

  const handleLockSession = () => {
    clearStoredDevToken();
    setDevToken('');
    setAuthMethod('none');
    setUserEmail('');
    setIsAuthenticated(false);
    setInputToken('');
    setAuthError(null);
  };

  const labeledCount = useMemo(() => {
    return snapshots.filter((s) => labels[s.key]).length;
  }, [snapshots, labels]);

  if (!isAuthenticated) {
    return (
      <div className="h-screen w-full overflow-hidden bg-black text-zinc-100 flex flex-col font-sans select-none">
        <header className="shrink-0 border-b border-zinc-800 bg-zinc-950 px-4 py-3 flex items-center justify-between">
          <Link
            href="/"
            className="text-xs uppercase tracking-wider px-2 py-1 bg-zinc-900 border border-zinc-700 text-zinc-300 hover:text-white hover:bg-zinc-800 rounded transition"
          >
            ← {t.backToMap}
          </Link>
          <button
            type="button"
            onClick={() => setLocale((prev) => (prev === 'vi' ? 'en' : 'vi'))}
            className="text-xs font-mono px-2 py-1 bg-zinc-900 border border-zinc-700 rounded text-zinc-300 hover:text-white hover:border-zinc-500 transition"
          >
            {locale.toUpperCase()}
          </button>
        </header>

        <main className="flex-1 overflow-y-auto flex items-center justify-center p-4">
          <div className="w-full max-w-md border border-zinc-800 bg-zinc-950 p-6 rounded shadow-xl">
            <div className="text-[10px] font-mono tracking-widest text-zinc-500 uppercase mb-2">
              [TRUY CẬP NỘI BỘ | DEV ONLY]
            </div>
            <h2 className="text-base font-semibold uppercase tracking-wide text-zinc-100 mb-2">
              {t.devAuthRequired}
            </h2>
            <p className="text-xs text-zinc-400 mb-6 leading-relaxed">
              {t.devAuthSubtitle}
            </p>

            <form onSubmit={handleUnlock} className="space-y-4">
              <div>
                <input
                  type="password"
                  value={inputToken}
                  onChange={(e) => {
                    setInputToken(e.target.value);
                    if (authError) setAuthError(null);
                  }}
                  placeholder={t.devTokenPlaceholder}
                  className="w-full bg-zinc-900 border border-zinc-700 focus:border-zinc-400 focus:outline-none px-3 py-2 text-xs rounded text-zinc-200 placeholder-zinc-500 font-mono transition"
                  autoFocus
                />
              </div>

              {authError && (
                <div className="text-xs text-rose-400 bg-rose-950/40 border border-rose-900/60 px-3 py-2 rounded">
                  {authError}
                </div>
              )}

              <div className="flex items-center justify-end gap-3 pt-2">
                <Link
                  href="/"
                  className="text-xs text-zinc-400 hover:text-zinc-200 transition px-3 py-1.5"
                >
                  {t.backToMap}
                </Link>
                <button
                  type="submit"
                  disabled={isUnlocking || !inputToken.trim()}
                  className="text-xs font-medium px-4 py-2 bg-zinc-100 text-black hover:bg-white disabled:opacity-50 disabled:cursor-not-allowed rounded uppercase tracking-wider transition"
                >
                  {isUnlocking ? '...' : t.devUnlock}
                </button>
              </div>
            </form>
          </div>
        </main>
      </div>
    );
  }

  return (
    <div className="h-screen w-full overflow-hidden bg-black text-zinc-100 flex flex-col font-sans select-none">
      {/* Top Header */}
      <header className="shrink-0 border-b border-zinc-800 bg-zinc-950 px-4 py-3 flex flex-wrap items-center justify-between gap-4">
        <div className="flex items-center gap-3">
          <Link
            href="/"
            className="text-xs uppercase tracking-wider px-2 py-1 bg-zinc-900 border border-zinc-700 text-zinc-300 hover:text-white hover:bg-zinc-800 rounded transition"
          >
            ← {t.backToMap}
          </Link>
          <h1 className="text-sm font-semibold tracking-wide text-zinc-100 uppercase">
            {t.title}
          </h1>
          <span className="text-xs text-zinc-500 hidden sm:inline">|</span>
          <span className="text-xs text-zinc-400 hidden sm:inline">{t.subtitle}</span>
          {authMethod === 'cloudflare_access' && (
            <span
              className="text-[10px] font-mono px-2 py-0.5 rounded bg-emerald-950/60 border border-emerald-800 text-emerald-300 hidden sm:inline"
              title={userEmail ? `Authenticated via Cloudflare Zero Trust (${userEmail})` : 'Authenticated via Cloudflare Zero Trust'}
            >
              [CF ACCESS{userEmail ? `: ${userEmail}` : ''}]
            </span>
          )}
          {authMethod === 'dev_token' && (
            <span
              className="text-[10px] font-mono px-2 py-0.5 rounded bg-amber-950/60 border border-amber-800 text-amber-300 hidden sm:inline"
              title="Authenticated via Developer Access Token Fallback"
            >
              [DEV TOKEN]
            </span>
          )}
        </div>

        <div className="flex items-center gap-3">
          {/* Language Toggle */}
          <button
            type="button"
            onClick={() => setLocale((prev) => (prev === 'vi' ? 'en' : 'vi'))}
            className="text-xs font-mono px-2 py-1 bg-zinc-900 border border-zinc-700 rounded text-zinc-300 hover:text-white hover:border-zinc-500 transition"
          >
            {locale.toUpperCase()}
          </button>

          {/* Export JSON Button */}
          <button
            type="button"
            onClick={handleExportJson}
            disabled={Object.keys(labels).length === 0}
            className="text-xs font-medium px-3 py-1 bg-zinc-800 hover:bg-zinc-700 border border-zinc-700 disabled:opacity-50 disabled:cursor-not-allowed rounded text-zinc-200 transition"
          >
            {t.exportJson}
          </button>

          {/* Lock Session Button */}
          <button
            type="button"
            onClick={handleLockSession}
            title={t.devLockSession}
            className="text-xs font-medium px-3 py-1 bg-zinc-900 hover:bg-zinc-800 border border-zinc-700 rounded text-zinc-300 transition"
          >
            {t.devLockSession}
          </button>
        </div>
      </header>

      {/* Control Bar: Date Selector, Annotator Name, Status Filter Pills, Progress Counter */}
      <div className="shrink-0 border-b border-zinc-800 bg-zinc-900/60 px-4 py-2.5 flex flex-wrap items-center justify-between gap-4 text-xs">
        <div className="flex flex-wrap items-center gap-4">
          <label className="flex items-center gap-2 text-zinc-400">
            <span>{t.dateSelect}:</span>
            <select
              value={selectedDate}
              onChange={(e) => setSelectedDate(e.target.value)}
              className="bg-black border border-zinc-700 text-zinc-100 px-2 py-1 rounded focus:outline-none focus:border-zinc-500"
            >
              {dates.map((d) => (
                <option key={d} value={d}>
                  {d}
                </option>
              ))}
            </select>
          </label>

          <label className="flex items-center gap-2 text-zinc-400">
            <span>{t.annotatorLabel}:</span>
            <input
              type="text"
              value={annotator}
              onChange={(e) => handleAnnotatorChange(e.target.value)}
              placeholder={t.annotatorPlaceholder}
              className="bg-black border border-zinc-700 text-zinc-100 px-2 py-1 rounded w-36 focus:outline-none focus:border-zinc-500"
            />
          </label>

          {/* Status Filter Pills */}
          <div className="flex items-center gap-1 font-mono text-xs border border-zinc-800 rounded p-0.5 bg-black">
            <button
              type="button"
              onClick={() => handleFilterChange('all')}
              className={`px-2 py-1 rounded transition ${
                filterStatus === 'all'
                  ? 'bg-zinc-100 text-black font-semibold'
                  : 'text-zinc-400 hover:text-zinc-200'
              }`}
            >
              {t.filterAll} ({counts.all})
            </button>
            <button
              type="button"
              onClick={() => handleFilterChange('pending_auto')}
              className={`px-2 py-1 rounded transition ${
                filterStatus === 'pending_auto'
                  ? 'bg-zinc-100 text-black font-semibold'
                  : 'text-zinc-400 hover:text-zinc-200'
              }`}
            >
              {t.filterPendingAuto} ({counts.pendingAuto})
            </button>
            <button
              type="button"
              onClick={() => handleFilterChange('approved_human')}
              className={`px-2 py-1 rounded transition ${
                filterStatus === 'approved_human'
                  ? 'bg-zinc-100 text-black font-semibold'
                  : 'text-zinc-400 hover:text-zinc-200'
              }`}
            >
              {t.filterApprovedHuman} ({counts.approvedHuman})
            </button>
          </div>
        </div>

        <div className="flex items-center gap-4 text-zinc-400 font-mono">
          <span>
            {t.progress}: {labeledCount} / {snapshots.length} (
            {snapshots.length > 0 ? Math.round((labeledCount / snapshots.length) * 100) : 0}%)
          </span>
          {saveStatus === 'saved' && (
            <span className="text-zinc-200 bg-zinc-800 px-2 py-0.5 rounded text-[11px]">
              {t.savedBadge}
            </span>
          )}
          {saveStatus === 'error' && (
            <span className="text-red-400 bg-red-950/50 border border-red-900 px-2 py-0.5 rounded text-[11px]">
              {t.saveError}
            </span>
          )}
        </div>
      </div>

      {/* Main Work Area */}
      <div className="flex-1 min-h-0 flex flex-col lg:flex-row overflow-y-auto lg:overflow-hidden">
        {/* Left / Center: Main Snapshot Image & Viewer */}
        <div className="w-full shrink-0 flex flex-col bg-zinc-950 p-3 lg:p-4 lg:flex-1 lg:min-h-0 lg:overflow-hidden">
          {isLoading ? (
            <div className="flex-1 flex items-center justify-center text-zinc-500 text-sm">
              {t.loading}
            </div>
          ) : !currentSnapshot ? (
            <div className="flex-1 flex items-center justify-center text-zinc-500 text-sm">
              {t.noSnapshots}
            </div>
          ) : (
            <div className="flex flex-col h-full gap-3 min-h-0">
              {/* Snapshot Metadata Bar */}
              <div className="shrink-0 flex flex-wrap items-center justify-between text-xs text-zinc-400 border-b border-zinc-800/80 pb-2">
                <div className="flex items-center gap-2 font-mono">
                  <span className="text-zinc-100 font-semibold">
                    {currentSnapshot.camera_name || currentSnapshot.camera_id}
                  </span>
                  <span className="text-zinc-600">|</span>
                  <span className="text-zinc-400">{currentSnapshot.camera_id}</span>
                  <span className="text-zinc-600">|</span>
                  {/* Provenance Badge */}
                  {(() => {
                    const currentLabel = labels[currentSnapshot.key];
                    if (!currentLabel) {
                      return (
                        <span className="text-[10px] uppercase font-mono px-1.5 py-0.5 rounded border border-zinc-800 bg-zinc-900/60 text-zinc-500">
                          {t.badgeUnlabeled}
                        </span>
                      );
                    }
                    if (isAutoHarvested(currentLabel)) {
                      return (
                        <span className="text-[10px] font-mono px-2 py-0.5 rounded border border-zinc-700 bg-zinc-900 text-zinc-200">
                          [{t.badgeAutoHarvester}]
                        </span>
                      );
                    }
                    return (
                      <span className="text-[10px] font-mono px-2 py-0.5 rounded border border-zinc-700 bg-zinc-900 text-zinc-200">
                        [{t.badgeHumanReviewer}: {currentLabel.annotator}]
                      </span>
                    );
                  })()}
                </div>
                <div className="flex items-center gap-3 font-mono text-[11px]">
                  <span>{formatSnapshotTimestamp(currentSnapshot.timestamp)}</span>
                  <span className="text-zinc-600">|</span>
                  <span className="uppercase text-zinc-300">{currentSnapshot.reason}</span>
                  <span className="text-zinc-600">|</span>
                  <span>{Math.round(currentSnapshot.size / 1024)} KB</span>
                </div>
              </div>

              {/* High-res Image View */}
              <div className="w-full h-[260px] sm:h-[340px] lg:h-auto lg:flex-1 lg:min-h-0 relative flex items-center justify-center bg-black border border-zinc-800 rounded overflow-hidden">
                {/* eslint-disable-next-line @next/next/no-img-element */}
                <img
                  src={(() => {
                    const baseUrl = currentSnapshot.key
                      ? `/api/archiver/snapshots/raw?key=${encodeURIComponent(currentSnapshot.key)}`
                      : currentSnapshot.raw_url;
                    return devToken
                      ? `${getApiUrl(baseUrl)}&token=${encodeURIComponent(devToken)}`
                      : getApiUrl(baseUrl);
                  })()}
                  alt={`Snapshot from camera ${currentSnapshot.camera_id}`}
                  className="max-h-full max-w-full object-contain"
                />
              </div>

              {/* Navigation Bar */}
              <div className="shrink-0 flex items-center justify-between pt-2">
                <button
                  type="button"
                  onClick={() => setCurrentIndex((i) => Math.max(0, i - 1))}
                  disabled={currentIndex === 0}
                  className="px-3 py-1.5 bg-zinc-900 border border-zinc-700 hover:bg-zinc-800 disabled:opacity-30 disabled:cursor-not-allowed rounded text-xs text-zinc-300 font-mono transition"
                >
                  ← {t.prev}
                </button>
                <span className="text-xs font-mono text-zinc-500">
                  {filteredSnapshots.length > 0 ? currentIndex + 1 : 0} / {filteredSnapshots.length}
                </span>
                <button
                  type="button"
                  onClick={() => setCurrentIndex((i) => Math.min(filteredSnapshots.length - 1, i + 1))}
                  disabled={currentIndex >= filteredSnapshots.length - 1}
                  className="px-3 py-1.5 bg-zinc-900 border border-zinc-700 hover:bg-zinc-800 disabled:opacity-30 disabled:cursor-not-allowed rounded text-xs text-zinc-300 font-mono transition"
                >
                  {t.next} →
                </button>
              </div>
            </div>
          )}
        </div>

        {/* Right: Annotation Controls & Hotkey Panels */}
        <div className="w-full shrink-0 border-t lg:border-t-0 lg:border-l border-zinc-800 bg-zinc-900/40 p-4 flex flex-col gap-5 lg:w-96 lg:min-h-0 lg:overflow-y-auto">
          {/* Dimension 1: Flood Level & Physical Anchor */}
          <div className="flex flex-col gap-2">
            <span className="text-xs font-semibold text-zinc-400 uppercase tracking-wider">
              {t.floodLabel}
            </span>
            <div className="grid grid-cols-3 gap-2">
              {[
                { val: 'none' as FloodLevel, label: t.floodNone, anchor: 'dry' as FloodSubmersionAnchor },
                { val: 'puddle' as FloodLevel, label: t.floodPuddle, anchor: 'tire_base_under_10cm' as FloodSubmersionAnchor },
                { val: 'deep' as FloodLevel, label: t.floodDeep, anchor: 'exhaust_hub_over_15cm' as FloodSubmersionAnchor },
              ].map((item) => (
                <button
                  key={item.val}
                  type="button"
                  onClick={() =>
                    setCurrentDraft((d) => ({
                      ...d,
                      flood_level: item.val,
                      flood_submersion_anchor: d.flood_submersion_anchor || item.anchor,
                    }))
                  }
                  className={`text-xs px-2 py-2 rounded border font-mono transition text-left ${
                    currentDraft.flood_level === item.val
                      ? 'bg-zinc-100 text-black border-white font-bold'
                      : 'bg-zinc-950 border-zinc-800 text-zinc-300 hover:border-zinc-600'
                  }`}
                >
                  {item.label}
                </button>
              ))}
            </div>

            {/* Physical Flood Submersion Anchor */}
            <div className="mt-1 flex flex-col gap-1 bg-zinc-950/60 border border-zinc-800/80 p-2 rounded">
              <span className="text-[11px] font-mono text-zinc-400 font-medium">
                {t.floodSubmersionLabel}
              </span>
              <div className="grid grid-cols-2 gap-1.5 text-[10px] font-mono">
                {[
                  { val: 'dry' as FloodSubmersionAnchor, label: t.anchorDry },
                  { val: 'wet_asphalt_no_standing' as FloodSubmersionAnchor, label: t.anchorWetNoStanding },
                  { val: 'tire_base_under_10cm' as FloodSubmersionAnchor, label: t.anchorTireBase },
                  { val: 'exhaust_hub_over_15cm' as FloodSubmersionAnchor, label: t.anchorExhaustHub },
                ].map((item) => (
                  <button
                    key={item.val}
                    type="button"
                    onClick={() => setCurrentDraft((d) => ({ ...d, flood_submersion_anchor: item.val }))}
                    className={`px-1.5 py-1 rounded border text-left truncate transition ${
                      currentDraft.flood_submersion_anchor === item.val
                        ? 'bg-zinc-200 text-zinc-950 border-white font-semibold'
                        : 'bg-zinc-900 border-zinc-800 text-zinc-400 hover:border-zinc-700'
                    }`}
                  >
                    {item.label}
                  </button>
                ))}
              </div>
            </div>
          </div>

          {/* Dimension 2: Directional Traffic Density */}
          <div className="flex flex-col gap-2.5">
            {/* Direction A (Primary / Near lane) */}
            <div className="flex flex-col gap-1.5">
              <span className="text-xs font-semibold text-zinc-400 uppercase tracking-wider">
                {t.trafficDirALabel}
              </span>
              <div className="grid grid-cols-2 gap-1.5">
                {[
                  { val: 'clear' as DirectionalTrafficLevel, rollup: 'clear' as TrafficLevel, label: t.trafficClear },
                  { val: 'moderate' as DirectionalTrafficLevel, rollup: 'moderate' as TrafficLevel, label: t.trafficModerate },
                  { val: 'heavy' as DirectionalTrafficLevel, rollup: 'heavy' as TrafficLevel, label: t.trafficHeavy },
                  { val: 'congested' as DirectionalTrafficLevel, rollup: 'gridlock' as TrafficLevel, label: t.trafficGridlock },
                ].map((item) => (
                  <button
                    key={item.val}
                    type="button"
                    onClick={() =>
                      setCurrentDraft((d) => ({
                        ...d,
                        traffic_dir_a: item.val,
                        traffic_level: item.rollup,
                      }))
                    }
                    className={`text-xs px-2 py-1.5 rounded border font-mono transition text-left ${
                      currentDraft.traffic_dir_a === item.val || currentDraft.traffic_level === item.rollup
                        ? 'bg-zinc-100 text-black border-white font-bold'
                        : 'bg-zinc-950 border-zinc-800 text-zinc-300 hover:border-zinc-600'
                    }`}
                  >
                    {item.label}
                  </button>
                ))}
              </div>
            </div>

            {/* Direction B (Secondary / Far lane) */}
            <div className="flex flex-col gap-1.5">
              <span className="text-xs font-semibold text-zinc-400 uppercase tracking-wider">
                {t.trafficDirBLabel}
              </span>
              <div className="grid grid-cols-3 gap-1.5 text-[11px] font-mono">
                {[
                  { val: 'clear' as DirectionalTrafficLevel, label: 'Q: Clear' },
                  { val: 'moderate' as DirectionalTrafficLevel, label: 'W: Mod' },
                  { val: 'heavy' as DirectionalTrafficLevel, label: 'E: Hvy' },
                  { val: 'congested' as DirectionalTrafficLevel, label: 'R: Jam' },
                  { val: 'not_applicable' as DirectionalTrafficLevel, label: t.trafficNotApplicable },
                ].map((item) => (
                  <button
                    key={item.val}
                    type="button"
                    onClick={() => setCurrentDraft((d) => ({ ...d, traffic_dir_b: item.val }))}
                    className={`px-1.5 py-1.5 rounded border text-center transition ${
                      currentDraft.traffic_dir_b === item.val
                        ? 'bg-zinc-100 text-black border-white font-bold'
                        : 'bg-zinc-950 border-zinc-800 text-zinc-300 hover:border-zinc-600'
                    }`}
                  >
                    {item.label}
                  </button>
                ))}
              </div>
            </div>

            {/* Queue Reason */}
            <div className="flex flex-col gap-1 bg-zinc-950/60 border border-zinc-800/80 p-2 rounded">
              <span className="text-[11px] font-mono text-zinc-400 font-medium">
                {t.queueReasonLabel}
              </span>
              <div className="grid grid-cols-2 gap-1.5 text-[10px] font-mono">
                {[
                  { val: 'free_flow' as QueueReason, label: t.reasonFreeFlow },
                  { val: 'traffic_light' as QueueReason, label: t.reasonTrafficLight },
                  { val: 'bottleneck' as QueueReason, label: t.reasonBottleneck },
                  { val: 'flood_barrier' as QueueReason, label: t.reasonFloodBarrier },
                ].map((item) => (
                  <button
                    key={item.val}
                    type="button"
                    onClick={() => setCurrentDraft((d) => ({ ...d, queue_reason: item.val }))}
                    className={`px-1.5 py-1 rounded border text-left truncate transition ${
                      currentDraft.queue_reason === item.val
                        ? 'bg-zinc-200 text-zinc-950 border-white font-semibold'
                        : 'bg-zinc-900 border-zinc-800 text-zinc-400 hover:border-zinc-700'
                    }`}
                  >
                    {item.label}
                  </button>
                ))}
              </div>
            </div>
          </div>

          {/* Dimension: Quality & Environmental Tags */}
          <div className="flex flex-col gap-1.5 bg-zinc-950/60 border border-zinc-800/80 p-2 rounded">
            <span className="text-[11px] font-mono text-zinc-400 font-medium">
              {t.qualityTagsLabel}
            </span>
            <div className="grid grid-cols-2 gap-1.5 text-[10px] font-mono">
              {[
                { tag: 'rain_on_lens' as QualityTag, label: t.tagRainOnLens },
                { tag: 'night_glare' as QualityTag, label: t.tagNightGlare },
                { tag: 'occluded' as QualityTag, label: t.tagOccluded },
                { tag: 'signal_noise' as QualityTag, label: t.tagSignalNoise },
              ].map((item) => {
                const isActive = (currentDraft.quality_tags || []).includes(item.tag);
                return (
                  <button
                    key={item.tag}
                    type="button"
                    onClick={() => {
                      const tags = currentDraft.quality_tags || [];
                      const updated = isActive ? tags.filter((x) => x !== item.tag) : [...tags, item.tag];
                      setCurrentDraft((d) => ({ ...d, quality_tags: updated }));
                    }}
                    className={`px-1.5 py-1 rounded border text-left truncate transition ${
                      isActive
                        ? 'bg-zinc-200 text-zinc-950 border-white font-semibold'
                        : 'bg-zinc-900 border-zinc-800 text-zinc-400 hover:border-zinc-700'
                    }`}
                  >
                    {item.label}
                  </button>
                );
              })}
            </div>
          </div>

          {/* Dimension 3: Weather */}
          <div className="flex flex-col gap-2">
            <span className="text-xs font-semibold text-zinc-400 uppercase tracking-wider">
              {t.weatherLabel}
            </span>
            <div className="grid grid-cols-3 gap-2">
              {[
                { val: 'clear' as Weather, label: t.weatherClear },
                { val: 'rain' as Weather, label: t.weatherRain },
                { val: 'fog' as Weather, label: t.weatherFog },
              ].map((item) => (
                <button
                  key={item.val}
                  type="button"
                  onClick={() => setCurrentDraft((d) => ({ ...d, weather: item.val }))}
                  className={`text-xs px-2 py-2 rounded border font-mono transition text-left ${
                    currentDraft.weather === item.val
                      ? 'bg-zinc-100 text-black border-white font-bold'
                      : 'bg-zinc-950 border-zinc-800 text-zinc-300 hover:border-zinc-600'
                  }`}
                >
                  {item.label}
                </button>
              ))}
            </div>
          </div>

          {/* Dimension 4: Time of Day */}
          <div className="flex flex-col gap-2">
            <span className="text-xs font-semibold text-zinc-400 uppercase tracking-wider">
              {t.timeOfDayLabel}
            </span>
            <div className="grid grid-cols-3 gap-2">
              {[
                { val: 'day' as TimeOfDay, label: t.timeOfDayDay },
                { val: 'dusk' as TimeOfDay, label: t.timeOfDayDusk },
                { val: 'night' as TimeOfDay, label: t.timeOfDayNight },
              ].map((item) => (
                <button
                  key={item.val}
                  type="button"
                  onClick={() => setCurrentDraft((d) => ({ ...d, time_of_day: item.val }))}
                  className={`text-xs px-2 py-2 rounded border font-mono transition text-left ${
                    currentDraft.time_of_day === item.val
                      ? 'bg-zinc-100 text-black border-white font-bold'
                      : 'bg-zinc-950 border-zinc-800 text-zinc-300 hover:border-zinc-600'
                  }`}
                >
                  {item.label}
                </button>
              ))}
            </div>
          </div>

          {/* Notes Input */}
          <div className="flex flex-col gap-1.5">
            <span className="text-xs font-semibold text-zinc-400 uppercase tracking-wider">
              {t.notesLabel}
            </span>
            <textarea
              rows={2}
              value={currentDraft.notes || ''}
              onChange={(e) => setCurrentDraft((d) => ({ ...d, notes: e.target.value }))}
              placeholder={t.notesPlaceholder}
              className="bg-black border border-zinc-800 rounded p-2 text-xs text-zinc-200 focus:outline-none focus:border-zinc-600 font-mono resize-none"
            />
          </div>

          {/* Save & Advance Action Buttons */}
          <div className="mt-auto pt-4 flex flex-col gap-2 border-t border-zinc-800/80">
            {currentSnapshot && labels[currentSnapshot.key] && isAutoHarvested(labels[currentSnapshot.key]) && (
              <button
                type="button"
                onClick={handleFastApprove}
                disabled={isSaving}
                title={t.fastApproveTooltip}
                className="w-full py-2.5 px-4 bg-zinc-100 hover:bg-white text-black font-semibold rounded text-xs uppercase tracking-wider transition disabled:opacity-30 disabled:cursor-not-allowed shadow border border-white"
              >
                {isSaving ? '...' : t.fastApproveAction}
              </button>
            )}
            <button
              type="button"
              onClick={() => saveCurrent(true)}
              disabled={isSaving || !currentSnapshot || !isLabelComplete(currentDraft)}
              className={`w-full py-2.5 px-4 font-semibold rounded text-xs uppercase tracking-wider transition disabled:opacity-30 disabled:cursor-not-allowed ${
                currentSnapshot && labels[currentSnapshot.key] && isAutoHarvested(labels[currentSnapshot.key])
                  ? 'bg-zinc-900 hover:bg-zinc-800 text-zinc-300 border border-zinc-700'
                  : 'bg-zinc-100 hover:bg-white text-black shadow'
              }`}
            >
              {isSaving ? '...' : t.saveAndNext}
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}
