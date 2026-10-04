const { test, expect, waitForMetadata } = require('../fixtures/auth');
const { execFileSync } = require('node:child_process');

// Valid red, green and blue PNGs make page order observable after renumbering.
const pageImages = {
  1: 'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAIAAACQd1PeAAAADElEQVR4nGP4z8AAAAMBAQDJ/pLvAAAAAElFTkSuQmCC',
  2: 'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAIAAACQd1PeAAAADElEQVR4nGNg+M8AAAICAQB7CYF4AAAAAElFTkSuQmCC',
  10: 'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAIAAACQd1PeAAAADElEQVR4nGNgYPgPAAEDAQAIicLsAAAAAElFTkSuQmCC',
};

test('真实上传：原包字段保留与明确清空，经 PostgreSQL 和 worker 生成 CBZ', async ({ page, request }) => {
  const archive = execFileSync('python3', ['-c', `
import base64, io, json, sys, zipfile
images = json.loads('${JSON.stringify(pageImages)}')
buffer = io.BytesIO()
with zipfile.ZipFile(buffer, 'w') as archive:
    for page in [10, 2, 1]:
        archive.writestr(f'pages/{page}.png', base64.b64decode(images[str(page)]))
    archive.writestr('README.txt', 'ignored non-image file')
    archive.writestr('ComicInfo.xml', '<ComicInfo><Title>Original title</Title><Summary>Explicitly remove this</Summary><Publisher>Original publisher</Publisher><Notes>Preserve this note</Notes></ComicInfo>')
sys.stdout.buffer.write(buffer.getvalue())
`]);

  await page.goto('/');
  await waitForMetadata(page);
  await page.getByLabel('上传压缩包').check();
  await page.setInputFiles('#archive_file', { name: 'real-upload.zip', mimeType: 'application/zip', buffer: archive });
  await page.locator('#metadata-creators-writer').fill('E2E 作者');
  await page.locator('#metadata-series').fill('E2E 系列');
  await page.locator('#metadata-number').fill('3');
  await page.locator('#metadata-title').fill('E2E 漫画');
  await page.locator('.metadata-group').filter({ has: page.locator('summary', { hasText: '简介与分类' }) }).locator('summary').click();
  await page.getByRole('button', { name: '明确清空简介', exact: true }).click();

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
  expect(completedTask.effective_metadata_document.fields.publisher.value).toBe('Original publisher');
  expect(completedTask.effective_metadata_document.fields.summary.state).toBe('cleared');
  expect(completedTask.metadata_document.fields.publisher).toBeUndefined();
  expect(JSON.stringify(completedTask)).not.toContain('source-archive.bin');

  const download = await request.get(`/api/tasks/${taskID}/download`);
  expect(download.ok()).toBeTruthy();
  expect(download.headers()['content-disposition']).toContain('.cbz');
  execFileSync('python3', ['-c', `
import base64, io, json, sys, xml.etree.ElementTree as ET, zipfile
images = json.loads('${JSON.stringify(pageImages)}')
with zipfile.ZipFile(io.BytesIO(sys.stdin.buffer.read())) as archive:
    assert set(archive.namelist()) == {'ComicInfo.xml', '0001.png', '0002.png', '0003.png'}, archive.namelist()
    for output_page, source_page in enumerate([1, 2, 10], start=1):
        assert archive.read(f'{output_page:04d}.png') == base64.b64decode(images[str(source_page)])
    metadata = ET.fromstring(archive.read('ComicInfo.xml'))
    assert metadata.findtext('Writer') == 'E2E 作者'
    assert metadata.findtext('Series') == 'E2E 系列'
    assert metadata.findtext('Number') == '3'
    assert metadata.findtext('Title') == 'E2E 漫画'
    assert metadata.findtext('Publisher') == 'Original publisher'
    assert metadata.findtext('Notes') == 'Preserve this note'
    assert metadata.find('Summary') is None
    assert metadata.findtext('PageCount') == '3'
`], { input: await download.body() });

  const copy = await request.post(`/api/tasks/${taskID}/copy-to-komga`, { data: {} });
  expect(copy.ok()).toBeTruthy();
  expect((await copy.json()).ok).toBe(true);
});
