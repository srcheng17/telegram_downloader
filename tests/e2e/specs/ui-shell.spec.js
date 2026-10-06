const { test, anonymousTest, expect, adminPassword, enterReview, waitForMetadata, metadataDocument } = require('../fixtures/auth');
const { execFileSync } = require('node:child_process');

anonymousTest('工作台：匿名路由保护、网页登录与退出后的旧会话失效', async ({ page, context, browser, baseURL }) => {
  for (const path of ['/api/tasks', '/api/metadata/schema', '/api/settings/metadata-fields', '/v2/settings', '/api/tasks/not-authorized/download']) {
    expect((await context.request.get(path)).status()).toBe(401);
  }
  expect((await context.request.get('/healthz')).ok()).toBeTruthy();
  expect((await context.request.get('/readyz')).ok()).toBeTruthy();
  const html = await context.request.get('/settings', { headers: { Accept: 'text/html' }, maxRedirects: 0 });
  expect(html.status()).toBe(303);
  expect(html.headers().location).toBe('/auth/login');
  const htmx = await context.request.get('/settings', { headers: { 'HX-Request': 'true' }, maxRedirects: 0 });
  expect(htmx.status()).toBe(401);
  expect(htmx.headers()['hx-redirect']).toBe('/auth/login');

  await page.goto('/settings');
  await expect(page).toHaveURL(/\/auth\/login$/);
  await expect(page.locator('#settings-form')).toHaveCount(0);
  await page.locator('#admin-password').fill(adminPassword());
  await page.getByRole('button', { name: '登录', exact: true }).click();
  await expect(page).toHaveURL(/\/$/);
  await waitForMetadata(page);
  const cookies = await context.cookies();
  const sessionCookie = cookies.find(cookie => cookie.name === 'td_admin_session');
  expect(sessionCookie.httpOnly).toBe(true);
  expect(sessionCookie.sameSite).toBe('Strict');
  const missingCSRF = await context.request.post('/api/metadata/validate', { headers: { Origin: new URL(baseURL).origin }, data: {} });
  expect(missingCSRF.status()).toBe(403);

  await page.locator('[data-admin-logout]').click();
  await expect(page).toHaveURL(/\/auth\/login$/);
  const replay = await browser.newContext({ baseURL, storageState: { cookies, origins: [] } });
  try { expect((await replay.request.get('/api/tasks')).status()).toBe(401); }
  finally { await replay.close(); }
  await page.goBack();
  await expect(page).toHaveURL(/\/auth\/login$/);
  await expect(page.locator('#metadata-title')).toHaveCount(0);
});

