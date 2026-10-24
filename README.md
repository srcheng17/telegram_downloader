# 墨渡 PanelPort

把 Telegraph 图集或本地 ZIP / RAR / 7Z 整理成带 ComicInfo 元数据的 CBZ，自动放入 Komga 共享书库。

## 使用流程

1. 在「偏好设置」填写应用可访问的 Komga 书库目录，可另填 Komga 网站链接。
2. 首页只负责新建任务：选择 Telegraph 链接或上传压缩包，填写作者、系列、编号、漫画名、简介、标签和类型。
3. 在独立的「任务记录」查看进度。生成 CBZ 后自动入库，成功显示「已入库」。
4. 失败或取消后可手动重试。入库失败会保留已生成的内部 CBZ，重试直接交付，不必重新抓取。

界面与错误反馈使用中文，支持桌面和移动端。URL 来源限于 `telegra.ph`、`graph.org`。站点解析和下载交给固定版本 [gallery-dl](https://github.com/mikf/gallery-dl)，RAR/7Z 使用 [libarchive](https://www.libarchive.org/) 的 bsdtar；ZIP、XML 和 SQLite 使用 Python 标准库。

上传最多 64 MiB；解包最多 300 张图片、单图 25 MiB、所有常规成员合计 500 MiB。失败不交付半成品。入库成功后删除应用的临时 CBZ 和上传源包，最终文件保留在 Komga 书库中。

## Komga 连接方式

`komga_url` 是可选的 HTTP/HTTPS 网站链接，用于打开书库；实际交付使用现有、可写、绝对路径的共享书库目录 `komga_dir`。官方 Komga 没有 CBZ 上传 API，因此这里不通过网站 URL 上传，也不要求 API 凭据或承诺触发扫描。

Docker 中先将宿主书库挂到 `/komga`，再在设置页填写容器内路径 `/komga`。目录未配置时提交返回 409。系列名用作子目录，无系列使用 `tankobon`。交付采用目标文件系统中的临时文件和原子发布，同名不同内容不覆盖；短暂的「入库中」阶段不可取消。

## Docker 启动

```bash
cp .env.example .env
bash scripts/start_local.sh
```

访问 `http://localhost:5002`。本地脚本使用当前用户 UID/GID，按 Compose 配置创建数据和共享目录。默认新数据目录为 `./data/single-host`，与旧 PostgreSQL 分开。

| 配置 | 默认值 | 用途 |
| --- | --- | --- |
| `APP_PORT` | `5002` | 宿主端口，容器固定监听 5000 |
| `DATA_DIR_HOST` | `./data/single-host` | SQLite、输入与内部工作缓存 |
| `APP_UID / APP_GID` | `10001 / 10001` | 容器用户；须能写挂载目录 |
| `KOMGA_DIR_HOST` | `./data/komga` | 挂载到 `/komga` 的宿主共享书库 |
| `KOMGA_DIR` | 空 | 尚未保存设置时的初始目录，可设为 `/komga` |

在 `.env` 设置 `KOMGA_DIR_HOST` 指向实际书库；首次启动可在设置页保存 `/komga`，也可设置初始 `KOMGA_DIR=/komga`。数据库中已保存的设置优先于环境默认值。

直接使用 Compose 时，先为 APP_UID/GID 创建可写目录：

```bash
docker compose up -d --build
docker compose ps
```

使用发布镜像：

```bash
docker compose -f docker-compose.yml -f docker-compose.image.yml pull
docker compose -f docker-compose.yml -f docker-compose.image.yml up -d
```

`TELEGRAPH_IMAGE` 可固定为 `ghcr.io/srcheng17/telegram_downloader:sha-<完整提交SHA>`。main 的 CI 通过后发布 `latest` 和 SHA 标签，支持 amd64/arm64；PR 不发布镜像。现有 GHCR 路径与 Python 模块名暂时沿用，不随产品更名自动改变。

## 架构与开发

一个 Python 3.14 / FastAPI 进程提供页面和 API，并运行一个串行执行器。SQLite 保存任务与 Komga 设置，文件留在本机目录。独占目录锁阻止重复实例；不要使用多个 Uvicorn workers、横向扩容或网络共享 SQLite。

```text
telegraph_downloader/
  __main__.py, main.py   启动、页面与 API
  config.py             路径与限制
  store.py              SQLite 任务、入库状态与设置
  jobs.py               串行执行、自动交付、取消/重试/重启恢复
  media.py              下载器、受限解包、CBZ 与安全复制
  templates/, static/   中文页面、CSS 与原生 JavaScript
tests/                  Python 功能、安全与浏览器测试
scripts/                启动、发布门禁与隔离容器 smoke
```

```bash
python3.14 -m venv .venv
source .venv/bin/activate
python -m pip install -r requirements-dev.txt
python -m playwright install chromium
DATA_DIR="$PWD/data/local" python -m telegraph_downloader
```

Linux 浏览器依赖使用 `python -m playwright install --with-deps chromium` 安装；RAR/7Z 另需系统 bsdtar。macOS 可安装 libarchive，确认 bsdtar 在 PATH 中。

```bash
python -m ruff check telegraph_downloader tests
python -m pytest -m 'not e2e'
python -m pytest -m e2e
bash scripts/verify_release_gates.sh
bash scripts/smoke_container.sh
```

容器 smoke 使用唯一 project、临时挂载、随机本地端口和空 env-file。它保存测试 Komga 设置，上传两张不同 PNG 的 ZIP，校验书库 CBZ 页序、图片字节、中文 XML 与本地缓存清理，并确认旧下载/手动复制接口不存在。测试不读取生产目录，也不依赖公网图集。

静态源文件直接进入镜像，没有 Node/Vite 构建。外部下载器的超时、取消和临时磁盘预算不等于操作系统硬配额。只报告实际检查；浏览器或 Docker 不可用时注明未运行。

## API

- `POST /api/jobs/url`：JSON 或表单，提交 URL 与元数据。
- `POST /api/jobs/upload`：multipart，一次提交 `file` 与元数据。
- `GET /api/jobs`、`GET /api/jobs/{id}`：任务列表和详情，公开文件名字段为 `filename`。
- `POST /api/jobs/{id}/cancel`、`/retry`：取消或重试；可用动作只有 cancel/retry。
- `GET/POST /api/settings`：`{komga_url, komga_dir, configured}`。
- `GET /healthz`：健康检查。

没有浏览器产物下载或手动 Komga 复制接口。新版本不兼容历史 `/download`、`/api/tasks`、`/v2/settings` 协议。

## 切换与备份

新版本不自动读取、迁移或删除旧 PostgreSQL、下载目录和已有 CBZ。切换前保留旧不可变镜像、Compose/env、数据库与文件备份；步骤见 [单机版切换说明](docs/runbooks/2026-10-24-single-host-cutover.md)。

排队任务重启后保留，运行/入库中任务标为中断失败，用户手动重试。备份同时考虑应用 DATA_DIR 和独立的 Komga 书库。当前约束见 [模块边界](docs/development/module-boundaries.md)、[测试策略](docs/development/testing-strategy.md) 和 [系统概览](docs/architecture/current-system-overview.md)；旧 plans/runbooks 仅作历史追溯。
