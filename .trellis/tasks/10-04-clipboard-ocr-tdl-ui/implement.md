> 2026-10-05 计划增补：第 9、10 个[Komga 存量元数据管理](../10-05-komga-metadata-management/prd.md)与[CLI/AI Skill](../10-05-cli-ai-skill/prd.md)子任务已获实施确认并处于 `in_progress`；隔离验证及剩余外部验收见[集成记录](../10-04-media-workspace-integration/research/verification-2026-10-05.md)。原三批实施/验收状态不自动覆盖它们。前端以 [UI v3 设计](../10-04-modern-ui-shell/redesign-v3.md) 为准。

> 2026-10-05 范围调整：用户移除新建任务的 Telegram 消息下载来源，保留 tdl 登录并迁入「设置 → 连接」；旧 `/telegram` 跳至 `/settings#connections`。后端接口、历史任务及账号数据保留，不再要求首页 Telegram 来源入口。

> 执行更新（2026-10-05）：第一、二批代码已实现，子任务均在 review；第二批已通过分阶段本地检查，真实外部验收与第三批尚未完成。见[第一批记录](batch-1-verification.md)、[第二批记录](batch-2-verification.md)。下文 planning 描述为原计划背景。

# 分批并行执行计划

状态：planning。本文保留原 8 个子任务的三批执行顺序；新增两个扩展子任务独立规划，不表示已实现或测试通过。当前父任务保留总需求与集成责任。

## 1. 批次与依赖

| 批次 | 子任务 | 准入 | 独立交付 |
| --- | --- | --- | --- |
| 1 | [metadata-contracts](../10-04-metadata-contracts/prd.md) | 共享文档/字段语义冻结 | 可扩展注册表、文档/候选/legacy 契约及持久化模块 |
| 1 | [source-settings-auth](../10-04-source-settings-auth/prd.md) | 单管理员选择已确认 | 会话与来源/AI配置、独立凭据、模型发现/测试 |
| 1 | [modern-ui-shell](../10-04-modern-ui-shell/prd.md) | 共享 draft/API 契约冻结 | 中文页面框架、通用元数据编辑、登录/设置挂载点 |
| 2 | [multi-image-ocr-ai](../10-04-multi-image-ocr-ai/prd.md) | 前三项完成并接线 | 多图本地 OCR、规则、动态 schema AI 候选 |
| 2 | [metadata-provider-search](../10-04-metadata-provider-search/prd.md) | 前三项完成并接线 | 三个来源、授权查询、差异预览与采用 |
| 2 | [tdl-account-download](../10-04-tdl-account-download/prd.md) | 前三项完成并接线 | QR/2FA/helper交接、账号单写、Task Core 附件下载 |
| 2 | [comicinfo-roundtrip](../10-04-comicinfo-roundtrip/prd.md) | metadata-contracts 完成；接线就绪 | 原元数据保留、扩展字段导出、schema/图片完整性 |
| 3 | [media-workspace-integration](../10-04-media-workspace-integration/prd.md) | 第二批全部交付且无共享契约漂移 | 全链路、实际外部依赖/阅读器验收、迁移恢复与文档 |
| 扩展，进行中 | [komga-metadata-management](../10-05-komga-metadata-management/prd.md) | 元数据模型、ComicInfo 与管理员登录已可复用；受控共享挂载、Komga API 与安全写回已在隔离环境接通 | 全库列表中选择存量 CBZ，写回 ComicInfo，单书分析并验证 Komga 读回；外部兼容继续独立验收 |
| 扩展，进行中 | [cli-ai-skill](../10-05-cli-ai-skill/prd.md) | 复用受保护应用 API 与 Komga 写回；本地 OCR/规则已接通 | CLI 覆盖网页业务功能并交付项目 Skill；真实剪贴板、tdl 与全新会话发现继续独立验收 |

“批次 2”是依赖允许后的有界队列，并非 4 个 worker 同时启动：最多 3 个实现单元 + 1 名集成/评审负责人。默认先 OCR、书目源、tdl，任一释放槽位即补位打包；若打包更早可独立开工，允许调整队列以减少空等，但不能跳过依赖。

集成任务虽在最终批完成，其 root 负责人从第一批开始工作：每个模块交付后及时接共同路由/存储/页面，不能把所有接线留到最后。模块单测完成与可用集成完成分别记录。

## 2. 共享文件所有权

| 所有者 | 独占范围 |
| --- | --- |
| metadata-contracts | 新 domain metadata document/registry/merge/legacy 模块、app/metadata、store/postgres/metadatadoc 及测试 |
| source-settings-auth | app/adminauth、app/sourcesettings、对应独立仓储、credentials/modelapi、独立 HTTP handlers、settings/integrations 与 admin_session |
| modern-ui-shell | web/templates 结构、web/static/style.css、共享 UI/draft/editor、初始导航和挂载契约；不写来源/tdl 请求业务 |
| multi-image-ocr-ai | frontend/src/ocr、规则设置服务/仓储、专属 HTTP/app 提取模块、OCR 资源清单及专属测试 |
| metadata-provider-search | app/metadatasearch、infra/metadataproviders、metadata_search handlers、frontend/src/metadata-search 及测试 |
| tdl-account-download | app/telegram、infra/telegram、telegram handlers、frontend/src/telegram、tools/tdl-auth-helper 及测试 |
| comicinfo-roundtrip | internal/comicinfo、archive/metadata_bundle、downloader/metadata_pack 模块及 fixtures |
| root 集成 | cmd/server/worker、共享 router/config、旧 Task Core/HTTP/history/worker 签名接线、迁移落号、app/page_modules/api transport、依赖锁/Vite/dist、集成测试/部署文档 |

