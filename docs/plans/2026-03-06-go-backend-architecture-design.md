# Go 后端全量重构架构设计（API + Worker + Redis Streams）

> 目标：将现有 Python 后端全量迁移到 Go，并在不改动前端的前提下完成接口 100% 兼容，同时优化为可水平扩展的分层架构。

## 1. 背景与目标

当前系统已经存在 Go 网关/API 骨架，但下载执行仍桥接到 Python。目标是完成“全量 Go 化”：

- 前端接口路径、字段、状态码、错误文案保持兼容；
- Go API 与 Go Worker 职责彻底分离；
- 引入 Redis Streams 消费组实现可扩展任务队列；
- PostgreSQL 成为任务状态与业务数据唯一真相源（source of truth）。

## 2. 架构总览

### 2.1 组件划分

- `go-api`
  - 负责 HTTP 接口层：参数校验、任务创建、查询、取消、下载文件输出。
  - 入队职责：将任务投递到 Redis Streams。
  - 不执行下载逻辑。

- `go-worker`
  - 负责下载执行层：抓取页面、下载图片、打包 CBZ、写入 ComicInfo.xml、状态推进。
  - 消费 Redis Streams 消息，按任务状态机更新 PostgreSQL。

- `redis`（Streams）
  - 任务队列载体：`XADD` 入队、`XREADGROUP` 消费、`XACK` 确认。
  - 支持多 worker 并行扩展。

- `postgres`
  - 存储任务实体与状态演进。
  - 接口返回、筛选、统计均基于 DB。

- 文件存储（首期本地卷）
  - worker 写产物至挂载目录。
  - api 通过安全路径校验提供 GET/HEAD 下载。

### 2.2 核心时序

1. 前端调用 `POST /download`。
2. `go-api` 在 DB 创建 `PENDING` 任务并生成入队令牌（enqueue token）。
3. `go-api` `XADD` 到 stream。
4. `go-worker` 消费消息并原子切换到 `IN_PROGRESS`。
5. 下载完成后更新 `SUCCESS` + `result_zip_path`；异常更新 `FAILED`；取消更新 `CANCELED`。
6. 前端通过 `/api/summary`、`/api/logs`、`/api/tasks/{id}/download` 获取结果。

## 3. 数据模型与状态机

### 3.1 tasks（核心表）

首版建议字段：

- 基础：`id`, `url`, `canonical_url`, `status`, `start_time`, `end_time`
- 执行：`progress`, `total_images`, `image_concurrency`, `error`, `result_zip_path`
- 元数据：`author`, `series_name`, `comic_name`, `summary`, `tags_raw`, `tags_normalized`, `genres_raw`, `genres_normalized`
- 协同控制：`enqueue_token`, `claimed_by`, `claimed_at`, `heartbeat_at`, `cancel_requested_at`, `retry_count`, `version`

索引建议：

- `idx_tasks_status_start_time(status, start_time desc)`
- `idx_tasks_canonical_url_start_time(canonical_url, start_time desc)`
- `idx_tasks_heartbeat(status, heartbeat_at)`（恢复任务扫描）

### 3.2 task_events（审计表，可选但推荐）

记录状态迁移和关键行为：

- `task_id`, `event_type`, `from_status`, `to_status`, `payload_json`, `created_at`

用于排障、重放分析与运营审计。

### 3.3 状态机

保持兼容状态集合：

- 活跃态：`PENDING`, `IN_PROGRESS`, `CANCEL_REQUESTED`
- 终态：`SUCCESS`, `FAILED`, `CANCELED`

迁移路径：

- `PENDING -> IN_PROGRESS -> SUCCESS|FAILED`
- `PENDING|IN_PROGRESS -> CANCEL_REQUESTED -> CANCELED`

### 3.4 幂等与去重

`POST /download` 处理规则：

- 命中同 `canonical_url` 且活跃态：返回 `reuse_active`；
- 命中同 `canonical_url` 且 `SUCCESS` 且产物存在：返回 `reuse_success`；
- `force=true`：跳过 success 复用，但仍避免活跃任务重复并发。

## 4. API 兼容策略（严格 100%）

冻结并保持如下契约：

