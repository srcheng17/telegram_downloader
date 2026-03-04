# CBZ 元数据与中文化改造 Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 在下载流程中支持手动输入漫画元数据，生成包含 `ComicInfo.xml` 的 `.cbz` 文件，命中重复文件时提供“生成新的CBZ / 取消并下载已有文件”分支，并将前端页面（项目名除外）统一中文。

**Architecture:** 在路由层扩展下载请求载荷（元数据+确认态协议），在任务存储层持久化元数据，在下载服务中按元数据生成 `ComicInfo.xml` 与 `.cbz` 命名；前端新增确认态 UI 与中文文案。保留现有去重和任务调度主干，仅在重复成功时从“直接复用”改为“先确认再决定”。

**Tech Stack:** Flask, SQLite, Jinja2, Vanilla JS, CSS, unittest, Playwright

---

### Task 1: 扩展任务存储模型以持久化 CBZ 元数据

**Files:**
- Modify: `tests/repositories/test_task_store_repository.py`
- Modify: `telegram_downloader/repositories/task_store.py`

**Step 1: Write the failing test**

在 `tests/repositories/test_task_store_repository.py` 新增测试：

```python
def test_create_task_persists_cbz_metadata_fields(self):
    self.store.create_task(
        {
            "id": "meta-task",
            "url": "https://telegra.ph/meta-task",
            "canonical_url": "https://telegra.ph/meta-task",
            "status": "PENDING",
            "start_time": 100,
            "error": None,
            "progress": 0,
            "total_images": 0,
            "image_concurrency": 2,
            "result_zip_path": None,
            "author": "作者A",
            "comic_name": "漫画B",
            "summary": "简介C",
            "tags_raw": "热血，冒险",
            "tags_normalized": "热血,冒险",
        }
    )

    row = self.store.get_task("meta-task")
    self.assertEqual(row["author"], "作者A")
    self.assertEqual(row["comic_name"], "漫画B")
    self.assertEqual(row["summary"], "简介C")
    self.assertEqual(row["tags_raw"], "热血，冒险")
    self.assertEqual(row["tags_normalized"], "热血,冒险")
```

**Step 2: Run test to verify it fails**

Run: `python -m unittest tests.repositories.test_task_store_repository.TaskStoreRepositoryTests.test_create_task_persists_cbz_metadata_fields -v`
Expected: FAIL（缺少字段或 KeyError）

**Step 3: Write minimal implementation**

在 `telegram_downloader/repositories/task_store.py` 中最小实现：

```python
_TASK_SELECT_FIELDS = """
    id, url, canonical_url, status, start_time, error,
    progress, total_images, image_concurrency, result_zip_path,
    author, comic_name, summary, tags_raw, tags_normalized
"""

self._ensure_column(conn, "tasks", "author", "TEXT")
self._ensure_column(conn, "tasks", "comic_name", "TEXT")
self._ensure_column(conn, "tasks", "summary", "TEXT")
self._ensure_column(conn, "tasks", "tags_raw", "TEXT")
self._ensure_column(conn, "tasks", "tags_normalized", "TEXT")
```

并同步 `_COLUMNS`、`_normalize_task_payload`、`INSERT/SELECT` 字段清单。

**Step 4: Run test to verify it passes**

Run: `python -m unittest tests.repositories.test_task_store_repository.TaskStoreRepositoryTests.test_create_task_persists_cbz_metadata_fields -v`
Expected: PASS

**Step 5: Commit**

```bash
git add tests/repositories/test_task_store_repository.py telegram_downloader/repositories/task_store.py
git commit -m "feat: persist cbz metadata fields in task store"
```

---

### Task 2: 扩展下载接口协议（元数据 + 重复确认态）

**Files:**
- Modify: `tests/web/test_routes_api_logs.py`
- Modify: `tests/integration/test_download_submission_flow.py`
- Modify: `telegram_downloader/web/routes.py`
- Modify: `telegram_downloader/services/task_orchestrator.py`

**Step 1: Write the failing test**

新增路由测试（示例）：

