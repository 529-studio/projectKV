/**
 * forecastApi.ts - Pure fetch function and runtime validation for Short-Horizon Forecast API (ADR-012 §D5, S10-T6)
 * Strict 4-file pattern Step 2.
 */

import { API_ENDPOINTS, getApiUrl } from '../config/constants';
import type { ForecastResult, ForecastErrorResponse } from '../types/forecast';
import { isValidForecastResponse } from './forecastUtils';

export interface FetchForecastOptions {
  signal?: AbortSignal;
  model?: 'ridge_delta' | 'persistence' | 'historical_median';
  timestamp?: string;
}

/**
 * Fetches short-horizon multi-horizon forecast from the canonical route.
 * GET /api/v1/cameras/:id/forecast
 */
export async function fetchForecast(
  cameraId: string,
  options: FetchForecastOptions = {}
): Promise<ForecastResult> {
  if (!cameraId || !cameraId.trim()) {
    return {
      success: false,
      status: 400,
      error: {
        error: 'invalid_camera_id',
        message: 'Camera ID is required',
      },
    };
  }

  const endpoint = API_ENDPOINTS.FORECAST(cameraId);
  const url = new URL(getApiUrl(endpoint), typeof window !== 'undefined' ? window.location.origin : 'http://localhost:3000');

  if (options.model) {
    url.searchParams.set('model', options.model);
  }
  if (options.timestamp) {
    url.searchParams.set('timestamp', options.timestamp);
  }

  try {
    const response = await fetch(url.toString(), {
      method: 'GET',
      headers: {
        Accept: 'application/json',
      },
      signal: options.signal,
    });

    if (response.ok) {
      const data = await response.json();
      if (!isValidForecastResponse(data)) {
        return {
          success: false,
          status: response.status,
          error: {
            error: 'invalid_response_schema',
            message: 'Server returned payload not conforming to forecast schema',
          },
        };
      }
      return { success: true, data };
    }

    // Handle non-200 responses (e.g. 422 insufficient_history, 503 model_unvalidated)
    let errorPayload: ForecastErrorResponse;
    try {
      errorPayload = (await response.json()) as ForecastErrorResponse;
    } catch {
      errorPayload = {
        error: response.statusText || 'request_failed',
        message: `HTTP ${response.status}`,
      };
    }

    return {
      success: false,
      status: response.status,
      error: errorPayload,
    };
  } catch (err) {
    if (err instanceof DOMException && err.name === 'AbortError') {
      throw err;
    }
    return {
      success: false,
      status: 0,
      error: {
        error: 'network_error',
        message: err instanceof Error ? err.message : 'Network request failed',
      },
    };
  }
}
