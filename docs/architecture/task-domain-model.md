# 任务领域模型（Baseline to Target）

本文保留历史重构目标。当前 Task Core 状态、执行代际和输入资格以 [任务生命周期](task-lifecycle-baseline.md) 及 `.trellis/spec/backend/task-runtime-contract.md` 为准。

## 目的

本文档用于把当前项目中与“任务”有关的核心概念、状态、动作资格、进度语义和结果语义统一描述出来。它既总结当前实现，也为后续把规则收口到 `domain/app` 提供迁移目标。

## 为什么需要这份文档

当前任务相关规则并没有完全集中在一个包里，而是散布在：

- `internal/app/tasks/`
- `internal/httpapi/`
- `internal/httpv2/`
- `internal/worker/`
- `internal/store/postgres/`
- `internal/domain/v2/`

这种分布已经能支撑功能，但会带来三个问题：

1. 同一概念需要在多处维护；
2. 某些规则会悄悄停留在 adapter / worker 中；
3. 前端和接口层容易读取“底层字段”，而不是消费统一语义。

因此，后续重构的目标不是发明新概念，而是把已存在的概念收口。

## 当前任务核心概念

### Task identity

每个任务至少具备：

- `id`
- `task_type`
- `status`
- `created_at`
- `updated_at`

### Task source

任务来源当前有两类：

#### URL task

关键字段：
- `url`
- `canonical_url`

特点：
- 来源是用户输入的 Telegraph URL；
- 结果依赖远程页面解析与图片下载；
- 进度在“发现总图数后”以图片数推进。

#### Upload task

关键字段：
- `source_archive_name`
- `source_archive_path`
- `upload_loaded_bytes`
- `upload_total_bytes`

特点：
- 来源是用户上传的 ZIP / RAR / 7Z；
- 结果依赖源包解析和重新封装；
- 失败后的重试能力依赖 `source_archive_path` 是否仍存在。

## 当前状态模型

### 状态集合

当前主状态至少包括：

- `UPLOADING`
- `QUEUED`
- `RUNNING`
- `CANCEL_REQUESTED`
- `SUCCESS`
- `FAILED`
- `CANCELED`

此外还存在 legacy 层的状态映射，用于页面与历史接口兼容。

### 当前合法流转（概念层）

#### URL task

```text
QUEUED
  -> RUNNING
  -> SUCCESS
  -> FAILED
  -> CANCEL_REQUESTED -> CANCELED
```

#### Upload task

```text
UPLOADING
  -> QUEUED
  -> RUNNING
  -> SUCCESS
  -> FAILED
  -> CANCEL_REQUESTED -> CANCELED
```

### 当前状态规则落点观察

- `internal/domain/v2/state_machine.go`
  - 已有部分 v2 状态机规则
- `internal/app/tasks/run_task.go`
  - 终态 patch、取消后可重试等执行语义
- `internal/httpapi/task_actions.go`
  - retry 资格判断
- `internal/httpapi/legacy_adapter.go`
  - legacy 状态映射、日志动作映射
- `internal/store/postgres/v2_repo.go` / `upload_task_store.go`
  - 持久化层状态推进与条件更新

结论：状态机存在，但规则尚未单一中心化。

## 动作能力语义（目标需要统一）

后续任务模型应统一表达四类动作资格：

- `can_retry`
- `can_cancel`
- `can_download`
- `can_copy_to_komga`

### 当前 can_retry 语义

目前系统实际已经采用的统一语义是：

#### URL task

可重试条件：
- `status ∈ {FAILED, CANCELED}`
- `url` 非空

#### Upload task

可重试条件：
- `status ∈ {FAILED, CANCELED}`
- `source_archive_path` 非空且源包仍应被保留用于重试

当前这一规则主要落在：
- `internal/httpapi/task_actions.go` (`canRetryTask`)
- `internal/httpapi/legacy_adapter.go`（日志中使用同一判断）
- `internal/httpv2/upload_task_store.go`（真正执行 retry 的 SQL 条件）

### 当前 can_cancel 语义

