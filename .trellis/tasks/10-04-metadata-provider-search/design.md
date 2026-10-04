> 执行更新（2026-10-05）：第二批已实现并完成本地集成检查，状态 review；实际外部验收仍未全部完成。结果以[第二批验收记录](../10-04-clipboard-ocr-tdl-ui/batch-2-verification.md)为准。下文的 planning/未执行描述保留为原计划背景。

# 书目检索技术设计

## 边界与文件所有权

本子任务实现 `internal/app/metadatasearch/` 的编排、`internal/infra/metadataproviders/` 的三个 adapter、独立 `internal/httpapi/metadata_search*.go`、`frontend/src/metadata-search/` 及专属测试。包名以实施前目录复查为准；不为此搬移现有模块。

metadata-contracts 唯一定义 metadata_document、typed registry、custom.user.* 扩展、provenance 和 Legacy7 projection；source-settings-auth 唯一定义配置与凭据持久化、admin/CSRF；modern-ui-shell 提供组件 mount/unmount 和草稿读写入口。路由注册、cmd 装配、共享运行配置、父页面插槽和 dist 由父集成者串行接线，避免并行编辑共享入口。

## 配置与 adapter 契约

`ProviderDescriptor` 暴露 id、label、capabilities、auth_modes、attribution/terms 链接；`PublicSourceConfig` 来自共享服务（enabled、priority、auth_mode、credential_configured、config_version、filters、field_preferences），adapter 通过 server-only credential resolver 取凭据。公共描述不包含 secret、任意请求头、命令、用户指定可执行文件或 session。

| provider | 首版能力 | 认证 | 映射注意 |
| --- | --- | --- | --- |
| mangabaka | 标题关键词搜索、详情/多语种标题、创作者、分类、源标识 | none；不展示无意义密钥输入 | 自有字段 CC BY-NC-SA 4.0，嵌套 source 保留原来源条款；作者定向搜索不冒充已验证 |
| mangaupdates | 标题/别名搜索、详情、Author/Artist、分类及 Doujinshi 类型 | none；首版无账号写操作 | hit_title 不是标准标题；请求 perpage 未必生效，客户端自行截限 |
| bangumi | 书籍关键词、中文/原名、人物关联、系列/单卷、标签/NSFW | none 或 bearer token | 匿名可见性有限；nsfw schema 的版本差异用固定 fixture 和授权 smoke 验证 |

应用依赖最小 `Search(ctx, query, limits)` / `Resolve(ctx, recordID)` 接口；capabilities 只列实际实现能力。固定官方 origin，禁止重定向时转发凭据到另一主机；不把 provider 返回 URL 当作任意抓取指令。服务端校验 recordID 格式，详情仍由 adapter 构建固定 URL。

## HTTP 与数据流（由父任务统一冻结路由名）

- `GET /api/metadata/providers`：能力与脱敏配置，不进行检索。
- `POST /api/metadata/search`：`{keyword, provider_ids, query_revision}`。响应含相同 query_revision、逐源 config_versions（对应 PublicSourceConfig.config_version）、每源 candidates/error/visibility、attribution；整体请求有效而部分失败用逐源结果表达。
- `POST /api/metadata/candidates/resolve`：`{provider_id, record_id, query_revision, base_document_revision, field_revisions}`；返回独立 `MetadataCandidate`（candidate_id、origin、base_document_revision、相关 field revisions、input/config revision、typed 候选字段）及 attribution，不写任务/历史，不返回已确认字段状态或伪造 manual_locked。
- 请求中的基准 revision 只用于建议与当前草稿关联，不证明客户端事实正确；无需上传整份草稿或 OCR 原文。采用动作调用共享 ApplyCandidate/reducer：文档 schema_version=1，fields[key] 的 state=value|cleared、revision、manual_locked、provenance 由 metadata-contracts 唯一管理，缺 key 表示未指定。校验文档/字段/input/config revision，显示 diff，由用户明确采用选定字段；任何所选字段冲突整次原子失败，不能部分覆盖或后台解锁；确认提交时沿 metadata-contracts 路径持久化。搜索模块不复制元数据清洗、任务创建或 ComicInfo 映射。
- `/api/*` 错误仍用 `{error, code, message?, details?}`。provider error code 固定枚举：no_results、auth_required、auth_invalid、permission_denied、rate_limited、timeout、unavailable、invalid_response、cancelled；visibility 为单独能力说明，不凭零结果推断过滤。

## 有界外部访问

首版最多三源并行，每源一个在途请求，应用设有限 deadline、响应字节、字段长度和候选数（建议 20 条/源、2 MiB 响应；最终常量跟共享 contracts 对齐）。关键词限长，不静默截断。无自动多关键词排列组合或自动扩大来源。

MangaBaka 搜索未缓存 30/min、一般读取 180/min；MangaUpdates 使用缓存和保守请求间隔而不声称有官方数值限额；Bangumi 遵循带开发者/应用标识的 UA 与 429/Retry-After。只缓存必要书目结果，有限 TTL/容量；key 包含 provider、标准化 query、筛选、配置/权限 revision。启停或 token 更换使旧缓存失效，日志不打印 key 中的原文。限流不自动切换其他未确认来源。

## 元数据与许可

共同字段映射使用 registry；标准无法表达但有价值的字段仅在用户已有显式映射时进入已注册 `custom.user.<name>`，类型限定 string/string[]/integer/boolean。不自动创建 provider namespace 或任意 object。未映射源字段仅作有界候选详情，不混入确认文档。只保留白名单中性字段，不保存整个上游响应/图片/章节。标题相似不是跨源合并依据；跨源 ID 或用户确认才能关联。source record ID、源 URL、retrieved_at、原字段路径、采用方式和许可/署名作为 provenance 保存；聚合来源逐字段保留实际来源归属。条款不允许持久化/导出的字段只作候选展示或明确不提供，不能静默移除证据后导出。

## 降级与回滚

关闭各 provider 即停止后续外部请求，保留已确认的用户元数据和 provenance。网络/认证故障不影响 OCR 和下载。无需新 migration：复用 metadata-contracts 与 source-settings-auth 存储；若实现发现额外 schema 需求，先回父设计分配，禁止抢用其他子任务 migration 编号。

配置模块通过注入能力目录及只读 `Test` 端口调用本适配器；批次1使用冻结 descriptor/fake，批次2安装真实 adapter 后才可报告来源连接已验证。`config_revision` 仅映射 config_version，不维护另一个版本。
