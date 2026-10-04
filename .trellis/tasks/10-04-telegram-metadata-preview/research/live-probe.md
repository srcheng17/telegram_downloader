# tdl 只读实测记录

## 当前结果：样例验证通过

2026-10-04 恢复 tdl 默认重连上限 5m 后，用户看到 `Login successfully!`，独立导出进程成功。已读到 10 条消息，目标 3870 存在且所有 raw.PeerID.ChannelID 与导出频道一致。未下载 Telegram 附件或网页图片。

| 证据 | 结果 |
| --- | --- |
| 3870 | 介绍帖，标题、4 行简介、5 个尾部标签 |
| 3870 / 3871 / 3872 | 同一相册；不能据此连接 ZIP |
| 3873 | ZIP，两行 caption 的作者一致，译名精确匹配 3870；附件 142.43 MiB |
| 3874 | 同名阅读帖，TextURL entity 指向 Telegraph |
| 3865 / 3868 / 3869 | 前一部作品，标题不同，被本地规则排除 |
| Telegraph | 只读取 HTML，HTTP 200；页面 h1 包含目标译名，51 个图片引用；尚未验证图片下载 |
| 本地解析 PoC | ready，author/comic_name/summary/tags 四字段；来源 3870/3873/3874，0 条歧义警告 |
| 既有 Go 输出链 | 实际调用 NormalizeMetadata、WriteComicInfoXML 并 XML 回读，四字段保留，验证通过 |

系列名、序号、类型没有证据，保持空值。真实 JSON、候选值和 XML 仅存本机私密目录，权限均为 0600；未进入 Git 或报告原文。合成样本的 7 项测试覆盖邻近作品隔离、歧义、作者冲突、emoji、链接白名单、int64 和字节上限。

当前是可行性原型，网页表单及 `/api/metadata/preview` 尚未实现。只验证了当前样例格式，不能等同于已支持所有频道、任意消息类型或直接 Telegram 媒体下载。

## 验证准备 · 2026-10-04

- 用户同意按“读取 → 关联 → 解析 → 预填”流程继续调研。
- 从 iyear/tdl 官方 v0.20.4 GitHub release 下载 MacOS arm64，发布 API digest 和 checksums 文件双重匹配。
- 归档 SHA-256：`7ea79e68c3f46a5dccdbd0356220a3a689cc00d715fbce9dcabde7f18bb99365`。
- 本机 `tdl version` 确认为 0.20.4、commit 9d7d49e、darwin/arm64；login/export 帮助核实 `-T qr`、`--all`、`--with-content`、`--raw` 和 ID 范围参数。
- 工具安装在 `~/.local/share/telegraph-downloader/research/tdl-v0.20.4/`，未加入系统 PATH、go.mod 或生产镜像。
- 本次 probe 位于 `~/.config/telegraph-downloader/telegram/tdl-probe-edd39364/`，目录 0700、umask 077；独立 storage 和 namespace，无现有 Desktop 会话读取。
- 初次创建的 `~/.tdl` 外层目录为 0700。tdl 固定写入其 log 子目录，显式 storage 参数不改变该日志位置；不打开 debug，不向报告输出原始日志。

## 登录与读取约束

私密 launcher 先执行二维码登录（最长 300 秒），成功后读取 MLSHHZ 的 3865–3875 ID 窗口（最长 60 秒），同时校验进程退出、JSON 解码、目标 3870 是否存在。二维码、可选 2FA 输入留在本机终端，不传到聊天。只保存消息 JSON，不下载附件、不发送消息、不加入频道。

验证工具明确拒绝控制 `com.apple.Terminal`，因此交由用户手动打开 launcher。用户已启动脚本，登录进程约 92 秒后退出 0，随后导出进程退出 1。真实失败原因是 `not authorized. please login first`，未生成消息 JSON；同会话只读重试一次仍相同。此时尚未请求频道历史，不能归类为频道内容限制。

## 授权失败诊断

