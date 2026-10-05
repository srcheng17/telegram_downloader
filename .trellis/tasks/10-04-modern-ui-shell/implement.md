> 最新范围（2026-10-05 UI v3）：安装指定设计技能后的控制性设计见 [redesign-v3.md](redesign-v3.md)，验证见 [redesign-v3-verification.md](redesign-v3-verification.md)。一级导航为新建任务、任务、设置；桌面侧栏可完全隐藏，设置分七个 Tab；Telegram 消息来源不在新建页，tdl 登录保留在设置「连接」。下文为第一批历史方案。

> Execution update (2026-10-04): Batch 1 was approved and implemented. The full release gate passed; status is review; the planning statements below are historical. Actual scope and results are tracked in [the Batch 1 report](../10-04-clipboard-ocr-tdl-ui/batch-1-verification.md). No commit or deployment.

# UI 框架实施计划：Batch 1

仅规划，以下命令尚未执行。先冻结父设计和 metadata/auth 接口，再在独占范围实施。现有未提交运行修复保留，禁止撤销其他 worker 的编辑。

## 所有权与依赖

本 worker 负责初始 `web/templates/base.html/index.html/logs.html/settings.html` 结构、新 `login.html/telegram.html` 与 shell partial；`web/static/style.css`；新 `frontend/src/shared/metadata/`、`frontend/src/ui_shell/`；新增对应 `frontend/src/tests/metadata_*.test.mjs`、`ui_shell.test.mjs` 和 `tests/e2e/specs/ui-shell.spec.js`。

root 独占 `frontend/src/app.js`、`shared/page_modules.js`、`shared/api/tasks_api.js`、现有 home/logs/settings/index.js 的行为接线、HTTP/router、模板注册与导航路由、Vite/package/lock/dist、共享 E2E auth 启动。source-settings-auth 独占 `shared/admin_session.js` 和 `settings/integrations*`，第二批业务模块不同时改 shell 模板。初始模板交付后如需调整挂载，由 root 串行修改；本 worker 提供变更清单。

依赖 metadata-contracts 的 registry/version/document/candidate 合成样例和 auth 的状态接口。两者实现未就绪时使用注入 fake；不等待真实 AI/书目/Telegram。root 本批立即接 schema/auth/旧提交历史，不把可用性验证推至最后批次。

## 步骤

1. [ ] 记录旧 DOM ID、入口和正常/错误回归基线；先写动态字段、manual clear、候选过期、历史视图与挂载清理的失败测试。
2. [ ] 固定设计 tokens、页面模板与稳定挂载槽；保持旧 `/logs` 入口及后端 task action 语义。先做桌面核对工作区，再完成窄屏/键盘布局。
3. [ ] 实现 schema decoder、单一 draft、canonical candidate 预览/采用与动态 editor；消费共享定义，不复制七字段业务规则。支持 aliases、count/volume/number、自定义和定义版本快照。
4. [ ] 通过 fake adapters 验证登录/受保护 shell、任务与历史状态、未知/失效 schema、慢响应与错误。fake 代码只能在测试，缺真实能力时页面明确不可用。
5. [ ] 与 auth worker 联调登录挂载/退出/401，以及设置 UI 槽；提供 metadata-fields 槽给 root 绑定受限字段设置和 definitions_version 更新通知；root 接 transport/CSRF/session 检查，实测无业务内容闪现、失效后不恢复敏感历史。
6. [ ] root 将旧 home 提交/历史、logs 行为映射到新 shell；逐项移除重复七字段状态，保证 metadata_document 唯一。历史提交/产物两视图不能混写任务输入。
7. [ ] 运行 focused tests 与静态检查，由 root 一次构建共享 dist；隔离 Compose 浏览器验证镜像里的模板、bundle 和样式。保存脱敏截图并根据实际布局修正。
8. [ ] 交付 slot/adapter 接口及视觉/键盘验收记录，第二批模块复用 editor；真实 OCR/AI/provider/tdl 不计入本任务 fake 验证成果。

## 计划检查命令

新增后运行，以下四个测试文件当前尚未创建：

```sh
node --test frontend/src/tests/metadata_schema.test.mjs frontend/src/tests/metadata_draft.test.mjs frontend/src/tests/metadata_editor.test.mjs frontend/src/tests/ui_shell.test.mjs
```

现有回归入口可执行；产品源码实施后按前端规范执行：

```sh
node --test frontend/src/tests/page_modules.test.mjs frontend/src/tests/page_async_lifecycle.test.mjs frontend/src/tests/home_metadata_history.test.mjs frontend/src/tests/logs_polling.test.mjs
npm run test:frontend
npm run lint
npm run build
```

模板/路由接线后由 root 运行 `go test ./internal/httpui ./internal/httpapi -count=1`。新增 `tests/e2e/specs/ui-shell.spec.js` 并接入合成管理员启动后，隔离 Compose 浏览器入口为：

```sh
npm run e2e:test -- specs/ui-shell.spec.js
```

该现有 runner 会启动/清理隔离栈并转发 Playwright 参数；不得改为对生产 URL 做验证。fake browser 测试只拦截候选/provider 数据，真实 session/页面/bundle 的集成检查另列，不以路由拦截模拟鉴权成功。最后由 root 统一执行发布门禁，避免重复跑全套 E2E。

## 必须覆盖

- 动态 schema：7 字段以外至少 8 个标准字段及四种自定义类型；false/0/空值区别，未知定义只读不删，旧版本不能静默重解释。
- 候选/历史：编辑、clear、锁定、请求迟到、切页重挂载、批量冲突；effective_metadata_document 缺失/存在与提交快照分开，采用不改原历史。
- 生命周期：重复 mount、失败导航、确认/取消离开、401/退出、history restore/BFCache、未挂载节点；监听/请求/计时器释放且无敏感浏览器持久化。
- 旧链路：Telegraph 提交、本地上传 HTML 413/取消/重试、任务筛选分页、Komga 失败反馈。既有行为更改必须有契约依据，不为视觉调整删断言。
- 浏览器：四组视口、200% 缩放、键盘顺序/焦点恢复、reduced-motion、中文长文本/URL；输出中记录真实完成范围和未跑项。

## 交付与回退

模块测试通过与真实接线通过分别记录；未接能力保持禁用和明确说明。必要回退只撤本模块未交付布局/模块变更，不退回未鉴权页面、不删除已保存扩展文档；旧路由兼容和隐私清理规则保留。提交/推送/合并依父任务授权规则。
