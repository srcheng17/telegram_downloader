> 执行更新（2026-10-05）：第二批已实现并完成本地集成检查，状态 review；实际外部验收仍未全部完成。结果以[第二批验收记录](../10-04-clipboard-ocr-tdl-ui/batch-2-verification.md)为准。下文的 planning/未执行描述保留为原计划背景。

# ComicInfo 往返保留与归档封装

状态：planning；本轮仅规划，未运行产品测试。所属父任务：[截图识别、tdl 与现代 UI](../10-04-clipboard-ocr-tdl-ui/prd.md)。执行批次：Batch 2，依赖 `10-04-metadata-contracts` 的 v1 文档、明确清空和来源契约。

## 目标与范围

将当前“只提取图片、重建七字段 XML”的流程升级为“有界读取原元数据 → 合并用户确认字段 → 生成可验证 CBZ”，保留未编辑信息并明确报告无法导出的字段。

- 默认固定 ComicInfo 2.1 draft，保留 Tags；2.0 仅作离线兼容校验对照，首版不开放 2.0 导出，不静默删扩展。
- 扩展标准字段映射，并从原归档保留未编辑字段、原始元数据证据；可能冲突的旧格式保存在私密副本，非元数据 ZIP comment 可有界保留。
- 唯一根目录 UTF-8 ComicInfo.xml；自然页序与图片字节不变，处理同目录/跨目录同名图片、Pages 映射和实际 PageCount。
- 新增 CBZ 输入的提取能力；完整 UI/HTTP/worker 接线由 root 集成。

不含：无损原归档就地编辑、扫描整个书库、MetronInfo/ComicBookInfo 主动生成、采集源 HTTP、脚本插件、新阅读器服务生产部署。

## 验收标准

- [ ] 默认输出按固定 draft XSD 顺序通过真实 XSD 校验；2.0 对照准确报告扩展不兼容，不作为用户可选丢字段导出路径。
- [ ] 已有标准元数据的未编辑字段保持；missing 保留、explicit clear 清空；作者角色、GTIN、语言、分级及 Count/Volume/Number 不混淆；别名内部保留不伪造 XML 映射。
- [ ] 未知/冲突/损坏原元数据不会被静默丢弃；原始字节在受保护副本中保留且可恢复，输出警告明确哪些未导出。
- [ ] ComicBookInfo comment、MetronInfo 等竞争元数据在私密副本原样保留并提示不再主动写入新包；非元数据 ZIP comment 可有界保留。私有来源/凭据/OCR 文字不注入包内。
- [ ] 多目录同名、1/2/10、中文文件名、重复 entry 的策略确定；图片数量、顺序、内容哈希和 CRC 正确，无同名覆盖。
- [ ] Pages 的 Image 索引与最终页序一致；无法明确映射时报告并保留原数据，不能生成错误页面索引。
- [ ] 取消、损坏输入、写盘/校验失败不发布半包、不损坏原件或既有成功产物；保留现有临时文件加 rename 模式。
- [ ] CBZ 能经实际选择/上传或 tdl → worker → 打包链路处理；隔离 Komga/Kavita 实际导入结果与 XSD 检查分别记录。

详细策略见 [design.md](design.md)；范围与检查见 [implement.md](implement.md)。
