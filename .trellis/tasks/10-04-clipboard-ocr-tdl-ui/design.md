> 2026-10-05 范围调整：用户移除新建任务的 Telegram 消息下载来源，保留 tdl 登录并迁入「设置 → 连接」；旧 `/telegram` 跳至 `/settings#connections`。后端接口、历史任务及账号数据保留。后续验收按 [UI v2 设计](../10-04-modern-ui-shell/redesign-v2.md) 执行，不再要求首页 Telegram 来源入口。

> 执行更新（2026-10-05）：第一、二批代码已实现，子任务均在 review；第二批已通过分阶段本地检查，真实外部验收与第三批尚未完成。见[第一批记录](batch-1-verification.md)、[第二批记录](batch-2-verification.md)。下文 planning 描述为原计划背景。

# 可扩展媒体工作台：共享设计

状态：最终规划草案，待整份计划审阅；没有开始产品实现。依据 [PRD](prd.md) 与其研究链接。管理员模式已由用户确认，原七字段是兼容入口，不是新模型的上限。

## 1. 架构与唯一事实源

沿用 Go API/worker、PostgreSQL Task Core、nginx、Go templates、原生 JavaScript/htmx/Vite。新增能力按 domain → application → adapter/infrastructure 分层，任务状态仍由现有 Task Core 定义。

```text
多图本地 OCR / 手工填写 / 已有归档 / 书目源
                    ↓ 候选及来源
      字段注册表 + 元数据草稿 + 用户确认
                    ↓ 版本化文档快照
        PostgreSQL Task Core → worker
                    ↓
        图片 + ComicInfo.xml → CBZ → Komga

受保护设置 → 各书目源授权 / AI 配置 / Telegram 账号
```

- 页面草稿拥有本次未提交编辑；来源/规则/AI 只返回候选，不直接更新已提交任务或历史。
- application 层校验并保存完整版本化文档，legacy 七字段从文档投影，worker 读取创建任务时的不可变快照。
- provider 适配器和 ComicInfo 适配器使用同一字段注册表；HTTP handler 不另造字段规则或任务状态。

## 2. 可扩展字段契约

共同契约由 [metadata-contracts](../10-04-metadata-contracts/design.md) 定义，本父设计规定所有子任务必须遵守的部分：

```text
MetadataDocument
  schema_version: 1
  definitions_version: immutable registry version
  definition_snapshot: validated definitions used by this document
  revision: monotonically increasing integer
  fields[key]:
    state: value | cleared
    value: registry-validated typed value (only when state=value)
    revision: integer
    manual_locked: boolean
    provenance: bounded source descriptors, not raw provider responses
```

- key 不存在表示未提供，cleared 表示明确清空；false、0、空列表不是缺省判定。候选不能自行设置人工锁或确认来源。
- 内置字段包括标题/别名、系列/卷号/计数、简介、分类标签、创作者各角色（`creators.writer`、`creators.penciller`、`creators.translator` 等列表）、出版社/品牌、出版日期精度、语言、阅读方向、分级、标识符列表和公共链接。
- `identifiers` 使用 scheme/value，不把书目站 ID 当 ISBN。`number` 允许非整数卷标（`series_number` 仅是 legacy 名称）；PageCount 从实际产物计算，不接受模型编造。
- 自定义字段使用注册后的 `custom.user.*` key；类型限定为字符串、整数、布尔及有界字符串列表。字段定义含中文标签、类型、约束、可编辑/AI 可提取能力与导出映射。无脚本、eval、任意嵌套对象或客户端指定文件路径。
- 字段 definitions 与 doc 都有版本；新增 optional 字段对旧文档兼容。删除定义只停用录入，不静默清除已有值；不兼容类型修改需新 key 或显式迁移。旧任务快照不能随当前定义重解释。
- 历史/任务保存文档，旧七字段接口保留为边界适配。若同一请求同时给文档和 legacy 值且不一致，拒绝歧义，不设两个独立真相源。新数据库列与历史迁移采用 additive 015，旧记录按需转换并有幂等验证。

## 3. 通用候选和采用操作

```text
MetadataCandidate
  candidate_id, request_id, origin
  schema_version, definitions_version
  base_document_revision, field_revisions
  input_revision, config_revision (when applicable)
  fields: key → {state, value?, provenance, warnings?}
  warnings

可批量返回 candidates: MetadataCandidate[]，不再定义第二套候选对象。
```