```python
def test_download_duplicate_success_json_requires_confirmation(self):
    # 预置 SUCCESS 且文件存在的任务
    # POST /download with json metadata
    # 断言 payload: ok=true, duplicate=true, needs_confirmation=true, download_url 存在
```

新增集成测试（示例）：

```python
def test_download_force_true_with_metadata_creates_new_task(self):
    # 先写入已成功任务
    # 再以 force=true + metadata 提交
    # 断言创建新 task 且 metadata 字段入库
```

**Step 2: Run test to verify it fails**

Run: `python -m unittest tests.web.test_routes_api_logs.WebRoutesApiLogsTests.test_download_duplicate_success_json_requires_confirmation tests.integration.test_download_submission_flow.DownloadSubmissionIntegrationTests.test_download_force_true_with_metadata_creates_new_task -v`
Expected: FAIL（返回载荷缺字段，或 submit_download 参数/入库字段不匹配）

**Step 3: Write minimal implementation**

在 `telegram_downloader/web/routes.py`：
- 新增 `_extract_cbz_metadata()` 与标准化函数（仅 trim，不做过度业务逻辑）
- `claim` 命中 `reuse_success` 时返回：

```python
{
  "ok": True,
  "duplicate": True,
  "needs_confirmation": True,
  "task_id": task_id,
  "download_url": download_url,
  "force_applied": force_download,
}
```

- 创建新任务时把 metadata 字段写入 pending task。
- `runtime.task_orchestrator.submit_download(...)` 增加 metadata 参数。

在 `telegram_downloader/services/task_orchestrator.py`：
- `submit_download` / `_run_download` 签名接收 `metadata`
- 传递给 `download_images(..., metadata=metadata)`

**Step 4: Run test to verify it passes**

Run: `python -m unittest tests.web.test_routes_api_logs tests.integration.test_download_submission_flow -v`
Expected: PASS

**Step 5: Commit**

```bash
git add tests/web/test_routes_api_logs.py tests/integration/test_download_submission_flow.py telegram_downloader/web/routes.py telegram_downloader/services/task_orchestrator.py
git commit -m "feat: add metadata payload and duplicate confirmation flow"
```

---

### Task 3: 在下载服务中生成 ComicInfo.xml 并输出 .cbz

**Files:**
- Modify: `tests/services/test_image_downloader_service.py`
- Modify: `telegram_downloader/services/image_downloader.py`

**Step 1: Write the failing test**

新增服务测试（示例）：

```python
def test_download_images_outputs_cbz_and_writes_comicinfo(self):
    metadata = {
        "author": "某作者",
        "comic_name": "某漫画",
        "summary": "某简介",
        "tags_raw": "热血，冒险",
        "tags_normalized": "热血,冒险",
    }
    cbz_path = logic.download_images(..., metadata=metadata)
    self.assertTrue(cbz_path.endswith('.cbz'))

    with zipfile.ZipFile(cbz_path, 'r') as archive:
        xml_text = archive.read('ComicInfo.xml').decode('utf-8')
    self.assertIn('<Writer>某作者</Writer>', xml_text)
    self.assertIn('<Series>某漫画</Series>', xml_text)
    self.assertIn('<Title>某漫画</Title>', xml_text)
    self.assertIn('<Summary>某简介</Summary>', xml_text)
    self.assertIn('<Tags>热血,冒险</Tags>', xml_text)
    self.assertIn('<Genre>热血,冒险</Genre>', xml_text)
```

新增占位符测试：作者/漫画名空时文件名含 `未知作者_未命名漫画_`。

**Step 2: Run test to verify it fails**

Run: `python -m unittest tests.services.test_image_downloader_service.ImageDownloaderServiceTests.test_download_images_outputs_cbz_and_writes_comicinfo -v`
Expected: FAIL（目前输出 zip 且无 ComicInfo.xml）

**Step 3: Write minimal implementation**

在 `telegram_downloader/services/image_downloader.py` 中新增：

