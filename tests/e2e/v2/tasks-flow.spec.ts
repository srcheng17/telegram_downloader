import { expect, test } from '@playwright/test';

test.describe('v2 tasks flow', () => {
  test('create -> cancel -> filter -> download', async ({ page }) => {
    test.skip(true, 'placeholder: enable after v2 pages are mounted by runtime router');

    await test.step('create task', async () => {
      await page.goto('/v2');
      await page.locator('#v2-url').fill('https://telegra.ph/demo-v2');
      await page.locator('#v2-create-submit').click();
      await expect(page.locator('#v2-create-result-text')).toContainText('任务已创建');
    });

    await test.step('cancel task from list page', async () => {
      await page.goto('/v2/tasks-ui');
      const firstRow = page.locator('#v2-tasks-table-body tr').first();
      await expect(firstRow).toBeVisible();
      await firstRow.locator('.v2-cancel-btn').click();
      await expect(page.locator('#v2-tasks-feedback')).toContainText('已提交取消');
    });

    await test.step('filter list by status and keyword', async () => {
      await page.locator('#v2-filter-status').selectOption('CANCELED');
      await page.locator('#v2-filter-query').fill('demo-v2');
      await page.getByRole('button', { name: '筛选' }).click();
      await expect(page).toHaveURL(/status=CANCELED/);
    });

    await test.step('download success artifact', async () => {
      const successRow = page.locator('#v2-tasks-table-body tr', { hasText: 'SUCCESS' }).first();
      await successRow.locator('.v2-download-btn').click();
      await expect(page).toHaveURL(/\/v2\/tasks/);
    });
  });
});
