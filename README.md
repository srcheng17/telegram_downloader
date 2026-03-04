# Telegraph Downloader

## 声明

**本项目中的大部分代码由 AI（Google Gemini）生成和修改。** 它旨在作为一个功能原型和开发示例，可能未经过详尽的测试，请谨慎用于生产环境。

## 概述

Telegraph Downloader 是一个简单的 Web 应用，旨在帮助用户从 [Telegraph](https://telegra.ph/) 页面（通常用于发布漫画或图集）批量下载图片，并将它们打包为 CBZ 漫画文件（兼容历史 ZIP 下载记录）。

## 主要功能

*   **通过 URL 下载**：只需粘贴 Telegraph 页面的 URL 即可开始下载。
*   **重复 URL 智能复用 + 强制重抓**：默认命中已下载文件时进入“确认生成新 CBZ”流程；可强制创建新任务。
*   **CBZ 元数据**：首页支持手动填写作者、漫画系列名、漫画名、简介、标签（可空），并写入 `ComicInfo.xml`（`Writer/Series/Title/Summary/Tags/Genre`，其中 `Series` 与 `Title` 双写）。
*   **并发下载**：支持多线程并发下载图片，以提高效率。
*   **自动打包**：下载完成后，所有图片会自动打包成 `.cbz`，文件名规则为 `作者_[系列名]_漫画名_时间戳.cbz`（系列名为空则省略该段，作者/漫画名空值自动占位）。
*   **后端暂存 + 手动下载**：任务完成后产物会先存储在后端，用户可在日志页面下载。
*   **下载预检与页内错误反馈**：日志页下载按钮会先预检文件状态；如果文件不可用会在当前页给出明确错误，不会跳离 Logs 页面。
*   **容错下载**：单张图片 404/失败会继续尝试其他图片；只要存在失败，该任务最终标记为失败并不给下载按钮。
*   **下载资源护栏**：内置下载上限，防止资源失控（默认：单任务最多 `300` 张图、单图最多 `25 MiB`、总下载最多 `500 MiB`）。
*   **下载日志**：提供一个日志页面，可以查看所有下载任务的状态（中文标签）、进度和错误信息。
*   **任务看板与筛选**：首页/日志页提供任务概览指标；日志支持按状态与关键词筛选，便于快速定位问题任务。
*   **准确取消反馈**：取消操作为异步流程，前端会按后端真实响应显示状态与提示，减少误导。
*   **可配置性**：
    *   可自定义并发数、下载超时和重试次数。
    *   可配置日志保留时间。
    *   可配置结果文件缓存保留时间（到期自动清理）。
*   **动态前端**：前端使用 htmx + 原生 JS 模块，实现无刷新页面切换；除项目名外页面文案均为中文。
*   **Docker 支持**：项目已完全容器化，并支持通过环境变量和卷挂载自定义下载路径。

## 技术栈

*   **后端**: Python, Flask
*   **前端**: HTML, CSS, htmx
*   **部署**: Docker

## 后端架构（重构后）

项目已按分层结构拆分，核心目录如下：

```text
telegram_downloader/
  __init__.py              # Flask app factory + 依赖装配
  constants.py             # 全局常量
  settings.py              # 配置归一化与参数校验
  url_validation.py        # URL 安全校验
  runtime.py               # 运行时状态容器
  repositories/
    task_store.py          # 任务持久化层（SQLite）
  services/
    task_orchestrator.py   # 任务调度与线程池管理
    image_downloader.py    # 图片抓取与打包业务逻辑
    log_cleanup.py         # 日志定期清理后台服务
  web/
    routes.py              # HTTP 路由层
    template_helpers.py    # 模板辅助函数
```

同时保留了 `app.py`、`task_store.py`、`downloader_logic.py` 的兼容入口，避免影响既有脚本与测试。

## 测试分层（重构后）

```text
tests/
  integration/   # 跨层流程测试（路由 + 运行时协作）
  web/           # 路由/API 行为测试
  services/      # 业务服务单元测试
  repositories/  # 持久化层测试
  e2e/           # Playwright 端到端测试
```

## 项目修改文档

本轮改动的分项说明见：`docs/PROJECT_UPDATES.md`（后端、前端、测试、CI 与容器运行命令汇总）。

## 运行测试

### Python 单元/集成测试

```bash
.venv/bin/python -m unittest discover -s tests -q
```

### Playwright 端到端测试

```bash
npm install
npm run e2e:install
npm run e2e:test
```

> E2E 会自动启动本地 Flask 服务，并通过 `tests/e2e/fixtures/prepare_e2e_state.py` 生成可重复的测试数据。

## 关键接口说明（新增）

*   `POST /download`
    *   支持元数据字段：`author`、`series_name`、`comic_name`、`summary`、`tags`（表单或 JSON）。
    *   支持 `force` 参数（布尔语义）。
    *   命中已有成功文件时返回确认态（`needs_confirmation=true` + `download_url`）；用户可选择直接下载已有文件，或以 `force=true` 再次提交生成新 CBZ。
    *   若已有同 URL 活跃任务，仍会复用活跃任务避免重复并发。
*   `GET /api/tasks/<task_id>/download`
    *   下载任务产物（新任务为 CBZ；历史任务可为 ZIP）。
*   `HEAD /api/tasks/<task_id>/download`
    *   仅做下载可用性预检（前端用来避免错误时离开 Logs 页面）。

## 如何运行

### 1. 构建 Docker 镜像

```bash
docker build -t telegraph-downloader:latest .
```

### 2. 运行 Docker 容器

```bash
docker run -d -p 5002:5000 \
  -v "/path/to/your/manga/folder:/app/downloaded_images" \
  -v "/path/to/your/temp/folder:/app/temp_downloads" \
  -e SECRET_KEY='a_super_secret_key_that_you_should_change' \
  --name telegram-downloader \
  telegraph-downloader:latest
```

**参数说明:**
*   `-p 5002:5000`: 将主机的 `5002` 端口映射到容器的 `5000` 端口。
*   `-v "/path/to/your/manga/folder:/app/downloaded_images"`: **（必需）** 将您希望存放最终 CBZ/ZIP 文件的本地目录挂载到容器中。
*   `-v "/path/to/your/temp/folder:/app/temp_downloads"`: **（必需）** 将您希望存放临时下载文件的本地目录挂载到容器中。
*   `-e SECRET_KEY='...'`: **（推荐）** 设置一个安全的 `SECRET_KEY` 用于 Flask session 加密。

### 3. 访问应用

在浏览器中打开 `http://localhost:5002`。

## 更新后重建（镜像与容器）

当代码或前端资源有变更时，建议重新构建并替换容器：

```bash
docker rm -f telegram-downloader 2>/dev/null || true
docker build -t telegraph-downloader:latest .
docker run -d -p 5002:5000 \
  -v "/path/to/your/manga/folder:/app/downloaded_images" \
  -v "/path/to/your/temp/folder:/app/temp_downloads" \
  -e SECRET_KEY='a_super_secret_key_that_you_should_change' \
  --name telegram-downloader \
  telegraph-downloader:latest
```
