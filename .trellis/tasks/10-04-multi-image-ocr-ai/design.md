> 执行更新（2026-10-05）：第二批已实现并完成本地集成检查，状态 review；实际外部验收仍未全部完成。结果以[第二批验收记录](../10-04-clipboard-ocr-tdl-ui/batch-2-verification.md)为准。下文的 planning/未执行描述保留为原计划背景。

# 多图识别与 AI 候选设计

## 1. 数据流与模块所有权

```text
专用粘贴/多选 → 图片校验与单worker队列 → 每图raw/edit text
  → 按用户顺序合并/明确选择完成子集
  → 本地有限规则 或 用户确认的AI发送快照
  → MetadataCandidate → 共享draft对照/采用 → 确认提交
```

| 所有者 | 范围 |
| --- | --- |
| 本子任务 | `frontend/src/ocr/`：图片入口/队列/worker封装/逐图编辑/合并/AI预览与API、规则解析及专属Node tests |
| 本子任务 | `frontend/src/settings/extraction_rules.js` 与专属API模块：独立规则编辑控件；不改共享settings入口或其他来源设置 |
| 本子任务 | `internal/app/metadataextract/`、`internal/httpapi/metadata_extract.go` 及测试：提示/schema、文本推理编排、候选证据验证 |
| 本子任务 | `internal/app/extractionrules/`、`internal/store/postgres/extractionrules/`、`internal/httpapi/extraction_rules.go`：有限规则类型/校验/版本/仓储；不反向依赖metadataextract或settings app |
| 本子任务 | OCR资源清单（版本/URL/hash/license/目标路径）与专属资源验证脚本/fixture；不拉取用户媒体 |
| metadata-contracts | FieldDefinition、版本化schema、MetadataCandidate、ApplyCandidate和legacy投影 |
| source-settings-auth | 管理员/CSRF、AI配置/密钥、模型发现及无敏感内容连接测试；拥有 `internal/modelapi/` |
| modern-ui-shell | 页面/设置插槽、通用元数据编辑器、共享draft状态；本模块只要求稳定挂载点 |
| root集成 | 路由/cmd/shared transport/app/page_modules、共享模板插槽、依赖锁/Vite/static打包/dist；迁移016中的独立非秘密规则记录、部署及模型私密网络 |

新包/文件名实施前按真实目录复核。不得为新功能搬移旧模块、修改Task Core状态或重复实现字段merge。每个批次交付后root接线和构建，不将共同依赖延迟到最后一批。其余agent并行改动必须保留。

## 2. 图片与 OCR 生命周期

采用已调研的Tesseract.js 7.0.0作为实施候选，实际依赖在能力检查时固定版本/hash/Apache-2.0及模型资源许可证，交root统一改依赖锁。worker、WASM/core和chi_sim/chi_tra/jpn/eng语言资源同源静态发布，明确workerPath/corePath/langPath；禁默认CDN和网络fallback。公共语言资源可按内容hash缓存，图片及识别文字不得进入该缓存。

首版接收静态PNG/JPEG/WebP，拒绝SVG、动画、多页文档及伪造MIME；先按受限header读取声明尺寸再解码，解码后再次核对实际尺寸，限制header扫描/文件字节，不能在检查前无界解码。统一常量：10张、10 MiB/图、50 MiB/组、12,000,000 pixels/图、8192px/边。超限拒绝该项并解释，不自动缩图损失文字；已接收项保留。

每图状态包括稳定image_id、content revision、ocr_generation、status、progress、raw_text、editable_text、edit_revision/dirty、error和预览引用。数组下标不是身份。一个worker串行处理，逐图解码/释放；语言切换影响下一轮识别，已有文字不覆盖。结果需同时匹配page generation/image_id/ocr_generation；删除或旧重试回调直接丢弃。

raw_text是本次成功识别的只读证据，editable_text才是合并来源。用户已编辑后重试，新的raw结果可供对照但不得自动替换editable_text；显式采用新识别文字才推进文本revision。识别运行中的新增项进入队尾；重排不改变在途图片身份，只推进input_revision并重算合并预览。单图取消/失败不影响其他完成项；取消队列时终止worker并重新创建供明确重试，不能假定Tesseract内部中止必然已释放资源。

## 3. 合并、规则与编辑保护

