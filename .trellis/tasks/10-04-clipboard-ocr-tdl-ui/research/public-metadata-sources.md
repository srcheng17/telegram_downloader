# 公开漫画书目源与 BL / 男同向漫画检索

> 范围更新：用户已明确元数据可扩展、来源及授权可配置；当前契约以 [父设计](../design.md) 为准。研究中的七字段实测仍只代表历史样例，不是扩展字段验收。

调研日期：2026-10-04。状态：planning，未接入产品、未部署新服务。只核对公开书目/API 资料与少量非露骨作品关键词；未发送用户截图、频道消息或文件，未下载封面或章节。

## 结论与范围

可增加“关键词搜索 → 选择作品/版本 → 对照并采用字段”，与多图本地 OCR 并列。优先评估 MangaBaka 聚合 API、MangaUpdates 和 Bangumi；使用某一来源前分别落实数据许可、署名和输出到 ComicInfo 的条件。Komf 是可参考或独立试用的现成开源聚合工具，但本轮不部署、不自动改写 Komga。

商业 BL、男同向漫画/gay manga/geikomi、成人同人志是不同收录维度。本轮接口字段与少量书目命中只能证明可检索和存在对应分类，不能证明特定成人同人志有完整数据库。检索失败时继续使用多图 OCR、MiniCPM 与人工确认。

## 来源对比与证据

### MangaBaka：统一搜索候选

