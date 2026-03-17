import test from 'node:test';
import assert from 'node:assert/strict';

import { buildStatusBadgeModel } from '../logs/table_render.js';

test('buildStatusBadgeModel uses status catalog labels when available', () => {
  const model = buildStatusBadgeModel('success', { SUCCESS: { label: '已完成' } });
  assert.equal(model.label, '已完成');
  assert.equal(model.statusCode, 'SUCCESS');
});
