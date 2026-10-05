> Execution update (2026-10-04): Batch 1 was approved and implemented. The full release gate passed; status is review; the planning statements below are historical. Actual scope and results are tracked in [the Batch 1 report](../10-04-clipboard-ocr-tdl-ui/batch-1-verification.md). No commit or deployment.

# 元数据文档设计

## 边界与当前缺口

现有 `internal/app/tasks/metadata.go` 清洗七字段；HTTP、Task Core 输入、历史和 `TaskMetadata` 分别映射，扩展信息容易在中间层丢失。规则归 `internal/domain/metadata/`，采用用例归 `internal/app/metadata/`，SQL 归 PostgreSQL 仓储；worker 只消费已确认快照。

## v1 公共契约

`metadata_document = {schema_version: 1, definitions_version: string, revision: uint64, fields: {key: FieldState}, definition_snapshot: {key: FieldDefinition}}`。schema_version 表示结构版本，definitions_version 表示不可变字段注册表版本，revision 表示该份文档的编辑代际，不能混用。

`FieldState = {state: value|cleared, value?: typed, revision: uint64, manual_locked: bool, provenance: [...]}`。缺 key 为 absent；cleared 必须无 value；空数组、空串不得伪装 clear。每次成功更改递增文档 revision；被修改字段记该 revision。文档 revision 可从 0 开始，字段 revision 不大于文档 revision。

来源记录最少包含 `kind`（manual/archive/ocr/rule/ai/provider/legacy）、`source_id`、可选 `record_id`、公开 `public_url`、采用时间、可选证据定位及置信度。证据定位使用截图 ID/字符区间等引用，不存 OCR 正文；provider 配置 ID 与凭据版本可作为非秘密标识，不保存密钥或私有 URL。置信度未知时省略，不能编造统一分数。

候选统一为 `MetadataCandidate`，OCR/AI/provider 不另定义字段建议类型：

```text
MetadataCandidate
  candidate_id, request_id, origin
  schema_version, definitions_version
  base_document_revision, input_revision, config_revision?
  field_revisions: {key: uint64}  # 缺省字段基线为 0
  fields: {key: {state: value|cleared, value?, provenance, warnings?}}
  warnings: []
```

未采用候选不进入事实文档；candidate 不携带可控制最终事实的 manual_locked。若传输需 `SuggestionSet`，它仅是 `{request_id, candidates: MetadataCandidate[]}` envelope，禁止维护另一套 draft_revision 或字段结构。跨来源不自动认定同作，冲突保留供确认。

## 字段注册表

| key | 类型/意义 | 主要映射 |
| --- | --- | --- |
| title / series / number / summary | string；number 保留小数或特别篇文本 | Title / Series / Number / Summary |
| aliases | string[]；有界的作品别名，不把译名自动当同作证明 | 内部保存，无直接标准 Alias 元素 |
| count | 正整数；已确认同一系列编号体系的总册/期数 | Count |
| volume | 正整数；ComicInfo 的系列轮次/卷系标识，部分资料用起始年份 | Volume；不是本册 number |
| tags / genres | string[]；保留来源原分类，不混年龄等级 | Tags / Genre |
| creators.writer、penciller、inker、colorist、letterer、cover_artist、editor、translator | 分角色 string[]；社团不自动当作者或出版社 | 对应创作者元素 |
| publisher / imprint / language / format | string；language 校验语言代码，format 保留出版形式 | Publisher / Imprint / LanguageISO / Format |
| publication_date | {year,month?,day?}；保留精度，校验实际日期 | Year / Month / Day |
| identifiers | [{scheme,value}]；ISBN/GTIN 与源站 ID 分开 | 经校验选出的 GTIN；其他内部保存 |
| reading_direction / manga | ltr、rtl、unknown / yes、no、unknown | Manga 合并映射；冲突提示 |
| age_rating | ComicInfo 合法枚举或 unknown | AgeRating；无推断 |
| page_count | 非负整数；最终导出由实际页数校正 | PageCount |
| web | 单一公开 http(s) URL，不含凭据 | Web |

legacy `author/comic_name/series_name/series_number/summary/tags/genres` 分别投影到 writer/title/series/number/summary/tags/genres。旧 author 与标签解析继续调用既有 normalizer，不重复实现；新数组值不再按空格拆分。作者角色不能混写，系列总卷数不能填 number。count/volume 上限 2147483647；未知值缺省，不写 0 猜测。来源 totalVolumes 不自动填 count 或 volume，先核对它是否表示同一系列编号体系的总数、作品总卷数或系列轮次；number 始终表示当前册/期的文本编号。

自定义 key 首版限定为 `custom.user.<name>`，name 为 `[a-z][a-z0-9_]{0,31}`；仅已注册定义可使用，不开放任意来源命名空间。定义带中文 label、固定类型 string/string[]/integer/boolean，不接受任意 object、脚本、模板求值或 XML 名称。标准与自定义定义采用同一 FieldDefinition，包含 key、中文 label、类型、边界、来源可提取能力、editable、export_mapping/status。首版自定义字段无标准映射，UI 显示未导出。

