# Komga 1.28.1 ComicInfo 导入与清空字段语义

2026-10-05 源码只读核对；用于规划字段白名单和同步状态，不表示已在用户书库写入。

## ComicInfo 导入不是对称覆盖

- `ComicInfoProvider` 把空/缺失值转换成 null；`MetadataApplier.getIfNotLocked` 只有在导入值非 null 时才覆盖当前 Komga 字段。因此把旧 ComicInfo 元素清空或删除，单书 analyze 可能保留 Komga 数据库里的旧值。CBZ 回读为空不能推出 Komga 显示已清空。
- 已锁字段不会被 refresh 覆盖。首版保存前拒绝需要同步但仍被锁的字段；不暗中解除锁。导入开关关闭也在改写前拒绝。
- `SeriesGroup`、`AlternateSeries`、`StoryArc`、`StoryArcNumber` 会触发 collection/readlist 的添加语义；从 XML 删除不等于从既有集合/阅读列表删除。首版不把它们当普通 set/clear 字段。系列标题、标签等聚合值来自多本书，单本 CBZ 写回不能保证系列视图等于该本。

## Book metadata PATCH 的清空能力

PATCH 不能代替写回 ComicInfo；若手工 clear 后 Komga 导入器不移除旧值，可在**文件已提交、单书分析之后**，对以下明确白名单字段补充 PATCH 清空并回读。该步骤属于非原子的 Komga 投影同步，失败要保留 `file_committed/sync_pending` 并可重试，不能宣称文件和 Komga 一次事务完成。

| 字段 | 省略 | 可清空的 JSON 值 | 首版策略 |
| --- | --- | --- | --- |
| `summary` | 不改 | `null` 或 `""` → 空串 | 允许 clear；分析后按需 PATCH 空值 |
| `releaseDate` | 不改 | `null` → 真正清空 | 允许 clear；分析后按需 PATCH null |
| `authors`、`tags`、`links` | 不改 | `null` 或 `[]` → 空集合 | 允许 clear；分析后按需 PATCH 空集合 |
| `isbn` | 不改 | `null` 或 `""` → 空串 | 仅当书库关闭 `importBarcodeIsbn` 时允许 clear；分析后按需 PATCH 空值 |
| `title`、`number`、`numberSort` | 不改 | null 为 no-op；title/number 空串被拒 | 首版禁止 clear |

书籍 PATCH 的 `xxxLock`：省略或 null 保持原锁状态；true/false 显式设锁/解锁。首版补充 clear 时不触碰 lock，且 preflight 要确认目标字段未锁。不同 DTO 对 null 的解释不同，不能把通用“JSON null=清空”写进 API 客户端。

Komga 的 `IsbnBarcodeProvider` 可在启用 `importBarcodeIsbn` 时从前/末页条码再次导入 ISBN。若 clear 后仍保持未锁，稍后的 refresh 可能把刚清空的值写回。因此首版在该开关开启时拒绝 ISBN clear，不以 PATCH 后立刻 GET 为空作为持久清空证据。

Komga 导入还会规范化值：Tags 逗号切分、trim/lowercase；标题、简介和编号 trim；ISBN 校验/规范化；NumberSort 从可解析编号派生；作者按角色拆分。实施时须固定 `ComicInfo XML → Komga book 字段 → 规范化/clear/lock/验收` 的字段表，GET 读回按该表比较，不能做原字符串逐字比较。

系列 PATCH 的清空语义不作为首版单本编辑能力：`summary`/`publisher`/`language` 需 `""`；`readingDirection`/`ageRating`/`totalBookCount` 用显式 null；集合型字段用 null/空数组；`status`/`title`/`titleSort` 不能以 null 清空。首版不因修改某本 CBZ 就 PATCH 整个 series。

## 验收解释

分别报告：① CBZ 的唯一根 `ComicInfo.xml` 已写入并回读；② Komga 当前 book 字段与目标一致（可能来自 analyze 或补充 clear PATCH）；③ 是否有证据确认本次 analyze 实际完成。PATCH 后 GET 与目标一致**不能证明** analyze 已读取新 XML；若原值本就一致，也不能仅凭相等推断导入运行。隔离测试要使用同时改变的非 clear 字段验证分析链路，生产状态需诚实注明不可观测的因果限度。

## 来源

- [ComicInfoProvider](https://github.com/gotson/komga/blob/1.28.1/komga/src/main/kotlin/org/gotson/komga/infrastructure/metadata/comicrack/ComicInfoProvider.kt)、[MetadataApplier](https://github.com/gotson/komga/blob/1.28.1/komga/src/main/kotlin/org/gotson/komga/domain/service/MetadataApplier.kt)
- [BookMetadataUpdateDto](https://github.com/gotson/komga/blob/1.28.1/komga/src/main/kotlin/org/gotson/komga/interfaces/api/rest/dto/BookMetadataUpdateDto.kt)、[SeriesController](https://github.com/gotson/komga/blob/1.28.1/komga/src/main/kotlin/org/gotson/komga/interfaces/api/rest/SeriesController.kt)
- [IsbnBarcodeProvider](https://github.com/gotson/komga/blob/1.28.1/komga/src/main/kotlin/org/gotson/komga/infrastructure/metadata/barcode/IsbnBarcodeProvider.kt)
