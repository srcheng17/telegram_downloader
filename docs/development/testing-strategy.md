# 测试策略

验证墨渡 PanelPort 的真实自动入库链路与信任边界，避免保留旧接口 mock。

```bash
python -m ruff check telegraph_downloader tests
python -m pytest -m 'not e2e'
python -m pytest -m e2e
```

安装 requirements-dev.txt 和 `python -m playwright install chromium`；Linux CI 使用 --with-deps。完整门禁为 verify_release_gates.sh，部署另跑 smoke_container.sh。

- Store/runner：持久排队、独占锁、中断恢复、取消与 begin_delivery 竞态、交付失败保留缓存、重试不重复抓取、成功清理。
- Media：受控 URL fixture、序列/字节/XML、ZIP/RAR/7Z、数量/体积、损坏/不安全成员、工具取消/超时、Komga 幂等/冲突。
- HTTP：Komga 目录未配置返回 409、保存设置覆盖环境、网站 URL 只作链接、上传实际大小与部分清理、旧下载/手动复制路由 404。
- Browser e2e：桌面/移动端首页只提交，独立任务历史，设置书库，真实上传 -> 自动已入库、错误恢复、取消/重试。

测试用 tmp_path、临时本地端口与合成内容。API mock 只证明交互，不能替代实际媒体执行。默认不依赖公网图集。

容器 smoke 使用唯一 project、空 env-file、临时 DATA_DIR/Komga 和本地端口：先保存 /komga 与测试网站链接，上传两张不同 PNG 的 ZIP；等已入库，读取挂载书库 CBZ 检查页序/图片字节/中文 XML；确认旧下载/手动复制接口 404，成功任务内部产物和源包已清理。结束只清理自己的测试资源。

无 Go/PG/Node/Vite/bundle 门禁。页面源直接入镜像，需核验镜像内页面和静态资源。只报告实际执行结果及未跑原因，不操作生产数据。
