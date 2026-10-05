# 现有 CLI Proxy 的 Grok 接入评估

> 已撤回：用户后续明确不再使用 Grok，并告知已自行删除认证文件。本文件仅为此前的只读调查记录，不代表当前模型列表或授权状态；当前方向为 RackNerd 开源小模型试验。

## 用户问题

2026-10-04：用户询问能否依赖 `https://cliproxy.ryancheng.org/management.html#/oauth` 中的 Grok 模型处理 OCR 元数据。

## 当前实例的只读证据

- Safari 已有管理会话可用；未发起新的 OAuth，未修改服务配置或认证文件。
- OAuth 页显示 Grok / xAI OAuth 登录入口。
- 中心信息显示 CLI Proxy API **v8.0.13**、管理中心 **v1.25.3**，主程序仓库链接为 `https://github.com/router-for-me/CLIProxyAPI`。
- 中心信息页面通过模型发现请求显示 42 个模型，其中 Grok 18 个。可见文本模型包括 `grok-4.7`、`grok-4.6`、`grok-4.5`、`grok-4.3`、`grok-4.7-build-fast` 等；模型枚举成功不等于推理验证成功。
- 匿名 GET 根路径返回 200，并公布 `POST /v1/chat/completions`、`POST /v1/completions`、`GET /v1/models`；匿名 GET `/v1/models` 返回 401，说明接入需要客户端 API 鉴权。
- 未读取/输出 API 密钥、OAuth token 或认证文件；未向模型发送截图、OCR 原文或真实作品数据；尚未执行推理请求。

## 接入建议

- 将此实例列为首个兼容验证目标。项目服务端使用 `https://cliproxy.ryancheng.org/v1` 作为本实例的 OpenAI-compatible base URL，调用 `/chat/completions`。用户已明确要求地址及凭据可配置，自动读取模型列表供用户选择；此地址和 Grok 模型不能写死为唯一配置。
- 使用代理为客户端提供的 API key；`management.html#/oauth` 是管理页面，不能作为模型推理 URL，管理凭据和 OAuth token 也不应充当应用请求密钥。
- 项目复用代理管理的 Grok 登录，避免在本项目中再实现 Grok OAuth；Telegram/tdl 的登录仍是独立能力。
- 首版维持“浏览器本地 OCR → 可编辑文本 → 用户主动调用代理 → 七字段候选 → 确认提交”。AI 未配置或拒绝时继续规则/手工流程。
- 模型可在设置中切换，内容拒绝不自动换模型重发。不能承诺 Grok 或代理能消除上游内容策略、限流、账号处理或 OAuth 失效。

## 实现前验证项

1. 用无敏感内容的合成样例测试实际推理、七字段 JSON 输出和中文保真。
2. 该版本源码已确认 `response_format.type=json_schema` 的转换支持（见下）；仍须实测该实例/账号/所选模型是否接受 schema。不能因为代理宣称 OpenAI-compatible 就承诺所有响应格式均兼容。
3. 超时、限流、拒绝、无效 JSON、额外字段和凭据失效应保持 OCR 与手工草稿，不能阻止 tdl 下载。
4. 日志不记原文或凭据，测试连接只用固定无敏感样例。真实样例验证须另行明确发送内容及目标。

## 固定版本结构化输出源码证据

- [xai_executor_request.go:60](https://github.com/router-for-me/CLIProxyAPI/blob/v8.0.13/internal/runtime/executor/xai_executor_request.go#L60)：默认将 OpenAI Chat 请求转为 FormatCodex/Responses。
- [codex_openai_request.go:377](https://github.com/router-for-me/CLIProxyAPI/blob/v8.0.13/internal/translator/codex/openai/chat-completions/codex_openai_request.go#L377)：`response_format.type=json_schema` 映射为 `text.format`，保留 name、strict、schema。此处只有 text 与 json_schema 分支，没有 json_object 分支。
- [xai_executor_execute.go:46](https://github.com/router-for-me/CLIProxyAPI/blob/v8.0.13/internal/runtime/executor/xai_executor_execute.go#L46)：向上游 `/responses` 提交转换后的请求。

因此首个推理验证应使用七字段 `json_schema`，不要将 JSON mode (`json_object`) 和 JSON Schema 混为一谈。源码转换支持仍不保证模型一定遵守格式；返回的内容仍须经过七字段白名单与类型/长度校验。

该版本的 [上游选择逻辑](https://github.com/router-for-me/CLIProxyAPI/blob/v8.0.13/internal/runtime/executor/xai_executor_request.go#L216) 区分 OAuth 默认的 `https://cli-chat-proxy.grok.com/v1/responses` 与官方 API Key 路径 `https://api.x.ai/v1`，配置可以改变选择。本轮没有读取该实例的认证配置，不能仅凭 OAuth 页面判断具体调用路径，也不能把官方 API 的所有保证套用于 OAuth 路径。

官方 [Structured Outputs 文档](https://docs.x.ai/developers/model-capabilities/text/structured-outputs) 提供文本解析与 JSON Schema 用法；[Chat API](https://docs.x.ai/developers/rest-api-reference/inference/chat-completions) 定义了 refusal 字段。它们支持技术可行性及“可能拒绝”的边界，不证明本实例对特定 R18 样例的接受情况。本轮未取得可读的最新内容政策正文，不据此推断某类内容必然被允许或账号必然安全。

本文记录可行性和建议，不代表用户已批准整套实现设计，也不代表实例已完成推理或 R18 内容兼容验证。
