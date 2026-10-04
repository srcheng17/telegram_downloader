# Telegram 只读访问验证记录

## 已确认的方案
- 网页 embed 返回 HTTP 200 但正文为频道限制提示，不能用于采集。
- 标准 Bot API 没有任意历史频道消息查询接口；计划使用用户 MTProto。
- gotd/td v0.162.0 已通过 Go module registry 核验并下载源码，要求 Go >=1.25，项目 Go 1.27.1 可满足。它是第三方库，不是官方 SDK。
- contacts.resolveUsername -> channels.getMessages 精确读目标；必要时 messages.getHistory 读取小窗口。不下载媒体、不发送消息、不加入频道。
- 消息关联不能仅依赖相邻：reply_to/grouped_id/显式链接优先，标题/文件名其次；无显式关系时需展示候选。entities 的 offset/length 是 UTF-16 code units。

## 本机准备
私密配置文件已建立在用户配置目录 `~/.config/telegraph-downloader/telegram/credentials.json`，文件权限 0600、专用目录初始权限 0700。模板不包含真实凭据。

```json
{"api_id": 0, "api_hash": ""}
```

api_id/api_hash 是应用身份，不等于账号登录。用户通过 https://my.telegram.org 的 API development tools 创建应用。官方说明每个号码当前只能关联一个 api_id；若刷新后已经出现应用详情，应复用该应用而不是重复创建。

用户授权操作后，通过 Safari 实际填写应用标题、合法短名、Desktop、用途描述并提交；补全项目 URL、换用唯一短名后仍失败。Web Inspector 确认 `POST https://my.telegram.org/apps/create` 返回 HTTP 200，正文仅为 5 字节 `ERROR`。表单字段实际已发送，未创建应用。响应没有具体原因，不能断言是代理、IP、短名或账号限制；已停止盲目重试。没有导出 cookies、HAR、登录会话或凭据。

用户随后要求同时调研免申请 API 凭据工具和下载/元数据工具。已发现 tdl v0.20.4 内置应用身份并支持二维码登录，官方 Telegram Desktop 支持 JSON 导出；详见 [开源方案比较](./open-source-options.md)。这些是替代访问路径，不代表目标频道已经通过验证。

后续登录会话只保存在上述专用目录；gotd 默认 session.FileStorage 是权限受限的明文文件，不宣称已加密。手机号、验证码、2FA 密码由用户在本机交互输入，不写配置或日志。

## 凭据就绪后的验证
1. 本机读取凭据，只输出格式验证是否通过，不回显值。
2. 首次由用户交互建立会话，后续复用，不扫描客户端现有 session。
3. 精确读取目标 ID，仅当返回真实 message 且可见才判定成功。
4. 读取至多 10-20 条候选原文并私密保存；向聊天只报告字段可用性和关联结果，不倾倒原文。
5. 区分缺失、私有/限制、登录失效和限流；不自动修改内容设置，不声称绕过访问限制。
6. 验证通过后再开发 /api/metadata/preview 与表单接入。

## 待完成
API 应用创建仍未完成，但 tdl 路径不依赖它。后续已完成独立二维码登录并成功读取目标；早先“未授权”由自设短重连上限影响扫码连接切换导致，恢复默认值后通过。已验证目标、附件及阅读帖关联和四字段解析，详见 [只读实测记录](./live-probe.md)。没有读取 Desktop 的 tdata；网页预填尚未实现。

## 来源
- https://core.telegram.org/api/obtaining_api_id
- https://core.telegram.org/method/channels.getMessages
- https://core.telegram.org/method/messages.getHistory
- https://core.telegram.org/api/entities#entity-length
- https://core.telegram.org/api/auth
- https://github.com/gotd/td/tree/v0.162.0
