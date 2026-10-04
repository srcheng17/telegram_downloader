const { test, expect, waitForMetadata } = require('../fixtures/auth');

let originalSettings = null;

test.beforeEach(async ({ request }) => {
  const response = await request.get('/v2/settings');
  expect(response.ok()).toBeTruthy();
  originalSettings = await response.json();
});

test.afterEach(async ({ request }) => {
  if (!originalSettings) {
    return;
  }
  const restoreResponse = await request.put('/v2/settings', {
    data: originalSettings,
  });
  expect(restoreResponse.ok()).toBeTruthy();
});

test('设置页保存后会在会话内保持核心运行参数', async ({ page }) => {
  await page.goto('/settings');

  await page.locator('#image_concurrency').fill('7');
  await page.locator('#timeout').fill('45');
  await page.locator('#retries').fill('6');
  await page.locator('input[name="download_action_mode"][value="komga_copy"]').check();

  await page.getByRole('button', { name: '保存设置', exact: true }).click();

  await expect(page).toHaveURL(/\/settings$/);
  await expect(page.locator('#image_concurrency')).toHaveValue('7');
  await expect(page.locator('#timeout')).toHaveValue('45');
  await expect(page.locator('#retries')).toHaveValue('6');
  await expect(page.locator('input[name="download_action_mode"][value="komga_copy"]')).toBeChecked();

  await page.goto('/');
  await waitForMetadata(page);
  await page.goto('/settings');
  await expect(page.locator('#image_concurrency')).toHaveValue('7');
  await expect(page.locator('#timeout')).toHaveValue('45');
  await expect(page.locator('#retries')).toHaveValue('6');
  await expect(page.locator('input[name="download_action_mode"][value="komga_copy"]')).toBeChecked();
});

