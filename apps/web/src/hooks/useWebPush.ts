'use client';
/**
 * React hook and pure utilities for Web Push notifications (RFC 8291 / RFC 8292).
 * S18-T2 / Issue #525.
 * Strictly zero emoji per AGENTS.md Rule 1.
 */

import { useCallback, useEffect, useState } from 'react';

export type PushPermissionState = 'default' | 'granted' | 'denied' | 'unsupported';

export interface UseWebPushResult {
  isSupported: boolean;
  permission: PushPermissionState;
  isSubscribed: boolean;
  loading: boolean;
  error: string | null;
  endpoint: string | null;
  subscribe: (targetType?: string, targetId?: string) => Promise<boolean>;
  unsubscribe: () => Promise<boolean>;
  sendTestNotification: () => Promise<boolean>;
  refreshStatus: () => Promise<void>;
}

/**
 * Converts a Base64URL-encoded VAPID public key into a Uint8Array suitable for PushManager.subscribe.
 */
export function urlBase64ToUint8Array(base64String: string): Uint8Array {
  const clean = base64String.trim();
  const padding = '='.repeat((4 - (clean.length % 4)) % 4);
  const base64 = (clean + padding).replace(/-/g, '+').replace(/_/g, '/');

  if (typeof atob === 'function') {
    const rawData = atob(base64);
    const outputArray = new Uint8Array(rawData.length);
    for (let i = 0; i < rawData.length; ++i) {
      outputArray[i] = rawData.charCodeAt(i);
    }
    return outputArray;
  }

  // Node.js test environment fallback
  if (typeof Buffer !== 'undefined') {
    return new Uint8Array(Buffer.from(base64, 'base64'));
  }

  throw new Error('No base64 decoding method available');
}

/**
 * Checks whether the current environment supports Web Push APIs.
 */
export function checkPushSupport(
  nav: { serviceWorker?: unknown } | null = typeof navigator !== 'undefined' ? navigator : null,
  win: { PushManager?: unknown; Notification?: unknown } | null = typeof window !== 'undefined' ? window : null,
): boolean {
  if (!nav || !win) return false;
  return Boolean(
    'serviceWorker' in nav &&
      'PushManager' in win &&
      'Notification' in win,
  );
}

/**
 * Maps raw browser NotificationPermission to typed PushPermissionState.
 */
export function mapPushPermission(
  raw: NotificationPermission | null | undefined,
  supported = true,
): PushPermissionState {
  if (!supported) return 'unsupported';
  if (!raw) return 'default';
  if (raw === 'granted' || raw === 'denied') return raw;
  return 'default';
}

