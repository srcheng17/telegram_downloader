import test from 'node:test';
import assert from 'node:assert/strict';

import { createLogsActionController } from '../logs/action_controller.js';

test('requestRetry shows success feedback and refreshes logs', async () => {
  const feedback = [];
  let refreshed = 0;
  const controller = createLogsActionController({
    api: {
      postJson: async () => ({ response: { ok: true }, payload: { ok: true, task_id: 'task-1', status: 'QUEUED', message: 'Task requeued.' } }),
      getJson: async () => ({ payload: null }),
    },
    logsApi: {
      head: async () => ({ ok: true, status: 200 }),
      getSettingsMode: async () => ({ response: { ok: true }, payload: { download_action_mode: 'browser' } }),
    },
    win: {},
    doc: { body: { contains: () => false }, getElementById: () => null, createElement: () => ({ setAttribute() {}, style: {} }) },
    state: { currentPage: 2, downloadInProgressTaskIds: new Set(), retryInProgressTaskIds: new Set() },
    showFeedback(message, kind) { feedback.push({ message, kind }); },
    fetchLogs() { refreshed += 1; },
  });

  await controller.requestRetry('task-1', null);
  assert.deepEqual(feedback[0], { message: 'Task requeued.', kind: 'success' });
  assert.equal(refreshed, 1);
});

test('getDownloadActionMode prefers cached settings mode', async () => {
  const win = { __telegraphSettingsState: { downloadActionMode: 'komga_copy' } };
  const controller = createLogsActionController({
    api: { getJson: async () => ({ payload: null }) },
    logsApi: {
      head: async () => ({ ok: true, status: 200 }),
      getSettingsMode: async () => ({ response: { ok: true }, payload: { download_action_mode: 'browser' } }),
    },
    win,
    doc: { body: { contains: () => false }, getElementById: () => null, createElement: () => ({ setAttribute() {}, style: {} }) },
    state: { currentPage: 1, downloadInProgressTaskIds: new Set(), retryInProgressTaskIds: new Set() },
    showFeedback() {},
    fetchLogs() {},
  });

  const mode = await controller.getDownloadActionMode();
  assert.equal(mode, 'komga_copy');
});


test('late task actions cannot update or repoll a page after unmount', async () => {
  let resolveRequest;
  const request = new Promise((resolve) => { resolveRequest = resolve; });
  const state = { mountedRoot: {}, pageController: new AbortController(), currentPage: 1, downloadInProgressTaskIds: new Set(), retryInProgressTaskIds: new Set() };
  const feedback = [];
  let refreshes = 0;
  const controller = createLogsActionController({
    api: { postJson: () => request }, logsApi: {}, win: {}, doc: {}, state,
    showFeedback: (message) => feedback.push(message),
    fetchLogs: () => { refreshes += 1; },
  });
  const cancel = controller.requestCancel('task-1', null);
  state.pageController.abort();
  state.mountedRoot = null;
  resolveRequest({ response: { ok: true }, payload: { ok: true } });
  await cancel;
  assert.deepEqual(feedback, []);
  assert.equal(refreshes, 0);
});
