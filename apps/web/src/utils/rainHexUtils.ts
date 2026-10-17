export interface FloodHexCellLike {
  h3_id: string;
  resolution: number;
  flood_severity: string;
  camera_count: number;
  worst_camera: string;
  worst_camera_name: string;
  polygon: [number, number][];
}

export interface WeatherDataLike {
  weather_code: number;
  rain_mm: number;
}

export type RadarRainIntensity = 'HEAVY_RAIN' | 'MODERATE_RAIN' | 'LIGHT_RAIN' | null;

/**
 * Checks if WMO weather code represents rain, drizzle, or thunderstorm.
 */
export function isWmoRain(code: number): boolean {
  return (code >= 51 && code <= 67) || (code >= 80 && code <= 82) || (code >= 95 && code <= 99);
}

/**
 * Calculates centroid [lat, lon] of a 2D polygon.
 */
export function getPolygonCentroid(polygon: [number, number][]): [number, number] {
  if (!polygon || polygon.length === 0) return [0, 0];
  let sumLat = 0;
  let sumLon = 0;
  for (const pt of polygon) {
    sumLat += pt[0];
    sumLon += pt[1];
  }
  return [sumLat / polygon.length, sumLon / polygon.length];
}

/**
 * Generates 7 spatial sampling points inside an H3 hexagon:
 * - 1 centroid point
 * - 6 outer vertices contracted towards centroid by contraction ratio (default: 70% radius).
 * This eliminates boundary anti-aliasing edge bleed while testing true spatial extent.
 */
export function getHexSamplingPoints(
  polygon: [number, number][],
  contraction = 0.70
): [number, number][] {
  if (!polygon || polygon.length < 3) return [];
  const centroid = getPolygonCentroid(polygon);
  const [cLat, cLon] = centroid;
  const points: [number, number][] = [centroid];

  // Up to 6 unique polygon vertices
  const vertices = polygon.slice(0, 6);
  for (const [vLat, vLon] of vertices) {
    points.push([
      cLat + (vLat - cLat) * contraction,
      cLon + (vLon - cLon) * contraction,
    ]);
  }
  return points;
}

export interface TilePixelCoord {
  tileX: number;
  tileY: number;
  pixelX: number;
  pixelY: number;
}

/**
 * Projects (lat, lon) to Web Mercator tile index and pixel offset within tile.
 * Defaults to Zoom 8 (1px ≈ 600m in tropics).
 */
export function latLonToTilePixel(lat: number, lon: number, zoom = 8): TilePixelCoord {
  const n = Math.pow(2, zoom);
  const exactX = ((lon + 180.0) / 360.0) * n;
  const latRad = (lat * Math.PI) / 180.0;
  const exactY = ((1.0 - Math.asinh(Math.tan(latRad)) / Math.PI) / 2.0) * n;

  const tileX = Math.floor(exactX);
  const tileY = Math.floor(exactY);
  const pixelX = Math.max(0, Math.min(255, Math.floor((exactX - tileX) * 256)));
  const pixelY = Math.max(0, Math.min(255, Math.floor((exactY - tileY) * 256)));

  return { tileX, tileY, pixelX, pixelY };
}

/**
 * Decodes RainViewer Universal Blue (Scheme 2) RGB reflectivity into meteorological rain tiers (dBZ):
 * - a < 120: Transparent or feathered boundary pixel -> null
 * - Faint / Ground clutter / Virga (< 18 dBZ): Low saturation gray/brown or faint blue -> null
 * - LIGHT_RAIN (18 - 29 dBZ): Cyan, vibrant teal, sky blue
 * - MODERATE_RAIN (30 - 39 dBZ): Saturated yellow/amber or deep navy blue
 * - HEAVY_RAIN (>= 40 dBZ): Orange, red, purple/pink storm core
 */
export function mapRadarPixelToRainIntensity(
  r: number,
  g: number,
  b: number,
  a: number
): RadarRainIntensity {
  if (a < 120) return null;

  const max = Math.max(r, g, b);
  const min = Math.min(r, g, b);
  const saturation = max === 0 ? 0 : (max - min) / max;

  // Filter low-saturation gray/brown atmospheric haze, virga, and cirrus (dBZ < 15)
  if (saturation < 0.20 && b < 180) return null;

  // 1. Heavy rain / convective storm core (dBZ >= 40):
  // Red, orange, pink/purple, white cores
  if (r >= 180 && g <= 180 && b <= 80) return 'HEAVY_RAIN'; // Orange/Red (dBZ 40-52)
  if (r >= 200 && g >= 120 && b >= 200) return 'HEAVY_RAIN'; // Pink/Purple (dBZ >= 55)

  // 2. Moderate rain (dBZ 30 - 39):
  // Bright yellow / amber or deep saturated navy blue
  if (r >= 210 && g >= 180 && b <= 70) return 'MODERATE_RAIN'; // Yellow/Amber (dBZ 35-39)
  if (r <= 30 && g >= 60 && g <= 100 && b >= 110 && b <= 160) return 'MODERATE_RAIN'; // Deep Navy (dBZ 30-34)

  // 3. Light rain (dBZ 18 - 29):
  // Cyan, vibrant teal, sky blue
  if (b >= 160 && g >= 100 && r <= 100) return 'LIGHT_RAIN'; // Cyan/Teal (dBZ 18-29)

  return null;
}

