# 跨模块验收记录（2026-10-05）

分支：`creepy-kiwi`。本轮使用隔离 PostgreSQL、隔离 Komga 1.28.1、合成 CBZ 和合成截图；未访问生产 Komga 书库或真实漫画文件，未部署、push 或合并。以下是可复核的本地证据，任务状态仍为 `planning`，不能据此宣布父计划完成。

## 已执行的检查

| 路径 | 结果与可观察断言 |
| --- | --- |
| `bash scripts/verify_release_gates.sh`，指向隔离 PostgreSQL | 完整通过：Go 全套与 race、`go vet ./...`、OCR 资源 15/15、前端 Node 214/214、ESLint、Vite 构建及镜像浏览器 E2E 30/30。发布检查使用临时 Git index 核对全部待加入的 `dist` 与源码；真实 Git index 未暂存。 |
| `npm run test:cli`、`npm run lint:cli`、`npm run build:cli` | CLI 66/66，lint 与 build 通过。`npm pack` 后离线清洁安装的 help/version 与本地 OCR 资源通过。结构化 URL 缺省 `force:false` 的回归用例先失败、修复后通过。 |
| 隔离 Komga 1.28.1 + `mediactl` | 合成 CBZ 执行 `books edit → metadata preview → update → edits status → restore`；文件写回及 Komga 当前值回读一致；恢复后完整 CBZ SHA-256 与原件一致。此前服务端还验证了标题写回、简介清空和 PageCount 显式修正；ComicInfo 经固定 XSD 校验。 |
| 本机 Tesseract + 隔离服务 | 两张合成截图本地识别并合并为内存草稿；从同一草稿用结构化输入提交测试任务，服务端回读标题快照一致，随后取消。没有读取用户剪贴板、启动真实下载或访问 Telegram。 |
| 页面与差异检查 | 桌面、手机截图检查并修正作品库搜索样式；`git diff --check` 通过。保留 Trellis journal 的既有 `merge=union` 属性。 |

## 需求验收边界

| PRD | 当前证据 | 尚缺的实际验收 |
| --- | --- | --- |
| I1、I2、I8、I9 | 隔离 PG、受保护 API、任务快照、产物与保留/恢复路径有自动化覆盖 | 生产升级与真实备份恢复另行验收 |
| I3 | 浏览器多图 OCR E2E 与本机两张合成截图通过 | 用户真实 Mac 剪贴板、低清/复杂截图与人工校对 |
| I4 | 模型发现、配置和协议测试通过 | 应用容器到选定 MiniCPM5-2B Q4 的受保护通道、扩展字段实际输出 |
| I5 | 三来源公开中性适配器及配置路径通过 | 带授权来源、R18/男同/冷门作品的收录与权限边界 |
| I6 | 登录/下载状态机和错误路径自动化通过 | 当前网页到 tdl 的实际扫码授权、只读附件下载、断线和 nginx 长连接 |
| I7 | 合成归档的 XML/XSD、图片和顺序校验通过；隔离 Komga 1.28.1 验证存量 CBZ 写回/恢复 | 新产物在 Komga/Kavita 的双阅读器导入与真实阅读页核对 |
| I10 | 桌面/手机浏览器 30 项 E2E、键盘/导航及打包检查通过 | 真实移动设备性能仍未测 |
| I11 | 隔离迁移和测试环境启动通过 | 新密钥与数据的完整部署备份/恢复演练 |

CLI 子任务还缺真实剪贴板、用户独立终端 AI 外发核对、真实 tdl 授权和全新 AI 会话 Skill 自动发现；Komga 子任务还缺 Kavita 与更多 ZIP 边界的现场验证。两任务均继续保持 `in_progress`。旧 Telegram 消息元数据预览已被用户改为截图 OCR，不把旧计划当成交付。

本轮隔离 API 进程、Komga/PostgreSQL 测试容器和临时目录在取证后已清理。详见 [第二批记录](../../10-04-clipboard-ocr-tdl-ui/batch-2-verification.md)、[Komga 子任务](../../10-05-komga-metadata-management/implement.md)和 [CLI 子任务](../../10-05-cli-ai-skill/implement.md)。