test('工作台：自定义字段保存重载，动态录入与真实任务历史保留完整文档', async ({ page, request }) => {
  const suffix = Date.now().toString(36);
  const definitions = [
    { name: `note_${suffix}`, label: '浏览器核对备注', type: 'string', value: '保留中文备注' },
    { name: `index_${suffix}`, label: '浏览器整理序号', type: 'integer', value: 0 },
    { name: `checked_${suffix}`, label: '浏览器已核对', type: 'boolean', value: false },
    { name: `list_${suffix}`, label: '浏览器别称列表', type: 'string[]', value: ['A B', '中文别称'] },
  ];
  await page.goto('/settings');
  await page.getByRole('tab', { name: '字段', exact: true }).click();
  await expect(page.locator('#metadata-fields-add')).toBeEnabled();
  for (const definition of definitions) {
    await page.locator('#metadata-fields-add').click();
    const row = page.locator('[data-module-slot="metadata-fields"] .settings-field-group').last();
    await row.getByLabel('字段标识（小写字母、数字、下划线）', { exact: true }).fill(definition.name);
    await row.getByLabel('中文名称', { exact: true }).fill(definition.label);
    await row.getByLabel('字段类型（保存后不可更改）', { exact: true }).selectOption(definition.type);
    await row.getByLabel('历史回填', { exact: true }).check();
  }
  await page.locator('#metadata-fields-save').click();
  await expect(page.locator('#metadata-fields-status')).toContainText('自定义字段已保存');
  await page.reload();
  await expect(page.locator('#metadata-fields-add')).toBeEnabled();
  for (const definition of definitions) {
    const row = page.getByRole('group', { name: definition.label, exact: true });
    await expect(row.getByLabel('中文名称', { exact: true })).toHaveValue(definition.label);
    await expect(row.getByLabel('字段类型（保存后不可更改）', { exact: true })).toBeDisabled();
  }

  const schemaResponse = await request.get('/api/metadata/schema');
  const schema = await schemaResponse.json();
  for (const definition of definitions) expect(schema.definitions[`custom.user.${definition.name}`].export_status).toBe('internal_only');
  await page.goto('/');
  await waitForMetadata(page);
  await enterReview(page);
  await page.locator('#metadata-title').fill(`扩展元数据 ${suffix}`);
  await page.locator('#metadata-number').fill('2.5 特别篇');
  await page.locator('.metadata-group').filter({ has: page.locator('summary', { hasText: '简介与分类' }) }).locator('summary').click();
  await page.locator('#metadata-aliases').fill('原文名\nEnglish title');
  // Blurring a field must not move a disclosure between pointer down and up.
  await expect(page.locator('#metadata-aliases')).toBeFocused();
  const customGroup = page.locator('.metadata-group').filter({ has: page.locator('summary', { hasText: '自定义字段' }) });
  await expect(customGroup).toHaveJSProperty('open', false);
  await customGroup.locator('summary').click();
  await expect(customGroup).toHaveJSProperty('open', true);
  for (const definition of definitions) {
    const control = page.locator(`#metadata-custom-user-${definition.name}`);
    await expect(control).toBeVisible();
    if (definition.type === 'boolean') await control.selectOption(String(definition.value));
    else await control.fill(Array.isArray(definition.value) ? definition.value.join('\n') : String(definition.value));
  }
  const archive = execFileSync('python3', ['-c', `
import base64, io, sys, zipfile
buffer = io.BytesIO()
with zipfile.ZipFile(buffer, 'w') as output:
    output.writestr('1.png', base64.b64decode('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAIAAACQd1PeAAAADElEQVR4nGP4z8AAAAMBAQDJ/pLvAAAAAElFTkSuQmCC'))
sys.stdout.buffer.write(buffer.getvalue())
`]);
  await page.getByRole('button', { name: '上一步', exact: true }).click();
  await page.getByLabel('上传压缩包').check();
  await page.setInputFiles('#archive_file', { name: 'metadata-roundtrip.zip', mimeType: 'application/zip', buffer: archive });
  await enterReview(page);
  const created = page.waitForResponse(response => response.url().includes('/api/tasks/upload/init') && response.request().method() === 'POST');
  await page.getByRole('button', { name: '确认并开始', exact: true }).click();
  const response = await created;
  expect(response.status()).toBe(202);
  const { task_id: taskID } = await response.json();
  await expect(page.locator('#download-feedback')).toContainText('任务已加入队列');
  let history;
  await expect.poll(async () => {
    const result = await request.get('/api/metadata-history');
    expect(result.ok()).toBeTruthy();
    const entries = await result.json();
    history = entries.find(entry => entry.metadata_document?.fields?.title?.value === `扩展元数据 ${suffix}`);
    return Boolean(history);
  }).toBe(true);
  expect(history.metadata_document.definitions_version).toBe(schema.definitions_version);
  expect(history.metadata_document.fields.number.value).toBe('2.5 特别篇');
  expect(history.metadata_document.fields.aliases.value).toEqual(['原文名', 'English title']);
  for (const definition of definitions) expect(history.metadata_document.fields[`custom.user.${definition.name}`].value).toEqual(definition.value);
  await expect.poll(async () => {
    const response = await request.get('/api/tasks?per_page=100');
    const task = (await response.json()).tasks.find(task => task.id === taskID);
    return task?.status === 'FAILED' ? `FAILED: ${task.error}` : task?.status;
  }, { timeout: 30_000 }).toBe('SUCCEEDED');

  // The successful submitted revision is clean: normal navigation should not prompt.
  let prompts = 0;
  const onDialog = async dialog => { prompts += 1; await dialog.dismiss(); };
  page.on('dialog', onDialog);
  await page.locator('.main-nav').getByRole('link', { name: '任务', exact: true }).click();
  await expect(page).toHaveURL(/\/logs$/);
  page.off('dialog', onDialog);
  expect(prompts).toBe(0);
  await page.goto('/');
  await waitForMetadata(page);
  await enterReview(page);
  await page.locator('#metadata-history-collapsible > summary').click();
  await expect(page.locator('#metadata-history-list')).toContainText(`扩展元数据 ${suffix}`);
});

