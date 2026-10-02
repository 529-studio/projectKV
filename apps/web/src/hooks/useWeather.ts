'use client';

import { useState, useEffect, useCallback, useRef } from 'react';
import { WeatherData, RadarMetadata } from '../utils/weatherUtils';
import { API_ENDPOINTS, getApiUrl } from '../config/constants';

const REFRESH_INTERVAL_MS = 5 * 60 * 1000; // 5 minutes

export interface UseWeatherReturn {
  weather: WeatherData | null;
  radar: RadarMetadata | null;
  loading: boolean;
  error: string | null;
  refresh: () => Promise<void>;
}

export function useWeather(): UseWeatherReturn {
  const [weather, setWeather] = useState<WeatherData | null>(null);
  const [radar, setRadar] = useState<RadarMetadata | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const lastFetchedRef = useRef<number>(0);

  const fetchData = useCallback(async () => {
    try {
      setLoading(true);
      setError(null);

      // Concurrent fetch of weather forecast and radar metadata via configured backend API URL
      const [weatherRes, radarRes] = await Promise.allSettled([
        fetch(getApiUrl(API_ENDPOINTS.WEATHER)),
        fetch(getApiUrl(API_ENDPOINTS.WEATHER_RADAR)),
      ]);

      if (weatherRes.status === 'fulfilled' && weatherRes.value.ok) {
        const weatherJson: WeatherData = await weatherRes.value.json();
        setWeather(weatherJson);
      } else if (weatherRes.status === 'rejected') {
        console.warn('Failed to fetch /api/weather:', weatherRes.reason);
      }

      if (radarRes.status === 'fulfilled' && radarRes.value.ok) {
        const radarJson: RadarMetadata = await radarRes.value.json();
        setRadar(radarJson);
      } else if (radarRes.status === 'rejected') {
        console.warn('Failed to fetch /api/weather/radar:', radarRes.reason);
      }

      if (weatherRes.status === 'rejected' && radarRes.status === 'rejected') {
        setError('Failed to fetch weather & radar data');
      }

      lastFetchedRef.current = Date.now();
    } catch (err: unknown) {
      const message = err instanceof Error ? err.message : 'Unknown error';
      setError(message);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchData();

    const interval = setInterval(() => {
      fetchData();
    }, REFRESH_INTERVAL_MS);

    const handleFocus = () => {
      if (Date.now() - lastFetchedRef.current > REFRESH_INTERVAL_MS) {
        fetchData();
      }
    };

    window.addEventListener('focus', handleFocus);
    return () => {
      clearInterval(interval);
      window.removeEventListener('focus', handleFocus);
    };
  }, [fetchData]);

  return {
    weather,
    radar,
    loading,
    error,
    refresh: fetchData,
  };
}
