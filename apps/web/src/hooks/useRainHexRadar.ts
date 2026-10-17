'use client';

import { useState, useEffect, useRef } from 'react';
import type { FloodHexCell } from '../utils/floodUtils';
import type { RadarMetadata, WeatherData } from '../utils/weatherUtils';
import type { Camera } from './useCameras';
import type { TrafficVerdict } from '../utils/trafficUtils';
import {
  getHexSamplingPoints,
  latLonToTilePixel,
  evaluateHexRadarIntensity,
  buildRadarTileUrl,
  generateOpenMeteoFallbackHexCells,
} from '../utils/rainHexUtils';

// Module-level cache for static H3 resolution 7 polygon definitions (121KB)
let cachedPolygons: Record<string, [number, number][]> | null = null;
let polygonFetchPromise: Promise<Record<string, [number, number][]> | null> | null = null;

async function loadH3Polygons(): Promise<Record<string, [number, number][]> | null> {
  if (cachedPolygons) return cachedPolygons;
  if (polygonFetchPromise) return polygonFetchPromise;

  polygonFetchPromise = fetch('/h3_hex_polygons.json')
    .then((res) => (res.ok ? res.json() : null))
    .then((data) => {
      if (data && typeof data === 'object') {
        // Enforce Resolution 7 only (starts with '87') to avoid drawing nested Resolution 9 sub-hexagons
        const res7Only: Record<string, [number, number][]> = {};
        for (const [k, v] of Object.entries(data)) {
          if (!k.startsWith('89')) {
            res7Only[k] = v as [number, number][];
          }
        }
        cachedPolygons = res7Only;
        return res7Only;
      }
      return null;
    })
    .catch((err) => {
      console.warn('Failed to load /h3_hex_polygons.json:', err);
      return null;
    });

  return polygonFetchPromise;
}

function loadImage(src: string): Promise<HTMLImageElement | null> {
  return new Promise((resolve) => {
    const img = new Image();
    img.crossOrigin = 'anonymous';
    img.onload = () => resolve(img);
    img.onerror = (e) => {
      console.warn('Radar tile load failed:', src, e);
      resolve(null);
    };
    img.src = src;
  });
}

export interface UseRainHexRadarReturn {
  rainRadarHexCells: Map<string, FloodHexCell>;
  loading: boolean;
  error: string | null;
}

