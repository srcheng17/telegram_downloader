# Telegraph Downloader

## 声明

**本项目中的大部分代码由 AI（Google Gemini）生成和修改。** 它旨在作为一个功能原型和开发示例，可能未经过详尽的测试，请谨慎用于生产环境。

## 概述

Telegraph Downloader 是一个简单的 Web 应用，旨在帮助用户从 [Telegraph](https://telegra.ph/) 页面（通常用于发布漫画或图集）批量下载图片，并将它们打包为 CBZ 漫画文件（兼容历史 ZIP 下载记录）。

## 主要功能

*   **通过 URL 下载**：只需粘贴 Telegraph 页面的 URL 即可开始下载。
*   **通过压缩包上传生成 CBZ**：首页支持上传 ZIP / RAR / 7Z，按首页填写的元数据重新生成 CBZ。
*   **重复 URL 智能复用 + 强制重抓**：默认命中已下载文件时进入“确认生成新 CBZ”流程；可强制创建新任务。
*   **CBZ 元数据**：首页支持手动填写作者、漫画系列名、漫画名、简介、标签、类型（可空），并写入 `ComicInfo.xml`（`Writer/Series/Title/Summary/Tags/Genre`，其中 `Tags` 与 `Genre` 独立填写）。
*   **首页元数据历史**：首页展示最近 20 条填写过的元数据，点击即可回填；URL 记录会连同链接一起恢复，上传记录会自动切到上传模式。
*   **并发下载**：支持多线程并发下载图片，以提高效率。
*   **自动打包**：下载完成后，所有图片会自动打包成 `.cbz`，文件名规则为 `作者_[系列名]_漫画名_时间戳.cbz`（系列名为空则省略该段，作者/漫画名空值自动占位）。
*   **后端暂存 + 手动下载 / Komga 复制**：任务完成后产物会先存储在后端；日志页可按设置选择浏览器下载，或复制到固定 Komga 目录（系列名目录 / `tanbokon`）。
*   **上传任务日志化**：上传任务和 URL 任务共用同一张日志表，日志页会标识任务类型，并显示上传进度、失败重试、取消与成功动作。
*   **下载预检与页内错误反馈**：日志页成功任务在浏览器下载模式下会先预检文件状态；如果文件不可用会在当前页给出明确错误，不会跳离 Logs 页面。
*   **容错下载**：单张图片 404/失败会继续尝试其他图片；只要存在失败，该任务最终标记为失败并不给下载按钮。
*   **下载日志**：提供一个日志页面，可以查看所有下载任务的状态（中文标签）、进度和错误信息。
*   **任务看板与筛选**：首页/日志页提供任务概览指标；移动端默认折叠概览卡，日志支持按状态与关键词筛选，便于快速定位问题任务。
*   **准确取消反馈**：取消操作为异步流程，前端会按后端真实响应显示状态与提示，减少误导。
*   **可配置性**：
    *   可自定义并发数、下载超时和重试次数。
    *   可配置日志保留时间。
    *   可配置结果文件缓存保留时间（到期自动清理）。
*   **动态前端**：前端使用 htmx + 原生 JS 模块，实现无刷新页面切换；除项目名外页面文案均为中文。
*   **Docker 支持**：项目已完全容器化，并支持通过环境变量和卷挂载自定义下载路径。

## 技术栈

*   **后端主线**: Go（`go-api` + `go-worker`）
*   **前端**: HTML + CSS + htmx + 原生 JS（Vite 最小工程化）
*   **部署**: Docker Compose, Nginx, Redis Streams, PostgreSQL
*   **测试**: Go test, Playwright, GitHub Actions CI

## 后端架构（重构后）

默认生产拓扑：

`gateway -> go-api -> postgres/redis`

`go-worker` 独立消费 Redis Streams（consumer group + ack + pending reclaim），并按 v2 状态机推进任务生命周期。

核心目录：

```text
cmd/server/               # go-api 入口
cmd/worker/               # go-worker 入口
internal/httpui/          # 首页/日志/设置 UI（保持现有界面）
internal/httpapi/         # /download + /api/* legacy-facing 适配层
internal/httpv2/          # /v2/* API
internal/queue/v2/        # Redis Streams v2 队列抽象
internal/store/postgres/  # v2 任务仓储 + migrations runner
web/templates/            # Go 页面模板
web/static/               # 页面静态资源与构建产物
```

仓库当前只保留 Go 运行时主线；历史 Python compatibility runtime 已下线并转入 Git 历史参考。

## 测试分层（重构后）

```text
./...            # Go 单元/集成/竞态测试
tests/e2e/       # Playwright 端到端测试（可复现，自动拉起 Compose）
.github/workflows/ci.yml  # CI 门禁（go test + race + lint + e2e）
```

## 项目修改文档

本轮改动的分项说明见：`docs/PROJECT_UPDATES.md`（后端、前端、测试、CI 与容器运行命令汇总）。

## 架构与运行文档

- `docs/architecture/current-system-overview.md`：当前系统结构与模块职责基线
- `docs/architecture/task-lifecycle-baseline.md`：URL / 上传任务生命周期与动作路径基线
- `docs/architecture/config-inventory.md`：环境变量、卷挂载、运行隐性知识清单

## 开发约束

- `docs/development/module-boundaries.md`：重构期模块边界与职责约束
- `docs/development/testing-strategy.md`：测试分层与最低回归要求

## 前端源码与静态资源约定

- `frontend/src/` 是首页、日志页、设置页和应用壳层的源码入口；Vite 从这里构建运行时 bundle。
- 页面模板只直接引用 `web/static/dist/*.bundle.js`。
- 当前 Docker/Compose 运行链路不会在启动时自动执行 `vite build`，因此变更前端源码时，必须同时提交更新后的 `web/static/dist/*.bundle.js`。

## 运行测试

### Go 单元与竞态测试

```bash
go test ./...
go test -race ./...
```

### 前端检查与构建

```bash
npm ci
npm run test:frontend
npm run lint
npm run build
```

### Playwright 端到端测试（可复现）

```bash
npm ci
npm run e2e:install
npm run e2e:test
```

> `npm run e2e:test` 会自动执行 `docker compose up -d --build`，等待 `/readyz`，执行 Playwright，然后自动 `docker compose down` 清理容器。
>
> 本地运行前会先做一次 Playwright 浏览器 preflight：
> - macOS 本地默认优先尝试 `chrome`，再回退到 `chromium`
> - 可用 `E2E_BROWSER_PROJECT=chrome` 或 `E2E_BROWSER_PROJECT=chromium` 强制指定项目
> - 如需跳过 preflight，使用 `E2E_SKIP_BROWSER_PREFLIGHT=1 npm run e2e:test`
> - 若 preflight 报 `Permission denied (1100)` / `SIGABRT`，通常表示当前 macOS 会话不允许该 shell 启动浏览器自动化

### 发布级验收（2026-03-18）

推荐直接运行统一门禁脚本：

```bash
bash scripts/verify_release_gates.sh
```

脚本会顺序执行：

```bash
go test ./... && go test -race ./...
npm run test:frontend && npm run lint && npm run build
npm run e2e:test
```

若在 sibling worktree 复用仓库根虚拟环境，脚本会自动回退到 `../.venv/bin/pytest`。

如需额外做显式 Compose smoke：

```bash
INTERNAL_ENQUEUE_TOKEN=test-token docker compose up -d --build
curl -fsS http://localhost:5002/healthz
curl -fsS http://localhost:5002/readyz
docker compose down
```

> Compose smoke 依赖 `INTERNAL_ENQUEUE_TOKEN` 与可用 Docker 凭据会话（macOS 非交互 shell 可能遇到 keychain 访问失败）。
>
> 迁移步骤、回滚方案与已知问题见：`docs/runbooks/2026-03-16-fullstack-refactor-migration.md` 与 `docs/runbooks/2026-03-18-project-refactor-rollout.md`。

## 关键接口说明（新增）

*   `POST /download`
    *   支持元数据字段：`author`、`series_name`、`comic_name`、`summary`、`tags`、`genres`（表单或 JSON）。
    *   支持 `force` 参数（布尔语义）。
    *   命中已有成功文件时返回确认态（`needs_confirmation=true` + `download_url`）；用户可选择直接下载已有文件，或以 `force=true` 再次提交生成新 CBZ。
    *   若已有同 URL 活跃任务，仍会复用活跃任务避免重复并发。
*   `GET /api/tasks/<task_id>/download`
    *   下载任务产物（新任务为 CBZ；历史任务可为 ZIP）。
*   `HEAD /api/tasks/<task_id>/download`
    *   仅做下载可用性预检（前端用来避免错误时离开 Logs 页面）。

## 如何运行

### 1. 准备环境变量

```bash
cp .env.example .env
# 至少设置 INTERNAL_ENQUEUE_TOKEN
```

### 2. 启动 Compose 单主线（Go）

```bash
docker compose up -d --build
docker compose ps
```

### 3. 访问应用

在浏览器中打开 `http://localhost:5002`。

## Compose 单主线部署（Go）

```bash
docker compose up -d --build
docker compose ps
```

默认服务拓扑为：`gateway + go-api + go-worker + redis + postgres`。

- `gateway`（Nginx）统一对外暴露 `APP_PORT`（默认 `5002`）。
- 网关规则文件：`deploy/nginx/canary-go-full.conf`。
- 页面与 API 入口统一转发到 `go-api`（单主线，不再依赖 Python web/worker）。

可用以下命令验证服务健康：

```bash
curl -sS http://localhost:5002/healthz
curl -sS -w '\nHTTP %{http_code}\n' http://localhost:5002/readyz
```

返回体中 `service` 为 `go-backend` 表示网关已命中 Go 主线。

## SQLite 迁移到 Postgres（停机迁移）

1. 停止旧服务（冻结写入）。
2. 准备一个**可从迁移脚本所在主机直连**的 Postgres 实例（可为外部实例，或临时对 Compose 的 `postgres` 暴露端口）。
3. 执行迁移脚本（将 `<host>:<port>` 替换为实际可达地址）：

```bash
.venv/bin/python scripts/migrate_sqlite_to_postgres.py \
  --source-sqlite data/tasks.db \
  --target-postgres "postgresql://telegraph:telegraph@<host>:<port>/telegraph" \
  --truncate-target
```

> 注意：当前 `docker-compose.yml` 默认**不**对外发布 `postgres:5432`。

4. 迁移完成后使用 compose 启动新架构：

```bash
docker compose up -d --build
```

## 更新后重建（镜像与容器）

当代码或前端资源有变更时，建议重新构建并替换 Compose 服务：

```bash
docker compose down --remove-orphans
docker compose up -d --build
```
