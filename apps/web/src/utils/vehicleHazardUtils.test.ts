import assert from 'node:assert/strict';
import { test, describe } from 'node:test';
import {
  getVehicleProfile,
  evaluateVehicleHazard,
} from './vehicleHazardUtils.ts';
import { VEHICLE_PROFILES } from '../types/vehicle.ts';

describe('S17-T3 / Issue #510: Vehicle Clearance Profiles & Dynamic Hazard Evaluation', () => {
  test('Scenario 1: Profile retrieval and safe fallback', () => {
    assert.equal(getVehicleProfile('scooter').id, 'scooter');
    assert.equal(getVehicleProfile('motorbike').id, 'motorbike');
    assert.equal(getVehicleProfile('sedan').id, 'sedan');
    assert.equal(getVehicleProfile('suv').id, 'suv');

    // Unknown or null should fall back to default scooter
    assert.equal(getVehicleProfile('tank' as any).id, 'scooter');
    assert.equal(getVehicleProfile(null).id, 'scooter');
    assert.equal(getVehicleProfile(undefined).id, 'scooter');
  });

  test('Scenario 2: Dynamic threshold divergence at 22 cm flood depth', () => {
    // 22 cm flood:
    // - Scooter (impassable: 20cm) => IMPASSABLE (Red)
    // - Motorbike (caution: 15cm, impassable: 28cm) => CAUTION (Amber)
    // - Sedan (caution: 15cm, impassable: 25cm) => CAUTION (Amber)
    // - SUV (caution: 25cm, impassable: 40cm) => PASSABLE (Green)

    const depth = 22.0;

    const scooterEval = evaluateVehicleHazard({
      maxDepthCm: depth,
      vehicleType: 'scooter',
      hasVerifiedCameras: true,
    });
    assert.equal(scooterEval.verdict, 'IMPASSABLE');
    assert.equal(scooterEval.statusColor, '#ef4444');
    assert.equal(scooterEval.label.vi, 'NGUY CƠ CHẾT MÁY / THỦY KÍCH');

    const motorbikeEval = evaluateVehicleHazard({
      maxDepthCm: depth,
      vehicleType: 'motorbike',
      hasVerifiedCameras: true,
    });
    assert.equal(motorbikeEval.verdict, 'CAUTION');
    assert.equal(motorbikeEval.statusColor, '#eab308');
    assert.equal(motorbikeEval.label.vi, 'NƯỚC NGẬP MÉP BÁNH XE');

    const sedanEval = evaluateVehicleHazard({
      maxDepthCm: depth,
      vehicleType: 'sedan',
      hasVerifiedCameras: true,
    });
    assert.equal(sedanEval.verdict, 'CAUTION');
    assert.equal(sedanEval.statusColor, '#eab308');
    assert.equal(sedanEval.label.vi, 'CẢNH BÁO XE GẦM THẤP');

    const suvEval = evaluateVehicleHazard({
      maxDepthCm: depth,
      vehicleType: 'suv',
      hasVerifiedCameras: true,
    });
    assert.equal(suvEval.verdict, 'PASSABLE');
    assert.equal(suvEval.statusColor, '#22c55e');
    assert.equal(suvEval.label.vi, 'THÔNG THOÁNG');
  });

  test('Scenario 3: Deep flood divergence at 32 cm flood depth', () => {
    // 32 cm flood:
    // - Scooter, Motorbike, Sedan => IMPASSABLE
    // - SUV => CAUTION (between 25cm and 40cm)
    const depth = 32.0;

    const scooterEval = evaluateVehicleHazard({ maxDepthCm: depth, vehicleType: 'scooter' });
    const motorbikeEval = evaluateVehicleHazard({ maxDepthCm: depth, vehicleType: 'motorbike' });
    const sedanEval = evaluateVehicleHazard({ maxDepthCm: depth, vehicleType: 'sedan' });
    const suvEval = evaluateVehicleHazard({ maxDepthCm: depth, vehicleType: 'suv' });

    assert.equal(scooterEval.verdict, 'IMPASSABLE');
    assert.equal(motorbikeEval.verdict, 'IMPASSABLE');
    assert.equal(sedanEval.verdict, 'IMPASSABLE');
    assert.equal(suvEval.verdict, 'CAUTION');
    assert.equal(suvEval.label.vi, 'NƯỚC NGẬP NỬA BÁNH XE');
  });

  test('Scenario 4: Boundary values and null/zero depth handling', () => {
    const dryEval = evaluateVehicleHazard({
      maxDepthCm: 0,
      worstSeverity: 'CLEAR',
      vehicleType: 'scooter',
      hasVerifiedCameras: true,
    });
    assert.equal(dryEval.verdict, 'PASSABLE');
    assert.equal(dryEval.statusColor, '#22c55e');

    const nullEval = evaluateVehicleHazard({
      maxDepthCm: null,
      worstSeverity: 'CLEAR',
      vehicleType: 'scooter',
      hasVerifiedCameras: true,
    });
    assert.equal(nullEval.verdict, 'PASSABLE');

    const unverifiedEval = evaluateVehicleHazard({
      maxDepthCm: null,
      vehicleType: 'scooter',
      hasVerifiedCameras: false,
    });
    assert.equal(unverifiedEval.verdict, 'UNKNOWN');
    assert.equal(unverifiedEval.statusColor, '#71717a');
  });

  test('Scenario 5: Traffic congestion overrides dry pavement to CAUTION', () => {
    const trafficEval = evaluateVehicleHazard({
      maxDepthCm: 0,
      worstSeverity: 'CONGESTED',
      vehicleType: 'suv',
      hasVerifiedCameras: true,
    });
    assert.equal(trafficEval.verdict, 'CAUTION');
    assert.equal(trafficEval.statusColor, '#eab308');
  });
});
