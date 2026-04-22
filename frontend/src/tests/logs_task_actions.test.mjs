import test from 'node:test';
import assert from 'node:assert/strict';

import {
  canCancelTaskAction,
  canCopyToKomgaTaskAction,
  canDownloadTaskAction,
  canRetryTaskAction,
  requestRetryTask,
  runSuccessTaskAction,
} from '../logs/task_actions.js';

test('runSuccessTaskAction calls copy endpoint when mode is komga_copy', async () => {
  const calls = [];
  const api = {
    async postJson(url, payload) {
      calls.push({ url, payload });
      return { response: { ok: true, status: 200 }, payload: { ok: true, target_path: '/komga/系列/demo.cbz' } };
    },
  };

  const result = await runSuccessTaskAction({
    api,
    taskId: 'task-upload-success',
    mode: 'komga_copy',
    browserDownload: async () => {
      throw new Error('should not trigger browser download');
    },
  });

  assert.equal(calls[0].url, '/api/tasks/task-upload-success/copy-to-komga');
  assert.equal(result.payload.target_path, '/komga/系列/demo.cbz');
});

test('requestRetryTask posts retry request', async () => {
  const calls = [];
  const api = {
    async postJson(url, payload) {
      calls.push({ url, payload });
      return { response: { ok: true, status: 202 }, payload: { ok: true, task_id: 'task-upload-failed', status: 'QUEUED' } };
    },
  };

  const result = await requestRetryTask(api, 'task-upload-failed');

  assert.equal(calls[0].url, '/api/tasks/task-upload-failed/retry');
  assert.equal(result.payload.status, 'QUEUED');
});

test('backend available actions take priority over legacy status heuristics', () => {
  const task = { status: 'SUCCEEDED', available_actions: ['download', 'copy_to_komga'] };

  assert.equal(canDownloadTaskAction(task), true);
  assert.equal(canCopyToKomgaTaskAction(task), true);
  assert.equal(canCancelTaskAction(task), false);
});

test('legacy cancel and download actions still work without backend available actions', () => {
  assert.equal(canCancelTaskAction({ status: 'QUEUED' }), true);
  assert.equal(canCancelTaskAction({ status: 'UPLOADING' }), true);
  assert.equal(canCancelTaskAction({ status: 'IN_PROGRESS' }), true);
  assert.equal(canDownloadTaskAction({ status: 'SUCCESS' }), true);
});

test('legacy retry action still requires retryable url or source archive data', () => {
  assert.equal(
    canRetryTaskAction({
      status: 'FAILED',
      retryable: true,
      task_type: 'url',
      url: 'https://telegra.ph/retry-me',
    }),
    true,
  );
  assert.equal(
    canRetryTaskAction({
      status: 'FAILED',
      retryable: true,
      task_type: 'upload',
      source_archive_path: '/tmp/source.zip',
    }),
    true,
  );
  assert.equal(
    canRetryTaskAction({
      status: 'FAILED',
      retryable: true,
      task_type: 'url',
    }),
    false,
  );
});
