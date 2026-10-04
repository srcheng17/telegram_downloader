# 漫画元数据封装：InkTag 与成熟项目对照

> 范围更新：用户已明确元数据可扩展、来源及授权可配置；当前契约以 [父设计](../design.md) 为准。研究中的七字段实测仍只代表历史样例，不是扩展字段验收。

调研日期：2026-10-04。状态：planning。只读审阅仓库及执行合成 XML schema 校验；未运行第三方应用、安装依赖、处理用户漫画或修改产品代码。

## 建议

继续输出 **CBZ（ZIP）＋根目录唯一的 UTF-8 `ComicInfo.xml`**。以 anansi-project 的 ComicInfo schema 为字段依据、ComicTagger 为元数据读写参考、Komga/Kavita 为消费者兼容性目标；InkTag 用作字段编辑、预览和补空缺交互参考。保留现有 Go 实现，不为格式写入引入 .NET/JVM 运行时。

当前已有 Tags，建议采用固定的 **ComicInfo 2.1 draft 兼容配置**并验证目标消费者；不能声称含 Tags 的文件严格符合 2.0。严格 2.0 是另一个可验证的核心字段配置，不能无提示丢掉 Tags 来伪造兼容。版本选择不等于增加多个用户必须理解的格式开关。

## 热度与项目职责

本轮 GitHub 公开 API 快照见 [仓库证据](metadata-reference-repos.json)。星数是 2026-10-04 时点数据，不能替代正确性验证；最后 push 也不等同 release 时间。

