> Execution update (2026-10-04): Batch 1 was approved and implemented. The full release gate passed; status is review; the planning statements below are historical. Actual scope and results are tracked in [the Batch 1 report](../10-04-clipboard-ocr-tdl-ui/batch-1-verification.md). No commit or deployment.

# 执行计划：Batch 1

本文件是待执行计划；本轮未运行下列产品验证。先批准父任务共享设计，再开始实施。

## 独占范围

本 worker 新增：`internal/domain/metadata/document*.go`、`registry*.go`、`merge*.go`、`legacy*.go`；`internal/app/metadata/`；`internal/store/postgres/metadatadoc/`，含同目录测试和合成 fixtures。复用既有 normalize，不大范围重写。

root integration 独占：现有 taskcore 输入/服务/仓储接口接线、`internal/app/tasks/types.go`、`internal/httpapi/`、现有历史仓储入口、迁移 `015_*`、cmd/router、前端 dist。worker 提供类型/函数签名与迁移建议，不能越界同时修改这些文件。与其他 worker 共用目录时不撤销其修改。

## 顺序与交付门槛

1. 为旧七字段投影、扩展字段、明确清空、迟到候选、字段锁、非法类型和边界先写失败测试，冻结类型与 JSON 合成样例。
2. 实现 registry、版本化 decode/validate、纯合并规则和 legacy adapter；输出同一字段注册表及 definitions_version/快照供前端渲染与 AI 白名单使用；交付 canonical MetadataCandidate。
3. 增加应用采用接口与 PostgreSQL document 编解码/事务 helper；向 root 交付 schema GET、自定义字段版本化保存、validate/patch 的无持久化应用接口、015 不可变定义版本、提交/产物文档列与回填约束，测试数据库由集成统一分配。
4. 与 root 验证任务创建 → PG → worker 输入、历史回填、重试快照全链路；对 NULL 旧行和新旧请求冲突做契约回归。
5. 发布字段、错误及示例契约后，允许 Batch 2 packaging/OCR/provider 消费；接口若改变，先通知依赖任务，不各自复制兼容逻辑。

## 计划检查

- 局部：`go test ./internal/domain/metadata ./internal/app/metadata ./internal/store/postgres/metadatadoc -count=1`。
- 接线后：`go test ./internal/httpapi ./internal/app/taskcore ./internal/store/postgres/... ./internal/worker/taskcore -count=1`。
- 后端与仓储规范：`go test ./... -count=1`、`go test -race ./... -count=1`。
- PG 必须使用隔离 `TEST_DATABASE_URL`，不与其他任务并发跑同一测试库；缺数据库只报告未运行，不能以 skip 代替验收。

测试重点：两个相同基线的无持久化 patch 响应先后到达，页面只接受仍匹配当前 revision 的一个；服务器不得把纯函数校验误写成持久草稿 CAS；manual clear 不被恢复；未知版本不丢失；JSONB/历史回填/重试保留扩展和定义版本；aliases/count/volume/number 区分且总卷数不误映射；旧字段投影确定且无第二写入口；非法来源不泄露秘密。前端显示和完整发布门禁由 root 集成统一验收。每批模块完成后立即接线与运行本批检查，不将所有接线延后到最终批次。
