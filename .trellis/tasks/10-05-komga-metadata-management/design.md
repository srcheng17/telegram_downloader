# Komga 存量 CBZ 元数据编辑设计

状态：in_progress；依据 [PRD](prd.md)、[Komga 1.28.1 接口调研](research/komga-official-api.md)、[字段清空语义](research/komga-field-semantics.md)、[CBZ 写回边界](research/safe-cbz-writeback.md)。首版以 CBZ 内的 `ComicInfo.xml` 为保存目标，Komga 是随后异步更新的读取视图。补充 PATCH 只用于导入器无法清除旧值的白名单字段，绝不能代替归档写回。

## 边界与入口

- 在左侧新增「作品库」一级入口，列表和编辑在同一工作区：书库筛选/关键词/分页 → book 详情 → 从 CBZ 加载的元数据草稿 → 差异预览 → 明确保存 → 文件与 Komga 双重结果。非 CBZ 或没有共享写入映射的 book 可见但只读。标题、系列、文件名足以辨认目标；对浏览器隐藏 Komga 的 `file:` URL、宿主路径、API key 和备份路径。
- `internal/infra/komga` 只封装官方书库/书籍列表、详情、单书 analyze 与受限 PATCH-clear；`internal/app/komgaedit` 编排资格、字段映射、版本、备份、写回和同步；独立 `internal/archive/cbzedit` 处理 ZIP 文件。`internal/comicinfo` 继续拥有 Parse/profile 与字段语义，但存量编辑须有独立 merge policy，不能盲用新产物 `Merge` 的 PageCount/Pages/无效值处理；HTTP 和页面不重建规则。既有 Task Core 快照、下载器和新产物打包器不参与存量编辑。
- 连接凭据放服务端加密设置，显示于「设置 → 连接」时仅回报是否配置；端点校验 http(s)、禁止 URL 内含凭据和跨目标重定向，远程连接需 HTTPS。库 ID 与 Komga 容器根路径到本应用挂载根的映射由部署配置允许列表提供；不能在浏览器用任意路径扩张写入范围。当前 Compose 只挂载一个子目录，列表可读其他 Komga book，编辑资格须按映射单独计算。
- API 草案：`GET /api/komga/libraries`、`GET /api/komga/books?library_id=&query=&page=`、`GET /api/komga/books/{id}`、`POST /api/komga/books/{id}/preview`、`POST /api/komga/books/{id}/save`、`GET /api/komga/edits/{operationId}`、`POST /api/komga/edits/{operationId}/sync` 与显式 `restore`。名称在实施时对齐路由约定，但列表/预览/保存/重试职责保持分离；所有业务路由走现有管理员中间件，写操作还校验 Origin/CSRF。

## 标识、映射与预览契约

