# UI v2 验证记录

日期：2026-10-05。分支：`creepy-kiwi`。当前工作区实施，未提交、推送、合并或部署；先前批次及运行修复保留。第一、二批历史验收分别保留在父任务原报告中，本记录只说明本轮界面改版。

## 实际改动

- 一级入口为「新建任务 / 任务 / 设置」。224px 石墨色侧栏可收为64px图标栏，偏好跨 htmx 导航保留；手机改为带背景隔离、焦点限制与 Escape 关闭的抽屉。
- 设置拆为下载、书目来源、AI 模型、字段、识别规则、连接、安全七个 Tab，支持 URL hash 深链与键盘左右/Home/End。按需挂载，普通输入切换后保留；保存反馈属于相应区块。
- 新建页仅保留 Telegraph 链接和压缩包上传。截图识别与书目搜索分 Tab，保留同一草稿和证据；元数据按字段分组，标签/间距/按钮统一。
- tdl 登录移入「设置 → 连接」。离开此 Tab 清除 QR、密码和事件连接，返回重新读账号。`/telegram` 普通请求303跳转，htmx请求200并带 `HX-Redirect`。后端Telegram接口、历史任务及私密会话保留。
- 参考 [Paperless-ngx、shadcn/ui、Immich 的公开源码与官方截图](research/redesign-references.md)。没有引入其框架或品牌素材；本项目保持 Go templates、htmx、原生 JavaScript、Vite。

## 复核发现及修复

1. 首次设置加载失败后，返回 Tab 不重试。独立评审动态复现，修复为仅补充失败/未完成加载，成功表单保持实例与编辑；聚焦回归覆盖 HTTP、网络、无效响应、进行中读取及编辑保护。凭据无需替换时隐藏整行空控件。
2. 元数据别名输入失焦时，焦点提示的 `display` 切换令 summary 在鼠标按下/松开之间上移约22.58px，单击自定义字段不能展开。真实 Chromium 最小复现确认；非空短提示改为稳定显示，真实点击回归不得以强制 `open` 替代。
3. 手机截图截到抽屉退场中间帧。真实 CSS/导航复核确认终态为 `x=-260/right=0`、`visibility:hidden`、`inert:true`；截图改为等待实际可见/隐藏和 transform 终态。
4. 任务列表旧样式只有 `status-success` 等 legacy 名称，而 Task Core 使用 `status-succeeded`，白色字落在透明背景上。补齐当前七状态对应样式及未知状态灰色底，并在真实任务浏览器截图检查状态徽标背景。
5. `/telegram` 使用共享 `isHTMXRequest` 识别大小写/空白，回归测试先失败后通过。

6. 任务页截图出现上一页的导航高亮。修复措施是在会话刷新成功后按当时 URL 同步导航，回归覆盖等待刷新期间地址改变，并用真实跳转检查唯一 active 链接。htmx 1.9.10 源码显示先更新 URL 再触发 `afterSwap`，不能把事件先后顺序当作已证实根因。

## 检查结果

| 检查 | 结果 |
| --- | --- |
| `npm run test:frontend` | 186/186 通过 |
| `npm run lint` | 通过 |
| `npm run build` | 通过，`web/static/dist/` 已重建 |
| `go test -p 1 ./... -count=1` | 通过；本轮未设置数据库专项环境变量 |
| `go test ./internal/httpui -count=1` | 最终路由修复后通过 |
| 独立复核 | v2 全范围审查 + 设置失败恢复定点复核通过；原发现已修复 |
| `git diff --check` / Trellis context validate | 通过 |
| 隔离 Compose Playwright | 首轮24/25通过；修复后完整26/26通过（35.8秒） |

状态徽标修复后的两项定点视觉复验已通过（5.6秒）；导航高亮修复后的最终复验进行中。

## 界面预览

以下均为隔离环境、合成作品与配置截图，不含真实频道、账号、密钥或 OCR 私密原文。

- [新建任务（桌面）](research/redesign-v2-ui/workspace-desktop.png)
- [侧栏收起](research/redesign-v2-ui/workspace-sidebar-collapsed.png)
- [新建任务（手机）](research/redesign-v2-ui/workspace-mobile.png)
- [下载设置](research/redesign-v2-ui/settings-desktop.png)
- [AI 模型设置](research/redesign-v2-ui/settings-ai-desktop.png)
- [书目来源（桌面）](research/redesign-v2-ui/settings-sources-desktop.png)
- [书目来源（手机）](research/redesign-v2-ui/settings-mobile.png)
- [tdl 连接](research/redesign-v2-ui/settings-connections-desktop.png)
- [手机导航抽屉](research/redesign-v2-ui/settings-mobile-drawer.png)
- [截图识别与候选核对](research/redesign-v2-ui/ocr-review-workspace.png)
- [任务列表](research/redesign-v2-ui/tasks-desktop.png)

实际审查了桌面首页、下载/AI/来源/连接设置、任务列表，390px 手机首页与来源/下载设置，以及收起侧栏与抽屉。浏览器断言另外覆盖360px和768px页面无横向溢出、reduced-motion、键盘焦点、hash深链和敏感数据清理。任务表格在窄空间允许自身横向滚动，不要求把每一列挤到屏幕中。

本轮未使用真实账号扫码、外部 AI 模型或真实阅读器；这些仍属于第三批外部验收。全 Go 单测本轮未设置 `TEST_DATABASE_URL`，不能据此宣称新一轮数据库专项测试通过。隔离 E2E 使用临时 PostgreSQL、合成管理员和测试归档，生产环境未改动。