test('AI 设置保留手填模型，凭据替换、保留与清除均不回显', async ({ page, request }) => {
  const syntheticCredential = 'e2e-synthetic-provider-token';
  await page.goto('/settings');
  await page.getByRole('tab', { name: 'AI 模型', exact: true }).click();
  await expect(page).toHaveURL(/\/settings#ai$/);
  const ai = page.locator('[data-module-slot="ai-settings"]');
  await expect(ai.getByLabel('模型 ID（可手动填写）', { exact: true })).toBeVisible();
  // Disabled with no API address: persistence is real and no provider is contacted.
  await ai.getByLabel('模型 ID（可手动填写）', { exact: true }).fill('synthetic-manual-model');
  await ai.getByRole('combobox', { name: '凭据操作', exact: true }).selectOption('replace');
  await ai.getByLabel('新凭据（仅替换时填写）', { exact: true }).fill(syntheticCredential);
  await ai.getByRole('button', { name: '保存 AI 设置', exact: true }).click();
  await expect(ai).toContainText('AI 设置已保存');
  await expect(ai.getByLabel('新凭据（仅替换时填写）', { exact: true })).toHaveValue('');
  const configured = await request.get('/api/settings/ai');
  const saved = await configured.json();
  expect(saved.credential_configured).toBe(true);
  expect(JSON.stringify(saved)).not.toContain(syntheticCredential);
  await page.reload();
  await expect(ai.getByLabel('模型 ID（可手动填写）', { exact: true })).toHaveValue('synthetic-manual-model');
  await expect(ai).toContainText('凭据已配置');
  await ai.getByRole('button', { name: '保存 AI 设置', exact: true }).click();
  await expect(ai).toContainText('AI 设置已保存');
  expect((await (await request.get('/api/settings/ai')).json()).credential_configured).toBe(true);
  await ai.getByRole('combobox', { name: '凭据操作', exact: true }).selectOption('clear');
  await ai.getByRole('button', { name: '保存 AI 设置', exact: true }).click();
  await expect(ai).toContainText('尚未配置凭据');
  expect((await (await request.get('/api/settings/ai')).json()).credential_configured).toBe(false);
});

test('来源设置真实保存与重载，连接测试展示有限验证范围', async ({ page, request }) => {
  await page.goto('/settings');
  await page.getByRole('tab', { name: '书目来源', exact: true }).click();
  const card = page.locator('[data-module-slot="source-settings"] details.source-card').filter({ has: page.locator('summary').filter({ hasText: 'Bangumi' }) });
  await card.locator('summary').click();
  const source = card.locator('form.integration-card');
  await source.getByLabel('启用来源', { exact: true }).check();
  await source.getByLabel('优先级（0–1000，越小越靠前）', { exact: true }).fill('25');
  await source.getByRole('button', { name: '保存来源设置', exact: true }).click();
  await expect(source).toContainText('来源设置已保存');
  const payload = await (await request.get('/api/settings/sources')).json();
  const saved = payload.sources.find(item => item.provider_id === 'bangumi');
  expect(saved.enabled).toBe(true);
  expect(saved.priority).toBe(25);
  expect(saved.config_version).toBeGreaterThan(0);
  await page.reload();
  await expect(page.getByRole('tab', { name: '书目来源', exact: true })).toHaveAttribute('aria-selected', 'true');
  await card.locator('summary').click();
  await expect(source.getByLabel('启用来源', { exact: true })).toBeChecked();
  await expect(source.getByLabel('优先级（0–1000，越小越靠前）', { exact: true })).toHaveValue('25');
  // Only the external probe response is controlled. Persistence/reload above are real;
  // public service protocol smoke is recorded separately from deterministic browser checks.
  await page.route('**/api/settings/sources/bangumi/test', route => route.fulfill({ json: {
    provider_id: 'bangumi', config_version: saved.config_version,
    status: 'passed', scope: 'public_catalog_only', checked_at: new Date().toISOString(),
  } }));
  await source.getByRole('button', { name: '测试已保存的来源', exact: true }).click();
  await expect(source).toContainText('连接测试通过');
  await expect(source).toContainText('仅验证公开目录可访问，未验证成人内容或受限条目权限');
  await expect(source).not.toContainText('public_catalog_only');
});

test('设置分类支持七个深链，只有当前面板可见并可用键盘切换', async ({ page }) => {
  const tabs = [
    ['download', '下载'], ['sources', '书目来源'], ['ai', 'AI 模型'],
    ['fields', '字段'], ['rules', '识别规则'], ['connections', '连接'], ['security', '安全'],
  ];
  for (const [id, label] of tabs) {
    await page.goto(`/settings#${id}`);
    const tablist = page.getByRole('tablist', { name: '设置分类', exact: true });
    await expect(tablist.getByRole('tab')).toHaveCount(7);
    await expect(tablist.getByRole('tab', { name: label, exact: true })).toHaveAttribute('aria-selected', 'true');
    await expect(page.locator(`#settings-panel-${id}`)).toBeVisible();
    await expect(page.getByRole('tabpanel')).toHaveCount(1);
    await expect(page.locator('#settings-page [data-tab-panel][hidden]')).toHaveCount(6);
  }

  await page.getByRole('tab', { name: '安全', exact: true }).focus();
  await page.keyboard.press('Home');
  await expect(page.getByRole('tab', { name: '下载', exact: true })).toBeFocused();
  await expect(page).toHaveURL(/\/settings#download$/);
  await page.keyboard.press('ArrowRight');
  await expect(page.getByRole('tab', { name: '书目来源', exact: true })).toBeFocused();
  await expect(page.locator('#settings-panel-sources')).toBeVisible();
  await page.keyboard.press('End');
  await expect(page.getByRole('tab', { name: '安全', exact: true })).toBeFocused();
  await expect(page.locator('#settings-panel-security')).toBeVisible();

  await page.goto('/settings#unknown-category');
  await page.reload();
  await expect(page.getByRole('tab', { name: '下载', exact: true })).toHaveAttribute('aria-selected', 'true');
  await expect(page.locator('#settings-panel-download')).toBeVisible();
  await expect(page.getByRole('tabpanel')).toHaveCount(1);
});

test('切换设置分类保留未保存的下载、AI 与来源字段，不提前写入服务端', async ({ page, request }) => {
  const savedAI = await (await request.get('/api/settings/ai')).json();
  const savedSources = await (await request.get('/api/settings/sources')).json();
  const savedBangumi = savedSources.sources.find(source => source.provider_id === 'bangumi');
  const draftModel = 'e2e-unsaved-model';
  const draftPriority = String(savedBangumi.priority === 321 ? 322 : 321);
  const draftTimeout = String(originalSettings.timeout === 67 ? 68 : 67);

  await page.goto('/settings#ai');
  const model = page.locator('[data-module-slot="ai-settings"]').getByLabel('模型 ID（可手动填写）', { exact: true });
  await expect(model).toHaveValue(savedAI.model_id);
  await model.fill(draftModel);
  await page.getByRole('tab', { name: '书目来源', exact: true }).click();
  const card = page.locator('[data-module-slot="source-settings"] details.source-card').filter({ has: page.locator('summary').filter({ hasText: 'Bangumi' }) });
  await card.locator('summary').click();
  const priority = card.getByLabel('优先级（0–1000，越小越靠前）', { exact: true });
  await priority.fill(draftPriority);
  await page.getByRole('tab', { name: '下载', exact: true }).click();
  await expect(page.locator('#timeout')).toHaveValue(String(originalSettings.timeout));
  await page.locator('#timeout').fill(draftTimeout);

  await page.getByRole('tab', { name: 'AI 模型', exact: true }).click();
  await expect(model).toHaveValue(draftModel);
  await expect(page.locator('#timeout')).toBeHidden();
  await page.getByRole('tab', { name: '书目来源', exact: true }).click();
  await expect(priority).toBeVisible();
  await expect(priority).toHaveValue(draftPriority);
  await expect(model).toBeHidden();
  await page.getByRole('tab', { name: '下载', exact: true }).click();
  await expect(page.locator('#timeout')).toHaveValue(draftTimeout);
  await expect(page.getByRole('tabpanel')).toHaveCount(1);

  expect((await (await request.get('/v2/settings')).json()).timeout).toBe(originalSettings.timeout);
  expect((await (await request.get('/api/settings/ai')).json()).model_id).toBe(savedAI.model_id);
  const afterSources = await (await request.get('/api/settings/sources')).json();
  expect(afterSources.sources.find(source => source.provider_id === 'bangumi').priority).toBe(savedBangumi.priority);
});

test('设置首次读取失败后切回可恢复，成功加载后不重复覆盖草稿', async ({ page }) => {
  const cases = [
    { tab: '下载', path: '/v2/settings', root: '#settings-panel-download', control: '#timeout', draft: '71' },
    { tab: 'AI 模型', path: '/api/settings/ai', root: '#settings-panel-ai', control: 'input[type="text"]', draft: 'e2e-recovered-model' },
    { tab: '书目来源', path: '/api/settings/sources', root: '#settings-panel-sources' },
  ];
  for (const item of cases) {
    item.reads = 0;
    await page.route(`**${item.path}`, async route => {
      if (route.request().method() !== 'GET') return route.continue();
      item.reads += 1;
      if (item.reads === 1) return route.fulfill({ status: 503, json: { code: 'unavailable' } });
      return route.continue();
    });
  }
  await page.goto('/settings');
  for (const item of cases) {
    await page.getByRole('tab', { name: item.tab, exact: true }).click();
    const panel = page.locator(item.root);
    await expect(panel).toContainText(item.tab === '下载' ? '读取失败' : '加载失败');
    await page.getByRole('tab', { name: '安全', exact: true }).click();
    await page.getByRole('tab', { name: item.tab, exact: true }).click();
    if (item.control) {
      const input = panel.locator(item.control).first();
      await expect(panel).not.toContainText('返回此分类时将重试');
      await expect(input).toBeVisible();
      await input.fill(item.draft);
      await page.getByRole('tab', { name: '安全', exact: true }).click();
      await page.getByRole('tab', { name: item.tab, exact: true }).click();
      await expect(input).toHaveValue(item.draft);
    } else await expect(panel.locator('details.source-card')).toHaveCount(3);
    expect(item.reads).toBe(2);
  }
});