test('工作台：历史提交和产物分别核对，清空与迟到候选不被覆盖', async ({ page, request }) => {
  const schema = await (await request.get('/api/metadata/schema')).json();
  const entry = {
    task_type: 'upload', comic_name: '历史提交标题',
    metadata_document: metadataDocument(schema, { title: '历史提交标题', summary: '提交简介' }),
    effective_metadata_document: metadataDocument(schema, { title: '归档标题', summary: '原包补充简介' }),
  };
  await page.route('**/api/metadata-history**', route => route.fulfill({ json: [entry] }));
  await page.goto('/');
  await waitForMetadata(page);
  await page.locator('#url').fill('https://telegra.ph/preserve-source');
  await enterReview(page);
  await page.locator('#metadata-history-collapsible > summary').click();
  await page.locator('.metadata-history-item').click();
  await page.locator('[data-history-view="submitted"]').click();
  await expect(page.locator('.metadata-candidates')).toContainText('候选：历史提交标题');
  await page.getByRole('button', { name: '明确清空标题', exact: true }).click();
  await page.locator('.metadata-candidates').getByRole('checkbox', { name: '标题', exact: true }).check();
  await page.locator('.metadata-candidates').getByRole('checkbox', { name: '确认替换所选的手工保护字段', exact: true }).check();
  await page.getByRole('button', { name: '采用所选字段', exact: true }).click();
  await expect(page.locator('#metadata-editor > [role="status"]')).toContainText('过期');
  await expect(page.locator('#metadata-title')).toHaveValue('');

  await page.locator('[data-history-view="effective"]').click();
  await expect(page.locator('.metadata-candidates')).toContainText('候选：归档标题');
  await page.locator('.metadata-candidates').getByRole('checkbox', { name: '标题', exact: true }).check();
  await page.getByRole('button', { name: '采用所选字段', exact: true }).click();
  await expect(page.locator('#metadata-editor > [role="status"]')).toContainText('确认');
  await expect(page.locator('#metadata-title')).toHaveValue('');
  await page.locator('.metadata-candidates').getByRole('checkbox', { name: '确认替换所选的手工保护字段', exact: true }).check();
  await page.getByRole('button', { name: '采用所选字段', exact: true }).click();
  await expect(page.locator('#metadata-title')).toHaveValue('归档标题');
  await expect(page.locator('#metadata-summary')).toHaveValue('');
  await expect(page.locator('#url')).toHaveValue('https://telegra.ph/preserve-source');
});

test('工作台：取消离开保留草稿，确认离开和后退不恢复敏感历史', async ({ page }) => {
  await page.goto('/');
  await waitForMetadata(page);
  await enterReview(page);
  await page.locator('#metadata-title').fill('私密草稿测试标记');
  page.once('dialog', dialog => dialog.dismiss());
  await page.locator('.main-nav').getByRole('link', { name: '任务', exact: true }).click();
  await expect(page).toHaveURL(/\/$/);
  await expect(page.locator('#metadata-title')).toHaveValue('私密草稿测试标记');
  page.once('dialog', dialog => dialog.accept());
  await page.locator('.main-nav').getByRole('link', { name: '任务', exact: true }).click();
  await expect(page).toHaveURL(/\/logs$/);
  const localCache = await page.evaluate(() => JSON.stringify({ ...localStorage, ...sessionStorage }));
  expect(localCache).not.toContain('私密草稿测试标记');
  await page.goBack();
  await waitForMetadata(page);
  await expect(page.locator('#metadata-title')).toHaveValue('');
});

