const { test, expect, enterReview, waitForMetadata } = require('../fixtures/auth');

function decodeFormBody(request) {
  const body = request.postData() || '';
  const payload = Object.fromEntries(new URLSearchParams(body).entries());
  if (payload.metadata_document) payload.metadata_document = JSON.parse(payload.metadata_document);
  return payload;
}

async function mockExistingSuccessfulTask(page, taskId, submissions) {
  await page.route('**/api/summary', route => route.fulfill({ json: { total_tasks: 1, active_tasks: 0, success_tasks: 1 } }));
  await page.route('**/download', route => {
    submissions.push(decodeFormBody(route.request()));
    return route.fulfill({ json: {
      ok: true, task_id: taskId, status: 'SUCCEEDED', duplicate: true, needs_confirmation: true,
      logs_url: '/logs', download_url: `/api/tasks/${taskId}/download`,
    } });
  });
  await page.route(`**/api/tasks/${taskId}`, route => route.fulfill({ json: {
    ok: true, task: { id: taskId, status: 'SUCCEEDED', status_label: '成功', available_actions: ['download'] },
  } }));
}

test('首页已有成功作品：一次确认复用同一任务并下载，不要求二次选择', async ({ page }) => {
  const submissions = []; let existingDownloadRequests = 0;
  await mockExistingSuccessfulTask(page, 'e2e-success-download', submissions);
  await page.route('**/api/tasks/e2e-success-download/download', async route => {
    existingDownloadRequests += 1;
    await route.fulfill({ status: 200, contentType: 'application/zip',
      headers: { 'Content-Disposition': 'attachment; filename="e2e-success-download.zip"' }, body: 'PK\x03\x04mock' });
  });
  await page.goto('/'); await waitForMetadata(page);
  await page.locator('#url').fill('https://www.telegra.ph/E2E-Success-01-01');
  await enterReview(page);
  await page.locator('#metadata-creators-writer').fill('E2E作者');
  await page.locator('#metadata-series').fill('E2E系列');
  await page.locator('#metadata-title').fill('E2E漫画');
  await page.locator('.metadata-group').filter({ has: page.locator('summary', { hasText: '简介与分类' }) }).locator('summary').click();
  await page.locator('#metadata-summary').fill('E2E简介');
  await page.locator('#metadata-tags').fill('科幻\n冒险\n连载');
  await page.locator('#metadata-genres').fill('青年\n悬疑\n热血');
  await page.locator('#delivery-target').selectOption('download');
  expect(submissions).toHaveLength(0);
  await page.getByRole('button', { name: '确认并开始', exact: true }).click();
  await expect(page.locator('section[data-workflow-step="result"]')).toBeVisible();
  await expect(page.locator('#download-feedback')).toContainText('已复用内容一致的已有任务');
  await expect(page.locator('[data-task-result]')).toContainText('归档已生成');
  await expect(page.getByRole('button', { name: '生成新的CBZ', exact: true })).toBeHidden();
  await expect(page.getByRole('button', { name: '取消并下载已有文件', exact: true })).toBeHidden();
  expect(submissions).toHaveLength(1);
  expect(submissions[0]).toMatchObject({ url: 'https://www.telegra.ph/E2E-Success-01-01', force: 'false', delivery_target: 'download' });
  expect(submissions[0].idempotency_key).toMatch(/^[A-Za-z0-9_-]{16,128}$/);
  expect(submissions[0].metadata_document.fields).toMatchObject({
    'creators.writer': { value: ['E2E作者'] }, series: { value: 'E2E系列' }, title: { value: 'E2E漫画' },
    summary: { value: 'E2E简介' }, tags: { value: ['科幻', '冒险', '连载'] }, genres: { value: ['青年', '悬疑', '热血'] },
  });
  expect(submissions[0]).not.toHaveProperty('author');
  const download = page.waitForEvent('download');
  await page.getByRole('link', { name: '下载 CBZ', exact: true }).click(); await download;
  expect(existingDownloadRequests).toBe(1); expect(submissions).toHaveLength(1);
});