初始上限作为统一常量：文档 UTF-8 JSON 256 KiB；最多 64 自定义字段；普通 string 4 KiB，summary 16 KiB；列表最多 64 项、每项 1 KiB；来源最多 8 条/字段。验证拒绝超限，不截断；父任务若统一调整预算，须同步所有消费者与样例。

## 注册表版本与 HTTP 接口

- 注册表是唯一字段定义源。`definition_snapshot` 保存该文档使用字段的不可变定义投影；不是允许客户端提交任意新 schema 的入口。服务端在保存时从已验证的 definitions_version 构建/校验快照，拒绝客户端篡改类型或映射。
- 增加 optional 字段发布新 definitions_version；停用只关闭新录入，不删除历史定义/值。不兼容类型、语义或导出映射变化使用新 key 或显式迁移。旧任务按已保存快照解释与打包，不能被当前字段设置重解释；公共注册表实现须保留可解码的旧定义版本。
- `GET /api/metadata/schema` 返回 `{schema_version, definitions_version, definitions, limits}`。这里公开的是无秘密定义；与其他业务路由一样需要管理员会话。UI、provider 与 AI 生成请求约束消费同一注册表，不维护独立七字段 schema。
- `GET/PUT /api/settings/metadata-fields`：管理员受保护的自定义字段设置，PUT 携带 expected_definitions_version 及完整自定义定义集合，新增/调整标签或约束/停用形成不可变新版本；内置字段不可删除或改类型，使用过的自定义 key 不可改变类型/语义，停用不删除历史快照。保存拒绝超过64项、重复key或任意export mapping，冲突返回409；由 root 将定义编辑器挂入设置页。旧版本定义持久保留，schema GET 引用同一仓储，避免仅进程内注册导致重启丢失。
- `POST /api/metadata/validate` 输入 `{document}`，输出规范化文档及字段错误/导出警告；输入中的版本/定义快照必须匹配受支持版本，不能以最新定义静默覆盖。
- `POST /api/metadata/patch` 输入 `{document, expected_revision, operations}`；需要采用候选时 operations 引用上述 canonical candidate 及选定 keys，应用统一合并规则。输出 `{document, warnings}`，revision 冲突返回既有 API 409 契约，类型/超限返回 400。
- 首版 validate/patch 是无持久化校验与转换，页面保有草稿；服务端不会为两份独立提交的相同旧草稿建立跨请求排他锁。浏览器仅在响应基线仍匹配当前 revision/input/config 时采用，否则丢弃；真实持久化发生在现有创建任务事务，不能宣称存在未实现的持久草稿 CAS。

## 合并、编辑与兼容

- `Decode/Validate` 只接受支持版本和已知字段类型；遇未来版本返回明确不支持，不降级丢字段。
- `ApplyPatch(document, expected_revision, operations)` 提供 set/clear/unlock；未出现 key 不修改。人工 set/clear 建立锁，unlock 是明确动作。对锁定字段采用候选必须作为用户确认操作且 revision 匹配，后台不能解锁。
- `ApplyCandidate` 检查输入/配置与被选字段 revision；任一冲突返回字段冲突，整次选择原子失败。其他字段可由用户重新选择后提交，不能部分成功却显示全部采用。
- 历史回填也是候选采用；启动新草稿时可完整克隆，必须分配当前草稿 revision，旧异步响应失效。
- 请求含 metadata_document 与 legacy 七字段时，以适配后语义比较；不一致返回输入冲突，一致则只保存 document。legacy 省略/空值保留既有协议含义；explicit clear 仅新 patch 协议表达，不改变老客户端行为。

## 存储与集成

015 迁移由 root integration 负责：保存不可变注册表版本、自定义定义；为 Task Core 提交快照、按执行代际的产物有效快照及 metadata_history 增加 JSONB 文档；具体表名以当前仓储为准，不另建并行任务中心。旧行 NULL 时按 legacy 确定性读取为 v1；新写入保证文档非空。回填以批次/事务实施并保留兼容列，回退版本只能使用仍保留的投影。

任务创建在既有事务保存完整文档快照；重试复用该快照，不读取后续草稿/来源设置。提交历史保存已提交文档；产物结果另存 `effective_metadata_document`，包含原包补全和实际页数，按 task/generation 关联成功 artifact，由详情/历史明确标注提交与产物版本，不能回写提交文档。原 XML 合并使用 domain 规则、由应用用例编排、worker 驱动；新定义快照沿用提交定义版本，未注册标准 XML 字段由适配器/产物清单保留。仅在当前 generation 成功发布并通过既有 fencing 的事务中晋升结果，失败代际不覆盖。仅扩展元数据持久化，不更改现有 URL 身份锁、generation、lease 或 task action 资格。

权威读策略为 document 非 NULL 即只读 document；兼容列由单一 adapter 生成，禁止“读取时合并两份可编辑真相”。来源凭据独立由设置任务管理，metadata_document 只引用非秘密 ID。
