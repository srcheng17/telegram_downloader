> 2026-10-05 范围调整：用户移除新建任务的 Telegram 消息下载来源，保留 tdl 登录并迁入「设置 → 连接」；旧 `/telegram` 跳至 `/settings#connections`。后端接口、历史任务及账号数据保留。后续验收按 [UI v2 设计](../10-04-modern-ui-shell/redesign-v2.md) 执行，不再要求首页 Telegram 来源入口。

# 执行与证据计划

状态planning；以下是实施时的命令与检查点，本轮未执行产品测试。

## 第一批开始即执行

1. 用户审阅父任务最终计划后，启动当前交付子任务；读before-dev及相关spec，记录未提交runtime修复基线和worker所有权，禁止reset/stash他人改动。
2. 冻结DTO/路由/fixture和迁移预留，先补跨层失败测试；root逐个接入metadata、auth、shell。source能力使用注入端口，未接入provider明确不可测。
3. 加入自定义定义/文档JSONB兼容、匿名/CSRF/健康探针及旧页面回归；focused检查通过后再放行第二批。

## 第二批交付时执行

4. 协调OCR依赖、worker/WASM/语言离线发布及Vite构建入口；规则设置版本和模型transport统一接线。provider、tdl、packaging按依赖队列接入，不能同时抢写共享文件。
5. tdl先验证pinned helper/CLI真实授权交接、锁继承/孤儿进程与硬输出额度，再开放网页和Task Core下载；失败保留feature未就绪状态。
6. 打包合并结果与提交快照分开保存；验证成功/失败代际fencing、原件/sidecar保留引用及取消/崩溃清理，不新增任务生命周期状态。
7. 每批共享接线结束统一重建dist并跑相关检查，记录实际结果。

## 最终验证顺序

- 已新增模块的focused tests先跑；数据库使用独立TEST_DATABASE_URL，整套固定ID的PG测试串行，缺库不算通过。
- `go test ./... -count=1`、`go test -race ./... -count=1`、`go vet ./...`。
- `npm run test:frontend`、`npm run lint`、`npm run build`；检查镜像内bundle和同源OCR资源，不仅本地dev server。
- `npm run e2e:test`（需要单独定位时）或最终统一 `bash scripts/verify_release_gates.sh`；release gate已经含重复检查/E2E时组合运行一次，不重复刷结果。bundle gate比较Git index，阶段性验证在可审阅/staged产物后进行，不自动commit。
- 固定XSD与合成归档fixture自动检查，再用实际文件验证Komga/Kavita读取；逐页hash/CRC、Tags、扩展字段、原有元数据/清空/未知副本纳入报告。
- 真实多图OCR/扩展字段AI/三源书目/Telegram附件分项执行，只读外部样例；记录命中、拒绝、耗时、大小/内存、未覆盖权限，不提交秘密或原文。
- 隔离备份恢复、错密钥/缺密钥拒绝服务、改密后旧SSE关闭、会话过期、取消/重试/磁盘不足及旧入口回归。
- `git diff --check`，检查生成物/文档一致性；由未实现本模块的reviewer复查越界写、协议漂移和证据。

## 退出条件与恢复

PRD I1–I11均有证据才完成；外部关键验收缺失则保持未完成并说明具体条件。根据验证更新README/配置样例/部署恢复说明，记录Trellis会话。只停用或回退本批模块，不删除已持久化数据/秘密/session；保留原runtime修复。无自动commit/push/PR/merge或生产切换，提交遵守English Conventional Commits及仓库授权规则。

迁移交接：提取规则表契约在第一批016落地前交付，016一次创建其独立非秘密记录结构，第二批仅接服务/控件。若已应用后仍需调整，使用新的前向迁移编号，不修改已应用016。
