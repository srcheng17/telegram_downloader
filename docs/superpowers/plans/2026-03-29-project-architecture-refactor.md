# Project Architecture Refactor Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将当前项目重构为以任务领域为主轴、接口职责清晰、前端映射统一、运行部署标准化的可持续演进系统。

**Architecture:** 先建立基线与护栏，再以“任务领域收口”为核心切入，随后收敛接口层与前端映射，最后治理部署、文档与流程。重构按 Phase 0 → 4 分阶段执行，每个阶段都要求最小可验证交付和独立回归。

**Tech Stack:** Go, PostgreSQL, Redis Streams, Nginx, HTML/CSS/htmx, Vite, Playwright, Docker Compose

---

## Scope and execution notes

- 这是一个**全项目重构计划**，但实施时必须按阶段拆分提交；不要尝试单次完成所有 Phase。
- 每个 Phase 都应先补测试、再最小改造、再跑对应回归。
- 若某阶段暴露出额外子系统（例如 queue 层大重构、前端状态系统重建），应拆出新的 spec / plan，而不是在当前计划里继续膨胀。
- 提交节奏建议：**每个 Task 一次 commit**；若 Task 过大，继续向下切小。

## File structure map

### Existing areas that will be the primary touch points

- `internal/domain/`
  - 目标：沉淀任务状态、动作资格、命名、元数据、Komga 规则。
- `internal/app/`
  - 目标：沉淀 task use cases，减少 handler/worker 直接持有业务编排。
- `internal/httpapi/`
  - 目标：缩薄 legacy-facing 适配层。
- `internal/httpv2/`
  - 目标：收口 v2 数据模型与查询接口，避免继续承载兼容语义。
- `internal/worker/`
  - 目标：只保留执行/协调逻辑，上移业务规则。
- `internal/store/postgres/`
  - 目标：仓储仅表达持久化实现，不承载规则。
- `frontend/src/`
  - 目标：按页面组织模块，引入统一 view model mapper。
- `web/static/`, `web/templates/`
  - 目标：配合前端模块重构与日志/首页一致性验证。
- `docker-compose.yml`, `Dockerfile`, `deploy/nginx/`
  - 目标：构建、挂载、网关、健康检查与运行语义治理。
- `docs/runbooks/`, `docs/superpowers/`, `README.md`
  - 目标：架构、运行、开发约束与决策文档固化。

### Planned new documentation artifacts

- `docs/architecture/current-system-overview.md`
- `docs/architecture/target-architecture.md`
- `docs/architecture/task-domain-model.md`
- `docs/runbooks/development-workflow.md`
- `docs/runbooks/deploy-and-rollback.md`
- `docs/development/module-boundaries.md`
- `docs/development/frontend-module-guidelines.md`
- `docs/adr/` 下若干 ADR 文档

> 如果仓库当前没有 `docs/architecture`、`docs/development`、`docs/adr`，在对应任务中创建。

---

## Phase 0 — Baseline and guardrails

### Task 0.1: Capture architectural baseline documents

**Files:**
- Create: `docs/architecture/current-system-overview.md`
- Create: `docs/architecture/task-lifecycle-baseline.md`
- Create: `docs/architecture/config-inventory.md`
- Modify: `README.md`

- [ ] **Step 1: Gather current module and runtime facts**

Run:
```bash
find internal -maxdepth 2 -type d | sort
find frontend/src -maxdepth 2 -type f | sort
sed -n '1,220p' README.md
sed -n '1,260p' docker-compose.yml
```
Expected: 获得当前模块、前端源码、README、Compose 实际结构。

- [ ] **Step 2: Write baseline docs**

在 `docs/architecture/current-system-overview.md` 中记录：
- 当前运行拓扑（gateway → go-api → postgres/redis；go-worker 独立消费）
- 当前目录职责观察
- 当前 legacy / v2 / worker / store 的调用关系

在 `docs/architecture/task-lifecycle-baseline.md` 中记录：
- URL 任务生命周期
- 上传任务生命周期
- 取消、重试、下载、Komga copy 的当前路径

