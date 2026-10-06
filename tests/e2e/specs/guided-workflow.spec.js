const { test, expect, waitForMetadata } = require('../fixtures/auth');

const synthetic = [
  '《星 海 旅 记》',
  '剧情 介绍 :',
  '旅人于 12:30 出发（第 2 次）。',
  '他们找到星图。',
  '',
  '#奇 幻 #冒 险',
  '👍 12',
  '👁 250 18:40',
  'Leave a Comment',
  '[River Ink]Star Jour...zip',
  '40.5 MB',
  '[River Ink]Star Journey(Aster_x_Beryl)[星 光 汉 化]',
  '[#River Ink]星 海 旅 记（阿斯特×贝丽尔）[#星 光 汉 化]',
].join('\n');

async function mockPreparation(page, extract) {
  await page.route('**/api/settings/ai', route => route.fulfill({ json: { enabled: true, config_version: 7, protocol: 'llama_cpp_chat', base_url: 'https://synthetic.example/v1', model_id: 'synthetic-model', credential_present: true } }));
  await page.route('**/api/metadata/providers', route => route.fulfill({ json: { sources: [] } }));
  await page.route('**/api/metadata/extract', extract);
  await page.goto('/');
  await waitForMetadata(page);
  await page.locator('.ocr-edit-section > summary').click();
  await page.getByLabel('补充或手工录入文字（加入合并预览）').fill(synthetic);
}

test('自动准备：集中核对四核心字段，返回保留修正且不重复 AI，确认前零写入', async ({ page, request }) => {
  const beforeTasks = (await (await request.get('/api/tasks?per_page=100')).json()).tasks.map(task => task.id);
  let extractions = 0;
  const writes = [];
  page.on('request', request => {
    const path = new URL(request.url()).pathname;
    if (request.method() === 'POST' && (path === '/download' || path === '/api/tasks/upload/init' || path.endsWith('/copy-to-komga'))) writes.push(path);
  });
  await mockPreparation(page, route => {
    extractions += 1;
    const input = route.request().postDataJSON();
    expect(input.field_keys).toEqual(expect.arrayContaining(['title', 'aliases', 'creators.writer', 'creators.translator', 'summary', 'tags']));
    expect(input.text).not.toMatch(/Leave a Comment|40\.5 MB|Star Jour\.\.\.zip/);
    return route.fulfill({ json: { request_id: input.request_id, candidates: [], warnings: [] } });
  });
  await page.locator('#url').fill('https://telegra.ph/synthetic-guided-source');
  await expect(page.locator('#automatic-preparation')).toBeChecked();
  await page.getByRole('button', { name: '下一步', exact: true }).click();
  await expect(page.locator('#home-page')).toHaveAttribute('data-workflow-step', 'review');
  await expect(page.locator('#workflow-review-title')).toBeFocused();
  await expect(page.locator('#metadata-title')).toHaveValue('星海旅记');
  await expect(page.locator('#metadata-aliases')).toHaveValue('Star Journey');
  await expect(page.locator('#metadata-creators-writer')).toHaveValue('River Ink');
  await expect(page.locator('#metadata-creators-translator')).toHaveValue('星光汉化');
  await expect(page.locator('#metadata-series')).toHaveValue('');
  await expect(page.locator('#metadata-summary')).toHaveValue('旅人于 12:30 出发（第 2 次）。\n他们找到星图。');
  expect(extractions).toBe(1);
  expect(writes).toEqual([]);
  expect((await (await request.get('/api/tasks?per_page=100')).json()).tasks.map(task => task.id)).toEqual(beforeTasks);
  await page.locator('#metadata-title').fill('人工校对书名');
  await page.getByRole('button', { name: '上一步', exact: true }).click();
  await expect(page.getByLabel('补充或手工录入文字（加入合并预览）')).toHaveValue(synthetic);
  await expect(page.locator('#url')).toHaveValue('https://telegra.ph/synthetic-guided-source');
  await page.getByRole('button', { name: '下一步', exact: true }).click();
  await expect(page.locator('#metadata-title')).toHaveValue('人工校对书名');
  expect(extractions).toBe(1);
  expect(writes).toEqual([]);
  const persisted = await page.evaluate(() => JSON.stringify({ url: location.href, history: history.state, local: { ...localStorage }, session: { ...sessionStorage } }));
  expect(persisted).not.toMatch(/人工校对书名|星海旅记|River Ink/);
});

test('自动准备期间返回取消推进，迟到响应不能抢页面或键盘焦点', async ({ page }) => {
  let release;
  const pending = new Promise(resolve => { release = resolve; });
  let received = false;
  let settled = false;
  await mockPreparation(page, async route => {
    received = true;
    const input = route.request().postDataJSON();
    await pending;
    await route.fulfill({ json: { request_id: input.request_id, candidates: [], warnings: [] } }).catch(() => {});
    settled = true;
  });
  await page.getByRole('button', { name: '下一步', exact: true }).click();
  await expect.poll(() => received).toBe(true);
  await expect(page.locator('section[data-workflow-step="preparing"]')).toBeVisible();
  await page.getByRole('button', { name: '返回上一步', exact: true }).click();
  await expect(page.locator('#workflow-materials-title')).toBeFocused();
  await page.locator('#url').focus();
  release();
  await expect.poll(() => settled).toBe(true);
  await expect(page.locator('#home-page')).toHaveAttribute('data-workflow-step', 'materials');
  await expect(page.locator('#url')).toBeFocused();
  await expect(page.getByLabel('补充或手工录入文字（加入合并预览）')).toHaveValue(synthetic);
});