只有注册且允许该来源提取的 key 可以出现在候选；类型/长度/枚举/UTF-8 校验失败不会部分写入事实文档。AI 不提供任意 request URL、工具调用或注册新字段。

首版未提交草稿仅在浏览器内存，后端 validate/patch 若提供仅做无状态校验/转换，不宣称能对两个浏览器草稿 CAS；真正持久化发生在任务提交事务。前端采用操作核对页面/输入/配置及字段 revision；用户清空也推进 revision。自动预填仅允许未编辑且未锁定的空字段，其他差异列为待采用。排序/删除截图、改识别文字、改搜索关键词、变更凭据/模型都使对应旧候选失效。候选比较由一个共享 draft 模块负责，OCR/搜索模块不复制覆盖逻辑。

## 4. 采集源与管理员边界

内置单管理员；无注册、多用户或 RBAC。初始化凭据和加密主密钥只经服务器私密配置提供，缺少初始化条件时业务入口不能开放。会话绝对有效期建议 12h、空闲 30m，HttpOnly/SameSite 与 HTTPS Secure cookie，CSRF/Origin 校验写操作；登录失败有有界退避，退出/改密码撤销相关会话。

保留现有 `GET /healthz`、`GET /readyz` 为最小无敏感探针，登录页/登录 API/必要静态资源之外的业务路由全部鉴权。不要为登录引入新健康 URL 或破坏既有 Compose/E2E 探针；HTML 导航未授权与 API 401 分别处理。

SourceConfig 按稳定 provider ID 保存 enabled、priority、受支持的筛选/字段偏好、config_version、credential reference。首版书目适配 MangaBaka/MangaUpdates/Bangumi；AI 与 Telegram 用独立配置空间。每源能力声明是否匿名可用、接受何种凭据、支持哪些查询/分级；配置不能超出适配器能力。

- 凭据独立加密保存，主密钥不在数据库或 Git；返回公开配置只含 configured 状态。keep/replace/clear 显式区分，连接测试绑定配置/凭据版本。
- 来源能力目录以共享只读接口注入，不让 settings 与 search package 循环导入。批次 1 使用已冻结描述及 fake test adapter 验证配置契约，尚未安装的真实适配器明确报不可测试；批次 2 接入实际来源后验证真实只读测试。固定书目 provider 使用固定官方目标。管理员可配置 OpenAI-compatible AI Base URL，允许其明确设置的内网服务；禁止凭据跟随重定向/主机切换，截图/候选不能指定目标。
- 来源排序只用于召回/展示及空字段建议，不能代表同作确认，也不自动向所有来源发送 OCR 全文。用户明确搜索才调用所选启用源，授权失败和无结果分别显示。
- 凭据加密、会话与源设置采用 additive 016，由专属模块定义 SQL，集成负责人落号/接线；不使用现有普通 settings 字符串存储明文密钥。

## 5. 界面和模块边界

页面为“新建任务、任务、Telegram、设置”，历史在录入页按需打开。新建页下载来源在上、左侧截图/检索证据、右侧元数据；标准常用字段优先，出版/创作者/标识符/自定义字段分组展开。中文反馈说明本次采用哪些字段、哪些仅存应用，不能把 schema/密钥实现细节堆给用户。

沿用 [ui-direction](research/ui-direction.md) 的蓝白/灰蓝工作台：`#F3F6FA` 画布、`#FFFFFF` 工作面、`#182435` 正文、`#526175` 次文、`#2459C4` 主操作、`#D4DDE8` 分界；中文系统字体，正文 15–16px。核心辨识点是截图与元数据逐项核对，不添加装饰性统计卡片。移动端单列、键盘排序/焦点可见、长字段换行、遵守 reduced-motion。

通用表单消费 registry definitions 与 draft，不写死七输入框。来源配置页逐项显示启停/优先级/授权状态/测试；密钥从不回填。内置管理员登录作为整站入口，原 Telegraph、本地归档流程保留。

全局 app/page_modules、CSRF transport、导航/路由和基础模板属于共同接线面，由集成负责人收口。业务子任务新增自己的模块与 partial，不同时重写首页入口。敏感 OCR、登录二维码与识别文字不进入 htmx 持久历史快照。

