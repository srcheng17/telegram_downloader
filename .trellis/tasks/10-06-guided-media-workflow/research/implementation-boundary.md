# Implementation boundary — 2026-10-07

用户已在最终规划后明确回复“确认实施”。目标差异是：并列工具面板与重复候选采用，改为单工作区自动准备、集中核对、一次确认后创建任务及展示交付结果。现有模型/Telegram认证修复已在私有目录保存基线，所有实施继续保留这些改动。

## Ownership

- 向导：`frontend/src/home/`、`web/templates/index.html`、`web/static/style.css`及对应测试，负责步骤/返回/提交快照/任务进度。
- 识别：`frontend/src/ocr/`、`frontend/src/metadata-search/`、`cli/workspace_state.mjs`及对应测试，负责有限caption与CJK文本处理、自动准备API、来源默认，保持证据映射。
- 提交/交付：Task Core应用/仓储、HTTP task handlers、Komga客户端及对应测试/迁移，负责稳定创建幂等与真实收录结果。
- 主代理：共享metadata编辑/候选协调、AI提取失败定位、跨层连接、规范同步、构建、整体测试与独立review。

具体文件可按同一责任内现有代码模式细化；跨责任编辑先协调。测试先表达用户可见行为，避免无关重构。最终确认前禁止upload init/下载创建/库写入；自动准备只读来源与模型提取。

实现验收阶段不恢复Telegram下载UI、不改网络/模型部署、不处理真实作品、不push/merge或发布生产；后续提交与PR审阅独立进行。原始截图仅作私有验证，新增回归使用合成图。前端bundle由主代理统一重建，避免并行构建覆盖。
