import test from 'node:test';
import assert from 'node:assert/strict';

import { buildStatusBadgeModel, mapTaskToLogViewModel } from '../logs/view_model.js';

test('buildStatusBadgeModel uses status catalog labels when available', () => {
  const model = buildStatusBadgeModel('success', { SUCCESS: { label: '已完成' } });
  assert.equal(model.label, '已完成');
  assert.equal(model.statusCode, 'SUCCESS');
});

test('mapTaskToLogViewModel maps upload task progress and type label', () => {
  const view = mapTaskToLogViewModel({
    id: 'task-upload',
    task_type: 'upload',
    status: 'UPLOADING',
    upload_loaded_bytes: 12,
    upload_total_bytes: 40,
  });
  assert.equal(view.taskTypeLabel, '上传');
  assert.equal(view.progressText, '12 / 40');
  assert.equal(view.canRetry, false);
});

test('mapTaskToLogViewModel marks canceled retryable tasks', () => {
  const view = mapTaskToLogViewModel({
    id: 'task-canceled',
    task_type: 'url',
    status: 'CANCELED',
    retryable: true,
  });
  assert.equal(view.taskTypeLabel, 'URL');
  assert.equal(view.canRetry, true);
});

test('mapTaskToLogViewModel renders preparing state for running url tasks before totals exist', () => {
  const view = mapTaskToLogViewModel({
    id: 'task-running',
    task_type: 'url',
    status: 'IN_PROGRESS',
    progress: 0,
    total_images: 0,
  });
  assert.equal(view.progressText, '准备中');
});

test('mapTaskToLogViewModel prefers backend status label, progress, and available actions', () => {
  const view = mapTaskToLogViewModel({
    id: 'task-core',
    task_type: 'url',
    status: 'RUNNING',
    status_label: '运行中',
    phase_label: '下载中',
    progress: { phase: 'downloading', current: 2, total: 5, unit: 'images', message: '下载中' },
    available_actions: ['cancel'],
  });

  assert.equal(view.statusLabel, '运行中');
  assert.equal(view.progressText, '下载中 2/5');
  assert.deepEqual(view.availableActions, ['cancel']);
});
