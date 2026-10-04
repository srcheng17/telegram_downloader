> 最新范围（2026-10-05 UI v3）：安装指定设计技能后的控制性设计见 [redesign-v3.md](redesign-v3.md)，验证见 [redesign-v3-verification.md](redesign-v3-verification.md)。一级导航为新建任务、任务、设置；桌面侧栏可完全隐藏，设置分七个 Tab；Telegram 消息来源不在新建页，tdl 登录保留在设置「连接」。下文为第一批历史方案。

> Execution update (2026-10-04): Batch 1 was approved and implemented. The full release gate passed; status is review; the planning statements below are historical. Actual scope and results are tracked in [the Batch 1 report](../10-04-clipboard-ocr-tdl-ui/batch-1-verification.md). No commit or deployment.

# 中文工作台布局与模块接缝

## 当前边界

现有 `frontend/src/app.js` 与 `shared/page_modules.js` 管理 htmx 挂载；home、logs、settings 已有独立状态和 API 模块。当前首页把统计和七个固定输入放在主区域，历史直接按七字段回填。新框架把元数据编辑变成独立共享模块，保留现有任务行为，由 root 迁移旧入口调用；不在模板或 editor 复制任务业务规则。

## 视觉系统与页面

采用父任务已确定的蓝白工作台：画布 `#F3F6FA`、工作面 `#FFFFFF`、正文 `#182435`、次文 `#526175`、主操作 `#2459C4`、边界 `#D4DDE8`。中文系统字体 PingFang SC、Microsoft YaHei、sans-serif，正文 16px/1.6、辅助 13–14px、标题 26px。内容左对齐，正文行长以约 70 字符为目标；不用远程字体、装饰统计卡片或营销横幅。

桌面 208px 导航 + 可伸缩主区；主内容最大 1520px，分区间距 24px。新建页证据/编辑器约 44:56 且列设 min-width:0；960px 以下改单列，手机导航折叠为可访问菜单。表单/预览圆角 8px，按钮 6px；主要靠间距与分界区分层级，避免每个字段各套卡片。主操作蓝只用于本页推进动作。

```text
导航             新建任务                    管理员 / 退出
新建任务         来源：Telegram | Telegraph | 本地归档
任务             链接/文件、输入校验与来源说明
Telegram         ┌证据工作区──────────┬元数据确认─────────────┐
设置             │截图 / 识别 / 搜索 │常用字段、字段差异     │
                 │本页只保留当前作品 │创作者、出版、标识符   │
                 │候选原文与来源     │自定义、仅应用内提示   │
                 └───────────────────┴───────────────────────┘
                 最近填写 / 校验反馈               开始任务
```

| 页面/入口 | shell 职责 | 业务模块接缝 |
| --- | --- | --- |
| `/` 新建任务 | 来源切换、证据区、元数据、历史、提交状态布局 | 旧 Telegraph/上传由 root 接线；OCR/search/tdl 注入 |
| `/logs` 任务 | 列表/详情结构与中文导航标签“任务” | 复用 logs presenter、动作、分页/轮询；保留 URL |
| `/telegram` | 账号页容器、不可用/加载反馈 | tdl module 负责真实状态/二维码/2FA |
| `/settings` | 下载行为/来源、AI 与自定义字段分组、挂载槽 | auth settings/integrations 负责请求、密钥状态；root 绑定 metadata-fields 设置 |
| `/auth/login` | 无业务导航的登录表单/反馈/焦点 | admin_session 负责会话、preauth CSRF、登录/退出 |

旧 DOM ID 如 `#content`、`#download-form`、`#logs-page`、`#settings-form` 和反馈区域保留。统计移出新建首屏，旧 summary 挂载由 root 同步停用或迁入任务页。字段控件改造先交付 ID/映射清单；不得保留两组可独立编辑的七字段 DOM 与 document。

## 单一草稿与注册表

`frontend/src/shared/metadata/` 拥有客户端 schema 解码、draft、candidate 差异与 editor。权威字段/类型/边界来自 `GET /api/metadata/schema` 和 metadata-contracts；前端只有显示及输入转换，无第二份字段注册表。structured 标准字段如 publication_date、identifiers 使用注册定义所声明的渲染器；custom.user.* 只接受 string/string[]/integer/boolean。

按常用、创作者、出版、分类/分级、标识符/来源、自定义分组；分组是展示信息，不限制可存字段。bool 使用明确的未知/是/否状态，数值 0/false 与 absent 分开；number 使用文本控件，count/volume 使用各自整数定义；日期保留年/月/日精度。aliases 无直接 ComicInfo 字段，显示“仅应用内保存”。停用定义和未知版本已有值只读保留，不因当前 schema 加载失败而删掉。

建议模块接口（名称由本任务冻结，payload 不重新定义）：

