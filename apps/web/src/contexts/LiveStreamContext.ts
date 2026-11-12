'use client';

import React, {
  createContext,
  useContext,
  useEffect,
  useRef,
  useState,
  useCallback,
  useMemo,
} from 'react';
import { API_ENDPOINTS, getApiUrl } from '../config/constants.ts';

export type LiveStreamEventType =
  | 'camera_update'
  | 'hex_update'
  | 'flood_hex_update'
  | 'user_flood_report';

export type LiveStreamSubscriber<T = unknown> = (data: T) => void;

export interface LiveStreamContextValue {
  connected: boolean;
  subscribe: <T = unknown>(
    eventType: LiveStreamEventType,
    handler: LiveStreamSubscriber<T>
  ) => () => void;
}

export const defaultLiveStreamContextValue: LiveStreamContextValue = {
  connected: false,
  subscribe: () => () => {},
};

export const LiveStreamContext = createContext<LiveStreamContextValue>(
  defaultLiveStreamContextValue
);

/**
 * Pure helper to parse an SSE message event and extract data safely.
 * Returns parsed payload or null on JSON parse failure.
 */
export function parseSSEPayload(data: string): unknown | null {
  if (typeof data !== 'string' || !data.trim()) {
    return null;
  }
  try {
    return JSON.parse(data);
  } catch (err) {
    console.warn('Failed to parse SSE JSON payload:', err);
    return null;
  }
}

export interface LiveStreamProviderProps {
  children?: React.ReactNode;
  url?: string;
}

export function LiveStreamProvider({ children, url }: LiveStreamProviderProps) {
  // If an ancestor already provides LiveStreamContext with an active subscribe function, reuse it.
  const parentContext = useContext(LiveStreamContext);
  if (parentContext !== defaultLiveStreamContextValue) {
    return React.createElement(React.Fragment, null, children);
  }

  return React.createElement(LiveStreamRootProvider, { url }, children);
}

function LiveStreamRootProvider({ children, url }: LiveStreamProviderProps) {
  const [connected, setConnected] = useState(false);
  const streamUrl = url ?? getApiUrl(API_ENDPOINTS.SSE_LIVE);

  const subscribersRef = useRef<Map<LiveStreamEventType, Set<LiveStreamSubscriber<unknown>>>>(
    new Map([
      ['camera_update', new Set()],
      ['hex_update', new Set()],
      ['flood_hex_update', new Set()],
      ['user_flood_report', new Set()],
    ])
  );

  const subscribe = useCallback(
    <T = unknown>(eventType: LiveStreamEventType, handler: LiveStreamSubscriber<T>): (() => void) => {
      let set = subscribersRef.current.get(eventType);
      if (!set) {
        set = new Set();
        subscribersRef.current.set(eventType, set);
      }
      const genericHandler = handler as LiveStreamSubscriber<unknown>;
      set.add(genericHandler);
      return () => {
        set?.delete(genericHandler);
      };
    },
    []
  );

  useEffect(() => {
    if (typeof window === 'undefined' || typeof EventSource === 'undefined') {
      return;
    }

    let es: EventSource | null = null;
    let isCancelled = false;

    const dispatch = (eventType: LiveStreamEventType, rawData: string) => {
      setConnected(true);
      const parsed = parseSSEPayload(rawData);
      if (parsed === null) return;
      const listeners = subscribersRef.current.get(eventType);
      if (listeners && listeners.size > 0) {
        listeners.forEach((fn) => {
          try {
            fn(parsed);
          } catch (e) {
            console.error(`Error in LiveStream subscriber for ${eventType}:`, e);
          }
        });
      }
    };

    try {
      es = new EventSource(streamUrl);

      es.onopen = () => {
        if (!isCancelled) {
          setConnected(true);
        }
      };

      es.addEventListener('camera_update', (e: MessageEvent) => {
        if (!isCancelled) dispatch('camera_update', e.data);
      });

      es.addEventListener('hex_update', (e: MessageEvent) => {
        if (!isCancelled) dispatch('hex_update', e.data);
      });

      es.addEventListener('flood_hex_update', (e: MessageEvent) => {
        if (!isCancelled) dispatch('flood_hex_update', e.data);
      });

      es.addEventListener('user_flood_report', (e: MessageEvent) => {
        if (!isCancelled) dispatch('user_flood_report', e.data);
      });

      es.onerror = (err) => {
        if (!isCancelled) {
          setConnected(false);
          if (es?.readyState === EventSource.CLOSED) {
            console.warn('SSE connection closed:', err);
          }
        }
      };
    } catch (err) {
      console.warn('Failed to initialize EventSource:', err);
    }

    return () => {
      isCancelled = true;
      if (es) {
        es.close();
      }
      setConnected(false);
    };
  }, [streamUrl]);

  const value = useMemo<LiveStreamContextValue>(
    () => ({
      connected,
      subscribe,
    }),
    [connected, subscribe]
  );

  return React.createElement(LiveStreamContext.Provider, { value }, children);
}

export function useLiveStream(): LiveStreamContextValue {
  return useContext(LiveStreamContext);
}

export function useLiveStreamSubscription<T = unknown>(
  eventType: LiveStreamEventType,
  handler: LiveStreamSubscriber<T>
): void {
  const { subscribe } = useLiveStream();
  const handlerRef = useRef(handler);
  handlerRef.current = handler;

  useEffect(() => {
    return subscribe<T>(eventType, (data: T) => {
      handlerRef.current(data);
    });
  }, [eventType, subscribe]);
}

export function useCameraUpdate(handler: (data: unknown) => void): void {
  useLiveStreamSubscription('camera_update', handler);
}

export function useHexUpdate(handler: (data: unknown) => void): void {
  useLiveStreamSubscription('hex_update', handler);
}

export function useFloodHexUpdate(handler: (data: unknown) => void): void {
  useLiveStreamSubscription('flood_hex_update', handler);
}

export function useUserFloodReport(handler: (data: unknown) => void): void {
  useLiveStreamSubscription('user_flood_report', handler);
}
