import assert from 'node:assert/strict';
import { test, describe } from 'node:test';
import {
  getPolygonCentroid,
  getHexSamplingPoints,
  latLonToTilePixel,
  mapRadarPixelToRainIntensity,
  mapRadarPixelToSeverity,
  evaluateHexRadarIntensity,
  buildRadarTileUrl,
  mergeFloodHexCells,
  generateOpenMeteoFallbackHexCells,
} from './rainHexUtils.ts';
import type { FloodHexCell } from './floodUtils.ts';
import type { WeatherData } from './weatherUtils.ts';

describe('Rain Hex Utils (Issue #106 / #120 Restoration & S8-T1 Pipeline Overhaul)', () => {
  const samplePoly: [number, number][] = [
    [10.0, 106.0],
    [10.0, 107.0],
    [11.0, 107.0],
    [11.0, 106.0],
  ];

  test('getPolygonCentroid calculates average lat and lon correctly', () => {
    const centroid = getPolygonCentroid(samplePoly);
    assert.equal(centroid[0], 10.5);
    assert.equal(centroid[1], 106.5);

    // Empty polygon fallback
    assert.deepEqual(getPolygonCentroid([]), [0, 0]);
  });

  test('getHexSamplingPoints returns 7 points with contraction', () => {
    const hexVertices: [number, number][] = [
      [10.8, 106.6],
      [10.85, 106.65],
      [10.85, 106.75],
      [10.8, 106.8],
      [10.75, 106.75],
      [10.75, 106.65],
    ];
    const points = getHexSamplingPoints(hexVertices, 0.7);
    assert.equal(points.length, 7);
    // First point is centroid
    const centroid = getPolygonCentroid(hexVertices);
    assert.equal(points[0][0], centroid[0]);
    assert.equal(points[0][1], centroid[1]);

    // Contracted point is between centroid and vertex
    const p1 = points[1];
    const expectedLat = centroid[0] + (hexVertices[0][0] - centroid[0]) * 0.7;
    const expectedLon = centroid[1] + (hexVertices[0][1] - centroid[1]) * 0.7;
    assert.ok(Math.abs(p1[0] - expectedLat) < 1e-6);
    assert.ok(Math.abs(p1[1] - expectedLon) < 1e-6);

    // Empty or small polygon fallback
    assert.deepEqual(getHexSamplingPoints([]), []);
  });

  test('latLonToTilePixel converts HCMC coordinates to zoom 7 tile and pixel offset by default', () => {
    // Defaults to Zoom 7 (RainViewer maximum supported tile zoom)
    const coordDefault = latLonToTilePixel(10.7769, 106.7009);
    assert.equal(coordDefault.tileX, 101);
    assert.equal(coordDefault.tileY, 60);
    assert.ok(coordDefault.pixelX >= 0 && coordDefault.pixelX <= 255);
    assert.ok(coordDefault.pixelY >= 0 && coordDefault.pixelY <= 255);

    // Explicit Zoom 8
    const coord8 = latLonToTilePixel(10.7769, 106.7009, 8);
    assert.equal(coord8.tileX, 203);
    assert.equal(coord8.tileY, 120);

    // Eastern HCMC / Thu Duc at Zoom 7 (Suoi Tien / Long Binh, lon >= 106.875)
    const coordThuDuc = latLonToTilePixel(10.86, 106.90, 7);
    assert.equal(coordThuDuc.tileX, 102);
    assert.equal(coordThuDuc.tileY, 60);
  });

  test('mapRadarPixelToRainIntensity decodes Universal Blue Scheme 2 dBZ palette', () => {
    // 1. Transparent or feathery border pixels (alpha < 120) -> null
    assert.equal(mapRadarPixelToRainIntensity(0, 0, 0, 0), null);
    assert.equal(mapRadarPixelToRainIntensity(0, 163, 224, 80), null);
    assert.equal(mapRadarPixelToRainIntensity(255, 68, 0, 119), null);

    // 2. Tan / brownish boundary mask and terrain clutter (r>180, g>180, b>120, a<200) -> null
    assert.equal(mapRadarPixelToRainIntensity(206, 192, 135, 150), null);
    assert.equal(mapRadarPixelToRainIntensity(214, 200, 143, 170), null);

    // 3. Low-saturation gray/brown atmospheric haze and virga (dBZ < 18) -> null
    // #827b69 (r=130, g=123, b=105, sat=0.19)
    assert.equal(mapRadarPixelToRainIntensity(130, 123, 105, 255), null);
    // #9e9375 (r=158, g=147, b=117, sat=0.26, b < 180) -> virga/faint haze
    assert.equal(mapRadarPixelToRainIntensity(100, 100, 100, 255), null);

    // 4. Light rain (18 - 29 dBZ): Cyan, vibrant teal, sky blue, medium blue
    // #36bae5 (r=54, g=186, b=229)
    assert.equal(mapRadarPixelToRainIntensity(54, 186, 229, 255), 'LIGHT_RAIN');
    // #00a3e0 (r=0, g=163, b=224)
    assert.equal(mapRadarPixelToRainIntensity(0, 163, 224, 255), 'LIGHT_RAIN');
    // #0077aa (r=0, g=119, b=170)
    assert.equal(mapRadarPixelToRainIntensity(0, 119, 170, 255), 'LIGHT_RAIN');

    // 5. Moderate rain (30 - 39 dBZ): Saturated yellow/amber or deep navy blue
    // #005588 deep navy blue (r=0, g=85, b=136)
    assert.equal(mapRadarPixelToRainIntensity(0, 85, 136, 255), 'MODERATE_RAIN');
    // #ffee00 bright yellow (r=255, g=238, b=0)
    assert.equal(mapRadarPixelToRainIntensity(255, 238, 0, 255), 'MODERATE_RAIN');
    // #ffc500 amber (r=255, g=197, b=0)
    assert.equal(mapRadarPixelToRainIntensity(255, 197, 0, 255), 'MODERATE_RAIN');

    // 6. Heavy rain (>= 40 dBZ): Orange, red, violet/pink storm core
    // #ffaa00 orange (r=255, g=170, b=0)
    assert.equal(mapRadarPixelToRainIntensity(255, 170, 0, 255), 'HEAVY_RAIN');
    // #ff4400 red-orange (r=255, g=68, b=0)
    assert.equal(mapRadarPixelToRainIntensity(255, 68, 0, 255), 'HEAVY_RAIN');
    // #c10000 crimson (r=193, g=0, b=0)
    assert.equal(mapRadarPixelToRainIntensity(193, 0, 0, 255), 'HEAVY_RAIN');
    // #ffaaff violet/pink core (r=255, g=170, b=255)
    assert.equal(mapRadarPixelToRainIntensity(255, 170, 255, 255), 'HEAVY_RAIN');
  });

  test('mapRadarPixelToSeverity preserves backward compatibility by delegating to rain intensity', () => {
    // Clear / clutter
    assert.equal(mapRadarPixelToSeverity(100, 100, 100, 255), null);
    assert.equal(mapRadarPixelToSeverity(0, 0, 0, 0), null);

    // Light rain -> WET
    assert.equal(mapRadarPixelToSeverity(0, 163, 224, 255), 'WET');

    // Moderate rain -> FLOODED_SHALLOW
    assert.equal(mapRadarPixelToSeverity(255, 238, 0, 255), 'FLOODED_SHALLOW');

    // Heavy rain -> FLOODED_DEEP
    assert.equal(mapRadarPixelToSeverity(255, 68, 0, 255), 'FLOODED_DEEP');
  });

  test('evaluateHexRadarIntensity enforces >= 3/7 quorum and median calculation', () => {
    const points: [number, number][] = [
      [10.0, 106.0],
      [10.1, 106.0],
      [10.2, 106.0],
      [10.3, 106.0],
      [10.4, 106.0],
      [10.5, 106.0],
      [10.6, 106.0],
    ];

    // Case 1: 0, 1, or 2 noisy points -> null (rejected by quorum)
    const noisyFn = (lat: number) => {
      if (lat === 10.0 || lat === 10.1) {
        return { r: 255, g: 68, b: 0, a: 255 }; // Heavy rain, but only 2 points
      }
      return { r: 0, g: 0, b: 0, a: 0 };
    };
    assert.equal(evaluateHexRadarIntensity(points, noisyFn), null);

    // Case 2: Exactly 3 points light rain -> LIGHT_RAIN
    const threeLightFn = (lat: number) => {
      if (lat <= 10.2) {
        return { r: 0, g: 163, b: 224, a: 255 }; // Light rain (3 points)
      }
      return null;
    };
    assert.equal(evaluateHexRadarIntensity(points, threeLightFn), 'LIGHT_RAIN');

    // Case 3: Mixed intensities (1 light, 2 moderate, 1 heavy) -> median is MODERATE_RAIN
    const mixedFn = (lat: number) => {
      if (lat === 10.0) return { r: 0, g: 163, b: 224, a: 255 }; // LIGHT (score 1)
      if (lat === 10.1) return { r: 255, g: 238, b: 0, a: 255 }; // MODERATE (score 2)
      if (lat === 10.2) return { r: 255, g: 238, b: 0, a: 255 }; // MODERATE (score 2)
      if (lat === 10.3) return { r: 255, g: 68, b: 0, a: 255 }; // HEAVY (score 3)
      return null;
    };
    assert.equal(evaluateHexRadarIntensity(points, mixedFn), 'MODERATE_RAIN');
  });

  test('buildRadarTileUrl properly substitutes {z}, {x}, {y}', () => {
    const template = 'https://tilecache.rainviewer.com/v2/radar/abc/256/{z}/{x}/{y}/2/1_1.png';
    const url = buildRadarTileUrl(template, 101, 60, 7);
    assert.equal(url, 'https://tilecache.rainviewer.com/v2/radar/abc/256/7/101/60/2/1_1.png');
  });

  test('mergeFloodHexCells prioritizes camera ground-truth observations over radar', () => {
    const cameraCells = new Map<string, FloodHexCell>();
    const radarCells = new Map<string, FloodHexCell>();

    const camCell: FloodHexCell = {
      h3_id: 'cell_overlap',
      resolution: 7,
      flood_severity: 'FLOODED_DEEP',
      camera_count: 3,
      worst_camera: 'cam_01',
      worst_camera_name: 'Cam 01',
      polygon: samplePoly,
    };

    const radarCellOverlap: FloodHexCell = {
      h3_id: 'cell_overlap',
      resolution: 7,
      flood_severity: 'WET',
      camera_count: 0,
      worst_camera: 'radar',
      worst_camera_name: 'Radar',
      polygon: samplePoly,
    };

    const radarCellOnly: FloodHexCell = {
      h3_id: 'cell_radar_only',
      resolution: 7,
      flood_severity: 'FLOODED_SHALLOW',
      camera_count: 0,
      worst_camera: 'radar',
      worst_camera_name: 'Radar',
      polygon: samplePoly,
    };

    cameraCells.set('cell_overlap', camCell);
    radarCells.set('cell_overlap', radarCellOverlap);
    radarCells.set('cell_radar_only', radarCellOnly);

    const merged = mergeFloodHexCells(cameraCells, radarCells);
    assert.equal(merged.size, 2);

    // Overlapping cell should retain camera verdict (FLOODED_DEEP instead of radar WET)
    assert.equal(merged.get('cell_overlap')?.flood_severity, 'FLOODED_DEEP');
    assert.equal(merged.get('cell_overlap')?.worst_camera, 'cam_01');

    // Radar-only cell should be preserved
    assert.equal(merged.get('cell_radar_only')?.flood_severity, 'FLOODED_SHALLOW');
  });

  test('generateOpenMeteoFallbackHexCells generates baseline wet cells when city has rain', () => {
    const weatherRain: WeatherData = {
      location: 'Ho Chi Minh City',
      latitude: 10.77,
      longitude: 106.70,
      temperature_c: 28,
      apparent_temperature_c: 32,
      humidity_percent: 85,
      weather_code: 61, // rain
      weather_desc_vi: 'Mưa vừa',
      weather_desc_en: 'Moderate rain',
      precipitation_probability_percent: 80,
      rain_mm: 5.5,
      source: 'open-meteo',
      hourly_forecast: [],
      updated_at: new Date().toISOString(),
    };

    const polygons = {
      cell_1: samplePoly,
      cell_2: samplePoly,
    };

    const monitoredCells = new Set(['cell_1', 'cell_2']);
    const result = generateOpenMeteoFallbackHexCells(weatherRain, polygons, monitoredCells);

    assert.equal(result.size, 2);
    assert.equal(result.get('cell_1')?.flood_severity, 'FLOODED_SHALLOW');

    // When dry
    const weatherDry: WeatherData = {
      ...weatherRain,
      weather_code: 0, // clear sky
      rain_mm: 0,
    };
    const dryResult = generateOpenMeteoFallbackHexCells(weatherDry, polygons, monitoredCells);
    assert.equal(dryResult.size, 0);

    // When minor trace rain / cloud moisture below threshold (rain_mm < 1.0 and cloudy weather code)
    const weatherTrace: WeatherData = {
      ...weatherRain,
      weather_code: 2, // partly cloudy
      rain_mm: 0.3,
    };
    const traceResult = generateOpenMeteoFallbackHexCells(weatherTrace, polygons, monitoredCells);
    assert.equal(traceResult.size, 0);
  });
});
