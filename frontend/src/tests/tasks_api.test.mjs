import test from 'node:test';
import assert from 'node:assert/strict';

import { createTasksApi } from '../shared/api/tasks_api.js';

test('createTasksApi.createTask sends JSON body and returns parsed payload', async () => {
  const calls = [];
  const api = createTasksApi(async (url, options) => {
    calls.push({ url, options });
    return new Response(JSON.stringify({ task_id: 't1', status: 'QUEUED' }), { status: 202 });
  });

  const result = await api.createTask({ url: 'https://telegra.ph/demo' });

  assert.equal(result.task_id, 't1');
  assert.equal(calls[0].url, '/v2/tasks');
  assert.equal(calls[0].options.method, 'POST');
  assert.equal(calls[0].options.headers['Content-Type'], 'application/json');
});