```python
def normalize_tags(raw_tags: str) -> str:
    tokens = [part.strip() for part in str(raw_tags or '').replace('，', ',').split(',')]
    return ','.join([token for token in tokens if token])


def build_comicinfo_xml(metadata: dict) -> str:
    author = (metadata.get('author') or '').strip()
    comic_name = (metadata.get('comic_name') or '').strip()
    summary = (metadata.get('summary') or '').strip()
    tags = normalize_tags(metadata.get('tags_raw') or metadata.get('tags_normalized'))
    return f"""<?xml version=\"1.0\" encoding=\"utf-8\"?>
<ComicInfo>
  <Writer>{escape(author)}</Writer>
  <Series>{escape(comic_name)}</Series>
  <Title>{escape(comic_name)}</Title>
  <Summary>{escape(summary)}</Summary>
  <Tags>{escape(tags)}</Tags>
  <Genre>{escape(tags)}</Genre>
</ComicInfo>
"""
```

并完成：
- 产物后缀由 `.zip` 改 `.cbz`
- 文件名：`作者_漫画名_时间戳.cbz`（空值占位）
- 在临时目录写入 `ComicInfo.xml` 后再打包

**Step 4: Run test to verify it passes**

Run: `python -m unittest tests.services.test_image_downloader_service -v`
Expected: PASS

**Step 5: Commit**

```bash
git add tests/services/test_image_downloader_service.py telegram_downloader/services/image_downloader.py
git commit -m "feat: generate cbz with comicinfo metadata"
```

---

### Task 4: 调整下载文件接口为 CBZ 输出语义

**Files:**
- Modify: `tests/web/test_routes_api_logs.py`
- Modify: `telegram_downloader/web/routes.py`

**Step 1: Write the failing test**

在 `tests/web/test_routes_api_logs.py` 增加断言：

```python
self.assertIn('filename="sample.cbz"', response.headers.get('Content-Disposition', ''))
self.assertEqual(response.mimetype, 'application/vnd.comicbook+zip')
```

**Step 2: Run test to verify it fails**

Run: `python -m unittest tests.web.test_routes_api_logs.WebRoutesApiLogsTests.test_download_task_file_endpoint_returns_zip -v`
Expected: FAIL（当前 mimetype 为 application/zip）

**Step 3: Write minimal implementation**

在 `telegram_downloader/web/routes.py` 下载接口：

```python
return send_file(
    zip_path,
    as_attachment=True,
    download_name=os.path.basename(zip_path),
    mimetype="application/vnd.comicbook+zip",
    conditional=True,
)
```

> 变量名可保持 `zip_path`，但实际值允许 `.cbz`。

**Step 4: Run test to verify it passes**

Run: `python -m unittest tests.web.test_routes_api_logs.WebRoutesApiLogsTests.test_download_task_file_endpoint_returns_zip -v`
Expected: PASS

**Step 5: Commit**

```bash
git add tests/web/test_routes_api_logs.py telegram_downloader/web/routes.py
git commit -m "feat: expose cbz mime type in download endpoint"
```

---

### Task 5: 首页元数据输入与重复确认交互（中文）

**Files:**
- Modify: `templates/index.html`
- Modify: `static/index.js`
- Modify: `static/style.css`
- Modify: `tests/e2e/specs/logs-flow.spec.js`

**Step 1: Write the failing test**

在 `tests/e2e/specs/logs-flow.spec.js` 新增/调整：

```javascript
test('重复任务时可取消并下载已有文件', async ({ page }) => {
  await page.goto('/');
  await page.locator('#url').fill('https://www.telegra.ph/E2E-Success-Exists-01-01');
  await page.getByRole('button', { name: '开始下载' }).click();

  await expect(page.locator('#download-feedback')).toContainText('该文件已有下载');
  await page.getByRole('button', { name: '取消并下载已有文件' }).click();

  const [download] = await Promise.all([
    page.waitForEvent('download'),
    page.getByRole('button', { name: '下载已有文件' }).click(),
  ]);
  expect(download.suggestedFilename()).toContain('.cbz');
});
```

**Step 2: Run test to verify it fails**

Run: `npm run test:e2e -- tests/e2e/specs/logs-flow.spec.js -g "重复任务时可取消并下载已有文件"`
Expected: FAIL（UI 元素/文案/流程尚不存在）

**Step 3: Write minimal implementation**

- `templates/index.html` 增加元数据字段与确认操作区域：