merged_preview只从选定顺序的逐图editable_text派生，保留段落边界和片段到image_id/text_revision/字符区间的映射。失败/未完成项显式列出；默认等全部成功，用户可以明确选已完成子集，该subset也进入input_revision。重复图仅提示；重叠行可预览但需用户确认才去除，不做模糊全局去重。

规则运行只产生共享MetadataCandidate。多值字段按registry类型有界处理；同标签不同作者/标题/序号保持冲突，作者/社团/汉化组角色不靠猜测；简介沿原文顺序保留，不润色。原文没有的字段省略，不产生clear建议，不计算page_count。规则候选origin=rule，provenance引用image_id/文本revision/范围和rules_version，不复制全文。

共享草稿拥有唯一metadata_document：schema_version、definitions_version、document/field revision、manual_locked和value/cleared/absent语义均由metadata-contracts管理。本模块记录请求基线并调用共享ApplyCandidate；不能复制一套dirty覆盖逻辑。选择字段中任一revision冲突整次采用失败。手工clear仍受保护，候选即便值相同也不得解锁或重置用户revision。

## 4. 可保存规则契约

`ExtractionRuleSet={rules_version:uint64, definitions_version:string, rules:Rule[]}`。`Rule={id,target_key,labels:string[],mode,label_value_options?}`；mode限定label_value、continuation、hashtag_list，options只允许固定分隔选项（逗号/中文逗号/分号/换行）与有限大小写/空白归一化。无正则源、JS、eval、模板执行、任意URL/请求头。

target_key必须来自指定版本registry且允许rule来源；continuation只用于兼容string字段，hashtag_list只用于string[]。label_value的数字/布尔/日期/标识符用已定义的有限解析器；不支持的类型组合在保存时拒绝，不把原文硬塞进对象字段。相同归一化标签指向多个不同字段时报配置冲突，不通过规则顺序暗中抢占。初始规则JSON上限64 KiB、128条、每条最多8标签、单标签128 UTF-8 bytes；统一计数并拒绝超限，不截断。

- `GET /api/settings/extraction-rules` 返回当前rules_version/definitions_version/rules及校验警告；需管理员，无秘密。
- `PUT /api/settings/extraction-rules` 输入 `{expected_version,definitions_version,rules}`；admin+CSRF，成功原子保存并递增rules_version。409版本冲突、400结构/类型/越界，不覆盖其他设置。
- 由独立extractionrules service/repository拥有记录，root在预留migration016中增加独立非秘密记录并接线；不塞入旧app_settings四字段、不和source_settings共享secret或反向循环依赖。
- 首版设置控件只提供target字段、标签chips、有限模式/分隔选项、示例预览、保存；预览文本只在内存。注册表版本变化后重新校验当前规则，失效规则可见且不运行；历史规则版本引用留在provenance中，不改已创建任务快照。

## 5. AI 请求与共享模型服务

`POST /api/metadata/extract` 输入：

```text
request_id, text, field_keys,
schema_version, definitions_version,
base_document_revision, field_revisions, input_revision,
config_revision, rules_version?  # rules_version仅追溯，不由AI执行规则
```

不接收base_url/model_id/api_key/arbitrary headers/截图/整份document。服务端读取一次已保存AI配置快照，核对config_revision=source-settings权威config_version投影、enabled和选定模型能力；使用准确model_id。接口使用共享管理员/CSRF，JSON body有界（建议128 KiB，text最多64 KiB UTF-8，精确prompt token预算另算），返回标准`{request_id,candidates:MetadataCandidate[],warnings}`。即使服务端返回成功，页面也必须检查当前全部基线后才展示可采用候选。

本任务声明最小AIClient调用需求：`Snapshot(ctx,expectedConfigVersion)`、`ExtractJSON(ctx,snapshot,prompt,schema,outputBudget)`，以及仅已知MiniCPM/llama.cpp能力可用的`EstimateRenderedTokens`；这些是内部端口，不是新增公开任意代理接口。实现/连接复用settings拥有的modelapi目的地/凭据/禁止重定向策略，扩展该包由root与其owner协调，本任务不抢写。模型发现和固定无敏感测试仍完全归source-settings-auth。

