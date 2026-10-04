# Komga 1.28.1 官方 API 与存量元数据编辑

2026-10-05 只读调研。以下是规划依据，不表示已连接用户的 Komga 实例或修改书库。

## 列表与定位

- `GET /api/v1/libraries` 取得书库，`GET /api/v1/libraries/{libraryId}` 重读当前书库设置；`LibraryDto` 含 `importComicInfoBook`、`importComicInfoSeries`、`importBarcodeIsbn`，可用于保存前判断导入和 ISBN 清空资格。`POST /api/v1/series/list`、`POST /api/v1/books/list` 用条件与全文搜索分页列出系列、书籍；`GET /api/v1/books/{bookId}` 读取所选书籍详情。BookDto 包含 `id`、`name`、`url`、`sizeBytes`、`libraryId`、`seriesId`、`metadata` 和 `lastModified`。以 book ID 作服务端操作标识，不用浏览器传入宿主文件路径。
- 旧 `GET /series`、`GET /books`、`GET /series/{seriesId}/books` 自 1.19.0 起废弃，首版应使用 `/list` 接口并核对实际目标实例版本。

## 写入与权限

- `PATCH /api/v1/books/{bookId}/metadata` 更新书籍，`PATCH /api/v1/series/{seriesId}/metadata` 更新系列；成功返回 204，需要 Komga ADMIN 权限。官方 OpenAPI 提供 HTTP Basic 或 `X-API-Key` 认证。项目应由后端保存管理员凭据并用自己的管理员会话和 CSRF 防护代理访问。
- 书籍支持 `title`、`summary`、`number`、`numberSort`、`releaseDate`、`authors`、`tags`、`isbn`、`links` 及对应锁字段。系列支持 `title`、`titleSort`、`summary`、`status`、`publisher`、`language`、`readingDirection`、`ageRating`、`genres`、`tags`、`totalBookCount`、`sharingLabels`、`links`、`alternateTitles` 及对应锁字段。项目自定义字段不能假定可全部写入 Komga。
- Komga Web 编辑器会自动处理字段锁；官方 PATCH DTO 的实现不会自动把修改字段设为锁定。若单独以 Komga 数据库为人工编辑目标，保存人工值时须显式提交对应 `xxxLock: true`，以免后续元数据刷新覆盖它。本任务以 CBZ 为源，写回后仅对导入器无法清掉旧值的白名单字段补充 PATCH-clear，保持原锁状态，不自动上锁或解锁；预检已锁字段时拒绝保存。只发送改动字段，PATCH 后 GET 回读校验。
- PATCH 没有 `If-Match` 或版本前置条件。保存前可重读并比较元数据快照，但检查与 PATCH 无法构成原子事务；`lastModified` 也不能直接当作元数据版本号。计划中需明确并发覆盖限制，避免宣称强一致冲突保护。

## 文件内元数据与刷新

- PATCH 修改 Komga 自己的元数据存储，不会写回 CBZ 内 `ComicInfo.xml`，也不要求媒体库扫描。官方 API 没有 ComicInfo 文件写回端点。
- 用户已选择首版写回 CBZ 内 ComicInfo。Komga 1.28.1 提供 `POST /api/v1/books/{bookId}/analyze`（202，只代表入队）：`AnalyzeBook` 重读归档并更新 media 文件索引，READY 后自动入队 `RefreshBookMetadata`，再刷新系列元数据。推荐保存后对所选 book 调 analyze，并有界轮询 `GET /api/v1/books/{bookId}`；若展示系列字段，再轮询系列详情。Komga 导入要求该库开启 `importComicInfoBook`/相应系列选项，已锁定字段不会被刷新覆盖。
- `POST /api/v1/books/{bookId}/metadata/refresh` 不能替代 analyze：首次新增 ComicInfo 时，导入器先检查旧 media 文件索引是否包含它，单独 refresh 会跳过。库级 `POST /api/v1/libraries/{libraryId}/scan?deep=true` 可作受控恢复，但范围大、可能较慢；普通 scan 可能因目录 mtime 未变而跳过。首版优先单书 analyze，不默认扫描全库。
- 导入器要求 ZIP **根目录**精确名 `ComicInfo.xml`，大小写与路径不符会被忽略。系列级字段由多书聚合，单本改动不保证成为系列展示值。analyze 返回 202、SSE 事件或全局队列数字都不能单独证明本次导入完成；须分别回读 CBZ 和 Komga 可映射的变化字段，并对未变化/锁定字段如实报告验证限度。
- Komga 管理员 BookDto 的 `url` 是其本机 file URL，LibraryDto 的 `root` 是其本机路径。项目只有明确配置的 Komga 库根到本进程共享挂载根的映射才能写回；服务端按 book ID 重新读取、限定根路径并拒绝符号链接/跨根，浏览器不得自报绝对路径。若目标书库无共享写入挂载，该书只能列出，不能宣称可编辑。

## 官方来源

- [书籍列表](https://komga.org/docs/openapi/get-books)、[书籍元数据更新](https://komga.org/docs/openapi/update-book-metadata)、[系列元数据更新](https://komga.org/docs/openapi/update-series-metadata)
- [编辑元数据与锁定](https://komga.org/docs/guides/edit-metadata)、[扫描、分析和刷新](https://komga.org/docs/guides/scan-analysis-refresh)
- [1.28.1 OpenAPI](https://github.com/gotson/komga/blob/1.28.1/komga/docs/openapi.json)、[LibraryDto 源码](https://github.com/gotson/komga/blob/1.28.1/komga/src/main/kotlin/org/gotson/komga/interfaces/api/rest/dto/LibraryDto.kt)、[BookMetadataUpdateDto 源码](https://github.com/gotson/komga/blob/1.28.1/komga/src/main/kotlin/org/gotson/komga/interfaces/api/rest/dto/BookMetadataUpdateDto.kt)、[BookController 源码](https://github.com/gotson/komga/blob/1.28.1/komga/src/main/kotlin/org/gotson/komga/interfaces/api/rest/BookController.kt)
- [单书分析](https://komga.org/docs/openapi/book-analyze)、[书籍元数据刷新](https://komga.org/docs/openapi/book-refresh-metadata)、[书库扫描](https://komga.org/docs/openapi/library-scan)、[TaskHandler](https://github.com/gotson/komga/blob/1.28.1/komga/src/main/kotlin/org/gotson/komga/application/tasks/TaskHandler.kt)、[ComicInfoProvider](https://github.com/gotson/komga/blob/1.28.1/komga/src/main/kotlin/org/gotson/komga/infrastructure/metadata/comicrack/ComicInfoProvider.kt)、[BookDto](https://github.com/gotson/komga/blob/1.28.1/komga/src/main/kotlin/org/gotson/komga/interfaces/api/rest/dto/BookDto.kt)