test('首页已有成功作品：返回核对再查看结果仍复用原任务且不重新创建', async ({ page }) => {
  const submissions = [];
  await mockExistingSuccessfulTask(page, 'e2e-success-revisit', submissions);
  await page.goto('/'); await waitForMetadata(page);
  await page.locator('#url').fill('https://www.telegra.ph/E2E-Success-Revisit-01-01');
  await enterReview(page);
  await page.locator('#metadata-title').fill('复用的作品');
  await page.locator('#delivery-target').selectOption('download');
  await page.getByRole('button', { name: '确认并开始', exact: true }).click();
  await expect(page.locator('section[data-workflow-step="result"]')).toBeVisible();
  await expect(page.locator('[data-task-result]')).toContainText('归档已生成');
  await expect(page.getByRole('link', { name: '下载 CBZ', exact: true })).toHaveAttribute('href', '/api/tasks/e2e-success-revisit/download');
  await page.getByRole('button', { name: '查看已提交信息', exact: true }).click();
  await expect(page.locator('section[data-workflow-step="review"]')).toBeVisible();
  await expect(page.locator('#metadata-title')).toHaveValue('复用的作品');
  await expect(page.locator('#metadata-title')).toBeDisabled();
  await expect(page.getByRole('button', { name: '确认并开始', exact: true })).toBeHidden();
  await page.getByRole('button', { name: '返回处理结果', exact: true }).click();
  await expect(page.locator('section[data-workflow-step="result"]')).toBeVisible();
  await expect(page.getByRole('link', { name: '下载 CBZ', exact: true })).toHaveAttribute('href', '/api/tasks/e2e-success-revisit/download');
  expect(submissions).toHaveLength(1);
  expect(submissions[0].force).toBe('false');
  expect(submissions[0].metadata_document.fields.title.value).toBe('复用的作品');
});