在 `docs/architecture/config-inventory.md` 中记录：
- 环境变量清单
- 挂载目录清单
- 隐性知识（bundle build、gateway upstream、Komga 挂载等）

- [ ] **Step 3: Link docs from README**

在 README 增加“架构与运行文档”小节，引用上述文档。

- [ ] **Step 4: Verify docs are readable**

Run:
```bash
sed -n '1,220p' docs/architecture/current-system-overview.md
sed -n '1,220p' docs/architecture/task-lifecycle-baseline.md
sed -n '1,220p' docs/architecture/config-inventory.md
```
Expected: 文档标题、结构、链接正确。

- [ ] **Step 5: Commit**

```bash
git add README.md docs/architecture/current-system-overview.md docs/architecture/task-lifecycle-baseline.md docs/architecture/config-inventory.md
git commit -m "docs: capture architecture and config baseline"
```

### Task 0.2: Freeze refactor guardrails in writing

**Files:**
- Create: `docs/development/module-boundaries.md`
- Create: `docs/development/testing-strategy.md`
- Modify: `README.md`

- [ ] **Step 1: Write module boundary rules**

在 `docs/development/module-boundaries.md` 明确：
- domain 不依赖 HTTP/DB/Redis/UI
- app 编排 use case，不含协议细节
- interfaces 只做协议适配
- infra 只做技术实现
- worker 只做异步执行协调
- legacy adapter 禁止新增业务规则

- [ ] **Step 2: Write testing strategy**

在 `docs/development/testing-strategy.md` 明确：
- domain / app / infra / e2e 的测试职责
- 何时补单测，何时补集成测试，何时补 E2E
- 重构提交的最小回归要求

- [ ] **Step 3: Reference guardrails from README**

增加“开发约束”入口。

- [ ] **Step 4: Review for actionability**

Run:
```bash
sed -n '1,220p' docs/development/module-boundaries.md
sed -n '1,220p' docs/development/testing-strategy.md
```
Expected: 规则具体、可执行，不是空泛口号。

- [ ] **Step 5: Commit**

```bash
git add README.md docs/development/module-boundaries.md docs/development/testing-strategy.md
git commit -m "docs: define refactor boundaries and testing strategy"
```

---

## Phase 1 — Task domain consolidation

### Task 1.1: Inventory task rules before moving them

**Files:**
- Create: `docs/architecture/task-domain-model.md`
- Modify: `internal/domain/`
- Modify: `internal/app/`
- Modify: `internal/httpapi/`
- Modify: `internal/worker/`

- [ ] **Step 1: Trace current task rule locations**

Run:
```bash
rg -n "retry|cancel|progress|status|komga|filename|metadata" internal/domain internal/app internal/httpapi internal/httpv2 internal/worker internal/store
```
Expected: 找到规则散落位置，形成迁移清单。

- [ ] **Step 2: Write the domain-model doc**

在 `docs/architecture/task-domain-model.md` 中明确：
- 任务类型
- 任务状态与合法流转
- `can_retry/can_cancel/can_download/can_copy_to_komga` 语义
- URL / 上传任务进度定义
- 结果文件和源文件语义

- [ ] **Step 3: Review doc against real code**

逐条比对当前代码，避免写成理想化假设。

- [ ] **Step 4: Commit**

```bash
git add docs/architecture/task-domain-model.md
git commit -m "docs: define task domain model"
```

### Task 1.2: Introduce focused domain packages for task rules

**Files:**
- Create: `internal/domain/task/status.go`
- Create: `internal/domain/task/actions.go`
- Create: `internal/domain/task/progress.go`
- Create: `internal/domain/task/status_test.go`
- Create: `internal/domain/task/actions_test.go`
- Create: `internal/domain/task/progress_test.go`
- Modify: existing task-related callers under `internal/app/`, `internal/httpapi/`, `internal/worker/`

- [ ] **Step 1: Write failing domain tests for status and actions**

示例测试内容：
```go
func TestCanRetryAllowsFailedAndCanceledTasks(t *testing.T) {}
func TestCanCancelOnlyAllowsActiveTasks(t *testing.T) {}
func TestURLProgressLabelStateBeforeDiscovery(t *testing.T) {}
```

