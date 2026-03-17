import test from 'node:test';
import assert from 'node:assert/strict';

import { getStatusMeta } from '../shared/models/status_catalog.js';

test('getStatusMeta normalizes uppercase status lookup', () => {
  const meta = getStatusMeta({ success: { label: '已完成', can_download: true } }, ' success ');
  assert.equal(meta.label, '已完成');
  assert.equal(meta.can_download, true);
});
