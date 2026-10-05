> 2026-10-05 范围调整：用户移除新建任务的 Telegram 消息下载来源，保留 tdl 登录并迁入「设置 → 连接」；旧 `/telegram` 跳至 `/settings#connections`。后端接口、历史任务及账号数据保留。后续验收按 [UI v2 设计](../10-04-modern-ui-shell/redesign-v2.md) 执行，不再要求首页 Telegram 来源入口。

> 执行更新（2026-10-05）：第二批已实现并完成本地集成检查，状态 review；实际外部验收仍未全部完成。结果以[第二批验收记录](../10-04-clipboard-ocr-tdl-ui/batch-2-verification.md)为准。下文的 planning/未执行描述保留为原计划背景。

# Telegram 单账号登录与 tdl 下载

## 目标与范围

在项目网页由已登录的单管理员完成 Telegram 扫码/可选两步验证，用固定官方 tdl 下载指定消息中的归档，并将确认后的扩展元数据交给现有 Task Core、解包、CBZ/ComicInfo 和 Komga 链路。

Batch 2，依赖 metadata-contracts 的 metadata_document/旧七字段兼容、source-settings-auth 的管理员和受保护配置、modern-ui-shell 的页面/草稿契约。首版单 Telegram 账号、每账号一个活跃 tdl/helper 进程，允许任务排队；账号状态与任务状态分离，不新增 Task Core 生命周期状态。

## 需求

- T1 提供账号状态、创建扫码、自动 QR 刷新/到期、可选 2FA、取消与重新连接。只有真实授权并经新进程恢复验证后才显示已连接；CLI exit 0、session 文件存在、二维码消失均不足以成功。
- T2 重新连接使用候选授权，不覆盖当前有效账号。取消、过期、密码错误、限流、网络故障、授权失效和忙碌分别反馈。页面刷新可恢复当前可公开状态；迟到事件不能覆盖新 attempt。
- T3 session、api_hash、密码和 QR token 仅在必要的受保护服务端/当前管理员通道使用；不进入日志、历史、数据库普通事件、命令行、环境变量、前端持久化或第三方 QR 服务。浏览器只得到短期 QR 与最少账号状态，2FA 输入提交后清除。
- T4 API/worker/状态验证/helper 跨进程串行使用账号；锁持有期间不可有第二个存储写入者。取消必须等进程真正退出、关闭存储后才能清理或切换账号；进程崩溃/锁丢失不能产生双写。
- T5 用户提供一个 Telegram 消息链接；首版每任务处理该消息中一个支持的 ZIP/RAR/7Z 附件，拒绝无附件、目录/批量消息和不支持媒体，多个候选不静默挑第一项。访问以账号实际权限为准，不修改 Telegram 内容限制设置或自动加入频道。
- T6 下载压缩源上限 500 MiB，可配置降低，不可在首版提高；下载前检查声明大小并在实际传输中限制字节。后续仍使用现有 300 页、25 MiB/页、500 MiB 总解包大小；浏览器上传维持 64 MiB，不能混用两个入口限额。
- T7 确认后的 metadata_document、来源链接/身份、下载运行设置在创建时固定。取消、重试、去重、lease owner/generation、artifact containment 与 Komga action 继续由 Task Core 决定；旧 Telegraph 和上传行为保持兼容。
- T8 下载和包装在 task/generation 隔离目录内；超限/取消/失败不发布半成品；用户可以从明确错误恢复或重试。新账号不得静默接管旧账号创建的任务，应反馈 account_changed。

## 独立验收

- [ ] T1/T2：合成 bridge 事件覆盖 QR 刷新、2FA、失败、取消、过期、乱序及重连；exit 0 但未授权不能晋升 active。独立 helper 关闭 KV 后，新进程恢复授权验证可成功，真实手动烟测证据单独记录。
- [ ] T3：所有账号操作受 admin+CSRF 保护；日志/HTTP/历史快照中不存在密码/session/api_hash/过期 token；QR 响应 no-store，第三方生成服务调用为零。
- [ ] T4：两个 worker 加一个 API 争抢同账号只启动一个进程；取消/崩溃/DB 连接丢失时不并行写存储，旧 lease/generation 不能切换账号或发布结果。
- [ ] T5/T6：有权限的单附件可进入现有链路；不可访问消息、无附件、多目标、格式不支持及大小未知/超限都有明确结果。500 MiB 边界和实际超限测试不需要真实大文件；142.43 MiB 级合成归档走 tdl 路径不受 64 MiB 上传限制误挡。
- [ ] T7/T8：重试保留创建时设置与 metadata_document；取消等待 Execute/子进程退出后清理再 ack；过时代际产物被拒绝，成功 CBZ 的页面/元数据及 Komga 复制通过既有资格规则验收。
- [ ] 独立 smoke：固定 helper 编译 → 隔离 candidate 扫码/2FA（若账号开启）→ 关闭 → 非登录新进程 Self/Auth.Status → 官方 CLI 下载一个用户授权的合成附件 → CBZ 字节/ComicInfo 核对；未执行必须明确记为未验证。

## 不在范围

多账号、并行同账号多进程、session 导入导出、网页终端/PTY 透传、频道自动爬取/订阅、自动加入频道、任意命令参数、下载更多媒体格式、扩大解包或上传限额、替换 Task Core 状态机。helper/CLI 交接尚未实测，不把既有 CLI 测试当作其验收。
