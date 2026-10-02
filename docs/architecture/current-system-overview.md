# 当前系统概览

本文描述 2026-10-03 优化后的运行契约；历史设计和迁移文档用于追溯。

## 运行拓扑

```text
gateway (nginx) -> go-api -> PostgreSQL
go-worker -> PostgreSQL
```

API 渲染 Go 模板并提供 `/download`、`/api/*` 兼容接口及 `/v2/settings`。Task Core 是当前任务生命周期的唯一运行主线，不使用 Redis 调度。历史仓储/迁移代码保留用于兼容和迁移，不能据此推断当前 runtime。

## 模块职责

| 模块 | 职责 |
| --- | --- |
| `cmd/server`、`cmd/worker` | 配置、数据库、迁移和依赖装配 |
| `internal/httpui` | 首页、日志、设置模板和静态资源 |
| `internal/httpapi` | 请求验证及 Task Core 到现有页面/API payload 的映射 |
| `internal/httpv2` | 设置 transport；其他 v2 类型/handler 保留供兼容代码使用 |
| `internal/app/taskcore` | 创建、去重、取消、重试、领取、进度和终态用例 |
| `internal/domain/taskcore` | 状态、转换、动作资格和进度语义 |
| `internal/app/tasks` | 共用产物路径验证、Komga 文件复制 |
| `internal/store/postgres` | SQL 仓储、设置、元数据历史及编号迁移 |
| `internal/worker/taskcore` | lease/heartbeat/recovery、执行取消、打包和清理 |
| `internal/downloader`、`internal/archive` | Telegraph 下载、受限解包、CBZ 内容生成 |

## 核心链路

URL：`POST /download -> Task Core service -> SQL 去重/READY -> worker claim -> downloader -> CBZ -> SUCCEEDED`。同 canonical URL 的活跃任务在事务 advisory lock 下复用；已有成功且可读产物要求确认，`force=true` 可新建。

上传：`POST /api/tasks/upload/init -> CREATED -> PUT upload-source -> READY -> worker -> CBZ -> SUCCEEDED`。API 限制源包体积，worker 限制成员数量和解压体积；成功清理源，失败/取消保留源以供重试。

日志：`GET /api/logs -> Task Core QueryTasks -> SQL 筛选/count/page -> taskcore_presenter -> frontend`；概览使用 SQL status aggregation，无一万条截断。

设置：`/v2/settings -> postgres.SettingsStore -> app_settings`；新建任务保存下载设置快照，worker 读取该快照。保留超时、重试、图片并发和成功动作模式；没有任务并发/日志保留/文件保留的 UI 承诺。

## 执行与文件边界

每次 claim 同时递增 retry attempt 和单调 generation。手动 retry 只重置 attempt，旧 generation 的 heartbeat、progress、终态与取消确认均被拒绝。worker 等待实际执行退出再确认取消，取消或过期执行不会发布结果。

物理 CBZ 路径包含 task ID 和 generation，展示文件名沿用元数据命名。API 通过共用 ArtifactAccess 检查真实路径及 symlink containment。新产物互不覆盖；Komga 仍按用户指定系列目录和显示文件名复制。

## 前端与打包

前端保持原生 JS、htmx 和 Vite。页面模块在 swap 和 history restore 时成对卸载/挂载，撤销在途请求、轮询和旧回调。Docker 构建静态资源；Compose 默认挂载检出资源，所以源码与 bundle 一起提交。E2E 使用镜像静态资源并隔离所有可写目录。
