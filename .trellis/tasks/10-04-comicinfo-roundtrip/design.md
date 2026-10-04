> 执行更新（2026-10-05）：第二批已实现并完成本地集成检查，状态 review；实际外部验收仍未全部完成。结果以[第二批验收记录](../10-04-clipboard-ocr-tdl-ui/batch-2-verification.md)为准。下文的 planning/未执行描述保留为原计划背景。

# ComicInfo 与归档往返设计

## 格式固定与公共输入

输入为 `metadata_document v1` 及其 definitions_version/definition_snapshot 的已确认快照及 `ArchiveMetadataBundle`（原始 XML/其他元数据 entry、ZIP comment、源页面 identity/order、解析警告）。XML 解析和归档 IO 在 infrastructure；字段类型、锁与 clear 采用 domain 契约，不在 worker 重建规则。

默认 profile 为 `comicinfo-2.1-draft@99e1453a163c777b4b5320a68732f6f133ac7918`；2.0 使用同提交 schema/v2.0。实现时将固定 XSD 与许可作为测试资源纳入仓库，核对研究报告 SHA-256，验证不能在线随 HEAD 漂移。

2.0 没有 Tags/Translator/GTIN；两个 XSD 都要求 sequence。首版只输出固定 draft2.1，2.0 用于离线对照，不提供导出切换。默认 UI 不要求用户理解 schema，显示未导出字段与兼容警告即可。

## 字段与顺序

由单一映射表输出 schema 顺序：Title、Series、Number、Count、Volume、Summary、日期、创作者角色、Publisher/Imprint、Genre、Tags、Web、PageCount、LanguageISO、Format、Manga、AgeRating、Pages、GTIN 等；完整顺序以 pinned XSD 为准，不能沿用现有 Go struct 顺序。

- 标准创作者角色分别写入；社团、源站 ID、未知自定义字段留内部。aliases 无标准直接映射，保留内部并提示；不塞到 Title/Series。count → Count（同一系列总册/期数），volume → Volume（系列轮次/卷系），number → Number（当前册/期标识）；不能从来源 totalVolumes 猜填任何一个。
- Web 默认一个不含凭据的公开规范 URL；详细来源不输出为自造 Source 元素。
- Tags/Genre 为逗号列表；内部值保留来源分类语义。无法无损表示包含分隔符的项时报告损失，不静默制造多项。
- GTIN 只从经校验标识符选取；多个候选先确认，不把任意 record_id 填进去。
- 未知数值/枚举省略；AgeRating 使用合法枚举。同性题材不推断成人分级。
- PageCount 由实际图片计算；来源页数仅供核对。Manga 映射区分 Unknown/No/Yes/YesAndRightToLeft；不能将 rtl 与明确非漫画矛盾静默覆盖。

## 读取、合并与保留

原始 ComicInfo 先有界读取：单文件 1 MiB、元数据总量 8 MiB；原 XML 禁止 DTD/外部实体解析，限制嵌套深度 64，归档总量仍受既有 500 MiB 等限制。超限或损坏停止处理并保留原件，提示修正归档后重新提交；首版不支持中途等待选择/忽略解析，不能将失败当无元数据。

先统计全包 ComicInfo 候选，总数唯一才读取；唯一非根候选提示已规范化；多个候选或重复 ComicInfo entry 返回冲突并停止，保留原件供修正后新建任务。所有候选原始字节均先保存，不把不变快照的普通重试当作修复方式，不新增等待输入的任务状态。

合并顺序：原包值作 baseline，任务已确认文档按 set/clear 合并；缺 key 不改。标准但未进入可编辑 registry 的元素仍由 XML adapter 保留其语义。明确 clear 是 tombstone，不能被 baseline 补回来。

严格 schema 产物与保留未知扩展分别处理：输出 XML 只含 profile 支持且可校验的元素；原始 XML/未知属性、元素、注释等作为受保护 source-metadata 副本保留，报告未导出列表。不得保留未知扩展后仍宣称严格 XSD 合规，也不能直接丢掉。副本写入失败则不发布产物。

source-metadata 存在任务/执行代际 artifact 目录下的私密 sidecar，有独立保留引用，保留至管理员明确删除关联任务/原件；不能通过普通漫画下载路由暴露，不能只放随成功清除的上传临时目录。记录名称、字节数与哈希便于恢复；原始 XML 副本不等于完整漫画备份。只复制已有元数据，不新增截图、OCR 或凭据。

ComicBookInfo JSON comment、MetronInfo 等已识别的竞争元数据原样保存到私密副本，默认不作为仍生效的元数据写入输出，提示已保留原件及未导出原因；不主动生成第二格式或同步其字段。可确认不承载元数据的 ZIP comment 在 ZIP 长度限制内原样保留；无法判断时保留在私密副本并提示。未识别非图片 entry 不执行、不全量复制为任意附件；记录并保留原件，按父集成的持久 source artifact 策略保留至明确删除，Complete 不清除其引用。不能以“元数据保存了”宣称整个归档无损重写。

## 页面 identity 与发布

页面 identity 使用原 entry 路径加原位置，避免同名条目失去身份。保持现有目录级自然排序，最终输出固定宽度序号文件名，图片不重编码，验证哈希逐页对应。

Pages/Image 按源图片索引建立映射后重排；若源索引越界、重复歧义或无法确定原页面枚举顺序，不猜测：保留原 Pages 副本，省略无法可靠映射的输出项并明确警告。不能因 schema 允许就声称下游消费 Pages。

在同目录临时文件完成写入后重新打开：唯一根 ComicInfo、无重复页面名、图数、顺序、逐页哈希/CRC、映射与 XML profile 检查通过才 rename。临时失败/取消清理不能删原件、source-metadata 副本或先前成功代际产物。沿用 generation 隔离，不改 Task Core 终态资格。

XSD 全量验证在自动化测试/发布检查实际运行；运行时由同一映射表、类型验证与 XML 回读防错，不能把普通 XML unmarshal 标记为 XSD 通过。若引入运行时 XSD 库或外部命令，须在集成确认依赖、超时和离线行为后统一决定。

## 产物有效元数据

打包模块返回合并后 `effective_metadata_document`、导出 profile、未导出/损失警告和 source-metadata 清单；父集成按当前成功 task/generation 保存，与用户提交的不可变 document 分开。来源补全及实际 PageCount 属于派生结果，不回写提交字段或改变人工锁；重试从提交快照重新派生。未注册的标准 XML 字段继续由格式适配器保留并在清单注明，不能因不在表单中而静默删除。
