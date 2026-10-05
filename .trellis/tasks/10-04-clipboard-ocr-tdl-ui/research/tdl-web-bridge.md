# tdl 网页扫码与 2FA 桥接调研

状态：planning。2026-10-04。仅调研，不代表实现批准。本轮没有登录、读取 session、请求频道正文或修改产品代码。

## 首轮结论

**可以在保留官方 tdl 下载 CLI 的前提下实现网页扫码和 2FA，但官方 v0.20.4 没有可直接消费的网页登录事件接口。** 建议增加一个职责很小的 Go 登录 helper，复用固定版本 tdl 的公开存储与客户端包，向项目 API 提供结构化事件；下载继续调用固定版本官方 tdl。正式实现前先用隔离 namespace 完成 helper → 官方 CLI 的兼容性验证。

这是源码支持的设计建议，尚未编译或实测 helper。若复用上游 Go 包的依赖、初始化行为或构建成本不可接受，备选是对固定 tdl 源码增加很小的机器接口。把 ANSI 终端输出解析成正式网页协议不作为首选。

## 已核实：现有 CLI 的能力和边界

核对版本：`iyear/tdl v0.20.4`、独立模块 `iyear/tdl/core v0.20.4`、其依赖 `gotd/td v0.140.0`、`survey/v2 v2.3.7`、`bbolt v1.3.10`；上游 Go 要求为 1.25.8。沿用本任务 [context.md](context.md) 中已有的官方 CLI 登录与读取成功证据；本轮不重新验证账号。

| 项目 | 源码核实结果 |
| --- | --- |
| 机器接口 | `cmd/login.go:38–43` 只有 type、desktop、passcode 和旧 code 参数，没有 JSON、QR URL、二维码图片或事件输出参数；root 的全局参数也没有提供这些接口。[T1][T3] |
| QR 输出 | `app/login/qr.go:51–60` 得到 typed token 后调用 `token.URL()`，编码为终端半块字符并打印光标上移序列；未输出 URL 或 expires。不能把普通 stdout 当作稳定 JSON/行协议。[T2] |
| 自动刷新 | gotd `QR.Auth:172–202` 根据 token 的 expires 设置 timer，到期重新 Export，再调用 show callback。刷新发生在同一个 client/login attempt 中，不需要另起登录命令。[G1] |
| 网页可用数据 | gotd `Token.Expires():52–55`、`URL():70–72`、`Image():74–80` 都是公开接口。helper 可直接发送到期时间并生成图片，无需 OCR/解码终端二维码。[G2] |
| 2FA | tdl `qr.go:73–90` 遇到 `SESSION_PASSWORD_NEEDED` 后用 `survey.Password` 读取一次密码，再调用 `c.Auth().Password`。密码错误返回错误并结束当前官方 CLI 流程，没有网页请求接口或内置交互重试循环。[T2] |
| passcode 的含义 | `--passcode/-p` 是 Desktop 本地数据密码，不是 Telegram 账号 2FA 密码参数，不能拿来做网页 2FA 传递。[T1] |
| 终端依赖 | survey `password.go:51–64` 操作终端模式并读取输入；`terminal/runereader.go:59–62` 还查询终端尺寸和光标。简单向 stdin 管道写密码不能当作已验证的稳定集成；PTY 可支持终端交互，但仍需状态解析。[S1][S2] |
| 成功与取消 | tdl `qr.go:93–100` 在 `Self` 成功后才打印原生成功提示；gotd `connect.go:189–193` 会吞 `context.Canceled`。网页必须保存显式授权完成状态并真实验证，不能以 exit 0、二维码消失或 DB 文件存在判定成功。[T2][G3] |

不能承诺“手机已扫描”是独立可观测状态：当前 helper 回调直接提供的是 QR token；收到 login-token 更新或进入 2FA/授权完成才有进一步证据。网页使用“等待手机确认”等保守状态，不伪造扫描进度。

## 桥接方案比较

以下为方案推论，不是已经存在的上游接口。

| 方案 | 可复用部分 | 额外成本和边界 | 判断 |
| --- | --- | --- | --- |
| 官方 CLI + PTY/终端解析 | 完全保留官方二进制与登录逻辑 | 必须处理 ANSI 光标、分块 stdout、二维码刷新、2FA 提示、终端尺寸及异常退出；输出没有 expires，二维码 URL 本身也不包含可解析的到期时间；把完整终端转发到网页还会暴露不需要的账号/诊断内容 | 可做短期演示，正式产品不推荐 |
| 小型上游改动/固定版本补丁 | 保留 tdl 登录、存储和下载实现 | 在 QR callback 输出结构化 token/期限、用受控输入获取 2FA、显式区分 cancelled/failed/authenticated；必须将机器事件与普通提示隔离，维护构建和版本补丁 | 可靠备选；不能称为未修改官方二进制 |
| 独立 Go 登录 helper + 官方下载 CLI | 复用公开 `pkg/kv`、`pkg/key`、`pkg/tclient`，底层仍是同版本 gotd | 需要一个小型事件/输入协议及账号进程协调器；必须锁定依赖并验证与官方 CLI 的交接；不能假设这些包有长期稳定兼容承诺 | 首轮建议 |
| 完全独立 gotd 存储/自行转换 session | gotd 提供原生 QR/2FA 回调 | 自己承担 tdl app 身份、存储 key、编码版本和迁移契约；与本项目需求相比增加兼容维护 | 不作为首版默认方案 |

