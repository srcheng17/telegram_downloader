> 2026-10-05 范围调整：用户移除新建任务的 Telegram 消息下载来源，保留 tdl 登录并迁入「设置 → 连接」；旧 `/telegram` 跳至 `/settings#connections`。后端接口、历史任务及账号数据保留。后续验收按 [UI v2 设计](../10-04-modern-ui-shell/redesign-v2.md) 执行，不再要求首页 Telegram 来源入口。

# 集成边界与验证设计

## 调度与所有权

依赖父 [设计](../10-04-clipboard-ocr-tdl-ui/design.md) 和 [分批计划](../10-04-clipboard-ocr-tdl-ui/implement.md)。最多3个实现worker并行，root集成/评审；第一批用已冻结接口和测试double独立开发，模块交付即接线。最终验收等待七模块全部完成，集成工作从第一批开始，不把所有路由留到最后。

root独占 `cmd/server/`、`cmd/worker/`、`internal/config/`、共享router、旧HTTP/Task Core/history/store/worker接口接线、迁移runner与编号、`frontend/src/app.js`/page_modules/共享transport/旧home及logs接线、依赖manifest/锁文件、Vite、dist、Docker/Compose/E2E及项目文档。worker仅写父计划分配的新增模块；模板初版由shell写完后移交root，业务worker以挂载契约接入。

集成不复制领域规则；共享registry是类型权威，domain定义set/clear/候选采用，application编排提交/原包合并，infra负责SQL/XML/网络，worker驱动现有Task Core。fake只用于测试，不能成为生产异常回退。

## 分批接线

1. Batch1：metadata registry/自定义定义版本仓储 → 文档提交/legacy adapter/历史；auth包裹业务router → CSRF transport → shell登录/设置/通用editor；冻结来源descriptor供配置验证，无真实adapter时显示尚未接入。
2. Batch2：OCR资源与rule设置 → AI端口；真实provider的search/resolve/test → settings能力注入；tdl helper及受控session目录 → Telegram API/worker；原包bundle → 合并应用用例 → CBZ → 结果快照。
3. Batch3：镜像内资源和完整路径；数据库/秘密恢复；真实服务与阅读器；收敛部署文档和已知限制。

共享端点遵循父设计，自定义字段设置和提取规则配置作为独立非秘密资源，不能塞入既有四项公开settings。modelapi由配置任务定义transport，OCR消费提取端口，root协调新增方法，禁止两个包互相导入。

## 数据与执行代际

预留015：不可变字段定义版本、提交文档、历史和结果effective文档；016：admin/source/AI及独立extraction_rules配置；017：Telegram账户/尝试/协调。落地前复查最新编号；新增迁移只前向执行，不改已应用文件。旧记录NULL按legacy适配，兼容列只由document投影。

创建任务在原事务固定提交文档和定义快照。执行时导入原包baseline，再应用已确认set/clear，派生实际页数和导出信息。结果文档与artifact归同一task/generation；沿当前fencing更新成功结果，不通过metadata通道绕开lease检查。重试始终从提交文档派生；页面分别选择提交/产物版本作为新草稿候选。

临时CBZ先验证再rename，DB失败不得把无结果登记的文件暴露为新成功产物；依照既有artifact代际目录恢复/清理。文件系统与DB非单事务，要有幂等晋升/孤儿回收测试，不能只测正常写入。

需保留的归档原件和source-metadata先提升到受保护task/generation artifact并登记保留引用；达到条件后才允许清理临时源。原件不得经普通漫画下载接口泄露；保留到管理员明确删除关联任务/原件，无自动TTL；已引用文件不能被Complete清理。空间不足拒绝新增保留/发布并保持旧文件，页面展示保留占用及原因。完整原件与仅XML副本不可混称。

## 验证分层与外部边界

- deterministic：领域/HTTP/仓储/前端状态、错误和竞态；真实隔离PG执行迁移/lease/JSONB，禁止mock替代PG验收。
- 镜像E2E：真实上传/worker/CBZ/Komga copy；包含匿名拦截、登录CSRF、旧入口回归、拓展字段/历史、真实打包与取消恢复。UI mock只证交互，不算外部成功。
- 实际依赖：真实脱敏截图多图OCR，三来源中性只读样例及可获得授权，已选模型扩展字段测试，真实Telegram授权和受控附件，隔离Komga/Kavita读取。验证记录分别列范围与未覆盖项，不公开原文/账号/凭据。
- RackNerd模型当前loopback部署不改动；实施验收可用临时、受保护、可清理的通道做容器真实请求。持久网络及生产开放需单独明确范围；若通道不可用则相关验收未完成，不能用本机curl冒充容器访问。
- 未获来源成人权限时只报告未验证，不能凭零结果认定未收录；模型合成七字段基线只作参考，重新验证扩展字段与上下文预算。

E2E bootstrap使用测试专用随机密码与独立主密钥文件，trace/截图遮罩密码/二维码/来源token/私有OCR；Compose config和日志不带明文环境秘密。worker和API的session/文件锁共享卷要在实际容器中验证。

## 长请求与事件流

现有 Go WriteTimeout=30s 不能覆盖 AI test/extract 的120s预算。root 仅对这些受保护路由通过 ResponseController 设置有界写 deadline（例如130s），配合应用120s总 deadline 与 nginx 对应 location 的140s读取上限；读取正文仍有独立大小/超时限制，不全站无限放宽。

扫码 SSE 采用专属 location 关闭 proxy buffering/cache，Go 逐事件 Flush，每15s心跳并刷新有界写 deadline，按 attempt 总期限终止；会话最长30s复查，断线后用 attempt 状态快照+seq 重连，过期 QR 不重放。handler 若不支持 Flush/deadline 必须明确失败。通过真实 nginx → Go 验证超过30s的慢 AI 响应、即时 QR/心跳、取消断线及登出撤销，不以直连 handler 或 mock 替代。

## 恢复与发布

数据库备份与外部主密钥分开受保护保管；恢复校验版本与可解密性。回滚不drop新表/列，不让旧代码覆写扩展文档。含auth升级回退不得直接重新开放无认证旧服务，先维护页或已验证外层保护。每批可停用新入口保留旧手工路径，但不能以开关绕过整站鉴权。生产部署不属于本地验收自动操作。