```html
<div class="form-group">
  <label for="author">作者（可选）</label>
  <input id="author" name="author" type="text">
</div>
```

并加确认按钮：`生成新的CBZ`、`取消并下载已有文件`。

- `static/index.js`：
  - 提交时发送 `author/comic_name/summary/tags`
  - 命中 `needs_confirmation=true` 进入确认态
  - “生成新的CBZ”再次提交并带 `force=true`
  - “取消并下载已有文件”直接绑定 `download_url`
  - 全部提示改中文

- `static/style.css`：补充确认态与元数据区样式。

**Step 4: Run test to verify it passes**

Run: `npm run test:e2e -- tests/e2e/specs/logs-flow.spec.js -g "重复任务时可取消并下载已有文件"`
Expected: PASS

**Step 5: Commit**

```bash
git add templates/index.html static/index.js static/style.css tests/e2e/specs/logs-flow.spec.js
git commit -m "feat: add metadata form and duplicate confirmation ui"
```

---

### Task 6: 前端全页面中文化（项目名除外）

**Files:**
- Modify: `templates/base.html`
- Modify: `templates/logs.html`
- Modify: `templates/settings.html`
- Modify: `templates/index.html`
- Modify: `static/logs.js`
- Modify: `static/index.js`
- Modify: `tests/e2e/specs/logs-flow.spec.js`
- Modify: `tests/e2e/specs/settings.spec.js`

**Step 1: Write the failing test**

更新 e2e 断言以中文文案为准（示例）：

```javascript
await page.getByRole('button', { name: '保存设置' }).click();
await page.getByRole('button', { name: '筛选' }).click();
await page.getByRole('button', { name: '取消' }).click();
await expect(page.locator('#page-info')).toContainText('第 1 页');
```

**Step 2: Run test to verify it fails**

Run: `npm run test:e2e -- tests/e2e/specs/logs-flow.spec.js tests/e2e/specs/settings.spec.js`
Expected: FAIL（页面与脚本文案仍包含英文）

**Step 3: Write minimal implementation**

统一替换前端文案为中文：
- 导航：主页 / 日志 / 设置
- 日志页：筛选、重置、下载、取消、详情、分页
- 设置页：字段标题、说明、保存按钮
- 首页：统计卡、护栏说明、提交态提示

并将 `STATUS_CATALOG` 的 `label` 改中文（如 Pending→排队中），确保日志状态徽章中文化。

**Step 4: Run test to verify it passes**

Run: `npm run test:e2e -- tests/e2e/specs/logs-flow.spec.js tests/e2e/specs/settings.spec.js`
Expected: PASS

**Step 5: Commit**

```bash
git add templates/base.html templates/logs.html templates/settings.html templates/index.html static/logs.js static/index.js tests/e2e/specs/logs-flow.spec.js tests/e2e/specs/settings.spec.js telegram_downloader/constants.py
 git commit -m "feat: localize frontend ui text to chinese"
```

---

### Task 7: 全量回归与收尾

**Files:**
- Modify: `README.md`（补充 CBZ 与元数据说明）
- Modify: `docs/PROJECT_UPDATES.md`（记录行为变化）

**Step 1: Write the failing test**

无新增业务测试；先整理文档 TODO 清单并确认预期命令。

**Step 2: Run test to verify it fails**

若前面任务均完成，此处不要求制造失败；直接进入全量验证。

**Step 3: Write minimal implementation**

文档更新：
- 下载产物从 ZIP 调整为 CBZ
- 元数据字段与 ComicInfo 映射
- 重复文件确认交互说明

**Step 4: Run test to verify it passes**

Run:
- `python -m unittest discover -s tests -p "test_*.py"`
- `npm run test:e2e`

Expected: 全部 PASS

**Step 5: Commit**

```bash
git add README.md docs/PROJECT_UPDATES.md
git commit -m "docs: document cbz metadata workflow"
```

---

## Implementation Notes
- 全流程必须按 @superpowers:test-driven-development 执行（每个行为先红后绿）。
- 最终收尾前必须执行 @superpowers:verification-before-completion。
- 如要并行执行任务，使用 @superpowers:subagent-driven-development 拆分 UI 与后端独立任务。
