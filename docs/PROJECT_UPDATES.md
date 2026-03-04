# 项目修改梳理（2026-03-02）

本文档用于汇总本轮代码改动，便于后续联调、回归测试和部署。

---

## 增补（2026-03-04）：CBZ 元数据与中文化

### A. CBZ 与 ComicInfo 元数据

- 下载产物默认由 ZIP 切换为 CBZ（兼容历史 ZIP 文件下载）。
- 新增 `ComicInfo.xml` 生成：
  - 作者 -> `Writer`
  - 漫画名 -> `Series` + `Title`
  - 详情介绍 -> `Summary`
  - 标签 -> `Tags` + `Genre`
- 标签支持中英文逗号输入并自动规范化。
- 文件名规则：`作者_漫画名_时间戳.cbz`；作者/漫画名为空时自动使用占位符。
- 增强了文件名长度安全与 XML 非法字符清洗。

### B. 重复任务确认流程（前端）

- 首页提交命中“已有成功文件”时，不再直接复用下载，而是进入确认态：
  - `生成新的CBZ`（force=true 二次提交创建新任务）
  - `取消并下载已有文件`（不创建新任务）
- 首页新增元数据输入（作者、漫画名、书籍详情介绍、标签）。

### C. 全站中文化（项目名除外）

- 导航、首页、日志页、设置页及前端提示文案统一中文。
- `STATUS_CATALOG` 状态标签改为中文显示。

### D. 下载接口与 MIME

- 下载接口按扩展名动态返回 MIME：
  - `.cbz` -> `application/vnd.comicbook+zip`
  - `.zip` -> `application/zip`

### E. 测试覆盖

- Python 单测/集成新增并通过：
  - 元数据字段持久化
  - duplicate 确认协议
  - CBZ + ComicInfo 生成
  - MIME 动态映射
- Playwright E2E 新增并通过：
  - duplicate SUCCESS 取消分支（下载已有）
  - duplicate SUCCESS 确认分支（force 生成新任务）
  - 中文化后的日志/设置流程

## 1. 后端能力改动

### 1.1 重复任务复用与强制重抓

- 新增 Telegraph URL 规范化（`www`、协议、尾斜杠、query/fragment 去噪），统一比较键为 `canonical_url`。
- `POST /download` 默认优先复用同 URL 的最近成功任务（且 ZIP 文件仍存在）。
- 若存在同 URL 活跃任务（`PENDING/IN_PROGRESS/CANCEL_REQUESTED`），会复用活跃任务避免并发重复抓取。
- 支持 `force=true` 强制新建任务；但若已有同 URL 活跃任务，仍复用活跃任务。

### 1.2 下载资源护栏

- 新增下载限制常量（默认）：
  - 单任务最多 `300` 张图；
  - 单图最大 `25 MiB`；
  - 单任务总下载量最大 `500 MiB`。
- 限制触发时会返回清晰错误信息并将任务标记为失败，避免资源失控。

### 1.3 API 增强

- `POST /download`：新增 JSON 提交支持，并返回结构化 JSON 结果（包含 duplicate/active/force_applied）。
- `GET /api/summary`：新增任务看板统计接口（总数、活跃数、完成数、成功率等）。
- `GET /api/logs`：新增 `status` 与 `q` 过滤参数，响应中回传 filters 与 summary。
- `HEAD /api/tasks/<task_id>/download`：新增下载预检，供前端先确认文件是否可下载。

### 1.4 数据层与清理策略

- `tasks` 表新增 `canonical_url` 字段及索引，兼容旧数据自动补列。
- 新增原子化 `claim_download_task`，降低并发下重复建任务风险。
- 新增状态聚合、按条件分页过滤、按 ID 批量删除等仓储能力。
- 日志清理策略调整：
  - 日志保留期与文件保留期解耦；
  - 文件仍在保留期且存在时，任务日志可延后删除；
  - 过期文件、孤儿文件仍按策略清理。

## 2. 前端与交互改动

- 页面脚本模块化拆分：`static/app.js`、`static/index.js`、`static/logs.js`。
- 首页新增：
  - 任务概览卡片（Total/Active/Success/Failed）；
  - 下载护栏说明；
  - `Force re-download` 开关；
  - 提交反馈提示（成功/错误/信息）。
- 日志页新增：
  - 状态/关键词过滤；
  - 概览统计（Active/Finished/Success Rate/Failures）；
  - 下载前 `HEAD` 预检及页内错误提示；
  - 错误详情弹窗可访问性增强（Esc 关闭、焦点回退、Tab 焦点圈定）。
- 设置页补充字段说明与“仅影响新任务”的提示文案。

## 3. 测试与工程化改动

- 扩充 Python 测试：
  - 集成测试覆盖重复任务复用、force 行为；
  - 仓储层覆盖并发 claim、聚合、过滤；
  - 服务层覆盖下载护栏、日志清理策略；
  - Web 层覆盖 summary、过滤、download HEAD 预检。
- 新增 Playwright E2E：
  - 日志流（筛选、取消、错误弹窗、下载预检/下载成功）；
  - 设置页保存与会话保持；
  - 固定测试数据准备脚本 `tests/e2e/fixtures/prepare_e2e_state.py`。
- 新增 GitHub Actions CI：
  - Python unittest；
  - Playwright E2E。
- 忽略规则更新：
  - `.gitignore` 增加 `node_modules/`、`playwright-report/`、`test-results/`、`tests/e2e/.artifacts/`；
  - `.dockerignore` 增加 `data/`。

## 4. 镜像与容器（建议命令）

```bash
docker build -t telegraph-downloader:latest .
docker rm -f telegram-downloader 2>/dev/null || true
docker run -d -p 5002:5000 \
  -v "$(pwd)/downloaded_images:/app/downloaded_images" \
  -v "$(pwd)/temp_downloads:/app/temp_downloads" \
  -e SECRET_KEY='change-this-secret' \
  --name telegram-downloader \
  telegraph-downloader:latest
```

访问地址：`http://localhost:5002`
