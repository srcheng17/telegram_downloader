# mediactl：工作台命令行与 AI 调用

`mediactl` 是本项目网页的客户端入口，调用同一管理员 HTTP API。它不直连 PostgreSQL、Komga 文件挂载或 tdl 私密卷。任务、设置与 Komga 写回由服务端判定；本地截图 OCR 和有限识别规则在客户端执行。

项目 AI skill 位于 `.agents/skills/media-workspace-cli/SKILL.md`，供在本仓库工作的代理发现；npm 安装包只安装 CLI，本身不配置 AI 代理。

## 安装与连接

客户端需要 Node.js 22 或更新版本。建议从与服务端**同一提交或发布包**安装；有副作用的命令会先读取服务端 CLI 协议版本，不兼容或无法读取时拒绝写入。仓库内开发可执行 `npm ci`、`npm run build:cli`、`npm run test:cli`，然后使用 `node cli/mediactl.mjs --help`。生成可安装包可执行 `npm pack`，在客户端用 `npm install --global ./telegram-downloader-frontend-0.1.0.tgz` 安装实际生成的文件；安装后运行 `mediactl --version`。包中包含 CLI 所需的前端纯逻辑和 `web/static/ocr` 离线资源；`build:cli` 会校验四种语言资源的字节数和 SHA-256。

第一次登录在**用户自己打开的终端**执行 `mediactl --server https://工作台域名 auth login`，密码隐藏输入。之后普通命令沿用本机受限会话；本机 HTTP 必须在每次命令显式加 `--allow-insecure-loopback`，且目标只能是回环地址。远端只允许 HTTPS。业务写请求会先只读核验服务端 `GET /api/client-contract` 的协议版本；旧版或不兼容服务端会拒绝写入。`--json` 输出 `{ok,code,data}`，失败写 stderr 并按输入、认证、冲突、服务、传输或交互需求返回不同退出码。非交互模式不会收集密码、2FA 或 API Key；不要把这些值放在 argv、JSON、环境变量、管道或 AI 对话中。

会话位于 `~/.config/mediactl/session.json`（目录 0700、文件 0600）；本机内存工作区的 Unix socket 位于 `~/.config/mediactl/workspaces/`。`auth logout` 撤销会话并关闭本机工作区；`auth password-change` 同样清理本机会话。内存工作区闲置约 30 分钟后销毁，意外退出无法恢复 OCR 原文。升级客户端前先结束正在编辑的工作区；升级不会删除服务端任务或元数据。

## 业务流程

`mediactl --help` 是实际命令清单。常用只读命令有 `auth status`、`overview`、`tasks list/show`、`metadata schema/providers/history`、各设置的 `get/list`，以及 `connections telegram status`、`connections komga status`。词句、字段值和截图正文不出现在默认 JSON 中；需要核对它们时，在独立终端使用 `workspace review --workspace ID` 或 Komga 的 `review` 命令。AI 工具创建的 PTY 会被工具读取，不能当作独立用户终端。

截图可多次传 `--image`，Mac 还可使用 `--clipboard`；其他系统用图片文件。支持 PNG/JPEG/WebP，单图最多 10 MiB、1200 万像素和 8192px 边长，最多 10 张且整组不超过 50 MiB。`workspace start --json` 得到工作区 ID，后续图片、文字、候选和草稿命令带 `--workspace ID`；写操作还带当前 `--revision N`。在独立终端完成图片校对、候选字段核对和 AI 发送预览。公开书目搜索及 AI 提取会访问已保存的外部目标，按本次用户意图发起；AI 只发送经确认的文字，不发送图片。

连续操作可使用 `workspace attach --workspace ID --jsonl`：stdin 每行一个 JSON 对象 `{"op":"status"}`，有参数时传 `args`，写入时还传 `expected_revision`；stdout 每行返回带 `seq`、`ok`、`code` 和安全 `data` 的结果。操作名使用 `image-add`、`text-set`、`candidate-adopt` 等 kebab-case。流式接口默认拒绝 `review`、`document`、`ai-review`、`ai-extract`；敏感正文和 AI 发送确认仍在独立终端完成。用受控文件或结构化 stdin 传 JSONL，不把自由文本拼进 shell 命令。若 AI 工具只有 shell 命令入口，可固定执行 `node .agents/skills/media-workspace-cli/scripts/run_args.mjs`，再通过 stdin 发单行 JSON 参数数组；该程序不经 shell 展开参数。密码、API Key、2FA 不得经此入口传入。

已在工作区采用的草稿可直接提交：`workspace tasks create-url --workspace ID --revision N --url URL` 或 `workspace tasks upload --workspace ID --revision N --file PATH`。机器调用可用 `--input-json -` 从 stdin 传入 `{ "url": "…" }` 或 `{ "file": "…" }`，也可带布尔值 `accept_partial`；不能与相应的 argv 值混用。JSONL 对应 `task-create-url` 和 `task-upload`。提交前会重新校验当前字段定义、草稿修订和未完成图片；只有明确 `accept_partial` 才允许忽略未完成图片。重复 URL 返回 `snapshot_attached:false`，表示没有新建任务，也没有把当前草稿附到旧任务；确认重抓后同时给 `--force` 与 JSON 中的 `"force":true`（或纯 argv 用 `--force`）。上传中途失败会保留草稿及已创建的任务 ID，供查询和重试判断。提交后用 `tasks show --id ID` 核对网页共享的任务快照。