test('日志页：筛选、错误详情弹窗、取消任务、下载预检', async ({ page }) => {
  const statusCatalog = {
    IN_PROGRESS: { label: '进行中', can_cancel: true, can_download: false },
    CANCEL_REQUESTED: { label: '取消中', can_cancel: false, can_download: false },
    CANCELED: { label: '已取消', can_cancel: false, can_download: false },
    SUCCESS: { label: '成功', can_cancel: false, can_download: true },
    FAILED: { label: '失败', can_cancel: false, can_download: false },
  };
  const logs = [
    {
      id: 'e2e-failed-modal',
      url: 'https://telegra.ph/e2e-failed-modal',
      status: 'FAILED',
      progress: 2,
      total_images: 8,
      start_time: 1762531200,
      error: 'modal-token '.repeat(20),
    },
    {
      id: 'e2e-pending-cancel',
      url: 'https://telegra.ph/e2e-pending-cancel',
      status: 'IN_PROGRESS',
      available_actions: ['cancel'],
      progress: 3,
      total_images: 10,
      start_time: 1762531201,
      error: '',
    },
    {
      id: 'e2e-success-missing',
      url: 'https://telegra.ph/e2e-success-missing',
      status: 'SUCCESS',
      available_actions: ['download'],
      progress: 10,
      total_images: 10,
      start_time: 1762531202,
      error: '',
    },
    {
      id: 'e2e-success-download',
      url: 'https://telegra.ph/e2e-success-download',
      status: 'SUCCESS',
      available_actions: ['download'],
      progress: 11,
      total_images: 11,
      start_time: 1762531203,
      error: '',
    },
  ];
  let successDownloadRequests = 0;

  await page.route('**/api/logs**', async (route) => {
    const requestURL = new URL(route.request().url());
    const statusFilter = String(requestURL.searchParams.get('status') || '')
      .trim()
      .toUpperCase();
    const queryFilter = String(requestURL.searchParams.get('q') || '').trim();

    const filteredLogs = logs.filter((log) => {
      if (statusFilter && log.status !== statusFilter) {
        return false;
      }
      if (!queryFilter) {
        return true;
      }
      return (
        String(log.id).includes(queryFilter) ||
        String(log.url).includes(queryFilter) ||
        String(log.error).includes(queryFilter)
      );
    });

    const finishedTasks = logs.filter(
      (item) => ['SUCCESS', 'FAILED', 'CANCEL_REQUESTED', 'CANCELED'].includes(item.status),
    ).length;

    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        logs: filteredLogs,
        total: filteredLogs.length,
        page: 1,
        per_page: 25,
        total_pages: filteredLogs.length > 0 ? 1 : 0,
        has_active_tasks: false,
        filters: {
          status: statusFilter,
          q: queryFilter,
        },
        summary: {
          active_tasks: logs.filter((item) => item.status === 'IN_PROGRESS').length,
          finished_tasks: finishedTasks,
          success_rate: 50,
          failed_tasks: logs.filter((item) => item.status === 'FAILED').length,
          canceled_tasks: logs.filter((item) => ['CANCEL_REQUESTED', 'CANCELED'].includes(item.status)).length,
        },
        status_catalog: statusCatalog,
      }),
    });
  });

  await page.route('**/api/tasks/*/cancel', async (route) => {
    const match = route.request().url().match(/\/api\/tasks\/([^/]+)\/cancel$/);
    const taskID = match ? decodeURIComponent(match[1]) : '';
    const task = logs.find((item) => item.id === taskID);
    if (task) {
      task.status = 'CANCELED';
      task.retryable = true;
      task.available_actions = ['retry'];
    }
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        ok: true,
        message: 'Cancellation requested.',
      }),
    });
  });

  await page.route('**/api/tasks/*/retry', async (route) => {
    await route.fulfill({
      status: 202,
      contentType: 'application/json',
      body: JSON.stringify({
        ok: true,
        task_id: 'e2e-pending-cancel',
        status: 'QUEUED',
      }),
    });
  });

  await page.route('**/api/tasks/*/download**', async (route) => {
    const method = route.request().method();
    const match = route.request().url().match(/\/api\/tasks\/([^/]+)\/download/);
    const taskID = match ? decodeURIComponent(match[1]) : '';

    if (method === 'HEAD') {
      if (taskID === 'e2e-success-missing') {
        await route.fulfill({ status: 404 });
        return;
      }
      await route.fulfill({ status: 200 });
      return;
    }

    if (taskID === 'e2e-success-missing') {
      await route.fulfill({
        status: 404,
        contentType: 'application/json',
        body: JSON.stringify({
          ok: false,
          message: 'Stored file is unavailable.',
          code: 'artifact_unavailable',
        }),
      });
      return;
    }

    if (taskID === 'e2e-success-download') {
      successDownloadRequests += 1;
      await route.fulfill({
        status: 200,
        contentType: 'application/zip',
        headers: {
          'Content-Disposition': 'attachment; filename="e2e-success-download.zip"',
        },
        body: 'PK\x03\x04mock',
      });
      return;
    }

    await route.fulfill({
      status: 404,
      contentType: 'application/json',
      body: JSON.stringify({
        ok: false,
        message: 'Task not found.',
        code: 'task_not_found',
      }),
    });
  });

  await page.goto('/logs');
  await expect(page.locator('#summary-active-count')).toHaveText('1');

  await page.locator('#status-filter').selectOption('FAILED');
  await page.locator('#query-filter').fill('modal-token');
  await page.getByRole('button', { name: '筛选' }).click();

  const failedRow = page.locator('#log-body tr', { hasText: 'e2e-failed-modal' });
  await expect(failedRow).toBeVisible();
  await failedRow.getByRole('button', { name: '详情' }).click();

  await expect(page.locator('#error-modal')).toBeVisible();
  await expect(page.locator('#error-modal-content')).toContainText('modal-token');
  await page.keyboard.press('Escape');
  await expect(page.locator('#error-modal')).toHaveClass(/hidden/);

  await page.getByRole('button', { name: '重置' }).click();

  const pendingRow = page.locator('#log-body tr', { hasText: 'e2e-pending-cancel' });
  await expect(pendingRow).toBeVisible();
  await pendingRow.getByRole('button', { name: '取消' }).click();
  await expect(page.locator('#logs-feedback')).toContainText('已提交取消请求。');
  await expect(pendingRow.locator('[data-task-status-label]')).toHaveText(/取消中|已取消/);
  await expect(pendingRow.locator('[data-task-status-code]')).toHaveAttribute('data-task-status-code', /CANCEL_REQUESTED|CANCELED/);
  await expect(pendingRow.getByRole('button', { name: '重试' })).toBeVisible();

  await page.locator('#status-filter').selectOption('SUCCESS');
  await page.locator('#query-filter').fill('e2e-success-missing');
  await page.getByRole('button', { name: '筛选' }).click();

  const missingRow = page.locator('#log-body tr', { hasText: 'e2e-success-missing' });
  await expect(missingRow).toBeVisible();
  await missingRow.getByRole('button', { name: '下载' }).click();

  await expect(page).toHaveURL(/\/logs$/);
  await expect(page.locator('#logs-feedback')).toContainText('缓存文件不可用。');
  await expect(page.locator('#logs-feedback')).not.toContainText('下载已开始。');

  await page.locator('#status-filter').selectOption('SUCCESS');
  await page.locator('#query-filter').fill('e2e-success-download');
  await page.getByRole('button', { name: '筛选' }).click();

  const successRow = page.locator('#log-body tr', { hasText: 'e2e-success-download' });
  await expect(successRow).toBeVisible();
  await expect(successRow.locator('[data-task-status-label]')).toHaveText('成功');
  await expect(successRow.locator('[data-task-status-code]')).toHaveAttribute('data-task-status-code', 'SUCCESS');
  await expect(successRow.getByRole('button', { name: '下载' })).toBeVisible();
  await successRow.getByRole('button', { name: '下载' }).click();

  await expect(page.locator('#logs-feedback')).toContainText('下载已开始。');
  await expect.poll(() => successDownloadRequests).toBeGreaterThan(0);
});

