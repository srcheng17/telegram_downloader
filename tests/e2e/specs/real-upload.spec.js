const { test, expect, enterReview, waitForMetadata } = require('../fixtures/auth');
const { execFileSync } = require('node:child_process');

// Valid red, green and blue PNGs make page order observable after renumbering.
const pageImages = {
  1: 'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAIAAACQd1PeAAAADElEQVR4nGP4z8AAAAMBAQDJ/pLvAAAAAElFTkSuQmCC',
  2: 'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAIAAACQd1PeAAAADElEQVR4nGNg+M8AAAICAQB7CYF4AAAAAElFTkSuQmCC',
  10: 'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAIAAACQd1PeAAAADElEQVR4nGNgYPgPAAEDAQAIicLsAAAAAElFTkSuQmCC',
};

test('真实整链：一次确认、初始化丢响应恢复同任务、worker 保留图片及元数据并自动收录到 Komga', async ({ page, request }) => {
  test.setTimeout(90_000);
  expect(process.env.E2E_KOMGA_LIBRARY_ID, 'runner provisioned the isolated Komga library').toBeTruthy();
  expect(process.env.E2E_KOMGA_LIBRARY_ROOT, 'runner owns the shared synthetic library').toBeTruthy();
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

  const beforeTasks = (await (await request.get('/api/tasks?per_page=100')).json()).tasks.map(task => task.id);
  let initRequests = 0;
  let receiptReads = 0;
  let taskID = '';
  let confirmedPayload;
  let verifiedDelivery;
  page.on('response', async response => {
    if (response.request().method() !== 'POST' || !response.url().endsWith('/copy-to-komga') || !response.ok()) return;
    const payload = await response.json().catch(() => null);
    if (payload?.komga_indexed === 'verified') verifiedDelivery = payload;
  });
  // The real API commits the receipt, but the client never receives init's reply.
  await page.route('**/api/tasks/upload/init', async route => {
    initRequests += 1;
    confirmedPayload = route.request().postDataJSON();
    const response = await route.fetch();
    expect(response.status()).toBe(202);
    taskID = (await response.json()).task_id;
    await route.abort('connectionfailed');
  });
  page.on('request', request => { if (request.url().includes('/api/tasks/submissions/')) receiptReads += 1; });
  await page.goto('/');
  await waitForMetadata(page);
  await page.getByLabel('上传压缩包').check();
  await page.setInputFiles('#archive_file', { name: 'real-upload.zip', mimeType: 'application/zip', buffer: archive });
  await enterReview(page);
  await page.locator('#metadata-creators-writer').fill('E2E 作者');
  await page.locator('#metadata-series').fill('E2E 系列');
  await page.locator('#metadata-number').fill('3');
  await page.locator('#metadata-title').fill('E2E 漫画');
  await page.locator('#delivery-target').selectOption('komga');
  await page.locator('.metadata-group').filter({ has: page.locator('summary', { hasText: '简介与分类' }) }).locator('summary').click();
  await page.getByRole('button', { name: '明确清空简介', exact: true }).click();

  expect(initRequests).toBe(0);
  expect((await (await request.get('/api/tasks?per_page=100')).json()).tasks.map(task => task.id)).toEqual(beforeTasks);
  await page.getByRole('button', { name: '确认并开始' }).click();
  await expect(page.locator('section[data-workflow-step="result"]')).toBeVisible();
  expect(taskID).toBeTruthy();
  expect(initRequests).toBe(1);
  expect(receiptReads).toBeGreaterThan(0);
  expect(confirmedPayload.idempotency_key).toMatch(/^[a-zA-Z0-9_-]{16,128}$/);
  expect(confirmedPayload.file_sha256).toMatch(/^[a-f0-9]{64}$/);
  expect(confirmedPayload.delivery_target).toBe('komga');
  // Real replay keeps the original PostgreSQL task, including after worker starts.
  const replay = await request.post('/api/tasks/upload/init', { data: confirmedPayload });
  expect(replay.ok()).toBeTruthy();
  expect((await replay.json()).task_id).toBe(taskID);

  let completedTask;
  await expect.poll(async () => {
    const response = await request.get('/api/tasks?per_page=100');
    expect(response.ok()).toBeTruthy();
    completedTask = (await response.json()).tasks.find((task) => task.id === taskID);
    return completedTask?.status === 'FAILED'
      ? `FAILED: ${completedTask.error}`
      : completedTask?.status;
  }, { timeout: 30_000 }).toBe('SUCCEEDED');
  await expect(page.locator('[data-task-result]')).toContainText('Komga 已收录', { timeout: 45_000 });
  await expect.poll(() => verifiedDelivery?.book_id).toMatch(/^[A-Za-z0-9_-]{1,128}$/);
  expect(verifiedDelivery.library_id).toBe(process.env.E2E_KOMGA_LIBRARY_ID);
  const afterTasks = (await (await request.get('/api/tasks?per_page=100')).json()).tasks.map(task => task.id);
  expect(afterTasks.filter(id => !beforeTasks.includes(id))).toEqual([taskID]);
  await page.getByRole('button', { name: '查看已提交信息', exact: true }).click();
  await expect(page.locator('#metadata-title')).toHaveValue('E2E 漫画');
  await expect(page.getByRole('button', { name: '确认并开始', exact: true })).toBeHidden();
  await page.getByRole('button', { name: '返回处理结果', exact: true }).click();
  expect(initRequests).toBe(1);
  expect(completedTask.available_actions).toContain('download');
  expect(completedTask.progress.current).toBe(completedTask.progress.total);
  expect(completedTask.effective_metadata_document.fields.publisher.value).toBe('Original publisher');
  expect(completedTask.effective_metadata_document.fields.summary.state).toBe('cleared');
  expect(completedTask.metadata_document.fields.publisher).toBeUndefined();
  expect(JSON.stringify(completedTask)).not.toContain('source-archive.bin');

  const download = await request.get(`/api/tasks/${taskID}/download`);
  expect(download.ok()).toBeTruthy();
  expect(download.headers()['content-disposition']).toContain('.cbz');
  const artifact = await download.body();
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
`], { input: artifact });

  const catalogResponse = await request.get(`/api/komga/books/${verifiedDelivery.book_id}`);
  expect(catalogResponse.ok()).toBeTruthy();
  const catalogBook = await catalogResponse.json();
  expect(catalogBook.id).toBe(verifiedDelivery.book_id);
  expect(catalogBook.library_id).toBe(verifiedDelivery.library_id);
  expect(catalogBook.media_status).toBe('READY');
  expect(catalogBook.metadata.title).toBe('E2E 漫画');
  expect(catalogBook.metadata.number).toBe('3');
  expect(catalogBook.metadata.summary).toBe('');
  expect(catalogBook.metadata.authors).toContainEqual({ name: 'E2E 作者', role: 'writer' });

  // Compare the actual mounted Komga file with the downloaded worker artifact,
  // including every image and the full ComicInfo bytes; retry may not recopy it.
  const inspectCopy = () => JSON.parse(execFileSync('python3', ['-c', `
import hashlib, json, pathlib, sys
books = list(pathlib.Path(sys.argv[1]).rglob('*.cbz'))
assert len(books) == 1, 'delivery must create exactly one CBZ'
actual = books[0].read_bytes()
assert actual == sys.stdin.buffer.read(), 'delivered archive differs from worker artifact'
stat = books[0].stat()
print(json.dumps({'inode': stat.st_ino, 'mtime_ns': stat.st_mtime_ns, 'sha256': hashlib.sha256(actual).hexdigest()}))
`, process.env.E2E_KOMGA_LIBRARY_ROOT], { input: artifact }).toString());
  const firstCopy = inspectCopy();
  const copy = await request.post(`/api/tasks/${taskID}/copy-to-komga`, { data: {} });
  expect(copy.ok()).toBeTruthy();
  expect(await copy.json()).toMatchObject({ ok: true, komga_indexed: 'verified', book_id: verifiedDelivery.book_id, library_id: verifiedDelivery.library_id });
  expect(inspectCopy()).toEqual(firstCopy);
  const catalog = await request.get(`/api/komga/books?library_id=${encodeURIComponent(verifiedDelivery.library_id)}`);
  expect(catalog.ok()).toBeTruthy();
  expect((await catalog.json()).total_elements).toBe(1);
  const finalTasks = (await (await request.get('/api/tasks?per_page=100')).json()).tasks.map(task => task.id);
  expect(finalTasks.filter(id => !beforeTasks.includes(id))).toEqual([taskID]);
});
