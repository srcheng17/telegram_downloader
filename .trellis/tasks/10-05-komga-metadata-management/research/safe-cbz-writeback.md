# 存量 CBZ 写回 ComicInfo 的安全边界

2026-10-05 只读源码复核。用户已明确首版写回 CBZ 的 ComicInfo；这里记录实现门槛，不表示已编辑任何实际书库文件。

## 不可直接复用的现有打包器

- `internal/downloader/metadata_pack.go` 的 `PackageMetadataCBZ` 面向新产物：只输出 ComicInfo 与规范化名称/自然排序后的图片，并拒绝覆盖已有目标。它会丢弃存量文件中非图片附件及竞争元数据，不能用来改写已有书籍。
- `internal/downloader/cbz_writer.go` 的旧打包流程同样重组图片，也不是存量文件保真编辑器。现有 `internal/comicinfo/Parse`/`Merge` 可复用字段语义，但不能原样调用 `Merge` 后宣称只改选中字段：它无条件重算 PageCount、可能过滤无效 Web/GTIN，并可能重排 Pages。固定 2.1 draft 严格序列化也不会保留未知 XML 元素、属性或注释。存量编辑需独立的 merge policy 与输出前后字段差异检查；不能保真的输入拒写。

## 建议写回流程

1. 由 Komga book ID 获取当前文件标识，服务端只在配置允许的共享书库根目录内解析相对路径；用 Go 1.27 `os.OpenRoot`、逐路径分量 `Root.Lstat` 等有界根操作拒绝符号链接、非普通文件、目录越界和非 CBZ。`os.Root` 本身允许根内相对符号链接，不能只检查最终项。浏览器不提供实际写入路径。当前 Compose 仅挂载一个子目录，覆盖其他 Komga 库前须显式配置对应只读/可写映射，不以 Komga URL 自动放开宿主路径。
2. 有界检查 ZIP 中央目录、条目数、声明解压大小与实际读取大小；完整读取/校验所有条目的 CRC 和大小。ComicInfo 必须是唯一根目录 `ComicInfo.xml`，大小写/嵌套/重复候选造成歧义时拒绝。损坏 ZIP、DTD、加密或不支持的压缩方式及超限归档拒绝修改。
3. 使用 `comicinfo.Parse` 与受限的存量专用 merge policy 合并本次手工 set/clear；`page_count` 不作普通可编辑字段，旧 PageCount 与实际页数不符时只有在预览列为额外变化并经确认才修正。页面不重排，Pages 保持原枚举身份与顺序；Web/GTIN 等即使未编辑也不能因严格输出被静默过滤。未改标准字段的语义和可观察值必须保留；无法无损表示的 XML 扩展和列表值在预览中标明并拒绝写入。
4. 对原件生成版本 token（完整 SHA-256、大小及辅助文件身份），确认用户打开后未被外部改动。先建立 `preparing` 操作记录，再在私密目录**独立复制**完整原件备份（不能硬链接到外部可能原位写入的 CBZ），校验哈希、fsync 文件与目录，并检查剩余空间；备份未完成时不能动原件。备份记录与本次操作 ID 关联，保留至管理员明确清理，不走任务成功后的自动清理。
5. 在目标同目录建隐藏、非漫画扩展名的临时文件（例如 `.<nonce>.tmp`），防止 Komga 扫描到半包。原位置替换唯一根 ComicInfo；原包没有 ComicInfo 时在确定位置新增一个。其他所有条目按原中央目录顺序使用 `archive/zip.Writer.Copy` 复制原压缩流、可观察 FileHeader 与 entry comment，保留目录、图片、封面、附件、竞争元数据及 ZIP comment。Go 会重建 ZIP local header/中央目录，不能保证 local-only extra、前缀或尾随数据字节不变；检测到无法证明保真的非标准结构时拒写。`Writer.Copy` 绕过校验，故仍须单独验证原件与临时包所有条目的解压内容。
6. 临时文件 close/fsync 后重开，按中央目录相对序号+名称比较除 ComicInfo 外的解压 SHA-256、CRC、长度、压缩方法、顺序与 ZIP comment；校验唯一根 XML、set/clear、未改字段和页面/Pages 身份。再次核对源版本后，在同目录原子替换并 fsync 父目录；读取新文件验证。失败清理本次临时文件，保留原件和备份；冲突返回明确状态并保留草稿。
7. 应用内按文件串行编辑；客户端提供稳定 idempotency key/operation ID，HTTP 响应丢失后重复保存应返回已有操作状态，不重新备份或覆写。若备份/rename 与状态记录之间崩溃，按持久记录和目标旧/新哈希恢复。外部不合作写入者在最后一次版本核对与替换之间仍有竞态，不能宣称跨进程原子 CAS。通过复核、备份和结果回读降低风险，冲突或恢复状态如实报告。

## 必须验证的不变量

- 原文件未因预览、失败、取消、权限不足、空间不足、CRC 错误或并发版本变化而改变；备份可读并与编辑前哈希一致。
- 除唯一目标 XML 外，ZIP 条目数量、中央目录相对顺序、名称（含同名不同序号）、压缩数据或解压内容哈希/CRC/长度、压缩方法、可观察 FileHeader/entry comment 及全局 comment 不变；漫画图片字节和实际阅读顺序不变。ZIP 条目的物理字节偏移与中央目录偏移可随目标 XML 变化，不能要求整个新旧 CBZ 字节逐位相同。
- ComicInfo 保持唯一根条目，符合固定 profile 的运行时回读和测试时的 XSD 检查；明确清空与保持不变可区分。Komga/Kavita 的实际隔离导入是独立验收，不能仅以 ZIP/XSD 通过代替。

## 现有源码锚点

- `internal/downloader/metadata_pack.go:38-219,242-299`、`internal/downloader/cbz_writer.go:64-131`：新包重组/发布及回读模式。
- `internal/comicinfo/parse.go:45-149`、`internal/comicinfo/merge.go:27-255,377-422`：有界解析、set/clear 合并、严格序列化边界。
- `internal/archive/metadata_bundle_retention.go:24-53,111-187`：完整原件留存、无覆盖/哈希/fsync 可参考机制；新编辑不能借用任务代际路径。
