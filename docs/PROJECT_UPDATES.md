# 项目修改梳理（2026-03-02）

本文档用于汇总本轮代码改动，便于后续联调、回归测试和部署。

---

## 增补（2026-03-07）：Task 11/12 收口（CI + 可复现 E2E + 切流证据）

### A. CI 与 E2E 可复现落地

- 新增 GitHub Actions：`.github/workflows/ci.yml`，统一执行：
  - `go test ./...`
  - `go test -race ./...`
  - `npm ci`
  - `npm run lint`
  - `npm run e2e:test`
- 新增 Playwright 项目配置：`tests/e2e/playwright.config.ts`。
- E2E 执行脚本统一为：`tests/e2e/run-e2e.sh`（自动拉起/回收 Compose，等待 `/readyz`，并处理无交互 Docker 凭据场景）。
- CI 增加 E2E 产物归档：`playwright-report`、`test-results`、`tests/e2e/.artifacts`。
- 更新 E2E 用例以匹配当前 Go 单主线路由与页面行为：
  - `tests/e2e/specs/logs-flow.spec.js`
  - `tests/e2e/specs/settings.spec.js`
  - `tests/e2e/specs/backend-smoke.spec.js`
  - `tests/e2e/v2/tasks-flow.spec.ts`

### B. 发布与回滚文档更新

- 更新 `README.md`：补充 Go 主线测试命令、可复现 E2E 说明、`/readyz` 健康探测。
- 更新切流清单：`docs/runbooks/v2-cutover-checklist.md`（加入 `lint`、`/readyz`、UI 页面可达性验收项）。
- 更新回滚清单：`docs/runbooks/v2-rollback-checklist.md`（加入 `/readyz` 与 UI 页面验收）。

### C. 最终演练证据

- 新增 `docs/runbooks/go-mainline-cutover-evidence.md`，记录 2026-03-07 的完整演练证据：
  - 自动化门禁（Go test / race / lint / e2e）
  - Compose 启停与 `docker compose ps`
  - `/healthz`、`/readyz`、`/`、`/logs`、`/settings`、`/v2`、`/v2/tasks` smoke
  - 回滚 dry-run（`docker compose down --remove-orphans`）

---

## 增补（2026-03-06）：Go 单主线切换完成态（Task 12 总验收）

### A. 架构完成态

- Compose 拓扑已收敛为单 Go 主线：`gateway + go-api + go-worker + postgres + redis`。
- 网关规则已收敛为全入口转发到 `go-api`（页面与 API 不再依赖 Python Web）。
- `go-api` 提供基础页面入口（`/`、`/logs`、`/v2`、`/v2/tasks-ui`）与 v2 API 契约，确保切流后页面不再 404。
- Python/Flask 路径保留为 legacy 兼容参考，不再作为当前主发布链路。

### B. 关键变更汇总（切流前）

- v2 核心链路：任务创建、列表、详情、取消、artifact 下载、settings、dashboard summary。
- v2 前端：`/v2` 创建页、`/v2/tasks-ui` 列表页，已完成与 `/v2/tasks*` 协议闭环。
- 迁移工具：支持 legacy -> v2 全量迁移、幂等 MIGRATED 事件、确定性 checksum 校验。
- Compose 合同测试：约束服务集合与网关路由收敛配置，防止回退到双栈。

### C. 验证结论（2026-03-06 实跑）

| 验收项 | 命令 | 结果 |
|---|---|---|
| Go 全量测试 | `go test ./...`（go-backend） | ✅ 通过 |
| 静态检查 | `go vet ./...`（go-backend） | ✅ 通过 |
| 竞态测试 | `go test -race ./...`（go-backend） | ✅ 通过 |
| E2E | `npm run e2e:test` | ✅ 通过（8/8） |
| Compose 配置解析 | `docker compose config` | ✅ 通过 |
| Compose 启动烟雾 | `docker compose up -d --build` | ✅ 通过（本机首次执行需先处理 Docker keychain 非交互凭据问题） |
| Schema 准备 | `psql < 002_v2_schema.sql` + `psql < 003_app_settings.sql` | ✅ 通过（`001` 依赖 legacy `tasks` 表，空库可跳过） |
| 切流后接口烟雾 | `curl http://localhost:5002/v2/dashboard/summary` / `curl http://localhost:5002/v2/tasks` | ✅ HTTP 200，返回合法 JSON |

> 备注：若在无交互会话中遇到 `osxkeychain` 凭据阻塞，可先解锁 keychain，或临时移除 `~/.docker/config.json` 的 `credsStore` 后重试（完成后恢复原配置）。

