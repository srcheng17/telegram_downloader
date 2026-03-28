# 配置与运行约束清单（Baseline）

## 目的

本文档盘点当前系统运行依赖的环境变量、挂载目录与隐性知识，为后续配置治理和 runbook 标准化提供基线。

## Compose 服务级配置

### postgres

环境变量：
- `POSTGRES_DB`（默认 `telegraph`）
- `POSTGRES_USER`（默认 `telegraph`）
- `POSTGRES_PASSWORD`（默认 `telegraph`）

卷：
- `./data/postgres:/var/lib/postgresql/data`

### redis

卷：
- `./data/redis:/data`

### go-api

关键环境变量：
- `PORT`（默认 `5000`）
- `DATABASE_URL`
- `REDIS_URL`
- `STREAM_NAME`（默认 `download_tasks`）
- `INTERNAL_ENQUEUE_TOKEN`（必填）
- `GO_DOWNLOAD_TIMEOUT`
- `GO_DOWNLOAD_RETRIES`
- `GO_IMAGE_CONCURRENCY`
- `DOWNLOAD_PATH`（默认 `/app/downloaded_images`）
- `KOMGA_LIBRARY_ROOT`（默认 `/app/komga/myReadingManga`）

卷：
- `./downloaded_images:/app/downloaded_images`
- `./web/static:/app/static:ro`
- `${KOMGA_LIBRARY_ROOT_HOST:-/Users/ryancheng/docker_data/komga/data/myReadingManga}:${KOMGA_LIBRARY_ROOT:-/app/komga/myReadingManga}:rw`

### go-worker

关键环境变量：
- `DATABASE_URL`
- `REDIS_URL`
- `STREAM_NAME`
- `INTERNAL_ENQUEUE_TOKEN`
- `CONSUMER_GROUP`
- `CONSUMER_NAME`
- `GO_DOWNLOAD_TIMEOUT`
- `GO_DOWNLOAD_RETRIES`
- `GO_IMAGE_CONCURRENCY`
- `DOWNLOAD_PATH`
- `TEMP_PATH`

卷：
- `./downloaded_images:/app/downloaded_images`
- `./temp_downloads:/app/temp_downloads`

### gateway

关键环境变量：
- `APP_PORT`（默认 `5002`）

卷：
- `./deploy/nginx/canary-go-full.conf:/etc/nginx/conf.d/default.conf:ro`

## 当前配置分类观察

当前配置大致混合了以下几类语义：

1. **应用行为**
   - 下载超时、重试次数、图片并发数
2. **数据存储与路径**
   - `DOWNLOAD_PATH`、`TEMP_PATH`、`KOMGA_LIBRARY_ROOT`
3. **队列与 worker**
   - `STREAM_NAME`、`CONSUMER_GROUP`、`CONSUMER_NAME`
4. **基础设施连接**
   - `DATABASE_URL`、`REDIS_URL`
5. **运维与入口**
   - `PORT`、`APP_PORT`、`INTERNAL_ENQUEUE_TOKEN`

当前问题不是变量不够，而是**分组和归属还没有完全制度化**。

## 当前挂载语义

### 结果与临时文件

- `downloaded_images/`
  - 任务产物目录，也是 API / worker 共享结果目录。
- `temp_downloads/`
  - worker 临时下载与处理目录。

### 前端静态资源

- `web/static/` 被只读挂载进 `go-api`。
- 运行时直接消费 `web/static/dist/*.bundle.js`。

### Komga 目录

- 宿主机目录通过 `KOMGA_LIBRARY_ROOT_HOST` 挂载到容器内 `KOMGA_LIBRARY_ROOT`。
- 当前默认宿主机路径是：
  `/Users/ryancheng/docker_data/komga/data/myReadingManga`
- 当前默认容器内路径是：
  `/app/komga/myReadingManga`

这套路径映射是修复 Komga copy 失败问题后的当前基线。

## 当前隐性知识

### 1. 前端源码修改后必须同步更新 bundle

当前 Compose / 运行链路不会自动执行 `vite build`。因此：

- 改了 `frontend/src/`
- 必须同时更新 `web/static/dist/*.bundle.js`

否则容器里仍会运行旧前端逻辑。

### 2. gateway 有时需要跟着 go-api 一起重启

在某些本地 Docker 环境下，`go-api` 重建后 nginx 可能短时间保留旧 upstream IP，导致：

- `/logs` 等页面偶发 `502`

经验性解决办法通常是：
- 额外执行一次 `docker compose restart gateway`

这说明当前网关刷新语义仍带有环境依赖。

### 3. Komga copy 依赖容器挂载，而不是宿主机直写

当前代码默认写容器内路径，真正落宿主机依赖 volume 映射。如果仅在代码里写宿主机路径、不做 volume 挂载，会重新触发类似：

- `mkdir /Users: permission denied`

### 4. 热修链路存在二进制覆盖经验路径

在紧急场景中，可以通过本地交叉编译 Linux 二进制，再 `docker cp` 到运行中的容器并重启服务完成热修。这不是标准发布流程，但目前是实际存在的运维经验路径。

### 5. macOS 本地工具链存在环境约束

当前仓库上下文中已观察到：

- Apple 自带 `git` / `python3` 可能触发 Xcode license 问题；
- 通常需要优先使用 Homebrew `git` 或已知可用解释器路径。

## 建议后续治理方向（摘要）

后续在重构阶段应把这些基线信息转化为：

- 更明确的配置分组；
- 更稳定的开发/运行脚本；
- 更标准化的 runbook；
- 更少依赖经验记忆的发布与排障过程。