| 项目 | 本轮 stars | 许可证 | 本任务参考价值 |
| --- | ---: | --- | --- |
| [InkTag](https://github.com/aurora7795/InkTag) | 8 | MIT | 元数据编辑/预览/补空缺交互；写包实现需避开下述风险 |
| [ComicTagger](https://github.com/comictagger/comictagger) | 837 | Apache-2.0 | 漫画归档和多元数据格式读写，较成熟的专门工具 |
| [ComicInfo](https://github.com/anansi-project/comicinfo) | 240 | MIT | 字段、枚举、顺序及稳定/草案版本的规范来源，不是应用 |
| [Kavita](https://github.com/Kareadita/Kavita) | 11,798 | GPL-3.0 | 实际阅读器消费、分组及字段兼容性 |
| [Komf](https://github.com/Snd-R/komf) | 715 | MIT | 多来源书目匹配和写入流程，见 [来源调研](public-metadata-sources.md) |
| [Komga](https://github.com/gotson/komga) | 本轮未取到 | 未据失败响应判定 | 本项目现有对接目标，消费者兼容性优先；本轮仓库 API 请求失败，不引用旧 star 数 |

参考架构不代表复制代码；若采用第三方实现，需遵循对应许可证及依赖许可。书目数据许可仍独立于工具代码许可。

## InkTag：可借鉴与不可照搬

固定 [commit c17ec6dc0763fc4a5f54af3a6355f55511025139](https://github.com/aurora7795/InkTag/commit/c17ec6dc0763fc4a5f54af3a6355f55511025139)，2026-09-15，Core 0.13.1 / net10.0。

- [README](https://github.com/aurora7795/InkTag/blob/c17ec6dc0763fc4a5f54af3a6355f55511025139/README.md#L9) 与实现均采用 CBZ 根目录 ComicInfo；CBR 转为 CBZ。
- [字段模型](https://github.com/aurora7795/InkTag/blob/c17ec6dc0763fc4a5f54af3a6355f55511025139/src/InkTag.Core/ComicInfo.cs#L19) 包含作者角色、出版/日期、语言、分级、页信息和 Tags。现有七字段可作为基础表单，额外标准字段可供导入/来源候选保留，不必把全部字段都变成必填表单。
- [读取入口](https://github.com/aurora7795/InkTag/blob/c17ec6dc0763fc4a5f54af3a6355f55511025139/src/InkTag.Core/ComicArchiveHandler.cs#L61) 优先 ComicInfo，旧 ComicBookInfo JSON comment 补空缺；可参考“已有值/补空缺/用户覆盖”的区分。
- [MetadataBackupService](https://github.com/aurora7795/InkTag/blob/c17ec6dc0763fc4a5f54af3a6355f55511025139/src/InkTag.Core/Backup/MetadataBackupService.cs#L86) 备份元数据而非完整归档；备份失败可继续，不能把此行为等同于无损保证。

静态源码发现的限制（本轮未运行 InkTag 复现）：

1. [ArchiveSwapService:61](https://github.com/aurora7795/InkTag/blob/c17ec6dc0763fc4a5f54af3a6355f55511025139/src/InkTag.Core/ArchiveSwapService.cs#L61) 使用 `ExtractFullPath=false, Overwrite=true`，展平目录会导致 `chapter1/001.jpg` 与 `chapter2/001.jpg` 同名覆盖风险。
2. [重打包遍历:172](https://github.com/aurora7795/InkTag/blob/c17ec6dc0763fc4a5f54af3a6355f55511025139/src/InkTag.Core/ArchiveSwapService.cs#L172) 未显式排序；读取时 basename 排序也不能完整保持目录级页面身份。
3. [交换与清理:203](https://github.com/aurora7795/InkTag/blob/c17ec6dc0763fc4a5f54af3a6355f55511025139/src/InkTag.Core/ArchiveSwapService.cs#L203) 是多步 atomic-like swap，成功删除 `.bak`；CBR 转换也删除原件。[直接 XML 更新:346](https://github.com/aurora7795/InkTag/blob/c17ec6dc0763fc4a5f54af3a6355f55511025139/src/InkTag.Core/ArchiveSwapService.cs#L346) 没有对应失败回滚 catch。不能根据 README 的 atomic wording 宣称跨文件系统或崩溃安全。
4. [XML 序列化:45](https://github.com/aurora7795/InkTag/blob/c17ec6dc0763fc4a5f54af3a6355f55511025139/src/InkTag.Core/ComicInfoXmlSanitizer.cs#L45) 经固定字段模型往返，未保留未知扩展。随附 XSD 与 Tags 模型不一致，校验仅告警；归档非空/至少一个 entry 不足以证明页数和图片内容未变。

## 格式版本与消费者

规范固定 [anansi commit 99e1453a163c777b4b5320a68732f6f133ac7918](https://github.com/anansi-project/comicinfo/tree/99e1453a163c777b4b5320a68732f6f133ac7918)：

- [稳定 2.0 XSD](https://github.com/anansi-project/comicinfo/blob/99e1453a163c777b4b5320a68732f6f133ac7918/schema/v2.0/ComicInfo.xsd) 没有 Tags、Translator、GTIN。
- [2.1 draft XSD](https://github.com/anansi-project/comicinfo/blob/99e1453a163c777b4b5320a68732f6f133ac7918/drafts/v2.1/ComicInfo.xsd) 包含这些扩展。两版都是 `xs:sequence`，元素顺序有约束；并非 Go XML 能反序列化就满足 XSD。
- 调研的 Komga 1.28.1 和 Kavita v0.9.1.4 均读取 Tags/Translator/GTIN，但消费者只支持其中部分语义，不能反向证明文件 schema 合规。GTIN 在这些消费者中主要用于有效 ISBN；不能填任意书目站 ID。
- Komga 会将 Tags 转为小写，Genre 用于系列、Web 按空格拆分；Kavita 的 Web 注释采用逗号分隔，故首版 Web 优先单个公开规范 URL。Kavita 优先根目录 ComicInfo，但有非根回退并会清除空元素。默认写根目录唯一文件最明确；保留原标签大小写的来源证据不能只依赖下游书库。
- ComicBookInfo 的 ZIP comment JSON 是可研究的旧格式导入来源，不作为默认导出；MetronInfo 等替代格式若未证明目标消费者兼容，不与 ComicInfo 同时生成多份可互相冲突的元数据。
- ComicInfo 没有通用 Source 字段；公开引用放 Web，私有下载/解析来源放应用数据库。Manga 的 YesAndRightToLeft 表示右向左阅读，AgeRating 应采用合法枚举（如 Adults Only 18+），不能直接写任意 R18 字符串；未知数值/枚举宜省略。

固定消费者及成熟写入工具证据：

- Komga 1.28.1：[DTO](https://github.com/gotson/komga/blob/1.28.1/komga/src/main/kotlin/org/gotson/komga/infrastructure/metadata/comicrack/dto/ComicInfo.kt)、[实际读取/映射](https://github.com/gotson/komga/blob/1.28.1/komga/src/main/kotlin/org/gotson/komga/infrastructure/metadata/comicrack/ComicInfoProvider.kt)。
- Kavita v0.9.1.4：[模型](https://github.com/Kareadita/Kavita/blob/v0.9.1.4/Kavita.Models/Metadata/ComicInfo.cs)、[字段转换](https://github.com/Kareadita/Kavita/blob/v0.9.1.4/Kavita.Services/Extensions/ComicInfoExtensions.cs)、[归档读取](https://github.com/Kareadita/Kavita/blob/v0.9.1.4/Kavita.Services/ArchiveService.cs)。这些是调研版本，不代表用户当前部署版本。
- ComicTagger 开发快照 `fa11a3456cbe5aa9f54f47bdc374ba26b2b946f3`：[读取已有 XML 后更新](https://github.com/comictagger/comictagger/blob/fa11a3456cbe5aa9f54f47bdc374ba26b2b946f3/comicapi/tags/comicrack.py)、[ZIP 重写保留 comment](https://github.com/comictagger/comictagger/blob/fa11a3456cbe5aa9f54f47bdc374ba26b2b946f3/comicapi/archivers/zip.py)。可参考保留策略，仍不能无条件承诺未知 XML 无损或严格 schema 合规。
- ComicTagger 1.5.5：[ComicBookInfo/1.0 JSON](https://github.com/comictagger/comictagger/blob/1.5.5/comicapi/comicbookinfo.py)、[写入 ZIP comment](https://github.com/comictagger/comictagger/blob/1.5.5/comicapi/comicarchive.py)。这与必须存在 ComicInfo.json 是两回事。
- [MetronInfo 固定提交的采用名单及 XSD 1.1 要求](https://github.com/Metron-Project/metroninfo/blob/77b9fdbe489568b9e047206005e7f6b0700bb3f0/README.md)：名单未列 Komga/Kavita，不能推断目标阅读器支持。

### 合成 XML 校验

使用 `xmllint --nonet --schema` 对两个固定 XSD 执行共 6 组校验，[结果及 schema SHA-256](comicinfo-schema-probe.json)。样例按当前 Go struct 的字段与顺序构造，未执行或修改产品函数：

| 合成输出 | 2.0 | 2.1 draft |
| --- | --- | --- |
| 当前顺序 Writer → Series → Number → Title → Summary → Tags → Genre | 不通过 | 不通过 |
| 按 schema 排序，保留 Tags | 不通过（Tags 不在 2.0） | 通过 |
| 按 schema 排序，仅核心字段 | 通过 | 通过 |

由此可明确需要规范化写入顺序，并分别验证核心配置与 Tags 扩展。此结果不表示现有 Komga 一定拒绝读取，也不是实际阅读器往返验收。

## 本项目当前代码与缺口

- `internal/downloader/cbz_writer.go:18` / `:33` 仅定义七字段，`:44` 生成 XML。`cbz_writer_test.go:14` 验证 ZIP 内容和反序列化，不检验 XSD。
- `cbz_writer.go:82` 在目标目录创建临时包，关闭 ZIP 和文件后于 `:126` rename；已有取消检查与临时文件清理，应保留。不能将此提升为已验证掉电恢复或完整文件系统事务。
- `internal/archive/extractor.go:105` 只收集图片，`:116` 自然排序；`internal/worker/taskcore/downloader.go:185` 只接收图片，元数据来自任务输入，没有读取原包 ComicInfo 的路径。已有归档中的出版/分级等信息不会自动保留。
- `internal/downloader/downloader.go:281` 按图片列表重新命名并打包新文件。当前是图片归档转换流程，不是原归档的无损就地元数据编辑器；不能把两者混称。
- `web/templates/index.html:79`、`internal/httpapi/taskcore_handlers.go:626` 与 `internal/archive/extractor.go:68` 当前入口只接受 zip/rar/7z；新增 cbz 输入需贯穿 UI、HTTP、保存扩展名和提取器。不能只改 accept 后宣称兼容。

## 首版封装契约建议

1. **明确产物**：新建标准 CBZ，根目录一个 `ComicInfo.xml`；图片保留原始字节，文件名使用固定宽度序号维持确定顺序，不能展平已有路径后产生同名覆盖。若今后做原包纯元数据更新，保留原 entry 路径及未修改条目，另定兼容契约。
2. **先读取再合并**：读取已有 ComicInfo 与来源候选，用户确认值优先。未提供字段保留，用户明确清空是独立动作；外部来源与 AI 无权覆盖手工修改。不支持/有歧义的旧字段需提示并保留原件或原元数据副本，不能静默丢失。
3. **保留标准信息**：现有七字段保持兼容，整体扩展为父 design 的注册字段模型；支持 PageCount、LanguageISO、Manga、AgeRating、Writer/Penciller/Translator、Publisher、Web、GTIN 等可选字段。PageCount 基于实际图片生成；页索引随顺序核对。未知分级不补成全年龄；BL/男同题材写题材标签，与年龄分级分开。源站 ID 不是 ISBN，社团与出版社也不能互填。
4. **来源与隐私**：书目公共页面可映射 Web；详细 provider ID、字段来源、用户编辑轨迹默认保留在项目内部。API key、Telegram session、下载令牌、截图及原始 OCR 均不装入 CBZ。首版不另造私有 JSON 作为必需的阅读器格式。
5. **校验与原件保护**：有界读取 XML，处理重复/嵌套 ComicInfo、异常 XML 与不支持字段；写出后重新打开包，验证唯一元数据文件、图片数量/顺序/哈希、CRC、目标 XML 配置和业务字段。成功且用户确认原件策略前不删除原件；失败/取消不能留下半包或覆盖已成功产物。
6. **未知扩展诚实处理**：保存原始元数据与能识别的字段；若保留不在 schema 内的扩展，不能称该 XML 严格 XSD 合规。严格 schema 导出与无损保留非标准扩展存在取舍，具体方案须在设计中写清；不得通过忽略校验告警宣称两者都已实现。

## 后续验收

- 合成 CBZ：中文、XML 转义、空值/主动清空、标签/类型分离、来源链接、未知分级、作者/画师/译者角色；分别校验所用 schema 配置。
- 导入已有 XML 后再次导出，未编辑标准字段保持；未知扩展、多个 ComicInfo、根目录与子目录同名元数据给出明确处理结果。
- 同名不同目录图片、1/2/10 页序、中文名、损坏 ZIP、重复 entry、取消、写盘失败：图数/图片字节/顺序不变，原件和已完成文件不损坏。
- 在隔离 Komga 和 Kavita 中实测读取代表性产物，核对 series/number、标签、作者角色、年龄分级和来源；仅 XSD 通过或单元测试反序列化不能替代此验证。

本轮只完成源码调研和合成 schema 校验，以上产品改动及阅读器往返测试仍待实施。
