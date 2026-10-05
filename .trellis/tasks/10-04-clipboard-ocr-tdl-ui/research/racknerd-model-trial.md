# RackNerd 小模型隔离试验

## 授权与目的

用户已放弃 Grok，并授权查找开源小参数模型，在 RackNerd 部署并测试 OCR 文字到七字段元数据的提取能力。此试验不实现产品前端、不处理真实频道正文，也不代表模型能接受任意 R18 输入。

试验使用 10 个合成中文文本（含非露骨的成年人分级标签），分别请求普通提示 JSON 和 JSON Schema 两种模式。测试作者/社团/翻译组区分、空字段、多行简介、OCR 噪声和指令式文本。模型运行不需要云端 AI 账号；是否正确遵循提取要求仍以实测为准。

## 主机与隔离边界

- 主机实测：6 vCPU，Xeon E5-2680 v2，支持 AVX/F16C、无 AVX2；物理内存约 7.75 GiB，试验前可用约 6.71 GiB，磁盘空闲约 121 GiB。
- 通过官方 Dockhand v1.0.49 API 创建独立 `internal` Stack `ocr-model-probe`，未修改 CLI Proxy、Bark、PostgreSQL 或 Hawser 的配置，不加入自动更新。
- RackNerd 项目目录：`/opt/docker_data/stacks/ocr-model-probe/`。管理副本位于 Dockhand 原生 RackNerd Stack 路径。
- 一次只加载一个模型；4 CPU/4 线程、3500 MiB 内存及相同 swap 上限、4096 上下文、并发 1；容器非 root、只读文件系统、模型只读挂载。
- 仅发布 `127.0.0.1:18085`，推理与模型列表要求 API key；密钥在服务器私密文件内，未写入仓库或工具输出。`/health` 返回无敏感健康状态。
- 四款权重下载固定 revision，并逐文件校验大小和 SHA-256，见 [Qwen 候选来源](local-model-candidates.md) 与 [MiniCPM5 来源](minicpm5-candidate.md)。权重总计约 6.28 GB，保留用于复现，但服务只加载当前一款。

## 运行时

- 官方 `ghcr.io/ggml-org/llama.cpp` CPU server，版本 `b11382`，源码 revision `11fe02151f79c41d0d4af7da708755d73b9c0da6`。
- 固定 Linux amd64 manifest：`sha256:1cdfea0828170c34d56a8a6d9fc5a0898e8bca3762e5ec6be0c01032f7f7bde8`。
- 该 revision 的 CPU Dockerfile 使用 `GGML_NATIVE=OFF`、动态后端和全部 CPU 变体；实际启动与推理进一步检验老 CPU 兼容性。
- `--jinja --chat-template-kwargs '{"enable_thinking":false}'` 禁用支持该模板开关的 Qwen3 思考输出；不把隐藏 reasoning 的格式参数当作关闭思考。
- [初始 Compose](model-probe.compose.yaml) 固定运行时和资源设置；各模型的 Compose 变体保留在本研究目录。MiniCPM5 采用 `--reasoning off --min-p 0`，符合官方建议。所有切换均通过 Dockhand 保存和部署。

## 验证状态

已验证服务健康、匿名 `/v1/models` 返回 401、带密钥模型发现返回正确 alias。原有四个业务容器在切换试验模型后 ID 未变且健康，见 [最终运行状态核对](final-runtime-check.json)。

四款模型各完成 10 组样例、两种模式，共 80 次主对照请求。以下均为原始模型响应评分，未加业务规则收尾：

| 模型 / 模式 | 合规 JSON | 七字段全对 | 字段匹配率 | 中位耗时 |
| --- | --- | --- | --- | --- |
| Qwen2.5-1.5B / 提示 JSON | 9/10 | 4/10 | 77.1% | 5.94 秒 |
| Qwen2.5-1.5B / JSON Schema | 10/10 | 4/10 | 84.3% | 5.83 秒 |
| Qwen3-1.7B abliterated / 提示 JSON | 9/10 | 3/10 | 72.9% | 7.76 秒 |
| Qwen3-1.7B abliterated / JSON Schema | 10/10 | 4/10 | 77.1% | 7.64 秒 |
| Qwen3-4B-Instruct-2507 / 提示 JSON | 10/10 | 9/10 | 98.6% | 15.50 秒 |
| Qwen3-4B-Instruct-2507 / JSON Schema | 10/10 | 8/10 | 97.1% | 14.76 秒 |
| MiniCPM5-2B / 提示 JSON | 0/10 | 0/10 | 0%（格式未解析） | 8.99 秒 |
| MiniCPM5-2B / JSON Schema | 10/10 | 7/10 | 90.0% | 8.20 秒 |

