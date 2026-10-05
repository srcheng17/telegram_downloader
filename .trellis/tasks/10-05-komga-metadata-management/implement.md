# 执行计划：Komga 存量 CBZ 元数据编辑

状态：in_progress。本清单作为实施与验收记录；已启动本子任务，隔离 Komga 1.28.1 与合成 CBZ 已验证写回/清空/恢复，未修改用户实际 Komga 漫画文件。原三批媒体工作区验收不自动证明此功能完成。

## 依赖与分工

1. 前置模块：单管理员/CSRF、可扩展 Document 与字段定义、`internal/comicinfo`、前端通用编辑器已可用；先核对这些子任务 review 中的实际接口和未完成外部验收，不以 task 状态代替代码证据。
2. 可并行的独占单元：Komga 官方 API 客户端/书库映射与凭据（新增 `internal/infra/komga`、独立测试）；存量 CBZ writer/备份校验（新增 `internal/archive/cbzedit`、合成 ZIP fixtures）；页面列表/编辑组件（新增 `frontend/src/komga`、相关组件测试）。每单元只写自有新文件，不动同一个共享入口。
3. 集成负责人串行接入 `cmd/server`、`internal/config`、共享 HTTP router/管理员中间件、PostgreSQL 迁移编号、设置连接入口、`frontend/src/app.js`/导航、Go 模板、Vite/dist、Compose/镜像及 E2E。各单元交付冻结 DTO/接口后再接线；不能撤销当前工作区的其他未提交改动。CLI 子任务只能在此处的真实 Komga API/写回契约稳定后添加对应命令。

## 实施顺序

1. 按 `trellis-before-dev` 读取相关后端、前端 spec 与模块边界；记录当前迁移最大编号、工作区未提交基线、Komga 目标实例版本/可用测试隔离库。冻结允许书库映射、预览/save/operation DTO、ComicInfo set/clear 白名单、Komga 规范化回读与错误语义。以[字段清空矩阵](research/komga-field-semantics.md)处理导入器忽略空值的行为，先写会失败的代表性归档/路径/冲突测试。
2. 实现 Komga 连接设置、管理员 API 客户端、书库/book 分页列表与详情、版本/导入能力检查及仅白名单字段的 PATCH-clear。配置 Komga 容器路径到本进程可写挂载根的显式映射，只有映射内 CBZ 可编辑；逐路径分量拒绝 symlink，测试 URL 编码、相近前缀、`..` 与无映射库。服务端 GET 的绝对路径、API key 均不进入浏览器 payload。
3. 实现独立 CBZ 写回器与私密备份：有界预检、唯一根 ComicInfo、存量专用 merge policy（PageCount/Pages/未改 Web/GTIN 不静默变化）、不可保真 XML/ZIP 结构拒写；先持久 `preparing` 与独立完整备份/哈希，再用隐藏 `.tmp` 和 ZIP raw Copy 保留非目标条目，重开逐条校验，最后同目录受根限制的原子替换/fsync。故障注入覆盖备份、临时写/close/fsync/回读、源版本变更和取消。不能改用新产物打包器或无检查的 rename。
4. 加入持久 edit operation/backup 清单及恢复判定，使用新的前向迁移编号，不改已应用迁移。编排 preview → 幂等 save → analyze → 白名单 clear 的必要 PATCH → 有界回读，分别记录 XML 写入、Komga 当前值和 analyze 证据限度；保存前阻止导入关闭、字段锁和其他不可验证条件。仅当 XML 与 Komga 投影均一致且无待处理 operation 才完全 no-op；XML 未变但需清空/同步时创建仅同步操作，不重复备份/写回。file committed 后 Komga 失败保留 `sync_pending`，按 operation ID 安全重试；目标 SHA/路径不匹配时拒绝重试或恢复。启动及同 key 请求/operation 查询/重试先有界 reconciliation，不能判定旧/新哈希时进入 `restore_needed`。测试备份成功未登记、rename 成功响应丢失、重复 save、进程在替换与状态更新间崩溃的恢复结果。
5. 接入受保护 HTTP 路由和「作品库」页面：书库筛选/搜索/分页、不可编辑原因、原文件/Komga 值区别、通用字段编辑与改动预览、保存状态、同步重试及显式恢复入口。旧页面和任务动作不改变；中文错误、键盘/移动端与异步草稿保护按前端 spec 检查。统一重建 `web/static/dist/*.bundle.js`。
6. 在独立 Komga 1.28.1 测试书库（含共享挂载）验证已有与首次新增 ComicInfo、字段锁/导入关闭、条码 ISBN 导入开启时拒绝 ISBN clear、analyze 202 后 READY 与字段读回、系列聚合限制、无映射/非 CBZ 只读；用代表性 CBZ 在 Komga/Kavita 核对实际阅读页顺序和元数据。不要在用户生产书库直接做破坏性试验。
7. 更新 README、连接/挂载/备份恢复文档、脱敏验收矩阵和 Trellis journal；让未参与实现的 reviewer 复核路径越界、ZIP 数据保真、非事务部分完成与真实外部证据。只在全部 PRD K1–K8 满足后申请完成，不以 unit/E2E 模拟成功冒充 Komga 实例验收。

