# Research: 现有真实操作链路与分步向导边界

- Query: 截图/链接/归档到候选、确认、任务和 Komga 的当前操作是否连续；如何减少选择并支持前进/返回？
- Scope: internal，当前工作树静态只读研究；不等同于生产浏览器验收。
- Date: 2026-10-07

## Findings

### 1. 当前实际路径

| 阶段 | 实际页面、动作和默认值 | 连续性问题 / 证据 |
| --- | --- | --- |
| 添加作品 | `/` 先选 Telegraph 链接（默认）或上传归档；链接/文件二选一必填。归档支持 ZIP/CBZ/RAR/7Z，64 MiB。 | 截图是辅助元数据，不是作品来源；只给截图不能生成下载任务。`web/templates/index.html:28`、`:34`、`:40`、`:44` |
| 作品信息 | 同一长页面左侧元数据、右侧证据标签，底部提交；另设“前往提交 ↓”锚点。 | 没有步骤完成条件、下一步或上一步；本质是并列工具面板。`web/templates/index.html:48`、`:54`、`:56`、`:79` |
| 截图识别 | 粘贴/选图后队列识别；简中+英语默认选中，其他语言可选；每图可编辑、重试、采用原文、重排或移除。 | 图片加入后展开校对面板，未自动进行规则提取或进入最终核对。`frontend/src/ocr/index.js:73`、`:98`、`:173` |
| 规则候选 | 点“使用本地规则提取”→候选中的“核对此候选”→到元数据面板勾字段→“采用所选字段”。 | 每个无冲突字段也默认不勾；必须在两个面板间理解候选与草稿的区别。`frontend/src/ocr/index.js:42`、`:66`、`:126`、`:183`；`frontend/src/shared/metadata/editor.js:204`、`:237` |
| AI 候选 | 打开 AI details→生成发送预览→可编辑文字→核对目标/模型和字段→勾选同意→发送→打开候选→逐字段采用。 | 默认只抽 title、writer、summary、tags；aliases、translator 等虽可能可提取，默认未选，形成“识别不到”的感知。`frontend/src/ocr/index.js:131`、`:138`、`:184`、`:189` |
| 书目补全 | 切书目 tab→重新输入关键词→勾来源（全部初始未勾）→确认搜索→选记录详情→逐字段采用。 | 未从已有标题预填关键词；已启用来源仍需每次再选。`frontend/src/metadata-search/index.js:165`、`:171`、`:175`、`:180`；详情/候选入口 `:67`、`:119` |
| 创建任务 | 点底部“开始下载”；重复来源可能再选生成新 CBZ/使用已有文件。 | 成功只显示“已加入队列”和“查看任务”，未自动展示该任务进度；上传 init 后即出现任务入口，离开会中断上传。`frontend/src/home/index.js:179`、`:197`、`:300`、`:319`、`:332`；`frontend/src/home/submit_flow.js:15` |
| 完成与去向 | `/logs` 里的成功动作按保存的 download_action_mode 走浏览器下载或 Komga copy。 | 按钮文案仍恢复“下载”，实际操作却可能是复制到 Komga，目标语义不直观。`frontend/src/logs/action_controller.js:63`、`:157`、`:173`；`frontend/src/logs/task_actions.js:63` |
| Komga 收录 | copy endpoint 检查成功产物资格，复制并返回目标路径；作品库另在 `/komga` 选择书库、搜索、选书。 | copy 不调用 scan、不取得 book ID、不验证收录、也不跳到作品；“文件已复制”不能等于“Komga 已收录”。`internal/httpapi/taskcore_handlers.go:254`、`:278`、`:284`；`internal/app/tasks/komga_copy.go:32`；`frontend/src/komga/index.js:72`、`:84` |

当前元数据控件本身没有把所有字段设为必填；负担主要是显式候选采用和分散步骤，不能把问题简单描述成“表单必填项过多”。编辑器已将信息分组，默认只展开“作品信息”（`frontend/src/shared/metadata/editor.js:141`、`:161`），应复用而非再做一套 schema 表单。

### 2. 状态保存与返回的关键事实

