# Research: 识别链路与人工决策负担

- Query: 现有截图 OCR → 合并/规则 → AI → 书目源 → 字段采用为何容易误识别、遗漏字段并要求大量选择；可在保持草稿保护的前提下如何最小改进。
- Scope: internal；只读源码、规范及已有任务，未读取用户私有原图，未运行生产服务或修改配置。
- Date: 2026-10-07

## Findings

### 1. 当前链路事实

| 环节 | 当前行为及证据 | 对本次 Telegram 样式样例的影响 |
| --- | --- | --- |
| OCR 引擎 | Tesseract.js 7.0.0 同源 Worker；`frontend/src/ocr/recognizer.js:3`、`:41`。默认语言简中+英文，另支持繁中/日文；`frontend/src/ocr/queue.js:3`、`:7`。 | 默认语言方向合理，但不能因此保证暗色混排精度。 |
| OCR 输入与输出 | 原文件字节直接 recognize，initialize config/recognize options 都为空；输出仅 `text:true`，blocks/hocr/tsv 均关闭，最终只返回字符串；`frontend/src/ocr/recognizer.js:45`、`:47`–`:50`。 | 项目未配置专门的裁剪、放大、反色、版面模式，也没有保留区域坐标或置信度供后续筛选。Tesseract 内部仍有其默认图像处理，不能表述为引擎完全无预处理。 |
| CLI 对照 | CLI 同样调用 recognize(bytes, {}, …)，额外取 imageColor 只用于核对解码尺寸；`cli/ocr.mjs:116`–`:127`。 | CLI 实测可定位 OCR 问题，但浏览器 Worker/解码环境仍需独立验收，不能把 CLI 成功直接等同浏览器 UI 成功。 |
| 队列与合并 | 串行 OCR；成功结果是 rawText/text；按图片顺序加两个换行合并，只提示精确重复行，不去重；`frontend/src/ocr/queue.js:18`–`:34`、`:68`–`:81`。 | 不理解聊天气泡、文件卡、caption、评论栏。上方简介与下方完整标题都平铺为文本，下游失去视觉优先级。 |
| 规则匹配 | 匹配第一冒号前的整段文字和配置 labels，必须有冒号；`frontend/src/ocr/rules.js:63`–`:71`。hashtag_list 只在匹配标签后解析 `#`；`:47`。 | 裸书名、裸 hashtags、`[作者]标题[汉化组]` 不会因为规则模式名称而自动识别。若图片没有“作者：”，只增加“作者”标签也不能解决。 |
| 摘要延续 | continuation 从标签后持续吸收行，直到下一行匹配“任意短标签+冒号”；`:73`–`:79`。 | 无冒号的 hashtags、点赞、评论入口和下一条文件卡文字都可能进入 summary；时刻冒号又可能提前终止。必须分别验证，不能只说“摘要模型不好”。 |
| 规则候选 | 每一个不同字段值生成独立 candidate；同字段相同值跳过，不同值保留并告警；`:85`–`:93`。 | 即便多个字段无冲突，也需要逐候选进入对照。规则不是产生一份集中待核对稿。 |
| AI 默认字段 | UI 默认只选 title / creators.writer / summary / tags；`frontend/src/ocr/index.js:188`–`:189`。 | 英文别名、译者/汉化团体不会被默认请求。用户看到“没识别出来”不一定是模型漏读，也可能根本没选该字段。 |
| AI 请求 | 必须生成发送预览、勾选确认、点击发送；`:131`–`:146`、`:185`–`:191`。只把文字送服务，无图像。 | 现有 AI 无法从图像修复 OCR 错字或利用视觉布局；它只能处理保留下来的文本。 |
| AI 结果 | 一个多字段 candidate，所有返回字段需要类型正确、原文 evidence_quote 及 literal grounding；`internal/app/metadataextract/service.go:117`–`:153`。任一非法字段导致整份 invalid_response。 | 有意避免接受部分无效输出，但一个错误字段会让整个建议不可用，用户只能手动缩减字段/修正文案再试。 |
| 书目查询 | 关键词初始为空；所有来源 checkbox 初始未选；`frontend/src/metadata-search/index.js:171`–`:180`。选择来源后显式搜索，选记录后显式 resolve；`:125`–`:143`。 | 已开启服务仍需每次重复选择来源/输入检索词。OCR 标题没有直接成为初始检索词。 |
| 采用 | OCR候选先点“核对此候选”，再到元数据面板选择；`frontend/src/ocr/index.js:66`–`:71`。每个字段 checkbox 默认未选；人工保护确认 checkbox 无论是否存在保护字段均渲染；`frontend/src/shared/metadata/editor.js:194`–`:225`。 | 用户重复做“选择候选→选择字段→采用”决定；即使空稿且无冲突也一样。 |

