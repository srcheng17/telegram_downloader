> 执行更新（2026-10-05）：第二批已实现并完成本地集成检查，状态 review；实际外部验收仍未全部完成。结果以[第二批验收记录](../10-04-clipboard-ocr-tdl-ui/batch-2-verification.md)为准。下文的 planning/未执行描述保留为原计划背景。

# 实施计划：多图 OCR 与可选 AI（Batch 2）

状态：planning；以下命令和测试尚未执行，不运行task.py start、不改产品。

## 准入与并行边界

父任务最新规划获准实施且Batch1元数据/设置授权/shell完成接线。先读取这三项最新design，冻结MetadataCandidate、definitions_version、共享draft采用、modelapi调用端口和挂载点。只改本design列出的独占文件；依赖锁、共享modelapi、templates/app/settings入口、migration016和dist由root协调串行落地，不撤回他人运行修复。

## 有序步骤

1. [ ] 本地OCR能力checkpoint：固定Tesseract及四语言资源版本/hash/license，设计同源发布清单；用非敏感合成与代表性截图验证实际浏览器识别、所选语言、资源内存和离线资源已缓存场景。缺资源明确失败，不用CDN/远端fallback。
2. [ ] 先写队列/state测试，再实现图片校验、稳定ID/generation、单worker串行、缩略图/逐图raw/edit、取消/重试/资源释放。实现10图/10MiB/50MiB/12MP/8192px边界及保留已接收项。
3. [ ] 实现合并与来源片段映射、完成子集确认、重复/重叠提示、跨图简介/冲突展示；接共享draft reducer，验证手工clear/dirty和迟到回调保护。
4. [ ] 实现extractionrules类型/校验、有限本地解析、独立仓储/HTTP与设置控件；root把独立非秘密记录纳入016并接线，先用隔离PG测版本冲突/重开保存，不同时修改旧app_settings。
5. [ ] 根据注册表生成所选字段JSON Schema/提示，实施AI预算与输出证据校验，使用受控假modelapi覆盖拒绝/超限/格式/取消；与settings owner/root完成最小内部端口，不能在handler读取密钥或自己拼任意目的地。
6. [ ] 实现文字发送预览、目标/模型/字段确认及受保护extract API；所有返回包装canonical MetadataCandidate，input/config/schema/field revision匹配才可采用，任何错误保留已有内容。
7. [ ] 独立checker审查输入→OCR→规则/AI→candidate→ApplyCandidate的完整数据流、隐私/日志和生命周期；root在本批次接线、build/dist、生产镜像静态资源及浏览器集成检查。
8. [ ] 父集成者建立已授权的受保护模型路径后，从真实Go容器调用MiniCPM扩展字段样例；记录schema/预算/证据/语义通过率和耗时，不拿旧七字段成绩替代。实际截图OCR、规则和AI各自记录失败案例，未验证项保持未完成。

## 计划自动化验收

| 层 | 必须覆盖 |
| --- | --- |
| 图片/worker | MIME/扩展不一致、损坏header、解码实际尺寸、边界超限、单worker最大并发1、取消terminate、失败后可重试 |
| 状态/合并 | 批量/连续粘贴、输入框文本粘贴、重排/移除/在途新增、旧image generation、原文保留、部分完成明确采用、重复/重叠不静默删除 |
| 规则 | 版本保存/冲突、非法target/type/label/mode、规则预算、注册表变更、不同角色/卷号/计数、冲突候选、明确未知、不支持类型拒绝 |
| AI | 选定model_id、动态标准/custom字段schema、仅发送选择文字、完整prompt预算、空/拒绝/429/401/超时/长度截断、非法类型/extra key/伪造证据、指令式输入无工具/改目标能力 |
| 采用 | manual_locked/cleared保护、版本乱序、字段冲突整次拒绝、定义/规则/模型变化后旧候选失效、历史回填不接受旧响应 |
| 隐私/浏览器 | 无截图网络外发、同源WASM/worker/语言资源、无CDN fallback、无敏感storage/htmx缓存、退出/离页清理、失败导航不误卸载 |

- Go：`go test ./... -count=1`、`go vet ./...`；规则仓储/并发版本变更再跑`go test -race ./... -count=1`，设置隔离TEST_DATABASE_URL且不与其他agent共享固定ID库并行测试。
- 前端：`npm run test:frontend`、`npm run lint`；root统一`npm run build`并纳入`web/static/dist/`与OCR资源；浏览器关键流`npm run e2e:test`在隔离Compose执行。
- 实际图片验证记录每种语言/字号/背景/长图的识字错误率或精确文本差异、规则/AI字段准确性、峰值内存和时间；本任务不预先承诺准确率阈值或用synthetic fake OCR冒充真实识别。
- 所有截图fixture使用自行生成的中性文字和可公开非敏感样例，严禁提交用户截图、频道原文、凭据；可用模拟时序复现竞态，但真实WASM和模型链路必须单独留证。
- 规划校验：`python3 .trellis/scripts/task.py validate .trellis/tasks/10-04-multi-image-ocr-ai`、`git diff --check`；检查manifest路径真实存在。

## 交付、风险与回滚

交付独占代码/资源清单、挂载接口、modelapi所需端口、016规则表方案和验证记录；不自行接线共享入口。关键风险是移动端WASM内存、CJK截图准确度、扩展schema占用4096上下文及模型私密网络可达性。能力检查不通过则保持手工/规则降级，不能改成远端OCR或自动换模型。回滚只停用新识别模块，保留已有metadata_document、规则记录和原下载功能；不以删除数据作为回退。

迁移交接：提取规则表契约在第一批016落地前交付，016一次创建其独立非秘密记录结构，第二批仅接服务/控件。若已应用后仍需调整，使用新的前向迁移编号，不修改已应用016。