## 验证计划

- focused Go：`go test ./internal/comicinfo ./internal/archive/... ./internal/app/komgaedit/... ./internal/infra/komga/... ./internal/httpapi/... -count=1`，实际包路径以落地代码为准；路径、ZIP 与 HTTP 错误测试使用隔离 fixture。
- 跨层：`go test ./... -count=1`、`go test -race ./... -count=1`、`go vet ./...`。新迁移在独立 PostgreSQL 执行/恢复验证，不指向现有生产库。
- 前端：相关 focused Node 测试后运行 `npm run test:frontend`、`npm run lint`、`npm run build`；确认源码与 `web/static/dist` 同步。浏览器 E2E 覆盖匿名阻断、列表分页、编辑/clear、草稿不被刷新覆盖、故障/部分完成/重试和移动端。
- 归档协议：固定 ComicInfo 2.1 XSD 用 `xmllint --nonet` 校验合成输出；逐条比对非目标条目的中央目录相对序号/名称、解压 SHA-256/CRC/大小、压缩方法、可观察 FileHeader/entry comment、全局 comment、页面与 Pages identity，验证独立完整备份与恢复；不要求物理字节偏移不变。损坏 ZIP/CRC、多份或嵌套 ComicInfo、未知 XML/压缩法、竞争格式、prefix/trailing/local-only extra、错误 PageCount、无效 Web/GTIN、Pages 非升序、ZIP bomb、磁盘不足和外部修改均有拒绝或明确额外差异测试。
- 真实依赖：隔离 Komga/Kavita 的文件写回、单书 analyze、规范化 GET 回读与 PATCH-clear；分别记录版本、导入选项、锁状态、耗时、文件/Komga 两阶段结果和因果验证限度。用户实际书库只有在实施阶段另获相应部署范围授权时才可触及。
- 最终运行 `git diff --check` 与项目适用的 release gate；若 release gate 已包含上述全套检查，不机械重复。所有结果只记录实际执行的命令、通过/失败与原因。

## 恢复与停止条件

改写前必须有可验证的私密完整原件。若写回后 Komga 异步处理失败，不自动回滚文件；保留 backup/operation，页面报告 file committed 与同步状态，并允许受版本保护的重试或显式恢复。空间不足、无映射、未知损失、锁定/导入关闭、ZIP/路径校验失败时停止在改写前。新功能可停止服务入口，但不得删除既有 CBZ、备份或已应用迁移；不自动提交、push、合并或生产部署。

## 2026-10-05 隔离验收补记

- 在隔离 Komga 1.28.1、独立 PostgreSQL 和合成 CBZ 上，服务端路径已验证标题写回、简介清空、Komga 规范化回读和原件恢复。旧 `PageCount=9`、实际 1 页时，未确认的预览拒绝；显式确认后可单独修正。生成的 ComicInfo 通过固定 XSD 的 `xmllint --nonet` 校验。
- 本轮又用实际 `mediactl` 经新编译的隔离 Go 服务完成 `books edit → metadata preview → update → edits status → restore`；保存状态为 `current_value_consistent`，恢复后原 CBZ SHA-256 与编辑前完全一致。CLI 只持有管理员会话，不直连 Komga 文件。本地 macOS release gate 在独立 PostgreSQL 数据库上通过完整 Go 与 race、vet、前端 214/214、构建及浏览器 E2E 30/30；见[集成验收矩阵](../10-04-media-workspace-integration/research/verification-2026-10-05.md)。
- 独立 reviewer 已修复 PostgreSQL advisory lock 错误路径、系列级字段误开放、页面失败保存后的锁死状态和前端状态文案。外部进程在最后一次源文件校验与原子替换之间仍有竞态，不能宣称跨进程强 CAS。Kavita 实例、所有 ZIP 边界及用户真实书库均未用于本轮现场验收；保持 `in_progress`，不在生产文件上补测。
- PR #4 首次 Ubuntu CI 暴露夹具环境差异：`t.TempDir()` 位于 `/tmp`（`01777`），被私密备份根的祖先权限检查正确拒绝。两个测试夹具改在解析后的用户主目录下创建并清理私有备份父目录；产品安全检查不变。远端复跑结论以 PR Checks 为准。