## 增补（2026-03-05）：类型字段拆分、移动端统计折叠与二期架构

### A. 元数据字段拆分（标签/类型）

- 首页新增“类型（可选）”输入字段（`genres`）。
- 后端新增 `genres_raw` / `genres_normalized` 持久化字段。
- `ComicInfo.xml` 映射调整为：
  - `Tags` <- 标签
  - `Genre` <- 类型
- 标签与类型支持中英文逗号分隔，且不再互相复用。

### B. 移动端统计展示优化（B 方案）

- 首页与日志页任务概览改为可折叠区域。
- 移动端默认折叠，桌面端默认展开，减少首屏占位。

### C. 二期架构升级（A 方案）

- 引入 `Celery + Redis + PostgreSQL`：
  - Web 仅负责任务受理与入队；
  - Worker 异步执行下载任务；
  - 任务存储升级为 `SQLite/PostgreSQL` 双兼容实现。
- 新增 `docker-compose.yml`，提供 `web/worker/redis/postgres` 一体化编排。
- 新增停机迁移脚本：`scripts/migrate_sqlite_to_postgres.py`。

## 增补（2026-03-06）：Go 全量重构上线验证与发布说明

### A. 新架构拓扑（go-api + go-worker）

- 新增 `go-api`（只负责 HTTP 契约与入队）与 `go-worker`（消费 Redis Streams 执行下载）。
- `go-api` 已接入 Redis Streams producer；`go-worker` 已接入真实 `Executor + Downloader` 执行链路。
- 新增网关配置 `deploy/nginx/canary-go-full.conf`：`/download`、`/api/*`、`/healthz` 全量转发到 `go-api`，页面请求仍走 Python `web`。

### B. 迁移与启动步骤（最小可运行）

1. 启动编排：
   ```bash
   docker compose up -d --build
   ```
2. 若目标库是全新 Postgres，需要先初始化 `tasks` 基表（可复用 Python 仓储建表逻辑），再执行：
   ```bash
   docker exec -i telegraph-postgres psql -U telegraph -d telegraph < go-backend/internal/store/postgres/migrations/001_go_full_rewrite.sql
   ```
3. 烟雾检查：
   ```bash
   curl -sS http://localhost:5002/healthz
   curl -sS http://localhost:5002/api/summary
   ```

### C. 回滚命令（切回 Python API）

1. 将 `deploy/nginx/canary-go-full.conf` 中 `/download`、`/api/*` 的 `proxy_pass` 切回 `http://telegraph_python_web`。
2. 重载网关：
   ```bash
   docker compose exec gateway nginx -s reload
   ```
3. 需要时停用 Go 链路：
   ```bash
   docker compose stop go-api go-worker
   ```
4. 回滚后验证（Python 可用接口）：
   ```bash
   curl -sS -o /dev/null -w "%{http_code}\n" http://localhost:5002/
   curl -sS -o /dev/null -w "%{http_code}\n" http://localhost:5002/api/summary
   ```

## 增补（2026-03-04）：CBZ 元数据与中文化

### A. CBZ 与 ComicInfo 元数据

- 下载产物默认由 ZIP 切换为 CBZ（兼容历史 ZIP 文件下载）。
- 新增 `ComicInfo.xml` 生成：
  - 作者 -> `Writer`
  - 漫画系列名 -> `Series`
  - 漫画名 -> `Title`
  - 详情介绍 -> `Summary`
  - 标签 -> `Tags`
  - 类型 -> `Genre`
- 标签支持中英文逗号输入并自动规范化。
- 文件名规则：`作者_[系列名]_漫画名_时间戳.cbz`；系列名为空时省略该段；作者/漫画名为空时自动使用占位符。
- 增强了文件名长度安全与 XML 非法字符清洗。

### B. 重复任务确认流程（前端）

- 首页提交命中“已有成功文件”时，不再直接复用下载，而是进入确认态：
  - `生成新的CBZ`（force=true 二次提交创建新任务）
  - `取消并下载已有文件`（不创建新任务）
- 首页新增元数据输入（作者、漫画系列名、漫画名、书籍详情介绍、标签、类型）。
- 下载护栏说明改为默认折叠，避免首屏遮挡输入区，按需展开查看。

### C. 全站中文化（项目名除外）

- 导航、首页、日志页、设置页及前端提示文案统一中文。
- `STATUS_CATALOG` 状态标签改为中文显示。

### D. 下载接口与 MIME

