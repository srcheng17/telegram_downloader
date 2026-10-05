# Telegram 开源工具比较 · 2026-10-04

## 结论

优先验证 **tdl v0.20.4 的独立命名空间二维码登录 + 有限 ID 范围消息导出**。它的源码内置应用身份，不要求用户先申请自己的 api_id/api_hash，适合暂时绕开 my.telegram.org 应用创建失败这一流程障碍。仍必须登录自己的 Telegram 账号，频道读取仍受该账号与 Telegram 服务端权限约束。

官方 **Telegram Desktop** 提供 JSON/HTML 聊天导出，也是免个人应用申请的路径，但通常需要 GUI 操作，适合作为离线导入备用。macOS 应选择 desktop.telegram.org 的跨平台 Desktop 版本，不能假设另一个原生 macOS 客户端有相同导出入口。

完整媒体下载器能下载附件，但不直接完成本项目的作品字段预填。长期 Go 原生集成可选 gotd/td；本轮不增加依赖或部署新服务。

## 比较

| 工具 | 个人 API 应用凭据 | 文本与附件能力 | 适配判断 |
| --- | --- | --- | --- |
| [iyear/tdl](https://github.com/iyear/tdl) | 内置应用身份；支持二维码/验证码登录，无需用户先申请 | 按消息链接下载；按 ID/时间范围导出 JSON；`--all --with-content` 包括纯文本与 caption，`--raw` 保留 MTProto 原始消息字段 | 首选验证工具；Go CLI，最新 release v0.20.4（2026-08-23），AGPL-3.0 |
| [Telegram Desktop](https://github.com/telegramdesktop/tdesktop) | 官方客户端内置，无需申请 | 聊天菜单导出历史为 JSON/HTML，可关闭媒体下载 | 最省配置的手工导出备用；持续维护，GPL-3.0 |
| [tangyoha/telegram_media_downloader](https://github.com/tangyoha/telegram_media_downloader) | 要求提供 | Web 进度/批量下载；`enable_download_txt` 仅保存无媒体的 text，不能据此假设图片介绍的 caption 也会导出 | 偏完整下载器，增加 Python 服务成本；最新 release v2.2.5（2025-01-06），主下载代码最近提交 2026-03-03，MIT |
| [Dineshkarthik/telegram_media_downloader](https://github.com/Dineshkarthik/telegram_media_downloader) | 要求提供 | Web UI、批量媒体下载；v3 使用 Telethon；下载历史库不保存正文/caption | 最新 v3.4.0（2026-02-24）；未找到通用文本导出或漫画字段映射，需要二次开发 |
| [Telethon](https://codeberg.org/Lonami/Telethon) | 要求提供 | Python 读取消息、caption、entities、grouped ID，可脚本导出 | 适合一次性 PoC；当前 PyPI 1.45.0；GitHub archived 是迁往 Codeberg，不代表停更 |
| [TDLib](https://github.com/tdlib/td) | `setTdlibParameters` 要求提供 | 官方结构化 JSON interface、消息链接解析、历史读取 | 仍持续维护；需要 C++/native library，本项目只做预填时成本偏高 |
| [gotd/td](https://github.com/gotd/td) | `telegram.NewClient(appID, appHash, …)` 要求提供 | Go MTProto，精确消息和小窗口读取，完整消息类型 | 长期 Go adapter 首选；v0.162.0 要求 Go >=1.25，项目版本满足；尚未集成 |

下载器的 metadata 通常是文件尺寸、日期、caption、下载路径，不等于作者、作品名、系列、标签或 ComicInfo。此次检查两个 downloader 的主程序、相关模块和文档共 57 个文件，未找到 ComicInfo/漫画字段流程；这仅是已检查候选范围内的结论。

## tdl 验证路径

以下是根据 v0.20.4 文档与源码核对的命令示例。后续已完成本机独立安装、账号登录和目标读取，详见 [只读实测记录](./live-probe.md)。首次登录显式使用 `-T qr`，因为裸 `tdl login` 默认可能导入 Desktop 登录数据。使用全新的专用命名空间，避免覆盖已有会话；保留默认重连 backoff，使用独立的外层总超时限制进程寿命。

```sh
tdl -n metadata-preview login -T qr
```

由用户在自己的 Telegram 设备确认二维码；若有 2FA，则在本机交互输入。会话放仓库之外并收紧权限，关闭 debug 日志，不把二维码、session 或 2FA 写入报告。

```sh
umask 077
tdl -n metadata-preview chat export -c MLSHHZ -T id -i 3865,3875 \
  --all --with-content --raw -o /私密目录/telegram-preview.json
```

- `--all`：默认导出仅媒体消息；不加会遗漏纯文本介绍。
- `--with-content`：默认最小 JSON 不带 text；加上才有正文/caption。
- `--raw`：按需保留 entities/reply/grouped_id 等关联证据；不要向聊天倾倒原始 JSON。
- ID 范围只表示候选窗口，不能保证每个 ID 都存在，也不能证明附近介绍就是目标附件。
- 源码历史 iterator 每批 100 条，输出 ID 范围不等于服务端只读取 11 条。如果产品需要严格限定网络读取条数，应使用 SDK 精确读取/自定义 limit。
- tdl 同一 namespace 的并发存储访问有限制，若接入服务，应串行使用专用会话，并限制进程并发与执行时间。
- 本次验证只读消息，不调用 `tdl dl`，不加入频道、不自动调整敏感内容设置。

下载能力也已从文档核实：`tdl dl -u https://t.me/MLSHHZ/3870`。这不是本轮执行过的操作，也不代表该链接一定包含可下载附件。

## 本项目最小接入方案

1. 粘贴链接后解析允许的 Telegram URL，得到频道和 message ID。
2. 获取目标与有限关联候选。首选显式链接/reply/album，次选标题和附件文件名；存在歧义时让用户选来源，不把“相邻”当作正确性证据。
3. 将 caption/text 归一化，再按实际频道格式提取作品名、作者、系列、简介、标签。entities offset 为 UTF-16 code units；不能按 Go 字节下标直接截取。
4. `/api/metadata/preview` 返回候选字段及来源，前端预填但允许修改，不覆盖用户已编辑值；确认提交后沿用现有元数据与 ComicInfo 生成链路。
5. 若正文包含 Telegraph 链接，可另行预填既有 URL 下载入口。Telegram 附件下载与文本预填分开评估；当前上传 64 MiB 上限不会因该方案自动改变。

首次 PoC 可通过独立 CLI 进程和私密 JSON 交换数据；不为此重写 Go API/worker/PG，不引入持续监听、OCR/LLM、通用插件层或完整 Python 下载服务。正式产品化前，确认 tdl AGPL-3.0 对实际集成/分发方式的要求；不能把“独立进程”当作自动免除许可证义务的结论。

## 已验证与尚未验证

已验证：工具文档、相关源码、公开 release/维护信息；tdl 登录模式和导出字段；Desktop 官方导出能力。

后续已实测：用户独立登录、MLSHHZ/3870 及附近消息读取、同名 ZIP/阅读帖关联、四字段离线解析与现有 Go ComicInfo 输出。其他候选未安装。没有读取既有 Desktop 会话，没有下载媒体，也没有宣称能绕过服务端内容限制。

尚未完成：正式 UI/API 接入、其他频道格式的准确性验证、任意附件链接的反向定位及实际媒体下载。

## 主要来源

- [tdl v0.20.4 release](https://github.com/iyear/tdl/releases/tag/v0.20.4)
- [tdl 二维码登录文档](https://github.com/iyear/tdl/blob/v0.20.4/docs/content/zh/getting-started/quick-start.md)、[登录实现](https://github.com/iyear/tdl/blob/v0.20.4/app/login/qr.go)、[应用身份选择](https://github.com/iyear/tdl/blob/v0.20.4/pkg/tclient/app.go)
- [tdl 消息导出文档](https://github.com/iyear/tdl/blob/v0.20.4/docs/content/zh/guide/tools/export-messages.md)、[导出实现](https://github.com/iyear/tdl/blob/v0.20.4/app/chat/export.go)、[下载文档](https://github.com/iyear/tdl/blob/v0.20.4/docs/content/zh/guide/download.md)
- [Telegram 官方 Desktop 导出介绍](https://telegram.org/blog/export-and-more)
- [tangyoha 纯文本导出条件](https://github.com/tangyoha/telegram_media_downloader/blob/master/media_downloader.py#L271)、[metadata 定义](https://github.com/tangyoha/telegram_media_downloader/blob/master/utils/meta_data.py)
- [Dineshkarthik 历史库定义](https://github.com/Dineshkarthik/telegram_media_downloader/blob/master/db.py#L26)
- [Telethon 迁移说明](https://github.com/LonamiWebs/Telethon/blob/v1/README.rst)、[PyPI](https://pypi.org/project/Telethon/)
- [TDLib 参数及消息 schema](https://github.com/tdlib/td/blob/master/td/generate/scheme/td_api.tl)
- [gotd v0.162.0](https://github.com/gotd/td/tree/v0.162.0)