```text
createMetadataDraft({document, definitions, definitionsVersion})
  getSnapshot(), subscribe(listener), setField(key,value), clearField(key), unlockField(key)
  previewCandidate(MetadataCandidate), applyCandidate(candidate,selectedKeys), dispose()
createMetadataEditor({root,draft,definitions,win,doc}) -> {mount,unmount}
createWorkspaceShell({root,adapters,draft,win,doc}) -> {mount,unmount}
```

候选结构精确遵循 metadata-contracts 的 canonical MetadataCandidate。OCR/search/history 只调用 preview/apply，不直接改 input.value 或 draft.fields。人工编辑/清空推进文档及字段 revision 并建立锁；重复/迟到 candidate 在页面代际、input/config/definitions 版本或字段 revision 不匹配时标记过期，不自动采用。

“采用空字段”只处理 absent 且未锁定字段，cleared 不是空缺。已编辑字段仍可由用户逐项确认替换，但必须匹配当前 revision；整次选择失败保留原草稿。前端状态规则与 Go domain 共用合成契约样例做一致性测试，后端提交校验仍是最终裁决。

每个草稿一个运行时对象，DOM 是投影。旧七字段协议由 root 在边界统一生成投影，不能独立维护第二套字段状态。任务提交前调用后端 validate/patch 时携带版本；它们首版无持久化，响应只有基线仍匹配时采用。当前页面编辑不写 localStorage/sessionStorage 或草稿数据库。

## 历史与产物信息

历史使用明确模式：“提交时填写”读取任务 metadata_document；“最终归档元数据”读取父任务的 effective_metadata_document（包含原包补充、实际 PageCount 等派生结果）。结果缺失显示原因/暂无；失败任务、旧任务不会从提交值伪造产物快照。

点击记录先选要采用的视图和字段，再走 canonical candidate 差异预览；不直接清空未选字段或重置下载来源。当前定义与旧快照不一致时保留来源版本，只允许经过显式兼容转换的值写入当前草稿；类型变化提示无法直接采用。

## 鉴权、异步与生命周期

auth/session 状态由 source-settings-auth 的 admin_session 统一提供，shell 只展示 checking/anonymous/authenticated/expired/error。checking 时不挂载业务模块；匿名显示登录；失败提供重试，不能当匿名成功。登录成功由真实 session 回读确认，return path 由 root 同源白名单校验。页面和 API 保护始终由服务端执行，隐藏按钮不算授权。

API CSRF、401 和 HTML/htmx 登录响应由 root 的共享 transport 处理；shell 订阅失效事件，停止所有请求/轮询并清理密码、QR、截图、OCR 与未提交敏感草稿后展示中文说明。鉴权模块管理 CSRF，editor 不接收或存储秘密。

普通导航离开有未提交草稿时先显示“继续编辑/放弃并离开”，用户取消则不发导航请求、不 unmount。已确认离开才 dispose 草稿/证据。失败 HTTP swap 保留当前模块；成功 swap 清理监听/AbortController/timer/object URL。保留 mount/unmount 幂等及 mount generation 门控，卸载后的响应不能渲染或重启轮询。

保留 `#content hx-history-elt`；所有敏感工作区/登录/Telegram/设置区域禁止 htmx history snapshot，实际浏览器检查本地缓存。后退/前进及 BFCache pageshow 先重新校验会话再挂载；敏感内容不从历史恢复，普通任务过滤器依既有规则恢复。具体共享事件入口由 root 修改 app/page_modules，不在每个业务模块各加一套。

## Batch 1 fake adapter 与交付边界

注入接口提供 registry、session、task list、历史和 candidate 假数据，覆盖成功/加载/空/失败/迟到状态。fake 只在 Node fixtures 或显式测试浏览器路由使用；生产 factory 缺真实 adapter 时显示不可用，不使用假成功兜底。

shell 暴露 `data-module-slot`：evidence、metadata-search、telegram-account、source-settings、ai-settings、metadata-fields、extraction-rules。metadata-fields 供 root 绑定 `GET/PUT /api/settings/metadata-fields` 的受限定义设置（expected_definitions_version）；shell 不实现另一套可执行 schema 或自定义脚本。定义保存后通过共享事件通知 editor：旧候选失效，当前值及快照保留；用户刷新定义时做兼容校验，不静默重解释或清空既有字段。业务 worker 提供独立 module/partial 及 mount/unmount，root 在本批和后续批次串行绑定；shell 不实现这些模块的网络请求或状态机。

## 可访问性与视觉验收

语义 nav/main/form/fieldset 与 labels、错误摘要和字段 aria-describedby 配套；状态使用可控 aria-live，避免轮询反复朗读。候选差异同时给出原值、新值、来源和操作，不能只标颜色。弹窗有 Escape、焦点限制/恢复；移动菜单键盘可展开/关闭；截图排序预留按钮，不仅支持拖动。

在 1440×900、1024×768、390×844、360×800 和 200% 缩放检查中文密度、长文件名/URL/标签、焦点与错误；主操作不遮挡输入或软键盘。无自动入场动画；用户触发过渡遵守 prefers-reduced-motion。用实际浏览器截图审查布局，Node 测试不替代视觉与键盘验收。
