const { test, expect, waitForMetadata } = require('../fixtures/auth');

const settings = { timeout: 30, retries: 3, image_concurrency: 2, download_action_mode: 'browser' };

async function mockHomeReads(page) {
  await page.route('**/api/summary', (route) => route.fulfill({ json: { total_tasks: 2, active_tasks: 2 } }));
  await page.route('**/api/metadata-history**', (route) => route.fulfill({ json: [] }));
}

async function mockLogs(page, logs, onRequest = () => {}) {
  await page.route('**/api/logs**', (route) => {
    const query = new URL(route.request().url()).searchParams;
    onRequest(query);
    return route.fulfill({ json: {
      logs, total: logs.length, page: 1, total_pages: 1, per_page: 25, has_active_tasks: false,
      filters: { status: query.get('status') || '', q: query.get('q') || '' },
      status_catalog: { SUCCEEDED: { label: '成功' } },
    } });
  });
}

test('浏览器历史恢复重新挂载交互，缓存缺失时保留导航和页面容器', async ({ page }) => {
  let logRequests = 0;
  let settingsRequests = 0;
  let lastQuery = '';
  await mockHomeReads(page);
  await mockLogs(page, [], (query) => { logRequests += 1; lastQuery = query.get('q') || ''; });
  await page.route('**/v2/settings', (route) => {
    settingsRequests += 1;
    return route.fulfill({ json: settings });
  });
  await page.goto('/');
  await waitForMetadata(page);
  await page.locator('.main-nav').getByRole('link', { name: '任务', exact: true }).click();
  await expect.poll(() => logRequests).toBeGreaterThan(0);
  await page.locator('.main-nav').getByRole('link', { name: '设置' }).click();
  await expect.poll(() => settingsRequests).toBe(1);

  const beforeRestore = logRequests;
  await page.goBack();
  await expect(page.locator('#logs-page')).toBeVisible();
  await expect.poll(() => logRequests).toBeGreaterThan(beforeRestore);
  await page.locator('#query-filter').fill('cached-history');
  await page.getByRole('button', { name: '筛选', exact: true }).click();
  await expect.poll(() => lastQuery).toBe('cached-history');

  await page.goForward();
  await expect(page.locator('#settings-form')).toBeVisible();
  await expect.poll(() => settingsRequests).toBe(2);
  await expect(page.locator('#task_concurrency, #log_retention_days, #file_retention_days')).toHaveCount(0);
  await page.evaluate(() => localStorage.removeItem('htmx-history-cache'));
  const beforeMiss = logRequests;
  await page.goBack();
  await expect(page.locator('#content #logs-page')).toBeVisible();
  await expect(page.locator('.main-nav')).toBeVisible();
  await expect(page.locator('.app-header')).toBeVisible();
  await expect.poll(() => logRequests).toBeGreaterThan(beforeMiss);
  await page.locator('#query-filter').fill('cache-miss-history');
  await page.getByRole('button', { name: '筛选', exact: true }).click();
  await expect.poll(() => lastQuery).toBe('cache-miss-history');
});

test('Komga复制错误显示失败反馈', async ({ page }) => {
  await page.addInitScript(() => { window.__telegraphSettingsState = { downloadActionMode: 'komga_copy' }; });
  await mockLogs(page, [{ id: 'missing-komga-artifact', status: 'SUCCEEDED', status_label: '成功', available_actions: ['download', 'copy_to_komga'], task_type: 'upload' }]);
  await page.route('**/api/tasks/missing-komga-artifact/copy-to-komga', (route) => route.fulfill({ status: 404, json: { ok: false, message: 'Stored file is unavailable.' } }));
  await page.goto('/logs');
  await page.getByRole('button', { name: '下载', exact: true }).click();
  await expect(page.locator('#logs-feedback')).toContainText('缓存文件不可用。');
  await expect(page.locator('#logs-feedback')).toHaveClass(/feedback-error/);
  await expect(page.locator('#logs-feedback')).not.toContainText('已复制');
});

test('上传HTML 413错误会释放提交按钮并允许重试', async ({ page }) => {
  let uploads = 0;
  let cancellations = 0;
  const pageErrors = [];
  page.on('pageerror', (error) => pageErrors.push(error.message));
  await mockHomeReads(page);
  await page.route('**/api/tasks/upload/init', (route) => route.fulfill({ status: 202, json: { ok: true, task_id: 'too-large-upload', upload_url: '/api/tasks/too-large-upload/upload-source' } }));
  await page.route('**/api/tasks/too-large-upload/upload-source', (route) => {
    uploads += 1;
    return route.fulfill({ status: 413, contentType: 'text/html', body: '<html>413 Request Entity Too Large</html>' });
  });
  await page.route('**/api/tasks/too-large-upload/cancel', (route) => {
    cancellations += 1;
    return route.fulfill({ status: 202, json: { ok: true } });
  });
  await page.goto('/');
  await waitForMetadata(page);
  await page.getByLabel('上传压缩包').check();
  await page.setInputFiles('#archive_file', { name: 'demo.zip', mimeType: 'application/zip', buffer: Buffer.from('PK\x03\x04demo') });
  const submit = page.getByRole('button', { name: '开始下载', exact: true });
  await submit.click();
  await expect(page.locator('#download-feedback')).toContainText('上传失败（413）');
  await expect(submit).toBeEnabled();
  await submit.click();
  await expect.poll(() => uploads).toBe(2);
  await expect.poll(() => cancellations).toBe(2);
  await expect(submit).toBeEnabled();
  expect(pageErrors).toEqual([]);
});


test('失败导航不会卸载仍显示的首页表单', async ({ page }) => {
  let submissions = 0;
  await mockHomeReads(page);
  await page.route('**/settings', (route) => route.fulfill({ status: 500, contentType: 'text/html', body: 'Temporary navigation failure' }));
  await page.route('**/download', (route) => {
    submissions += 1;
    return route.fulfill({ status: 202, json: { ok: true, task_id: 'after-navigation-error' } });
  });
  await page.goto('/');
  await waitForMetadata(page);
  const failedNavigation = page.waitForResponse((response) => new URL(response.url()).pathname === '/settings' && response.status() === 500);
  await page.locator('.main-nav').getByRole('link', { name: '设置' }).click();
  await failedNavigation;
  await expect(page).toHaveURL(/\/$/);
  await expect(page.locator('#download-form')).toBeVisible();
  await page.locator('#url').fill('https://telegra.ph/After-Navigation-Error-10-03');
  await page.getByRole('button', { name: '开始下载', exact: true }).click();
  await expect.poll(() => submissions).toBe(1);
  await expect(page.locator('#download-feedback')).toContainText('任务已加入队列。');
});