- OCR 与书目标签当前都在同一 home 生命周期内挂载；tab 切换不销毁唯一 draft（`frontend/src/home/index.js:434`、`:442`）。这是向导内保存状态可复用的基础。
- **真正切页**会 dispose OCR queue、书目请求、shell draft；`shell.unmount()` 最终 `draft.dispose()`（`frontend/src/home/index.js:496`；`frontend/src/ui_shell/index.js:39`）。普通链接跳转后返回不恢复未提交草稿。
- `canLeave` 检查元数据或 OCR dirty 并弹出放弃确认（`frontend/src/home/index.js:557`）。OCR 的 dirty 判定只看图片/文本存在（`frontend/src/ocr/index.js:197`），而提交成功只 mark metadata revision clean（`frontend/src/home/index.js:254`）；因此提交后仍保留图片时，离页仍可能提示未提交识别文字。
- 最新输入/配置/字段 revision 和 AbortController 会使迟到 AI/来源候选失效，避免覆盖手改（`frontend/src/ocr/index.js:27`、`:144`、`:152`；`frontend/src/shared/metadata/editor.js:235`）。向导改版必须保留这些能力，自动推进不代表自动覆盖。
- `hx-history="false"`、草稿仅内存，以及离页/401 清理是现有明确隐私契约；不能为了浏览器后退方便把截图/OCR/草稿塞入 localStorage、URL 或 htmx 历史。参见 `.trellis/spec/frontend/workspace-lifecycle.md`、`candidate-adoption.md`。

### 3. Telegram 是明确的旧范围决定

当前前端未调用 `/api/telegram/downloads`；后端仍接收 message_url、force、metadata_document（`internal/httpapi/telegram_download.go:14`、`:19`、`:40`）。但这不是可直接当 bug 恢复的漏接：

- `.trellis/tasks/10-04-media-workspace-integration/prd.md:1` 和 `.trellis/tasks/10-04-tdl-account-download/prd.md:1` 明确记录 **2026-10-05 用户移除新建任务的 Telegram 消息下载来源，保留账号设置和后端历史**。
- `.trellis/spec/frontend/workspace-lifecycle.md` 同样规定首页 Telegraph/上传，`/telegram` 跳到设置连接。
- 本次图像来自 Telegram，并不自动推翻旧决定。新任务可覆盖“用 Telegram 截图识别作品信息”，恢复消息下载入口应列单独产品决策，不能悄悄扩大范围。
- 旧 Telegram metadata preview 任务只做过只读关联消息验证，正式表单/API 未实现（`.trellis/tasks/10-04-telegram-metadata-preview/prd.md:20`）。若后续用户明确要消息链接自动预填，应在旧任务复用研究，不重做账号/tdl。

### 4. 建议的最简向导（规划建议，尚未获实施批准）

采用一个 workspace 生命周期、三个用户步骤和一个结果屏，而不是每一步 htmx 卸载：

1. **添加作品**：接受现有链接或归档、可附截图；自动识别输入类型，截图到位自动 OCR。来源可先缺省以支持“先截图，后补归档”，最终提交才要求实际作品来源。
2. **核对信息**：显示一份待提交作品摘要；默认集中展示不确定/冲突/缺少关键项，其他字段折叠但可编辑；提供“上一步”。规则输出和 AI 输出采用同一候选协调器，可信空字段可批量预选，手工锁/冲突保持显式处理。
3. **确认并开始**：清楚展示作品、来源和交付目标，只需一次最终确认；成功自动进入本次任务进度，重复提交不会新增任务。高级规则/语言/字段集合/重抓开关折叠。
4. **处理结果**：真实显示打包、文件复制与 Komga 收录三个可辨别结果。已确认去向可自动继续 copy/scan/readback；未验证收录不能称全部完成。失败直接提供本阶段重试，完成后进入对应作品或下载产物。

AI/书目自动外发的产品决策必须明确：当前 spec 与旧 PRD 要求每次主动确认目标/文字/来源，不能仅删勾选框便宣称自动化。可把授权设计成首次可见且可撤回的“此流程使用已配置 AI/书目补全”，日常不再逐次选择字段和来源；最终是否采纳由主任务方案定稿并同步旧契约。单纯简化为预填关键词、默认勾已启用来源、生成发送预览与发送合一按钮，也能在保留一次主动发送动作的情况下减少步骤。

默认自动填充应先限定“有直接证据、当前空值、无矛盾、未手改”的字段；不能用小模型自报置信度直接覆盖。无结果时仍能手动完成，来源选择和自定义字段映射转到高级选项。管理员配置已经保存的连接/模型无需每次要求重选。

### 5. 可测验收建议

- 给定已经配置连接的正常输入，主要路径不再要求重复选模型、全部提取字段、书目来源；量化“主动点击 / 必须决策次数”与当前基准比较，避免仅凭截图主观评价。
- 任意步骤返回后，文件引用、图片顺序、OCR 手改、候选和人工修正全部保留；再次前进不重复 OCR/AI/搜索。替换来源只失效相关结果，并明显标记。
- 自动前进只发生于当前用户动作对应的当前 generation 成功；用户返回/继续编辑后迟到结果不能把页面拉走或覆盖。失败停留在可恢复步骤，不强制从头开始。
- 无冲突结果默认一次核对；冲突/人工锁才提出具体问题。缺失可选值保持未知，不能为了“补全率”编造。
- 提交中按钮幂等；成功聚焦本任务状态；后退到信息步骤不二次创建任务，也不显示误导的未提交 OCR 提示。
- Komga 目标验证到实际 book ID 与可见元数据；复制成功但 scan 不可用明确显示待收录，并可安全续跑，不能重新下载或覆写来修复同步。
- 键盘、360px 窄屏、读屏步骤标题/焦点、失败定位均覆盖；无截图/无 AI/无来源配置时基本 Telegraph/上传仍可完成。
- 保留 manual clear/lock、preflight、CAS、401、BFCache、取消、过期来源和旧候选拒用测试；用户体验改进不损坏现有安全/一致性边界。

