# 用户截图分层实测 — 2026-10-07

范围：用户授权此图的识别/分析；通过已安装mediactl管理员边界测试本地OCR、保存的规则、已配置CPA→MiniCPM和三书目源。不是浏览器UI验收，也未创建作品任务、下载附件或改写Komga。未改生产规则/配置。

## Input and human baseline

- 原图 SHA-256 仅保留在受保护本地证据中。
- JPEG，1197×1597，305358 bytes；暗色Telegram截图，正文气泡+ZIP文件卡+中英完整caption，蓝色标签。
- 人工基准包含 title、aliases、creators.writer、creators.translator 四项；真实值仅保留在受保护本地证据中。
- 括号内角色配对不应成为writer/series；截断文件名不能优先于完整caption。摘要/成人标签仅在受保护证据中核对，本文件不复制正文。
- UI噪声五类：评论入口、浏览量、时间、文件大小、截断文件卡名；点赞/反应也须在后续合成回归排除。

私有证据目录：受保护本地证据目录（目录0700，文件0600）。原图不入Git，派生图片与原始结果同样仅本地保留。

## Method

1. 原图按CLI默认`chi_sim+eng`识别；通过本次授权范围的单次reveal票据读取merged文本。
2. 用macOS sips对副本裁剪两个内容区域（x/y/width/height）：body `(45,10,876,750)`；caption `(45,1210,935,275)`。不变更像素颜色/缩放/识别模型，按body→caption合并。
3. 对两组分别运行保存规则v1（16条，standard-v1）。不采用候选，不改源文本。
4. 两组分别请求AI六字段：title、aliases、creators.writer、creators.translator、summary、tags；沿用config v19，`llama_cpp_chat`，`minicpm5-2b-q4`。用户此前已委托助手代为发送/核对，本轮图像分析也获授权；凭据未读取或输出。
5. 独立临时工作区，以人工基准的中文标题、英文别名分别向Bangumi/MangaBaka/MangaUpdates检索；核对返回record的title/aliases是否精确同名，不自动采用。

## Observed results

| 层 | 原图 | 内容裁剪 | 含义 |
|---|---|---|---|
| OCR | 完成，660 UTF-8 bytes | 2图均完成，图内合计532 bytes（合并另加分隔） | 技术调用成功不等于字段可用 |
| 四核心文字标记 | 去全部空白后4/4均存在 | 去全部空白后4/4均存在 | 仅字符串存在性，不是字段准确率 |
| 五类UI噪声标记 | 5/5存在 | 0/5存在 | 裁剪能去噪；不证明自动版面识别已实现 |
| 规则 | 0候选、0告警 | 0候选、0告警 | 规则成功执行，但无法形成有用草稿 |
| AI六字段 | `unavailable`，无候选 | `unavailable`，无候选 | 尚不能评价AI字段语义精度；也未证明错误源是模型/预算/网络 |

原图首次OCR约3.1秒；裁剪两图约1.7秒。前者首次运行/缓存等条件未受控，仅运行记录，不作为性能提升结论。裁剪也造成个别字形/括号输出变化，不能宣称它统一提升文字准确率。

规则失败机制分别成立：

- OCR标题含内部空格，且为裸《标题》而非“标题：值”；现有label matcher要求冒号。
- OCR输出`剧情 介绍 :`。保存规则的简介labels为“简介/簡介/内容简介/內容簡介/介绍/介紹/Summary/Description”，没有“剧情介绍”；此外matcher仅trim两端，不归一化内部空白。只增加别名或只去空白均不能解决所有输入。
- 裸hashtag以及方括号作者/译者caption均非现有label模式。
- 本图**没有summary候选**，不能写成已实测摘要污染；摘要吸收页脚/下一气泡是源码推导与历史合成验收风险，另建回归。

书目两次检索均：Bangumi返回20、MangaUpdates返回20、MangaBaka `no_results`；返回title/aliases精确同名数0。仅说明这两次返回集合无精确匹配，不证明全库未收录，也未验证模糊译名身份。不能自动取首条。

## Failure boundaries and cleanup

- 初次规则请求及另一workspace创建出现`transport_error`；随后同一认证/规则读取成功，对原工作区重试规则成功。这是瞬时传输现象，未定位网络根因，也未更改网络配置。
- AI两次均由正常CLI提取入口返回`unavailable`；之后设置读回仍为启用/config19且完全一致。失败没有字段候选可核对；本次不据此猜测协议/模型错误或实施修复。
- 临时provider工作区以及原图/裁剪两个工作区均已关闭；没有候选采用、作品提交或真实库修改。
- `recognition.safe.json`、`ai.safe.json`、`providers.safe.json`、`cleanup.safe.json`保留安全计数/版本；`.private.json`和裁剪图仅私有目录。

## Task implications

优先把“已读到但未归类”“未请求的字段”“未命中书目”“AI服务失败”与OCR错字分开呈现。默认合理字段集合、有限caption/CJK处理、内容分区与集中核对比继续增加选择框更切合此图。AI可用性须在实施阶段定位并重新验收，不能让向导把失败伪装成准备完成。

后续用中性虚构同构图建立可入Git的回归：四核心字段、括号角色、完整/截断标题、聊天噪声、合法正文数字/时间、中文空白、无源/失败可继续。真实浏览器结果仍需独立验证。
