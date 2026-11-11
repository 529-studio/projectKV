'use client';
/**
 * S12-T2 / Issue #400 — one-shot + watch geolocation state machine.
 *
 * Contract: `docs/sprints/s12_t2_spec.md` §4.2, §5 E1–E4, E12–E14.
 * Privacy: coordinates never leave the browser — no storage, no logging, no
 * network call. Support and secure-context detection happen in an effect so
 * the server render and the first client render agree (`idle`, unsupported).
 */
import { useCallback, useEffect, useRef, useState } from 'react';

export type GeolocationStatus =
  | 'idle'
  | 'locating'
  | 'success'
  | 'denied'
  | 'timeout'
  | 'unavailable'
  | 'unsupported'
  | 'insecure'
  | 'invalid';

export interface GeolocationFix {
  latitude: number;
  longitude: number;
  accuracy: number;
  timestamp: number;
}

export type GeolocationErrorCode =
  | 'PERMISSION_DENIED'
  | 'TIMEOUT'
  | 'POSITION_UNAVAILABLE'
  | 'UNSUPPORTED'
  | 'INSECURE_CONTEXT'
  | 'INVALID_POSITION';

export interface UseGeolocationOptions {
  timeoutMs?: number;
  maximumAgeMs?: number;
  highAccuracy?: boolean;
}

export interface UseGeolocationReturn {
  status: GeolocationStatus;
  fix: GeolocationFix | null;
  error: GeolocationErrorCode | null;
  isSupported: boolean;
  isWatching: boolean;
  /** One-shot request; no-op while a request is already in flight. */
  request: () => void;
  /** Alias of {@link request}, allowed after success. */
  refresh: () => void;
  startWatch: () => void;
  stopWatch: () => void;
}

/** Raw shape accepted by {@link normalizeGeolocationPosition} (browser or fake). */
export interface RawGeolocationCoords {
  latitude?: number | null;
  longitude?: number | null;
  accuracy?: number | null;
}

export interface RawGeolocationPosition {
  coords?: RawGeolocationCoords | null;
  timestamp?: number | null;
}

const DEFAULT_TIMEOUT_MS = 10_000;
const DEFAULT_MAXIMUM_AGE_MS = 30_000;

/** Maps a `GeolocationPositionError.code` to our public error union. */
export function mapGeolocationErrorCode(code: number): GeolocationErrorCode {
  if (code === 1) return 'PERMISSION_DENIED';
  if (code === 3) return 'TIMEOUT';
  return 'POSITION_UNAVAILABLE';
}

function isFiniteNumber(value: unknown): value is number {
  return typeof value === 'number' && Number.isFinite(value);
}

/**
 * Validates and flattens a browser `GeolocationPosition`. Returns `null` for
 * missing, out-of-range or non-finite coordinates and for a missing,
 * non-finite or negative accuracy. Never mutates the input.
 */
export function normalizeGeolocationPosition(
  raw: GeolocationPosition | RawGeolocationPosition | null | undefined,
): GeolocationFix | null {
  if (!raw || typeof raw !== 'object') return null;

  const coords = (raw as RawGeolocationPosition).coords;
  if (!coords || typeof coords !== 'object') return null;

  const { latitude, longitude, accuracy } = coords;
  if (!isFiniteNumber(latitude) || !isFiniteNumber(longitude)) return null;
  if (Math.abs(latitude) > 90 || Math.abs(longitude) > 180) return null;

  if (!isFiniteNumber(accuracy) || accuracy < 0) return null;

  const timestamp = isFiniteNumber((raw as RawGeolocationPosition).timestamp)
    ? ((raw as RawGeolocationPosition).timestamp as number)
    : Date.now();

  return { latitude, longitude, accuracy, timestamp };
}

function statusForErrorCode(code: GeolocationErrorCode): GeolocationStatus {
  switch (code) {
    case 'PERMISSION_DENIED':
      return 'denied';
    case 'TIMEOUT':
      return 'timeout';
    case 'INVALID_POSITION':
      return 'invalid';
    case 'UNSUPPORTED':
      return 'unsupported';
    case 'INSECURE_CONTEXT':
      return 'insecure';
    case 'POSITION_UNAVAILABLE':
    default:
      return 'unavailable';
  }
}

function hasGeolocationApi(): boolean {
  return typeof navigator !== 'undefined' && Boolean(navigator.geolocation);
}

function isInsecureContext(): boolean {
  return typeof window !== 'undefined' && window.isSecureContext === false;
}

