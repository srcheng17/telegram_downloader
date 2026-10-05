# 分批计划校验记录

日期：2026-10-04。范围：本父任务及8个子任务的规划文档。状态仍为planning，未运行task.py start；本记录不代表产品功能完成。

## 本轮实际检查

- 9个任务逐一执行 `python3 .trellis/scripts/task.py validate <task-dir>`：全部通过，无警告。
- 每任务prd/design/implement和两份JSONL齐全；JSON解析、manifest引用存在、Markdown相对链接检查通过。
- task.json父子双向关系、planning状态及depends_on有向无环检查通过；同时最多3个实现worker，集成从第一批开始、第三批验收完成。
- 所有规划文件逐行尾随空白检查通过，包含Git尚未跟踪文件；`git diff --check`通过。
- 人工核对共享文件所有权、015/016/017预留、16号规则表第一批预建与后续前向迁移原则。
- 独立只读复核完成；发现的缺口已收敛到设计和验收条件：提交/产物快照分离、自定义定义管理、模型验证绑定字段版本、真实CLI锁寿命、原件保留引用、损坏/多XML失败恢复、慢AI/扫码SSE代理超时。
- 最终统一login路径为/auth/login；全包仅唯一ComicInfo候选可读，固定draft2.1输出，2.0只作离线对照。

## 规划状态与未执行项目

本轮仅更新Trellis规划资料，已有runtime修复和产品文件未被撤销或修改。没有运行Go、前端、E2E、真实OCR、模型推理、Telegram下载或阅读器产品验收。没有提交、push、PR、merge或生产部署。

实现仍须验证真实多图OCR、扩展字段MiniCPM提取及容器通道、来源权限/覆盖、tdl交接/硬额度/孤儿锁，以及Komga/Kavita实际读取；不能用本记录或历史七字段研究代替这些门槛。
