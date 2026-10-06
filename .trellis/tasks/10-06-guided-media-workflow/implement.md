# Implementation plan

状态：review；用户已明确确认实施，代码及验收已完成，进入提交与PR审阅阶段，未部署生产。

## Before start

- [x] 用户选择自动准备后一次“确认并开始”；PRD/design与验收同步，产品决策已收敛。
- [x] 用户于2026-10-07明确回复“确认实施”。
- [x] 核对最新工作树、相关任务、配置版本；保留其他任务未提交修改。
- [x] context路径验证有效；批准后已执行`task.py start`。
- [x] 读取module-boundaries及层级细则。本图实验不等于改进功能验收通过。

## Ordered work

1. 建立当前无冲突/冲突路径的主动决策与点击基准；生成脱敏聊天图/字段基准，原图不进Git。
2. 单workspace向导：前进/返回、generation、焦点、结果复用；不改后端Task Core状态。
3. 识别：按证据处理CJK空白、caption、摘要边界和原文映射；定位六字段AI unavailable并验证预算/语义，不放松grounding。
4. 集中核对：去重建议、常用字段/已启用源默认、关键词预填、高级设置折叠，保留人工锁/preflight；移除重复逐候选采用操作，同步旧交互契约。
5. 提交结果：最终确认前禁止创建任务/upload init/库写入；一次确认绑定当前草稿、来源与目标，版本变化回核对。补稳定创建幂等键与受保护结果回查，覆盖上传init丢响应；当前任务进度、阶段重试；先追踪Komga scan/readback契约再最小补充收录编排。
6. 浏览器验收：窄屏/键盘/后退/迟到请求；合成归档Task→ComicInfo→Komga可见并清理测试资源。

## Risky boundaries

- `frontend/src/home/`、`ui_shell/`、`web/templates/index.html`：步骤切换不得dispose工作区。
- `frontend/src/ocr/`、共享`metadata/`、`cli/`：原文映射、中文空白、字段/设置版本、人工保护；Go/JS规则契约一致。
- `metadata-search/`、`internal/app/metadataextract/`：角色语义、输出预算、来源身份；不降低完整响应/证据验证。
- `internal/app/tasks/`、HTTP任务动作、Komga客户端：允许目标、幂等复制/扫描；不造平行Task Core状态。
- `web/static/dist/`：源码变化随同重建。生产配置不得作为代码回滚替代。

## Validation commands (planned)

- 新行为聚焦测试后按层运行`npm run test:frontend`、`npm run lint`、`npm run build`。
- CLI/shared受影响：`npm run test:cli`、`npm run lint:cli`、`npm run build:cli`。
- 后端受影响：`go test ./... -count=1`；任务/worker/持久化受影响再`go test -race ./... -count=1`。
- 按`docs/development/testing-strategy.md`配置浏览器测试；`scripts/check_ocr_browser.mjs`补聊天fixture，不能只重复白底大字。
- 记录决策/点击、字段错误、噪声、时延；合成作品验证图像不变、ComicInfo及Komga读回一致。
- 验证自动准备完成只进入核对页、确认前无任务/库写入、返回修改后提交新快照、确认后版本改变须重新核对；无冲突路径只有一次最终提交确认。
- `git diff --check`和`task.py validate`。路径有效不代表产品决策已批准。

## Rollback and review

按向导/识别/候选/交付拆分审阅，保留旧API兼容。测试不批改真实作品；push/PR后报告分支、链接和实际验证，等待用户审阅；只有明确指令才合入main。

## Implementation outcome

- [x] 单内存向导、集中核对、一次最终确认、当前任务与交付结果。
- [x] 六字段默认、保守书目身份匹配、规则/AI/本地caption候选汇总与最终authority校验。
- [x] CJK/跨行/角色/UI噪声/摘要边界及原文映射，浏览器暗图增强与低清保护。
- [x] 稳定提交回执与SHA校验、丢响应恢复、同元数据URL复用及冲突保护。
- [x] Komga精确内容幂等复制、scan/readback与pending/verified分离。
- [x] 独立trellis-check修复、273前端/73CLI、Go/PG/race/vet、lint/build、真实WASM及32浏览器通过。
- [x] README/spec同步，dist重建，私有原图/凭据未入Git，隔离环境清理。

详见 `research/implementation-validation.md`、`recognition-acceptance.md`、`ai-diagnostic.md`、`komga-delivery-acceptance.md`。

本轮代码及隔离验收完成，进入提交与PR审阅阶段；未合并或部署生产。低清/浅色仍可能漏字，不报告整体精度保证；AI省略字段由本地证据/用户核对处理。
