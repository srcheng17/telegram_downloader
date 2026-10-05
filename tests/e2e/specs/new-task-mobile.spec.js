const { test, expect, waitForMetadata } = require('../fixtures/auth');

test('手机新建页：基础信息先于采集区，可选字段可展开且提交反馈可见', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto('/');
  await waitForMetadata(page);

  const source = page.locator('.workspace-source');
  const metadata = page.locator('.metadata-workspace');
  const evidence = page.locator('.evidence-workspace');
  const [sourceBox, metadataBox, evidenceBox] = await Promise.all([source.boundingBox(), metadata.boundingBox(), evidence.boundingBox()]);
  expect(sourceBox.y).toBeLessThan(metadataBox.y);
  expect(metadataBox.y).toBeLessThan(evidenceBox.y);
  for (const option of await page.locator('.input-mode-option').all()) {
    const box = await option.boundingBox();
    expect(box.height).toBeGreaterThanOrEqual(44);
  }
  await page.locator('#url').focus();
  await page.keyboard.press('Tab');
  expect(await page.evaluate(() => Boolean(document.activeElement.closest('.metadata-workspace')))).toBe(true);

  for (const key of ['title', 'creators-writer', 'series', 'number']) {
    await expect(page.locator(`#metadata-${key}`)).toBeVisible();
  }
  const optional = page.locator('.metadata-group').filter({ has: page.locator('summary', { hasText: '简介与分类' }) });
  await expect(optional).toHaveJSProperty('open', false);
  await optional.locator('summary').click();
  await page.locator('#metadata-summary').fill('手机录入简介');
  await optional.locator('summary').click();
  await expect(optional.locator('summary')).toContainText('已填写 1 项');

  await page.locator('.metadata-submit-shortcut').click();
  await expect(page.locator('#create-task-action')).toBeInViewport();
  await expect(page.locator('.workspace-submit')).toHaveCSS('position', 'static');
  await page.getByRole('button', { name: '开始下载', exact: true }).click();
  await expect(page.locator('#download-feedback')).toContainText('请先输入 Telegraph 链接');
  await expect(page.locator('#download-feedback')).toBeInViewport();
});