四款均未出现显式或启发式判定的拒绝。这里的成人样例只有非露骨分级标签，不能外推真实 R18 正文接受率。4B 的剩余错误集中于不确定作者、多行简介；MiniCPM5 的三组失败是未知占位、不确定 OCR 作者/序号、多行简介。格式合规并未消除语义错误。

MiniCPM5 提示模式的 0% 是严格 JSON 契约失败后统一记零，不能解读为模型完全不会提取。跨机补测普通样例时，确认为外包 Markdown 代码块，剥离完整外层代码块后七字段全对；这一补测不能倒推其余九条原输出，因此不回改主对照分数。

主对照原始计分报告：[Qwen1.5B](qwen25-results.json)、[Qwen1.7B](qwen3-results.json)、[Qwen4B](qwen4b-results.json)、[MiniCPM5](minicpm5-results.json)。报告只保存计分、耗时、用量及错误类别，不保存识别文字或生成正文。

## 客户端验证与最终状态

- 在 Mac 通过临时 SSH 本地转发连接 RackNerd 的回环服务；无密钥模型发现返回 401，带密钥发现返回 `minicpm5-2b-q4`。
- 补测三次实际推理：普通文本提示输出的格式诊断、未知占位诊断、成年标签的 JSON Schema 提取。成年标签的七字段全部匹配预期；未知占位中确有四个字段复制“未提供/未知/待确认/暂无”，仅该补测能够据此确认原文保留原因。
- 临时 SSH 转发在 `finally` 中关闭，未创建常驻隧道。详见 [客户端补测](client-minicpm5-verification.json)。该三次补测独立于 80 次主对照。
- 当前试验 Stack 保留 **MiniCPM5-2B Q4_K_M**，模型 ID 为 `minicpm5-2b-q4`；Base URL 为 RackNerd 本机 `http://127.0.0.1:18085/v1`，要求 API key。密钥未回传给前端或写入仓库。
- Dockhand 原生 Stack Compose 与 [MiniCPM5 配置](model-probe.minicpm5.compose.yaml) 作回读比对，见 [管理面回读](dockhand-model-readback.json)。RackNerd 环境自动更新开关当前为关闭，本试验未新增更新计划。
- 建议：在速度和内存优先的场景，MiniCPM5 配合 JSON Schema、未知占位清洗、歧义标记及人工确认可作为辅助预填候选；原始字段匹配率优先时，当前样例的 Qwen4B 更好。尚未实现这些业务清洗/前端功能，不能将“建议加规则”当作已通过的混合流程准确率。

## 评分口径

- 10 个样例，每个模型每模式各跑一次，固定 temperature=0、max_tokens=512、timeout=90；顺序交替，未做专门预热。中位耗时是本轮观测，不能代替多轮统计或并发吞吐。
- 字段匹配率使用脚本规范化比较，不是语义判分，也未调用项目 Go NormalizeMetadata。标签分隔符与现有业务规则略有差异，部分原始模型输出错误可能被已有规则消除。
- “预期空白但输出非空”可能是复制了未知占位或不清楚的原文，不能直接称为凭空编造。测试报告不保存生成原文，无法进一步归因或事后重算。
- JSON 格式限制只能保证格式，不能保证署名归属、简介完整性和缺失字段正确；模型建议仍需规则校验及人工确认。
- 进程 RSS 峰值约为 1.23 GiB（1.5B）、1.58 GiB（1.7B）、2.98 GiB（4B）与 1.73 GiB（MiniCPM5）。cgroup 内存读数受到权重页缓存归属影响，不能单独当总内存占用；无 OOM。
- 脚本 SHA-256：`8034ad93bc32d77a90ef73cd2a94f735144aa8a21c6a9c056222abf208e472b6`；样例 SHA-256：`696f0db7abc4799de2ae4d3330b3d526078d74cf9660e98facc9b6770e9068a3`。

## 恢复方式与后续边界

可在 Dockhand 单独停止 `ocr-model-probe`，不会停止现有业务容器。模型文件与报告保留在其独立目录；若用户之后要求彻底删除，可单独清理该 Stack 和目录，不改现有服务数据。

本次仅暴露回环接口；项目未来访问需单独设计受保护的内网或隧道路径。测试结果不能替代真实截图 OCR 精度评估、真实用户样例的人工验收或长期稳定性测试。