- 默认 Info 日志显示导出恢复了已有密钥，两个进程 DC 相同；没有 DC 迁移或具体 SESSION_REVOKED 等错误码证据。
- `Connection restored from state / Key already exists` 只证明 MTProto 密钥存在，不证明账号已授权。
- tdl 的 `RunWithAuth` 将账号授权状态为 false 统一报告为上述错误；底层可把 Telegram 401 归为 false，此文本不足以细分原因。
- 源码确认退出码假阳性路径：tdl SIGINT 转换为 context cancellation；gotd 的 QR wait 返回该错误，但 `Client.Run` 对 context.Canceled 返回 nil。故登录退出 0 不能作为授权完成证据；尚不能证明本次发生过取消。
- 私密 wrapper 原先在退出 0 后显示“已登录”，现已修正为“登录命令已结束，正在核验授权并尝试只读消息读取”；遇到上述错误明确记录 `auth_required`。没有重新登录覆盖现有会话。
- 用户明确确认两轮都已扫描二维码并在手机确认，但终端未显示 `Login successfully!`；第二轮约 25 秒后正常退出，不能归因于用户未操作。

## 短重连上限与扫码后切换

- 原 probe 自定义 `--reconnect-timeout 20s`。已用全新未绑定账号的 namespace 做不扫码对照：40 秒后仍持续等待且二维码已渲染；测试主动 SIGINT 后退出 0。仅记录布尔指标和耗时，没有保存二维码 stdout。
- 源码与独立复核显示：QR.Import 如收到 `AuthLoginTokenMigrateTo`，会通过 ensureRestart 取消旧连接；backoff 的计时从上次 onReady 开始，ensureRestart 不重置它。超过 20 秒后这一主动取消可能使重试停止，随后被 Client.Run 吞掉为 nil。
- 因此 20 秒不是“扫码等待超时”，但会影响扫码后的必要连接切换；恢复默认参数后日志出现 `Restart ensured`，随后用户看到成功提示，独立导出也成功，与上述源码机制和对照相符。
- 已备份原专用会话，仅把该参数恢复 tdl 默认 **5m**；保留外层登录 300 秒、导出 60 秒硬时限，其他参数保持不变。问题由自定义短 backoff 上限触发，不是频道内容限制，也不应要求用户重复申请 API 应用。

相关源码：[QR.Import](https://github.com/gotd/td/blob/v0.140.0/telegram/auth/qrlogin/qrlogin.go#L87)、[ensureRestart](https://github.com/gotd/td/blob/v0.140.0/telegram/migrate_to_dc.go#L11)、[runUntilRestart](https://github.com/gotd/td/blob/v0.140.0/telegram/connect.go#L55)、[onReady](https://github.com/gotd/td/blob/v0.140.0/telegram/connect.go#L107)、[backoff stop](https://github.com/cenkalti/backoff/blob/v4.3.0/retry.go#L98)。

源码依据：[tdl SIGINT](https://github.com/iyear/tdl/blob/v0.20.4/main.go#L17)、[QR callback](https://github.com/iyear/tdl/blob/v0.20.4/app/login/qr.go#L47)、[gotd QR wait](https://github.com/gotd/td/blob/v0.140.0/telegram/auth/qrlogin/qrlogin.go#L180)、[Client.Run cancellation](https://github.com/gotd/td/blob/v0.140.0/telegram/connect.go#L189)。

## 后续实现边界

UI/API 接入位置、字段保护、异常契约和验收要求已整理到 [设计草案](../design.md)。下一步是把已验证规则移植到 Go 并接入表单，而不是在运行服务中增加 Python 原型依赖。当前目标是介绍帖；若粘贴附件帖或阅读帖，需要另加反向定位分支及回归样本。会话部署位置、单 namespace 串行读取和限流/失效提示也应在正式接入时落地。

运行产品代码尚未修改。本轮运行 7 项合成测试、真实 JSON 离线解析、既有 Go XML 输出验证及 diff 检查；没有重复运行与这些研究脚本无关的全量 Go/浏览器测试。