- [ ] **Step 2: Run domain tests to verify failure**

Run:
```bash
go test ./internal/domain/task -count=1
```
Expected: FAIL，因为新包或函数尚未实现。

- [ ] **Step 3: Implement minimal domain helpers**

在 `status.go/actions.go/progress.go` 中实现：
- 状态枚举与流转判断
- 动作资格判断
- 进度阶段帮助函数

要求：不依赖 DB 或 HTTP 类型。

- [ ] **Step 4: Replace duplicated callers incrementally**

先替换最简单的重复逻辑，例如：
- retry / cancel eligibility
- URL progress phase判断

- [ ] **Step 5: Re-run focused tests**

Run:
```bash
go test ./internal/domain/task ./internal/httpapi ./internal/worker -count=1
```
Expected: PASS。

- [ ] **Step 6: Commit**

```bash
git add internal/domain/task internal/httpapi internal/worker internal/app
git commit -m "refactor: centralize task status and action rules"
```

### Task 1.3: Extract metadata normalization and naming rules

**Files:**
- Create: `internal/domain/metadata/normalize.go`
- Create: `internal/domain/metadata/normalize_test.go`
- Create: `internal/domain/naming/cbz.go`
- Create: `internal/domain/naming/cbz_test.go`
- Create: `internal/domain/komga/path.go`
- Create: `internal/domain/komga/path_test.go`
- Modify: current metadata/naming/komga callers in `internal/app/`, `internal/httpapi/`, `internal/worker/`, `cmd/server/`

- [ ] **Step 1: Write failing tests for metadata normalization**

测试至少覆盖：
- 作者支持 `,` `，` `#` `＃`
- 标签/类型支持空格、中文逗号、英文逗号、全角半角 #
- 去空格、去重

- [ ] **Step 2: Write failing tests for CBZ naming and Komga path**

测试至少覆盖：
- `作者_系列_漫画名_时间戳.cbz`
- 无系列时省略系列段
- 无系列目录时进入 `tanbokon`

- [ ] **Step 3: Run tests to verify failure**

Run:
```bash
go test ./internal/domain/metadata ./internal/domain/naming ./internal/domain/komga -count=1
```
Expected: FAIL。

- [ ] **Step 4: Implement minimal rule packages**

实现纯函数，不依赖具体 handler/store 类型。

- [ ] **Step 5: Migrate one caller at a time**

优先替换：
- 首页/后端元数据清洗
- 文件名拼接逻辑
- Komga 复制目标路径逻辑

- [ ] **Step 6: Re-run focused tests**

Run:
```bash
go test ./internal/domain/metadata ./internal/domain/naming ./internal/domain/komga ./internal/httpapi ./internal/app/tasks -count=1
```
Expected: PASS。

- [ ] **Step 7: Commit**

```bash
git add internal/domain/metadata internal/domain/naming internal/domain/komga internal/httpapi internal/app internal/worker cmd/server
git commit -m "refactor: centralize metadata naming and komga rules"
```

### Task 1.4: Introduce task-oriented application use cases

**Files:**
- Create: `internal/app/task/create_url_task.go`
- Create: `internal/app/task/init_upload_task.go`
- Create: `internal/app/task/attach_upload_source.go`
- Create: `internal/app/task/retry_task.go`
- Create: `internal/app/task/cancel_task.go`
- Create: `internal/app/task/copy_result.go`
- Create: `internal/app/task/list_logs.go`
- Create: `internal/app/task/interfaces.go`
- Create: corresponding `_test.go` files
- Modify: current orchestration call sites in `internal/httpapi/`, `internal/httpv2/`, `internal/worker/`

- [ ] **Step 1: Write failing tests around one use case first**

从 `RetryTask` 开始，示例：
```go
func TestRetryTaskRequeuesSameTaskID(t *testing.T) {}
func TestRetryTaskRejectsIneligibleTask(t *testing.T) {}
```

- [ ] **Step 2: Run focused app test**

