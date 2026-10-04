# 用户指定 MiniCPM5-2B 的接入证据

用户在 RackNerd 小模型试验进行期间指定尝试 `openbmb/Minicpm5-2b`。官方仓库的实际名称为 [openbmb/MiniCPM5-2B](https://huggingface.co/openbmb/MiniCPM5-2B)，是纯文本生成模型，不需视觉投影或 OCR 图片输入。

## 官方模型与量化权重

- 原始模型 revision：`f97400052a43d642bbc6e9975e2397e3ae6a6b52`。
- HF safetensors 元数据参数量：2,516,756,480；架构 `LlamaForCausalLM`，`model_type=llama`。
- 原模型及官方 GGUF 均标注 Apache-2.0；未据此声称其为专门成人内容微调或拒绝行为已消除的模型。
- 官方 GGUF repo：`openbmb/MiniCPM5-2B-GGUF`。
- GGUF revision：`2079a22f3beaa4e306449978533478fe0522f4b3`。
- 文件：`MiniCPM5-2B-Q4_K_M.gguf`，1,561,318,368 bytes。
- SHA-256：`ec2d5801640099e97d8d7e8003ad4d81f336e757811f03a26173dddf386602fd`。
- [固定下载 URL](https://huggingface.co/openbmb/MiniCPM5-2B-GGUF/resolve/2079a22f3beaa4e306449978533478fe0522f4b3/MiniCPM5-2B-Q4_K_M.gguf)。

## 已固定运行时的兼容性

当前 llama.cpp b11382 / revision `11fe02151f79c41d0d4af7da708755d73b9c0da6` 已包含：

- [Llama 架构登记](https://github.com/ggml-org/llama.cpp/blob/11fe02151f79c41d0d4af7da708755d73b9c0da6/src/llama-arch.cpp#L10)。
- [MiniCPM5 pre-tokenizer 识别](https://github.com/ggml-org/llama.cpp/blob/11fe02151f79c41d0d4af7da708755d73b9c0da6/src/llama-vocab.cpp#L2200) 和专用 tokenizer 正则。
- [MiniCPM5 聊天模板支持](https://github.com/ggml-org/llama.cpp/blob/11fe02151f79c41d0d4af7da708755d73b9c0da6/common/chat.cpp#L1210)。
- [关闭思考的参数](https://github.com/ggml-org/llama.cpp/blob/11fe02151f79c41d0d4af7da708755d73b9c0da6/common/arg.cpp#L3690)：`--reasoning off` 会设置 `enable_thinking=false`。官方模型模板也明确支持该开关。

官方 GGUF 卡建议设置 `min_p=0.0`，避免默认值引发重复。本轮仍保持 temperature=0、同样本、同资源及输出长度；为该模型显式设置 `--min-p 0` 并关闭思考，记录这一模型专用运行参数。

源码支持不代替实际加载/推理验证。最终以 [RackNerd 试验报告](racknerd-model-trial.md) 中记录的实际结果为准。
