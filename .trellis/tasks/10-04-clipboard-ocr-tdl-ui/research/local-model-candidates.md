# CPU 小模型候选：中文 OCR 文字到漫画元数据

调研日期：2026-10-04。范围是 OCR 已完成后的七字段提取，不是图像 OCR 或内容创作。本轮仅查询公开 Hugging Face 模型卡、仓库 API 和 llama.cpp 文档；未下载权重、访问服务器或运行推理。部署资源和真实准确率由独立实测确认。

## 首轮推荐

| 用途 | 候选及下载仓库 | 参数量 | 推荐量化与文件体积 | 判断 |
| --- | --- | --- | --- | --- |
| 中文通用基线 | `Qwen/Qwen2.5-1.5B-Instruct-GGUF`，来源 `Qwen/Qwen2.5-1.5B-Instruct` | 原模型 1,543,714,304，模型卡称 1.54B | Q4_K_M；1,117,320,736 字节，约 1.041 GiB | 官方量化来源明确，中文和结构化输出是模型卡声明的能力；原生非思考流程，适合作为第一条质量/延迟基线 |
| 低拒绝倾向对照 | `bartowski/mlabonne_Qwen3-1.7B-abliterated-GGUF`，来源 `mlabonne/Qwen3-1.7B-abliterated` | 1,720,574,976，约 1.72B | Q4_K_M；1,107,408,832 字节，约 1.031 GiB | 原作者明确声明 uncensored，但同时说明实验性质；必须关闭思考后比较中文提取质量，不能只比较是否拒绝 |

两者都处于 0.5B–3B 的目标级别，GGUF 支持由 llama.cpp 在 CPU 运行。**约 1.1 GB 权重文件不等于运行只需 1.1 GB 内存**：还需 KV cache、计算缓冲及服务进程空间；本轮没有承诺特定 VPS 可用内存、速度或并发。先只加载一个模型，使用短上下文和单请求并发测量。

## A：官方 Qwen2.5-1.5B-Instruct