Run:
```bash
go test ./internal/app/task -run Retry -count=1
```
Expected: FAIL。

- [ ] **Step 3: Implement interfaces and minimal use case**

将仓储、队列、文件动作抽象成接口，避免直接依赖具体 repo 类型。

- [ ] **Step 4: Port one endpoint/caller to the new use case**

优先把 `retry` 从 handler 内散逻辑迁移到 `internal/app/task/retry_task.go`。

- [ ] **Step 5: Repeat for cancel / copy_result / create_url_task / init_upload_task**

每个用例都遵循：先测试，再实现，再替换调用方。

- [ ] **Step 6: Run integration-focused tests**

Run:
```bash
go test ./internal/app/task ./internal/httpapi ./internal/httpv2 ./internal/worker -count=1
```
Expected: PASS。

- [ ] **Step 7: Commit**

```bash
git add internal/app/task internal/httpapi internal/httpv2 internal/worker
git commit -m "refactor: introduce task application use cases"
```

---

## Phase 2 — Interface layer and frontend convergence

### Task 2.1: Make legacy adapter thin and explicit

**Files:**
- Modify: `internal/httpapi/legacy_adapter.go`
- Modify: `internal/httpapi/task_actions.go`
- Modify: `internal/httpapi/*.go` related to logs/download/upload endpoints
- Test: `internal/httpapi/*_test.go`

- [ ] **Step 1: Write failing tests around adapter-only responsibilities**

示例：
```go
func TestLegacyAdapterMapsTaskViewWithoutAddingRules(t *testing.T) {}
func TestLegacyAdapterUsesDomainEligibilityFlags(t *testing.T) {}
```

- [ ] **Step 2: Run focused httpapi tests**

Run:
```bash
go test ./internal/httpapi -count=1
```
Expected: 至少有新增用例 FAIL。

- [ ] **Step 3: Refactor adapter to consume app/domain outputs**

目标：
- adapter 只做字段映射
- 规则判断来自 app/domain
- 减少对底层 store 字段的直译依赖

- [ ] **Step 4: Re-run tests**

