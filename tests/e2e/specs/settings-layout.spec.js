const { test, expect } = require('../fixtures/auth');

test('手机设置的七个分类全部直接可见且可点按', async ({ page }) => {
  for (const width of [390, 360]) {
    await page.setViewportSize({ width, height: 844 });
    await page.goto('/settings');
    const tabs = page.locator('.settings-tabs [role="tab"]');
    await expect(tabs).toHaveCount(7);
    const geometry = await tabs.evaluateAll((nodes) => nodes.map((node) => {
      const rect = node.getBoundingClientRect();
      return { left: rect.left, right: rect.right, top: rect.top, height: rect.height };
    }));
    expect(new Set(geometry.map((rect) => Math.round(rect.top))).size).toBe(2);
    for (const rect of geometry) {
      expect(rect.left).toBeGreaterThanOrEqual(0);
      expect(rect.right).toBeLessThanOrEqual(width);
      expect(rect.height).toBeGreaterThanOrEqual(44);
    }
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(width);
    await page.getByRole('tab', { name: '安全', exact: true }).click();
    await expect(page.locator('#settings-panel-security')).toBeVisible();
    await expect(page).toHaveURL(/\/settings#security$/);
  }
});

test('桌面设置保持单排分类且来源内容使用主区宽度', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/settings');
  const tabs = page.locator('.settings-tabs [role="tab"]');
  const rows = await tabs.evaluateAll((nodes) => new Set(nodes.map((node) => Math.round(node.getBoundingClientRect().top))).size);
  expect(rows).toBe(1);
  await page.getByRole('tab', { name: '书目来源', exact: true }).click();
  const panel = await page.locator('#settings-panel-sources').boundingBox();
  const content = await page.locator('#content').boundingBox();
  expect(panel.width).toBeGreaterThan(content.width * 0.75);
  await expect(page.locator('[data-module-slot="source-settings"] details.source-card')).toHaveCount(3);
});
