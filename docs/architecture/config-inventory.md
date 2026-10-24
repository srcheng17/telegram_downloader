# 墨渡 PanelPort 配置与数据

## 应用设置

GET/POST /api/settings 返回 `{komga_url: str, komga_dir: str, configured: bool}`。komga_dir 必须是现有、可写、应用内可见的绝对共享书库目录；komga_url 是可选 HTTP/HTTPS 网站链接，不是上传 API。无 API 凭据或自动扫描配置。

SQLite 中保存的设置优先于初始 KOMGA_DIR 环境值。未配置目录时任务提交返回 409。Docker 推荐将宿主书库挂到 /komga，再在页面保存 /komga；宿主路径与容器路径不要混填。

## Compose 变量

| 变量 | 默认 | 说明 |
| --- | --- | --- |
| APP_PORT | 5002 | 宿主映射到容器 5000 |
| DATA_DIR_HOST | ./data/single-host | 新 SQLite、输入与内部缓存根 |
| APP_UID / APP_GID | 10001 / 10001 | 须能写数据和书库 |
| KOMGA_DIR | 空 | 尚无保存设置时的初始目录，可用 /komga |
| KOMGA_DIR_HOST | ./data/komga | /komga 的宿主共享目录 |
| TELEGRAPH_IMAGE | ghcr.io/srcheng17/telegram_downloader:latest | 继续使用现有远程分发路径 |

start_local.sh 默认使用当前用户 UID/GID，并读取 Compose 解析结果创建目录。不要递归更改不相关宿主目录权限。旧 POSTGRES_*、DATABASE_URL、INTERNAL_ENQUEUE_TOKEN、GO_*、CONSUMER_NAME 不参与新运行时。

## 限制和存储

默认任务超时 900 秒；上传 64 MiB、300 图、单图 25 MiB、常规成员合计 500 MiB。下载器 argv 固定；超时、取消和暂存预算不等于内核硬配额。

DATA_DIR 保存 SQLite、失败/取消任务输入、工作目录与待交付 CBZ。成功入库后清理内部 CBZ 与源包，最终漫画只在 Komga 书库保留。交付失败保留缓存供重试，没有失败缓存定时删除承诺。

只支持本机单实例。备份时同时考虑 DATA_DIR 和 Komga 目录；应用数据库可停机复制或使用 SQLite backup API，不能仅复制在线主文件漏掉 WAL。镜像内含静态资源，无宿主静态挂载。