test('日志页：上传失败可重试，Komga 模式成功任务走复制动作', async ({ page }) => {
  await page.addInitScript(() => {
    window.__telegraphSettingsState = { downloadActionMode: 'komga_copy' };
  });

  const statusCatalog = {
    UPLOADING: { label: '上传中', can_cancel: true, can_download: false },
    FAILED: { label: '失败', can_cancel: false, can_download: false },
    QUEUED: { label: '等待中', can_cancel: true, can_download: false },
    SUCCESS: { label: '成功', can_cancel: false, can_download: true },
  };
  const logs = [
    {
      id: 'e2e-upload-progress',
      url: '',
      task_type: 'upload',
      source_archive_name: 'demo.zip',
      status: 'UPLOADING',
      available_actions: ['cancel'],
      upload_loaded_bytes: 12,
      upload_total_bytes: 40,
      progress: 0,
      total_images: 0,
      start_time: 1762531204,
      error: '',
    },
    {
      id: 'e2e-upload-failed',
      url: '',
      task_type: 'upload',
      source_archive_name: 'retry-me.7z',
      status: 'FAILED',
      retryable: true,
      available_actions: ['retry'],
      upload_loaded_bytes: 40,
      upload_total_bytes: 40,
      progress: 0,
      total_images: 0,
      start_time: 1762531205,
      error: 'upload failed',
    },
    {
      id: 'e2e-komga-copy',
      url: '',
      task_type: 'upload',
      source_archive_name: 'copy.zip',
      status: 'SUCCESS',
      available_actions: ['download', 'copy_to_komga'],
      progress: 8,
      total_images: 8,
      start_time: 1762531206,
      error: '',
    },
  ];
  let retryRequests = 0;
  let copyRequests = 0;

  await page.route('**/api/logs**', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        logs,
        total: logs.length,
        page: 1,
        per_page: 25,
        total_pages: 1,
        has_active_tasks: true,
        filters: { status: '', q: '' },
        summary: {
          active_tasks: 1,
          finished_tasks: 2,
          success_rate: 50,
          failed_tasks: 1,
          canceled_tasks: 0,
        },
        status_catalog: statusCatalog,
      }),
    });
  });

  await page.route('**/api/tasks/*/retry', async (route) => {
    retryRequests += 1;
    await route.fulfill({
      status: 202,
      contentType: 'application/json',
      body: JSON.stringify({
        ok: true,
        task_id: 'e2e-upload-failed',
        status: 'QUEUED',
      }),
    });
  });

  await page.route('**/api/tasks/*/copy-to-komga', async (route) => {
    copyRequests += 1;
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        ok: true,
        task_id: 'e2e-komga-copy',
        target_path: '/Users/ryancheng/docker_data/komga/data/myReadingManga/tanbokon/demo.cbz',
      }),
    });
  });

  await page.goto('/logs');

  const uploadRow = page.locator('#log-body tr', { hasText: 'e2e-upload-progress' });
  await expect(uploadRow).toContainText('上传');
  await expect(uploadRow.locator('[data-task-status-label]')).toHaveText('上传中');
  await expect(uploadRow).toContainText('12 / 40');

  const failedUploadRow = page.locator('#log-body tr', { hasText: 'e2e-upload-failed' });
  await failedUploadRow.getByRole('button', { name: '重试' }).click();
  await expect(page.locator('#logs-feedback')).toContainText('已重新加入队列。');
  await expect.poll(() => retryRequests).toBe(1);

  const successRow = page.locator('#log-body tr', { hasText: 'e2e-komga-copy' });
  await expect(successRow.locator('[data-task-status-label]')).toHaveText('成功');
  await expect(successRow.getByRole('button', { name: '下载' })).toBeVisible();
  await successRow.getByRole('button', { name: '下载' }).click();
  await expect(page.locator('#logs-feedback')).toContainText('myReadingManga');
  await expect.poll(() => copyRequests).toBe(1);
});
