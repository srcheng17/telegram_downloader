# 任务生命周期基线（Baseline）

## 目的

本文档冻结当前 URL 任务与上传任务的生命周期、状态语义和关键动作路径。它描述的是当前实现，不是重构后的目标模型。

## 当前任务类型

### URL 任务

来源：
- 首页填写 Telegraph URL 后通过 `/download` 提交。

核心特点：
- 有 URL / Canonical URL；
- worker 负责远程页面解析、图片抓取、CBZ 产物生成；
- 结果可浏览器下载，或按设置复制到 Komga。

### 上传任务

来源：
- 首页选择上传模式；
- 先调用 `POST /api/tasks/upload/init` 创建任务；
- 再调用 `PUT /api/tasks/{task_id}/upload-source` 上传源压缩包。

核心特点：
- 源文件保存在后端临时目录；
- worker 负责解包、筛选图片、重打包为 CBZ；
- 失败时可依赖保留源包进行重试。

## 当前状态集合

当前系统至少包含以下任务状态：

- `UPLOADING`
- `QUEUED`
- `RUNNING`
- `CANCEL_REQUESTED`
- `SUCCESS`
- `FAILED`
- `CANCELED`

legacy 日志层再映射为页面使用的中文/兼容状态值。

## URL 任务生命周期

```text
提交 URL
  -> QUEUED
  -> RUNNING
     -> 页面解析
     -> 图片总数发现
     -> 图片逐步下载（progress / total_images）
     -> 打包 CBZ
  -> SUCCESS | FAILED | CANCEL_REQUESTED -> CANCELED
```

### 当前 URL 进度语义

- 进入 `RUNNING` 但尚未拿到图片总数前：前端显示“准备中”。
- 一旦发现总图片数：开始按 `progress / total_images` 展示。
- 成功后可下载 / copy to Komga。

## 上传任务生命周期

```text
初始化任务
  -> UPLOADING
     -> 上传字节进度（loaded / total）
  -> QUEUED
  -> RUNNING
     -> 解包
     -> 图片整理
     -> ComicInfo.xml 写入
     -> 重新封装为 CBZ
  -> SUCCESS | FAILED | CANCEL_REQUESTED -> CANCELED
```

### 当前上传进度语义

- `UPLOADING`：显示字节进度 `loaded / total`。
- 已上传完成但 worker 处理中：显示“处理中”。
- 成功后显示完成动作。

## 取消路径基线

### URL / 上传任务取消

当前取消动作走 `/api/tasks/{task_id}/cancel` 一类接口（页面按钮触发），后端按当前状态推进：

- 活跃任务：`RUNNING/QUEUED/UPLOADING` 等进入 `CANCEL_REQUESTED`
- worker 或执行链路感知取消后落为 `CANCELED`

### 取消后的重试（当前已支持）

当前语义已统一为：

- `FAILED` 和 `CANCELED` 都可重试；
- URL / 上传任务都支持；
- 复用同一条任务记录；
- 前端按钮统一显示“重试”。

## 重试路径基线

### URL 任务

可重试条件（当前统一语义）：
- `status ∈ {FAILED, CANCELED}`
- URL 非空

### 上传任务

可重试条件（当前统一语义）：
- `status ∈ {FAILED, CANCELED}`
- `source_archive_path` 仍存在

说明：当前日志页已经不再盲信数据库中的 `retryable` 布尔字段，而是根据当前任务状态与必要资源动态判断可重试资格。

## 下载与 Komga copy 路径基线

### 浏览器下载

- 成功任务可通过 `HEAD /api/tasks/{id}/download` 预检；
- 预检可用后，再执行 `GET /api/tasks/{id}/download`。

### Komga copy

- 设置页可将 `download_action_mode` 切换到 `komga_copy`；
- 日志页点击下载动作时，实际触发 copy 接口；
- 目标目录根据系列名决定：
  - 有系列名：`<root>/<series>/file.cbz`
  - 无系列名：`<root>/tanbokon/file.cbz`

## 当前已知易变点

- `legacy adapter` 仍参与日志和部分动作语义映射；
- worker / repo / adapter 之间对状态与动作能力的语义尚未完全收口；
- 任务生命周期概念已经比较稳定，但代码落点还不是最终形态。
