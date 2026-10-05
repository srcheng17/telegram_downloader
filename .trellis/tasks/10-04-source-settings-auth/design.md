> Execution update (2026-10-04): Batch 1 was approved and implemented. The full release gate passed; status is review; the planning statements below are historical. Actual scope and results are tracked in [the Batch 1 report](../10-04-clipboard-ocr-tdl-ui/batch-1-verification.md). No commit or deployment.

# 设计：管理员会话与来源配置

## 当前差距与边界

`cmd/server/main.go` 先注册 `httpui`、`httpv2` 设置，再挂 `httpapi`；当前没有统一管理员会话。`app_settings` 仅保存四项下载设置，不能塞入秘密或扩展其公开快照。最小改变是根路由的认证边界、独立管理员/来源配置服务与仓储，以及 shell 中的登录/集成设置视图。Task Core 生命周期和 worker 不因本任务另建状态机。

## 模块归属与并行接口

| 归属 | 拟新增/修改文件 | 职责 |
| --- | --- | --- |
| 本子任务 | `internal/app/adminauth/{service,types}.go` 与测试 | 单管理员密码、会话、期限、吊销和仓储端口 |
| 本子任务 | `internal/app/sourcesettings/{service,types}.go` 与测试 | 来源/AI配置校验、版本、只读公开投影、凭据操作 |
| 本子任务 | `internal/store/postgres/adminauth/`、`internal/store/postgres/sourcesettings/` | pgx 仓储；不修改 Task Core 表 |
| 本子任务 | `internal/credentials/`、`internal/modelapi/` | 标准库 AES-GCM 封装；受控 OpenAI-compatible models/test 客户端 |
| 本子任务 | `internal/httpapi/admin_auth.go`、`source_settings.go` 与测试 | handlers、认证/CSRF middleware 构造器；不自行改共享路由装配 |
| 本子任务 | `frontend/src/settings/integrations.js`、`frontend/src/settings/integrations_api.js`、`frontend/src/shared/admin_session.js`、相关 Node tests | 设置模块、会话状态与跨页令牌消费；不重构 shell |
| modern-ui-shell | 登录/设置 DOM、`web/templates/{base,settings}.html`、页面入口、共享样式 | 提供稳定挂载点，接入本模块；秘密 input 不生成预填 value |
| 父集成 | `cmd/server/main.go`、`internal/config/`、共享 router、`frontend/src/{app,settings}.js`、Vite/生成 dist、部署配置 | 装配 middleware/services、受信 origin、secret file、UI 和构建 |
| 迁移契约归本模块，父集成落地 | `internal/store/postgres/migrations/016_admin_source_settings.sql` | 预留 016；不得抢用 metadata-contracts 的 015 或并发改迁移 runner |

`provider-search` 拥有 `GET /api/metadata/providers`、公共能力目录与搜索/详情适配器；本任务提供 `PublicSourceConfig`（enabled、priority、credential_configured、config_version、filters、field_preferences）和仅服务端 `ResolveCredentials(ctx, providerID)`。配置服务消费适配器提供的能力白名单，负责 noauth/凭据合法性，不另建能力目录。`config_version` 是仓储、设置 API 和共享 DTO 的唯一配置版本；provider-search 对外若使用 `config_revision`，它只是同一个 `config_version` 整数的字段映射，不能维护第二套 revision。适配器不能接收浏览器任意 headers。公开 DTO 与解密后的内部对象分离，后者不可 JSON 序列化、写日志或放入任务 metadata。

## 单管理员与会话

- 固定唯一 admin 身份，不引入用户表的多角色抽象。migration 创建 singleton `admin_account`（id=1、password_hash、credential_version、timestamps）、`admin_sessions`（随机 token 的 SHA-256 摘要、CSRF 摘要/关联秘密、创建/last_seen/到期、credential_version、preauth 标记），并建到期索引。
- bootstrap 只在无管理员的事务中读取 `ADMIN_BOOTSTRAP_PASSWORD_FILE` 或私密 `ADMIN_BOOTSTRAP_PASSWORD`，二者同时提供时报错；file 必须为进程所属 regular file、owner-only、拒绝 symlink。既有账户重启不重设密码；本地恢复只能通过明确的受控 reset 操作，并递增 credential_version、吊销所有会话，不能靠改 env 自动重置。
- 复用已有 `golang.org/x/crypto/bcrypt`，生产 cost 12，密码至少 12 字符、最多 72 UTF-8 bytes，超过上限明确拒绝而非截断；数据库只存加盐 hash。测试可注入较低 cost。密码/hash 不出现在日志/配置 JSON。
- 会话 token 使用 `crypto/rand` 32 bytes，浏览器 cookie 为 host-only、HttpOnly、Secure、SameSite=Strict、Path=/；服务器只存摘要。部署使用明确 `APP_PUBLIC_ORIGIN`，不盲信 Host/X-Forwarded-*；只有显式 loopback 开发模式可用 HTTP cookie，生产远程 UI 必须 TLS。
- 绝对 12h、空闲 30m；按服务端时间校验，last_seen 有界更新。成功登录销毁 preauth 并生成新 session/CSRF；退出删除当前会话，改密需当前密码并吊销全部旧会话，要求重新登录。数据库故障拒绝鉴权，不放行。
- 登录有按可靠 client IP 与总量的有界速率限制、统一失败文案；拒绝无限延长账户锁定。当前单 API 进程使用有上限/过期的内存限流器，未来多 API 实例前必须迁为共享计数，不声称当前已支持分布式限流。
- HTTP middleware 在根装配处默认保护所有路由。匿名白名单仅 `GET /auth/login`、`GET /api/auth/session`、`POST /api/auth/login`、指定静态资源、`GET /healthz`、`GET /readyz`。会话接口仅返 authenticated/CSRF/必要期限；匿名 preauth 有短期 TTL 和数量限制。保留现有 Compose/E2E 探针路径，仅返回现有最少状态或通用错误；不得展示数据库详情、版本、配置或任务信息。新未知路由默认受保护，不能使用宽泛健康前缀豁免。
- 页面匿名访问导航到登录页，返回地址仅允许站内路径；API/SSE 返回 401，旧 `/v2` 的错误 envelope 保留兼容；htmx 不能把整页登录 HTML 混入业务片段。所有敏感响应 `Cache-Control: no-store`；shell 禁止密码、QR、OCR 的 htmx history snapshot，退出清理页面状态。活跃敏感 SSE 最长每 30 秒重新检查会话并在吊销时关闭。