export function useWebPush(): UseWebPushResult {
  const [isSupported, setIsSupported] = useState<boolean>(false);
  const [permission, setPermission] = useState<PushPermissionState>('default');
  const [isSubscribed, setIsSubscribed] = useState<boolean>(false);
  const [loading, setLoading] = useState<boolean>(false);
  const [error, setError] = useState<string | null>(null);
  const [endpoint, setEndpoint] = useState<string | null>(null);

  const refreshStatus = useCallback(async () => {
    if (typeof window === 'undefined' || typeof navigator === 'undefined') return;

    const supported = checkPushSupport();
    setIsSupported(supported);

    if (!supported) {
      setPermission('unsupported');
      return;
    }

    const currentPermission = mapPushPermission(
      typeof Notification !== 'undefined' ? Notification.permission : null,
      true,
    );
    setPermission(currentPermission);

    if (currentPermission !== 'granted') {
      setIsSubscribed(false);
      setEndpoint(null);
      return;
    }

    try {
      const registration = await navigator.serviceWorker.ready;
      const sub = await registration.pushManager.getSubscription();
      if (sub) {
        setIsSubscribed(true);
        setEndpoint(sub.endpoint);
      } else {
        setIsSubscribed(false);
        setEndpoint(null);
      }
    } catch (err) {
      console.warn('[WebPush] Error inspecting push status:', err);
    }
  }, []);

  useEffect(() => {
    void refreshStatus();
  }, [refreshStatus]);

  const subscribe = useCallback(
    async (targetType = 'all', targetId = ''): Promise<boolean> => {
      setError(null);
      setLoading(true);

      try {
        if (!checkPushSupport()) {
          throw new Error('Web Push is not supported on this browser');
        }

        // 1. Request notification permission
        const perm = await Notification.requestPermission();
        const mappedPerm = mapPushPermission(perm, true);
        setPermission(mappedPerm);

        if (mappedPerm !== 'granted') {
          setLoading(false);
          return false;
        }

        // 2. Fetch VAPID public key
        const keyRes = await fetch('/api/push/vapid-key');
        if (!keyRes.ok) {
          throw new Error(`Failed to fetch VAPID key (${keyRes.status})`);
        }
        const keyData = await keyRes.json();
        const vapidPublicKey = keyData.public_key;
        if (!vapidPublicKey) {
          throw new Error('Server returned empty VAPID public key');
        }

        // 3. Register service worker if not already registered
        let registration: ServiceWorkerRegistration;
        try {
          registration = await navigator.serviceWorker.register('/sw.js');
          await navigator.serviceWorker.ready;
        } catch (swErr) {
          throw new Error(`Service Worker registration failed: ${swErr instanceof Error ? swErr.message : String(swErr)}`);
        }

        // 4. Subscribe to PushManager
        const applicationServerKey = urlBase64ToUint8Array(vapidPublicKey);
        const sub = await registration.pushManager.subscribe({
          userVisibleOnly: true,
          applicationServerKey: applicationServerKey.buffer as ArrayBuffer,
        });

        const subJson = sub.toJSON();
        if (!subJson.endpoint || !subJson.keys?.p256dh || !subJson.keys?.auth) {
          throw new Error('Browser returned incomplete push credentials');
        }

        // 5. Send subscription to Go backend
        const apiRes = await fetch('/api/push/subscribe', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            endpoint: subJson.endpoint,
            keys: {
              p256dh: subJson.keys.p256dh,
              auth: subJson.keys.auth,
            },
            target_type: targetType,
            target_id: targetId,
          }),
        });

        if (!apiRes.ok) {
          const errData = await apiRes.json().catch(() => ({}));
          throw new Error(errData.error || `Push subscription failed (${apiRes.status})`);
        }

        setIsSubscribed(true);
        setEndpoint(sub.endpoint);
        setLoading(false);
        return true;
      } catch (err) {
        const msg = err instanceof Error ? err.message : String(err);
        setError(msg);
        setLoading(false);
        return false;
      }
    },
    [],
  );

  const unsubscribe = useCallback(async (): Promise<boolean> => {
    setError(null);
    setLoading(true);

    try {
      if (!checkPushSupport()) {
        setLoading(false);
        return true;
      }

      const registration = await navigator.serviceWorker.ready;
      const sub = await registration.pushManager.getSubscription();

      if (sub) {
        // 1. Notify backend to remove credentials and flood alert mappings
        await fetch('/api/push/unsubscribe', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ endpoint: sub.endpoint }),
        }).catch(() => {
          // ignore network failure on backend unsubscribe
        });

        // 2. Unsubscribe locally
        await sub.unsubscribe();
      }

      setIsSubscribed(false);
      setEndpoint(null);
      setLoading(false);
      return true;
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      setError(msg);
      setLoading(false);
      return false;
    }
  }, []);

  const sendTestNotification = useCallback(async (): Promise<boolean> => {
    setError(null);
    try {
      if (!checkPushSupport()) {
        throw new Error('Web Push is not supported on this browser');
      }

      const registration = await navigator.serviceWorker.ready;
      const sub = await registration.pushManager.getSubscription();
      if (!sub) {
        throw new Error('No active push subscription found');
      }

      const subJson = sub.toJSON();
      const res = await fetch('/api/push/test', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          endpoint: subJson.endpoint,
          keys: {
            p256dh: subJson.keys?.p256dh,
            auth: subJson.keys?.auth,
          },
        }),
      });

      if (!res.ok) {
        const errData = await res.json().catch(() => ({}));
        throw new Error(errData.error || `Failed to send test push (${res.status})`);
      }

      return true;
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      setError(msg);
      return false;
    }
  }, []);

  return {
    isSupported,
    permission,
    isSubscribed,
    loading,
    error,
    endpoint,
    subscribe,
    unsubscribe,
    sendTestNotification,
    refreshStatus,
  };
}