Run:
```bash
go test ./internal/httpapi ./internal/httpv2 -count=1
```
Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add internal/httpapi internal/httpv2
git commit -m "refactor: thin legacy adapter responsibilities"
```

### Task 2.2: Introduce frontend log view-model mapper

**Files:**
- Create: `frontend/src/logs/view_model.js`
- Modify: `frontend/src/logs/table_render.js`
- Modify: `frontend/src/logs/index.js`
- Test: `frontend/src/tests/logs_view_model.test.mjs`
- Test: `frontend/src/tests/logs_table_render.test.mjs`

- [ ] **Step 1: Write failing tests for the log view-model mapper**

测试至少覆盖：
- 状态标签映射
- 进度文本映射
- 错误文本映射
- 动作按钮显隐
- URL/上传任务差异

- [ ] **Step 2: Run frontend tests for the new file**

Run:
```bash
npm run test:frontend -- logs_view_model
```
Expected: FAIL。

- [ ] **Step 3: Implement `mapTaskToLogViewModel`**

把当前散在 `table_render.js`、`index.js` 的视图判定收口到 `view_model.js`。

- [ ] **Step 4: Replace direct field branching in renderer**

`table_render.js` 只消费 view model，不再直接解释复杂状态。

- [ ] **Step 5: Run full frontend test suite**

Run:
```bash
npm run test:frontend
```
Expected: PASS。

- [ ] **Step 6: Build frontend bundle**

Run:
```bash
npm run build
```
Expected: PASS，`web/static/dist/*.bundle.js` 更新。

- [ ] **Step 7: Commit**

```bash
git add frontend/src/logs web/static/dist
git commit -m "refactor: add log view model mapper"
```

### Task 2.3: Reorganize homepage/settings frontend modules by page responsibilities

**Files:**
- Create: `frontend/src/home/api.js`
- Create: `frontend/src/home/state.js`
- Create: `frontend/src/home/actions.js`
- Create: `frontend/src/settings/api.js`
- Create: `frontend/src/settings/state.js`
- Modify: existing homepage/settings entry files
- Test: relevant `frontend/src/tests/*.test.mjs`

- [ ] **Step 1: Write a small failing test around extracted behavior**

例如首页历史回填或设置保存：
```js
it('maps metadata history records back into home form state', () => {})
it('maps download action mode in settings state', () => {})
```

- [ ] **Step 2: Run the focused test**

Run:
```bash
npm run test:frontend -- home
```
Expected: FAIL。

- [ ] **Step 3: Move page-specific state and actions into page folders**

保留原入口行为不变，先抽函数，再抽模块，避免一次性推翻。

- [ ] **Step 4: Re-run frontend tests and build**

Run:
```bash
npm run test:frontend
npm run build
```
Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add frontend/src/home frontend/src/settings web/static/dist
git commit -m "refactor: organize home and settings frontend modules"
```

---

## Phase 3 — Runtime, deployment, and configuration governance

### Task 3.1: Normalize runtime configuration and defaults

**Files:**
- Modify: `cmd/server/main.go`
- Modify: `cmd/worker/main.go`
- Modify: `internal/config/` (or create focused config package/files if absent)
- Modify: `.env.example`
- Modify: `README.md`
- Test: `internal/config/*_test.go`

- [ ] **Step 1: Inventory config loading locations**

Run:
```bash
rg -n "os.Getenv|LookupEnv|MustGetenv|KOMGA|PORT|REDIS|POSTGRES|TIMEOUT|RETRY" cmd internal
```
Expected: 找到所有零散配置入口。

- [ ] **Step 2: Write failing config tests**

测试至少覆盖：
- 默认值
- 必填项
- Komga 根目录解析
- 下载模式默认值

- [ ] **Step 3: Implement grouped config helpers**

按组收敛配置：
- app
- worker
- storage
- queue
- ops/debug

- [ ] **Step 4: Replace ad-hoc env reads gradually**

先替换 server/worker 入口，再替换散落 helper。

- [ ] **Step 5: Re-run focused tests**

Run:
```bash
go test ./internal/config ./cmd/server ./cmd/worker -count=1
```
Expected: PASS。

- [ ] **Step 6: Commit**

```bash
git add internal/config cmd/server cmd/worker .env.example README.md
git commit -m "refactor: normalize runtime configuration"
```

### Task 3.2: Standardize Compose/build/run workflow

**Files:**
- Modify: `docker-compose.yml`
- Modify: `Dockerfile`
- Modify: `scripts/verify_release_gates.sh`
- Create: `scripts/dev-up.sh`
- Create: `scripts/dev-down.sh`
- Create: `docs/runbooks/development-workflow.md`
- Create: `docs/runbooks/deploy-and-rollback.md`

- [ ] **Step 1: Write the workflow docs before changing scripts**

文档需明确：
- 开发态如何起 API/worker/frontend
- Compose 如何起全栈
- 改前端后何时必须 build
- 发布/回滚/健康检查怎么做

- [ ] **Step 2: Add workflow helper scripts**

脚本最小职责：
- `scripts/dev-up.sh`：启动本地或 Compose 开发栈
- `scripts/dev-down.sh`：停止开发栈

- [ ] **Step 3: Align Docker/Compose comments and defaults**

重点校准：
- 必需 volume
- Komga 挂载语义
- 健康检查说明
- frontend bundle 依赖说明

- [ ] **Step 4: Verify scripts and docs**

Run:
```bash
bash scripts/dev-up.sh --help || true
bash scripts/dev-down.sh --help || true
sed -n '1,240p' docs/runbooks/development-workflow.md
sed -n '1,240p' docs/runbooks/deploy-and-rollback.md
```
Expected: 脚本与文档可读，参数说明明确。

- [ ] **Step 5: Commit**

```bash
git add docker-compose.yml Dockerfile scripts/verify_release_gates.sh scripts/dev-up.sh scripts/dev-down.sh docs/runbooks/development-workflow.md docs/runbooks/deploy-and-rollback.md
git commit -m "chore: standardize development and deploy workflows"
```

---

## Phase 4 — Documentation, ADRs, and delivery workflow

### Task 4.1: Add ADRs for major architectural decisions

**Files:**
- Create: `docs/adr/0001-task-domain-centered-architecture.md`
- Create: `docs/adr/0002-keep-legacy-adapter-thin.md`
- Create: `docs/adr/0003-no-spa-for-current-frontend.md`
- Modify: `README.md`

- [ ] **Step 1: Draft ADRs from the approved spec**

每个 ADR 至少包含：
- 背景
- 决策
- 备选方案
- 取舍
- 后续影响

- [ ] **Step 2: Link ADRs from README or architecture index**

确保后续开发者能快速找到。

- [ ] **Step 3: Review ADR language for stability**

避免写“当前刚好这样”，要写“为什么这样长期合理”。

- [ ] **Step 4: Commit**

```bash
git add docs/adr README.md
git commit -m "docs: add architecture decision records"
```

### Task 4.2: Define contribution and review workflow for refactor-era development

**Files:**
- Create: `docs/development/refactor-delivery-workflow.md`
- Modify: `README.md`
- Optionally Modify: `.github/pull_request_template.md` (if the repo adopts one)

- [ ] **Step 1: Write the workflow doc**

内容包括：
- spec → plan → implementation → verification → merge
- 何时必须补测试
- 何时必须写 ADR
- 跨层改动需要怎样说明影响面
- 前端 bundle 更新要求

- [ ] **Step 2: If useful, add PR template prompts**

模板建议要求：
- touched layers
- tests run
- rollout risk
- docs updated

- [ ] **Step 3: Review for enforceability**

Run:
```bash
sed -n '1,220p' docs/development/refactor-delivery-workflow.md
[ -f .github/pull_request_template.md ] && sed -n '1,220p' .github/pull_request_template.md || true
```
Expected: 规则具体、可操作。

- [ ] **Step 4: Commit**

```bash
git add docs/development/refactor-delivery-workflow.md README.md .github/pull_request_template.md
git commit -m "docs: define refactor delivery workflow"
```

---

## Cross-phase verification gates

### Minimum verification after each backend-heavy task

Run:
```bash
go test ./... -count=1
```
Expected: PASS。

### Minimum verification after each frontend-heavy task

Run:
```bash
npm run test:frontend
npm run build
```
Expected: PASS。

### Verification after workflow/runtime tasks

Run:
```bash
bash scripts/verify_release_gates.sh
```
Expected: 全部门禁通过；若环境不允许全部执行，至少记录被阻断原因和已运行部分。

### Pre-merge full verification for major milestones

Run:
```bash
go test ./... -count=1
go test -race ./... -count=1
npm run test:frontend
npm run lint
npm run build
npm run e2e:test
```
Expected: PASS；若任何命令受环境阻断，必须记录命令、输出、阻断原因。

---

## Suggested execution batching

- **Batch A:** Task 0.1 → 0.2
- **Batch B:** Task 1.1 → 1.2
- **Batch C:** Task 1.3 → 1.4
- **Batch D:** Task 2.1 → 2.3
- **Batch E:** Task 3.1 → 3.2
- **Batch F:** Task 4.1 → 4.2

每个 Batch 完成后都应停下来 review 结构是否仍然 DRY / YAGNI，而不是机械推进到下一个阶段。

## Out of scope for this plan

以下事项暂不纳入本计划，除非后续单独立 spec：

- 全面替换前端为 React/Vue/Svelte
- 彻底改造数据库 schema 为全新模型
- 替换 Redis Streams 为其他队列系统
- 大规模 UI 视觉重设计
- 引入微服务拆分

## Completion criteria

当以下条件同时满足时，可认为本轮“项目整体重构优化”完成：

1. 任务领域规则已集中到 domain/app 主链路；
2. legacy adapter 与前端 view model 不再持有重复业务语义；
3. 配置、构建、Compose、runbook 已足够支撑新同学接手；
4. README 与开发文档能清楚说明架构、运行和提交流程；
5. 全量门禁（Go、frontend、build、E2E）可稳定通过。