### 2. AI 有证据不等于字段语义正确

- 固定 prompt 要求保留角色、摘要逐字、缺失不猜，但没有 Telegram 文件卡/caption/界面噪声的专门提示：`internal/app/metadataextract/service.go:93`。
- schema 的 description 只有字段 Label，未提供“角色配对不是作者/系列”“汉化组不是出版社”等角色判别说明：`:170`–`:206`，尤其 `:196`。
- `grounded` 明确说明只证明文字支持，不证明语义；字符串/数组主要判断值是否出现在 quote 中，summary 要求标准化后相等：`:209`–`:237`。因此“文本里存在一个人名”本身无法证明它是作者，也不能保证含 UI 文本的摘要语义合适。
- 重复 evidence 只定位第一次出现并告警：`:128`–`:141`。同一标题在上文和caption多次出现时，证据位置不一定对应最佳区域。
- OutputBudget 当前固定 1024：`internal/app/metadataextract/service.go:21`、`:94`。长剧情+大量字段+重复 evidence 可能触及输出预算；真实发生与否须主代理实际测量，不凭此样例推断。
- schema value-first 顺序是已有补丁，应保留：`:196`–`:204`；不可在本任务为了简化UI退回 quote-first 或削弱类型/证据校验。

### 2.1 主代理本图实测与 CJK 空白机制

主代理于本轮发回以下实测摘要，本研究未独立运行：原图OCR为660 UTF-8字节；忽略空白后，中文标题、英文别名、作者、汉化组四项标记均存在；标题含CJK字间空白（合成示例：`《星 河 旅 行 记 》`），标签呈 `剧情 介绍 :`；原图规则返回 **0 candidates / 0 warnings**。裁掉界面后的正文+caption为532字节，界面噪声已去除，但规则仍为 **0 candidates**。具体私有证据由主代理保存，不在本文件复制原文。

- `rules.js:11` 的 trimLabel 只裁首尾Unicode空白；`:12` 只处理 ASCII 大小写。`:66`–`:67` 对冒号前完整标签做精确比较，**不消除标签内部空白**。因此即使已配置 `剧情介绍`，也不会匹配 `剧情 介绍 `；首尾空白和半角/全角冒号本身可处理。
- 是否配置了 `剧情介绍` 是另一个独立前提；若标签列表仅有 `简介`/`剧情`，消除OCR空白后仍不匹配。没有读取本轮真实规则列表，不能直接认定是哪一个或两者同时发生。
- `rules.js:64` 要求冒号，裸 `《…》` 标题始终不匹配标签规则；字间空白并不是该标题无候选的唯一原因。
- 0 candidates / 0 warnings 是预期的“无标签命中”路径：warning 只在已匹配后的类型失败、冲突等情况产生（`:81`–`:89`）。UI目前不能区分“没读到文本”和“读到文本但没有任何规则覆盖”。
- AI grounding 的 normalization（`service.go:212`）仅把空白折叠为单空格，不把 CJK 字间空白删掉。若模型把合成示例 `星 河 旅…` 修复成自然中文连写，而 quote保留OCR字间空白，`:215` 的 substring 检查可能拒绝本来合理的修复；summary更要求 normalized value==quote（`:223`–`:225`）。这是需要实测的机制风险，不表示本次AI已失败。
- 最小方向：把OCR原文与规范化派生文本分开保存，针对CJK字间空白做受限、可追溯的处理并维护证据偏移；仅在中文字符之间去除识别插入空白，不能全局删空格破坏英文作者/标题。或者仅给规则标签匹配加入有限CJK空白容错，先解决标签覆盖；裸标题/caption仍需独立模式。不要直接放松所有AI grounding接受无证据改写。
- **本图没有产生摘要候选**，本文件其他段落关于摘要吸收hashtags/UI是源码推导的潜在缺陷与回归项，不是本图已经观察到的结果。裁剪后仍0候选说明本例规则失败不能仅靠去UI噪声解决。

### 3. 书目源没有跨源身份消歧或自动置信度

- 服务只对 1–3 个已选来源做有界并行，20秒总期限，各源分别返回最多 MaxResults：`internal/app/metadatasearch/service.go:56`–`:119`。
- adapter 直接把同一 keyword 发给三个固定搜索接口：`internal/infra/metadataproviders/adapters.go:91`–`:136`。这里没有标题/别名多策略搜索编排。
- UI 按来源分组显示，展示标题、别名、作者角色、系列/单卷层级，再让人选条目；`frontend/src/metadata-search/index.js:110`–`:130`。
- 本次冷门/同人作品是否被三源覆盖未知。无结果不能视为识别错误，也不能强迫用户选一个近似作品；既有错误文案已区分“未命中”和可见性未知（该文件 `:5`、`:117`）。
- 单纯“自动选首条”会减少点击但增加误配，不能作为推荐方案。标题一致只是证据之一，应结合作者、别名、作品/分卷关系并把冲突留到集中核对。

