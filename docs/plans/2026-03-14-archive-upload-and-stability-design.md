# 压缩包异步上传、元数据回溯与稳定性优化设计（方案三）

## 1. 背景、问题与目标

### 1.1 当前问题
- 首页当前只提供 `Telegraph URL` 输入，上传压缩包并不是一条真正接通的主线能力。
- 前端首页提交逻辑仅会向 `/download` 提交 `url + metadata`，不会发起真实的文件上传。
- Go 主线 `/download` 在 `url` 为空时会直接返回 `Please provide a Telegraph URL.`，导致压缩包场景被错误拦截。
- 仓库中存在 `archive_upload` 相关残留前端辅助代码，但没有完整接入 UI、API、worker 与日志链路，属于半成品能力。
- legacy 日志适配层当前会将 v2 任务的进度与元数据大量清空映射，无法承载上传进度、元数据回溯与失败重试等新需求。

### 1.2 本轮目标
- 首页同时支持两种入口：`Telegraph URL 下载` 与 `ZIP/CBZ 异步上传导入`，用户任选其一即可发起任务。
- 上传任务必须走真正的异步链路：`browser -> go-api -> 持久化任务 -> 上传文件 -> redis streams -> go-worker`。
- 上传进度、处理阶段、失败状态、重试按钮、成功下载按钮都要在现有日志页中体现，并与 URL 任务保持一致体验。
- 上传 `zip/cbz` 后，系统要读取压缩包已有元数据（如 `ComicInfo.xml`），与首页填写元数据合并，并将最终结果写入输出 `cbz`。
- 保存“首页请求元数据 / 压缩包原始元数据 / 最终生效元数据”三层信息，支持排障、重试与历史回溯。
- 在实现上传能力的同时，清理无用/残留代码，补齐测试，并增强运行稳定性。

### 1.3 非目标
- 本轮不引入新的独立上传页面，入口保留在首页。
- 本轮不改变最终产物类型，成功任务统一输出标准 `cbz`。
- 本轮不引入第二套任务/日志 UI，继续复用当前日志页与现有任务概览。

---

## 2. 方案选择

### 2.1 备选方案
1. **单接口混合模式**：继续复用 `/download`，同时支持 URL 与 multipart 上传。
2. **首页统一、后端双链路模式**：URL 与上传在首页共存，但后端分别走独立链路。
3. **上传全异步任务模式（选定）**：上传也像 URL 下载一样具备完整任务生命周期，上传文件、解压、校验、重打包全部进入异步任务体系。

### 2.2 选型结论
采用**方案三：上传全异步任务模式**。

原因：
- 可以让上传任务与 URL 任务共享统一的日志、取消、恢复、失败重试、成功下载能力。
- 上传进度可以从“文件传输阶段”开始进入日志，而不是只在文件传完后才出现。
- 便于隔离 API 接收与耗时处理，降低首页卡死、请求超时、假启动失败等问题。
- 为后续大文件、断点续传、对象存储等扩展保留演进空间。

---

## 3. 目标架构

### 3.1 总体架构

```text
Home UI
  |- URL download submit -------------------------------> go-api -> postgres -> redis -> go-worker
  |- archive task create -> upload content -> progress -^

logs UI <--------------------------------------------- go-api / legacy-facing adapter
```

### 3.2 入口设计
- 首页保留单个元数据表单（作者、系列名、漫画名、简介、标签、类型）。
- 在此基础上提供两个任务入口：
  - `通过 Telegraph URL 下载`
  - `上传 ZIP/CBZ 导入`
- 两种入口共用元数据输入，但提交方式不同：
  - URL：继续走 URL 创建任务链路。
  - 上传：先创建上传任务，再上传文件内容，最后由 worker 异步处理。

### 3.3 责任边界
- **前端首页**：负责采集入口类型、元数据、上传文件、展示提交反馈。
- **go-api**：负责任务创建、接收文件流、更新上传进度、入队、提供日志/详情/重试/下载接口。
- **go-worker**：负责压缩包校验、读取元数据、解压、合并元数据、重打包为最终 CBZ、写入最终状态。
- **postgres**：持久化任务、进度、阶段、三层元数据、源文件信息、产物路径、重试链路。
- **redis streams**：驱动异步执行与恢复。

---

## 4. 任务模型与状态机

### 4.1 顶层状态（保持统一）
对外继续沿用现有 legacy-facing 状态：
- `PENDING`
- `IN_PROGRESS`
- `CANCEL_REQUESTED`
- `SUCCESS`
- `FAILED`
- `CANCELED`

这样可以保持首页概览、日志筛选、下载按钮、取消按钮的兼容性。

