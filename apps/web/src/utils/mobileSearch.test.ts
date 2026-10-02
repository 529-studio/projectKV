import test from 'node:test';
import assert from 'node:assert/strict';
import { MOBILE_SEARCH_I18N } from '../config/constants.ts';

test('MOBILE_SEARCH_I18N contains all required strings with proper Vietnamese diacritics', () => {
  // Verify all required keys exist
  assert.ok(MOBILE_SEARCH_I18N.PLACEHOLDER, 'PLACEHOLDER should exist');
  assert.ok(MOBILE_SEARCH_I18N.BADGE, 'BADGE should exist');
  assert.ok(MOBILE_SEARCH_I18N.DRAWER_TITLE, 'DRAWER_TITLE should exist');
  assert.ok(MOBILE_SEARCH_I18N.CLOSE_LABEL, 'CLOSE_LABEL should exist');
  assert.ok(MOBILE_SEARCH_I18N.TRIGGER_LABEL, 'TRIGGER_LABEL should exist');

  // Verify Vietnamese diacritics
  assert.equal(MOBILE_SEARCH_I18N.PLACEHOLDER.vi, 'Tìm camera, tuyến đường, phường...');
  assert.equal(MOBILE_SEARCH_I18N.BADGE.vi, 'Đường / Phường');
  assert.equal(MOBILE_SEARCH_I18N.DRAWER_TITLE.vi, 'Tìm & lọc camera');
  assert.equal(MOBILE_SEARCH_I18N.CLOSE_LABEL.vi, 'Bản đồ');
  assert.equal(MOBILE_SEARCH_I18N.TRIGGER_LABEL.vi, 'Tìm camera theo đường, phường');

  // Verify English translations
  assert.equal(MOBILE_SEARCH_I18N.PLACEHOLDER.en, 'Search cameras, roads, wards...');
  assert.equal(MOBILE_SEARCH_I18N.BADGE.en, 'Street / Ward');
  assert.equal(MOBILE_SEARCH_I18N.DRAWER_TITLE.en, 'Search & filter');
  assert.equal(MOBILE_SEARCH_I18N.CLOSE_LABEL.en, 'Map');
  assert.equal(MOBILE_SEARCH_I18N.TRIGGER_LABEL.en, 'Search cameras by street, ward');
});