### 4. 人工选择多主要来自既有产品契约，而非单个按钮缺陷

当前规范要求：OCR完成不能自动调用AI；用户核对本次文本/目标/字段并确认；重复或冲突不能静默覆盖；采用时再验证版本、人工锁和字段修订。见 `.trellis/spec/frontend/candidate-adoption.md:60`–`:79` 及后续 adoption preflight 段。

相应实现会在 OCR/规则/AI 配置、输入等变化时清除候选并复位确认：`frontend/src/ocr/index.js:27`–`:31`；真正 apply 前重新获取 schema/settings，并检查原基线：`:49`–`:64`。这些失效保护应保留，重复交互的呈现方式可以改变。

旧任务明确把主动 AI/主动书目检索作为需求：

- `.trellis/tasks/10-04-multi-image-ocr-ai/prd.md` O8：不因 OCR完成、规则运行、书目搜索自动 AI；O9允许用户选择字段。task.json 当前 review。
- `.trellis/tasks/10-04-metadata-provider-search/prd.md` P2/P4/P6：显式查询、不能自动首条、逐字段采用；当前 review。
- `.trellis/tasks/10-04-media-workspace-integration/prd.md` 仍 planning；其页首明确 **2026-10-05 用户移除首页 Telegram 新任务入口，保留后端接口和连接设置**。不能把“UI没接 Telegram 下载”直接定性为漏实现，需要父任务核对本次是否重新引入。
- `.trellis/tasks/10-04-clipboard-ocr-tdl-ui/task.json`、`10-04-telegram-metadata-preview/task.json` 均 planning；这些任务不是完成验收的证明。

新任务应明确哪些旧交互需求被替代：例如“一次选择自动准备，随后集中核对结果”，而不是在实现时默默违反旧spec。用户当前诉求支持减少确认，但最终外发/提交的自动化边界仍应由主任务写清。

### 5. 最小改进建议（规划候选，尚未实施）

1. **先汇总结果，再集中核对**：规则、AI和来源候选按字段聚合到一份暂存建议。值相同的候选合并显示来源，不修改原始OCR；无冲突且当前字段为空的值默认选中；只把不同值、人工修改冲突、证据不足突出为待决定项。最终一次采用仍调用共享 draft 的验证逻辑，不能让OCR模块直接写字段。
2. **合理默认值替代重复配置**：初始语言继续简中+英文；默认字段覆盖常用标题/别名/作者/翻译/简介/标签，启用源按已保存优先级选中；字段/语言/来源放高级选项。不是“所有字段全部请求”：应评估完整schema+文本+输出预算。
3. **只在需要时展示保护确认**：人工保护替换仅在选择将覆盖保护字段时展示。空稿无冲突不显示额外确认；保留明确清空、手动编辑、版本变化的原子防护。
4. **Telegram 样式预处理先做有限实验**：对保留原图的临时副本比较原图、内容裁剪、放大、暗色归一化；同时保留块/行坐标与置信度作为诊断数据。选实际有效的最小处理，不直接加入多个引擎/云端OCR。OCR confidence 是信号，不能当书目字段正确概率。
5. **内容层分段，原文可追溯**：区分正文、完整caption、截断文件名、UI区；完整caption优先于带省略号文件名。规则增加经过测试的有限 caption 模式，并处理CJK标签内部空白及真实标签别名覆盖；不要用“任意方括号都是作者”或对正文全局删除数字/空白。摘要终止应依据段/气泡/已识别UI边界，保留原始文本与派生文本映射。
6. **AI 角色说明与可解释冲突**：为title/aliases/writer/translator等加入简短、具体的语义说明和中性示例；把不确定信息保留为空或集中告警，不能通过放松grounded让模型猜作者。不要让未选字段导致的缺失冒充模型识别失败。
7. **书目可选且可跳过**：用已提取标题/别名预填检索词，自动准备模式获明确授权后才自动外发。无命中继续原草稿；高置信候选也需要可见依据，模糊/同名/不同分卷集中询问。查询失败不阻断本地识别与提交。

### 6. 脱敏回归覆盖与分层指标

