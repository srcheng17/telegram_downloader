# 验证记录 · 2026-10-04

## 已完成修复

R1 HTTP 单次请求超时重试；R2 ZIP/RAR/7Z 自然数字排序；R3 URL 编码保真；R4 URL 创建/重试并发去重；R5 日志筛选草稿保留；R6 轮询与翻页请求的调度归属；R7 查询词截断保持有效 UTF-8。

## 实际运行结果

| 检查 | 结果与范围 |
| --- | --- |
| `go test ./... -count=1` | 通过，配置独立临时 PostgreSQL，数据库测试没有因缺少 DSN 跳过 |
| `go test -race ./... -count=1` | 通过，同一隔离 DB 串行执行 |
| `go vet ./...` | 通过 |
| R4 最终索引修正后的聚焦测试与 race | 通过；全量 Go 检查在此次 SQL 索引条件修正前完成，其后重跑受影响测试 |
| R4 EXPLAIN | 20,010 行合成数据验证普通 canonical_url 分支使用现有索引；无迁移或新索引 |
| `npm run test:frontend` | 79/79 通过，含 5 项新增日志异步回归 |
| Node browser preflight tests | 6/6 通过；浏览器预检选择 chromium |
| `npm run lint` | 通过 |
| `npm run build` | 通过；重复构建 4 个 bundle SHA-256 完全一致 |
| `npm run e2e:test` | 11/11 通过；独立 Compose 项目及数据库，真实 ZIP 经 API/worker/PG 生成 CBZ，校验 1/2/10 顺序的图片字节、ComicInfo 元数据和 Komga 复制 |
| `git diff --check` | 通过 |

## 审查与边界

- 独立 reviewer 复核最终 R4 锁顺序、READ COMMITTED 语句快照、NULL/空字符串旧输入回退及索引条件，通过；前端草稿/已应用状态、迟到请求、卸载重挂和定时器所有权通过。
- 下载超时、归档排序和日志异步回归曾在旧实现复现失败，修复后通过。
- `scripts/verify_release_gates.sh` 未整段运行；本轮执行上述检查及独立构建一致性验证，没有将未提交 bundle 相对 Git index 的差异误报为构建失败。
- E2E 中其他 UI mock 用例仅证明交互契约，不代表真实外部 Telegram/Telegraph 下载成功。
- 临时 PostgreSQL 容器与连接信息已清理；E2E 自动清理自身容器、镜像、卷、网络和临时目录。
- 生产环境、上传容量、状态机和 schema 未改变。无新增 Telegram 运行依赖。
- 本地改动未提交、未推送、未部署；任务保留待提交状态，不归档 Telegram 尚未完成的验证任务。
