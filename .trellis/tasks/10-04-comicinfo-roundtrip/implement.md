> 执行更新（2026-10-05）：第二批已实现并完成本地集成检查，状态 review；实际外部验收仍未全部完成。结果以[第二批验收记录](../10-04-clipboard-ocr-tdl-ui/batch-2-verification.md)为准。下文的 planning/未执行描述保留为原计划背景。

# 执行计划：Batch 2

本文件为待执行计划；未实施以下产品改动或阅读器测试。前置：Batch 1 元数据文档、clear/lock 与历史快照契约冻结。

## 独占范围

本 worker 新增 `internal/comicinfo/`（解析、profile、映射、合并、序列化、XSD fixtures/测试）、`internal/archive/metadata_bundle*.go`、`internal/downloader/metadata_pack*.go`，及同目录合成归档测试。

root integration 独占旧 `internal/archive/extractor.go` / `external_extractor.go`、`internal/downloader/cbz_writer.go` / `downloader.go` 和 `internal/worker/taskcore/downloader.go` 的调用/签名接线，以及 HTTP 上传扩展名、模板 accept、全局配置、路由、cmd、迁移、dist。worker 提供新接口与接线清单，避免与 tdl/前端 worker 并发改同文件；不撤销其他人修改。

## 顺序

1. 从研究固定 XSD 与 SHA-256，先建立能暴露当前字段顺序、扩展丢失、同名页面覆盖的合成失败测试。
2. 实现 XML adapter 与 ArchiveMetadataBundle：有界读取、重复候选冲突、原始元数据/注释收集；与文档契约对接，支持 missing 与 clear。
3. 实现默认 2.1 draft 导出与离线严格 2.0 对照差异、标准字段保留、未知项 sidecar/警告、Pages 映射及确定页名。
4. 实现“临时打包 → 回读验证 → 发布”，故障注入覆盖写入、关闭、校验和取消；验证原件、旧产物和受保护副本的生命周期。
5. 本批模块交付后 root 立即接线 CBZ 全入口、tdl/上传与历史，再用合成归档跑真实 worker 链路。隔离 Komga 1.28.1、Kavita v0.9.1.4 验证代表字段，记录环境版本，用户部署版本不得假定相同。

## 计划检查

- `go test ./internal/comicinfo ./internal/archive ./internal/downloader -count=1`。
- 接线后：`go test ./internal/worker/taskcore ./internal/httpapi -count=1`。
- `go test ./... -count=1`、`go test -race ./... -count=1`。
- 固定 schema 验证示例：`xmllint --nonet --noout --schema internal/comicinfo/testdata/schema/v2.1-draft/ComicInfo.xsd internal/comicinfo/testdata/generated/ComicInfo.xml`；2.0 使用独立对应 fixture。目录/fixture 由实现新增，不能把当前不存在的路径标记已通过。
- root 集成负责 `bash scripts/verify_release_gates.sh` 与真实隔离阅读器导入；单元测试或 XSD 通过不代替消费者验收。

样例矩阵：中文/XML 转义、空值/clear、Tags/Genre、角色/ISBN/分级、未知扩展、多个 ComicInfo、嵌套 XML、损坏 ZIP/CRC、同目录/跨目录同名、1/2/10、页面索引异常、取消/写盘失败。逐页断言数量、顺序、字节哈希与原件存活；元数据副本和 warning 断言不是只验证 ZIP 至少有一个 entry。