- GGUF repo：[`Qwen/Qwen2.5-1.5B-Instruct-GGUF`](https://huggingface.co/Qwen/Qwen2.5-1.5B-Instruct-GGUF)。本次 API revision：`91cad51170dc346986eccefdc2dd33a9da36ead9`。
- 文件：`qwen2.5-1.5b-instruct-q4_k_m.gguf`。单个文件，不需要下载整个仓库。
- 固定版本下载 URL：`https://huggingface.co/Qwen/Qwen2.5-1.5B-Instruct-GGUF/resolve/91cad51170dc346986eccefdc2dd33a9da36ead9/qwen2.5-1.5b-instruct-q4_k_m.gguf`
- LFS SHA-256：`6a1a2eb6d15622bf3c96857206351ba97e1af16c30d7a74ee38970e434e9407e`。
- API 文件大小：`1117320736` 字节。以上是 Hub 提供的元数据，尚未通过实际下载后 hash 校验。
- 许可证：GGUF 模型卡和仓库 LICENSE 均明确 **Apache-2.0**；原模型也明确 Apache-2.0。
- 来源：Qwen 官方组织上传；原模型本次 revision `989aa7980e4cf806f80c7fef2b1adb7bc71aa306`。GGUF 仓库最近修改时间为 `2024-09-20T06:31:38Z`，这是固定模型发布快照，不表示持续跟随最新 Qwen。
- 原模型卡声明支持包括中文在内的 29 种以上语言，并改进 JSON 结构化输出。它是文本模型，不能直接读取粘贴截图。
- 聊天模板：HF API 展示内嵌 Qwen/ChatML Jinja 模板，使用 `<|im_start|>role`、`<|im_end|>`；没有 Qwen3 的 `enable_thinking` 分支。使用模型自带模板传 system/user 消息，不自行拼接另一种模型格式。
- 原模型卡写上下文 32,768；该 GGUF 仓库 API 汇总 metadata 的 `context_length` 为 8,192。此处不假设二者等价，首轮使用 2,048 或 4,096 上下文并以下载后 loader 报告为准。
- HF GGUF 汇总 metadata 的 `total` 为 1,777,088,000，和原模型 safetensors 参数统计不同。候选参数量采用原模型统计及官方模型卡，不能据此将其误记为另一款 1.78B 基础模型。

## B：Qwen3-1.7B-abliterated 社区量化

- GGUF repo：[`bartowski/mlabonne_Qwen3-1.7B-abliterated-GGUF`](https://huggingface.co/bartowski/mlabonne_Qwen3-1.7B-abliterated-GGUF)。本次 API revision：`fb7311cea48018c704b343c2ccabaa0cbc13f4f3`。
- 文件：`mlabonne_Qwen3-1.7B-abliterated-Q4_K_M.gguf`。模型卡表格明确为单文件、`Split=false`。
- 固定版本下载 URL：`https://huggingface.co/bartowski/mlabonne_Qwen3-1.7B-abliterated-GGUF/resolve/fb7311cea48018c704b343c2ccabaa0cbc13f4f3/mlabonne_Qwen3-1.7B-abliterated-Q4_K_M.gguf`
- LFS SHA-256：`bea197fd05bad4126cdfe3198b53e6458b731ba554d9bc33548d0259788a7ba8`。
- API 文件大小：`1107408832` 字节。尚未下载校验。
- 来源链：`Qwen/Qwen3-1.7B` → `mlabonne/Qwen3-1.7B-abliterated` → bartowski GGUF。量化卡说明使用 llama.cpp `b5228` 和 imatrix；这不是 Qwen 官方量化。
- 许可证核实边界：原始 Qwen3 及 mlabonne 修改版模型卡明确 **Apache-2.0**；bartowski GGUF 卡没有单独填写 license 字段，而是明确指向该修改版。应随部署保留上游许可证与归属，不能把空的量化卡字段表述为“该卡独立声明了 Apache-2.0”。
- 维护快照：GGUF 最近修改 `2025-04-30T21:25:10Z`；修改版本次 revision 为 `62f9c246cf426703a08183aa883a0d8149984bb0`，最近修改 `2025-05-18T17:25:47Z`。量化卡没有记录所使用的原始 checkpoint commit，不能声称它一定对应当前源模型 revision；固定下载后的 GGUF revision/hash 才是此次试验的可复现身份。
- 作者原文声明：`This is an uncensored version of Qwen/Qwen3-1.7B`，并说明这是研究拒绝行为的实验项目，`it might not turn out as well as expected`。这支持“作者宣称低拒绝倾向”，**不证明中文成人向标签提取更准确，也不保证绝不拒绝**。
- 原始 Qwen3 模型卡声明支持 100 多种语言/方言；修改版没有给出本任务中文字段提取 benchmark。拒绝行为减少可能伴随指令遵循、事实性或格式稳定性变化，必须与官方基线实测比较。
- 聊天模板：API 展示内嵌 Qwen3 Jinja 模板，确有 `enable_thinking is defined and enable_thinking is false` 分支，随后预填空 `<think>...</think>`。为 CPU 提取任务使用这个硬开关，不依赖 OCR 文本里的 `/no_think` 指令。

## C：升级验证候选 Qwen3-4B-Instruct-2507

追加于 2026-10-04。前两款记录保留为首轮公开调研快照。主 agent 的后续实测发现，两款小模型虽然能满足 JSON Schema，严格字段提取仍不稳定，包括补写原文缺失字段。因此增加这一更大容量的官方 Instruct 基线进行验证；这只是选择理由，不代表已经证明 4B 更准确，亦不预判其成人向标签接受率或运行性能。

- 官方来源：[`Qwen/Qwen3-4B-Instruct-2507`](https://huggingface.co/Qwen/Qwen3-4B-Instruct-2507)，本次源模型 revision `cdbee75f17c01a7cc42f958dc650907174af0554`。原模型参数量为 `4022468096`，约 4.02B。
- GGUF 来源：社区量化 [`unsloth/Qwen3-4B-Instruct-2507-GGUF`](https://huggingface.co/unsloth/Qwen3-4B-Instruct-2507-GGUF)，模型卡明确将 `base_model` 指向上述官方模型；不是 Qwen 官方 GGUF 仓库。
- 许可证：官方源模型和 Unsloth 量化模型卡均明确 **Apache-2.0**，量化卡的 license_link 指向官方 LICENSE。
- 固定 GGUF revision：`a06e946bb6b655725eafa393f4a9745d460374c9`。
- 文件：`Qwen3-4B-Instruct-2507-Q4_K_M.gguf`，单个 Q4_K_M 文件；API 大小为 `2497281120` 字节，约 2.50 GB，符合本轮小于 3 GB 的文件目标。
- 固定版本下载 URL：`https://huggingface.co/unsloth/Qwen3-4B-Instruct-2507-GGUF/resolve/a06e946bb6b655725eafa393f4a9745d460374c9/Qwen3-4B-Instruct-2507-Q4_K_M.gguf`
- LFS SHA-256：`3605803b982cb64aead44f6c1b2ae36e3acdb41d8e46c8a94c6533bc4c67e597`。本节来源核对使用公开 Hub 元数据；下载后校验由主 agent 独立执行。
- 非思考机制：官方模型卡明确说明该版本仅支持 non-thinking，不生成 `<think></think>` 块，且不再要求 `enable_thinking=False`。查询的 GGUF 内嵌 Jinja 模板直接以 assistant generation prompt 结束，可使用模型自带模板；不能将它与 B 中需关闭思考的早期 Qwen3-1.7B 混同。
- 选择边界：本次是官方 Instruct 的社区量化，不是去拒绝修改版。升级是为了验证严格提取质量；仍需同一批输入核验缺失字段、作者/社团区分和成人分类标签处理，不能用参数量或格式合规率推定实际效果。

来源：[官方固定版本模型卡](https://huggingface.co/Qwen/Qwen3-4B-Instruct-2507/blob/cdbee75f17c01a7cc42f958dc650907174af0554/README.md)、[官方 LICENSE](https://huggingface.co/Qwen/Qwen3-4B-Instruct-2507/blob/cdbee75f17c01a7cc42f958dc650907174af0554/LICENSE)、[Unsloth 固定版本模型卡](https://huggingface.co/unsloth/Qwen3-4B-Instruct-2507-GGUF/blob/a06e946bb6b655725eafa393f4a9745d460374c9/README.md)、[Hub 文件/LFS 元数据](https://huggingface.co/api/models/unsloth/Qwen3-4B-Instruct-2507-GGUF?blobs=true)。

## 非思考模式及首次运行设置

已查询 llama.cpp 官方文档/源码 revision `dd266785c2595775001c1c714bd9d92b3ef34cde`。该版本推荐 `--reasoning off`，源码会设置模板参数 `enable_thinking=false`；`--chat-template-kwargs '{"enable_thinking":false}'` 仍存在但已标记该用途弃用。较旧构建可能只有后者，实际部署必须先看所选二进制 `--help`，不能照抄新文档参数到旧版本。

- Qwen2.5 使用内嵌模板即可，没有需要关闭的 Qwen3 思考流程。
- B 中的 Qwen3-1.7B 必须确认 Jinja 模板启用，使用支持的硬开关关闭思考，并核验响应没有额外推理消耗；C 中的 Instruct-2507 原生为非思考模型。
- `--reasoning-format none` **不等于关闭思考**；官方文档说明它只是让思考内容留在 `message.content`，不能用于节省 CPU/输出 token。
- 首轮以单请求、约 2K 上下文、限制最终输出长度开始，分别记录冷启动、首 token、总耗时、生成速度、RSS/峰值和是否发生 swap。具体线程数由主机实际 CPU 资源决定，不在本研究中假设。
- 使用相同字段定义、同一组脱敏 OCR 文字、相同输出上限比较。启用 JSON/schema 约束可以检验语法，但不能替代字段内容正确性检查。

## 选择时排除的近似结果

- `mradermacher/Qwen2.5-1.5B-Instruct-uncensored-GGUF` 确实存在且约 1.12 GB，但源 `thirdeyeai/Qwen2.5-1.5B-Instruct-uncensored` 的卡是未填写模板，license 为 More Information Needed，base_model 还指向 Coder 版；因此不作为来源可审查的首轮首选。
- `mradermacher/Qwen3-1.7B-abliterated-GGUF` 确实存在，源 license 明确，但目前 README 将大小写不同的两个完整文件名列成 PART 1/PART 2，API 也列出两个约 1.1 GB Q4_K_M 文件。为避免无谓的文件选择/拼接歧义，首轮使用 bartowski 明确的单文件。
- `DevQuasar/huihui-ai.Qwen3-1.7B-abliterated-GGUF` 确实存在，但查询的仓库 GGUF 汇总 metadata 没有显示 chat_template；没有下载读取其实际文件 header，不能确认缺失是否只在 Hub metadata 层。首轮不选这个需要额外模板确认的来源。

## 内存不足时的降档线索

如果主机剩余内存不能给上述 1.1 GB 模型留足缓冲，可再考虑官方 `Qwen/Qwen2.5-0.5B-Instruct-GGUF`：`qwen2.5-0.5b-instruct-q4_k_m.gguf` 为 `491400032` 字节，约 0.458 GiB；revision `9217f5db79a29953eb74d5343926648285ec7e67`，LFS SHA-256 `74a4da8c9fdbcd15bd1f6d01d621410d31c6fc00986f5eb687824e7b93d7a9db`，Apache-2.0。它是低资源后备，不应假设达到 1.5B 的中文结构化质量；官方卡参数量为 0.49B（商品名 0.5B）。

## 实测应回答的问题

1. 作者、社团、翻译组是否区分；缺失作者、系列或序号时是否留空，而不是推测。
2. 面对正常的 `R18`、`成人向` 等分类标签，能否按原文提取而不拒绝、不改写或扩写简介。本轮不生成露骨色情测试内容。
3. 中文/日文姓名、换行、OCR 错字、重复标签与多段截图文本的字段准确率；仅格式正确不计成功。
4. OCR 文本包含指令式句子时是否仍只作为数据处理，未知字段和越权输出是否被既有白名单拒绝。
5. 小 VPS 上单次提取的可接受延迟与内存占用；社区低拒绝模型是否真的优于通用基线。未测前不承诺质量或服务稳定性。

## 公开证据入口

- [官方 1.5B GGUF 模型卡（固定 revision）](https://huggingface.co/Qwen/Qwen2.5-1.5B-Instruct-GGUF/blob/91cad51170dc346986eccefdc2dd33a9da36ead9/README.md)；[Hub 文件/LFS 元数据](https://huggingface.co/api/models/Qwen/Qwen2.5-1.5B-Instruct-GGUF?blobs=true)。
- [官方 1.5B 原模型元数据](https://huggingface.co/api/models/Qwen/Qwen2.5-1.5B-Instruct?blobs=true)；[原模型 config](https://huggingface.co/Qwen/Qwen2.5-1.5B-Instruct/blob/989aa7980e4cf806f80c7fef2b1adb7bc71aa306/config.json)。
- [mlabonne 修改版模型卡（固定 revision）](https://huggingface.co/mlabonne/Qwen3-1.7B-abliterated/blob/62f9c246cf426703a08183aa883a0d8149984bb0/README.md)。
- [bartowski GGUF 模型卡（固定 revision）](https://huggingface.co/bartowski/mlabonne_Qwen3-1.7B-abliterated-GGUF/blob/fb7311cea48018c704b343c2ccabaa0cbc13f4f3/README.md)；[Hub 文件/LFS/模板元数据](https://huggingface.co/api/models/bartowski/mlabonne_Qwen3-1.7B-abliterated-GGUF?blobs=true)。
- [Qwen3 原模型卡：思考硬开关与语言能力](https://huggingface.co/Qwen/Qwen3-1.7B)；[Apache-2.0 原许可证](https://huggingface.co/Qwen/Qwen3-1.7B/blob/main/LICENSE)。
- [llama.cpp server 文档](https://github.com/ggml-org/llama.cpp/blob/dd266785c2595775001c1c714bd9d92b3ef34cde/tools/server/README.md#L231)；[参数处理源码](https://github.com/ggml-org/llama.cpp/blob/dd266785c2595775001c1c714bd9d92b3ef34cde/common/arg.cpp#L3690)。
