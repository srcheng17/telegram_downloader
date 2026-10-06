const { test, anonymousTest, expect, enterReview, waitForMetadata } = require('../fixtures/auth');

anonymousTest('采集工作区：新接口和 OCR 资源继续受管理员保护', async ({ context }) => {
  for (const endpoint of ['/api/settings/extraction-rules', '/api/metadata/providers', '/api/telegram/account', '/static/ocr/tesseract-7.0.0/worker.min.js']) {
    expect((await context.request.get(endpoint)).status()).toBe(401);
  }
});

test('多图识别：生产资源、本地规则和手工锁定字段', async ({ page, request, baseURL }, testInfo) => {
  const schema = await (await request.get('/api/metadata/schema')).json();
  const previous = await (await request.get('/api/settings/extraction-rules')).json();
  const saved = await request.put('/api/settings/extraction-rules', { data: { expected_version: previous.rules_version, definitions_version: schema.definitions_version, rules: [{ id: 'e2e-title', target_key: 'title', labels: ['Title'], mode: 'label_value' }] } });
  expect(saved.ok()).toBeTruthy();
  await page.goto('/');
  await waitForMetadata(page);
  // Measure the OCR operation after page bootstrap. The existing shell still
  // loads htmx from its CDN; that request is unrelated to local OCR resources.
  const external = [];
  const rawImageUploads = [];
  page.on('request', req => {
    if (req.url().startsWith('http') && new URL(req.url()).origin !== new URL(baseURL).origin) external.push(req.url());
    if (req.method() === 'POST' && /image\//.test(req.headers()['content-type'] || '')) rawImageUploads.push(req.url());
  });
  const slot = page.locator('[data-module-slot="evidence"]');
  await expect(slot).toContainText('已载入 1 条本地规则');
  await slot.getByText('高级识别语言', { exact: true }).click();
  await slot.getByLabel('简体中文', { exact: true }).uncheck();
  await slot.locator('.ocr-edit-section > summary').click();
  await slot.locator('input[type=file]').setInputFiles([
    { name: 'first.png', mimeType: 'image/png', buffer: require('node:fs').readFileSync('tests/e2e/fixtures/ocr/eng.png') },
    { name: 'second.png', mimeType: 'image/png', buffer: require('node:fs').readFileSync('tests/e2e/fixtures/ocr/eng.png') },
  ]);
  await expect(slot.locator('.ocr-images')).toContainText('识别完成', { timeout: 45_000 });
  await expect(slot.getByLabel('合并文字预览（保留图片顺序和段落）')).toHaveValue(/Star Atlas[\s\S]*Star Atlas/, { timeout: 45_000 });
  await slot.getByLabel('补充或手工录入文字（加入合并预览）').fill('Title: Verified Atlas');
  await enterReview(page, { automatic: true });
  await expect(page.locator('#metadata-title')).toHaveValue('');
  // Two different title suggestions are a real conflict, not an automatic first hit.
  await expect(page.locator('.metadata-candidates')).toContainText('Verified Atlas');
  await page.locator('#metadata-title').fill('手工保留标题');
  await page.getByRole('button', { name: '上一步', exact: true }).click();
  await page.getByRole('tab', { name: '书目搜索', exact: true }).click();
  await expect(slot).not.toBeVisible();
  await page.getByRole('tab', { name: '截图识别', exact: true }).click();
  await expect(slot.getByLabel('合并文字预览（保留图片顺序和段落）')).toHaveValue(/Star Atlas[\s\S]*Star Atlas/);
  await enterReview(page, { automatic: true });
  await expect(page.locator('#metadata-title')).toHaveValue('手工保留标题');
  await page.screenshot({ path: testInfo.outputPath('ocr-review-workspace.png'), fullPage: true });
  expect(external).toEqual([]);
  expect(rawImageUploads).toEqual([]);
  const resource = await request.get('/static/ocr/tesseract-7.0.0/worker.min.js');
  expect(resource.headers()['cache-control']).toContain('immutable');
  page.on('dialog', dialog => dialog.accept());
  await page.goto('/telegram');
  await expect(page.locator('[data-module-slot="telegram-account"]')).not.toContainText('待接入');
});