### 4.2 细粒度 phase
新增 `phase` 字段用于表达当前阶段：

#### URL 任务 phase
- `QUEUED`
- `DOWNLOADING`
- `PACKAGING`

#### 上传任务 phase
- `UPLOADING`
- `VALIDATING_ARCHIVE`
- `READING_ARCHIVE_METADATA`
- `EXTRACTING`
- `MERGING_METADATA`
- `REPACKAGING_CBZ`
- `FINALIZING`

### 4.3 进度字段
新增统一进度字段：
- `progress_mode`：`images` / `bytes` / `items` / `phase_only`
- `progress_current`
- `progress_total`
- `progress_label`

展示规则：
- URL 下载：继续可显示 `12 / 35`
- 上传阶段：显示 `84 MB / 200 MB` 或 `42%`
- 解压/重打包：显示 `23 / 120`
- 纯阶段任务：显示 `正在读取压缩包元数据`

### 4.4 操作能力字段
日志页的动作不再只靠顶层状态硬编码判断，而是支持统一能力判断：
- `can_cancel`
- `can_download`
- `can_retry`

效果：
- 进行中任务显示“取消”
- 失败任务显示“重试”
- 成功任务显示“下载”
- URL 与上传任务行为一致

---

## 5. 数据模型设计

### 5.1 任务来源与源文件信息
新增字段：
- `source_type`：`url` / `archive_upload`
- `source_file_name`
- `source_content_type`
- `source_file_size`
- `source_sha256`
- `source_archive_path`
- `retry_parent_task_id`

### 5.2 三层元数据
为满足“首页元数据回溯也需要保存压缩文件元数据”的要求，保存三层信息：

1. `request_metadata_json`
   - 首页用户填写的原始元数据
2. `archive_metadata_json`
   - 从上传压缩包中解析得到的元数据
3. `effective_metadata_json`
   - 最终写入输出 CBZ 的元数据

同时继续保留现有扁平字段：
- `author`
- `series_name`
- `comic_name`
- `summary`
- `tags_raw`
- `tags_normalized`
- `genres_raw`
- `genres_normalized`

这些扁平字段代表 `effective_metadata` 的展开结果，用于兼容现有日志/API。

### 5.3 元数据优先级
最终 CBZ 元数据合并规则：
1. 首页填写元数据优先级最高
2. 压缩包内元数据次之（优先读取 `ComicInfo.xml`）
3. 文件名推断/默认占位兜底

因此既能保留导入文件已有元数据，又能允许首页覆盖关键字段。

---

## 6. API 设计

### 6.1 URL 任务
- 保留现有 `/download` URL 链路，但前端校验改为“URL 模式必填 URL”，而不是页面全局强制必填 URL。
- 保留重复任务复用、强制重抓、成功下载、日志跳转等现有能力。

### 6.2 上传任务创建
新增：`POST /api/archive-tasks`

请求体包含：
- 元数据字段（author / series_name / comic_name / summary / tags / genres）
- 文件名、文件大小、内容类型等前置信息

返回：
- `task_id`
- `status`
- `logs_url`
- `upload_url`

创建后即写入任务记录：
- 顶层状态 `PENDING`
- `phase=UPLOADING`
- 保存 `request_metadata`

### 6.3 上传文件内容
新增：`PUT /api/archive-tasks/{task_id}/content`

行为：
- 接收浏览器文件流
- 边上传边更新 `progress_mode=bytes`
- 计算 `source_sha256`
- 原子落盘到上传暂存目录
- 上传完成后进入异步处理队列

### 6.4 统一日志与下载接口
- 继续使用现有日志页与日志接口，但返回体扩展为能表达 `source_type`、`phase`、`progress_*`、三层元数据摘要、动作能力。
- 成功下载继续复用：`GET /api/tasks/{task_id}/download`

### 6.5 统一重试接口
新增：`POST /api/tasks/{task_id}/retry`

行为：
- URL 任务：复用原 URL + 已保存元数据创建新任务
- 上传任务：复用已保存源压缩包 + 已保存 request_metadata 创建新任务
- 返回新任务 ID 与日志跳转地址

---

## 7. Worker 处理链路

### 7.1 上传任务处理流程
上传任务入队后，worker 执行：
1. 校验源文件存在、扩展名与 MIME
2. 校验 ZIP/CBZ 是否可读
3. 读取压缩包内已有元数据（优先 `ComicInfo.xml`）
4. 解压图片资源到隔离目录
5. 合并 request/archive/effective 元数据
6. 生成新的 `ComicInfo.xml`
7. 重打包为最终 `cbz`
8. 写入产物路径与最终状态

