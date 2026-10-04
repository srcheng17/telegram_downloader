# 当前业务 CLI 与项目 Skill 的可复用边界

2026-10-05 只读源码调查，供规划使用；不表示已实现业务 CLI 或 Skill。

## 现有命令与交付

- `cmd/adminctl/main.go` 只提供离线 `reset-password`、`rotate-key`，直接维护数据库和加密配置；`tools/migration/cmd/main.go` 为迁移工具，`tools/tdl-auth-helper/main.go` 是内部认证 helper。这些都不是通用业务 CLI。
- `Dockerfile` 当前构建/复制 go-api、worker、adminctl、tdl helper 和官方 tdl；新业务 CLI 需要明确构建、分发和版本对应方式。名称不能与官方 tdl 或离线 adminctl 混淆。

## 可复用应用能力

- `cmd/server/workspace.go` 统一保护业务 API。`internal/httpapi/admin_auth.go` 的管理员登录使用会话 cookie、CSRF 与精确 Origin；CLI 如经 HTTP 接入，必须遵守相同约束，只对 HTTPS 或显式本机 loopback 发送认证信息，不能把密码或会话放 argv、日志或 AI 输出。
- `internal/httpapi/taskcore_handlers.go` 暴露任务列表、创建/上传和任务动作，`internal/httpapi/metadata_document.go` 暴露文档能力，`internal/httpapi/metadata_search.go` 暴露书目搜索。`internal/app/taskcore/service.go` 有单任务读取用例，但当前缺对应 GET 详情 HTTP 路由；若 CLI 需要 `tasks show`，须补这一接口或经现有列表取得，不直接查库。现有 `copy-to-komga` 响应包含 `target_path` 宿主绝对路径，CLI 机器输出和 Skill 报告须脱敏，不能原样转述。
- `internal/app/metadata/service.go` 的 patch 仅做文档转换，不持久化。提取接口只生成候选，也不保存；前端“采用候选”只更新浏览器草稿。CLI 的命名、输出和 Skill 示例须区分本地草稿、任务提交快照和真实持久化结果。
- `frontend/src/ocr/index.js`、`frontend/src/ocr/recognizer.js` 使用浏览器 File/剪贴板和 Web Worker；`docs/development/metadata-workspace.md` 规定截图、校对文字与发送预览默认留在页面内存。图片 OCR 若纳入 CLI，需额外设计本地执行适配和同等资源边界；不能直接复用浏览器模块。AI 提取须保留发送目标、模型、字段与文字预览后的主动触发。
- `docs/development/metadata-workspace.md` 已移除 Telegram 消息新建任务来源；现有 tdl 登录是另一项账号管理能力，业务 CLI 不应改变该产品决定。

## Skill 位置和边界

- 项目现有 `.agents/skills/` 都是 Trellis Skill；`.agents/skills/trellis-meta/references/platform-files/skills-and-commands.md` 与 `.../customize-local/change-skills-or-commands.md` 允许新增项目本地 Skill，并提示避免与 `trellis-*` 内置名字碰撞。暂以 `.agents/skills/media-workspace-cli/SKILL.md` 为候选位置，名称在 CLI 命令范围确定时冻结。
- 按 `skill-creator` 规范，Skill 使用简短 frontmatter `name`/`description` 和必要的执行指引；长命令参考可按需放 `references/`。Skill 文本只是帮助 AI 选择和调用 CLI；认证、参数校验、写入约束、输出脱敏和并发保护须由 CLI/服务端实现。

## 用户已确认的首版范围与依赖

用户在本轮将首版 CLI 边界定为“全部页面功能”，不是早先建议的“核心任务与元数据”。用户另决定 Komga 存量元数据首版写回 CBZ ComicInfo；CLI 的 Komga update 要等该子任务可用，不能临时改为仅调用 Komga PATCH。

## 当前页面/接口对照（2026-10-05）

| 页面能力 | 源码锚点 | 当前可复用接口或缺口 |
| --- | --- | --- |
| 新建：Telegraph/上传、重复确认/force | `web/templates/index.html:28-46,79-89`；`internal/httpapi/taskcore_handlers.go:59-68,70-134,272-357` | `POST /download`；`POST /api/tasks/upload/init` + `PUT /api/tasks/{id}/upload-source`；上传上限 64 MiB |
| 新建：字段、历史和候选 | `web/templates/index.html:48-76,92-97`；`internal/httpapi/metadata_document.go:16-68` | schema/validate/patch/history；patch 只计算，不保存；`frontend/src/shared/metadata/draft.js` 拥有 set/clear/unlock/逐字段采用 |
| 新建：多图 OCR | `frontend/src/ocr/images.js:1-75`；`frontend/src/ocr/recognizer.js:1-48` | 浏览器 File/Worker，同源 Tesseract 7.0.0；CLI 缺本机图片/剪贴板 adapter。上限 10 张、10 MiB/图、50 MiB/组、1200 万像素、8192px/边 |
| 新建：规则、AI、公开书目 | `frontend/src/ocr/rules.js:58-95`；`frontend/src/ocr/index.js:149`；`frontend/src/metadata-search/index.js:84,139,167` | 规则执行仅 JS；AI `POST /api/metadata/extract`；providers/search/resolve API 已有，候选仍须本地采用 |
| 任务：概览、筛选/分页、动作、文件 | `web/templates/logs.html:18-124`；`frontend/src/logs/state.js:1-22`；`internal/httpapi/taskcore_handlers.go:59-68` | summary/logs/tasks/download/cancel/retry/copy 已有；app service 有 `GetTask`，但缺独立 `GET /api/tasks/{id}` 路由。copy 响应含 `target_path`（:268），CLI 不应直接转述 |
| 设置：下载 | `web/templates/settings.html:13-37`；`internal/httpui/handler.go:79-82,133-159` | 目前走 HTML `POST /settings`，缺稳定 JSON 读写 API |
| 设置：来源、AI、字段、规则 | `web/templates/settings.html:39-61`；`internal/httpapi/source_settings.go:19-25`；`internal/httpapi/metadata_document.go:24-42`；`internal/httpapi/extraction_rules.go:14-30` | API 已有；规则设置 API 只存储/验证，不执行规则 |
| 设置：连接、安全 | `web/templates/settings.html:62-75`；`internal/httpapi/telegram_account.go:40-46`；`internal/httpapi/admin_auth.go:56-59` | 账号/尝试/事件/2FA/取消、管理员 session/login/logout/password API 已有 |
| Komga 存量管理 | `../10-05-komga-metadata-management/prd.md` | 当前尚未交付列表/详情/ComicInfo 回写 API，属于依赖任务 |

`internal/httpapi/admin_auth.go:84-150` 对所有受保护路由执行会话检查；非安全方法还要求精确 Origin/Referer 与 CSRF。CLI 不能只复制浏览器 cookie 而遗漏这些条件。现有 `/api/telegram/downloads` 路由仍在 `internal/httpapi/telegram_download.go:14`，但产品入口已移除，不能为 CLI 暴露新的 Telegram 消息提交命令。

`package.json` 已依赖 `tesseract.js@7.0.0`，可研究 Node CLI 复用；`frontend/src/ocr/recognizer.js` 仍是浏览器 Worker 包装，不能直接在 Node 执行。Node CLI 复用 `frontend/src/shared/metadata/draft.js` 与 `frontend/src/ocr/rules.js` 的纯逻辑需要契约测试确认，图片读取/解码另做本机适配。