第一批 shell 可以修改初始模板，但业务 worker 只提交独立模块/partial 与明确挂载需求，root 在 shell 完成后串行绑定。任何跨界编辑先发送变更清单协调，不抢写。既有 runtime-review-fixes 未提交变更始终保留。

迁移预留：015 metadata definitions/submitted/effective document/history、016 admin/source settings/extraction rules、017 history task identity、018 Telegram account coordination、019 Telegram task input、020–022 retention/packaging reports。各模块提供契约/SQL方案，root 落地前检查最新编号并顺序集成。不得多个 worker 并发修改 migration runner 或共享 settings DTO。

## 3. 启动与检查点

### 计划审阅后，第一批

- [ ] 冻结父 design 与三个基础子设计，确认 task manifests 引用真实路径；仅启动当前交付子任务，不把父任务当实现目标。
- [ ] 记录工作区已有变更基线，保留独立运行修复；为工作单元安排隔离工作树或严格独占文件，遵守 [模块边界](../../../docs/development/module-boundaries.md)。
- [ ] 三模块各自 meaningful focused tests 后由独立 reviewer 检查；root 接入 schema、document 存储、auth、source settings 与 UI。
- [ ] 验证旧七字段请求/历史兼容、新字段/自定义字段可保存、管理员登录/退出和健康探针、基本上传/旧下载页面无回退。
- [ ] 模块失败只回退该模块的未交付改动，不撤销他人工作。公共接口变化更新所有调用方计划后才继续。

### 第二批

- [ ] 按最多 3 个 worker 队列调度；候选一律采用 MetadataCandidate 及公共 draft 合并逻辑。
- [ ] OCR 先验证同源资源、选定语言和真实图片，再接 AI；扩展字段重新测，不能复用七字段成绩宣称通过。
- [ ] 来源先验证公开接口，再验证用户已配置授权；无权限只能报告该覆盖未验，不能伪称成人同人已通过。
- [ ] tdl 首先完成 helper 编译和官方 CLI 真实会话交接、跨进程锁、硬输出额度的能力验证，再接 UI 和任务创建。未证明硬上限时不开放下载功能。
- [ ] 打包使用合成包验证原元数据/未知字段副本和 schema，随后与实际 worker 接线。
- [ ] 每个交付点 root 做共享变更并重建 dist；记录实际 focused 验证与遗留项。

### 第三批

- [ ] 在独立 TEST_DATABASE_URL 和隔离 Compose 栈验证 migrations/任务快照/重试取消/鉴权保护；禁止指向生产库/生产下载目录。
- [ ] 浏览器执行：登录 → 来源配置 → 搜索或多图 OCR → 人工采用/扩展字段 → 下载或上传 → CBZ → Komga；模拟失败测试与真实外部链路证据分开。
- [ ] 用固定版本的隔离 Komga/Kavita 读取代表性 XML，记录字段保留/大小写/分级/序号的差异。
- [ ] 校验生产镜像内 OCR 静态资源和 bundle，而非只用 Vite 开发服务器。
- [ ] 写 verification、升级/初始化/密钥备份和回滚文档；所有未验证外部条件显式列出，不以计划中的命令视为已跑。

## 4. 计划检查命令

每个 worker 的 focused 路径以其实现时真实文件为准，新增包未存在前不运行伪测试。前端源码变更需：

```sh
npm run test:frontend
npm run lint
npm run build
```

后端交付需 `go test ./... -count=1`；仓储、worker/并发协作另需 `go test -race ./... -count=1`；整体合入工作分支后的最终验证包括：

```sh
go vet ./...
npm run e2e:test
bash scripts/verify_release_gates.sh
git diff --check
```

`verify_release_gates.sh` 已含部分检查/E2E，执行时组合成一次完整门槛，不无理由重复。TEST_DATABASE_URL 用独立实例；共享测试库含固定 ID，整套 suite 不并发抢同一库。打包 schema 必须用固定 XSD；Docker/阅读器隔离启动只在实施验收阶段执行。

纯本轮规划仅运行 task/context 引用检查、文档一致性和 diff whitespace；不运行产品全套测试。

## 5. 完成、提交与恢复规则

- 子任务只有独立验收完成才标完成；父任务等待全部交付与跨子验收。关键真实外部验证未完成时保持未完成并列阻塞，不能用“mock 全绿”代替。
- 计划可按依赖分批评审/提交；是否 push/创建 PR 按用户后续指令。任何 PR/分支合入 main 都需要本次明确合并授权，CI 通过或“继续”不构成授权。
- 发布生产、切换服务入口和创建持久模型通道不由本计划自动执行。既有 RackNerd 独立模型试验保持原状态。
- 每批验证后记录状态和变更；回滚保持已保存扩展元数据/凭据解密能力，不 drop 新列/表，不删除仍被任务引用的 Telegram session。

迁移交接：提取规则表契约在第一批016落地前交付，016一次创建其独立非秘密记录结构，第二批仅接服务/控件。若已应用后仍需调整，使用新的前向迁移编号，不修改已应用016。