### 7.2 失败与恢复
- 任一步失败都要记录具体错误并进入 `FAILED`
- 中断/崩溃恢复时要能识别卡在 `UPLOADING` / `VALIDATING_ARCHIVE` / `EXTRACTING` / `REPACKAGING_CBZ` 的任务
- worker 重启后应基于状态与文件存在性决定恢复、失败或清理

### 7.3 取消行为
- 上传尚未完成时取消：终止上传接收、清理临时文件、任务进入 `CANCELED`
- worker 执行中取消：在阶段边界与长流程中检查取消标记，尽快终止并清理中间目录

---

## 8. 日志与前端交互

### 8.1 首页
首页变更为“同一个元数据表单 + 两种提交模式”：
- URL 模式：展示 URL 输入与 URL 提交按钮
- 上传模式：展示文件选择、上传按钮、上传进度反馈

校验规则：
- URL 模式必须填写合法 Telegraph URL
- 上传模式必须选择合法 ZIP/CBZ 文件
- 两种模式都允许填写元数据

### 8.2 日志页
日志列表保留现有主结构，但增强显示内容：
- 展示 `source_type`
- 展示 `phase` 与更真实的进度文本
- 失败任务显示“重试”按钮
- 成功任务显示“下载”按钮
- 可选展示元数据摘要与来源说明

### 8.3 元数据回溯
日志详情/后续接口中允许查看：
- 首页填写元数据
- 压缩包解析出的元数据
- 最终输出使用的元数据

这三层信息用于排障与重试复现。

---

## 9. 稳定性与安全性增强

### 9.1 上传/解压护栏
- 文件大小上限
- 解压后总大小上限
- 解压文件数量上限
- 允许的图片格式白名单
- ZIP Slip 路径逃逸防护
- ZIP Bomb/超高压缩比防护

### 9.2 文件系统隔离
分离目录：
- 上传暂存目录
- 解压工作目录
- 最终产物目录
- 失败清理/恢复目录

避免混用 `downloaded_images` 与上传临时文件，降低误删风险。

### 9.3 崩溃恢复与清理
- API/worker 启动时恢复未完成任务
- 定时清理孤儿上传文件、失败解压目录、过期临时文件
- 对“上传已完成但未入队”“入队后 worker 异常退出”“最终文件丢失”等情况补偿处理

### 9.4 兼容适配层收敛
- 修复 legacy adapter 到日志页的数据映射，不能再将进度与元数据无条件清空
- 逐步减少残留前端上传半成品与重复状态管理逻辑

---

## 10. 测试与验收

### 10.1 单元测试
- 上传任务创建/上传校验
- ZIP/CBZ 元数据读取
- 元数据优先级合并
- `ComicInfo.xml` 生成
- 上传/解压/重打包阶段进度更新
- retry 行为（URL 与上传）

### 10.2 集成测试
- `POST /api/archive-tasks`
- `PUT /api/archive-tasks/{id}/content`
- `POST /api/tasks/{id}/retry`
- worker 处理上传任务成功/失败/取消/恢复
- legacy-facing 日志与下载契约不回归

### 10.3 E2E
- 上传 zip -> 日志看到上传进度 -> worker 处理 -> 成功下载
- 上传 cbz -> 读取原有 `ComicInfo.xml` -> 首页元数据覆盖 -> 最终下载校验
- 失败任务显示“重试”并成功重跑
- URL 下载链路保持可用

### 10.4 验收标准
- 上传场景不再要求填写 Telegraph URL
- 上传任务从“创建”到“成功下载/失败重试”全链路可见
- 成功任务统一输出标准 CBZ，元数据符合优先级规则
- 日志页对 URL/上传两类任务保持统一操作体验
- 新增稳定性护栏后，无明显临时文件泄漏、路径逃逸、超限解压风险

---

## 11. 无用代码与项目优化范围

本轮优化同时覆盖：
- 清理/重构未接通主链路的上传残留代码
- 收敛首页提交状态管理，避免 URL/上传双套混乱逻辑
- 修复 legacy adapter 映射层对 v2 任务信息的丢失
- 为新老链路补齐测试与文档，保证“功能修复 + 可维护性提升”同步完成

---

## 12. 设计结论

采用**方案三：压缩包上传全异步任务模式**。

实施结果应达到：
- 首页统一入口但支持 URL 与上传两类任务
- 上传任务具备与 URL 任务一致的日志、下载、取消、失败重试体验
- 三层元数据可追踪、可回放、可用于最终 CBZ 重打包
- Go 主线日志与稳定性能力同步增强，避免继续积累残留/半成品逻辑
