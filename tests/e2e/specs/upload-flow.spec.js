const { test, expect } = require('@playwright/test');

test('首页：上传模式可初始化任务、上传源包，并支持从历史回填', async ({ page }) => {
  let uploadInitRequests = 0;
  let uploadSourceRequests = 0;
  const uploadLogs = [
    {
      id: 'task-upload-1-uploading',
      task_type: 'upload',
      source_archive_name: 'demo.zip',
      status: 'UPLOADING',
      available_actions: ['cancel'],
      upload_loaded_bytes: 4,
      upload_total_bytes: 8,
      start_time: 1762531300,
      error: '',
    },
    {
      id: 'task-upload-1-preparing',
      task_type: 'upload',
      source_archive_name: 'demo.zip',
      status: 'CREATED',
      phase_label: '准备中',
      progress: { current: 0, total: 0, message: '准备中' },
      available_actions: ['cancel'],
      start_time: 1762531301,
      error: '',
    },
    {
      id: 'task-upload-1-running',
      task_type: 'upload',
      source_archive_name: 'demo.zip',
      status: 'RUNNING',
      phase_label: '运行中',
      progress: { current: 2, total: 8, message: '运行中' },
      available_actions: ['cancel'],
      start_time: 1762531302,
      error: '',
    },
    {
      id: 'task-upload-1-success',
      task_type: 'upload',
      source_archive_name: 'demo.zip',
      status: 'SUCCESS',
      available_actions: ['download'],
      progress: { current: 8, total: 8, message: '成功' },
      start_time: 1762531303,
      error: '',
    },
  ];

  await page.route('**/api/summary', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        total_tasks: 2,
        active_tasks: 1,
        success_tasks: 1,
        failed_tasks: 0,
        canceled_tasks: 0,
      }),
    });
  });

  await page.route('**/api/metadata-history**', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify([
        {
          task_type: 'url',
          url: 'https://telegra.ph/history-url',
          author: '历史作者URL',
          series_name: '历史系列URL',
          comic_name: '历史漫画URL',
          summary: '历史简介URL',
          tags: 'tag-url',
          genres: 'genre-url',
        },
        {
          task_type: 'upload',
          author: '历史作者上传',
          series_name: '历史系列上传',
          comic_name: '历史漫画上传',
          summary: '历史简介上传',
          tags: 'tag-upload',
          genres: 'genre-upload',
        },
      ]),
    });
  });

  await page.route('**/api/logs**', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        logs: uploadLogs,
        total: uploadLogs.length,
        page: 1,
        per_page: 25,
        total_pages: 1,
        has_active_tasks: true,
        filters: { status: '', q: '' },
        summary: {
          active_tasks: 3,
          finished_tasks: 1,
          success_rate: 100,
          failed_tasks: 0,
          canceled_tasks: 0,
        },
        status_catalog: {
          UPLOADING: { label: '上传中', can_cancel: true, can_download: false },
          CREATED: { label: '准备中', can_cancel: true, can_download: false },
          RUNNING: { label: '运行中', can_cancel: true, can_download: false },
          SUCCESS: { label: '成功', can_cancel: false, can_download: true },
        },
      }),
    });
  });

  await page.route('**/api/tasks/upload/init', async (route) => {
    uploadInitRequests += 1;
    const payload = route.request().postDataJSON();
    expect(payload.file_name).toBe('demo.zip');
    expect(payload.file_size).toBeGreaterThan(0);
    expect(payload.author).toBe('上传作者');
    await route.fulfill({
      status: 202,
      contentType: 'application/json',
      body: JSON.stringify({
        ok: true,
        task_id: 'task-upload-1',
        upload_token: 'upload-token-1',
        upload_url: '/api/tasks/task-upload-1/upload-source',
        logs_url: '/logs',
        status: 'UPLOADING',
      }),
    });
  });

  await page.route('**/api/tasks/task-upload-1/upload-source', async (route) => {
    uploadSourceRequests += 1;
    await expect(route.request().headerValue('x-upload-token')).resolves.toBe('upload-token-1');
    await route.fulfill({
      status: 202,
      contentType: 'application/json',
      body: JSON.stringify({
        ok: true,
        task_id: 'task-upload-1',
        status: 'QUEUED',
        logs_url: '/logs',
      }),
    });
  });

  await page.goto('/');

  await expect(page.locator('#summary-active')).toHaveText('1');

  await page.getByLabel('上传压缩包').check();
  await page.setInputFiles('#archive_file', {
    name: 'demo.zip',
    mimeType: 'application/zip',
    buffer: Buffer.from('PK\x03\x04demo'),
  });
  await page.locator('#author').fill('上传作者');
  await page.locator('#series_name').fill('上传系列');
  await page.locator('#comic_name').fill('上传漫画');
  await page.locator('#summary').fill('上传简介');
  await page.locator('#tags').fill('剧情 # 动作');
  await page.locator('#genres').fill('青年 ＃ 悬疑');

  await page.getByRole('button', { name: '开始下载' }).click();

  await expect.poll(() => uploadInitRequests).toBe(1);
  await expect.poll(() => uploadSourceRequests).toBe(1);
  await expect(page.locator('#download-feedback')).toContainText('任务已加入队列。');
  await expect(page.getByRole('button', { name: '查看日志' })).toBeVisible();

  await page.getByRole('button', { name: '查看日志' }).click();
  await expect(page).toHaveURL(/\/logs$/);
  await expect(page.locator('[data-task-status-label]')).toContainText(['上传中', '准备中', '运行中', '成功']);
  const successfulUploadRow = page.locator('#log-body tr', { hasText: 'task-upload-1-success' });
  await expect(successfulUploadRow.locator('[data-task-status-label]')).toHaveText('成功');
  await expect(successfulUploadRow.getByRole('button', { name: '下载' })).toBeVisible();

  await page.goto('/');
  await page.getByRole('button', { name: /历史作者URL/ }).click();
  await expect(page.getByLabel('URL 下载')).toBeChecked();
  await expect(page.locator('#url')).toHaveValue('https://telegra.ph/history-url');
  await expect(page.locator('#author')).toHaveValue('历史作者URL');

  await page.getByRole('button', { name: /历史作者上传/ }).click();
  await expect(page.getByLabel('上传压缩包')).toBeChecked();
  await expect(page.locator('#author')).toHaveValue('历史作者上传');
});
