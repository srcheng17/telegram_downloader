# 修复下载与日志交互缺陷

## 目标与授权
修复 2026-10-04 审查中已确认的错误，维持外部 API 与当前架构。用户在审阅问题、影响和修正方向后明确回复“好的，继续”；本文件将已批准范围落盘，不新增产品决策。

## 验收标准
- R1：单次 HTTP 请求超时按 retries 重试，真正的任务上下文取消立即停止，不能误取消其他图片。
- R2：ZIP/RAR/7Z 图片按自然数字顺序处理，1/2/10 不错序；保持目录及零填充排序稳定。
- R3：合法 URL 编码路径在规范化及 worker 请求中保持语义，空格、中文、编码分隔符不得双重编码。
- R4：同 canonical URL 的创建与重试共享去重保护。旧失败任务重试遇到其他活跃任务返回冲突；手动 force 仍不能绕过活跃去重。保留 generation fencing。
- R5：日志轮询不覆盖正在编辑的筛选条件。
- R6：手动翻页不被旧轮询定时器打断；卸载、迟到响应、失败重试行为保持正确。
- R7：长查询词不会被截成非法 UTF-8，保留现有长度限制的资源边界。
- 所有缺陷有能识别原始问题的回归测试；Go test/race/vet、前端 tests/lint/build 通过，dist 同步；使用隔离 PostgreSQL 检验去重和并发；实际 E2E 验证上传链路。

## 非目标
不变更上传容量、ComicInfo 字段、保留策略或部署拓扑；不清理历史数据库；不增加 Telegram 运行依赖；不 push、merge 或部署。

## 证据
- internal/downloader/downloader.go:393,743：Client.Timeout 被误归为全局取消，已合成复现 retries=2 但仅一次请求。
- internal/archive/extractor.go:116 与 external_extractor.go:84：字典序排序，已复现 1/10/2。
- internal/httpapi/api.go:218：EscapedPath 赋值 Path，已复现 %20 变 %2520。
- internal/store/postgres/taskcore/store.go:268：Retry 不参与 canonical URL lock。
- frontend/src/logs/index.js:435,466,503：轮询重置输入、旧定时器中断翻页，均已复现。
- internal/httpapi/logs_query.go:13：字节截断产生无效 UTF-8，已复现。