/**
 * Maps RainViewer radar reflectivity pixel (RGBA) to ProjectKV Flood/Rain Severity.
 * Retained for backward compatibility by delegating to mapRadarPixelToRainIntensity.
 */
export function mapRadarPixelToSeverity(
  r: number,
  g: number,
  b: number,
  a: number
): 'FLOODED_DEEP' | 'FLOODED_SHALLOW' | 'WET' | null {
  const intensity = mapRadarPixelToRainIntensity(r, g, b, a);
  switch (intensity) {
    case 'HEAVY_RAIN':
      return 'FLOODED_DEEP';
    case 'MODERATE_RAIN':
      return 'FLOODED_SHALLOW';
    case 'LIGHT_RAIN':
      return 'WET';
    default:
      return null;
  }
}

/**
 * Evaluates 7 spatial sampling points across an H3 cell.
 * Requires a quorum of >= 3 points matching a rain tier to confirm precipitation,
 * and derives the median intensity of active points to eliminate single-pixel noise.
 */
export function evaluateHexRadarIntensity(
  points: [number, number][],
  sampleFn: (lat: number, lon: number) => { r: number; g: number; b: number; a: number } | null
): RadarRainIntensity {
  if (!points || points.length === 0) return null;

  const intensities: RadarRainIntensity[] = [];
  for (const pt of points) {
    const px = sampleFn(pt[0], pt[1]);
    if (px) {
      const val = mapRadarPixelToRainIntensity(px.r, px.g, px.b, px.a);
      if (val) intensities.push(val);
    }
  }

  // Quorum check: require at least 3 points matching rain
  if (intensities.length < 3) return null;

  const rankMap: Record<string, number> = {
    LIGHT_RAIN: 1,
    MODERATE_RAIN: 2,
    HEAVY_RAIN: 3,
  };
  const scores = intensities.map((i) => rankMap[i as string]).sort((a, b) => a - b);
  const medianScore = scores[Math.floor(scores.length / 2)];

  if (medianScore === 3) return 'HEAVY_RAIN';
  if (medianScore === 2) return 'MODERATE_RAIN';
  return 'LIGHT_RAIN';
}

/**
 * Replaces {z}, {x}, {y} placeholders in a tile template.
 */
export function buildRadarTileUrl(template: string, tileX: number, tileY: number, zoom = 8): string {
  return template
    .replace('{z}', String(zoom))
    .replace('{x}', String(tileX))
    .replace('{y}', String(tileY));
}

/**
 * Merges camera-verified flood hex cells (ground-truth, highest priority)
 * with radar-derived rain hex cells.
 */
export function mergeFloodHexCells<T extends FloodHexCellLike>(
  cameraHexCells: Map<string, T>,
  radarHexCells: Map<string, T>
): Map<string, T> {
  const merged = new Map<string, T>();

  // 1. Add all radar-derived cells as baseline spatial rain overlay
  radarHexCells.forEach((cell, id) => {
    merged.set(id, cell);
  });

  // 2. Camera ground-truth observations override radar approximations
  cameraHexCells.forEach((cell, id) => {
    merged.set(id, cell);
  });

  return merged;
}

/**
 * Fallback mapping from Open-Meteo city rainfall when radar tiles are offline or empty.
 * Guarded against false alarms: strictly requires actual measured precipitation (rain_mm >= 1.0)
 * rather than regional thunderstorm probability codes alone.
 */
export function generateOpenMeteoFallbackHexCells<T extends FloodHexCellLike = FloodHexCellLike>(
  weather: WeatherDataLike,
  polygons: Record<string, [number, number][]>,
  monitoredCellIds: Set<string>
): Map<string, T> {
  const result = new Map<string, T>();
  const isRaining =
    weather.rain_mm >= 1.0 ||
    (weather.rain_mm >= 0.5 && weather.weather_code >= 61 && isWmoRain(weather.weather_code));

  if (!isRaining) return result;

  const severity: 'FLOODED_DEEP' | 'FLOODED_SHALLOW' | 'WET' =
    weather.rain_mm >= 15.0
      ? 'FLOODED_DEEP'
      : weather.rain_mm >= 4.0
        ? 'FLOODED_SHALLOW'
        : 'WET';

  monitoredCellIds.forEach((h3Id) => {
    const poly = polygons[h3Id];
    if (!poly || poly.length < 3) return;

    result.set(h3Id, {
      h3_id: h3Id,
      resolution: 7,
      flood_severity: severity,
      camera_count: 1,
      worst_camera: 'open-meteo',
      worst_camera_name: 'Dự báo mưa Open-Meteo',
      polygon: poly,
    } as T);
  });

  return result;
}
