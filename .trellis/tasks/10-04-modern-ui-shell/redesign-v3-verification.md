# UI v3 验证记录

日期：2026-10-05。分支：`creepy-kiwi`。本轮工作仍在共享工作区，未提交、推送、创建 PR、部署或合并。此前批次的业务实现和测试记录保留；这里记录用户指定技能安装后的前端修订。

## 设计依据与实施

`nextlevelbuilder/ui-ux-pro-max-skill` 固定在上游提交 `477bcb28c9812b385cb51a4605ddf30d7b2266e2`，当前由 Codex 已安装的插件缓存 `.codex/plugins/cache/ui-ux-pro-max-skill/ui-ux-pro-max/2.13.0/.claude/skills/ui-ux-pro-max/` 加载。复核发现它与上游 73 个技能文件逐一一致；格式与数据校验、上游仓库布局下 164 项自带测试通过。`codex debug prompt-input` 能发现 `ui-ux-pro-max:ui-ux-pro-max`；额外用户目录副本会重复加载，已撤回。技能检索的工具型管理界面与轻量信息层级适合此项目；采用其导航、表单、触控、响应式与可访问性规则。没有引入营销首屏、远程字体或新框架。

- 桌面 224px 侧栏现在可完全隐藏至 0px，页头按钮可恢复；隐藏时从键盘与读屏访问中移除。手机仍用抽屉，44px 导航按钮在 180px CSS 视口中可打开、关闭；当前一级入口同时有视觉高亮和 `aria-current="page"`。
- 任务表八列合计 100%，1440px 下「操作」列与成功/失败动作可见。手机只在表格容器内横向滚动，页面本身不横溢，长 ID 和 URL 可折行。
- 设置七个 Tab 在 360/390px 下分两行全部可见，320px 以下继续回流；桌面保持单行。设置区减少重复外框，来源与连接保留单层内容卡；Tab 深链、键盘切换及按区块保存行为不变。
- 新建页按来源→作品信息→采集信息→提交排列 DOM 与焦点。桌面作品信息居左、采集居右，手机按相同顺序纵向显示。简介、标签、题材、别名默认收在「简介与分类」，标题显示已填写/已清空/错误数量；手机有「前往提交」锚点，提交动作保持文档流且不遮挡反馈。Telegraph/压缩包来源切换的手机点按区至少 44px。
- Telegram 消息仍不作为新建页来源；tdl 登录继续位于「设置 → 连接」。

## 实际验证

| 检查 | 结果 |
| --- | --- |
| `npm run test:frontend` | 188/188 通过 |
| `npm run lint` / `npm run build` | 通过；`web/static/dist/` 已重建 |
| `go test ./internal/httpui ./internal/httpapi` | 通过 |
| `git diff --check` | 通过 |
| `task.py validate`（本任务） | implement/check 两份上下文清单各 23 项，验证通过 |
| 隔离 Compose Playwright `npm run e2e:test` | 30/30 通过，临时容器、网络与卷已清理 |

浏览器回归覆盖 1440/768/390/360px，无页面横向溢出；180px CSS 视口下抽屉关闭按钮完全可见并能点击。检查了任务操作、七个设置 Tab、键盘焦点与导航状态、侧栏隐藏恢复、合成上传经 PostgreSQL/worker 生成 CBZ、元数据历史与私密会话失效。测试使用临时管理员与合成作品，不含真实频道、账号或凭据。一次重建遇到 Go 模块代理 `unexpected EOF`，重试后完成最终 30/30；该网络失败没有被记作产品通过。

## 界面预览

以下为最终隔离浏览器生成的合成数据截图：

- [新建任务桌面](research/redesign-v3-ui/workspace-desktop.png) / [手机](research/redesign-v3-ui/workspace-mobile.png) / [侧栏隐藏](research/redesign-v3-ui/workspace-sidebar-collapsed.png)
- [设置桌面](research/redesign-v3-ui/settings-desktop.png) / [手机七个分类](research/redesign-v3-ui/settings-mobile.png) / [手机导航抽屉](research/redesign-v3-ui/settings-mobile-drawer.png) / [连接](research/redesign-v3-ui/settings-connections-desktop.png)
- [任务桌面](research/redesign-v3-ui/tasks-desktop.png) / [手机表格](research/redesign-v3-ui/tasks-mobile.png) / [截图识别](research/redesign-v3-ui/ocr-review-workspace.png)

本轮未进行真实 Telegram 扫码、外部 AI 模型、真实书目授权或阅读器验收；这些不属于前端合成回归的结论。
