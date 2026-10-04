# 界面重设计 v2：2026-10-05

用户明确要求执行：移除新建页 Telegram 消息下载来源，保留 tdl 登录并迁入设置「连接」Tab；采用可隐藏的左侧一级导航、同页细分 Tab，参考热门 GitHub 开源项目实际界面。现有已授权 UI 任务重新进入实施，无需再次请求设计启动许可。

## 行为差距与边界

当前所有设置依次堆叠，证据区同时展开 OCR 和搜索，sidebar 桌面不能收起，页面空白与字段间距使操作跨越很长页面。改善发生在 Go 模板、CSS、导航/Tab生命周期和现有表单展示层。

保留 Go/native JS/htmx/Vite、单管理员保护、API/Task Core及历史语义。移除 Telegram 新建来源的模板和前端分支；保留历史任务、后端下载接口与私有账号数据。/telegram 旧入口跳至 /settings#connections。无数据库迁移、生产操作、自动提交或合并。

## 视觉与信息架构

参考材料记录在 research/redesign-references.md，以公开文档/代码/截图实际证据为准。采用 shadcn 风格的克制表单密度、Immich 的可收起功能导航和 Paperless 的文档处理分组方式；不复制其 React/Angular/Svelte 技术栈或品牌资产。

- 颜色：画布 #f6f7f9、工作面 #ffffff、正文 #18202b、次文 #687484、边界 #e4e7ec、行动蓝 #3564db。sidebar 用 #18202b 与低对比轮廓图标形成清晰导航层。
- 字体：本地系统 sans-serif（中文 PingFang SC / Microsoft YaHei），正文 14px、辅助 12px、页标题 26px，重量与留白承担层级。
- 布局：224px 左导航可折为 64px 图标栏，移动端为可关闭抽屉；主内容顶部工具栏与页面标题，内容最大约 1360px。内容左对齐，表单字段并排仅在宽度允许时。
- 主页：来源紧凑切换仅 Telegraph/上传；证据区用「截图识别 / 书目搜索」Tab，作品信息在右侧，辅助字段按组折叠；保留同一个草稿/模块实例，切换不丢截图或候选。
- 设置：上方 Tab「下载 / 书目来源 / AI 模型 / 字段 / 识别规则 / 连接 / 安全」；选中一项显示该内容；区块标题、简短说明和设置行，不再所有模块纵向展开。
- 页面通用：按钮、输入、导航图标、状态、空白、任务表格统一。无装饰性 KPI、渐变营销块或远程字体。

## 实现所有权

- root：base/index/logs/login 模板、公共样式、主页输入/证据Tab接线、旧 Telegram 路由、共享构建/集成验证与任务记录。
- settings worker：settings 模板、settings入口及子模块按需挂载/暂停，连接内 Telegram 生命周期，相关单元测试；不改公共CSS/dist。
- navigation worker：shared/app_lifecycle 的navigation disclosure及独立controller、app/page_modules 中 Telegram独立页注册移除、相关测试；不改模板/CSS。
- researcher：仅参考资料；复核者只读跨层审查。

## 生命周期与验收

Tab 必须符合 role/aria-controls/aria-selected、键盘方向/Home/End、hidden语义。普通设置/作品草稿在同页切换保留，离页统一abort/dispose；连接Tab离开清二维码、2FA和EventSource；旧异步返回不能复活隐藏/卸载模块。仅sidebar折叠这种非敏感偏好可存浏览器，草稿/凭据不可持久化。

验证 npm Node全套/lint/build，相关Go模板路由tests；隔离Compose生产页面验证360/390/768/1440宽度、导航收起展开、设置深链/切换/重载、Tab草稿保留、连接销毁、旧上传/历史/候选流程。截图实际审查后再记录验收；不以代码完成代替视觉完成。
