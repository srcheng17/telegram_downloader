# 当前系统概览（Baseline）

## 目的

本文档用于在正式重构前冻结当前项目的系统形态，帮助后续改造时回答三个问题：

1. 当前系统由哪些运行组件组成；
2. 目录与模块大致各负责什么；
3. 请求、任务、日志这些主链路目前是如何串起来的。

该文档描述的是 **2026-06-05 的基线状态**，不是目标架构。

## 运行拓扑

当前默认单主线部署拓扑为：

```text
gateway (nginx)
  -> go-api
       -> postgres
go-worker
  -> postgres
```

补充说明：

- `gateway` 是统一对外入口，默认暴露 `APP_PORT=5002`。
- `go-api` 同时负责页面渲染（首页/日志/设置）与 `/download`、`/api/*`、`/v2/*` 接口。
- `go-worker` 通过 PostgreSQL `task_core_*` 表中的 ready 任务、lease、heartbeat 与 recovery 推进任务状态。
- `postgres` 存储 Task Core 任务、上传任务、元数据历史、设置和状态变化相关数据。

## 当前目录职责观察

### 运行入口

- `cmd/server/`
  - Go API 入口，负责配置装配、HTTP 路由、legacy/v2 接口和页面服务。
- `cmd/worker/`
  - Go worker 入口，负责领取 PostgreSQL Task Core 任务、执行任务、上报状态与进度。

### 后端主要模块

- `internal/httpui/`
  - HTML 页面相关 handler 与模板装配，服务首页、日志页、设置页。
- `internal/httpapi/`
  - legacy-facing API 与桥接适配层；当前既负责旧接口行为，也承担部分 v2 到 legacy 的映射。
- `internal/httpv2/`
  - v2 任务读写接口、任务模型、查询和上传链路相关操作。
- `internal/app/tasks/`
  - 已存在部分应用编排逻辑，例如 legacy bridge、任务摘要等。
- `internal/domain/`
  - 当前较薄，尚未形成完整“任务领域规则中心”。
- `internal/downloader/`
  - Telegraph 页面解析、图片抓取、下载过程控制与进度回调。
- `internal/archive/`
  - ZIP/RAR/7Z 处理与 CBZ 打包相关能力。
- `internal/service/`
  - 若干与 artifact / 任务服务相关的运行时能力。
- `internal/worker/`
  - Task Core worker 执行链路、lease heartbeat、过期任务 recovery 与运行时协调逻辑。
- `internal/store/postgres/`
  - PostgreSQL 仓储与 migrations。

### 前端主要模块

- `frontend/src/home/`
  - 首页相关行为：模式切换、元数据历史、上传、表单提交、提示、概览等。
- `frontend/src/logs/`
  - 日志页相关行为：表格渲染、筛选、下载预检、任务动作、错误弹窗。
- `frontend/src/shared/`
  - 跨页面共享逻辑：轮询、消息本地化、页面模块挂载、上传归一化等。
- `web/templates/`
  - 服务端模板。
- `web/static/dist/`
  - 构建后的前端 bundle；当前运行链路直接引用这里的产物。

## 当前后端调用关系（观察版）

### URL 下载主链路

```text
Web Form / /download
  -> internal/httpapi
  -> 可能经过 LegacyAdapter / app/tasks bridge
  -> Task Core service 写入 PostgreSQL
  -> go-worker 领取 READY 任务并持有 lease
  -> downloader 抓取页面与图片
  -> archive / artifact 生成 CBZ
  -> store 更新状态、进度、结果路径
  -> logs / download 接口展示或输出
```

### 上传生成 CBZ 主链路

```text
首页上传初始化
  -> /api/tasks/upload/init
  -> Task Core service 创建 upload task
  -> PUT 上传源文件
  -> go-api 落盘并标记 queued
  -> go-worker 领取 READY 任务
  -> worker 解包/重打包/写 ComicInfo.xml
  -> store 更新状态与结果
  -> logs 下载或 copy to Komga
```

### 日志页主链路

```text
/logs 页面
  -> 前端轮询 /api/logs
  -> internal/httpapi/legacy_adapter.go
  -> 调用 v2 task list
  -> 映射为 legacy log view payload
  -> 前端 table_render / task_actions 渲染
```

## 当前结构上的主要张力

### 1. legacy 与 v2 共存

当前项目已经以 v2 任务模型作为主数据源，但外部页面和大量用户可见行为仍经由 legacy-facing 层暴露。这带来两个后果：

- 规则容易在 adapter 层被重复定义；
- 任何任务相关改动都可能同时涉及 `httpv2`、`httpapi`、前端映射与 worker。

### 2. 任务规则还没有完全收口

项目的真正主轴是任务系统，但当前“任务状态、动作资格、进度语义、命名与 Komga 路径规则”仍散落在多个模块中，领域层未成为唯一规则中心。

### 3. 前端已具备中等复杂度

当前前端虽然未引入大型 SPA 框架，但已经承担较多状态和映射逻辑。页面脚本开始朝“页面模块系统”演进，但还未形成统一 view model 与页面边界约束。

## 当前系统优点

- 运行时主线已经统一到 Go。
- 任务系统已经天然成为业务中心。
- Compose、测试、CI、worker、artifact 流程都已存在，可作为重构支点。
- 真实问题（上传、Komga、重试、进度）已推动出一批可验证的领域概念。

## 当前系统风险

- adapter 层继续膨胀会放大认知负担。
- handler / worker / repo 之间职责再交叉，会让未来重构成本更高。
- 前端若继续散点增长，状态映射问题会重复出现。
- 运行方式若继续依赖“口口相传的隐性知识”，发布与接手成本会持续上升。
