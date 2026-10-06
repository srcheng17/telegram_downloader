import test from 'node:test';
import assert from 'node:assert/strict';

import { resolveDownloadSubmission } from '../home/submit_flow.js';

test('resolveDownloadSubmission returns duplicate_active state', () => {
  const result = resolveDownloadSubmission(
    { duplicate: true, active: true, logs_url: '/logs' },
    { url: 'https://telegra.ph/demo' },
  );
  assert.equal(result.kind, 'duplicate_active');
  assert.deepEqual(result.feedback, { message: '该链接已在下载队列中。', kind: 'info' });
  assert.equal(result.actions.logsUrl, '/logs');
  assert.equal(result.pendingDuplicate, null);
});

test('resolveDownloadSubmission returns duplicate_confirm state', () => {
  const result = resolveDownloadSubmission(
    { duplicate: true, needs_confirmation: true, download_url: '/api/tasks/t1/download', logs_url: '/logs' },
    { url: 'https://telegra.ph/demo', force: 'false' },
  );
  assert.equal(result.kind, 'duplicate_confirm');
  assert.equal(result.feedback.kind, 'info');
  assert.equal(result.pendingDuplicate.downloadUrl, '/api/tasks/t1/download');
  assert.deepEqual(result.pendingDuplicate.basePayload, { url: 'https://telegra.ph/demo' });
});

test('resolveDownloadSubmission returns queued state', () => {
  const result = resolveDownloadSubmission(
    { ok: true, logs_url: '/logs' },
    { url: 'https://telegra.ph/demo' },
  );
  assert.equal(result.kind, 'queued');
  assert.deepEqual(result.feedback, { message: '任务已加入队列。', kind: 'success' });
  assert.equal(result.actions.logsUrl, '/logs');
});

test('guided keyed confirmation reuses the server-validated existing task without another decision', () => {
  const result = resolveDownloadSubmission(
    { duplicate: true, needs_confirmation: true, task_id: 'existing-task', download_url: '/api/tasks/existing-task/download' },
    { idempotency_key: 'stable-submission-key', metadata_document: '{}', force: 'false' },
  );
  assert.equal(result.kind, 'duplicate_existing');
  assert.equal(result.pendingDuplicate, null);
  assert.equal(result.actions.needsDuplicateChoices, undefined);
  assert.match(result.feedback.message, /已有任务/);
});

test('guided duplicate without a task identity fails safely instead of opening legacy confirmation', () => {
  assert.throws(() => resolveDownloadSubmission({ duplicate: true, needs_confirmation: true }, { idempotency_key: 'stable-submission-key' }), /已有任务/);
});