1. 列表使用 Komga `POST /api/v1/books/list` 与 `GET /api/v1/libraries`，由服务端限定允许的 library ID、分页大小与搜索长度；对 API 数据做输出投影。book ID 是外部稳定标识，客户端从不提交文件路径。选择时重新 GET book 与其 library；Komga 1.28.1 隔离实例实测 `BookDto.url` 为 `/media/demo.cbz` 绝对路径，兼容受限的 `file:` URI，二者都必须落在该库根下，再以配置的 Komga 根→本地共享挂载根映射得到相对路径。
2. 配置根自身及相对路径的每个分量逐级 `Lstat`，拒绝 symlink；继续用 Go 1.27 `os.OpenRoot` 的受根限制方法打开/替换，拒绝非普通文件、路径穿越、跨根与非 `.cbz`。`os.Root` 会跟随仍在根内的 symlink，单次末项 Lstat 不足以满足本契约。对 Komga URL 解码/规范化后再比较 path segments，不使用字符串前缀冒充目录包含关系。文件打开后的身份、大小和 SHA-256 形成 `sourceVersion`，预览与保存都重新检查；限制总大小、条目数和实际解压量，避免 ZIP bomb。
3. 解析唯一根目录、精确大小写的 `ComicInfo.xml`；若无 XML，生成空 baseline。旧 XML 的受支持标准字段进入当前字段注册表/Document 草稿；同时显示 Komga 当前投影及字段锁/导入状态，避免把两者误当同一事实。`metadata_document` 的 absent/set/clear 和人工修订用于本次草稿；页面后台刷新不得覆盖 dirty 字段。
4. 预览接收 book ID、sourceVersion、定义版本和已确认文档，返回逐字段旧/新值、导出能力、ZIP/ComicInfo 警告、Komga 导入/锁状态、预计操作和新的短时 preview token。按固定字段矩阵区分 ComicInfo set、Komga book 导入/规范化、可 clear 与仅文件级字段；首版仅开放有可核对单书投影及锁状态的字段，系列级和其他无单书投影字段只读，避免单本编辑隐式修改系列。book 的 summary/releaseDate/authors/tags/isbn/links 可 clear，但启用 `importBarcodeIsbn` 的书库禁清空 ISBN，以免后续 refresh 从页面条码回填；title/number/numberSort 禁 clear，`page_count` 不能作普通手工字段，旧值异常时须独立确认修正。未映射自定义字段只显示 `internal_only`，首版不保存在本 book 的另一个隐形存储。已有未知 XML 扩展、注释/属性、歧义 Pages 或竞争元数据若无法保证保存后读者语义一致，拒绝写回并保留原件；不能因为有备份就静默丢弃它们。
5. 库关闭 ComicInfo 导入、所改 Komga 字段处于 lock、目标 `media.status` 不可分析或目录没有共享写入映射时，预览给出不可保存原因。首版不自动解锁 Komga 字段；管理员可在 Komga 中明确调整后重新预览。预览不写文件、不锁字段、不扫描全库。

## 文件写回与恢复

- 保存要求最新 preview token、sourceVersion 与稳定 idempotency key；服务端重新取得 book/库/路径和文件 SHA。相同 key 的重复请求先对既有 operation 做有界恢复判定，再返回其可继续状态，不再备份/写回。每个映射路径应用内串行；外部不合作写者在最后检查与替换之间仍可能竞态，不能声称强 CAS。只有 XML 无差异、Komga 当前投影已一致且没有待处理 operation 才返回完全 no-op；XML 无差异但存在明确 clear 或待同步值时，持久记录仅同步操作，不制造文件备份或重复写入，仍需 analyze、必要的 PATCH-clear 与回读。
- 先持久登记 `preparing` 操作，再在独立私密 `KOMGA_EDIT_BACKUP_ROOT` 下按 operation ID 建目录，使用固定文件名**独立复制**完整原始 CBZ（不硬链接到可能被原位修改的来源）、SHA-256 和操作清单。目录/文件权限 0700/0600，备份根不在 Komga 或公开下载目录；先校验剩余空间，备份写完、fsync、回读哈希后推进 `prepared`，才允许改目标。备份无自动 TTL；未来清理由管理员明确执行，首版展示占用和位置类别而不公开路径。
- 新写入器读取并验证原 ZIP 全部条目的 CRC/大小，确认唯一 ComicInfo、有效 XML、页面 identity/order 和未改字段可保真。存量专用 merge policy 只改已选字段；当前 `comicinfo.Merge` 会重算 PageCount、过滤无效 Web/GTIN、可能重排 Pages，不能直接复用其输出。旧 PageCount 不符时只可在预览列为额外差异并获确认；其他未改值/顺序有差异则拒写。目标同目录建隐藏且非漫画扩展名的随机临时文件（如 `.<nonce>.tmp`），避免 Komga 扫描半包；只替换旧 XML 或追加唯一根 `ComicInfo.xml`。其他条目以 `archive/zip.Writer.Copy` 按原中央目录顺序复制压缩流、可观察 FileHeader/entry comment，并保留全局 ZIP comment。Go 会重建 local header/中央目录；无法证明保真的 prefix/trailing/local-only extra 等非标准结构拒写。`Writer.Copy` 不校验内容，因此生成后重开逐条核对非目标条目的索引、名称、压缩方法、可观察 header、解压 SHA-256/CRC/大小及页面顺序，校验 XML profile、set/clear 与 Pages 身份。
- 临时文件 Close/Sync、文件与原件再次校验成功后在同目录原子替换，fsync 父目录，再回读目标哈希/XML。不同文件系统不能用非原子复制代替。原件备份与操作记录保留；若保存被取消、写入/校验失败或源版本变化，删除本次临时文件并报告，不更改原件。
- 用专门的持久操作记录区分 `preparing`、`prepared`、`file_committed`、`sync_pending`、`current_value_consistent`、`sync_failed`/`restore_needed`，记录 book/library、映射相对路径、原/新哈希、备份引用及可回读的字段预期，不将绝对路径或 XML 正文写入普通日志。文件系统与 PostgreSQL 不在同一事务：记录预期新哈希后才 rename；若进程在备份/rename 与状态更新间崩溃，启动扫描及同 key 保存/operation 查询/同步重试先执行有界 reconciliation，通过操作清单、备份哈希与目标旧/新哈希恢复可继续状态，并处理有记录的孤儿备份/临时文件；无法判定时转 `restore_needed`，不盲目再次改写。显式恢复原件也重新按 book ID 解析允许映射、要求目标仍匹配本操作新哈希、备份校验通过，并先保留当前版本；不能覆盖后来别人所做的修改。

