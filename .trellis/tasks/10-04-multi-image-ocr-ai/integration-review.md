# Batch 2 集成审查与修复

日期：2026-10-05。范围：本地工作树内的模型协议、设置 adapter、规则服务/仓储/HTTP、候选采用与主页生命周期。先复现有意义的失败，再修改已协调的文件；未 commit、push、部署或访问生产模型。父会话负责共享入口和统一 dist 构建。

## 已修复问题

### P1：llama.cpp 真实返回与受控测试夹具不一致

固定上游源码 commit `11fe02151f79c41d0d4af7da708755d73b9c0da6` 的 native completion 返回 `generation_settings.n_predict/stream`，**不返回 `generation_settings.n_ctx`**。原实现按 README 示例强制要求 n_ctx，真实成功结果会全部被拒绝。另一个问题是 tokenizer 没有按实际聊天调用加入所需特殊 token，预算与真正使用的 prompt 可能不一致。

修复在 `internal/modelapi/extraction.go`：

- `/props` 读取有效上下文，并绑定 model alias/path、聊天模板、build fingerprint；只接受固定 commit 的合法 build 字符串和唯一可选模型，拒绝脏后缀、子串伪装及其他 commit。
- `/apply-template` 后使用 `add_special:true,parse_special:true` 计数；拒绝 null、负数和非 int32 token。将同一数字 token 数组直接发到 `/completion`，不会二次添加 BOS。
- 完整 rendered prompt（含 schema/模板/特殊 token）+ 输出预算 + 8 token 保守边界必须装入上下文；不截断、不分块、不换模型。
- 核对真实最终响应的 model、stop、truncated、stop_type、tokens_evaluated、tokens_predicted、n_predict 与 stream，推理后再次核对 `/props` fingerprint；不再要求不存在的 completion n_ctx。
- 明确非 null refusal 拒绝；已知 native HTTP400 machine code 分类为上下文超限/不支持 schema，其余拒绝以有限类别反馈。取消正确映射为 cancelled，不读取或回显上游 message。

源码证据（固定 commit，不以浮动 README 替代）：

