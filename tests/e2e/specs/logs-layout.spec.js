const { test, expect } = require('../fixtures/auth');

const successID = 'e2e-task-0123456789abcdef0123456789abcdef';
const successURL = 'https://telegra.ph/' + 'long-comic-source-name-'.repeat(5);

test('任务表：桌面操作列可见，手机仅表格横向滚动', async ({ page }, testInfo) => {
  const logs = [
    {
      id: successID,
      url: successURL,
      status: 'SUCCEEDED',
      status_label: '已完成',
      available_actions: ['download'],
      progress: { current: 12, total: 12 },
      start_time: 1762531200,
      error: '',
    },
    {
      id: 'e2e-failed-layout',
      url: 'https://telegra.ph/failed-layout',
      status: 'FAILED',
      status_label: '失败',
      available_actions: ['retry'],
      retryable: true,
      start_time: 1762531201,
      error: '网络响应无效。'.repeat(25),
    },
  ];
  await page.route('**/api/logs**', route => route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: JSON.stringify({
      logs,
      total: logs.length,
      page: 1,
      per_page: 25,
      total_pages: 1,
      has_active_tasks: false,
      filters: { status: '', q: '' },
      summary: { active_tasks: 0, finished_tasks: 2, success_rate: 50, failed_tasks: 1, canceled_tasks: 0 },
      status_catalog: {
        SUCCEEDED: { label: '已完成', can_download: true },
        FAILED: { label: '失败', can_retry: true },
      },
    }),
  }));

  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/logs');
  const table = page.locator('.logs-table');
  const scrollRegion = page.getByRole('region', { name: '任务表格' });
  const successRow = page.locator('#log-body tr').filter({ hasText: successID });
  await expect(successRow).toBeVisible();
  await expect(successRow.locator('.status-success, .status-succeeded')).toHaveText('已完成');
  await expect(successRow.getByRole('button', { name: '下载' })).toBeVisible();
  await expect(page.getByRole('button', { name: '重试' })).toBeVisible();
  await expect(page.getByRole('button', { name: '详情' })).toBeVisible();
  const desktop = await page.evaluate(() => {
    const region = document.querySelector('#logs-page .table-container');
    const action = document.querySelector('#log-body tr:first-child .log-action-cell button');
    const idText = document.querySelector('#log-body tr:first-child td:first-child .log-cell-text');
    const cell = idText.closest('td');
    return {
      scrollWidth: region.scrollWidth,
      clientWidth: region.clientWidth,
      regionRight: region.getBoundingClientRect().right,
      actionRight: action.getBoundingClientRect().right,
      idHeight: idText.getBoundingClientRect().height,
      lineHeight: parseFloat(getComputedStyle(idText).lineHeight),
      idOverflow: cell.scrollWidth > cell.clientWidth + 1,
    };
  });
  expect(desktop.scrollWidth).toBeLessThanOrEqual(desktop.clientWidth + 2);
  expect(desktop.actionRight).toBeLessThanOrEqual(desktop.regionRight + 1);
  expect(desktop.idHeight).toBeGreaterThan(desktop.lineHeight * 1.5);
  expect(desktop.idOverflow).toBe(false);
  await expect(scrollRegion).toHaveAttribute('tabindex', '0');
  await page.screenshot({ path: testInfo.outputPath('tasks-desktop.png'), fullPage: true });

  for (const width of [390, 360]) {
    await page.setViewportSize({ width, height: 800 });
    await expect(page.locator('#workspace-sidebar')).toHaveCSS('visibility', 'hidden');
    await expect(table).toHaveCSS('display', 'table');
    const mobile = await page.evaluate(() => {
      const region = document.querySelector('#logs-page .table-container');
      return {
        pageWidth: document.documentElement.scrollWidth,
        viewportWidth: innerWidth,
        regionWidth: region.clientWidth,
        tableWidth: region.scrollWidth,
      };
    });
    expect(mobile.pageWidth).toBeLessThanOrEqual(mobile.viewportWidth + 1);
    expect(mobile.tableWidth).toBeGreaterThan(mobile.regionWidth + 300);
    await scrollRegion.focus();
    await expect(scrollRegion).toBeFocused();
    await scrollRegion.evaluate(node => { node.scrollLeft = node.scrollWidth; });
    await expect(successRow.getByRole('button', { name: '下载' })).toBeInViewport();
    if (width === 390) {
      await page.evaluate(() => window.scrollTo(0, 0));
      await page.screenshot({ path: testInfo.outputPath('tasks-mobile.png'), fullPage: true });
    }
  }
});