- [官方 API 说明](https://mangabaka.org/data/api) 和 [API Explorer](https://mangabaka.org/data/api/explorer)：公开 JSON 接口，汇总 AniList、Anime-Planet、Kitsu、MangaUpdates、MyAnimeList、Shikimori；schema 尚未承诺 1.0 稳定性。
- 实际匿名调用 `GET https://api.mangabaka.org/v1/series/search?q=...`：`Naruto` 返回 HTTP 200、10 条当前页结果；`弟の夫` 返回 HTTP 200、7 条当前页结果。两者首条均有对应标题匹配，以及 titles/authors/artists/description/tags_v2/content_rating/source 字段。不是精度统计或成人向覆盖测试。
- 官方当前限流：未命中缓存的搜索 30 次/分钟，默认读取 180 次/分钟，以 IP 和 leaky bucket 计算；429 时退避，不通过代理绕过。
- [数据许可](https://mangabaka.org/about/data-license)：MangaBaka 自有数据 CC BY-NC-SA 4.0，要求署名；`source` 下第三方数据适用其各自条款。无来源或来源不明不能一概当成自由授权数据。个人非商业用途与公开再分发需要区分，不因聚合服务提供了字段便声称可任意镜像上游数据库。
- 可优先使用其已统一的标题/作者/分类作为候选；源标识仍应保留。作者/ISBN 定向搜索是否有专门参数需以最终采用的 OpenAPI 为准，本轮只验证标题关键词。

### MangaUpdates：漫画及同人分类补充

- [官方 API / AUP](https://api.mangaupdates.com/) 与 [OpenAPI](https://api.mangaupdates.com/openapi.yaml) 提供 `POST /v1/series/search`，书目有别名 associated、作者/画师角色、genres、categories 等；检索支持命中别名（hit_title），有 Doujinshi 类型及 genre/exclude_genre 参数，另有作者搜索和关联作品接口。
- 匿名非露骨书目 `Given` 检索成功（HTTP 200），观察到 Shounen Ai / Mature 分类。分类存在不代表其与本项目 BL、成人年龄分级完全同义。
- 同人志、短篇和系列仍需核对具体记录；不能把作品关联、出版信息或总卷数直接转换成本地当前文件卷号。实测请求 perpage=3 仍返回 25 条，客户端必须独立限制候选数量与响应大小。AUP 要求署名、缓存及合理请求间隔，未找到明确数值限额；不把公开接口当作无限制数据授权。

### Bangumi：中文名与中文标签补充

- [官方 API 文档](https://bangumi.github.io/api/) 与 [官方 API 仓库](https://github.com/bangumi/api) 提供 v0 条目检索、人物及关联作品能力。关键字、书籍类型、标签与 NSFW 条件可用于缩小候选。
- 匿名使用 `ギヴン` 检索成功（HTTP 200），返回中文名、BL 标签，`nsfw=false`。这证明同性题材与成人标记需分开处理。
- 成人条目存在权限要求；不能把匿名搜索不返回解释成“没有此作品”。文档说明中的旧 include 字符串与当前 nsfw 布尔字段存在差异，实施必须固定当前 schema 并用授权查询验证，不能照抄旧示例。
- 个人 access token 仅在服务端私密保存，不进入图片识别流程或前端；本轮未申请/读取用户 token，未验证 R18 权限与命中率。
- [当前 v0 schema](https://github.com/bangumi/api/blob/master/open-api/v0.yaml) 将该搜索标为实验性；[UA 规范](https://github.com/bangumi/api/blob/master/docs-raw/user%20agent.md) 要求应用和开发者标识。[版权与开发者协议](https://bgm.tv/about/copyright) 包含条目/角色信息 CC BY-SA 3.0 及其他素材、开发者使用条件，不能将整份响应无差别视为自由素材。搜索会同时返回系列和单卷，详情 series 字段须参与层级核对。

### AniList：多语种作品与作者别名补充

- [官方文档](https://docs.anilist.co/) 的 GraphQL 支持漫画关键词、原文/英文/罗马字名、synonyms、作者角色与别名，以及 tags / isAdult。
- 非露骨书目 `Given` 的匿名查询 HTTP 200，返回 Boys' Love 标签且 `isAdult=false`；本轮响应显示限额 30 次/分钟，不能只照抄常见的历史限额。
- [API 使用条款](https://docs.anilist.co/guide/terms-of-use) 对数据收集/存储和产品用途有限制。接入与长期写入本地库需先核对适用条件，不默认成为本项目长期持久化的主源。
- staff 也可能包含译者、排字人员，需按 role 筛选作者/原作/作画。[速率文档](https://docs.anilist.co/guide/rate-limiting) 的正常 90/min 与当前临时 30/min 不同，运行时以响应头为准。MediaSource 的 DOUJINSHI 表示作品来源，不能直接等同 MangaUpdates 的作品类型。

### 其他正式书目接口

| 来源 | 能力 | 本项目边界 |
| --- | --- | --- |
| [NDL Search](https://ndlsearch.ndl.go.jp/help/api) | [SRU / OpenSearch](https://ndlsearch.ndl.go.jp/help/api/specifications)，XML/DC-NDL；日本出版物书目 | [许可按提供方区分](https://ndlsearch.ndl.go.jp/help/api/provider)，个人非营利通常无需申请，须署名且控制并发；同人志/男同向样例覆盖未测 |
| [Google Books API](https://developers.google.com/books/docs/v1/using) | `intitle:`、`inauthor:`、`isbn:`，书名、作者、出版信息、ISBN；每页最多 40 条 | 公开检索不需用户 OAuth，但官方要求项目标识/API key；适合出版物核对，非 ISBN 同人覆盖未测。受 [Books 条款](https://developers.google.com/books/terms) 约束，不把文档的 CC BY 当成书目许可 |
| [MangaDex API](https://api.mangadex.org/docs/) | [官方 OpenAPI](https://api.mangadex.org/docs/static/api.yaml) 的作品 title、作者/画师 UUID、标签检索，多语言标题 | 作品级信息，无独立 ISBN 字段，lastVolume 不能填当前卷号；[请求限制](https://api.mangadex.org/docs/2-limitations/) 与署名/商业限制适用。具体同人覆盖未测 |

另核对 BL 专门书目站的可接入性时遇到访问验证页，未核实正式 API 或采集授权；不把“网站可浏览”写成“有稳定免费 API”，不把绕过验证作为接入方案。

## 可复用的开源工具：Komf

[Snd-R/komf](https://github.com/Snd-R/komf) 是 Komga/Kavita 元数据工具，代码 MIT。核对 revision `d8a34e9df29ddaa6941c302df216812ee6d525e9` 的 [README](https://github.com/Snd-R/komf/blob/d8a34e9df29ddaa6941c302df216812ee6d525e9/README.md) 与 LICENSE：

- 已有 MangaUpdates、AniList、MangaDex、Bangumi、MangaBaka 等 provider；支持配置优先级和字段范围。
- `GET /{media-server}/search?name=...` 提供多来源搜索；`POST /{media-server}/identify` 会设置书库元数据，两者不能混为只读预览。
- 可写服务 API 或 ComicInfo，具备按需识别能力；引入它意味着维护额外 JVM/Docker 服务及与媒体库的连接。
- 本项目的下载前可扩展元数据预填仍需设计候选详情和映射，不能只调用 identify 就声称已完成本项目录入流程。
- Komf 的 MIT 仅约束其代码，不替代每个数据来源的条款。首版可以参考适配逻辑，只接少量稳定 API；是否独立部署尚未决定。

## 搜索与映射方案

1. 用户手工输入标题/作者，或采用 OCR 提取的少量关键词，确认来源后再搜索；只有查询词外发。接口不支持作者全文搜索时可先查作者实体 ID，再按其作品关联查找，不能假装都是一个 q 参数。
2. 显示作品名/别名、作者角色、语言、出版形式、分级、来源和来源 ID。用户选择后再取详情，逐字段与当前草稿比对，不自动选择第一条或自动混合相似书名。
3. 不同源的记录保持独立，只有明确跨源 ID 或用户确认才认定同作；译本、合辑、单行本、短篇及同人续作不能因相似标题就合并。外部简介按源保留，不交给 AI 生成新剧情。
4. `author` 只接已确认创作者；社团、汉化/翻译组另保留来源证据。`series_name` 和 `series_number` 需确认层级，不能把总卷数、最新卷号或条目 ID 填到当前卷号。
5. 使用父 design 的版本化字段注册表和 metadata_document，原七字段经 legacy adapter 兼容；分类、来源 ID、ISBN、年龄分级及自定义字段遵循 registry 映射，不把扩展值私塞到旧文本字段。
6. BL/Boys' Love、Yaoi、Shounen Ai、gay manga/geikomi、bara 等按各源语义保留并提供搜索别名；不无条件互相替换。与“同人志/商业出版”“年龄分级/未知”分开表达，不推断作者或用户的性取向。
7. 搜索请求绑定输入/来源版本，采用字段遵循多图 OCR 的 dirty/revision 保护；用户改关键词、来源或页面离开后旧响应失效。无结果、缺权限、过滤、429/超时和解析失败保留草稿，不阻塞下载。

现有接入证据：`internal/app/tasks/metadata.go:7` 统一清洗七字段；`internal/worker/taskcore/downloader.go:225` 附近负责映射 ComicInfo 的 Writer/Series/Number/Title/Summary/Tags/Genre。外部来源不能把匹配规则散落到下载 worker。

## 尚需验收

- 当前仅证明公开接口和少量非露骨样例可检索；未验证用户实际收藏、R18 条目权限或冷门男同同人志命中率。
- 选定少量来源后，用用户授权的脱敏标题分别评估普通漫画、BL、男同向漫画、同人志；记录“查到同作/查到同系列不同版本/没有结果”，不能只统计 HTTP 200。
- 验证中文译名/原文/作者别名与繁简、全半角变体，保持查询次数和响应大小有界；不自动把全部 OCR 原文当搜索词。
- 核对所选字段的保存、署名和导出条件，验证匹配确认、未覆盖手工值、未知分级、系列卷号以及失败恢复。
