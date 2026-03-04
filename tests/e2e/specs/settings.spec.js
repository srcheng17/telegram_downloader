const { test, expect } = require('@playwright/test');

test('设置页保存后会在会话内保持数值', async ({ page }) => {
  await page.goto('/settings');

  await page.locator('#task_concurrency').fill('3');
  await page.locator('#image_concurrency').fill('7');
  await page.locator('#timeout').fill('45');
  await page.locator('#retries').fill('6');
  await page.locator('#log_retention_days').fill('9');
  await page.locator('#file_retention_days').fill('11');

  await page.getByRole('button', { name: '保存设置' }).click();

  await expect(page).toHaveURL(/\/settings$/);
  await expect(page.locator('#task_concurrency')).toHaveValue('3');
  await expect(page.locator('#image_concurrency')).toHaveValue('7');
  await expect(page.locator('#timeout')).toHaveValue('45');
  await expect(page.locator('#retries')).toHaveValue('6');
  await expect(page.locator('#log_retention_days')).toHaveValue('9');
  await expect(page.locator('#file_retention_days')).toHaveValue('11');

  await page.goto('/');
  await page.goto('/settings');
  await expect(page.locator('#task_concurrency')).toHaveValue('3');
  await expect(page.locator('#image_concurrency')).toHaveValue('7');
});