## 6. OCR / AI 与书目搜索

- 图片仅浏览器处理，Tesseract.js worker/WASM/语言资源同源构建发布；首版支持简体/繁体中文、日文、英文选择，缺资源不能悄悄回退错误语言。
- 初始资源建议：一组最多 10 张、每图压缩字节 10 MiB、合计 50 MiB、解码 12MP 且单边不超过 8192px；识别按单 worker 串行，图片逐张解码/释放。通过实际桌面/移动端测试校准，超限提前反馈并保留已有草稿。
- 单图 raw/edit text、稳定 ID 和 generation 分离，合并预览按用户顺序派生；发送草稿只是可编辑快照。失败图明确排除，部分结果需用户确认，重复/重叠文字不做无依据模糊删除。
- AI 首选建议为已部署 MiniCPM5 Q4，model ID `minicpm5-2b-q4`，需用户实际选择并保存；用户点击才发送文字。JSON Schema 根据本次选择的可提取字段生成，不固定七字段、不把整个 registry 和所有自定义字段一并塞入 4096 token 上下文。
- 模型 4096 上下文包含模板/系统提示/字段契约/输出预留；扩展字段的输出预算单独验证。上游截断、`finish_reason=length`、非法 JSON 或无效字段使本次建议失败，不能采用半份结果。无精确 tokenizer 的兼容源不能以字符数伪装精确预算；有界请求并明确处理上游超限，不静默截断或自动换模型。
- 旧 90%/8.2s 等结果只适用于七字段合成样例，新字段和真实 OCR 必须重新评估。
- 书目搜索显示系列/单卷/译本、作者角色、别名与来源；同名不自动合并，公共原标签与分级分开。规则/人工输入不依赖外部查询成功。

## 7. tdl 与打包

固定官方 tdl v0.20.4 下载 CLI，登录 helper 复用对应存储/客户端包，先在隔离 namespace 验证 QR/2FA → 关闭存储 → 官方 CLI 恢复真实授权。不能用 stdout 提示、退出码或会话文件存在代替授权。

一账号、同一 active namespace 最多一个 tdl 进程。跨 API/worker 的协调采用专用 PostgreSQL advisory-lock 连接与 OS 文件锁覆盖子进程完整生命周期，限同一受控主机/共享卷，并确认实际子进程退出；登录使用候选 namespace，成功且授权复验后切换。过期 owner/generation 不能覆盖新账号状态。tdl 自身 reconnect timeout 保留 5 分钟，应用登录期限独立控制。

首版单条 Telegram 消息的归档附件进入现有 Task Core；不新增任务状态。压缩附件默认上限 500 MiB、可配置下调，先校验预报大小及磁盘预算，再证明 CLI 的实际输出受严格字节边界或硬文件系统额度约束；大小轮询不算硬上限，未验证前不开放该路径；浏览器上传仍为 64 MiB。解压保持 300 页、每页 25 MiB、合计 500 MiB 等既有保护。扩大限制需单独资源验证，不在本轮无界放开。

打包以 [comicinfo-roundtrip](../10-04-comicinfo-roundtrip/design.md) 为准：先读取原 XML，再合并已确认字段，root 单个 XML，固定 draft2.1 schema 保留 Tags。未知扩展与竞争性旧元数据保留私密元数据副本并提示未导出；备份失败不能声称无损发布。派生 PageCount/Pages 与最终页序一致，图片字节不重编码，同目录临时包校验后发布。没有标准映射的用户自定义字段留在应用数据库。

提交快照 `metadata_document` 保持不可变；读取原包并合并后由应用用例生成派生 `effective_metadata_document`，含实际 PageCount、导出 profile、警告与来源。worker 随最终产物按 task/generation 持久化结果快照，只有当前 generation 的成功产物可被提升为可见结果；失败/迟到执行不覆盖旧成功记录。历史和详情明确区分“提交元数据”与“产物元数据”，重用任一版本须明确选择。重试继续消费提交快照，不能把上一代导入字段当新的人工输入。尚未进入 registry 的原 XML 标准字段由格式适配器保留并在产物清单中列明，不伪装成缺失的已编辑字段。

