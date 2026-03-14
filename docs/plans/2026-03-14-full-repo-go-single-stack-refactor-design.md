# 全仓 Go 单栈重构与可维护性优化设计（2026-03-14）

## 1. 背景与目标

当前仓库已经在 Go 主线上完成多轮收敛，但仍存在以下问题：

- 后端目录仍带有历史阶段命名（`httpapi` / `httpv2` / `service` / `store` 并存），职责边界不够清晰。
- 前端虽已迁移到 `frontend/src` + `static/dist`，但页面入口与共享逻辑组织仍可进一步统一。
- Python compatibility bridge 仍在仓库内占据“看似可运行”的位置，容易造成后续演进误解。
- API 与内部模型间适配代码分散，导致可维护性和重构信心不足。

本次设计目标：在允许长时间停机、允许接口大幅调整的前提下，完成“Go 单栈终态”设计，优先保障核心业务闭环（下载创建、日志查询、任务取消、产物下载），以稳定性与可维护性为第一优先级。

## 2. 用户确认约束（已达成）

- 范围：D（全仓）
- 优先级：A（稳定性 + 可维护性）
- 交付方式：A（分阶段渐进式）
- 业务影响：允许长时间停机，接口可大幅调整
- 技术终态：A（Go 单栈）
- 核心能力保留：A（下载主流程 + 任务日志/取消 + 文件下载）

## 3. 方案对比与选型

### 方案 1（采纳）：仓内渐进式强收敛重构

在现有仓库内分阶段重组目录、契约与状态流，逐步淘汰历史路径，最终收敛至单一 Go 运行时和单一 API 契约。

- 优点：迭代可控、可阶段验收、能复用既有测试与运维资产。
- 代价：过渡期存在新旧结构并行，重命名与迁移工作量大。

### 方案 2：新仓 clean-slate 重写

优点是架构最干净，但迁移切换风险和回归成本高，不符合“分阶段渐进式”偏好。

### 方案 3：先纯清理再重构

短期安全但收敛慢，不能快速解决结构性边界问题。

结论：采用方案 1。

## 4. 目标架构（终态）

运行时拓扑：

`gateway -> go-api -> postgres/redis`

`go-worker` 独立消费 Redis Streams 并执行下载任务。

关键原则：

1. Go 成为唯一业务运行时；Python 不再承载 Web/API 入口。
2. Postgres 是持久化真相来源；Redis 仅承担队列与短期协作状态。
3. 外部契约统一收敛到一套 API（以当前 v2 能力为基础重整命名与字段）。
4. 前端仅维护一个源码事实来源（`frontend/src`）。

## 5. 组件与目录重构设计

### 5.1 后端分层

`go-backend/internal` 收敛为四层：

- `api/http`: 路由、请求校验、响应映射、中间件。
- `application`: 用例编排（CreateDownload / ListTasks / CancelTask / GetArtifact）。
- `domain`: 任务状态机、幂等策略、命名规则、错误语义。
- `infrastructure`: Postgres 仓储、Redis Streams、文件存储实现。

### 5.2 现有模块迁移原则

- `httpapi` + `httpv2` 合并为统一 HTTP 层。
- `service` 逻辑按职责下沉到 `application` 与 `domain`。
- `store/postgres` 归位到 `infrastructure/postgres`（保留 migration runner）。
- `cmd/server` 与 `cmd/worker` 保留，启动 wiring 统一到 bootstrap。

### 5.3 前端重构原则

- 页面入口统一到 `frontend/src/pages/{home,logs,settings}`。
- 共享能力集中在 `frontend/src/shared/*`（轮询、模块装配、上传与下载行为）。
- 模板只引用 `static/dist/*.bundle.js`，避免历史脚本入口回流。

### 5.4 Python 代码定位

- `telegram_downloader/web` 从运行时路径降级为 legacy/tools。
- 保留迁移脚本、转换脚本与必要兼容工具，不再承载主线业务。

## 6. 核心数据流与状态机

### 6.1 统一状态机

任务状态统一为：

`PENDING -> RUNNING -> SUCCEEDED | FAILED | CANCELED`

所有状态迁移只允许由 application 用例驱动，禁止 handler/worker 绕过状态机直接写库。

### 6.2 下载创建流

1. HTTP 接收下载请求。
2. application 执行参数归一化与幂等判定。
3. 写入 Postgres（任务 + 事件）。
4. 写入 Redis Streams 入队。
5. worker 消费并执行下载。
6. 回写终态与产物元数据。

### 6.3 日志查询流

- 使用统一 filter builder 生成分页与筛选条件。
- 列表与计数共用条件构造，避免 SQL 逻辑漂移。

### 6.4 取消流

- 取消请求写入 `cancel_requested` 语义。
- worker 在可中断点检查并推进到 `CANCELED`。
- 明确区分“请求已受理”与“实际已停止”。

### 6.5 产物下载流

- application 校验任务终态与文件存在性后流式下载。
- 错误语义标准化：未完成、产物缺失、权限/参数错误。

### 6.6 幂等策略

- 以 `url + normalized_metadata` 计算 `idempotency_key`。
- 重复请求统一由 application 决策（复用旧任务或创建新版本），不在 HTTP 层分叉。

## 7. 测试、迁移与发布策略

### 7.1 测试分层

- Domain/Application：高覆盖单测（状态机、幂等、错误映射）。
- API/Infrastructure：集成测试（数据库、队列、下载接口）。
- E2E：仅保留核心闭环路径（创建、查询、取消、下载）。

### 7.2 CI 门禁

- 必跑：`go test ./...`、`go test -race ./...`、前端单测、前端构建、最小 e2e。
- Python 相关测试改为迁移期可选，不再作为主线强依赖。

### 7.3 停机迁移策略

1. 冻结写入并停机。
2. 导出旧任务与产物索引。
3. 应用新 schema/migration。
4. 回填与校验（数量、状态、产物引用一致性）。
5. 启动新链路并执行 smoke。

### 7.4 切换与回滚

- 切换：硬切到新 API 与新前端契约，移除 legacy 入口。
- 回滚：基于切换前 DB 快照 + 产物目录快照 + 镜像 tag 回退。

## 8. 分阶段实施框架（高层）

- Phase 1：后端分层骨架与统一错误模型。
- Phase 2：下载/日志/取消/产物四个核心用例迁移。
- Phase 3：worker 与 queue 协议收敛，状态机单一化。
- Phase 4：前端入口与数据契约全面切换。
- Phase 5：Python runtime 路径退役与文档/CI 清理。

## 9. 验收标准（DoD）

满足以下条件视为本轮重构完成：

1. 核心闭环（创建、日志、取消、下载）全部通过自动化与 smoke。
2. 代码结构满足四层边界，模块职责可追踪。
3. 主线运行与 CI 不再依赖 Python Web runtime。
4. Runbook、README、迁移文档与新契约一致。
5. 历史入口清理完成，无可误用的旧路径。

## 10. 设计确认记录

本设计由用户在 2026-03-14 分段确认通过：

- 第 1 部分：目标架构（确认）
- 第 2 部分：组件拆分与目录重构（确认）
- 第 3 部分：核心数据流与状态机（确认）
- 第 4 部分：测试、迁移与发布策略（确认）