## CSRF 与路由契约

登录也要求 preauth CSRF 及精确同源检查。所有 POST/PUT/PATCH/DELETE 使用服务端关联 CSRF：JSON/上传走 `X-CSRF-Token`，HTML form 走隐藏字段。校验 Origin（无 Origin 的浏览器表单检查 Referer origin，均缺失则拒绝），不允许跨源 CORS；不以 SameSite 单独替代 token。代理信任只来自固定配置，不接受客户端伪造头。

| 方法/路径 | 契约 |
| --- | --- |
| `GET /api/auth/session` | 匿名也可用；签发/读取短期 preauth 或已登录 CSRF，不返回密码/hash/session token |
| `POST /api/auth/login` | `{password}` + preauth CSRF；成功新 cookie，会话 fixation 不可复用 |
| `POST /api/auth/logout` | 已登录 + CSRF；删除当前会话并过期 cookie |
| `PUT /api/auth/password` | `{current_password,new_password}` + CSRF；成功吊销全部会话 |
| `GET /api/settings/sources` | 公开来源配置投影，不解密返回秘密 |
| `PUT /api/settings/sources/{provider}` | `{expected_version,enabled,priority,filters,field_preferences,credential:{action:keep|replace|clear,value?}}`；非秘密配置完整提交，filters/field_preferences 仅接受该适配器声明的字段和取值；不支持的选项返回 422 |
| `POST /api/settings/sources/{provider}/test` | `{config_version}` + 管理员会话/CSRF；按适配器能力对固定非敏感样例执行只读书目请求，不接受当前 OCR、关键词、URL、headers 或自定义请求体；不支持时明确反馈 |
| `GET /api/settings/ai` | Base URL、enabled、model_id、版本、credential_configured、独立发现/测试状态 |
| `PUT /api/settings/ai` | 完整非秘密配置 + expected_version + credential action；成功原子递增版本 |
| `POST /api/settings/ai/models` | `{config_version}`；使用已保存地址/凭据 GET 上游 `/models`，不接受临时 URL/token |
| `POST /api/settings/ai/test` | `{config_version,model_id}`；显式测试已选择的模型，固定无敏感样例；不可传 OCR、messages、工具或自定义请求体 |

新 `/api` 错误保持 `{error,code,message?,details?}`，401 未登录、403 CSRF/origin、409 config conflict、422 配置无效；上游错误以受控 code 表达 unauthorized/forbidden/rate_limited/timeout/unsupported/invalid_response/redirect_blocked/unreachable，禁止直接返回上游 body。模型成功与业务内容测试成功独立，不把 HTTP 200 算提取成功。

## 来源、密钥与版本