- 下载接口按扩展名动态返回 MIME：
  - `.cbz` -> `application/vnd.comicbook+zip`
  - `.zip` -> `application/zip`

### E. 测试覆盖

- Python 单测/集成新增并通过：
  - 元数据字段持久化
  - duplicate 确认协议
  - CBZ + ComicInfo 生成
  - MIME 动态映射
- Playwright E2E 新增并通过：
  - duplicate SUCCESS 取消分支（下载已有）
  - duplicate SUCCESS 确认分支（force 生成新任务）
  - 中文化后的日志/设置流程

## 1. 后端能力改动

### 1.1 重复任务复用与强制重抓

- 新增 Telegraph URL 规范化（`www`、协议、尾斜杠、query/fragment 去噪），统一比较键为 `canonical_url`。
- `POST /download` 默认优先复用同 URL 的最近成功任务（且 ZIP 文件仍存在）。
- 若存在同 URL 活跃任务（`PENDING/IN_PROGRESS/CANCEL_REQUESTED`），会复用活跃任务避免并发重复抓取。
- 支持 `force=true` 强制新建任务；但若已有同 URL 活跃任务，仍复用活跃任务。

### 1.2 下载资源护栏

- 新增下载限制常量（默认）：
  - 单任务最多 `300` 张图；
  - 单图最大 `25 MiB`；
  - 单任务总下载量最大 `500 MiB`。
- 限制触发时会返回清晰错误信息并将任务标记为失败，避免资源失控。

### 1.3 API 增强

- `POST /download`：新增 JSON 提交支持，并返回结构化 JSON 结果（包含 duplicate/active/force_applied）。
- `GET /api/summary`：新增任务看板统计接口（总数、活跃数、完成数、成功率等）。
- `GET /api/logs`：新增 `status` 与 `q` 过滤参数，响应中回传 filters 与 summary。
- `HEAD /api/tasks/<task_id>/download`：新增下载预检，供前端先确认文件是否可下载。

### 1.4 数据层与清理策略

- `tasks` 表新增 `canonical_url` 字段及索引，兼容旧数据自动补列。
- 新增原子化 `claim_download_task`，降低并发下重复建任务风险。
- 新增状态聚合、按条件分页过滤、按 ID 批量删除等仓储能力。
- 日志清理策略调整：
  - 日志保留期与文件保留期解耦；
  - 文件仍在保留期且存在时，任务日志可延后删除；
  - 过期文件、孤儿文件仍按策略清理。

## 2. 前端与交互改动

- 页面脚本模块化拆分：`static/app.js`、`static/index.js`、`static/logs.js`。
- 首页新增：
  - 任务概览卡片（Total/Active/Success/Failed）；
  - 下载护栏说明；
  - `Force re-download` 开关；
  - 提交反馈提示（成功/错误/信息）。
- 日志页新增：
  - 状态/关键词过滤；
  - 概览统计（Active/Finished/Success Rate/Failures）；
  - 下载前 `HEAD` 预检及页内错误提示；
  - 错误详情弹窗可访问性增强（Esc 关闭、焦点回退、Tab 焦点圈定）。
- 设置页补充字段说明与“仅影响新任务”的提示文案。

## 3. 测试与工程化改动

- 扩充 Python 测试：
  - 集成测试覆盖重复任务复用、force 行为；
  - 仓储层覆盖并发 claim、聚合、过滤；
  - 服务层覆盖下载护栏、日志清理策略；
  - Web 层覆盖 summary、过滤、download HEAD 预检。
- 新增 Playwright E2E：
  - 日志流（筛选、取消、错误弹窗、下载预检/下载成功）；
  - 设置页保存与会话保持；
  - 固定测试数据准备脚本 `tests/e2e/fixtures/prepare_e2e_state.py`。
- 新增 GitHub Actions CI：
  - Python unittest；
  - Playwright E2E。
- 忽略规则更新：
  - `.gitignore` 增加 `node_modules/`、`playwright-report/`、`test-results/`、`tests/e2e/.artifacts/`；
  - `.dockerignore` 增加 `data/`。

## 4. 镜像与容器（建议命令）

```bash
docker build -t telegraph-downloader:latest .
docker rm -f telegram-downloader 2>/dev/null || true
docker run -d -p 5002:5000 \
  -v "$(pwd)/downloaded_images:/app/downloaded_images" \
  -v "$(pwd)/temp_downloads:/app/temp_downloads" \
  -e SECRET_KEY='change-this-secret' \
  --name telegram-downloader \
  telegraph-downloader:latest
```

访问地址：`http://localhost:5002`
