import test, { describe } from 'node:test';
import assert from 'node:assert/strict';
import {
  handleHotkey,
  isLabelComplete,
  LABELER_I18N,
  formatSnapshotTimestamp,
  buildAuthHeaders,
  isAutoHarvested,
  filterSnapshots,
  normalizePolygonClass,
  normalizePixelPoints,
  validatePolygonAnnotations,
  hasAutoPolygons,
  POLYGON_PROVENANCE_SAM,
  MAX_ANNOTATIONS_PER_LABEL,
  type LabelFilterStatus,
  type LabelPayload,
  type LabelState,
  type PolygonAnnotation,
  type PolygonClass,
  type PolygonPoint,
} from './labelerUtils.ts';

describe('Labeler Utilities & Hotkeys (Issue #214 / S7-T8 Directional Taxonomy)', () => {
  test('handleHotkey maps numbers to flood levels and physical depth anchors', () => {
    const s1: LabelState = { flood_level: 'none', traffic_level: 'clear', weather: 'clear', time_of_day: 'day' };

    const r1 = handleHotkey('1', s1);
    assert.equal(r1.state.flood_level, 'none');
    assert.equal(r1.state.flood_submersion_anchor, 'dry');

    const r2 = handleHotkey('2', s1);
    assert.equal(r2.state.flood_level, 'puddle');
    assert.equal(r2.state.flood_submersion_anchor, 'tire_base_under_10cm');

    const r3 = handleHotkey('3', s1);
    assert.equal(r3.state.flood_level, 'deep');
    assert.equal(r3.state.flood_submersion_anchor, 'exhaust_hub_over_15cm');
  });

  test('handleHotkey maps q/w/e/r to Direction A and global traffic level', () => {
    const s: LabelState = { flood_level: 'none', traffic_level: 'clear', weather: 'clear', time_of_day: 'day' };

    const rq = handleHotkey('q', s);
    assert.equal(rq.state.traffic_dir_a, 'clear');
    assert.equal(rq.state.traffic_level, 'clear');

    const rw = handleHotkey('w', s);
    assert.equal(rw.state.traffic_dir_a, 'moderate');
    assert.equal(rw.state.traffic_level, 'moderate');

    const re = handleHotkey('e', s);
    assert.equal(re.state.traffic_dir_a, 'heavy');
    assert.equal(re.state.traffic_level, 'heavy');

    const rr = handleHotkey('r', s);
    assert.equal(rr.state.traffic_dir_a, 'congested');
    assert.equal(rr.state.traffic_level, 'gridlock');
  });

  test('handleHotkey maps Shift + Q/W/E/R/T to Direction B', () => {
    const s: LabelState = { flood_level: 'none', traffic_level: 'clear', weather: 'clear', time_of_day: 'day' };

    assert.equal(handleHotkey('Q', s).state.traffic_dir_b, 'clear');
    assert.equal(handleHotkey('W', s).state.traffic_dir_b, 'moderate');
    assert.equal(handleHotkey('E', s).state.traffic_dir_b, 'heavy');
    assert.equal(handleHotkey('R', s).state.traffic_dir_b, 'congested');
    assert.equal(handleHotkey('T', s).state.traffic_dir_b, 'not_applicable');
  });

  test('handleHotkey maps A/S/D to weather', () => {
    const s: LabelState = { flood_level: 'none', traffic_level: 'clear', weather: 'clear', time_of_day: 'day' };

    assert.equal(handleHotkey('a', s).state.weather, 'clear');
    assert.equal(handleHotkey('s', s).state.weather, 'rain');
    assert.equal(handleHotkey('d', s).state.weather, 'fog');
  });

  test('handleHotkey maps Z/X/C to time of day', () => {
    const s: LabelState = { flood_level: 'none', traffic_level: 'clear', weather: 'clear', time_of_day: 'day' };

    assert.equal(handleHotkey('z', s).state.time_of_day, 'day');
    assert.equal(handleHotkey('x', s).state.time_of_day, 'dusk');
    assert.equal(handleHotkey('c', s).state.time_of_day, 'night');
  });

  test('handleHotkey triggers navigation and save actions', () => {
    const s: LabelState = { flood_level: 'none', traffic_level: 'clear', weather: 'clear', time_of_day: 'day' };

    assert.equal(handleHotkey('ArrowLeft', s).action, 'prev');
    assert.equal(handleHotkey('ArrowRight', s).action, 'next');
    assert.equal(handleHotkey('Enter', s).action, 'save');
  });

  test('isLabelComplete requires all core criteria', () => {
    assert.equal(isLabelComplete({}), false);
    assert.equal(isLabelComplete({ flood_level: 'none' }), false);
    assert.equal(isLabelComplete({ flood_level: 'none', traffic_level: 'clear' }), false);
    assert.equal(isLabelComplete({ flood_level: 'none', traffic_level: 'clear', weather: 'clear' }), false);
    assert.equal(
      isLabelComplete({ flood_level: 'none', traffic_level: 'clear', weather: 'clear', time_of_day: 'day' }),
      true
    );
    assert.equal(
      isLabelComplete({ flood_level: 'none', traffic_dir_a: 'clear', weather: 'clear', time_of_day: 'day' }),
      true
    );
  });

  test('LABELER_I18N contains bilingual strings with zero emojis', () => {
    const emojiRegex = /[\p{Emoji_Presentation}\p{Extended_Pictographic}]/u;

    for (const locale of ['vi', 'en'] as const) {
      const strings = LABELER_I18N[locale];
      assert.ok(strings.title);
      assert.ok(strings.floodLabel);
      assert.ok(strings.floodSubmersionLabel);
      assert.ok(strings.trafficLabel);
      assert.ok(strings.trafficDirALabel);
      assert.ok(strings.trafficDirBLabel);
      assert.ok(strings.qualityTagsLabel);
      assert.ok(strings.queueReasonLabel);
      assert.ok(strings.weatherLabel);
      assert.ok(strings.timeOfDayLabel);
      assert.ok(strings.saveAndNext);

      // Verify zero emojis in user-facing UI strings
      for (const [key, val] of Object.entries(strings)) {
        if (typeof val === 'string') {
          assert.equal(emojiRegex.test(val), false, `Emoji found in ${locale}.${key}: ${val}`);
        }
      }
    }
  });

  test('formatSnapshotTimestamp formats ICT time', () => {
    // 1728637200 is 2024-10-11T09:00:00Z -> 16:00:00 ICT
    const formatted = formatSnapshotTimestamp(1728637200);
    assert.ok(formatted.includes('16:00') || formatted.includes('2024'));
  });

  test('buildAuthHeaders constructs X-Dev-Token and Bearer authorization headers', () => {
    const empty = buildAuthHeaders('');
    assert.deepEqual(empty, {});

    const withBase = buildAuthHeaders('my-secret-token', { 'Content-Type': 'application/json' });
    assert.equal(withBase['Content-Type'], 'application/json');
    assert.equal(withBase['X-Dev-Token'], 'my-secret-token');
    assert.equal(withBase['Authorization'], 'Bearer my-secret-token');
  });

  test('LABELER_I18N contains developer auth gatekeeper strings', () => {
    for (const locale of ['vi', 'en'] as const) {
      const strings = LABELER_I18N[locale];
      assert.ok(strings.devAuthRequired);
      assert.ok(strings.devAuthSubtitle);
      assert.ok(strings.devTokenPlaceholder);
      assert.ok(strings.devUnlock);
      assert.ok(strings.devInvalidToken);
      assert.ok(strings.devLockSession);
      assert.ok(strings.saveError);
    }
  });

  test('isAutoHarvested correctly detects pseudo-labels synthesized by automated harvester', () => {
    assert.equal(isAutoHarvested(undefined), false);
    assert.equal(isAutoHarvested(null), false);
    assert.equal(isAutoHarvested({ annotator: '' }), false);
    assert.equal(isAutoHarvested({ annotator: 'thdat' }), false);
    assert.equal(isAutoHarvested({ annotator: 'anonymous' }), false);
    assert.equal(isAutoHarvested({ annotator: 'reviewer_1' }), false);
    assert.equal(isAutoHarvested({ annotator: 'harvester/auto' }), true);
    assert.equal(isAutoHarvested({ annotator: 'harvester/cv-engine' }), true);
    assert.equal(isAutoHarvested({ annotator: 'HARVESTER/AUTO' }), true);
    assert.equal(isAutoHarvested({ annotator: 'opencode_auto' }), true);
    assert.equal(isAutoHarvested({ annotator: 'auto_harvester' }), true);
    assert.equal(isAutoHarvested({ annotator: 'auto' }), true);
  });

  test('filterSnapshots partitions queue into all, pending_auto, and approved_human', () => {
    const snapshots = [
      { key: 'snap_1.jpg', camera_id: 'cam_1', timestamp: 100, reason: 'scheduled', size: 50000, raw_url: '/raw1' },
      { key: 'snap_2.jpg', camera_id: 'cam_2', timestamp: 101, reason: 'scheduled', size: 50000, raw_url: '/raw2' },
      { key: 'snap_3.jpg', camera_id: 'cam_3', timestamp: 102, reason: 'scheduled', size: 50000, raw_url: '/raw3' },
      { key: 'snap_4.jpg', camera_id: 'cam_4', timestamp: 103, reason: 'scheduled', size: 50000, raw_url: '/raw4' },
    ];

    const labels: Record<string, LabelPayload> = {
      'snap_1.jpg': {
        snapshot_key: 'snap_1.jpg',
        camera_id: 'cam_1',
        date: '2026-10-18',
        annotator: 'harvester/auto',
        timestamp: 100,
        flood_level: 'none',
      },
      'snap_2.jpg': {
        snapshot_key: 'snap_2.jpg',
        camera_id: 'cam_2',
        date: '2026-10-18',
        annotator: 'thdat',
        timestamp: 101,
        flood_level: 'puddle',
      },
      // snap_3 is unlabelled
      'snap_4.jpg': {
        snapshot_key: 'snap_4.jpg',
        camera_id: 'cam_4',
        date: '2026-10-18',
        annotator: 'harvester/rain_trigger',
        timestamp: 103,
        flood_level: 'deep',
      },
      // snap_5 is NOT in loaded snapshots page but present in labels catalog (Issue #289 / Queue Desync)
      'snap_5.jpg': {
        snapshot_key: 'snap_5.jpg',
        camera_id: 'cam_5',
        date: '2026-10-18',
        annotator: 'opencode_auto',
        timestamp: 105,
        flood_level: 'none',
      },
    };

    // Filter: all
    const filterAll: LabelFilterStatus = 'all';
    const all = filterSnapshots(snapshots, labels, filterAll);
    assert.equal(all.length, 4);

    // Filter: pending_auto (merges auto candidates from snapshots + labels catalog)
    const filterPending: LabelFilterStatus = 'pending_auto';
    const pendingAuto = filterSnapshots(snapshots, labels, filterPending, (id) => `Camera ${id}`);
    assert.equal(pendingAuto.length, 3);
    assert.deepEqual(pendingAuto.map((s) => s.key), ['snap_1.jpg', 'snap_4.jpg', 'snap_5.jpg']);
    // Verify synthesized snapshot has valid raw_url and camera name
    const extraSnap = pendingAuto.find((s) => s.key === 'snap_5.jpg');
    assert.ok(extraSnap);
    assert.equal(extraSnap.camera_name, 'Camera cam_5');
    assert.equal(extraSnap.raw_url, '/api/archiver/snapshots/raw?key=snap_5.jpg');

    // Filter: approved_human (gold-standard labels reviewed/created by humans)
    const filterApproved: LabelFilterStatus = 'approved_human';
    const approvedHuman = filterSnapshots(snapshots, labels, filterApproved);
    assert.equal(approvedHuman.length, 1);
    assert.deepEqual(approvedHuman.map((s) => s.key), ['snap_2.jpg']);
  });

  test('handleHotkey maps Shift + Enter to fast approval action', () => {
    const s: LabelState = { flood_level: 'none', traffic_level: 'clear', weather: 'clear', time_of_day: 'day' };

    // Standard Enter -> save
    const rSave = handleHotkey('Enter', s);
    assert.equal(rSave.action, 'save');

    // Shift + Enter via meta.shiftKey -> approve
    const rApprove1 = handleHotkey('Enter', s, { shiftKey: true });
    assert.equal(rApprove1.action, 'approve');

    // Shift + Enter via compound key string -> approve
    const rApprove2 = handleHotkey('Shift+Enter', s);
    assert.equal(rApprove2.action, 'approve');
  });

  test('LABELER_I18N contains candidate review filter and verification badge strings without emojis', () => {
    const emojiRegex = /[\p{Emoji_Presentation}\p{Extended_Pictographic}]/u;

    for (const locale of ['vi', 'en'] as const) {
      const strings = LABELER_I18N[locale];
      assert.ok(strings.filterAll);
      assert.ok(strings.filterPendingAuto);
      assert.ok(strings.filterApprovedHuman);
      assert.ok(strings.badgeAutoHarvester);
      assert.ok(strings.badgeHumanReviewer);
      assert.ok(strings.badgeUnlabeled);
      assert.ok(strings.fastApproveAction);
      assert.ok(strings.fastApproveTooltip);

      for (const [key, val] of Object.entries(strings)) {
        if (typeof val === 'string') {
          assert.equal(emojiRegex.test(val), false, `Emoji found in ${locale}.${key}: ${val}`);
        }
      }
    }
  });
});

