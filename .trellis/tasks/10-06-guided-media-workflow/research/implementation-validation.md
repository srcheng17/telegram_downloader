# 实施验收记录

用户已于2026-10-07确认实施。当前改动在 `vigorous-mayfly`，保留此前31项未提交修改；实现验收阶段未发布生产、未处理真实作品、未push或合并。验收后进入提交与PR审阅阶段，生产保持不变。

## 已验证行为

- 同一内存工作区的材料/自动准备/核对/结果步骤；返回不销毁OCR、图片顺序或手工字段。迟到准备不会抢页面和焦点。
- 默认六字段及已启用书目源，caption/规则/AI结果统一汇入一份核对稿；有直接证据的空字段预填，人工锁/clear和冲突需要明确决定。
- 无冲突常规路径在材料输入完成后只有两次流程点击：“下一步”进入自动准备，“确认并开始”提交。无需挑模型/字段/来源/逐候选采用。返回再前进不重复AI请求；这是浏览器用例计数，不包含选文件或输入文本本身。
- 相比旧的单候选路径，从已有OCR到提交至少要本地提取、核对候选、勾字段、采用、开始、查看任务六个点击动作（仅代码基准，不是用户计时实验）。有多个候选、AI和书目时旧路径更多，不报告未经实测的节省百分比。
- 最终确认前没有upload init或任务创建。确认固定metadata/source/file hash/target和幂等键，服务器原子记录提交回执。上传init服务端成功但响应丢失仍恢复同task。
- 高级候选与自动候选共享authority最终复验；设置变化、field/input变动、手工保护和取消均覆盖。手工/自动面板ownership避免历史候选被自动订阅覆盖。
- 已有相同元数据URL任务直接复用并显示结果，不要求第二次确认；元数据不同返回冲突，不吞掉新快照。
- 任务资格由Task Core提供；Komga copy/pending/verified分开，只有真实book/library IDs及内容读回才已收录。

## 检查

| 检查 | 实际结果 |
|---|---|
| `npm run test:frontend` | 273/273 |
| `npm run test:cli` | 73/73 |
| frontend/CLI ESLint | 通过 |
| `npm run build`、`build:cli` | 通过，已生成最新dist；OCR 15资源哈希通过 |
| 隔离PG `go test -p 1 ./... -count=1` | 通过，含真实提交回执/并发仓储测试 |
| 隔离PG `go test -race -p 1 ./... -count=1` | 通过；后续仅AI提示改动另跑metadataextract race通过 |
| `go vet ./...` | 通过 |
| 真Komga1.28.1 `bash scripts/check_komga_delivery.sh` | 通过，见独立报告 |
| 真WASM `node scripts/check_ocr_browser.mjs` | 通过，含暗色/低清/浅色对照、离线和零外发 |
| 32项隔离Compose浏览器 | 最终32/32通过（36.3秒），含一次确认后真实Komga自动交付及丢init响应恢复同task |
| CLI打包dry-run | 58文件，包含共享OCR text模块，无私有/secret/session文件 |
| `git diff --check`、task context validate | 通过 |

数据库测试最初以Go默认package并行运行，多个包在同一隔离库争抢migration advisory lock出现测试死锁；串行package后所有包通过。没有为此改业务锁或生产库。

独立trellis-check已检查新流程、候选采用/最终验证、提交幂等、图像边界及Go/JS契约，发现问题均补回归；最后完整E2E已通过，测试容器、私网、临时凭据和书库均已清理。

## 精度和范围

原图四核心字段匹配；低清/浅色仍有漏字，结果可编辑且未知不猜。AI受控CPA探测修复摘要证据失败，但该例主标题仍由本地caption补充。生产尚未部署本轮提示、向导、数据库migration或bundle。

完整浏览器链路的实际worker CBZ与Komga共享目录文件逐字节一致；再次copy保持同inode/mtime/hash、同bookID，最后仅1个任务和1本书。该测试使用真实API、PG、worker、Komga，不用mock替代交付。有限日志 `/tmp/guided-e2e-complete.log` 为0600。
