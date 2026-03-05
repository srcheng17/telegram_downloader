const path = require('path');
const { execFileSync } = require('child_process');
const { test, expect } = require('@playwright/test');

const rootDir = path.resolve(__dirname, '..', '..', '..');
const prepareScript = path.join(rootDir, 'tests', 'e2e', 'fixtures', 'prepare_e2e_state.py');
const pythonBin = path.join(rootDir, '.venv', 'bin', 'python');

function recordDownloadSubmissions(page) {
  const submissions = [];
  page.on('request', (request) => {
    if (!request.url().endsWith('/download') || request.method() !== 'POST') {
      return;
    }
    const body = request.postData() || '';
    submissions.push(Object.fromEntries(new URLSearchParams(body).entries()));
  });
  return submissions;
}

test.beforeEach(() => {
  execFileSync(pythonBin, [prepareScript], { cwd: rootDir, stdio: 'inherit' });
});

test('首页：下载护栏默认折叠，避免遮挡表单输入', async ({ page }) => {
  await page.goto('/');

  const guardrailsPanel = page.locator('details.guardrails-panel');
  await expect(guardrailsPanel).toBeVisible();
  await expect(guardrailsPanel).not.toHaveAttribute('open', '');
  await expect(page.locator('.guardrails-description')).toBeHidden();
});

test('移动端：首页与日志任务概览默认折叠', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto('/');
  await expect(page.locator('#summary-collapsible-home')).not.toHaveAttribute('open', '');

  await page.goto('/logs');
  await expect(page.locator('#summary-collapsible-logs')).not.toHaveAttribute('open', '');
});

test('首页 duplicate SUCCESS 取消分支：展示确认并仅下载已有文件', async ({ page }) => {
  const submissions = recordDownloadSubmissions(page);

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
  });

  await page.getByRole('button', { name: '取消并下载已有文件' }).click();
  await expect(page.getByRole('button', { name: '下载已有文件' })).toBeVisible();

  await page.waitForTimeout(300);
  expect(submissions).toHaveLength(1);

  const [download] = await Promise.all([
    page.waitForEvent('download'),
    page.getByRole('button', { name: '下载已有文件' }).click(),
  ]);

  expect(download.suggestedFilename()).toBe('e2e-success-download.zip');
});

test('首页 duplicate SUCCESS 确认分支：force=true 二次提交创建新任务', async ({ page }) => {
  const submissions = recordDownloadSubmissions(page);

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

  await expect(page.locator('#download-feedback')).toContainText('任务已加入队列');
  await expect(page.getByRole('button', { name: '查看日志' })).toBeVisible();
});

test('日志页：筛选、错误详情弹窗、取消任务', async ({ page }) => {
  await page.goto('/logs');

  await expect(page.locator('#summary-active-count')).toHaveText('2');

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
  await expect(pendingRow).toContainText('取消中');
});

test('日志页：下载预检失败停留当前页并提示错误', async ({ page }) => {
  await page.goto('/logs');

  await page.locator('#status-filter').selectOption('SUCCESS');
  await page.locator('#query-filter').fill('e2e-success-missing');
  await page.getByRole('button', { name: '筛选' }).click();

  const missingRow = page.locator('#log-body tr', { hasText: 'e2e-success-missing' });
  await expect(missingRow).toBeVisible();

  await missingRow.getByRole('button', { name: '下载' }).click();

  await expect(page).toHaveURL(/\/logs$/);
  await expect(page.locator('#logs-page')).toBeVisible();
  await expect(page.locator('#logs-feedback')).toContainText('缓存文件不可用。');
  await expect(page.locator('#logs-feedback')).toHaveClass(/feedback-error/);
  await expect(page.locator('#logs-feedback')).not.toContainText('下载已开始。');
});

test('日志页：成功任务可下载', async ({ page }) => {
  await page.goto('/logs');

  await page.locator('#status-filter').selectOption('SUCCESS');
  await page.locator('#query-filter').fill('e2e-success-download');
  await page.getByRole('button', { name: '筛选' }).click();

  const successRow = page.locator('#log-body tr', { hasText: 'e2e-success-download' });
  await expect(successRow).toBeVisible();

  const [download] = await Promise.all([
    page.waitForEvent('download'),
    successRow.getByRole('button', { name: '下载' }).click(),
  ]);

  expect(download.suggestedFilename()).toBe('e2e-success-download.zip');
});