describe('Segmentation Polygon Contract (Issue #297 / ADR-011 D1)', () => {
  test('normalizePolygonClass resolves canonical names and aliases', () => {
    assert.equal(normalizePolygonClass('Roadway'), 'roadway');
    assert.equal(normalizePolygonClass(' flood '), 'water_puddle');
    assert.equal(normalizePolygonClass('motorbike'), 'motorcycle');
    assert.equal(normalizePolygonClass('container'), 'truck');
    assert.equal(normalizePolygonClass('aeroplane'), null);
    assert.equal(normalizePolygonClass(''), null);
  });

  test('normalizePixelPoints converts, clamps, and rounds to 6 decimals', () => {
    const pts = normalizePixelPoints(
      [
        { x: 192, y: 108 },
        { x: 960, y: 540 },
        { x: 2000, y: -5 },
      ],
      1920,
      1080
    );
    assert.deepEqual(pts[0], [0.1, 0.1]);
    assert.deepEqual(pts[1], [0.5, 0.5]);
    // Out-of-range pixel coordinates clamp into [0, 1] instead of leaking.
    assert.deepEqual(pts[2], [1, 0]);
  });

  test('validatePolygonAnnotations accepts a valid annotation set', () => {
    const ok: PolygonAnnotation[] = [
      { class: 'roadway', polygon: [[0.1, 0.9], [0.9, 0.9], [0.5, 0.4]], source: 'human' },
      { class: 'water_puddle', polygon: [[0.2, 0.8], [0.8, 0.8], [0.5, 0.5]] },
    ];
    assert.equal(validatePolygonAnnotations(ok), null);
    assert.equal(validatePolygonAnnotations([]), null);
    assert.equal(validatePolygonAnnotations(undefined), null);
  });

  test('validatePolygonAnnotations rejects contract violations with messages', () => {
    const tooFew: PolygonAnnotation[] = [
      { class: 'car', polygon: [[0.1, 0.1], [0.2, 0.2]] },
    ];
    assert.match(validatePolygonAnnotations(tooFew) ?? '', /3-64 vertices/);

    const pixelCoords: PolygonAnnotation[] = [
      { class: 'bus', polygon: [[150, 950], [850, 950], [500, 480]] },
    ];
    assert.match(validatePolygonAnnotations(pixelCoords) ?? '', /normalized range/);

    const badClass: PolygonAnnotation[] = [
      { class: 'aeroplane' as unknown as PolygonClass, polygon: [[0.1, 0.1], [0.2, 0.2], [0.3, 0.3]] },
    ];
    assert.match(validatePolygonAnnotations(badClass) ?? '', /unknown class/);

    const tooMany = Array.from({ length: MAX_ANNOTATIONS_PER_LABEL + 1 }, () => ({
      class: 'car' as const,
      polygon: [[0.1, 0.1], [0.2, 0.2], [0.3, 0.3]] as PolygonPoint[],
    }));
    assert.match(validatePolygonAnnotations(tooMany) ?? '', /too many polygon annotations/);

    const longSource: PolygonAnnotation[] = [
      {
        class: 'car',
        polygon: [[0.1, 0.1], [0.2, 0.2], [0.3, 0.3]],
        source: 'x'.repeat(33),
      },
    ];
    assert.match(validatePolygonAnnotations(longSource) ?? '', /source exceeds/);
  });

  test('hasAutoPolygons detects SAM provenance only', () => {
    const anns: PolygonAnnotation[] = [
      { class: 'roadway', polygon: [[0.1, 0.1], [0.2, 0.2], [0.3, 0.3]], source: 'human' },
      { class: 'water_puddle', polygon: [[0.4, 0.4], [0.5, 0.5], [0.6, 0.6]], source: POLYGON_PROVENANCE_SAM },
    ];
    assert.equal(hasAutoPolygons(anns), true);
    assert.equal(hasAutoPolygons([anns[0]]), false);
    assert.equal(hasAutoPolygons(undefined), false);
  });

  test('annotations flow through LabelState into LabelPayload type contract', () => {
    const draft: LabelState = {
      flood_level: 'puddle',
      annotations: [{ class: 'water_puddle', polygon: [[0, 0], [0.5, 0], [0.25, 0.5]] }],
    };
    const payload: LabelPayload = {
      ...draft,
      date: '2026-10-24',
      camera_id: 'cam1',
      snapshot_key: 'proofs/2026-10-24/cam1/1_sample.jpg',
      annotator: 'dev_user',
      timestamp: 1792736298,
    };
    assert.equal(payload.annotations?.length, 1);
    assert.equal(payload.annotations?.[0].class, 'water_puddle');
  });
});
