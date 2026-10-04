# 管理员登录与凭据维护

应用使用单管理员会话。除了登录、必要静态资源、`/healthz` 和 `/readyz`，业务页面、API、附件与事件流都要求登录；写操作还需要同源 Origin 与 CSRF。会话最长 12 小时，空闲 30 分钟过期。没有默认密码、公开注册或绕过登录的生产开关。

## 首次启动

准备 `.env` 中的 `APP_PUBLIC_ORIGIN`、`APP_UID`、`APP_GID`，以及两个私密文件。远程使用必须配置实际 HTTPS origin；本地开发可明确设置 `APP_PUBLIC_ORIGIN=http://127.0.0.1:5002` 与 `ALLOW_INSECURE_LOOPBACK=true`。默认网关只绑定 `127.0.0.1`；远程 TLS 入口及绑定地址由操作者明确配置。

密码至少 12 个字符、最多 72 个 UTF-8 字节。主密钥是随机 32 字节的标准 base64 编码。下面的命令交互读取密码，不回显、不放在命令参数中，且拒绝覆盖已有文件：

```sh
python3 - <<'PY'
import base64
import getpass
import os
from pathlib import Path

folder = Path('secrets')
folder.mkdir(mode=0o700, exist_ok=True)
password = getpass.getpass('初始管理员密码：')
if len(password) < 12 or len(password.encode()) > 72:
    raise SystemExit('密码长度不符合要求')
values = {
    'admin-bootstrap-password': password,
    'source-settings-master-key': base64.b64encode(os.urandom(32)).decode(),
}
for name, value in values.items():
    with os.fdopen(os.open(folder / name, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600), 'w') as handle:
        handle.write(value + '\n')
PY
```

两个文件必须为普通文件、非符号链接、权限 `0400` 或 `0600`，并归 API 进程的数值 UID 所有。默认容器 UID/GID 为 `10001:10001`；以默认配置部署时，在启动前将两个文件的属主设置为该值。若设置为宿主用户的 UID/GID，则无需额外改变属主。**Compose 本地 file secrets 不可靠地重映射属主，因此这里使用只读 bind 挂载，并由程序检查实际文件属主与权限。** 文件路径分别通过 `ADMIN_BOOTSTRAP_PASSWORD_FILE_HOST` 和 `SOURCE_SETTINGS_MASTER_KEY_FILE_HOST` 配置；必须预先存在。

已有管理员重启时不会使用 bootstrap 文件重设密码；恢复密码需使用下述明确维护命令。主密钥始终必需。密码只存 bcrypt 哈希；来源密钥使用 AES-256-GCM、独立随机 nonce 和来源/凭据版本绑定后保存。`secrets/` 已从 Git 与 Docker 构建上下文排除。主密钥应单独加密备份，不与普通数据库备份混放；丢失它后无法恢复已加密来源凭据。

运行源码构建部署：`docker compose up -d --build`。镜像部署叠加 `docker-compose.image.yml`，要求镜像包含本次认证版本。HTTP 健康探针仍不含业务数据；缺少初始化条件时服务不开放业务入口。

## 日常改密与本地恢复

在设置页“管理员与登录”中输入当前密码、新密码和确认密码。成功后全部旧会话失效，需重新登录。

忘记密码时，停止 API 与网关以避免并发维护，保留 PostgreSQL。维护前保留数据库备份。通过交互输入建立一个与上述权限、属主相同的临时 `secrets/reset-password` 文件，勿把密码写进 shell 参数或历史。执行：

```sh
docker compose stop gateway go-api
docker compose run --rm --no-deps \
  -e ADMIN_RESET_PASSWORD_FILE=/run/secrets/reset-password \
  -v "$(pwd)/secrets/reset-password:/run/secrets/reset-password:ro" \
  --entrypoint /app/adminctl go-api reset-password
docker compose up -d go-api gateway
```

重置只针对已存在的唯一管理员，使用凭据版本 CAS 并在同一数据库事务中撤销所有会话。命令成功后移除临时 reset 文件；bootstrap 文件变化不会触发恢复。失败时保留维护状态并检查数据库及文件权限。退出错误只输出受控消息，不打印密码、密钥、DSN 或数据库错误正文。

## 主密钥轮换

`adminctl rotate-key` 是离线运维命令，普通网页没有此权限入口。命令有 30 秒总期限；开始前停止网关与 API，禁止配置写入。先完成数据库备份，并将当前主密钥保存在独立受保护的备份中。生成新的随机 32 字节 base64 密钥到 `secrets/source-settings-master-key.next`，权限与属主遵循首次启动要求。

```sh
docker compose stop gateway go-api
docker compose run --rm --no-deps \
  -e SOURCE_SETTINGS_NEW_MASTER_KEY_FILE=/run/secrets/new-master-key \
  -v "$(pwd)/secrets/source-settings-master-key.next:/run/secrets/new-master-key:ro" \
  --entrypoint /app/adminctl go-api rotate-key
```

命令读取当前主密钥和新密钥，对来源与 AI 凭据加行锁，在单个事务中解密后重新加密，同时推进配置/凭据版本；任一行解密或更新失败则回滚。成功提交后还会用新密钥重新读取全部凭据进行验证。不会输出明文，也不会更改业务配置或发送第三方请求。

**只有明确报告轮换成功且读回通过后**，才用新文件替换当前主密钥文件。保留旧密钥的独立备份；在相同文件系统中将 `.next` 重命名到原路径可原子切换文件。随后 `docker compose up -d --force-recreate go-api gateway`，确保文件 bind 挂载指向新文件，再验证登录、来源配置和模型发现。不要在数据库已经使用新密钥后直接恢复旧文件并开放服务。

事务失败保留旧数据库与旧密钥。连接在提交时中断则提交状态可能不确定：保持 API 停止、保留两把密钥，检查数据库 `key_id` 并使用对应版本恢复，不能盲目重试或删除旧密钥。若提交后读回失败，同样保持维护状态。回滚数据库必须匹配该备份的主密钥版本；回退应用也必须保留访问控制，不能将旧无认证版本直接对外开放。

本地构建也可执行 `go run ./cmd/adminctl reset-password` 或 `rotate-key`，需通过私密环境提供 `DATABASE_URL` 和对应 `*_FILE`。命令不接受任何密码/密钥参数。每项秘密也支持同名无 `_FILE` 的私密环境变量，但与文件配置互斥；容器部署优先使用文件。

## 隔离验证

`E2E_CONFIG_ONLY=1 bash tests/e2e/run-e2e.sh` 只验证源码与镜像版 Compose 配置，不启动业务服务。完整 `npm run e2e:test` 建立独立随机项目、数据库卷与下载目录，生成仅用于测试的合成管理员密码和随机主密钥；临时文件为 `0600`，API 使用宿主数值 UID/GID，结束后删除临时文件与测试容器。测试 Origin 与随机 loopback 端口一致，不读取生产秘密。

数据库测试需要独立 `TEST_DATABASE_URL`，并以 `go test -p 1` 串行执行涉及迁移的包。密码恢复与轮换测试会清理其专用测试表，禁止使用生产数据库。来源真实适配器与真实 MiniCPM 网络链路尚需后续批次接入；本批的模型测试使用受控服务，不表示真实外部服务已验收。
