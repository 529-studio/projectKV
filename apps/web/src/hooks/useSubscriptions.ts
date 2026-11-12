'use client';
/**
 * S12-T4 / Issue #402 — citizen subscription data hook.
 *
 * Contract: `docs/sprints/s12_t4_spec.md` §5.2.
 *
 * - The taxonomy is fetched lazily: mounting the hook does nothing, `refetch()`
 *   (called the first time the modal opens) performs the request.
 * - Both requests are bounded by AbortController timeouts and `submit` never
 *   throws: a failure becomes `{ ok: false, status, message }`.
 * - `NEXT_PUBLIC_TELEGRAM_BOT_USERNAME` is a last-resort fallback for the deep
 *   link; a value served by the API always wins (AC4).
 */

import { useCallback, useEffect, useRef, useState } from 'react';
import { API_ENDPOINTS, getApiUrl } from '../config/constants';
import {
  buildStartPayload,
  buildTelegramDeepLink,
  isValidChatId,
  normalizeBotUsername,
  type TargetSelection,
  type WardItem,
} from '../utils/subscriptionUtils';

export interface SubscriptionChokepoint {
  id: string;
  name?: string;
}

export interface SubscriptionTaxonomy {
  target_types: string[];
  zones: string[];
  wards: WardItem[];
  chokepoints: SubscriptionChokepoint[];
  bot_username?: string;
  deep_link_enabled?: boolean;
}

export interface SubmitResult {
  ok: boolean;
  status: number;
  message?: string;
  /** Raw error body, so the modal can map a field-scoped message. */
  body?: unknown;
}

const TAXONOMY_TIMEOUT_MS = 10_000;
const SUBMIT_TIMEOUT_MS = 8_000;

function normalizeTaxonomy(raw: unknown): SubscriptionTaxonomy {
  const value = (raw ?? {}) as Partial<SubscriptionTaxonomy>;
  const wards = Array.isArray(value.wards)
    ? value.wards.filter(
        (ward): ward is WardItem =>
          Boolean(ward) && typeof ward.name === 'string' && ward.name.length > 0,
      )
    : [];
  const chokepoints = Array.isArray(value.chokepoints)
    ? value.chokepoints.filter(
        (point): point is SubscriptionChokepoint =>
          Boolean(point) && typeof point.id === 'string' && point.id.length > 0,
      )
    : [];
  const zones = Array.isArray(value.zones)
    ? value.zones.filter((zone): zone is string => typeof zone === 'string' && zone.length > 0)
    : [];

  return {
    target_types: Array.isArray(value.target_types)
      ? value.target_types.filter((item): item is string => typeof item === 'string')
      : ['all', 'zone', 'ward', 'chokepoint'],
    zones,
    wards,
    chokepoints,
    bot_username: typeof value.bot_username === 'string' ? value.bot_username : undefined,
    deep_link_enabled: value.deep_link_enabled === true,
  };
}

/**
 * Resolves the public bot handle. The API value wins; the environment variable
 * is consulted only when the taxonomy did not publish a handle at all. When the
 * API explicitly reports deep links disabled the CTA stays disabled even if a
 * build-time handle exists, so the UI never promises a bot that is not live.
 */
export function resolveBotUsername(taxonomy: SubscriptionTaxonomy | null): string | null {
  const fromApi = normalizeBotUsername(taxonomy?.bot_username);
  if (fromApi) {
    return taxonomy?.deep_link_enabled === false ? null : fromApi;
  }
  return normalizeBotUsername(process.env.NEXT_PUBLIC_TELEGRAM_BOT_USERNAME);
}

export function useSubscriptions() {
  const [taxonomy, setTaxonomy] = useState<SubscriptionTaxonomy | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<Error | null>(null);

  const taxonomyController = useRef<AbortController | null>(null);
  const requestId = useRef(0);
  const mounted = useRef(true);

  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
      taxonomyController.current?.abort();
    };
  }, []);

  const refetch = useCallback(async () => {
    taxonomyController.current?.abort();
    const controller = new AbortController();
    taxonomyController.current = controller;
    const currentRequest = requestId.current + 1;
    requestId.current = currentRequest;

    if (mounted.current) {
      setLoading(true);
      setError(null);
    }

    const timeout = setTimeout(() => controller.abort(), TAXONOMY_TIMEOUT_MS);
    try {
      const response = await fetch(getApiUrl(API_ENDPOINTS.SUBSCRIPTIONS_TAXONOMY), {
        method: 'GET',
        headers: { Accept: 'application/json' },
        cache: 'no-store',
        signal: controller.signal,
      });
      if (!response.ok) {
        throw new Error(`taxonomy request failed with status ${response.status}`);
      }
      const payload = (await response.json()) as unknown;
      if (!mounted.current || currentRequest !== requestId.current) return;
      setTaxonomy(normalizeTaxonomy(payload));
    } catch (caught) {
      if (!mounted.current || currentRequest !== requestId.current) return;
      const failure =
        caught instanceof Error && caught.name !== 'AbortError'
          ? caught
          : new Error('taxonomy request timed out');
      setError(failure);
    } finally {
      clearTimeout(timeout);
      if (mounted.current && currentRequest === requestId.current) {
        setLoading(false);
      }
    }
  }, []);

  const submit = useCallback(
    async (selection: TargetSelection, chatId: string): Promise<SubmitResult> => {
      if (!isValidChatId(chatId)) {
        return { ok: false, status: 400, message: 'invalid chat id' };
      }

      const controller = new AbortController();
      const timeout = setTimeout(() => controller.abort(), SUBMIT_TIMEOUT_MS);
      try {
        const response = await fetch(getApiUrl(API_ENDPOINTS.SUBSCRIPTIONS), {
          method: 'POST',
          headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
          body: JSON.stringify({
            channel_name: 'telegram',
            chat_id: chatId,
            target_type: selection.type,
            target_id: selection.type === 'all' ? '' : selection.id,
          }),
          signal: controller.signal,
        });

        const rawBody = await response.text();
        let body: unknown = null;
        if (rawBody) {
          try {
            body = JSON.parse(rawBody);
          } catch {
            body = rawBody;
          }
        }

        return { ok: response.ok, status: response.status, body };
      } catch {
        // Network failure, timeout or abort: status 0 is the honest signal.
        return { ok: false, status: 0, body: null };
      } finally {
        clearTimeout(timeout);
      }
    },
    [],
  );

  const botUsername = resolveBotUsername(taxonomy);

  /** Builds the Telegram deep link for a selection, or null when disabled. */
  const buildDeepLink = useCallback(
    (selection: TargetSelection): string | null => {
      if (!botUsername) return null;
      try {
        // buildTelegramDeepLink enforces the 64-char bound from spec §2.1.
        buildStartPayload(selection);
        return buildTelegramDeepLink(botUsername, selection);
      } catch {
        return null;
      }
    },
    [botUsername],
  );

  return {
    taxonomy,
    loading,
    error,
    refetch,
    submit,
    botUsername,
    deepLinkEnabled: Boolean(botUsername),
    buildDeepLink,
  };
}