helper 可直接使用与官方 QR 相同的装配：创建 bolt engine → 打开服务端生成的 namespace → 写 `key.App() = AppDesktop` → `pkg/tclient.New(..., login=true)`。这些包不在 Go `internal/` 下；`pkg/tclient` 已配置 `core/storage.NewSession`，因此不必复制 session JSON 格式。root/core 是独立 Go 模块，须一起固定版本，不能只固定下载二进制版本。[T2][T4][T5]

不建议通过网页上传 `tdl backup/recover` 文件来传递登录：它面向整个 storage 的 namespace 集合，并非单个网页登录 attempt；恢复会覆盖同名键。复用相同存储接口更直接，也避免新增高敏感凭据文件交换入口。[T8]

## 最小可靠网页流程建议

1. 管理员启动一个短期 login attempt。API 生成 attempt ID 和临时候选 namespace，浏览器不能选择本地路径、命令、namespace 或任意 CLI 参数。重新连接不直接覆盖当前正在服务的有效 namespace。
2. helper 的 QR callback 输出结构化 `qr` 事件：generation、expires_at，以及只供本次管理员连接使用的 QR URL 或本地生成图片。过期后替换旧码；终止 attempt 时清除页面和服务端内存中的 token。
3. 遇到 `SESSION_PASSWORD_NEEDED` 输出 `password_required`，等待同一管理员提交。密码仅经受保护的请求和 helper 私有输入通道传递，不放命令行参数、环境变量、持久化状态或日志；不将完整 PTY 开放给网页。错误密码允许用户明确重试，限流与超时单独反馈，不自动暴力重试。
4. `Self/Auth.Status` 成功后标记“已验证授权”，结束 client 并关闭整个 KV engine；再以非登录模式重新打开该候选 namespace 做一次真实授权读取，验证跨进程恢复。验证成功后才切换项目的 active namespace。只有 QR 完成事件或 exit 0 仍不足以完成交接。
5. 页面状态至少区分 waiting_qr、password_required、verifying、connected、cancelled、expired、failed；状态由服务端控制。成功状态展示最少账号信息；网页不接收 session、api_hash 或原始运行日志。
6. 事件可经同源 SSE 加普通 POST 输入实现，不要求双向终端/WebSocket。事件序号和 attempt ID 防止迟到结果覆盖新 attempt；页面刷新可读取当前状态，旧二维码及密码不作为历史事件长期保存。

这些登录接口必须处于明确的管理员访问边界内，并对创建 attempt、提交密码和取消操作做鉴权及跨站请求保护；二维码响应和事件不缓存、不经过第三方 QR 生成服务。访问控制采用何种现有部署机制，由主任务统一确定。

## 存储、下载并发与退出约束

- **同 namespace 的 CLI/helper 必须串行。** tdl bolt 文件是 `storagePath/namespace`；以可写方式打开，锁超时 1 秒。即使只是查授权状态，也会占用同一文件，不能在下载进程运行期间再打开一次。[T6][T7]
- 负责 `Close()` 的是 `pkg/kv.Storage` engine，不是 `core/storage.Storage` namespace 接口。helper 必须在所有成功、失败、取消路径关闭 engine，并等子进程退出后再移交下载。不能只取消 goroutine 就认为锁已释放。[T4][T6]
- 最小首版是每个账号最多一个活跃 tdl 进程；多个下载任务由已有任务系统排队。若需要传输内部并发，使用单次 tdl 调用自身的能力，不通过复制 session/namespace 绕开文件锁。跨 API/worker 进程的协调要由主任务设计，不能只放一个进程内 mutex。
- 账号页在下载期间可显示“上次验证时间/当前忙碌”，不为页面轮询反复打开 DB。授权失效以实际下载/验证返回为准，区分 busy、auth_required 和网络错误。
- `login=true` 的 SessionStorage 不加载既有 session；这正是候选 namespace 与 active namespace 分离的原因。取消时清理候选资源必须在进程退出、锁释放后执行。[T5]
- 保留 tdl 默认 `--reconnect-timeout 5m`，另设整个 attempt 的应用级期限。已有实测已暴露短 20s backoff 与扫码后计划重启相互影响，不能把该参数当整个登录倒计时。SIGINT/上下文取消后的 exit 0 必须映射为 cancelled，不能晋升 active namespace。[T3][G3]
- `--storage` 只改变 KV 目录，不改变 tdl 默认日志目录；服务运行身份、目录权限和日志目的地要在部署中明确。网页不回显默认 Info 日志；其中可能包含账号标识。[T3]

