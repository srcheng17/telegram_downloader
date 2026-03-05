# 元数据拆分与二期架构升级 Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 将标签/类型元数据拆分、优化移动端统计展示，并完成 Celery+Redis+Postgres 二期架构升级与停机迁移能力。

**Architecture:** 前端新增独立类型输入并在移动端默认折叠统计区；后端将元数据拆分持久化到任务存储并独立写入 ComicInfo；执行层在保持现有 orchestrator 接口的前提下新增 Celery worker 路径，并通过 Compose 提供完整部署拓扑。

**Tech Stack:** Flask, Python unittest, Playwright, Celery, Redis, PostgreSQL, Docker Compose

---

### Task 1: 元数据模型拆分（tags / genres）

**Files:**
- Modify: `templates/index.html`
- Modify: `static/index.js`
- Modify: `telegram_downloader/web/routes.py`
- Modify: `telegram_downloader/repositories/task_store.py`
- Modify: `telegram_downloader/services/image_downloader.py`
- Test: `tests/integration/test_download_submission_flow.py`
- Test: `tests/repositories/test_task_store_repository.py`
- Test: `tests/services/test_image_downloader_service.py`

**Step 1: Write failing tests**
- 在仓储测试中新增 `genres_raw/genres_normalized` 持久化断言。
- 在 integration 测试中新增 `/download` 对 `genres` 入参断言。
- 在 service 测试中新增 ComicInfo `Tags` 与 `Genre` 独立值断言。

**Step 2: Run tests to verify RED**
Run: `.venv/bin/python -m unittest tests.repositories.test_task_store_repository tests.integration.test_download_submission_flow tests.services.test_image_downloader_service`
Expected: FAIL（字段缺失或映射不符）

**Step 3: Minimal implementation**
- 增加前端 `genres` 输入和提交字段。
- 路由提取 `genres` 并规范化。
- TaskStore 增加新列与读写字段。
- 下载服务独立解析 `tags` 和 `genres`，分别写入 ComicInfo。

**Step 4: Verify GREEN**
Run same unittest command and ensure PASS.

**Step 5: Commit**
```bash
git add templates/index.html static/index.js telegram_downloader/web/routes.py telegram_downloader/repositories/task_store.py telegram_downloader/services/image_downloader.py tests/integration/test_download_submission_flow.py tests/repositories/test_task_store_repository.py tests/services/test_image_downloader_service.py
git commit -m "feat: split comic tags and genres metadata"
```

### Task 2: 移动端统计折叠（首页 + 日志）

**Files:**
- Modify: `templates/index.html`
- Modify: `templates/logs.html`
- Modify: `static/index.js`
- Modify: `static/logs.js`
- Modify: `static/style.css`
- Test: `tests/e2e/specs/logs-flow.spec.js`

**Step 1: Write failing e2e test**
- 添加移动端 viewport 下统计区默认折叠断言。

**Step 2: Run e2e target test (RED)**
Run: `npm run e2e:test -- --grep "移动端统计"`
Expected: FAIL（当前未折叠）

**Step 3: Minimal implementation**
- 统计容器包装为 `<details>`。
- mount 时根据 viewport 控制 `open`。
- 样式补齐折叠标题与内容间距。

**Step 4: Verify GREEN**
Run: `npm run e2e:test`
Expected: PASS

**Step 5: Commit**
```bash
git add templates/index.html templates/logs.html static/index.js static/logs.js static/style.css tests/e2e/specs/logs-flow.spec.js
git commit -m "feat: collapse summary panels by default on mobile"
```

### Task 3: 执行层升级到 Celery + Postgres 支持

**Files:**
- Modify: `telegram_downloader/repositories/task_store.py`
- Modify: `telegram_downloader/services/task_orchestrator.py`
- Add: `telegram_downloader/services/download_worker.py`
- Add: `telegram_downloader/celery_app.py`
- Add: `telegram_downloader/celery_tasks.py`
- Modify: `telegram_downloader/__init__.py`
- Modify: `requirements.txt`
- Test: `tests/services/test_task_orchestrator_service.py` (new)

**Step 1: Write failing orchestrator tests**
- 覆盖本地线程模式与 celery 模式分发。

**Step 2: Run RED tests**
Run: `.venv/bin/python -m unittest tests.services.test_task_orchestrator_service`
Expected: FAIL

**Step 3: Minimal implementation**
- 抽离下载执行函数（thread/celery 共用）。
- orchestrator 支持 `execution_backend=thread|celery`。
- 新增 celery app 与 task 定义。
- TaskStore 增加 postgres 连接与 SQL 适配。

**Step 4: Verify GREEN**
Run orchestrator tests + existing suites.

**Step 5: Commit**
```bash
git add telegram_downloader/repositories/task_store.py telegram_downloader/services/task_orchestrator.py telegram_downloader/services/download_worker.py telegram_downloader/celery_app.py telegram_downloader/celery_tasks.py telegram_downloader/__init__.py requirements.txt tests/services/test_task_orchestrator_service.py
git commit -m "feat: add celery worker backend and postgres task store support"
```

### Task 4: 迁移脚本与部署编排

**Files:**
- Add: `docker-compose.yml`
- Add: `scripts/migrate_sqlite_to_postgres.py`
- Modify: `README.md`
- Modify: `docs/PROJECT_UPDATES.md`

**Step 1: Write migration smoke check**
- 增加脚本 dry-run 或最小迁移校验逻辑。

**Step 2: Run migration script locally (dry run)**
Run: `.venv/bin/python scripts/migrate_sqlite_to_postgres.py --help`
Expected: usage 正常输出。

**Step 3: Implement compose + docs**
- 定义 web/worker/redis/postgres 服务。
- README 增加停机迁移步骤与回滚步骤。

**Step 4: Verify**
- `docker compose config` 成功。
- Python 全量 + E2E 全量通过。

**Step 5: Commit**
```bash
git add docker-compose.yml scripts/migrate_sqlite_to_postgres.py README.md docs/PROJECT_UPDATES.md
git commit -m "chore: add compose stack and sqlite-to-postgres migration guide"
```

### Task 5: 全量回归

**Files:**
- No code changes expected

**Step 1: Run tests**
Run: `.venv/bin/python -m unittest discover -s tests -p "test_*.py"`
Expected: PASS

**Step 2: Run e2e**
Run: `npm run e2e:test`
Expected: PASS

**Step 3: Final verification commands**
Run:
- `docker compose config`
- `git status --short`
Expected: compose 配置可解析，工作区仅保留预期改动。
