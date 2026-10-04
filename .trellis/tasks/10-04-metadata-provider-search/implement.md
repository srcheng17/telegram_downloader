> 执行更新（2026-10-05）：第二批已实现并完成本地集成检查，状态 review；实际外部验收仍未全部完成。结果以[第二批验收记录](../10-04-clipboard-ocr-tdl-ui/batch-2-verification.md)为准。下文的 planning/未执行描述保留为原计划背景。

# 实施计划：书目检索（Batch 2，尚未开始实现）

## 进入条件

父规划完成审阅并进入实施；metadata-contracts、source-settings-auth、modern-ui-shell 的 Batch 1 契约及接线接口已冻结。读取父设计最新路由命名、共享 draft reducer 和 credentials resolver；与其他 agent 并行时只修改本任务所有权文件，发现共享文件改动需求交父集成者处理。

## 有序步骤

1. [ ] 根据现有公开调研固定三个 API/schema fixture、descriptor、映射与许可清单；先写不联网契约测试，覆盖普通书目、未知字段、系列/单卷和角色差异。
2. [ ] 实现固定目的地 adapters 与限流/缓存/大小上限；只在 adapter 内处理来源协议；验证每源 token 不串用，重定向不泄漏认证。
3. [ ] 实现 application fanout、逐源错误、查询/配置 revision、候选详情和共享 MetadataCandidate 映射；关闭来源与取消按 context 生效。
4. [ ] 实现受保护的独立 handlers；交父集成者注册路由/共享依赖，不新增第二套 admin 或配置表。
5. [ ] 在 shell 插槽实现关键词/来源确认、候选列表、详情 diff 和逐字段采用；所有操作经共享 draft reducer，清理挂载监听器和请求。
6. [ ] 独立 checker 审查数据流、授权、条款、stale response、扩展字段往返；修复后交父集成者串行在本批次接线、重建 dist 和浏览器验收（不等到最后一批才验证）。

## 计划验证（本轮未运行）

- adapter/app/HTTP fixtures：三源成功、空结果、错误鉴权、受限可见性、429/Retry-After、慢/大/畸形响应、取消及跨源 token 隔离。
- metadata fixtures：中文/原文别名、作者/画师/译者、同名异作、总卷数不能写当前卷、未知分级、至少一个扩展字段和全部来源证据往返。
- Node：乱序搜索/详情响应、手工 dirty 值、切换来源、关闭来源、部分失败、重复 mount/unmount、显式采用。
- 后端：`go test ./... -count=1`、`go vet ./...`；前端：`npm run test:frontend`、`npm run lint`；父集成者执行 `npm run build` 并纳入生成产物。
- 集成浏览器：单管理员登录 → 保存无认证及 token 来源配置 → 明确关键词查询 → 选择/采用扩展字段 → 提交任务 → 历史回填不丢 provenance。外部 API 仅按明确选定的非敏感关键词做有界 smoke；自动测试不依赖公网。
- 文档验收：`python3 .trellis/scripts/task.py validate .trellis/tasks/10-04-metadata-provider-search`、`git diff --check`。

## 风险与回滚点

第三方 schema/权限变化、译名和同人覆盖不足通过独立错误/人工确认降级；不承诺搜索必有结果。接线前可独立回退本任务 adapters/UI；上线后用来源 enabled 开关停用外部调用，不删除用户已确认元数据。所有列出的命令是计划检查，不表示已经通过。