用户选定本次field_keys后，服务端按registry筛选允许AI提取的字段，生成typed JSON Schema及有限字段说明；不把全registry和全部custom定义塞进4096上下文。custom.user.*必须已注册并允许AI，不能由OCR文本/模型新建；page_count等派生字段排除。上游输出是受限提取数据，再由服务端包装唯一MetadataCandidate，不让模型生成revision、manual_locked、来源ID、请求目标或确认事实。

每个建议值需有输入中的直接文字证据。可要求模型返回短evidence_quote并在服务端进行精确子串定位及有限合法归一化验证，随后仅返回位置引用和warnings；quote不进入持久化metadata。找不到证据/角色无法确认时拒绝该次结构化结果或提示重新选择字段，不能将推测填为事实；summary必须来自选定文字的原段落而非新剧情。schema合法不代表语义正确，所有候选仍待用户确认。重复证据位置不冒充唯一定位。

输入原文用明确数据边界传入系统提示，不执行其中指令；请求不提供工具、function calling或外部URL能力。输出只接受本次选择的注册字段，拒绝额外key、错误类型/枚举/UTF-8/长度、非法数值、模型自建来源、伪造证据及清空建议。缺失值省略，不用null/空串冒充clear。任何上游截断、拒绝或不完整对象导致整次建议失败，不从半份JSON中抢救字段写入草稿。

## 6. 模型预算、取消与错误

已知MiniCPM当前上下文4096 token。能力checkpoint必须对实际聊天模板、系统提示、字段契约、文字、生成前缀和输出预留计算完整预算；仅私密已配置服务的受控tokenizer能力可用于精确计算，不公开为客户端任意调用入口。schema若进入prompt也计入；chat模板/服务版本变化使预算能力验证失效。输出预留按本次字段/实际文字验证，不沿用历史七字段512输出token作为保证。

如果已知MiniCPM缺少可验证预算能力，不能声称精确预检已通过；先实现该能力/取得服务明确超限拒绝且无自动截断的证据，再开放本路径。通用兼容服务无精确tokenizer时只作明确的字节/字段边界和保守提示，依赖其明确context超限响应；服务可能静默截断而无法禁用/证明时标不支持，不能悄悄发送完整文本后接受不完整解析。首版不自动分块、删字段、改模型、扩大服务ctx或选择另一供应商。

调用超时120s、响应最多1 MiB（复用modelapi设置约束）；每个工作区最多一个当前AI请求，新的显式请求取消旧请求并更换request_id。服务端传播context，用户cancel即终止本次处理，不自动重试；客户端AbortController加page/request/input/definitions/config/field revision共同防迟到。HTTP 200还需检查finish_reason/refusal和完整schema；finish_reason=length不能局部采用。错误类别独立包含disabled/not_configured、config_changed、schema_unsupported、input_too_large/context_exceeded、refused、unauthorized/forbidden、rate_limited、timeout、unreachable、invalid_response、cancelled，中文反馈不回显上游正文或密钥。

用户修改AI发送快照后，OCR更新只标记其过期并提供差异/明确重新生成，不覆盖该快照；发送快照只是一次提取的可编辑副本，不成为另一份合并文字真相。配置/字段定义变更使候选失效，但不改当前手工草稿。

## 7. 页面隐私、发布与恢复

所有截图、raw/edit/merged/send text只在页面内存。正文不进入请求日志/trace/APM、数据库、错误详情、URL/query、localStorage/sessionStorage/IndexedDB或漫画归档；静态OCR资源缓存与用户数据严格分开。provenance只存短非秘密标识、revision/范围，原截图未持久化时UI说明该证据仅本次会话可核对。

离页时先调用shell未保存保护；导航被取消不能unmount当前工作区。实际unmount时取消队列/AI，terminate worker、revokeObjectURL、关闭ImageBitmap、清理listeners/timers和引用，递增page generation。root为敏感工作区禁用htmx history snapshot；保留#content shell导航契约并测试cached/cache-miss restore，不以“不写localStorage”代替完整检查。退出登录使用共享session invalidation回调立即清除。

OCR能力/资源发布与界面接线由root在本批次完成，验证生产镜像而非仅Vite开发模式。停用AI仅禁建议，规则/手工及旧下载仍可用；OCR加载失败仍保留手工文字输入。回滚不删除已确认metadata_document或规则记录。RackNerd当前loopback部署不等于产品可达，专用受保护通道归父集成验收，本任务不改远程网络。
