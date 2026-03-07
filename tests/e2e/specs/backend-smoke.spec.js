const { test, expect } = require('@playwright/test');

test('后端真实链路 smoke：health/ready + v2 create/list/cancel + legacy summary/logs', async ({ request }) => {
  const healthz = await request.get('/healthz');
  expect(healthz.ok()).toBeTruthy();

  const readyz = await request.get('/readyz');
  expect(readyz.ok()).toBeTruthy();

  const createResponse = await request.post('/v2/tasks', {
    data: { url: 'https://telegra.ph/e2e-backend-smoke' },
  });
  expect(createResponse.status()).toBe(202);
  const createPayload = await createResponse.json();
  expect(typeof createPayload.task_id).toBe('string');
  expect(createPayload.task_id.length).toBeGreaterThan(0);
  expect(createPayload.status).toBeTruthy();

  const taskID = createPayload.task_id;

  const listResponse = await request.get('/v2/tasks?q=e2e-backend-smoke&per_page=20&page=1');
  expect(listResponse.ok()).toBeTruthy();
  const listPayload = await listResponse.json();
  expect(Array.isArray(listPayload.tasks)).toBeTruthy();
  const matchedTask = listPayload.tasks.find((task) =>
    String(task.canonical_url || task.url || '').includes('e2e-backend-smoke'),
  );
  expect(matchedTask).toBeTruthy();

  const cancelResponse = await request.post(`/v2/tasks/${encodeURIComponent(taskID)}/cancel`);
  expect([202, 409]).toContain(cancelResponse.status());

  const summaryResponse = await request.get('/api/summary');
  expect(summaryResponse.ok()).toBeTruthy();
  const summaryPayload = await summaryResponse.json();
  expect(summaryPayload).toHaveProperty('total_tasks');
  expect(summaryPayload).toHaveProperty('active_tasks');

  const logsResponse = await request.get('/api/logs?page=1&per_page=20');
  expect(logsResponse.ok()).toBeTruthy();
  const logsPayload = await logsResponse.json();
  expect(Array.isArray(logsPayload.logs)).toBeTruthy();

  const legacyDownloadURL = `https://telegra.ph/e2e-legacy-${Date.now()}`;
  const legacyCreate = await request.post('/download', {
    form: { url: legacyDownloadURL },
    headers: { Accept: 'application/json' },
  });
  expect(legacyCreate.status()).toBe(202);
  const legacyCreatePayload = await legacyCreate.json();
  expect(legacyCreatePayload.ok).toBeTruthy();
  expect(typeof legacyCreatePayload.task_id).toBe('string');
  expect(legacyCreatePayload.task_id.length).toBeGreaterThan(0);

  const legacyTaskID = legacyCreatePayload.task_id;
  const legacyCancel = await request.post(`/api/tasks/${encodeURIComponent(legacyTaskID)}/cancel`);
  expect([200, 202, 409]).toContain(legacyCancel.status());

  const legacyDownloadHead = await request.fetch(`/api/tasks/${encodeURIComponent(legacyTaskID)}/download`, {
    method: 'HEAD',
  });
  expect([200, 404, 409]).toContain(legacyDownloadHead.status());
});