- [server-task.cpp:340](https://github.com/ggml-org/llama.cpp/blob/11fe02151f79c41d0d4af7da708755d73b9c0da6/tools/server/server-task.cpp#L340)：native final response；同文件 task_params::to_json 返回生成参数，无 n_ctx。
- [server-context.cpp:4818](https://github.com/ggml-org/llama.cpp/blob/11fe02151f79c41d0d4af7da708755d73b9c0da6/tools/server/server-context.cpp#L4818)：/props 的 slot n_ctx、model_alias、model_path、chat_template、build_info。
- [server-context.cpp:4518](https://github.com/ggml-org/llama.cpp/blob/11fe02151f79c41d0d4af7da708755d73b9c0da6/tools/server/server-context.cpp#L4518)：聊天 prompt 按 true/true 分词；[server-common.cpp:867](https://github.com/ggml-org/llama.cpp/blob/11fe02151f79c41d0d4af7da708755d73b9c0da6/tools/server/server-common.cpp#L867) 的数字数组直接保留 token。
- [server-context.cpp:5279](https://github.com/ggml-org/llama.cpp/blob/11fe02151f79c41d0d4af7da708755d73b9c0da6/tools/server/server-context.cpp#L5279)：/apply-template 复用聊天模板处理；[server-common.cpp:1362](https://github.com/ggml-org/llama.cpp/blob/11fe02151f79c41d0d4af7da708755d73b9c0da6/tools/server/server-common.cpp#L1362) 接受布尔 enable_thinking。
- [build-info.cpp.in:27](https://github.com/ggml-org/llama.cpp/blob/11fe02151f79c41d0d4af7da708755d73b9c0da6/common/build-info.cpp.in#L27)：build_info 为 b + build number + commit。

### P1：opaque model handle 的格式化可能泄露凭据

使用合成凭据复现 `fmt.Sprintf("%+v", ModelSnapshot)` 深入未导出 containing fields 后暴露 `credentials.Secret` 内部值；仅 Secret.String 不足以保护该路径。

`internal/app/sourcesettings/extraction.go` 为整个 extractionSnapshot 增加 String/GoString 脱敏及拒绝 JSON marshal；`metadataextract.ModelSnapshot.Handle` 标为 `json:"-"`。回归核对 `%+v` 与 JSON 不含合成凭据。

同时修正 Snapshot 顺序：保存过的 disabled 配置先返回 disabled，不先解密已停用密钥或探测远程；未配置和缺模型有独立错误；版本变更前后复查，任意 adapter 错误只能映射至有限错误类别。

### P2：模型整体超时可超过 HTTP 写期限

原来能力探测、模板/分词、推理、最终复查分段计时，可能超过父集成的 130s 写期限。`metadataextract.Service.Extract` 现在使用同一个 120s context 包住整个提取；回归验证三阶段共享同一 deadline。

### P2：Go 与浏览器的规则校验不一致

- Go 过去允许同一规则内重复归一标签、显式 `label_value_options:null`，JS 会拒绝。
- Unicode lowercasing 在 Go/ECMAScript 对 dotted I/sigma 不完全一致，JS `.trim()` 还额外剥离 BOM。

两侧统一为 Unicode White_Space 修剪 + ASCII A-Z 大小写折叠；设置文案明确“忽略英文标签大小写”。Go 拒绝重复标签和显式 null，保留“省略 options 使用默认值；提供时三个成员完整”的契约。回归覆盖 dotted I、全角空格、BOM、重复标签和 null。

规则仓储审阅确认：保存事务锁 metadata_definition_head FOR SHARE、检查定义版本后 CAS 写规则；定义更新的 FOR UPDATE 与其互斥，未发现需改动的注册表/规则写入竞态。

### P1：主页重新载入字段会丢失未采用 OCR 内容

`frontend/src/home/index.js` 的窄修复 `retryMetadataWorkspace` 在 OCR 或草稿脏时保留内容并反馈；干净重试先 dispose OCR/unmount search，再让 shell 销毁旧 draft。两项回归覆盖保留内容和正确释放顺序。除这项协调函数外，主页回调透传由父会话实现。

### P1：其他设置页改变配置后仍可采用旧候选

仅在候选返回时或点击“核对此候选”检查本地版本，无法发现另一设置页修改规则、AI 或书目来源。现已把校验放在实际“采用所选字段”动作：

```js
shell.showCandidate(candidate, { beforeApply })
// beforeApply({ signal }) 必须成功完成；失败不修改 draft。
// OCR: onCandidates([candidate], { beforeApply })
```

- editor 冻结本次选择；等待时禁用采用/选择/人工覆盖确认，仍可“取消核对”；重复点击不会重复请求。清空、替换候选、取消、卸载使旧代际失效并 abort；即使 transport 忽略 abort，迟到成功也不能采用。失败取消同组剩余请求、保留草稿、显示有限中文反馈，允许手动重试。
- 成功 preflight 后仍由共享 draft.applyCandidate 重新检查本地 input/config/definition/所选 field revision、人工锁与无效输入；没有复制 merge 逻辑。历史候选可继续不传 preflight。
- OCR 规则候选捕获生成时 rules_version，采用时 no-store 读取 schema + extraction-rules 并重新校验；AI 捕获请求时 config_revision，读取 schema + ai 并要求仍 enabled/相同版本。任何 HTTP、网络或格式失败均拒绝采用，OCR 文字不变。
- 书目候选捕获 resolve 时 source config_version 与 provider_id，采用时 no-store 读取 schema + settings/sources，要求该来源存在、enabled、版本相同。共享 candidate.config_revision 仍是 AI context，不能误当书目来源版本。
- 无 BroadcastChannel 或后台轮询；authority GET 不发生在打开候选对照时。

## 本轮更改文件

- `internal/modelapi/extraction.go`, `extraction_test.go`, `client.go`, `client_test.go`
- `internal/app/sourcesettings/extraction.go`, `extraction_test.go`
- `internal/app/metadataextract/types.go`, `service.go`, `service_test.go`
- `internal/app/extractionrules/service.go`, `service_test.go`
- `frontend/src/ocr/rules.js`, `index.js`
- `frontend/src/settings/extraction_rules.js`
- `frontend/src/shared/metadata/editor.js`, `frontend/src/ui_shell/index.js`, `frontend/src/metadata-search/index.js`
- `frontend/src/home/index.js`（仅已协调的 retry 修复）
- `frontend/src/tests/ocr_rules.test.mjs`, `ocr_module.test.mjs`, `metadata_editor.test.mjs`, `metadata_search.test.mjs`, `home_metadata_retry.test.mjs`
- 本报告。原实施报告、资源与他人已有改动保留。

## 本轮实际检查

- `npm run test:frontend`：160/160 通过，覆盖本轮 editor/OCR/provider 的实际采用及迟到、配置变更、disabled、schema变更、HTTP/网络/格式失败，既有回归未破坏。
- `npm run lint`：通过。
- 最后增加失败分支 abort 后，`node --test frontend/src/tests/metadata_editor.test.mjs`：9/9 再次通过，含同组请求释放断言。
- `go test -race ./internal/modelapi ./internal/app/metadataextract ./internal/app/sourcesettings ./internal/app/extractionrules -count=1`：全部通过。
- `go vet ./internal/modelapi ./internal/app/metadataextract ./internal/app/sourcesettings ./internal/app/extractionrules ./internal/httpapi`：通过。
- 在父会话分配的隔离 PostgreSQL16 `ocr` 数据库：`go test -race ./internal/store/postgres/extractionrules ./internal/httpapi -run 'TestVersionedRulesPostgres|TestMetadataExtract' -count=1 -v`，3 项通过、无 skip。没有接触其他 agent 的测试数据库。
- `git diff --check`：通过。

原报告中的真实浏览器 WASM 四语言识别、15 项 vendored 资源校验结果仍有效，本轮未重复运行它们。统一 build/dist、全仓 Go 与完整产品浏览器/容器验收由父会话执行，不把它们写成上述已运行结果。

## 仍需父集成验收的边界

1. 上游固定源码审查和 transport mock 证明协议实现与特定 commit 对齐；**没有证明真实 MiniCPM 从产品 Go 容器可达或真实扩展字段提取通过**。需走实际受保护服务路径验证模板、上下文、输出和语义。
2. 返回 build_info/model fingerprint 是受配置信任的协议身份声明，不是远程二进制或模型权重的密码学证明。
3. Native llama.cpp 不一定输出独立 refusal 字段；显式 refusal 信号会拒绝，但 schema 合法的空 fields 无法区分“无证据”与语义拒答。不要把合成拒绝夹具说成真实模型拒答已验证。
4. 采用前 GET 检查的是当前读到的权威版本；浏览器本地采用与服务器设置写入之间没有分布式事务。当前实现遵循批准的按采用动作重检方案。
5. 移动设备内存、复杂/低清截图准确率与生产资源缓存需要单独测量；原报告的干净合成图仅代表该样例。
