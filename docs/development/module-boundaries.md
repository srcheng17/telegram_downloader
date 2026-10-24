# 模块边界

墨渡 PanelPort 是单机自动入库应用，保持小模块，不恢复旧 Go 分层与分布式队列。

- main.py：HTTP、上传、Komga 设置验证和三页渲染；首页只提交，任务页只查历史，设置页配置书库。
- store.py：参数化 SQLite 短事务、任务/交付状态、动作资格与 Komga 设置。
- jobs.py：串行生成、不可取消的交付提交、成功缓存清理、失败缓存重试与启动恢复。
- media.py：同步媒体执行，不访问 SQLite；固定工具、受限解包、XML/CBZ、路径验证和原子复制。
- config.py：共享限制、数据目录和初始 Komga 默认值。
- templates/static：中文交互、状态展示与任务轮询，使用后端 available_actions。

交付配置来自保存设置，覆盖环境默认。komga_url 只是网站链接，实际传输使用共享目录。不要添加下载/手动复制路由、上传 API 凭据或扫描承诺。

跨层追踪：元数据 -> SQLite -> ComicInfo/filename -> Komga 子目录；running -> delivering -> succeeded/failed -> 页面；失败缓存 -> retry；成功提交 -> 本地缓存/输入清理。

复用小函数，不预设额外 repository/service/plugin 包。路径/资源验证、进程取消、交付原子性必须保留。
