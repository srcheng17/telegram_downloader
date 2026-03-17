# Python compatibility runtime 历史说明

本仓库在重构早期曾同时维护一套 Python/Flask compatibility runtime，用于承接 legacy 页面、旧下载任务模型与 Go 主线之间的过渡代理。

截至 2026-03-18，仓库已完成：

- Go 主线提升到仓库根目录
- 页面模板与静态产物迁移到 `web/templates`、`web/static`
- 发布级门禁完全围绕 Go runtime、前端 bundle 与 Compose 拓扑组织
- Python compatibility runtime 从主发布链路移除

因此以下内容已退出当前运行时：

- `app.py`
- `telegram_downloader/`
- `templates/`
- `tests/web/*`
- `static/app.js` / `static/index.js` / `static/logs.js` / `static/v2/*`

如果需要追溯历史兼容实现，请直接查看 Git 历史：

```bash
git log -- app.py telegram_downloader templates tests/web static/v2
```

或在旧提交上使用：

```bash
git show <old-sha>:app.py
```