首版遇损坏或多份竞争 ComicInfo 时停止并保留原件，提示修正后新建任务；不新增中途交互等待。未知可解析扩展保存副本+警告。需保留的完整原件/副本提升为受保护 source artifact 并持久登记，保留至明确删除，Complete 不得清除；空间不足阻止新增发布。首版只输出 draft2.1，2.0 仅作离线兼容对照。

## 8. 共享 HTTP 合约及所有权

以下为本轮冻结的路由命名；详细 payload 由所属子设计定义并经集成评审，修改需同步调用方。所有业务 schema/capability 接口中的“公开”指无秘密 DTO，仍需管理员登录；config_version 为来源设置权威修订，候选中的 config_revision 是其投影。

| 接口组 | 归属 | 契约要点 |
| --- | --- | --- |
| `/api/metadata/schema`、`/validate`、`/patch`、文档提交/legacy adapter | metadata-contracts + root 接线 | 管理员可读的无秘密 definitions/version 与完整文档校验，旧七字段兼容 |
| `/api/auth/session,login,logout,password` | source-settings-auth | 单管理员、CSRF、session invalidation |
| `/api/settings/sources`、`/api/settings/sources/{provider}`、`/{provider}/test` | source-settings-auth | 公开配置/凭据写入分离，乐观版本校验 |
| `/api/settings/ai`、`/models`、`/test` 子路由 | source-settings-auth | 发现与推理测试分开，服务端密钥 |
| `GET/PUT /api/settings/metadata-fields` | metadata-contracts + root 设置页接线 | 自定义定义增改/停用，版本冲突校验，无任意 schema |
| `GET/PUT /api/settings/extraction-rules` | multi-image-ocr-ai + root 接线 | 有限声明式规则、rules_version，独立非秘密持久化 |
| `POST /api/metadata/extract` | multi-image-ocr-ai | text、field_keys、schema/definitions/base_document/input/config revisions → MetadataCandidate |
| `GET /api/metadata/providers`、`POST /api/metadata/search`、`/candidates/resolve` | metadata-provider-search | capability/关键词/来源 record ID → 只读候选 |
| `/api/telegram/account`、`/login-attempts`、attempt events/password/cancel、`/downloads` | tdl-account-download | 后端账号状态、结构化事件、Task Core 创建 |

## 9. 接线、迁移和恢复

- 工作区已有七项运行修复尚未提交，新任务不得撤销或重置它们。新子任务只写独占模块；root 在每批次后统一修改 `cmd/*`、共享 router/HTTP adapter、既有 store/worker 签名、配置装配和静态资源构建入口。
- 预留 015 元数据定义/提交与产物文档/历史、016 管理员/凭据/来源及提取规则、017 历史任务身份、018 Telegram 协调、019 Telegram 输入、020–022 原件保留与打包报告。应用前重新检查最新编号，不改已应用迁移。相互独立迁移不要隐式引用尚未交付的表，依赖写进子设计。
- AI 长请求和 Telegram SSE 采用专属有界 Go/nginx deadline、SSE flush/心跳与关闭缓冲，真实代理链路验证超过30秒请求，详见集成设计；不得无界提高全站超时。
- 每批接线后只构建一次共享 dist；单模块可跑 focused tests，整套数据库测试和 E2E 由集成负责人串行或使用完全隔离数据库，避免固定测试 ID 相撞。
- additive schema 兼容旧数据；回滚应用版本前验证旧进程不会重写/清空新文档。停用 provider/OCR/Telegram 功能可恢复手工/旧下载路径；不能以数据库降级删除新增字段数据。
- 本轮不对外暴露 RackNerd 模型；实际应用容器连接需验证专用受保护通道，loopback 不是容器可用 Base URL。模型已部署不等于产品已接通。
- 发布/提交/推送/合并遵守独立授权；规划与本地验证不改变生产登录或服务路由。

## 10. 验证门槛与技术风险

真实截图 OCR、扩展字段 AI、tdl helper 交接、来源权限/覆盖、模型跨机连接、两款阅读器读取是明确验收门槛，不能以 mock 或源码存在替代。各子任务保留独立验证记录，最终由集成任务记录实际命令、隔离环境、脱敏样例与尚未通过项。

详细执行依赖、模块所有权、每批准入/退出条件与命令见 [implement.md](implement.md)。
