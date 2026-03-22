import test from 'node:test';
import assert from 'node:assert/strict';

import { buildStatusBadgeModel, formatProgressValue, getTaskTypeLabel, shouldShowRetryAction } from '../logs/table_render.js';

test('buildStatusBadgeModel uses status catalog labels when available', () => {
  const model = buildStatusBadgeModel('success', { SUCCESS: { label: '已完成' } });
  assert.equal(model.label, '已完成');
  assert.equal(model.statusCode, 'SUCCESS');
});

test('formatProgressValue renders upload byte progress for upload tasks', () => {
  const value = formatProgressValue({
    task_type: 'upload',
    status: 'UPLOADING',
    upload_loaded_bytes: 12,
    upload_total_bytes: 40,
  });
  assert.equal(value, '12 / 40');
});

test('getTaskTypeLabel maps upload tasks to 上传', () => {
  assert.equal(getTaskTypeLabel({ task_type: 'upload' }), '上传');
  assert.equal(getTaskTypeLabel({ task_type: 'url' }), 'URL');
});

test('shouldShowRetryAction returns true for failed retryable url tasks', () => {
  assert.equal(
    shouldShowRetryAction({ task_type: 'url', status: 'FAILED', retryable: true, id: 'task-url-failed' }),
    true,
  );
});
