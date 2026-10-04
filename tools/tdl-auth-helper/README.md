# Telegram 授权 helper

此独立 Go module 固定使用官方 `tdl v0.20.4`、`tdl/core v0.20.4` 和 `gotd v0.140.0`。API/worker 不直接链接 Telegram SDK。helper 的 JSONL stdin/stdout 是私有进程协议，不能作为公开 HTTP 服务或人工调试日志使用。

## 构建与自动验证

在本目录执行：

```sh
go test -race ./... -count=1
go vet ./...
go build -mod=readonly -p 1 -o /tmp/tdl-auth-helper .
/tmp/tdl-auth-helper --version
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -mod=readonly -p 1 -o /tmp/tdl-auth-helper-linux-amd64 .
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -mod=readonly -p 1 -o /tmp/tdl-auth-helper-linux-arm64 .
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -mod=readonly -p 1 \
  -ldflags='-s -w -X github.com/iyear/tdl/pkg/consts.Version=0.20.4 -X github.com/iyear/tdl/pkg/consts.Commit=9d7d49e -X github.com/iyear/tdl/pkg/consts.CommitDate=2026-08-23T17:15:46Z' \
  -o /tmp/tdl-linux-amd64 github.com/iyear/tdl
```

官方 CLI 与 helper 从同一个固定模块依赖图构建。不要单独替换运行时 CLI 为其他版本。

## 可复现的人工只读验收

使用隔离数据库与新的 `0700` Telegram 私有目录启动项目测试部署，启用 `TELEGRAM_ENABLED`，指向上面构建的两个二进制。遵循项目 `docs/development/admin-access.md` 初始化管理员并登录，通过页面的 Telegram 账号入口执行以下步骤；不要复用生产 tdl 的 namespace。

1. 点击扫码连接，在 Telegram 手机客户端确认自己的账号。若提示两步验证，仅在受保护的密码框中输入。二维码和密码不复制到命令行、截图、测试报告或日志。
2. 等待页面显示连接完成。此状态要求 helper 的 `Auth.Status` 和 `Self` 明确成功、helper 退出并关闭 Bolt 存储、独立的新 helper 进程再次验证成功，然后才执行数据库 CAS promotion。
3. 停止并重启 API，重新打开账号页。重启后的首次读取必须重新启动 verify helper；磁盘上存在会话文件不代表已登录。
4. 提交一个本人有权访问、只有单个 ZIP/RAR/7Z 附件的确切帖子链接，核对任务成功、产物可下载、源文件大小和散列验证通过。可使用 65 MiB 合成无敏感内容附件验证它独立于浏览器 64 MiB 上传限制；最大源文件硬上限是 500 MiB，只可向下配置。
5. 下载期间在另一页面请求登录应提示忙碌；取消任务后确认官方 CLI 已退出、账号锁释放、后续任务可执行。发起新的登录后取消，旧账号的 revision/identity 必须保持不变。
6. 删除该隔离部署及合成源文件。账号登出/撤销 Telegram 会话由账号本人通过 Telegram 完成。

本验收不发送 Telegram 消息、不加入频道、不修改 Telegram 内容过滤设置。当前实现使用服务器直连网络；没有启用凭据代理配置。受限网页能否通过 MTProto 读取，仍以账号本身的访问权限和 Telegram 服务端响应为准。

## 进程与存储约束

- API/worker 必须先取得专用 PostgreSQL advisory lock 和 OS flock，再通过 FD 3 继承同一打开文件描述符给 helper。禁止脱离该 coordinator 直接操作私有 namespace。
- QR/2FA 只在本次进程内存和受保护的实时状态响应中存在。失败候选存储在 helper 退出后、仍持有账号锁时删除；已生效的账号 namespace 保留。
- 下载 launcher 为官方 CLI 设置内核 `RLIMIT_FSIZE`，只传一个 URL、不展开媒体组，使用固定 `source` 文件名；所有子进程必须回收后才能释放账号锁。
- CLI 成功退出仍需验证唯一普通文件、实际大小等于预检声明值、上限、SHA-256。Task Core 负责 generation fencing、取消、打包和产物发布。

自动测试覆盖真实子进程的文件大小限制和继承锁，以及根项目内的数据库 CAS、三进程互斥、PG 连接丢失、协议边界、HTTP/SSE 与前端生命周期。上述真实扫码/2FA/Telegram 附件链路仍需人工完成；自动测试通过不能替代它。