### 6. 既有任务去重

| 任务 | 当前 task.json 状态 | 新任务关系 |
| --- | --- | --- |
| 10-04-clipboard-ocr-tdl-ui | planning | 媒体工作台父需求；引用原能力，不重复创建完整平台 |
| 10-04-modern-ui-shell | review | 已有 shell、分组编辑器、tabs 和生命周期；新任务改流程编排 |
| 10-04-multi-image-ocr-ai | review | 已有 OCR/规则/AI 候选契约；新任务重点识别效果与操作自动化 |
| 10-04-metadata-provider-search | review | 已有三源与 resolve/adoption；新任务改默认值、触发时机和集中核对 |
| 10-04-media-workspace-integration | planning | 已拥有跨模块打包/真实链路验收；复用证据，新增向导验收 |
| 10-04-tdl-account-download | review | 保留账号/后端；不自动恢复已移除 UI |
| 10-04-telegram-metadata-preview | planning | 消息预填另有研究，只有重新纳入来源才扩展 |
| 10-05-komga-metadata-management | in_progress | 存量 CBZ 文件写回独立；新导入完成→作品页导航可集成，勿重写编辑器 |

## Files Found

- `web/templates/index.html`：当前全部新建工作区 DOM 与提交入口。
- `frontend/src/home/index.js`：表单协调、唯一 draft、模块挂载/卸载与提交反馈。
- `frontend/src/ocr/index.js`：识别、规则、AI 前置选择与候选操作。
- `frontend/src/metadata-search/index.js`：来源、关键词、搜索与详情候选。
- `frontend/src/shared/metadata/editor.js`、`ui_shell/index.js`：动态表单、候选采用及内存草稿生命周期。
- `frontend/src/logs/action_controller.js`、`task_actions.js`：完成后的下载/copy 分派。
- `internal/httpapi/taskcore_handlers.go`、`internal/app/tasks/komga_copy.go`：文件复制边界。
- `frontend/src/komga/index.js`：书库浏览与既有文件编辑入口。

## Related Specs

`.trellis/spec/frontend/index.md`、`workspace-lifecycle.md`、`candidate-adoption.md`、`state-management.md`；`.trellis/spec/backend/index.md`、`komga-edit.md`；`docs/development/module-boundaries.md`。

## External References

本研究没有访问外部文档或服务；实际栈为项目现有 Go/原生 JavaScript/htmx/Vite，未引入框架建议。

## Caveats / Not Found

- 只读代码证据，不是本轮用户图片的准确率实测；OCR/AI 实测由主任务独立记录。
- 当前工作树含其他已授权任务未提交修改，研究未改动产品代码或旧任务。
- Komga copy 当前后端只返回 ok/task_id/target_path；历史摘要里的 `komga_indexed: unverified` 可能是 CLI 包装契约，不能当成本 HTTP endpoint 现有字段。
- 恢复 Telegram 下载、自动外发、自动提交/自动入库是不同决定；“减少选择”不能合并成全自动执行授权。

## Planning artifact fact-check (2026-10-07)

只读核对本任务 `prd.md`、`design.md`、`implement.md`；自动化终点明确标记待决定，Telegram 范围与已有决定一致，未发现需要因此阻止本轮任务创建/研究完成的问题。计划列出的 frontend/CLI 命令与 package.json 一致，OCR browser 脚本存在。

一项设计需收紧：`design.md` 的“丢响应先查询已创建 task”不能只靠前端流程实现，尤其上传 init 响应丢失时客户端尚无 task ID。当前 HTTP 创建端自己 `uuid.NewString()`（`internal/httpapi/taskcore_handlers.go:93`、`:322`），上传创建未暴露请求幂等键，service 使用每次提供的新 ID 建任务（`internal/app/taskcore/service.go:117`）。建议将 R5/AC6 的响应丢失验收依赖写明：先设计服务端受会话保护的稳定创建幂等键/结果回查契约，再实现客户端恢复；不能用禁用按钮或猜测同名最近任务证明幂等。Telegraph 来源去重也不等于 force=true 或上传创建的请求幂等。
