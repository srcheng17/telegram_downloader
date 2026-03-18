const { test, expect } = require('@playwright/test');

function decodeFormBody(request) {
  const body = request.postData() || '';
  return Object.fromEntries(new URLSearchParams(body).entries());
}

test('首页 duplicate SUCCESS 取消分支：展示确认并可下载已有文件', async ({ page }) => {
  const submissions = [];
  let createAttempts = 0;
  let existingDownloadRequests = 0;

  await page.route('**/api/summary', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        total_tasks: 6,
        active_tasks: 2,
        success_tasks: 3,
        failed_tasks: 1,
        canceled_tasks: 0,
      }),
    });
  });

  await page.route('**/download', async (route) => {
    submissions.push(decodeFormBody(route.request()));
    createAttempts += 1;

    if (createAttempts === 1) {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          ok: true,
          duplicate: true,
          needs_confirmation: true,
          logs_url: '/logs',
          download_url: '/api/tasks/e2e-success-download/download',
        }),
      });
      return;
    }

    await route.fulfill({
      status: 202,
      contentType: 'application/json',
      body: JSON.stringify({
        ok: true,
        duplicate: false,
        logs_url: '/logs',
      }),
    });
  });

  await page.route('**/api/tasks/e2e-success-download/download**', async (route) => {
    existingDownloadRequests += 1;
    await route.fulfill({
      status: 200,
      contentType: 'application/zip',
      headers: {
        'Content-Disposition': 'attachment; filename="e2e-success-download.zip"',
      },
      body: 'PK\x03\x04mock',
    });
  });

  await page.goto('/');

  await expect(page.locator('#summary-total')).toHaveText('6');
  await expect(page.locator('#summary-active')).toHaveText('2');

  await page.locator('#url').fill('https://www.telegra.ph/E2E-Success-01-01');
  await page.locator('#author').fill('E2E作者');
  await page.locator('#series_name').fill('E2E系列');
  await page.locator('#comic_name').fill('E2E漫画');
  await page.locator('#summary').fill('E2E简介');
  await page.locator('#tags').fill('科幻,冒险，连载');
  await page.locator('#genres').fill('青年,悬疑，热血');

  await page.getByRole('button', { name: '开始下载' }).click();

  await expect(page.locator('#download-feedback')).toContainText('该文件已有下载，是否生成新的CBZ文件？');
  await expect(page.getByRole('button', { name: '生成新的CBZ' })).toBeVisible();
  await expect(page.getByRole('button', { name: '取消并下载已有文件' })).toBeVisible();

  await expect.poll(() => submissions.length).toBe(1);
  expect(submissions[0]).toMatchObject({
    url: 'https://www.telegra.ph/E2E-Success-01-01',
    author: 'E2E作者',
    series_name: 'E2E系列',
    comic_name: 'E2E漫画',
    summary: 'E2E简介',
    tags: '科幻,冒险，连载',
    genres: '青年,悬疑，热血',
    force: 'false',
  });

  await page.getByRole('button', { name: '取消并下载已有文件' }).click();
  await expect(page.getByRole('button', { name: '下载已有文件' })).toBeVisible();
  await page.getByRole('button', { name: '下载已有文件' }).click();
  await expect.poll(() => existingDownloadRequests).toBe(1);
});

test('首页 duplicate SUCCESS 确认分支：force=true 二次提交创建新任务', async ({ page }) => {
  const submissions = [];
  let createAttempts = 0;

  await page.route('**/api/summary', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        total_tasks: 3,
        active_tasks: 1,
        success_tasks: 2,
        failed_tasks: 0,
        canceled_tasks: 0,
      }),
    });
  });

  await page.route('**/download', async (route) => {
    submissions.push(decodeFormBody(route.request()));
    createAttempts += 1;

    if (createAttempts === 1) {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          ok: true,
          duplicate: true,
          needs_confirmation: true,
          logs_url: '/logs',
          download_url: '/api/tasks/e2e-success-download/download',
        }),
      });
      return;
    }

    await route.fulfill({
      status: 202,
      contentType: 'application/json',
      body: JSON.stringify({
        ok: true,
        duplicate: false,
        logs_url: '/logs',
      }),
    });
  });

  await page.goto('/');

  await page.locator('#url').fill('https://www.telegra.ph/E2E-Success-01-01');
  await page.locator('#author').fill('二次提交作者');
  await page.locator('#series_name').fill('二次提交系列');
  await page.locator('#comic_name').fill('二次提交漫画');
  await page.locator('#summary').fill('二次提交简介');
  await page.locator('#tags').fill('剧情,动作');
  await page.locator('#genres').fill('冒险,奇幻');

  await page.getByRole('button', { name: '开始下载' }).click();
  await expect(page.locator('#download-feedback')).toContainText('该文件已有下载，是否生成新的CBZ文件？');

  await page.getByRole('button', { name: '生成新的CBZ' }).click();

  await expect.poll(() => submissions.length).toBe(2);
  expect(submissions[1]).toMatchObject({
    url: 'https://www.telegra.ph/E2E-Success-01-01',
    force: 'true',
    author: '二次提交作者',
    series_name: '二次提交系列',
    comic_name: '二次提交漫画',
    summary: '二次提交简介',
    tags: '剧情,动作',
    genres: '冒险,奇幻',
  });

  await expect(page.locator('#download-feedback')).toContainText('任务已加入队列。');
  await expect(page.getByRole('button', { name: '查看日志' })).toBeVisible();
});

test('日志页：筛选、错误详情弹窗、取消任务、下载预检', async ({ page }) => {
  const statusCatalog = {
    IN_PROGRESS: { label: '进行中', can_cancel: true, can_download: false },
    CANCEL_REQUESTED: { label: '取消中', can_cancel: false, can_download: false },
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
      progress: 3,
      total_images: 10,
      start_time: 1762531201,
      error: '',
    },
    {
      id: 'e2e-success-missing',
      url: 'https://telegra.ph/e2e-success-missing',
      status: 'SUCCESS',
      progress: 10,
      total_images: 10,
      start_time: 1762531202,
      error: '',
    },
    {
      id: 'e2e-success-download',
      url: 'https://telegra.ph/e2e-success-download',
      status: 'SUCCESS',
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
      (item) => item.status === 'SUCCESS' || item.status === 'FAILED' || item.status === 'CANCEL_REQUESTED',
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
          canceled_tasks: logs.filter((item) => item.status === 'CANCEL_REQUESTED').length,
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
      task.status = 'CANCEL_REQUESTED';
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
  await expect(pendingRow).toContainText('取消中');

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
  await expect(uploadRow).toContainText('12 / 40');

  const failedUploadRow = page.locator('#log-body tr', { hasText: 'e2e-upload-failed' });
  await failedUploadRow.getByRole('button', { name: '重试' }).click();
  await expect(page.locator('#logs-feedback')).toContainText('已重新加入队列。');
  await expect.poll(() => retryRequests).toBe(1);

  const successRow = page.locator('#log-body tr', { hasText: 'e2e-komga-copy' });
  await successRow.getByRole('button', { name: '下载' }).click();
  await expect(page.locator('#logs-feedback')).toContainText('myReadingManga');
  await expect.poll(() => copyRequests).toBe(1);
});
