# 墨渡 PanelPort 系统概览

```text
首页提交 -> FastAPI -> SQLite 持久任务
                         |
              串行执行器 -> gallery-dl / bsdtar -> 内部 CBZ
                         -> 自动原子交付 -> Komga 共享书库
```

一个 Python 进程提供三个职责分明的页面：首页只新建任务，/tasks 展示历史，/settings 配置 Komga。SQLite 保存任务与 komga_url/komga_dir 设置；数据目录独占锁阻止重复实例，不使用分布式 lease/heartbeat 或自动崩溃重跑。

| 模块 | 职责 |
| --- | --- |
| main.py、__main__.py | HTTP、页面、设置校验与启动生命周期 |
| config.py | 数据根、初始 Komga 目录和资源限制 |
| store.py | SQLite 短事务、任务/入库状态、设置 |
| jobs.py | 串行生成、自动交付、取消、缓存重试与恢复 |
| media.py | 固定工具 argv、受限解包、XML/CBZ 与安全原子复制 |
| templates/、static/ | 中文响应式三页与原生 JS |

未配置现有可写共享书库目录时，任务提交返回 409。komga_url 仅是打开网站的可选链接；官方 Komga 没有 CBZ 上传 API，交付不使用网站凭据，也不承诺触发扫描。保存设置优先于初始 KOMGA_DIR 环境值。

CBZ 生成后进入短暂、不可取消的 delivering 阶段，原子复制到 Komga 成功后才标记 succeeded /「已入库」，并清理本地 CBZ 与上传源。交付失败保留内部产物和 filename，手动重试优先使用缓存。没有浏览器下载或手动复制接口。

镜像直接包含模板和静态源，无 Node/Vite。Python 模块 telegraph_downloader 与既有 GHCR 路径暂不改名。旧 Go/PostgreSQL/nginx 文档只作历史记录。