目前取消能力分散在：
- `internal/app/tasks/service.go`
- `internal/httpapi/legacy_adapter.go`
- `internal/store/postgres/*`
- `internal/worker/*`

概念上，可取消对象应是“尚未终态的任务”，但实现上仍带有 legacy/v2 过渡痕迹。

### 当前 can_download / can_copy_to_komga 语义

概念上都应要求：
- `status == SUCCESS`
- `result_zip_path` 存在且可访问

区别：
- 浏览器下载面向 artifact 输出；
- Komga copy 还依赖目标根目录和系列名路径规则。

## 当前进度语义

### URL task progress

关键字段：
- `progress`
- `total_images`

当前用户语义：
- `RUNNING` 且 `total_images == 0`：显示“准备中”
- `total_images > 0`：显示 `progress / total_images`

落点观察：
- 进度由 downloader / worker 回写数据库；
- legacy 日志层透传 progress 字段；
- 前端根据状态与数字组合决定显示文案。

### Upload task progress

关键字段：
- `upload_loaded_bytes`
- `upload_total_bytes`

当前用户语义：
- `UPLOADING`：显示字节进度
- 上传完成、worker 处理中：显示“处理中”

落点观察：
- upload handler 负责上传字节回写；
- worker 负责后续状态推进；
- 前端日志页根据 task type + status + byte fields 组合渲染。

## 当前结果与资源语义

### Result artifact

关键字段：
- `result_zip_path`

虽然字段名保留 `zip` 历史痕迹，但当前新主线结果通常为 CBZ。后续更适合把它视为“任务结果产物路径”，而不是把字段名当成产品语义。

### Upload source artifact

关键字段：
- `source_archive_path`
- `source_archive_name`

语义：
- 上传任务的输入源；
- 失败 / 取消后若保留，可支持同一任务一键重试；
- 成功后原则上应清理，避免长期占空间。

## Metadata and output semantics

虽然元数据、文件名、Komga 路径目前不全在同一包中，但它们都是任务领域的一部分：

- 作者 / 标签 / 类型规范化
- `作者_[系列名]_漫画名_时间戳.cbz` 文件名规则
- 无系列名时 Komga 放入 `tankobon`

这些规则目前主要散落在：
- `internal/app/tasks/metadata.go`
- `internal/app/tasks/komga_copy.go`
- `internal/worker/output_filename.go`
- `internal/httpapi/download_request.go` 相关解析/清洗逻辑

后续应向专门的 domain 子包收敛。

## 当前任务领域的主要问题

### 1. 规则概念已经稳定，但落点不统一

例如“可重试”已经有统一产品语义，但仍同时涉及：
- HTTP action handler
- legacy logs mapping
- SQL retry 条件

### 2. worker 仍承载部分领域语义

例如输出文件名与执行过程中的状态解释，说明 worker 还没有完全退回到“执行器”角色。

### 3. legacy adapter 仍承担部分事实语义

例如日志中的动作显隐、状态映射与字段兼容，导致 adapter 不是纯粹协议层。

## 后续重构目标

后续 Phase 1 / 2 应把任务领域收敛为：

### Domain 层负责

- 状态集合与合法流转
- 动作资格判断
- 进度阶段语义
- 结果/源文件语义
- 命名与元数据规则

### Application 层负责

- CreateURLTask
- InitUploadTask
- AttachUploadSource
- RetryTask
- CancelTask
- CopyResultToKomga
- ListTaskLogs

### Interface 层负责

- 只做 HTTP / legacy / v2 协议适配
- 不新增长期任务规则

### Worker 层负责

- 执行、上报、协调
- 不复制任务规则

## 推荐的下一步实现切口

为了降低风险，建议先从以下 3 组规则开始抽：

1. **状态与动作资格**
   - retry / cancel / success-action eligibility
2. **进度阶段语义**
   - URL preparing vs x/total
   - upload byte progress vs processing
3. **元数据 / 文件名 / Komga 路径规则**
   - 作为第二组领域包继续抽离

这也是接下来 `internal/domain/task/` 与相关 app 用例收敛的依据。
