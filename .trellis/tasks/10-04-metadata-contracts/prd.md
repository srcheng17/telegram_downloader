> Execution update (2026-10-04): Batch 1 was approved and implemented. The full release gate passed; status is review; the planning statements below are historical. Actual scope and results are tracked in [the Batch 1 report](../10-04-clipboard-ocr-tdl-ui/batch-1-verification.md). No commit or deployment.

# 可扩展元数据契约与存储

状态：planning；本轮仅规划，未实现、未运行产品测试。所属父任务：[截图识别、tdl 与现代 UI](../10-04-clipboard-ocr-tdl-ui/prd.md)。执行批次：Batch 1。

## 目标与范围

将仅七字段的跨层传递升级为版本化 `metadata_document`，使 OCR、规则、AI、书目来源、人工编辑、历史回填与 ComicInfo 共用一份字段事实。保留现有七字段输入输出适配，不保留可独立修改的第二份事实。

- 建立有类型的标准字段注册表及受约束的命名空间自定义字段；作品别名、Count/Volume、常用出版、语言、日期、创作者角色、标识符、阅读方向、分级与页数均可存储。
- 每字段携带来源、修订号、人工锁；缺省、候选、已采用值和明确清空具有独立语义。
- 定义任务不可变快照、历史 JSONB 与旧行升级契约；新迁移预留 015，由父任务集成，不改旧迁移。
- 给下游提供解码、校验、候选采用、legacy 投影接口及合成样例。

不含：采集源 HTTP 客户端及凭据、截图持久化、AI/OCR 实现、动态脚本、通用工作流、页面重设计、ComicInfo 序列化、Task Core 状态机改造。

## 验收标准

- [ ] 大于七字段的文档在保存、任务快照、历史读取、重试后无损；人工锁、清空标记、来源与版本保持。
- [ ] 别名有界保存且明确仅内部使用；count、volume 与当前 number 分开，不猜测映射来源 totalVolumes。
- [ ] 注册表版本/定义快照随任务与历史保存；schema GET、无持久化 validate/patch 共用同一类型与 MetadataCandidate，旧快照不随设置重解释。
- [ ] 注册表明确每个字段的类型、边界、枚举、导出能力；非法类型、未知顶层 key、未注册自定义字段或超限文档不能静默截断后保存。
- [ ] 同一字段候选在手工编辑/清空后迟到，或基于旧 revision，采用时返回冲突且不覆盖；外部建议不能直接建立人工锁。
- [ ] 缺字段保持原值；明确 clear 删除有效值并保留 tombstone；历史回填也走采用规则。
- [ ] 旧七字段请求与响应保持兼容；新旧协议混用出现冲突时明确拒绝，不隐式选取一份；旧数据库行确定性投影到 v1。
- [ ] JSONB 是新写入的权威元数据；旧列如暂留，仅为同一事务内生成的兼容投影，不接受独立写入。
- [ ] provider/source ID 与字段证据可核对，凭据、原始截图、OCR 全文不进入文档、历史、日志或导出。
- [ ] 自定义字段可保存与回填；无 ComicInfo 映射时明确“仅项目内保存”，不会伪造标准 XML 字段。

## 依赖与产物

依赖父任务冻结共享设计；向 `10-04-comicinfo-roundtrip`、采集源、OCR/AI 和 UI 子任务提供公共契约。文档与详细执行范围见 [design.md](design.md)、[implement.md](implement.md)。
