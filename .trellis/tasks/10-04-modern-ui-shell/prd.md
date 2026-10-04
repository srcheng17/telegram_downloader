> 最新范围（2026-10-05 UI v3）：安装指定设计技能后的控制性设计见 [redesign-v3.md](redesign-v3.md)，验证见 [redesign-v3-verification.md](redesign-v3-verification.md)。一级导航为新建任务、任务、设置；桌面侧栏可完全隐藏，设置分七个 Tab；Telegram 消息来源不在新建页，tdl 登录保留在设置「连接」。下文为第一批历史方案。

> Execution update (2026-10-04): Batch 1 was approved and implemented. The full release gate passed; status is review; the planning statements below are historical. Actual scope and results are tracked in [the Batch 1 report](../10-04-clipboard-ocr-tdl-ui/batch-1-verification.md). No commit or deployment.

# 现代中文媒体工作台框架

状态：planning；本轮仅规划，未实施产品或运行产品测试。父任务：[可扩展媒体工作台](../10-04-clipboard-ocr-tdl-ui/prd.md)。执行批次：Batch 1，与 metadata-contracts、source-settings-auth 并行。

## 目标与范围

以“下载来源 → 证据核对 → 元数据确认 → 创建任务”为中心，重设原生 JavaScript/htmx/Go templates/Vite 页面框架。使用元数据注册表构建可扩展编辑器，给第二批 OCR、书目搜索和 Telegram 模块提供稳定挂载接口。

- 页面覆盖新建任务、任务列表/详情、Telegram、设置、登录，以及新建页按需打开的历史。
- 统一中文导航、分区、反馈、空/加载/失败状态、键盘焦点及窄屏布局。沿用既有技术栈和旧下载/上传能力。
- 共享 draft/candidate editor 管理文档/字段修订、人工锁、明确清空、差异预览与来源；标准和 custom.user.* 均由同一注册表定义，不硬编码七字段。
- 接入单管理员登录状态的展示与挂载契约；业务鉴权、CSRF、会话和配置请求由 auth/集成负责。
- 首阶段通过注入 fake adapter 验证页面和状态；功能尚未接入时明确不可用，不在生产回退到模拟成功。

不含：OCR 引擎、AI/provider 请求与匹配、Telegram QR/session/下载实现、后台鉴权、持久草稿、Task Core 状态机、归档解析、前端框架替换。

## 验收标准

- [ ] 新建页突出来源、左右证据/字段核对及开始任务；桌面与 360px 窄屏可用，无遮挡/横向溢出，截图及长文本不撑破布局。
- [ ] `/`、`/logs`、`/settings` 旧入口保留；任务页面动作消费后端 available_actions，不在前端增加任务状态规则。
- [ ] 至少用标题、aliases、创作者两角色、日期精度、Count/Volume/Number、标识符、布尔/整数/列表自定义字段验证动态编辑；仅应用内保存项有中文标记，PageCount 不伪装成 AI 推断的已确认值。
- [ ] manual set/clear、候选迟到、旧定义版本、历史采用不会覆盖较新编辑；同批选择原子成功/失败，来源原分类可核对。
- [ ] 历史分开显示提交文档与成功产物 effective_metadata_document；未成功或旧记录缺结果时明确显示暂无，不拼成伪完整结果。采用前预览范围与冲突。
- [ ] 匿名/会话读取失败/已登录/失效/退出分别展示；未认证时无业务内容闪现，返回目标只允许同源站内路径，不由 UI 假装已获授权。
- [ ] 登录、配置、Telegram 模块的稳定挂载点及能力不可用状态可由 fake adapter 验证；自定义字段设置挂载点支持 root 绑定受限定义增改/停用；真实鉴权和 schema 接线后另做集成验收。
- [ ] mount/unmount 幂等，失败导航不卸载当前页；切页中止旧请求，迟到响应不能重绘；取消离开保留当前草稿，确认离开释放敏感内存。
- [ ] 截图/OCR、二维码、密码和 CSRF 不进入 htmx/localStorage/sessionStorage 历史；退出/认证失效撤销敏感 UI，后退恢复先检查会话。
- [ ] 交互可用键盘完成，错误聚焦和读屏反馈明确；对话框 Escape、焦点限制/恢复及 reduced-motion 有验证。
- [ ] fake 测试与实际镜像浏览器测试分开记录；前端源码交付包含 root 统一重建的 dist，旧上传错误/取消/重试和任务页回归通过。

## 依赖与交付

依赖父共享设计、metadata-contracts 的唯一注册表与 canonical MetadataCandidate、source-settings-auth 的 auth/session 接口。Batch 1 先用与契约一致的 fake 做模块验证，再由 root 当批接真实 schema/auth；第二批模块只消费共享 editor 和挂载点。详情见 [design.md](design.md)、[implement.md](implement.md)。
