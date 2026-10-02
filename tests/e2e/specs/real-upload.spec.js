const { test, expect } = require('@playwright/test');
const { execFileSync } = require('node:child_process');

const imageBase64 = 'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9Y9ZlN8AAAAASUVORK5CYII=';

test('真实上传：ZIP 经 PostgreSQL 和 worker 生成可下载的 CBZ', async ({ page, request }) => {
  const archive = execFileSync('python3', ['-c', `
import base64, io, sys, zipfile
image = base64.b64decode('${imageBase64}')
buffer = io.BytesIO()
with zipfile.ZipFile(buffer, 'w') as archive:
    archive.writestr('pages/02.png', image)
    archive.writestr('pages/01.png', image)
    archive.writestr('README.txt', 'ignored non-image file')
sys.stdout.buffer.write(buffer.getvalue())
`]);

  await page.goto('/');
  await page.getByLabel('上传压缩包').check();
  await page.setInputFiles('#archive_file', { name: 'real-upload.zip', mimeType: 'application/zip', buffer: archive });
  await page.locator('#author').fill('E2E 作者');
  await page.locator('#series_name').fill('E2E 系列');
  await page.locator('#series_number').fill('3');
  await page.locator('#comic_name').fill('E2E 漫画');

  const initResponsePromise = page.waitForResponse((response) =>
    response.url().includes('/api/tasks/upload/init') && response.request().method() === 'POST');
  await page.getByRole('button', { name: '开始下载' }).click();
  const initResponse = await initResponsePromise;
  expect(initResponse.status()).toBe(202);
  const { task_id: taskID } = await initResponse.json();
  expect(taskID).toBeTruthy();

  let completedTask;
  await expect.poll(async () => {
    const response = await request.get('/api/tasks?per_page=100');
    expect(response.ok()).toBeTruthy();
    completedTask = (await response.json()).tasks.find((task) => task.id === taskID);
    return completedTask?.status === 'FAILED'
      ? `FAILED: ${completedTask.error}`
      : completedTask?.status;
  }, { timeout: 30_000 }).toBe('SUCCEEDED');
  expect(completedTask.available_actions).toContain('download');
  expect(completedTask.progress.current).toBe(completedTask.progress.total);

  const download = await request.get(`/api/tasks/${taskID}/download`);
  expect(download.ok()).toBeTruthy();
  expect(download.headers()['content-disposition']).toContain('.cbz');
  execFileSync('python3', ['-c', `
import base64, io, sys, xml.etree.ElementTree as ET, zipfile
with zipfile.ZipFile(io.BytesIO(sys.stdin.buffer.read())) as archive:
    assert set(archive.namelist()) == {'ComicInfo.xml', '1.png', '2.png'}, archive.namelist()
    image = base64.b64decode('${imageBase64}')
    assert archive.read('1.png') == image
    assert archive.read('2.png') == image
    metadata = ET.fromstring(archive.read('ComicInfo.xml'))
    assert metadata.findtext('Writer') == 'E2E 作者'
    assert metadata.findtext('Series') == 'E2E 系列'
    assert metadata.findtext('Number') == '3'
    assert metadata.findtext('Title') == 'E2E 漫画'
`], { input: await download.body() });

  const copy = await request.post(`/api/tasks/${taskID}/copy-to-komga`, { data: {} });
  expect(copy.ok()).toBeTruthy();
  expect((await copy.json()).ok).toBe(true);
});