export function useRainHexRadar(
  radar: RadarMetadata | null,
  weather: WeatherData | null,
  cameras: Camera[],
  floodVerdicts?: Map<string, TrafficVerdict>
): UseRainHexRadarReturn {
  const [rainRadarHexCells, setRainRadarHexCells] = useState<Map<string, FloodHexCell>>(new Map());
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  // Map cameras by H3 cell ID for richer tooltips and surface cross-validation
  const camerasByCellRef = useRef<Map<string, Camera[]>>(new Map());
  useEffect(() => {
    const map = new Map<string, Camera[]>();
    cameras.forEach((cam) => {
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const h3Id = cam.h3_id || cam.h3_res7 || (cam as any).h3Res7;
      if (h3Id) {
        const list = map.get(h3Id) || [];
        list.push(cam);
        map.set(h3Id, list);
      }
    });
    camerasByCellRef.current = map;
  }, [cameras]);

  const floodVerdictsRef = useRef<Map<string, TrafficVerdict> | undefined>(floodVerdicts);
  useEffect(() => {
    floodVerdictsRef.current = floodVerdicts;
  }, [floodVerdicts]);

  useEffect(() => {
    let isCancelled = false;

    async function processRadar() {
      try {
        setLoading(true);
        setError(null);

        const polygons = await loadH3Polygons();
        if (isCancelled) return;

        if (!polygons) {
          setRainRadarHexCells(new Map());
          return;
        }

        const template = radar?.latest_tile_template;
        if (!template) {
          // If no radar template, check Open-Meteo fallback
          if (weather) {
            const monitoredSet = new Set(Object.keys(polygons));
            const fallback = generateOpenMeteoFallbackHexCells(weather, polygons, monitoredSet);
            if (!isCancelled) setRainRadarHexCells(fallback);
          } else {
            if (!isCancelled) setRainRadarHexCells(new Map());
          }
          return;
        }

        // HCMC & Southeast Vietnam fit inside Zoom 8 tiles: (x=203, y=120) and (x=204, y=120)
        const tile203Url = buildRadarTileUrl(template, 203, 120, 8);
        const tile204Url = buildRadarTileUrl(template, 204, 120, 8);

        const [img203, img204] = await Promise.all([
          loadImage(tile203Url),
          loadImage(tile204Url),
        ]);

        if (isCancelled) return;

        // If both tile images failed (e.g. offline / CORS error), apply Open-Meteo fallback
        if (!img203 && !img204) {
          if (weather) {
            const monitoredSet = new Set(Object.keys(polygons));
            const fallback = generateOpenMeteoFallbackHexCells(weather, polygons, monitoredSet);
            if (!isCancelled) setRainRadarHexCells(fallback);
          }
          return;
        }

        // Offscreen canvas for pixel extraction
        const canvas = document.createElement('canvas');
        canvas.width = 256;
        canvas.height = 256;
        const ctx = canvas.getContext('2d', { willReadFrequently: true });

        let data203: ImageData | null = null;
        let data204: ImageData | null = null;

        if (img203 && ctx) {
          ctx.clearRect(0, 0, 256, 256);
          ctx.drawImage(img203, 0, 0);
          data203 = ctx.getImageData(0, 0, 256, 256);
        }

        if (img204 && ctx) {
          ctx.clearRect(0, 0, 256, 256);
          ctx.drawImage(img204, 0, 0);
          data204 = ctx.getImageData(0, 0, 256, 256);
        }

        const mappedCells = new Map<string, FloodHexCell>();
        const currentVerdicts = floodVerdictsRef.current;
        const currentCamerasMap = camerasByCellRef.current;

        // Sample each H3 cell using 7-point contraction sampling (Resolution 7 macro cells only)
        for (const [h3Id, pts] of Object.entries(polygons)) {
          if (h3Id.startsWith('89')) continue;
          if (!pts || pts.length < 3) continue;

          const points = getHexSamplingPoints(pts, 0.70);

          const sampleFn = (lat: number, lon: number) => {
            const { tileX, tileY, pixelX, pixelY } = latLonToTilePixel(lat, lon, 8);
            if (tileY !== 120) return null; // Outside HCMC latitude range

            let targetData: ImageData | null = null;
            if (tileX === 203) targetData = data203;
            else if (tileX === 204) targetData = data204;

            if (!targetData) return null;

            const idx = (pixelY * 256 + pixelX) * 4;
            return {
              r: targetData.data[idx],
              g: targetData.data[idx + 1],
              b: targetData.data[idx + 2],
              a: targetData.data[idx + 3],
            };
          };

          const rawIntensity = evaluateHexRadarIntensity(points, sampleFn);
          if (!rawIntensity) continue;

          // Surface Cross-Validation Gate:
          // Cross-reference airborne radar echoes against surface Open-Meteo precipitation
          // and ground camera observations (DRY vs WET / FLOODED).
          const cellCameras = currentCamerasMap.get(h3Id) || [];
          let activeGroundFloodCount = 0;
          if (currentVerdicts) {
            for (const cam of cellCameras) {
              const v = currentVerdicts.get(cam.id);
              if (v && !v.stale) {
                const s = (v.flood_severity || '').toUpperCase().trim();
                if (s === 'WET' || s === 'FLOODED_SHALLOW' || s === 'FLOODED_DEEP') {
                  activeGroundFloodCount++;
                }
              }
            }
          }

          // If weather gauge shows < 0.2mm rain & sunny/clear weather code,
          // and no ground cameras observe wet roads, the echo aloft is virga/high clouds.
          const isSurfaceDry =
            (!weather || (weather.rain_mm < 0.2 && weather.weather_code <= 3)) &&
            activeGroundFloodCount === 0;

          const finalSeverity = isSurfaceDry ? 'ADVISORY_CLOUD' : rawIntensity;

          mappedCells.set(h3Id, {
            h3_id: h3Id,
            resolution: 7,
            flood_severity: finalSeverity,
            camera_count: cellCameras.length,
            worst_camera: 'radar_rainviewer',
            worst_camera_name: 'Dự báo radar thời tiết',
            polygon: pts,
          });
        }

        if (!isCancelled) {
          setRainRadarHexCells(mappedCells);
        }
      } catch (err) {
        console.warn('Error sampling RainViewer radar onto H3 cells:', err);
        if (!isCancelled) {
          setError(err instanceof Error ? err.message : String(err));
          // Graceful fallback to Open-Meteo
          if (weather && cachedPolygons) {
            const fallback = generateOpenMeteoFallbackHexCells(
              weather,
              cachedPolygons,
              new Set(Object.keys(cachedPolygons))
            );
            setRainRadarHexCells(fallback);
          }
        }
      } finally {
        if (!isCancelled) {
          setLoading(false);
        }
      }
    }

    processRadar();

    return () => {
      isCancelled = true;
    };
  }, [radar?.latest_tile_template, weather]);

  return {
    rainRadarHexCells,
    loading,
    error,
  };
}