test('工作台：桌面侧栏隐藏跨 htmx 导航保留，展开后恢复内容宽度', async ({ page }, testInfo) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/');
  await waitForMetadata(page);
  const sidebar = page.locator('#workspace-sidebar');
  const layout = page.locator('.workspace-layout');
  const toggle = page.locator('[data-sidebar-toggle]');
  const main = page.locator('.workspace-main');
  await expect(toggle).toHaveAttribute('aria-expanded', 'true');
  const expandedMainWidth = (await main.boundingBox()).width;
  await sidebar.evaluate(node => { node.dataset.e2ePersistent = 'same-shell'; });
  await toggle.click();
  await expect(layout).toHaveClass(/is-sidebar-collapsed/);
  await expect(toggle).toHaveAttribute('aria-label', '显示侧栏');
  await expect(sidebar).toBeHidden();
  await expect(sidebar).toHaveJSProperty('inert', true);
  await expect.poll(() => sidebar.evaluate(node => node.getBoundingClientRect().width)).toBe(0);
  await expect.poll(async () => (await main.boundingBox()).width).toBeGreaterThan(expandedMainWidth + 100);
  await page.screenshot({ path: testInfo.outputPath('workspace-sidebar-collapsed.png'), fullPage: true });

  const navigation = page.waitForResponse(response => new URL(response.url()).pathname === '/logs' && response.request().headers()['hx-request'] === 'true');
  await page.getByRole('link', { name: '查看任务', exact: true }).click();
  expect((await navigation).ok()).toBe(true);
  await expect(page.locator('#logs-page')).toBeVisible();
  await expect(sidebar).toHaveAttribute('data-e2e-persistent', 'same-shell');
  await expect(layout).toHaveClass(/is-sidebar-collapsed/);
  await expect(toggle).toHaveAttribute('aria-expanded', 'false');
  await toggle.click();
  await expect(toggle).toHaveAttribute('aria-label', '隐藏侧栏');
  await expect(layout).not.toHaveClass(/is-sidebar-collapsed/);
  await expect(sidebar).toHaveJSProperty('inert', false);
  await expect.poll(async () => (await sidebar.boundingBox()).width).toBeGreaterThan(180);
  await page.locator('.main-nav').getByRole('link', { name: '设置', exact: true }).click();
  await expect(page.locator('#settings-page')).toBeVisible();
});

test('工作台：移动导航限制键盘焦点并隔离背景，关闭后恢复焦点与操作', async ({ page }, testInfo) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto('/settings');
  const sidebar = page.locator('#workspace-sidebar');
  const toggle = page.locator('[data-navigation-toggle]');
  const main = page.locator('.workspace-main');
  await expect(sidebar).toBeHidden();
  await expect(sidebar).toHaveJSProperty('inert', true);
  const toggleBox = await toggle.boundingBox();
  expect(toggleBox.width).toBeGreaterThanOrEqual(44);
  expect(toggleBox.height).toBeGreaterThanOrEqual(44);
  await toggle.focus();
  await page.keyboard.press('Enter');
  await expect(page.getByRole('dialog', { name: '主导航', exact: true })).toBeVisible();
  await expect(sidebar).toHaveAttribute('aria-modal', 'true');
  await expect(sidebar).toHaveJSProperty('inert', false);
  await expect(main).toHaveJSProperty('inert', true);
  const closeBox = await sidebar.locator('[data-navigation-close]').boundingBox();
  expect(closeBox.width).toBeGreaterThanOrEqual(44);
  expect(closeBox.height).toBeGreaterThanOrEqual(44);
  await expect(page.locator('.skip-link')).toHaveJSProperty('inert', true);
  await expect(page.locator('body')).toHaveCSS('overflow', 'hidden');
  await expect(sidebar).toHaveCSS('transform', 'matrix(1, 0, 0, 1, 0, 0)');
  await page.screenshot({ path: testInfo.outputPath('settings-mobile-drawer.png'), fullPage: true });

  const first = sidebar.getByRole('link', { name: '漫画工作台首页', exact: true });
  const last = sidebar.getByRole('button', { name: '退出登录', exact: true });
  await first.focus();
  await page.keyboard.press('Shift+Tab');
  await expect(last).toBeFocused();
  await page.keyboard.press('Tab');
  await expect(first).toBeFocused();
  await page.locator('#timeout').evaluate(node => node.focus());
  await expect(first).toBeFocused();

  await page.keyboard.press('Escape');
  await expect(sidebar).toBeHidden();
  await expect(sidebar).toHaveJSProperty('inert', true);
  await expect(main).toHaveJSProperty('inert', false);
  await expect(page.locator('.skip-link')).toHaveJSProperty('inert', false);
  await expect(toggle).toBeFocused();
  await expect(toggle).toHaveAttribute('aria-expanded', 'false');
  await page.locator('#timeout').focus();
  await expect(page.locator('#timeout')).toBeFocused();
  await page.screenshot({ path: testInfo.outputPath('settings-mobile-download.png'), fullPage: true });

  await toggle.click();
  await expect(sidebar).toBeVisible();
  await page.locator('[data-navigation-backdrop]').click({ position: { x: 350, y: 300 } });
  await expect(sidebar).toBeHidden();
  await expect(main).toHaveJSProperty('inert', false);
  await expect(toggle).toBeFocused();

  await page.setViewportSize({ width: 180, height: 800 });
  await toggle.click();
  await expect(sidebar).toHaveCSS('transform', 'matrix(1, 0, 0, 1, 0, 0)');
  const zoomedCloseBox = await sidebar.locator('[data-navigation-close]').boundingBox();
  expect(zoomedCloseBox.x).toBeGreaterThanOrEqual(0);
  expect(zoomedCloseBox.x + zoomedCloseBox.width).toBeLessThanOrEqual(180);
  await sidebar.locator('[data-navigation-close]').click();
  await expect(sidebar).toBeHidden();
});

