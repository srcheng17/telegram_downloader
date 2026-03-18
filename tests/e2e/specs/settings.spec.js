const { test, expect } = require('@playwright/test');

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

  await page.getByRole('button', { name: '保存设置' }).click();

  await expect(page).toHaveURL(/\/settings$/);
  await expect(page.locator('#image_concurrency')).toHaveValue('7');
  await expect(page.locator('#timeout')).toHaveValue('45');
  await expect(page.locator('#retries')).toHaveValue('6');
  await expect(page.locator('input[name="download_action_mode"][value="komga_copy"]')).toBeChecked();

  await page.goto('/');
  await page.goto('/settings');
  await expect(page.locator('#image_concurrency')).toHaveValue('7');
  await expect(page.locator('#timeout')).toHaveValue('45');
  await expect(page.locator('#retries')).toHaveValue('6');
  await expect(page.locator('input[name="download_action_mode"][value="komga_copy"]')).toBeChecked();
});
