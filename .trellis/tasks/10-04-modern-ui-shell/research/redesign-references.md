# 工作台改版：公开项目设计参考

核验时间：**2026-10-05 01:11–01:18（Asia/Shanghai）**。Stars 查询时间为 01:11:39–41（UTC 2026-10-04 17:11:39–41）。

本轮只做外部研究。实时读取 GitHub 公共 API、固定提交的官方源码与官方仓库截图，下载并实际查看了下面四张图片；没有登录或操作这些项目的在线 demo。截图可以早于本次代码版本，不能用截图版本代替当前源码行为。Star 数用于确认项目热度，不代表布局适配性或质量评分。

**建议组合：Paperless-ngx 的设置 Tabs 与工作区层级 + shadcn 的可收缩侧栏 + Immich 的 Tab 内分组。** 保留本项目 Go templates / htmx / 原生 JavaScript / Vite 技术栈，参考交互结构，不引入 React、Angular 或 Svelte。

## 1. 实时项目与版本证据

| 项目 | 本次 Stars | 默认分支 | 核验源码提交 |
| --- | ---: | --- | --- |
| [immich-app/immich](https://github.com/immich-app/immich) | 115,572 | main | [`69f06a29`](https://github.com/immich-app/immich/commit/69f06a29ca67613355348172480054ebd7e09f6c) |
| [paperless-ngx/paperless-ngx](https://github.com/paperless-ngx/paperless-ngx) | 46,274 | dev | [`8adbff14`](https://github.com/paperless-ngx/paperless-ngx/commit/8adbff1423af58575bc5a08eee7a6d833fd95651) |
| [shadcn-ui/ui](https://github.com/shadcn-ui/ui) | 125,084 | main | [`295a1f11`](https://github.com/shadcn-ui/ui/commit/295a1f114a138f23b5dfee0e0c6812394dfeb90c) |

星数直接来自 GitHub 仓库 API 的 `stargazers_count`：
[Immich API](https://api.github.com/repos/immich-app/immich)、
[Paperless API](https://api.github.com/repos/paperless-ngx/paperless-ngx)、
[shadcn API](https://api.github.com/repos/shadcn-ui/ui)。
上述 SHA 另经 `/repos/{owner}/{repo}/commits/{sha}` 回读确认为提交；下文链接均固定到这些 SHA。Paperless 的参考是 dev 分支快照，不能表述成某个正式发行版的全部现行行为。

## 2. 已查看的官方界面与源码

### A. Paperless-ngx：最直接对应“左侧一级功能 + 设置 Tabs”

实际查看：

- [完整侧栏 + 文档表格](./reference-images/paperless-official-table.png)：左侧包含 Dashboard、Documents，以及 Saved views / Manage / Administration 分组；右侧顶部是当前页面标题和视图操作，其下是筛选区，再下面才是内容。Settings 位于管理区域，不把全部设置项铺到一级侧栏。
- [收缩后的图标侧栏](./reference-images/paperless-official-slimsidebar.png)：标签收起，导航图标与选中背景保留，主区得到更多横向空间；侧栏边缘仍有展开按钮。
- 完整侧栏图片角落写着 **Paperless-ngx v3.1.1**。这是图片展示版本，图片没有证明本次 dev 提交运行的版本。

对应源码证据：

| 证据 | 实际代码表达的行为 |
| --- | --- |
| [app-frame.component.html L73–105](https://github.com/paperless-ngx/paperless-ngx/blob/8adbff1423af58575bc5a08eee7a6d833fd95651/src-ui/src/app/components/app-frame/app-frame.component.html#L73-L105) | 同一个 sidebar 在 `slim` / `expanded` 间切换；显式 Expand/Collapse 按钮；图标导航配文字提示与 active 路由状态 |
| [app-frame.component.html L418–421](https://github.com/paperless-ngx/paperless-ngx/blob/8adbff1423af58575bc5a08eee7a6d833fd95651/src-ui/src/app/components/app-frame/app-frame.component.html#L418-L421) | 主区宽度同步采用 `col-slim` / `col-sidebar-expanded`，收缩导航会让出空间 |
| [settings.component.html L38–47](https://github.com/paperless-ngx/paperless-ngx/blob/8adbff1423af58575bc5a08eee7a6d833fd95651/src-ui/src/app/components/admin/settings/settings.component.html#L38-L47) | 设置容器是同一 form，顶部 `ngbNav` / `nav-tabs`，General Tab 内还有 Appearance 小标题 |
| [settings.component.html L210–439](https://github.com/paperless-ngx/paperless-ngx/blob/8adbff1423af58575bc5a08eee7a6d833fd95651/src-ui/src/app/components/admin/settings/settings.component.html#L210-L439) | 其余 Tabs 为 Documents、Permissions、Notifications；`ngbNavOutlet` 提供当前 Tab 内容区域 |
| [settings.component.ts L312–327](https://github.com/paperless-ngx/paperless-ngx/blob/8adbff1423af58575bc5a08eee7a6d833fd95651/src-ui/src/app/components/admin/settings/settings.component.ts#L312-L327)、[L425–439](https://github.com/paperless-ngx/paperless-ngx/blob/8adbff1423af58575bc5a08eee7a6d833fd95651/src-ui/src/app/components/admin/settings/settings.component.ts#L425-L439) | Tab 与 settings 路由的 section 对应；切换处理 dirty 状态和导航未成功时的 active Tab 恢复 |

可借鉴：导航、页内分类、表单分区是不同层级；当前页面有明确标题，收缩导航不会丢失当前所在位置。

边界：未获得并查看 Paperless 设置页的官方截图，**设置 Tabs 的结论来自当前固定源码**；不声称已操作过其设置页面。

### B. shadcn/ui：侧栏收缩、组内展开和页头位置的明确范例

实际查看 [官方 sidebar-07 图片](./reference-images/shadcn-official-sidebar-07.png)：侧栏有 Platform / Projects 分组，一级项 Playground 展开了 History / Starred / Settings；账号区域在底部；主区页头的侧栏开关紧挨 breadcrumb。右侧大块浅灰区域是**示例占位内容**，不是可照搬的业务工作区。

| 证据 | 实际代码表达的行为 |
| --- | --- |
| [app-sidebar.tsx L159–173](https://github.com/shadcn-ui/ui/blob/295a1f114a138f23b5dfee0e0c6812394dfeb90c/apps/v4/registry/new-york-v4/blocks/sidebar-07/components/app-sidebar.tsx#L159-L173) | `Sidebar collapsible="icon"`；Header / Content / Footer / Rail 各有位置 |
| [nav-main.tsx L36–68](https://github.com/shadcn-ui/ui/blob/295a1f114a138f23b5dfee0e0c6812394dfeb90c/apps/v4/registry/new-york-v4/blocks/sidebar-07/components/nav-main.tsx#L36-L68) | 分组内使用 Collapsible；当前组默认展开；菜单按钮有 tooltip；子项和父项层级分开 |
| [page.tsx L19–43](https://github.com/shadcn-ui/ui/blob/295a1f114a138f23b5dfee0e0c6812394dfeb90c/apps/v4/registry/new-york-v4/blocks/sidebar-07/page.tsx#L19-L43) | `SidebarTrigger` 位于主区页头，并与 Breadcrumb 对齐 |
| [sidebar.tsx L183–204](https://github.com/shadcn-ui/ui/blob/295a1f114a138f23b5dfee0e0c6812394dfeb90c/apps/v4/registry/new-york-v4/ui/sidebar.tsx#L183-L204)、[L529–545](https://github.com/shadcn-ui/ui/blob/295a1f114a138f23b5dfee0e0c6812394dfeb90c/apps/v4/registry/new-york-v4/ui/sidebar.tsx#L529-L545) | 手机改为独立 Sheet；桌面 collapsed 状态显示 tooltip，手机不依赖 hover 提示 |
| [Tabs 官方文档源码 L60–99](https://github.com/shadcn-ui/ui/blob/295a1f114a138f23b5dfee0e0c6812394dfeb90c/apps/v4/content/docs/components/radix/tabs.mdx#L60-L99) | TabsList / TabsTrigger / TabsContent 对应分类和面板；有 Account/Password 示例，以及 line/vertical 变体 |

可借鉴：**整栏收缩**和**功能组展开**是两个独立动作；不要把“折叠左侧导航”只实现成手机菜单的开关。页头开关一直可发现，不迫使用户寻找侧栏底部。

边界：shadcn 是组件/布局范例，不是已经包含本项目下载、OCR、设置流程的完整应用；本项目只实现等价交互，无需安装其 React 组件。

### C. Immich：主工作区清晰，复杂设置逐步展开

实际查看 [官方浅色界面图片](./reference-images/immich-official-light.webp)：左侧一级功能用图标和短文本，Photos 有明显选中背景；Library 是组标题；照片内容占据主空间；容量和服务状态放在导航底部。图片含移动端拼图，不是本次实际调整浏览器宽度的测试。图片左下角显示 **v1.112.1**，明显是历史官方截图。

| 证据 | 实际代码表达的行为 |
| --- | --- |
| [UserSidebar.svelte L42–87](https://github.com/immich-app/immich/blob/69f06a29ca67613355348172480054ebd7e09f6c/web/src/lib/components/shared-components/side-bar/UserSidebar.svelte#L42-L87) | Photos / Explore / Map 等入口与 Library 分组；Albums 可展开 RecentAlbums 子项 |
| [Sidebar.svelte L16–46](https://github.com/immich-app/immich/blob/69f06a29ca67613355348172480054ebd7e09f6c/web/src/lib/components/sidebar/Sidebar.svelte#L16-L46) | 窄屏侧栏有 hidden/expanded 状态、`inert`、Escape/外部点击关闭、focusTrap 和焦点恢复 |
| [system-settings/+page.svelte L59–199](https://github.com/immich-app/immich/blob/69f06a29ca67613355348172480054ebd7e09f6c/web/src/routes/admin/system-settings/+page.svelte#L59-L199) | 设置定义为 component/title/subtitle/key/icon 组成的分类清单：认证、备份、图片、任务、机器学习、元数据等 |
| [system-settings/+page.svelte L201–229](https://github.com/immich-app/immich/blob/69f06a29ca67613355348172480054ebd7e09f6c/web/src/routes/admin/system-settings/+page.svelte#L201-L229) | 搜索匹配标题与副标题；结果逐项渲染 SettingAccordion |
| [SettingAccordion.svelte L62–84](https://github.com/immich-app/immich/blob/69f06a29ca67613355348172480054ebd7e09f6c/web/src/lib/components/shared-components/settings/SettingAccordion.svelte#L62-L84) | 可点击分组标题，`aria-expanded`，标题下有一句说明 |

可借鉴：设置项先按用户任务命名，再在分组内展示详细控件；主工作区不被大量辅助模块挤占。移动侧栏的键盘、焦点和隐藏状态应作为完整行为处理。

边界：**Immich 系统设置本次源码是搜索 + Accordion，不是顶部 Tabs**；它为本项目提供 Tab 内高级分区参考。也不能凭这份侧栏源码声称已验证其桌面 icon rail，桌面收缩参考应引用上面两项。

## 3. 本项目可直接采用的五条模式

最终采用说明：研究提出五分类草案，实际实现采用七个独立 Tab（下载、书目来源、AI 模型、字段、识别规则、连接、安全），避免规则与 AI 配置继续堆叠。Telegram 不再出现在主导航，登录管理进入“连接”。下方图示为研究阶段草图，以 redesign-v2.md 为实施约定。

以下是结合用户要求和当前项目功能提出的设计建议，不是对参考项目界面的逐字描述。

| 模式 | 本项目建议 | 实施验收点 |
| --- | --- | --- |
| **1. 全局功能留在可收缩左栏** | 一级入口为“新建任务 / 任务 / 设置”；依据用户本轮补充，Telegram 登录移入设置的“连接”Tab。桌面默认图标 + 中文名称；整栏可收成图标轨道，建议宽度约 224–240px → 56–64px；设置与账号区域放在下方。用 Paperless/shadcn 的收缩行为，独立于组内展开。 | 收缩后主区确实变宽；选中项、可访问名称和焦点仍在；开关在主区页头可见；390px 窄屏改抽屉并可 Escape 关闭。数字是本项目建议，不是引用项目尺寸。 |
| **2. 设置采用五个顶部 Tabs** | “下载行为 / 书目来源 / 识别与 AI / 元数据字段 / 账号与安全”。所有类别留在同一个 `/settings` 页面，顶部 Tab 只切换对应面板。用 Paperless 的 settings 分类结构，不把全部表单垂直堆叠。 | 首屏只出现当前类别；Tab 有清晰 active 状态；键盘可切换且读屏能对应 tab/tabpanel；窄屏 Tab 条可滚动，页面本身不横向溢出。 |
| **3. Tab 内细分，常用项先出现** | “识别与 AI”内分“本地识别 / 提取规则 / AI 服务”；“书目来源”按来源分别展示启用、凭据状态、优先级及保存/测试；不常用参数放“高级选项”折叠区。借鉴 Immich 的标题 + 简短说明 + 按需展开。 | 当前使用项不用先展开高级区；折叠标题能说明内容；每个保存/测试反馈贴近对应区块；Tab 间不重复放同一设置控件。 |
| **4. 新建页明确来源、辅助证据和确认三个层级** | 页面上部用 Telegraph / 本地归档切换来源（用户已移除 Telegram 消息来源）；中部是证据工作区与元数据确认；证据区内再用“截图提取 / 书目检索”切换辅助功能。常用元数据直接显示，创作者/出版/标识符等用局部分类或高级分区。参考全局导航与页内任务分开的层级，避免同页所有模块同时争首屏。 | 切换来源不偷偷提交或清空已确认字段；用户不用滚过大量无关表单才能找到“创建任务”；不要给真正并行的区域套虚假的步骤编号。 |
| **5. 切换位置有记忆，草稿和敏感数据有明确生命周期** | 用 URL 中的非敏感 Tab key 或现有页面状态表达当前位置；页头显示“设置 / 书目来源”等当前位置。Tab 切换保留该页未提交编辑；真正离页才走已有 dirty guard。只允许保存导航偏好，密码、二维码、OCR 与元数据草稿继续遵守现有不持久化约束。 | 返回设置能定位到类别；未知 Tab key 回到默认；迟到请求不能写到新面板；键盘 focus 不留在隐藏面板；重新登录后不恢复敏感内容。Paperless 证明了 Tab/路由和 dirty 检查需要联动；具体保留策略沿用本项目契约。 |

建议设置页结构：

```text
左侧全局导航             [收缩导航] 设置
新建任务                 下载行为 | 书目来源 | 识别与 AI | 元数据字段 | 账号与安全
任务                     ─────────────────────────────────────────────────────
Telegram                 书目来源
                         MangaBaka       已启用 / 匿名访问
                         优先级           [保存来源设置] [测试已保存的来源]
                         [高级选项]
                         ─────────────────────────────────────────────────────
                         MangaUpdates    已启用 / 匿名访问
                         …
设置                     ─────────────────────────────────────────────────────
管理员 / 退出            Bangumi          未配置凭据 / 可匿名访问
```

视觉上沿用本项目中文蓝白方向：活跃导航和主要动作使用单一蓝色强调，靠留白、短标题和分隔线表达层级。参考截图的图片墙、占位卡片、绿色品牌色、推广/购买入口、团队切换器均没有必要复制到本项目。

## 4. 官方图片出处与本地文件

四张文件保持下载时的原始字节，仅供研究比对，不作为本产品的图像素材。

| 本地文件 | 像素 / 字节 | 官方固定版本原图 |
| --- | --- | --- |
| [immich-official-light.webp](./reference-images/immich-official-light.webp) | 2420×1560 / 256,120 | [raw](https://raw.githubusercontent.com/immich-app/immich/69f06a29ca67613355348172480054ebd7e09f6c/docs/static/img/screenshot-light.webp) |
| [paperless-official-table.png](./reference-images/paperless-official-table.png) | 3824×2786 / 995,838 | [raw](https://raw.githubusercontent.com/paperless-ngx/paperless-ngx/8adbff1423af58575bc5a08eee7a6d833fd95651/docs/assets/screenshots/documents-table.png) |
| [paperless-official-slimsidebar.png](./reference-images/paperless-official-slimsidebar.png) | 3824×2786 / 1,526,392 | [raw](https://raw.githubusercontent.com/paperless-ngx/paperless-ngx/8adbff1423af58575bc5a08eee7a6d833fd95651/docs/assets/screenshots/documents-smallcards-slimsidebar.png) |
| [shadcn-official-sidebar-07.png](./reference-images/shadcn-official-sidebar-07.png) | 2880×1800 / 105,028 | [raw](https://raw.githubusercontent.com/shadcn-ui/ui/295a1f114a138f23b5dfee0e0c6812394dfeb90c/apps/v4/public/r/styles/new-york/sidebar-07-light.png) |

校验 SHA256：

```text
immich-official-light.webp             a2a36dee23ddb2d4d4b2830848d5e847ae717623137b00746da1e165ec06399f
paperless-official-table.png           99f0dfcadfc5efb605673623d165c9f6546522c449e92af23cf305bc43e2c920
paperless-official-slimsidebar.png     13f08795f22816223942fa0f704e40aa9675593f4d201bb9d869668284fcd728
shadcn-official-sidebar-07.png          5f6688b5c0f9439b108adc8955ff67c725ab597581494b83cadb43e915a30813
```

## 5. 验证范围

- 已完成：三仓库公共 API 星数/分支/提交核验；固定 SHA 源码读取；四张官方图片下载、逐张查看、像素/字节与 SHA256 检查。
- 未进行：在线 demo 登录、真实交互点击、在参考应用内保存设置、参考应用的响应式浏览器测试。
- 本轮没有修改产品代码，也没有运行产品测试。后续实现必须用本项目实际浏览器检查桌面收缩、手机抽屉、Tab 键盘行为、未保存表单和 htmx 挂载/卸载，不能把这份外部研究算作本产品验收。