## Komga 同步契约

- 文件写回成功后调用 `POST /api/v1/books/{bookId}/analyze`。202 只表示入队；首次新增 ComicInfo 时不能仅用 `/metadata/refresh`，因为 Komga 需先更新 media 文件索引。默认不对整库 deep scan。有界轮询 book `media.status=READY` 与可导入、**经 Komga 规范化后的**非 clear 字段；Tags、作者、ISBN、NumberSort 等不能按 XML 原字符串比较。对于已明确 clear 的白名单 book 字段，analyze 不会清掉旧值，随后按字段 DTO 语义发送最小 PATCH 清空（不改 lock），再 GET 读回。系列字段可能由多书聚合，不用单本值硬判失败；SeriesGroup/StoryArc 添加语义不纳入首版。
- 分别报告 ① 归档 XML 已写回或本次无需写文件，② Komga 当前字段与目标一致，③ 本次 analyze 是否有足够证据确认完成。PATCH 后的 GET 一致只能证明当前显示，不证明 analyze 已导入；仅 clear、原值已相同或字段无法从 GET 观测时，第三层必须标记“无法证明”，不能凭 READY 或相等值宣称完成。同步超时、Komga 不可达、book 消失、PATCH 失败或字段仍不一致时保留 CBZ/备份，状态 `sync_pending`/`sync_failed`，不能报告完全成功。管理员按同一 operation ID 重试 analyze/必要 PATCH/回读；先核对目标 SHA 与 book 路径，不重复写回或覆盖后来文件。
- 后续 Komga 自身扫描可能更新 file mtime/hash；单书 analyze 只保证内容分析和元数据刷新，不等价于库扫描。需要全库 deep scan 时另作明确维护动作和成本提示，不作为保存默认步骤。实际阅读器兼容与 Komga 1.28.1 行为须在隔离库验证；目标实例版本不同先探测能力。

## 兼容与回滚

- 固定 ComicInfo 2.1 draft profile，运行时解析回读，自动化测试用仓库固定 XSD 与 `xmllint --nonet`；不能只以 XML 可解析断言 schema 合规。页面图片不能重编码、重命名或重排。ZIP 中央目录偏移可改变，不要求整包字节相同。
- 新功能通过独立路由/开关停用时，已改 CBZ 和私密备份仍存在；不能用数据库迁移回滚删除备份引用。生产部署前先在隔离 Komga/Kavita 书库证明保存、重扫、失败恢复，并核对真实挂载路径。文件恢复是单独显式操作，不因异步同步失败自动回滚用户已保存的 CBZ。
