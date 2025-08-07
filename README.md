# Telegraph Downloader

## 声明

**本项目中的大部分代码由 AI（Google Gemini）生成和修改。** 它旨在作为一个功能原型和开发示例，可能未经过详尽的测试，请谨慎用于生产环境。

## 概述

Telegraph Downloader 是一个简单的 Web 应用，旨在帮助用户从 [Telegraph](https://telegra.ph/) 页面（通常用于发布漫画或图集）批量下载图片，并将它们打包为 ZIP 文件。

## 主要功能

*   **通过 URL 下载**：只需粘贴 Telegraph 页面的 URL 即可开始下载。
*   **并发下载**：支持多线程并发下载图片，以提高效率。
*   **自动打包**：下载完成后，所有图片会自动打包成一个 ZIP 文件，并以 Telegraph 页面的标题命名。
*   **下载日志**：提供一个日志页面，可以查看所有下载任务的状态（等待、进行中、成功、失败）、进度和错误信息。
*   **可配置性**：
    *   可自定义并发数、下载超时和重试次数。
    *   可配置日志保留时间。
*   **动态前端**：前端使用 htmx 构建，实现了无刷新页面切换，提供了流畅的用户体验。
*   **Docker 支持**：项目已完全容器化，并支持通过环境变量和卷挂载自定义下载路径。

## 技术栈

*   **后端**: Python, Flask
*   **前端**: HTML, CSS, htmx
*   **部署**: Docker

## 如何运行

### 1. 构建 Docker 镜像

```bash
docker build -t telegram-downloader:v1.5 .
```

### 2. 运行 Docker 容器

```bash
docker run -d -p 5001:5000 \
  -v "/path/to/your/manga/folder:/app/downloaded_images" \
  -v "/path/to/your/temp/folder:/app/temp_downloads" \
  -e SECRET_KEY='a_super_secret_key_that_you_should_change' \
  --name telegram-downloader \
  telegram-downloader:v1.5
```

**参数说明:**
*   `-p 5001:5000`: 将主机的 `5001` 端口映射到容器的 `5000` 端口。
*   `-v "/path/to/your/manga/folder:/app/downloaded_images"`: **（必需）** 将您希望存放最终 ZIP 文件的本地目录挂载到容器中。
*   `-v "/path/to/your/temp/folder:/app/temp_downloads"`: **（必需）** 将您希望存放临时下载文件的本地目录挂载到容器中。
*   `-e SECRET_KEY='...'`: **（推荐）** 设置一个安全的 `SECRET_KEY` 用于 Flask session 加密。

### 3. 访问应用

在浏览器中打开 `http://localhost:5001`。
