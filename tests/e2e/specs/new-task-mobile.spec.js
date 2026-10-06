const { test, expect, waitForMetadata, enterReview } = require('../fixtures/auth');

test('360px 向导：材料和核对分步显示，键盘焦点跟随页面且返回保留字段', async ({ page }) => {
  await page.setViewportSize({ width: 360, height: 800 });
  await page.goto('/');
  await waitForMetadata(page);
  const materials = page.locator('section[data-workflow-step="materials"]');
  const review = page.locator('section[data-workflow-step="review"]');
  await expect(materials).toBeVisible();
  await expect(review).toBeHidden();
  await expect(review).toHaveJSProperty('inert', true);
  for (const option of await page.locator('.input-mode-option').all()) {
    expect((await option.boundingBox()).height).toBeGreaterThanOrEqual(44);
  }
  await page.locator('#automatic-preparation').uncheck();
  await page.locator('[data-workflow-next]').focus();
  await page.keyboard.press('Enter');
  await expect(page.locator('#workflow-review-title')).toBeFocused();
  await expect(materials).toBeHidden();
  await expect(materials).toHaveJSProperty('inert', true);
  await page.keyboard.press('Tab');
  expect(await page.evaluate(() => Boolean(document.activeElement.closest('section[data-workflow-step="review"]')))).toBe(true);
  for (const key of ['title', 'creators-writer', 'series', 'number']) {
    await expect(page.locator(`#metadata-${key}`)).toBeVisible();
  }
  await page.locator('#metadata-title').fill('手机录入作品');
  const optional = page.locator('.metadata-group').filter({ has: page.locator('summary', { hasText: '简介与分类' }) });
  await optional.locator('summary').click();
  await page.locator('#metadata-summary').fill('手机录入简介');
  await optional.locator('summary').click();
  await expect(optional.locator('summary')).toContainText('已填写 1 项');
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth + 1)).toBe(true);

  await page.getByRole('button', { name: '确认并开始', exact: true }).click();
  await expect(page.locator('#download-feedback')).toContainText('返回上一步');
  await expect(page.locator('#download-feedback')).toBeInViewport();
  await page.getByRole('button', { name: '上一步', exact: true }).click();
  await expect(page.locator('#workflow-materials-title')).toBeFocused();
  await page.locator('#url').fill('https://telegra.ph/synthetic-mobile-source');
  await enterReview(page);
  await expect(page.locator('#metadata-title')).toHaveValue('手机录入作品');
  await expect(page.locator('#metadata-summary')).toHaveValue('手机录入简介');
  await expect(page.locator('[data-workflow-source]')).toContainText('synthetic-mobile-source');
});
