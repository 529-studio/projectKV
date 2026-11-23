import { describe, it } from 'node:test';
import assert from 'node:assert/strict';
import {
  projectPointOntoPolyline,
  calculateCheckpointChainage,
  generateNavigationAlerts,
  formatAlertDistance,
} from './navigationAlertUtils.ts';
import type { CheckpointCamera } from '../types/corridor.ts';

describe('navigationAlertUtils', () => {
  // Simple straight polyline from west to east along latitude 10.75:
  // (lon 106.60 -> lon 106.70)
  const straightPolyline: [number, number][] = [
    [106.60, 10.75],
    [106.65, 10.75],
    [106.70, 10.75],
  ];

  describe('projectPointOntoPolyline', () => {
    it('projects a point directly on the polyline segment accurately', () => {
      // Point exactly halfway on the first segment
      const point: [number, number] = [106.625, 10.75]; // lon, lat
      const projection = projectPointOntoPolyline(point, straightPolyline);

      assert.ok(projection !== null);
      assert.strictEqual(projection.segmentIndex, 0);
      assert.ok(Math.abs(projection.t - 0.5) < 0.01);
      assert.ok(projection.orthogonalDistanceMeters < 1);
    });

    it('projects an offset point orthogonally onto the nearest segment', () => {
      // Point slightly north of segment 1 (lon 106.67, lat 10.751)
      const point: [number, number] = [106.67, 10.751];
      const projection = projectPointOntoPolyline(point, straightPolyline);

      assert.ok(projection !== null);
      assert.strictEqual(projection.segmentIndex, 1);
      // Offset of 0.001 deg lat is ~111 meters
      assert.ok(projection.orthogonalDistanceMeters > 100);
      assert.ok(projection.orthogonalDistanceMeters < 120);
    });

    it('clamps points before polyline start or after polyline end', () => {
      const beforeStart: [number, number] = [106.58, 10.75];
      const projStart = projectPointOntoPolyline(beforeStart, straightPolyline);
      assert.ok(projStart !== null);
      assert.strictEqual(projStart.segmentIndex, 0);
      assert.strictEqual(projStart.t, 0);

      const afterEnd: [number, number] = [106.72, 10.75];
      const projEnd = projectPointOntoPolyline(afterEnd, straightPolyline);
      assert.ok(projEnd !== null);
      assert.strictEqual(projEnd.segmentIndex, 1);
      assert.strictEqual(projEnd.t, 1);
    });

    it('returns null for degenerate polylines with fewer than 2 points', () => {
      assert.strictEqual(projectPointOntoPolyline([106.65, 10.75], []), null);
      assert.strictEqual(projectPointOntoPolyline([106.65, 10.75], [[106.65, 10.75]]), null);
    });
  });

  describe('calculateCheckpointChainage', () => {
    it('calculates strictly increasing cumulative distance along the polyline', () => {
      const p1: [number, number] = [106.62, 10.75];
      const p2: [number, number] = [106.65, 10.75];
      const p3: [number, number] = [106.68, 10.75];

      const chainage1 = calculateCheckpointChainage(p1, straightPolyline);
      const chainage2 = calculateCheckpointChainage(p2, straightPolyline);
      const chainage3 = calculateCheckpointChainage(p3, straightPolyline);

      assert.ok(chainage1 > 0);
      assert.ok(chainage2 > chainage1);
      assert.ok(chainage3 > chainage2);
    });
  });

  describe('formatAlertDistance', () => {
    it('formats meters under 1000m to nearest 10m', () => {
      assert.deepStrictEqual(formatAlertDistance(432), { vi: '430 m', en: '430 m' });
      assert.deepStrictEqual(formatAlertDistance(85), { vi: '90 m', en: '90 m' });
    });

    it('formats distances >= 1000m to 1 decimal place with locale separator', () => {
      assert.deepStrictEqual(formatAlertDistance(1450), { vi: '1,5 km', en: '1.5 km' });
      assert.deepStrictEqual(formatAlertDistance(2000), { vi: '2,0 km', en: '2.0 km' });
    });
  });

  describe('generateNavigationAlerts', () => {
    const mockCheckpoints: CheckpointCamera[] = [
      {
        camera_id: 'cam-end',
        name: 'Giao lộ Xa lộ Hà Nội',
        severity: 'CLEAR',
        flood_severity: 'DRY',
        flood_depth_cm: 0,
        stale: false,
        coordinates: [106.69, 10.75], // near end
      },
      {
        camera_id: 'cam-flood',
        name: 'Ngã tư Cống Quỳnh',
        severity: 'HEAVY',
        flood_severity: 'FLOODED_DEEP',
        flood_depth_cm: 25,
        stale: false,
        coordinates: [106.63, 10.75], // near start
      },
      {
        camera_id: 'cam-caution',
        name: 'Ngã tư Nguyễn Thái Học',
        severity: 'MODERATE',
        flood_severity: 'FLOODED_SHALLOW',
        flood_depth_cm: 12,
        stale: false,
        coordinates: [106.66, 10.75], // middle
      },
    ];

    it('orders alerts sequentially along route and applies vehicle clearance limits for scooter (stall 15cm)', () => {
      const alerts = generateNavigationAlerts({
        polylineCoordinates: straightPolyline,
        checkpoints: mockCheckpoints,
        vehicleType: 'scooter',
        userPosition: [106.60, 10.75], // at start of route
      });

      assert.strictEqual(alerts.length, 3);
      // Verify sequential order along route: cam-flood (lon 106.63) -> cam-caution (106.66) -> cam-end (106.69)
      assert.strictEqual(alerts[0].id, 'cam-flood');
      assert.strictEqual(alerts[1].id, 'cam-caution');
      assert.strictEqual(alerts[2].id, 'cam-end');

      // 25cm flood on scooter (stall limit 20cm) must be IMPASSABLE
      assert.strictEqual(alerts[0].severity, 'IMPASSABLE');
      assert.strictEqual(alerts[0].excessDepthCm, 5); // 25 - 20 = 5cm
      assert.ok(alerts[0].actionableAdvice.vi.includes('Nguy cơ chết máy'));
      assert.ok(alerts[0].actionableAdvice.en.includes('Engine stall hazard'));

      // 12cm flood on scooter (caution limit 10cm, stall limit 15cm) must be CAUTION
      assert.strictEqual(alerts[1].severity, 'CAUTION');
      assert.ok(alerts[1].actionableAdvice.vi.includes('Cảnh báo'));

      // 0cm flood must be PASSABLE
      assert.strictEqual(alerts[2].severity, 'PASSABLE');
    });

    it('adapts verdicts when vehicle is SUV (stall limit 35cm, caution limit 25cm)', () => {
      const alerts = generateNavigationAlerts({
        polylineCoordinates: straightPolyline,
        checkpoints: mockCheckpoints,
        vehicleType: 'suv',
        userPosition: [106.60, 10.75],
      });

      // 25cm flood for SUV is CAUTION (not IMPASSABLE like for scooter)
      assert.strictEqual(alerts[0].severity, 'CAUTION');
      assert.strictEqual(alerts[0].excessDepthCm, null);
    });

    it('flags checkpoints as passed when user position is ahead of them', () => {
      const alerts = generateNavigationAlerts({
        polylineCoordinates: straightPolyline,
        checkpoints: mockCheckpoints,
        vehicleType: 'scooter',
        userPosition: [106.64, 10.75], // past cam-flood (lon 106.63)
      });

      assert.strictEqual(alerts[0].isAhead, false);
      assert.strictEqual(alerts[1].isAhead, true);
      assert.strictEqual(alerts[2].isAhead, true);
    });

    it('supports reversed route traversal when traveling opposite direction', () => {
      const alerts = generateNavigationAlerts({
        polylineCoordinates: straightPolyline,
        checkpoints: mockCheckpoints,
        vehicleType: 'scooter',
        userPosition: [106.70, 10.75], // start from east
        reverseDirection: true,
      });

      // When reversed, cam-end is reached first
      assert.strictEqual(alerts[0].id, 'cam-end');
      assert.strictEqual(alerts[1].id, 'cam-caution');
      assert.strictEqual(alerts[2].id, 'cam-flood');
    });
  });
});