默认机器输出不含 OCR 和元数据正文。仅当用户明确要求 AI 解读某一段内容时，可先调用 `workspace reveal prepare --workspace ID --revision N --kind field --id KEY`（也支持 `merged`、`image`、`candidate`、`record`，按需提供 ID），取得只含 UUID、修订和字节数的票据；再调用 `mediactl --json workspace reveal consume --workspace ID --revision N --ticket UUID --to-ai-context`。第二步会把**所选内容**放入机器输出和 AI 上下文。票据限 60 秒、最多 256 KiB、只可使用一次；工作区修订变化即失效。JSONL 不支持 reveal，普通 `review` 仍只在用户独立终端显示。

创建 Telegraph 任务可用 `tasks create-url --url URL`；含元数据的提交用 `--input-json -` 或 `--input-file PATH` 传结构化 JSON。已有可用下载时先返回确认态，只有明确加 `--force` 且 JSON 中也为 `force:true` 才重抓。压缩包用 `tasks upload --file PATH`，CLI 会检查上传附着和任务回读；任务下载用 `tasks artifact-check` 与 `tasks artifact-get --id ID --output PATH`，默认不覆盖已有文件。异步取消、重试或复制到 Komga 后，继续用 `tasks show/wait` 核对最终状态。失败任务的完整错误详情可在用户独立终端运行 `tasks error-review --id ID` 查看；普通 JSON 只给有界状态摘要。

七类设置与连接入口分别是 `settings download/sources/ai/fields/rules`、`connections telegram`、`connections komga` 和 `auth password-change`。`settings sources review --provider ID`、`settings ai review`、`settings fields review`、`settings rules review` 只在用户独立终端展示完整的非秘密配置；AI 和普通 JSON 应使用 `list/get` 的安全摘要。设置更新使用当前版本做 CAS；冲突时原输入仍在本地，重新读取后再决定是否修改。来源、AI、Komga 凭据替换使用 `--credential replace`，只在独立终端隐藏输入。Telegram 扫码登录也只在该终端运行，完成后用 `connections telegram verify` 回读。

## Komga 作品与 CBZ 写回

先读 `connections komga status`，如需看已保存地址，在独立终端使用 `connections komga review`；`connections komga test --config-version N` 测试已保存版本。配置连接的普通 JSON 仅含 `expected_version` 和 `base_url`，凭据用 `--credential replace` 隐藏输入。允许书库及共享挂载映射仍由服务端部署配置限制，CLI 不接受任意文件路径。

用 `komga libraries list` 找书库 ID，`komga books list --library-id ID --page 0 --size 25` 分页查找。`komga books show --id BOOK_ID` 返回安全摘要；`komga books edit --id BOOK_ID` 返回可编辑字段、`source_version` 和 `definitions_version`，而 `komga books review --id BOOK_ID` 在独立终端显示文件和当前 ComicInfo 字段。非 CBZ、无映射或文件检查失败时保持只读。

编辑输入保存为 JSON 文件，示意如下；字段状态必须为 `value` 或 `cleared`，未列出的字段保持不变：

```json
{
  "source_version": "从 books edit 得到的 64 位文件哈希",
  "definitions_version": "从 books edit 得到的版本",
  "changes": [{"key": "summary", "state": "cleared"}]
}
```

先运行 `komga metadata review --id BOOK_ID --input-file PATH`，在独立终端核对差异；`preview` 可在非交互模式取得不含字段值的差异摘要。两者都返回短时 `preview_token`。保存时将**原样的三项编辑输入**加上该 token 和一个稳定的随机 `idempotency_key`（16–128 个字母、数字、`-` 或 `_`），运行 `komga metadata update --id BOOK_ID --input-file PATH`。若保存响应中断，保留同一个输入文件和幂等键再查询或重试，避免创建第二次写回。返回的 `operation.id` 可用于 `komga edits status --id OPERATION_ID`；只有状态和标志确认文件与 Komga 同步都完成时才视为完成。`sync` 只在 `available_actions` 含 `retry_sync` 时执行，`restore` 只在含 `restore` 时执行；CLI 先核对资格、执行后再回读。若结果为 `verification_pending`，先查状态，不能根据提交响应判断最终完成。

若 `books edit` 返回 `page_count_correction_needed:true`，说明 ComicInfo 页数与实际图片数不一致。需在预览和保存的 JSON 都显式加入 `"correct_page_count":true`；只修正页数时 `changes` 可为空数组。`preview` 的 `page_count_correction:true` 与 `diffs` 中的 `page_count/corrected` 表示此次会修正页数，仍应在独立终端核对实际前后值。未显式确认时服务端拒绝写回，不会暗中改动页数。

CBZ 写回有独立备份、原子替换和恢复检测；恢复命令调用服务端受保护接口，不从 CLI 直接复制备份。服务器回滚或文件恢复请按 Komga 任务的运行手册执行，不能通过卸载 CLI 还原 CBZ。卸载客户端可用 `npm uninstall --global telegram-downloader-frontend`；如要清除本机登录状态，先运行 `mediactl auth logout`，确认没有未完成草稿后再删除本机配置目录。服务端数据库、任务和 Komga 文件不随客户端卸载删除。
