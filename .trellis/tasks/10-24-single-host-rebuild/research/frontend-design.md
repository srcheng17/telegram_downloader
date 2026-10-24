# 墨渡 PanelPort：前端设计与实现边界

## 设计依据

使用本机已验证的 Anthropic `frontend-design`，提交 `41bbe19d1a1a7eaab5e7bb9050a417e5c6cffc8f`。这是个人漫画归档与 Komga 入库工作台：收集图页、补充书目、生成 CBZ 后自动送入书库。

## 视觉设计与自审

采用书页白 `#fcfcf8`、背景蓝灰 `#edf1f4`、浓墨蓝 `#172b46`、正文灰 `#526174`、钴蓝 `#2455db` 和规则线 `#d4dde5`。标题使用本机中文宋体/衬线栈，正文使用系统无衬线栈，无外部字体或 CDN。品牌书脊标记、克制的页角与细分隔线回应漫画工作台；正文和表单保持清楚、安静。桌面左侧导航，移动端顶部导航。

首页是完整的单一编辑表单；任务和书库设置各有独立页面，没有营销 hero、统计卡片或虚构成功案例。

```text
新建归档 /       来源 → 书目信息 → 生成并入库 → 查看任务回执
任务记录 /tasks  处理进度 / 入库结果 / 失败重试 / 取消
书库设置 /settings  Komga 网页地址 / 共享书库目录
```

## 页面与 API 边界

- 首页只接受 URL 或 ZIP/RAR/7Z 和七项可选书目信息。POST `/api/jobs/url` 或 `/api/jobs/upload` 后仅显示提交回执及 `/tasks` 链接，不创建任务列表 DOM，不请求 `/api/jobs`，不自动跳页。
- 任务页独占记录及两秒轮询，隐藏时暂停、请求不重叠、保留已有行节点和焦点。动作只消费后端 `available_actions` 中的 `cancel` 与 `retry`。最终成功文案为后端提供的“已入库”；`delivering` 显示入库中，客户端不自行推导可取消资格。
- 设置只保存 `komga_url` 和 `komga_dir`，成功后按服务返回值回填。网页地址可选且仅用于打开 Komga；实际文件投递使用本应用可写、与 Komga 映射到同一位置的共享绝对目录。页面明确网址本身不能接收 CBZ，没有凭据、HTTP 上传或扫描 API 功能。
- 未配置共享目录时，首页禁用提交并给出书库设置入口。任务页只在存在配置网址时提供一个页级“打开 Komga”链接。
- 用户文字和错误用 Jinja 自动转义或 `textContent`。无浏览器文件下载、手动入库或旧默认动作设置。

## 可访问性与验证

原生 radio 切换来源，所有输入有 label、maxlength 与必要提示，提交和操作有真实等待反馈。文件选择使用中文 label 触发原生控件，键盘 Tab/Space 可操作，并保留可见焦点；尊重 reduced-motion。

跳过链接采用 1×1 `clip-path` 隐藏，只在 `:focus-visible` 展开，避免 Chromium 在滚动后全页截图误绘负 transform 的 fixed 图层。已实际验证 Tab 首项显示、Enter 将焦点移至 main。

七项模板自检覆盖文字安全、动作资格、首页功能隔离、无下载/手动入库、Komga 配置门槛、可选元数据与空页。Ruff 和 JS 语法检查通过。一次独立浏览器路由契约检查确认：首页提交前后 `/api/jobs` 请求均为零、仅任务页轮询、设置字段和读回正确、移动端无横向溢出，控制台无错误；它是 UI 契约检查，不替代主代理的真实 API 浏览器回归。

最新截图：`test-results/panelport-desktop-home.png`、`panelport-mobile-home.png`、`panelport-settings.png`、`panelport-tasks.png`。实现仅修改 templates/static、UI 自检与本记录；后端、迁移、部署和真实媒体链路由其他代理负责。