- `POST /download`
- `GET /api/summary`
- `GET /api/logs`
- `POST /api/tasks/{task_id}/cancel`
- `GET|HEAD /api/tasks/{task_id}/download`

兼容要点：

- 路径、字段名、状态码不变；
- 错误体固定：`{"ok": false, "message": "..."}`；
- `message` 文案保持历史兼容（前端存在映射）；
- `success_rate` 允许 `null`；
- `status_catalog` 保持 `label/can_cancel/can_download/terminal`。

## 5. Worker 执行与队列语义

### 5.1 入队与消费

- 入队：`XADD download_tasks * task_id=<id> enqueue_token=<token>`
- 消费：`XGROUP` + `XREADGROUP GROUP go-workers <consumer>`
- 完成：成功后 `XACK`。

### 5.2 任务领取保护

worker 处理前执行 DB 原子更新：

- 条件：`status='PENDING' AND enqueue_token=:token`
- 更新：`status='IN_PROGRESS'`, `claimed_by`, `claimed_at`, `heartbeat_at`, `version=version+1`

若更新行数为 0，视为已被其他消费者处理或状态已变化，直接 `XACK` 丢弃重复消息。

### 5.3 取消与中断

- cancel API 仅写：`status='CANCEL_REQUESTED'`, `cancel_requested_at=now()`。
- worker 在下载循环与重试间隔点轮询状态，检测后抛取消并落库 `CANCELED`。

### 5.4 失败与重试

- 任务内重试：网络瞬时错误按指数退避。
- 达到阈值：更新 `FAILED` 并持久化错误信息。
- 可选二级重试：消息重投 + `retry_count` 上限（后续迭代）。

### 5.5 崩溃恢复

后台恢复器周期扫描：

- Redis PEL 中长期 pending 消息；
- DB 中 `IN_PROGRESS` 且 `heartbeat_at` 超时任务。

首期保守策略：超时任务标记 `FAILED` 并记录“worker heartbeat timeout”，避免重复下载副作用。

## 6. 可观测性与运维

### 6.1 日志与追踪

- 统一结构化日志（JSON）；
- 全链路携带 `task_id`、`request_id`；
- 关键事件写 task_events。

### 6.2 指标

- API：请求量、延迟、错误率；
- Worker：消费速率、成功率、失败率、平均任务时长；
- Queue：stream 长度、PEL 大小、重试次数。

### 6.3 健康检查

- `go-api`: `/healthz` + DB/Redis 连通性检查（可分级）
- `go-worker`: 进程存活 + 最近心跳 + queue lag

## 7. 测试策略

### 7.1 单元测试

- handler 参数校验与响应契约；
- 去重/复用判定；
- 状态机合法迁移；
- 下载文件路径安全校验。

### 7.2 集成测试

- API + Postgres（testcontainers）；
- Worker + Redis Streams + Postgres（端到端任务执行）；
- 取消、失败、恢复场景。

### 7.3 兼容回归

复用现有前端接口用例，重点覆盖：

- `POST /download` 三分支响应；
- `/api/logs` 分页/筛选；
- `/api/summary` 派生字段；
- cancel/download 的状态码与 message。

## 8. 迁移与上线策略

### 8.1 阶段化

1. **阶段 A**：完成 go-worker 基础执行链路，与现有 go-api 打通。
2. **阶段 B**：网关切流到 go-api + go-worker 全量。
3. **阶段 C**：下线 Python web/worker（保留回滚镜像窗口）。

### 8.2 发布策略

- 先灰度（canary）按比例切量；
- 对比关键指标（成功率、任务时延、失败码分布）；
- 稳定后全量。

### 8.3 回滚

- 网关一键切回 Python 路由；
- Go 服务保留只读查询用于排障；
- 数据层同库不回滚 schema destructive changes（通过前向兼容迁移脚本保障）。

## 9. 非目标（首期不做）

- S3/MinIO 对象存储抽象；
- 跨地域多活；
- Outbox/Saga 全量一致性框架；
- 多租户隔离。

---

该设计满足：

- 全量 Go 重构；
- API 100% 前端兼容；
- 架构分层可扩展；
- Redis 复用、改造成本可控；
- 为后续演进（对象存储、outbox、高级重试）预留接口。
