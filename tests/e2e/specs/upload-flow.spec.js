const { test, expect, waitForMetadata, metadataDocument } = require('../fixtures/auth');

test('首页：上传模式可初始化任务、上传源包，并支持从历史回填', async ({ page, request }) => {
  const schemaResponse = await request.get('/api/metadata/schema');
  expect(schemaResponse.ok()).toBeTruthy();
  const schema = await schemaResponse.json();
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

  await page.route('**/api/metadata-history**', route => route.fulfill({ json: [
    { task_type: 'url', url: 'https://telegra.ph/history-url', author: '历史作者URL', comic_name: '历史漫画URL',
      metadata_document: metadataDocument(schema, { 'creators.writer': ['历史作者URL'], title: '历史漫画URL', series: '历史系列URL' }) },
    { task_type: 'upload', author: '历史作者上传', comic_name: '历史漫画上传',
      metadata_document: metadataDocument(schema, { 'creators.writer': ['历史作者上传'], title: '历史漫画上传', series: '历史系列上传' }) },
  ] }));

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
    expect(payload.metadata_document.fields['creators.writer'].value).toEqual(['上传作者']);
    expect(payload).not.toHaveProperty('author');
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
  await waitForMetadata(page);


  await page.getByLabel('上传压缩包').check();
  await page.setInputFiles('#archive_file', {
    name: 'demo.zip',
    mimeType: 'application/zip',
    buffer: Buffer.from('PK\x03\x04demo'),
  });
  await page.locator('#metadata-creators-writer').fill('上传作者');
  await page.locator('#metadata-series').fill('上传系列');
  await page.locator('#metadata-title').fill('上传漫画');
  await page.locator('.metadata-group').filter({ has: page.locator('summary', { hasText: '简介与分类' }) }).locator('summary').click();
  await page.locator('#metadata-summary').fill('上传简介');
  await page.locator('#metadata-tags').fill('剧情\n动作');
  await page.locator('#metadata-genres').fill('青年\n悬疑');

  await page.getByRole('button', { name: '开始下载' }).click();

  await expect.poll(() => uploadInitRequests).toBe(1);
  await expect.poll(() => uploadSourceRequests).toBe(1);
  await expect(page.locator('#download-feedback')).toContainText('任务已加入队列。');
  await expect(page.getByRole('button', { name: '查看任务' })).toBeVisible();

  await page.getByRole('button', { name: '查看任务' }).click();
  await expect(page).toHaveURL(/\/logs$/);
  await expect(page.locator('[data-task-status-label]')).toContainText(['上传中', '准备中', '运行中', '成功']);
  const successfulUploadRow = page.locator('#log-body tr', { hasText: 'task-upload-1-success' });
  await expect(successfulUploadRow.locator('[data-task-status-label]')).toHaveText('成功');
  await expect(successfulUploadRow.getByRole('button', { name: '下载' })).toBeVisible();

  await page.goto('/');
  await waitForMetadata(page);
  await page.locator('#url').fill('https://telegra.ph/keep-current-source');
  await page.locator('#metadata-history-collapsible > summary').click();
  await page.locator('.metadata-history-item').nth(0).click();
  await page.locator('.metadata-candidates').getByRole('checkbox', { name: '作者／原作', exact: true }).check();
  await page.getByRole('button', { name: '采用所选字段' }).click();
  await expect(page.getByRole('radio', { name: 'Telegraph 链接', exact: true })).toBeChecked();
  await expect(page.locator('#url')).toHaveValue('https://telegra.ph/keep-current-source');
  await expect(page.locator('#metadata-creators-writer')).toHaveValue('历史作者URL');

  await page.locator('.metadata-history-item').nth(1).click();
  await page.locator('.metadata-candidates').getByRole('checkbox', { name: '作者／原作', exact: true }).check();
  await page.getByRole('button', { name: '采用所选字段' }).click();
  await expect(page.getByRole('radio', { name: 'Telegraph 链接', exact: true })).toBeChecked();
  await expect(page.locator('#metadata-creators-writer')).toHaveValue('历史作者上传');
  await expect(page.locator('#url')).toHaveValue('https://telegra.ph/keep-current-source');
});
