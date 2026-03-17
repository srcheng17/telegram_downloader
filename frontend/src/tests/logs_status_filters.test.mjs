import test from 'node:test';
import assert from 'node:assert/strict';

import { applyStatusCatalog } from '../logs/status_filters.js';

test('applyStatusCatalog normalizes status options', () => {
  const catalog = applyStatusCatalog({ success: { label: '已完成', can_download: true } });
  assert.equal(catalog.SUCCESS.can_download, true);
  assert.equal(catalog.SUCCESS.label, '已完成');
});