## 实现前必须验证的技术点

- helper 对固定 tdl Go 包的独立编译、Go 版本和目标 Linux amd64/arm64 构建；导入包的初始化/默认目录行为及 logger 注入。当前只是源码可访问性成立，未完成编译 PoC。
- helper QR → 可选 2FA → 关闭存储 → 官方 CLI 重新恢复授权 → 限定真实下载的完整交接。既有官方 CLI 实测不能代替此验证。
- 不扫码过期刷新、扫码后 DC 迁移、错误密码与限流、取消、进程崩溃、浏览器重连；尤其不得把取消或重连耗尽误报成功。
- 下载持锁时的登录/状态查询策略、多 worker 争抢、异常退出释放锁及 active namespace 切换；实现前明确单账号还是多账号范围。
- gotd v0.140.0 的 QR 初次 Export 直接返回 MigrateTo 等已知边界，以及后续升级兼容性；本轮没有为绕开边界自动升级依赖。
- 若选择机器模式补丁，验证 stdout 只含机器帧、stderr 不含密码/token、分块消息和重复事件处理；若选择 helper，同样执行协议测试，避免将普通 CLI 输出重新变成隐式协议。

## 固定版本来源

- [T1] [tdl v0.20.4 cmd/login.go](https://github.com/iyear/tdl/blob/v0.20.4/cmd/login.go#L24)：24–49，登录参数和覆盖提示。
- [T2] [tdl v0.20.4 app/login/qr.go](https://github.com/iyear/tdl/blob/v0.20.4/app/login/qr.go#L24)：24–101，QR、AppDesktop、2FA 和 Self。
- [T3] [tdl v0.20.4 cmd/root.go](https://github.com/iyear/tdl/blob/v0.20.4/cmd/root.go#L73)：73–119、150–169，日志、storage、关闭和默认重连参数。
- [T4] [tdl v0.20.4 pkg/tclient/tclient.go](https://github.com/iyear/tdl/blob/v0.20.4/pkg/tclient/tclient.go#L24)：24–52，应用身份及 SessionStorage 装配。
- [T5] [tdl v0.20.4 core/storage/session.go](https://github.com/iyear/tdl/blob/v0.20.4/core/storage/session.go#L17)：17–41，login 模式及 session key。
- [T6] [tdl v0.20.4 pkg/kv/bolt.go](https://github.com/iyear/tdl/blob/v0.20.4/pkg/kv/bolt.go#L132)：132–172，每 namespace 文件、打开与关闭。
- [T7] [tdl v0.20.4 pkg/kv/legacy.go](https://github.com/iyear/tdl/blob/v0.20.4/pkg/kv/legacy.go#L16)：16–20、150–153，1 秒锁等待、事务写入。
- [T8] [tdl v0.20.4 app/migrate/backup.go:16–39](https://github.com/iyear/tdl/blob/v0.20.4/app/migrate/backup.go#L16) 与 [app/migrate/recover.go:17–44](https://github.com/iyear/tdl/blob/v0.20.4/app/migrate/recover.go#L17)，以及 `pkg/kv/bolt.go:56–104` 的整库导出/恢复路径；仅作为不采用该交接方式的依据。
- [G1] [gotd v0.140.0 qrlogin.go](https://github.com/gotd/td/blob/v0.140.0/telegram/auth/qrlogin/qrlogin.go#L77)：77–120、143–203，Import/DC 迁移、刷新和等待。
- [G2] [gotd v0.140.0 token.go](https://github.com/gotd/td/blob/v0.140.0/telegram/auth/qrlogin/token.go#L44)：44–80，期限、URL 和图片接口。
- [G3] [gotd v0.140.0 connect.go](https://github.com/gotd/td/blob/v0.140.0/telegram/connect.go#L107)：107–114、174–193，backoff reset、取消及返回值。
- [S1] [survey v2.3.7 password.go](https://github.com/AlecAivazis/survey/blob/v2.3.7/password.go#L38)：38–64，终端密码输入。
- [S2] [survey v2.3.7 terminal/runereader.go](https://github.com/AlecAivazis/survey/blob/v2.3.7/terminal/runereader.go#L59)：59–62、92–131，终端查询、读取与中断。