test('工作台：Telegram 账号入口位于设置连接分类，一级导航只有主要功能', async ({ page }) => {
  await page.goto('/settings#connections');
  const navigation = page.locator('.main-nav');
  await expect(navigation.getByRole('link')).toHaveCount(4);
  for (const name of ['新建任务', '任务', '作品库', '设置']) await expect(navigation.getByRole('link', { name, exact: true })).toBeVisible();
  await expect(navigation.getByRole('link', { name: /Telegram/ })).toHaveCount(0);
  await expect(page.getByRole('tab', { name: '连接', exact: true })).toHaveAttribute('aria-selected', 'true');
  await expect(page.locator('#settings-panel-connections')).toBeVisible();
  await expect(page.getByRole('heading', { name: '服务连接', exact: true })).toBeVisible();
  await expect(page.locator('#telegram-connect')).toBeVisible();
  await expect(page.locator('#telegram-password')).toBeHidden();
  await page.getByRole('tab', { name: '下载', exact: true }).click();
  await expect(page.locator('#settings-panel-connections')).toBeHidden();
  await expect(page.locator('[data-module-slot="telegram-account"] > *')).toHaveCount(0);
});

test('工作台：桌面与窄屏布局、字段键盘操作及导航焦点', async ({ page }, testInfo) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/');
  await waitForMetadata(page);
  await enterReview(page);
  await page.locator('#metadata-title').fill('用于视觉核对的合成作品');
  await page.keyboard.press('Tab');
  await expect(page.getByRole('button', { name: '明确清空标题', exact: true })).toBeFocused();
  await page.keyboard.press('Shift+Tab');
  await expect(page.locator('#metadata-title')).toBeFocused();
  await page.screenshot({ path: testInfo.outputPath('workspace-desktop.png'), fullPage: true });
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth + 1)).toBe(true);

  await page.setViewportSize({ width: 390, height: 844 });
  await expect(page.locator('[data-navigation-toggle]')).toBeVisible();
  await expect(page.locator('#workspace-sidebar')).toBeHidden();
  await page.locator('[data-navigation-toggle]').focus();
  await page.keyboard.press('Enter');
  await expect(page.locator('[data-navigation-toggle]')).toHaveAttribute('aria-expanded', 'true');
  await expect(page.locator('.main-nav').getByRole('link', { name: '设置', exact: true })).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(page.locator('[data-navigation-toggle]')).toHaveAttribute('aria-expanded', 'false');
  await expect(page.locator('[data-navigation-toggle]')).toBeFocused();
  await expect(page.locator('#workspace-sidebar')).toBeHidden();
  await page.screenshot({ path: testInfo.outputPath('workspace-mobile.png'), fullPage: true });
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth + 1)).toBe(true);
  for (const viewport of [{ width: 360, height: 800 }, { width: 768, height: 1024 }]) {
    await page.setViewportSize(viewport);
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth + 1), `home at ${viewport.width}px`).toBe(true);
  }

  await page.emulateMedia({ reducedMotion: 'no-preference' });
  const title = page.locator('#metadata-title');
  const normalTransition = await title.evaluate(node => Math.max(...getComputedStyle(node).transitionDuration.split(',').map(Number.parseFloat)));
  expect(normalTransition).toBeGreaterThan(0.001);
  await page.emulateMedia({ reducedMotion: 'reduce' });
  expect(await page.evaluate(() => matchMedia('(prefers-reduced-motion: reduce)').matches)).toBe(true);
  const reducedMotion = await title.evaluate(node => {
    const styles = getComputedStyle(node);
    return {
      transition: Math.max(...styles.transitionDuration.split(',').map(Number.parseFloat)),
      animation: Math.max(...styles.animationDuration.split(',').map(Number.parseFloat)),
      scroll: getComputedStyle(document.documentElement).scrollBehavior,
    };
  });
  expect(reducedMotion.transition).toBeLessThanOrEqual(0.001);
  expect(reducedMotion.animation).toBeLessThanOrEqual(0.001);
  expect(reducedMotion.scroll).toBe('auto');
  await page.emulateMedia({ reducedMotion: 'no-preference' });

  await page.setViewportSize({ width: 1440, height: 900 });
  page.once('dialog', dialog => dialog.accept());
  await page.locator('.main-nav').getByRole('link', { name: '设置', exact: true }).click();
  await expect(page.locator('#settings-panel-download')).toBeVisible();
  await expect(page.getByRole('tabpanel')).toHaveCount(1);
  await expect(page.locator('.main-nav .nav-link.active')).toHaveAccessibleName('设置');
  await expect(page.locator('.main-nav [aria-current="page"]')).toHaveAccessibleName('设置');
  await expect(page.locator('#settings-panel-ai')).toBeHidden();
  await expect(page.locator('#settings-panel-sources')).toBeHidden();
  await page.screenshot({ path: testInfo.outputPath('settings-desktop.png'), fullPage: true });
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth + 1)).toBe(true);
  await page.getByRole('tab', { name: 'AI 模型', exact: true }).click();
  await expect(page.locator('[data-module-slot="ai-settings"]').getByRole('button', { name: '保存 AI 设置', exact: true })).toBeVisible();
  await expect(page.locator('#settings-panel-download')).toBeHidden();
  await page.screenshot({ path: testInfo.outputPath('settings-ai-desktop.png'), fullPage: true });
  await page.getByRole('tab', { name: '书目来源', exact: true }).click();
  await expect(page.locator('[data-module-slot="source-settings"] details.source-card')).toHaveCount(3);
  await page.locator('[data-module-slot="source-settings"] details.source-card').first().locator('summary').click();
  await expect(page.getByRole('tabpanel')).toHaveCount(1);
  await expect(page.locator('#settings-panel-ai')).toBeHidden();
  await page.screenshot({ path: testInfo.outputPath('settings-sources-desktop.png'), fullPage: true });
  await page.setViewportSize({ width: 390, height: 844 });
  await expect(page.locator('#workspace-sidebar')).toBeHidden();
  await page.screenshot({ path: testInfo.outputPath('settings-mobile.png'), fullPage: true });
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth + 1)).toBe(true);
  for (const viewport of [{ width: 360, height: 800 }, { width: 768, height: 1024 }]) {
    await page.setViewportSize(viewport);
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth + 1), `settings at ${viewport.width}px`).toBe(true);
  }

  await page.setViewportSize({ width: 1440, height: 900 });
  await page.getByRole('tab', { name: '连接', exact: true }).click();
  await expect(page.locator('#telegram-connect')).toBeVisible();
  await expect(page.locator('#telegram-account-status')).not.toContainText('正在读取');
  await page.screenshot({ path: testInfo.outputPath('settings-connections-desktop.png'), fullPage: true });
  await page.locator('.main-nav').getByRole('link', { name: '任务', exact: true }).click();
  await expect(page.locator('#logs-page')).toBeVisible();
  await expect(page.locator('.main-nav .nav-link.active')).toHaveAccessibleName('任务');
  await expect(page.locator('.main-nav [aria-current="page"]')).toHaveAccessibleName('任务');
  await expect(page.locator('#logs-state-loading-desc')).toHaveCount(0);
  const badges = page.locator('#log-body .status-badge');
  for (const badge of await badges.all()) {
    await expect(badge).toHaveText(/\S/);
    expect(await badge.evaluate(node => getComputedStyle(node).backgroundColor)).not.toBe('rgba(0, 0, 0, 0)');
  }
  await page.screenshot({ path: testInfo.outputPath('tasks-desktop.png'), fullPage: true });
});