- `source_settings` 按 provider_id 主键保存 enabled、priority（整数 0–1000，默认 100）、受控非秘密 config（filters、field_preferences）、auth_mode、ciphertext/nonce/key_id、config_version、credential_version、updated_at。priority 越小展示/查询次序越靠前，同值按 provider_id 稳定排序；优先级不自动采用候选或覆盖人工字段。AI 是独立保留 provider_id，不把秘密写入 `app_settings` 或 metadata。保存采用 expected_version 与当前 config_version 比较的乐观并发，所有非秘密配置及 keep/replace/clear 在同一事务完成并推进 config_version；clear 没有自动回退 env 凭据。
- filters 与 field_preferences 是适配器能力白名单约束的声明式非秘密选项，具体字段/取值由 provider-search 的能力目录和 metadata-contracts 的字段标识确定。完整 PUT 必须显式提交对象，空对象表示恢复该源允许的默认项，不把缺字段静默当清除。未知键、该源不支持的过滤条件/元数据字段、非法类型或范围均拒绝，而非存下后忽略；不得借配置声明 URL、请求头、脚本或凭据。不同来源的设置独立保存、独立版本冲突。
- 32-byte 外部主密钥来自 `SOURCE_SETTINGS_MASTER_KEY_FILE` 或私密 env（互斥）；文件和 bootstrap 相同权限要求。AES-256-GCM 每次随机 nonce，AAD 包含固定用途、provider_id、credential_version，防跨来源调换。密钥不进数据库、镜像、Git、普通备份或浏览器；missing/wrong key 时拒绝需要凭据的操作，不匿名重试。
- 轮换用受控本地维护过程：停止配置写入、持有旧新 key、事务性重新加密并更新 key_id，校验读回后切换主密钥；失败回滚事务并保留旧 key。独立保护 key 备份；数据库回滚必须配套其 key 版本，不能删旧 key 后声称可恢复。
- 初始适配器矩阵：MangaBaka 和 MangaUpdates 的已验证公开检索 `none`；Bangumi `none` 或可选 personal bearer（成人权限需真实授权核验）；OpenAI-compatible AI `none` 或 bearer API key。Google Books 若后续选入需专属 API key；AniList/NDL/MangaDex 等未实现适配器不显示可用设置或假定 OAuth。API key 的 query/header 放置由选定适配器固定，日志不得包含带 key 的 URL。
- 固定公开来源只允许其内置 HTTPS origin/路径，关闭重定向；从内容返回的 URL 不成为认证请求目的地。提供者认证不共享、不同来源不自动继承，也不引入任意 headers、脚本、通用代理或 OAuth 管理框架。
- 每来源 test 由本模块鉴权、读取一致配置快照/解析凭据并调用 provider-search 提供的能力感知只读测试方法；不重复实现搜索适配器。使用适配器内置的固定非敏感书目样例，沿用其超时、响应体和速率边界，无自动重试、不更改 enabled/priority/草稿或来源数据。返回 provider_id、config_version、受控 status、checked_at 与验证范围，不返回凭据/上游正文；结果绑定配置版本，迟到测试不覆盖新配置。测试成功只证明该读取操作可用，不默认证明成人条目权限、完整覆盖率或所有选项有效；没有可测试的只读能力返回 unsupported。

## AI 目的地与发现

Base URL 规范化后只允许 HTTP(S)、hostname 与路径，拒绝 userinfo/query/fragment；精确拼接 `/models`、`/chat/completions`，不重复加 `/v1`。支持显式保存的 localhost、容器名及内网 IP；这是受管理员保护的配置能力，普通提取请求只传文字/版本，不传地址。禁环境代理自动继承及所有重定向；凭据只在此配置目标的请求中应用。规范化 Base URL 改变时，现有秘密不可 `keep`，必须明确 replace/clear；HTTPS 降级或同主机路径变化也遵守该规则。

模型发现客户端初始边界：10 秒总超时、1 MiB body、最多 1000 个唯一 id，文本 ID 最大 256 UTF-8 bytes，非法项目忽略并反馈；超限失败不能假装完整列表。进入已保存设置、确认保存或用户刷新各触发一次请求，不逐字请求、不推理。列表中的已知非文本用途禁选；未知能力保留且显示未验证。无支持/无列表时手工 ID 可保存，已保存 ID 缺失不自动清空。

UI 用 mount generation、请求序号和 config_version 同时防迟到，地址/凭据草稿变化立即标旧列表失效。服务端开始请求前取单一配置快照，返回携带版本；配置期间更改则结果不可晋升当前验证状态。推理测试主动调用、120 秒超时、1 MiB body，无重试/自动换服务，使用选中模型的固定无敏感输入及 `metadata-contracts` schema_version/definitions_version、固定测试 field_keys 与 fixture_version；只有 schema/语义检查通过才记录通过。绑定 Base URL/config_version/credential_version/model_id/schema_version/definitions_version/field_keys/fixture_version（或等价稳定契约 hash）；只显示测试字段范围，定义或测试契约变化即失效，不能显示所有扩展字段均已验证；输出正文不存库。扩展字段契约由 metadata 子任务提供，不能继续硬编码研究用七字段。

`minicpm5-2b-q4` 是用户首选建议值，不强制静默选择模型，也不预填虚假的可达 URL。现有 RackNerd loopback 地址不可直接从项目容器访问；父任务另行规划受控隧道/网络与真实调用验收。本设计不授权或执行该网络变更。

## 发布与回滚

新增表为 additive migration 016，不改旧任务数据；部署前先准备 bootstrap/master key/origin、核对现有探针和升级 E2E 登录，再开放入口。回滚前暂停对外流量，不能回退到未鉴权旧版本后原样开放；需保持经过验证的外层访问控制或维护页。保留新增表和密钥以便前滚恢复，未经独立确认不 drop 表、不删除 Telegram session、任务或文件。配置回滚是带版本的管理员操作，不能使历史会话恢复有效。
