> Execution update (2026-10-04): Batch 1 was approved and implemented. The full release gate passed; status is review; the planning statements below are historical. Actual scope and results are tracked in [the Batch 1 report](../10-04-clipboard-ocr-tdl-ui/batch-1-verification.md). No commit or deployment.

# 实施顺序与验证计划

当前仅规划。以下命令是实施后计划，不代表本轮已经执行测试。不得自行 start、改 task.json、部署、创建隧道或调用真实来源。

## Batch 1 边界

与 metadata-contracts、modern-ui-shell 并行；仅在各自模块内实现。先向父集成确认 design 中接口、迁移 016 和稳定挂载点。metadata-contracts 负责扩展字段/schema version；modern-ui-shell 负责模板/布局；本模块提供认证、配置与 integrations UI 行为。共享 `cmd`、config/router、现有 settings 入口、dist 由父集成统一装配，避免并发覆盖。

## 顺序

1. [ ] 明确元数据输出契约与 provider-search 共享 `PublicSourceConfig`/`ResolveCredentials` 签名，包含 priority、credential_configured、config_version 和白名单非秘密选项；provider catalog 的 config_revision 仅映射同一 config_version。公开 provider registry/能力目录保持由搜索子任务拥有，本模块负责 noauth/凭据合法性。冻结匿名白名单、既有 /healthz、/readyz 的最少信息响应及 12h/30m 期限。
2. [ ] 增加迁移 016 表契约与 pgx 仓储测试：singleton bootstrap、session 摘要/吊销/过期、配置 CAS、加密字段 round-trip。父集成按 015 → 016 顺序应用，禁止修改旧迁移。
3. [ ] 实现密码哈希、私密 env/file 加载、随机会话/CSRF、限流、重置/轮换的最小本地操作。先验证未知/无 secret 启动不可放行业务，进程重启不重置管理员。
4. [ ] 实现 AES-GCM 密文、key_id/AAD、keep/replace/clear、整数 priority 与适配器白名单 filters/field_preferences；测试同一事务的 expected_version CAS、稳定排序、未知/不支持选项拒绝，以及错误 key/tamper/跨来源复制、不回显/不记录、清除不回退。接入 `POST /api/settings/sources/{provider}/test`，复用搜索适配器固定样例只读能力；缺能力、失败和迟到反馈可区分，不发送当前 OCR。无需真实 API 密钥。
5. [ ] 新 handlers 与根 middleware 由父集成接入。逐路由覆盖页面/htmx/API/v2/上传/附件/SSE/Telegram 准入，保护现有 `/settings` POST 和 `/v2/settings` PUT；旧 InternalToken 不作为网页登录旁路。完成 CSRF 传播与未登录 401/导航处理。
6. [ ] 通过 httptest 受控模型服务器实现 models/test：无凭据、本地地址、准确 model ID、路径拼接、redirect 禁止、body/timeout 边界、已知非文本/未知能力、版本过期、空列表和 HTTP 错误分类。固定测试样例接入扩展 schema，禁止真实 OCR 自动上传。
7. [ ] integrations UI 模块接入 shell：设置保存/秘密动作、模型搜索/手动 ID、独立发现与测试状态。Node tests 覆盖 config revision、请求取消与迟到、保存冲突、切页/退出清除、密码不入浏览器存储。共享 dist 由父集成构建并检查。
8. [ ] 隔离数据库和 Compose E2E 完成匿名拒绝 → 登录 → 配置 → models → 固定样例测试 → 退出吊销；同一环境回归现有下载/上传/历史/设置。使用合成密码/API key 和受控本地模型服务器，不能从生产取 secret。
9. [ ] 父任务另行落实 MiniCPM 网络通路，再以管理员明确配置的端点验证模型发现和固定样例；本子任务报告中把 mock 验证、真实可达性和真实提取分别记录。

## 计划测试命令

先运行新增包/handler/前端状态的 focused tests，再执行完整层级检查。数据库使用父任务创建的独立 `TEST_DATABASE_URL`，禁止并行跑共享固定 ID 的 PG 套件。

```sh
go test ./internal/app/adminauth ./internal/app/sourcesettings ./internal/credentials ./internal/modelapi -count=1
go test ./internal/httpapi ./internal/httpui ./internal/httpv2 -count=1
go test ./internal/store/postgres/adminauth ./internal/store/postgres/sourcesettings ./internal/store/postgres/migrations -count=1
go test ./... -count=1
go test -race ./... -count=1
go vet ./...
npm run test:frontend
npm run lint
npm run build
npm run e2e:test
```

实现完成并正确暂存 dist 后由父集成执行 `bash scripts/verify_release_gates.sh`。数据库/E2E 未实际运行时明确标记，不把本轮文档校验算运行验收。

## 必须通过的审查矩阵

- Bootstrap：缺少、重复设置、权限错误、symlink、并发初始化、已初始化重启；断言无明文持久化，无匿名降级。
- Auth：匿名及过期/退出 cookie 的所有路由，session fixation、12h/30m、密码轮换、CSRF/Origin、伪造转发头、站外 return URL、服务器重启；SSE 吊销后限时断开。
- 配置：CAS 冲突、keep/replace/clear、priority 整数/范围/同值稳定排序、filter/field preference 白名单与 unsupported 拒绝、config_revision 映射、empty/invalid key、跨源隔离、错 key/tamper、密钥轮换失败事务回滚；对响应与捕获日志检索合成 secret，而非只审查正常路径。
- 逐来源测试：固定只读样例、禁当前 OCR/自定义 URL/headers、能力不支持、凭据缺失/401/403/429/timeout、版本过期、无秘密回显；成功范围不冒充成人权限验证，测试不修改 enabled/priority/草稿。
- 出站：固定源目的地、AI loopback 可用、非法 scheme/userinfo/query/fragment、base path、cross-host 和同 host redirect；第二受控服务器收不到秘密；OCR/模型输出不能指定 headers/目标。
- 模型：data[].id 去重/格式/上限、空/401/403/404/429/timeout/非 JSON/超大体、缺失当前 ID、已知仅图片/未知能力、过期版本、测试拒绝/截断/不合 schema；不发送页面 OCR、不自动选模型。
- UI：键盘与错误焦点、敏感 input 清理、普通设置兼容、旧请求无覆盖、htmx 返回/退出无密码或截图历史；实际镜像包含更新后的 dist。

## 回滚点

- 迁移前：检查隔离数据库备份、外部 key 单独备份与部署 bootstrap；未准备就不开放新版本。
- 迁移后集成失败：新增表保留，关闭外部入口或维护页，恢复受保护旧服务；禁止裸露旧无认证版本。
- 配置保存失败：CAS/事务回滚至旧配置与凭据版本；失败模型调用保留配置和草稿。
- 主密钥轮换失败：回滚密文事务并保持旧 key，验证后才清理旧版本；不把回滚理解为恢复已吊销会话。
- 本轮仅规划文件；没有代码、数据库、网络或服务变更需要执行回滚。

AI 测试验证状态绑定 definitions_version、固定 field_keys/fixture_version；字段定义变化旧测试失效。120秒推理端点须经父集成的有界 Go/nginx 超时策略，通过实际代理验证，不能只测 handler。

迁移交接：提取规则表契约在第一批016落地前交付，016一次创建其独立非秘密记录结构，第二批仅接服务/控件。若已应用后仍需调整，使用新的前向迁移编号，不修改已应用016。
