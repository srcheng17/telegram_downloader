# Batch 2 OCR / 规则前端 / AI 提取模块交付

本报告只覆盖本 implementer 的独占路径；根集成者拥有规则 Go 服务/仓储/HTTP、sourcesettings/modelapi 适配、路由、主页/设置接线、依赖锁、模板、dist 和部署。没有 commit、push 或部署。

## 已实现

- `frontend/src/ocr/images.js`：压缩文件最多 10 MiB；解码前解析 PNG/JPEG/WebP 容器、尺寸和动画标志，拒绝 MIME/扩展名不符、动画/多图片、损坏边界、超过 1200 万像素或 8192px；解码后核对尺寸并关闭 ImageBitmap。
- `queue.js` / `recognizer.js`：最多 10 图/50 MiB、一个同源 Tesseract worker 串行；稳定 image_id/generation；逐图 raw/edit 分离，保留校对文字；重排、取消、重试、移除；实际终止正在初始化或识别的 Worker，释放 object URL。
- `index.js`：专用截图粘贴区和多选；普通文本粘贴不拦截；逐图校对、完整原文对照、合并预览、明确完成子集、重复文字提示、补充手工文字。本地规则与 AI 均只生成候选；采用交给共享 draft/editor。
- `rules.js` / `settings/extraction_rules.js`：有限 label_value / continuation / hashtag_list；按字段类型解析；无用户正则/eval；可保存的字段/标签/模式/分隔选项与本机预览。规则版本/定义变更使旧建议过期。不同字段同归一标签拒绝；未知/无效值跳过并提示；冲突值分别展示，不静默抢占。
- `internal/app/metadataextract/`：只接收用户确认的文字/所选字段/版本基线；动态 typed JSON Schema；未验证完整预算即拒绝发送；输出值必须有原文直接证据；拒绝额外字段、非法类型、伪造证据、清空、部分响应；返回 canonical MetadataCandidate，证据仅存 UTF-16 位置/非秘密引用。推理后重查配置与定义，拒绝在途过期结果。
- `internal/httpapi/metadata_extract*`：受共享 admin/CSRF 中间件保护的接线函数；128 KiB 请求体/64 KiB 文字边界；拒绝缺失/null/重复/额外成员；no-store；错误类别与中文提示，不记录/回显上游正文。
- `web/static/ocr/`：Tesseract.js/core 7.0.0，四语言 @tesseract.js-data 1.0.0/4.0.0_best_int，15 文件合计约 28.8 MB，SHA-256/source/license 清单与许可证。无 CDN fallback，vendor IndexedDB cache 禁用；静态公共资源可走 HTTP cache。

## 集成接口

```js
mountOCR(root, {
  draft, api, schema,
  onCandidates,       // 用户选候选时 [candidate]；失效时 []
  onInputChange,      // root 唯一协调器递增 inputRevision
  setConfigRevision,  // authoritative AI config_version
}) // => { queue, reloadSettings, isDirty, dispose }

createExtractionRulesModule({ doc, api }) // => { mount, unmount }
// data-settings-slot="extraction-rules" 或 data-module-slot="extraction-rules"
```

Go：`metadataextract.NewService(registry, AIClient)`；
`httpapi.RegisterMetadataExtract(router, service)`。`types.go` 固定 AIClient 的
Snapshot/ExtractJSON 端口，内部 ModelSnapshot.Handle 仅归配置 adapter 管理。
AI JSON 输出格式为 `{fields:{<key>:{value:<typed>,evidence_quote:<exact quote>}}}`。
证据偏移统一为 UTF-16 code unit，客户端可以用 `slice(start,end)` 核对。

## 已运行验证

- 新增前端专属测试 15 项：图片校验、队列串行/取消/重试/旧回调、完成子集与顺序、规则有限解析/冲突/类型/过期、发送确认/发送副本/迟到响应、规则保存 CAS 和卸载取消。
- `npm run test:frontend`：当时全量 141/141 通过。
- `npm run lint`：通过。
- `go test -race ./internal/app/metadataextract ./internal/httpapi -run 'Test(Extract|Reject|EmptyResult|MetadataExtract|CustomFieldsAndConfiguration)' -count=1`：通过。
- `go vet ./internal/app/metadataextract ./internal/httpapi`：通过。
- `python3 scripts/verify_ocr_resources.py`：15 资源哈希/大小/归属全部通过。
- `git diff --check`：通过。
- `node scripts/check_ocr_browser.mjs`：真实 Chromium/Worker/WASM 四语言、缺资源失败、零外域请求、已缓存后离线识别通过。脚本生成的中性 fixture 位于 `tests/e2e/fixtures/ocr/`。完整合成文字与差异见 `browser-ocr-report.json`。

| 语言 | 耗时 | 忽略空白后的字符编辑距离 |
| --- | ---: | ---: |
| eng | 195 ms | 0 / 43 |
| chi_sim | 191 ms | 3 / 29 |
| chi_tra | 185 ms | 3 / 29 |
| jpn | 194 ms | 2 / 28 |

中文/日文输出会插入空格，并把全角冒号转为半角；raw/edit 保留这种实际差异，不能称作全部准确。以上是干净合成图、桌面 Chromium 的局部证据，未使用用户截图。

## 明确剩余验收

- 根集成者的完整 Go/数据库/生产镜像/主页浏览器回归、dist 重建尚不由本报告代替。
- 已实现的 AI 单元测试使用受控 adapter；不代表真实 MiniCPM/受保护目的地连接成功，也不代表完整 chat template/tokenizer/schema 预算能力已验证。未证实的模型路径必须继续拒绝发送。
- 本报告没有测移动端峰值内存、长图/低清/复杂背景准确度或真实用户截图；不得用合成样例成绩声称这些已通过。
- 默认保留所有重复/重叠文字，仅提示；当前没有自动去重或自动分块/重试/换模型。