export function useGeolocation(options: UseGeolocationOptions = {}): UseGeolocationReturn {
  const {
    timeoutMs = DEFAULT_TIMEOUT_MS,
    maximumAgeMs = DEFAULT_MAXIMUM_AGE_MS,
    highAccuracy = true,
  } = options;

  const [status, setStatus] = useState<GeolocationStatus>('idle');
  const [fix, setFix] = useState<GeolocationFix | null>(null);
  const [error, setError] = useState<GeolocationErrorCode | null>(null);
  const [isSupported, setIsSupported] = useState(false);
  const [isWatching, setIsWatching] = useState(false);

  const mountedRef = useRef(true);
  const inFlightRef = useRef(false);
  const watchIdRef = useRef<number | null>(null);

  // Support / secure-context detection runs client-side only, after hydration.
  useEffect(() => {
    mountedRef.current = true;
    setIsSupported(hasGeolocationApi());

    return () => {
      mountedRef.current = false;
      inFlightRef.current = false;
      if (watchIdRef.current !== null && hasGeolocationApi()) {
        navigator.geolocation.clearWatch(watchIdRef.current);
        watchIdRef.current = null;
      }
      setIsWatching(false);
    };
  }, []);

  const request = useCallback(() => {
    if (typeof window === 'undefined') return;

    if (isInsecureContext()) {
      setError('INSECURE_CONTEXT');
      setStatus('insecure');
      return;
    }

    if (!hasGeolocationApi()) {
      setIsSupported(false);
      setError('UNSUPPORTED');
      setStatus('unsupported');
      return;
    }

    // Single-flight: double taps never create a second prompt.
    if (inFlightRef.current) return;
    inFlightRef.current = true;

    setError(null);
    setStatus('locating');

    navigator.geolocation.getCurrentPosition(
      (position) => {
        inFlightRef.current = false;
        if (!mountedRef.current) return;

        const normalized = normalizeGeolocationPosition(position);
        if (!normalized) {
          setError('INVALID_POSITION');
          setStatus('invalid');
          return;
        }

        setFix(normalized);
        setError(null);
        setStatus('success');
      },
      (positionError) => {
        inFlightRef.current = false;
        if (!mountedRef.current) return;

        const code = mapGeolocationErrorCode(positionError?.code as number);
        setError(code);
        setStatus(statusForErrorCode(code));
      },
      { enableHighAccuracy: highAccuracy, timeout: timeoutMs, maximumAge: maximumAgeMs },
    );
  }, [highAccuracy, maximumAgeMs, timeoutMs]);

  const refresh = useCallback(() => {
    request();
  }, [request]);

  const stopWatch = useCallback(() => {
    if (watchIdRef.current !== null && hasGeolocationApi()) {
      navigator.geolocation.clearWatch(watchIdRef.current);
    }
    watchIdRef.current = null;
    if (mountedRef.current) setIsWatching(false);
  }, []);

  const startWatch = useCallback(() => {
    if (typeof window === 'undefined') return;

    if (isInsecureContext()) {
      setError('INSECURE_CONTEXT');
      setStatus('insecure');
      return;
    }

    if (!hasGeolocationApi()) {
      setIsSupported(false);
      setError('UNSUPPORTED');
      setStatus('unsupported');
      return;
    }

    if (watchIdRef.current !== null) return;

    setIsWatching(true);
    setStatus((previous) => (previous === 'success' ? previous : 'locating'));

    watchIdRef.current = navigator.geolocation.watchPosition(
      (position) => {
        if (!mountedRef.current) return;

        const normalized = normalizeGeolocationPosition(position);
        if (!normalized) {
          setError('INVALID_POSITION');
          setStatus('invalid');
          stopWatch();
          return;
        }

        setFix(normalized);
        setError(null);
        setStatus('success');
      },
      (positionError) => {
        if (!mountedRef.current) return;

        const code = mapGeolocationErrorCode(positionError?.code as number);
        setError(code);
        setStatus(statusForErrorCode(code));
        stopWatch();
      },
      { enableHighAccuracy: highAccuracy, timeout: timeoutMs, maximumAge: maximumAgeMs },
    );
  }, [highAccuracy, maximumAgeMs, stopWatch, timeoutMs]);

  return {
    status,
    fix,
    error,
    isSupported,
    isWatching,
    request,
    refresh,
    startWatch,
    stopWatch,
  };
}
