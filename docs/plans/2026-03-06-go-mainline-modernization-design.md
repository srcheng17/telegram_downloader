# 全量优化设计（Go 单主线，一次性切换，UI 保持不变）

## 1. 目标与约束

### 1.1 目标
在不改变当前前端界面（视觉与交互）的前提下，完成项目架构与工程化全量优化：
- 后端收敛为 Go 单主线（Go API + Go Worker）
- 队列与任务执行语义现代化（可恢复、可观测、可运维）
- 前端实现工程化与模块化（但页面效果不变）
- 部署统一为 Docker Compose

### 1.2 已确认约束
- 优化范围：全部（前端、后端、工程化）
- UI 约束：保留现有前端界面与交互外观
- 发布模式：一次性集中改造后发布
- 兼容策略：可破坏升级（允许下线 legacy 接口）
- 部署目标：Docker Compose 单机拓扑

---

## 2. 目标架构

```text
gateway (nginx)
  -> go-api (页面 + API)
  -> postgres (业务真相)
  -> redis streams (异步调度)

go-worker
  -> 消费 redis stream
  -> 下载/打包
  -> 状态推进与事件记录
```

关键收敛原则：
1. Python runtime 不再参与生产热路径。
2. 网关只保留 Go upstream。
3. 任务模型只保留一套主模型与状态机。
4. 前端页面与样式保持不变，仅优化实现层。

---

## 3. 前端设计（界面不变，代码重构）

### 3.1 保持不变
- 页面路由与主要 DOM 结构：`/`、`/logs`、`/settings`、`/v2`、`/v2/tasks-ui`
- 视觉风格与样式输出（基于现有 `static/style.css`）
- 用户可见文案、按钮行为、筛选与下载流程

### 3.2 实现层优化
- 引入最小工程化：Vite + ES Modules + ESLint/Prettier
- 将现有脚本拆分为模块：
  - `api-client`
  - `state`
  - `ui bindings`
  - `polling/retry/error handling`
- 在 E2E 之外补模块级测试（核心状态流与错误分支）

---

## 4. 后端与数据流设计

### 4.1 核心状态机
`QUEUED -> RUNNING -> SUCCESS | FAILED | CANCELED`

取消路径：
`RUNNING/QUEUED -> CANCEL_REQUESTED -> CANCELED`

### 4.2 关键流程
1. 创建任务：落库 `QUEUED`，写入队列消息。
2. worker 执行：claim + `RUNNING`，下载打包，终态写回。
3. 取消任务：先标记 `CANCEL_REQUESTED`，worker 协作终止后写 `CANCELED`。
4. 下载制品：保留 HEAD 预检 + GET 下载。

### 4.3 队列语义
- 使用 Redis Streams consumer group
- 显式 ack
- pending reclaim（处理 worker 崩溃或网络抖动）
- 失败重试与终态补偿写入

---

## 5. 工程化与安全基线

### 5.1 CI/CD
- Go：`go test ./...`、`go test -race ./...`、`go vet ./...`
- Python：仅保留迁移/工具脚本相关测试（如存在）
- 前端：lint + 构建 + E2E smoke
- 合同测试：更新 Compose/Nginx 为 Go 单主线事实

### 5.2 安全
- 去除弱默认 token/密码
- 敏感环境变量缺失时启动失败
- 生产默认数据库 SSL（本地开发可单独覆盖）
- 内部接口统一鉴权策略，禁止“token 为空即放行”

### 5.3 迁移
- schema migration 纳入标准流程（发布步骤内执行）
- fresh DB 可一键初始化，不再依赖手工补 SQL

---

## 6. 可观测性与运维

### 6.1 健康检查
- `/healthz`: 进程存活
- `/readyz`: DB/Redis 连通 + schema 版本可用

### 6.2 指标
- 任务创建、状态迁移、任务耗时
- 队列 lag/pending 数量
- API 请求时延与错误率

### 6.3 日志
- 结构化日志（包含 request_id/task_id/worker/status_transition）
- 错误统一分级与可定位信息

---

## 7. 发布与回滚

### 7.1 一次性发布步骤
1. 冻结旧链路写入
2. 执行迁移与校验
3. 切换网关到 go-only upstream
4. 执行 smoke：创建/取消/下载/筛选/设置

### 7.2 回滚策略
- 预置上一版 compose 与 nginx 配置
- 保留切换前数据库快照
- 任一 P0 校验失败立即回切

---

## 8. 验收标准

1. 前端 UI 与现网保持一致（人工对比 + E2E 断言）
2. 单主线生效（网关、compose、文档一致）
3. 任务执行具备可恢复语义（ack + pending reclaim）
4. CI 门禁全绿且可复现
5. fresh DB 部署无缺表与启动失败问题

