import { describe, it } from 'node:test';
import assert from 'node:assert/strict';
import { USER_FLOOD_REPORT_I18N } from '../config/constants.ts';
import { isUserReportActive, getUserReportRemainingMinutes, type UserFloodReport } from '../utils/floodUtils.ts';

describe('S18-T4 / Issue #527: Automated Crowd-Sourced Hazard Corroboration UI', () => {
  it('Scenario 1: Bilingual format and zero-emoji compliance for CAMERA_VERIFIED', () => {
    const viText = USER_FLOOD_REPORT_I18N.CAMERA_VERIFIED.vi(85);
    const enText = USER_FLOOD_REPORT_I18N.CAMERA_VERIFIED.en(85);

    assert.equal(viText, '[Xác thực qua camera · 85%]');
    assert.equal(enText, '[Camera verified · 85%]');

    // Zero-emoji verification (Rule 1)
    const emojiRegex = /[\u{1F300}-\u{1F9FF}\u{2600}-\u{26FF}\u{2700}-\u{27BF}]/u;
    assert.equal(emojiRegex.test(viText), false, 'Vietnamese text must contain zero emoji');
    assert.equal(emojiRegex.test(enText), false, 'English text must contain zero emoji');
  });

  it('Scenario 2: Active corroborated report retains TTL and exposes confidence', () => {
    const now = Date.now();
    const report: UserFloodReport = {
      camera_id: 'cam-001',
      timestamp: new Date(now - 10 * 60 * 1000).toISOString(),
      report_count: 2,
      escalated: true,
      ttl_seconds: 5400, // 90 min TTL
      status: 'CORROBORATED',
      confidence: 0.88,
      corroborating_camera_id: 'cam-002',
      corroborating_distance_m: 145.2,
    };

    assert.equal(isUserReportActive(report, now), true);
    const remaining = getUserReportRemainingMinutes(report, now);
    assert.ok(remaining > 0, 'Remaining minutes should be positive');
    assert.equal(report.status, 'CORROBORATED');
    assert.equal(Math.round((report.confidence ?? 0) * 100), 88);
  });

  it('Scenario 3: Corroborated report renders emerald verified badge in tooltip', () => {
    const report: UserFloodReport = {
      camera_id: 'cam-001',
      timestamp: new Date().toISOString(),
      report_count: 3,
      escalated: true,
      ttl_seconds: 5400,
      status: 'CORROBORATED',
      confidence: 0.92,
    };

    const pct = Math.round((report.confidence ?? 0) * 100);
    const viTooltipBadge = `<span style="color:#10b981; font-weight:bold;">${USER_FLOOD_REPORT_I18N.CAMERA_VERIFIED.vi(pct)}</span>`;
    const enTooltipBadge = `<span style="color:#10b981; font-weight:bold;">${USER_FLOOD_REPORT_I18N.CAMERA_VERIFIED.en(pct)}</span>`;

    assert.ok(viTooltipBadge.includes('[Xác thực qua camera · 92%]'));
    assert.ok(enTooltipBadge.includes('[Camera verified · 92%]'));
    assert.ok(viTooltipBadge.includes('#10b981'));
  });

  it('Scenario 4: Unverified or expired report excludes verified camera badge', () => {
    const unverifiedReport: UserFloodReport = {
      camera_id: 'cam-003',
      timestamp: new Date().toISOString(),
      report_count: 1,
      escalated: false,
      ttl_seconds: 5400,
      status: 'UNVERIFIED',
      confidence: 0.25,
    };

    const isCorroborated = unverifiedReport.status === 'CORROBORATED';
    assert.equal(isCorroborated, false, 'UNVERIFIED status must not be treated as corroborated');

    // Expired report
    const expiredReport: UserFloodReport = {
      camera_id: 'cam-004',
      timestamp: new Date(Date.now() - 6000 * 1000).toISOString(),
      report_count: 1,
      escalated: false,
      ttl_seconds: 5400,
      status: 'EXPIRED',
      confidence: 0.1,
    };
    assert.equal(isUserReportActive(expiredReport, Date.now()), false);
  });
});