当前 OCR 浏览器验收使用白底、44px黑字、每语言3行合成canvas；`scripts/check_ocr_browser.mjs:24`–`:43`，计算忽略空白的字符编辑距离 `:48`–`:51`。脚本明确未覆盖用户真实截图/移动端 `:23`。现有规则测试重点是带冒号标签、类型、冲突及分隔符；`frontend/src/tests/ocr_rules.test.mjs:10`–`:32`。不能以这些测试证明聊天截图精度。

建议以虚构标题/作者/剧情生成以下回归图和文本，不把用户原图/露骨剧情加入Git：

- 暗色聊天白字+蓝色hashtags，中文标题、英文别名、方括号作者/汉化组、括号角色配对；上下两个气泡与ZIP文件卡。
- 中文字间插入空白、标签 `剧情 介绍 :` 对配置 `剧情介绍`、未配置别名与裸标题分别断言；英文人名空格不破坏；规范化前后evidence仍准确，缺规则覆盖与OCR无文本的反馈分开。
- 完整caption与截断文件名冲突；双语同一作品不是两个系列；角色配对不得进作者/系列；汉化组不得进出版社。
- 点赞、浏览、HH:MM、MB、评论入口均排除；保留剧情内合法数字/时间/括号，避免过度清洗。
- 摘要跨行/跨截图、重复重叠、无冒号页脚与下个标签；空规则/AI停用/来源没命中仍可完成草稿。
- 原图/裁剪/放大/归一化比较分别记录字符错误、字段准确/遗漏、噪声泄漏、时延；引用主代理真实实测结果而不据单图宣称总准确率。
- 规则+AI一致时一次核对；冲突只询问有分歧字段；返回上一步不丢人工修正；旧回调/旧候选不得覆盖；返回后已授权请求是否重发应有明确规则。
- 来源同名异作者、系列/分卷、匿名权限受限、单源超时；高相似度错误条目不得自动覆盖。
- 将“用户主动决策次数”与纯点击次数分开记录：素材选择、外发授权、冲突选择、最终提交。目标是减少必须理解技术参数的决定，而不是隐藏错误。

## Files Found

- `frontend/src/ocr/{recognizer,queue,images,index,rules}.js`：OCR入口、资源约束、队列、发送快照与有限规则。
- `cli/ocr.mjs`：CLI OCR及解码尺寸验证，可作为主代理实测入口差异参照。
- `internal/app/metadataextract/service.go`：版本化AI抽取、schema、输出预算、证据验证。
- `internal/modelapi/{extraction,extraction_chat}.go`：模型协议边界；本次不改协议。
- `frontend/src/shared/metadata/{editor,draft,schema}.js`：唯一草稿/采用/人工锁/字段定义边界。
- `frontend/src/metadata-search/index.js`、`internal/app/metadatasearch/service.go`、`internal/infra/metadataproviders/adapters.go`：书目查询与来源候选。
- `frontend/src/tests/ocr_{rules,module,queue,images}.test.mjs`、`metadata_{editor,search}.test.mjs`、`internal/app/metadataextract/service_test.go`：已有回归覆盖。
- `scripts/check_ocr_browser.mjs`：真实WASM合成图验收；现有覆盖局限已在脚本说明。

## External References

未新增联网外部研究。仓库规范固定 Tesseract.js/core 7.0.0、语言数据 `@tesseract.js-data/*@1.0.0/4.0.0_best_int`，源/许可/hash由 `web/static/ocr/manifest.json` 管理。模型协议/CPA版本边界以 `.trellis/spec/backend/extraction-protocol.md` 为准，本研究没有独立验证部署版本。

## Related Specs

- `.trellis/workflow.md`：本任务保持 planning；研究不等于实施授权。
- `.trellis/spec/frontend/candidate-adoption.md`：隐私、异步失效、显式采用与真实浏览器验收。
- `.trellis/spec/backend/extraction-protocol.md`：有限规则、Go/JS一致性、AI证据/版本/预算契约。
- `.trellis/spec/frontend/index.md`、`.trellis/spec/backend/index.md`：对应层的验证入口。
- `docs/development/module-boundaries.md`：业务语义不散落到handler或多个UI模块，不替换现有Task Core。

## Caveats / Not Found

- 本研究者没有运行测试、实测OCR、访问私有图像或验证生产配置。2.1节实测摘要来自主代理消息，私有逐层证据由主代理另存。
- 未发现当前浏览器OCR流程保留区域坐标/置信度或使用聊天版面识别；这不等于仓库所有依赖库没有相关能力。
- 未发现现有源查询流程做跨源身份评分、默认标题填入或自动采用；不能凭此推断未来一定需要复杂排序系统。
- 原图只是一例，应该用于定位回归和解释错误，不能给出总体准确率结论。
- 源码工作区有其他任务未提交变更，本研究只写本文件，不覆盖、提交或部署他人改动。
