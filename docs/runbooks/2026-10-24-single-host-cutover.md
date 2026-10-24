# 墨渡 PanelPort 单机版切换与回滚

此说明不自动部署生产。旧 PostgreSQL、下载产物和 Komga 文件不自动迁移或删除。Python 模块和 GHCR 路径暂沿用旧名，产品更名不等于远程仓库迁移。

## 切换前

1. 记录旧不可变镜像 tag/digest、Compose/env 安全备份、挂载路径、UID/GID。凭据不放 Git。
2. 备份旧 PostgreSQL 与下载/上传目录，确认可读，已有 CBZ 原样保留。
3. 选择新的 DATA_DIR_HOST，例如 data/single-host，禁止覆盖旧 data/postgres；没有自动历史导入 mapper。
4. 固定 Python 镜像 sha-<完整提交SHA>，用不同端口和临时目录验证。smoke_container.sh 会保存测试 Komga 设置、真实上传 ZIP、核对书库 CBZ 字节/XML、404 的旧接口与缓存清理。
5. 将实际书库挂入容器 /komga，确认运行 UID/GID 有权限。先用测试目录，不能把验证文件写入用户真实书库。

## 切换

在审阅后的生产部署停止旧入口与 worker，保留旧数据库、卷和文件；不执行删除卷/数据命令。独立配置启动一个 app 容器，默认宿主端口 5002。

在设置页填写应用内绝对共享目录 /komga；可填 Komga HTTP/HTTPS 网站链接。保存设置优先于初始 KOMGA_DIR。官方 Komga 没有 CBZ 上传 API，网站 URL 不能替代共享目录，也不配置 API 凭据或承诺扫描。

核验健康检查、三页和静态资源。首页只提交，任务记录单独显示；未配置目录返回 409。用授权测试 ZIP 确认自动「已入库」，检查书库文件和成功缓存清理，再验证一条有授权来源的 Telegraph 链接。短暂 delivering 阶段不可取消。

新版本没有浏览器下载或手动复制动作，旧协议与历史任务列表不会自动出现。入库失败保留内部 CBZ，可重试交付；成功删除本地缓存与上传源。

## 回滚与备份

停止新 app，备份新 DATA_DIR 和 Komga 增量，不覆盖旧 PostgreSQL。恢复记录的旧 Compose/env、不可变镜像及原挂载后启动旧应用，核验旧健康检查、历史与产物。

新版本写入 Komga 的文件在回滚后仍保留；不自动删除、不自动导回旧数据库，由操作人员核对。旧 CBZ 备份仍可访问。

新应用数据库可停机复制整个 DATA_DIR 或用 SQLite backup API；在线只复制主文件可能漏 WAL。最终漫画在独立 Komga 书库，应另行备份。重启保留 queued，running/delivering 标为中断失败，需手动重试。旧架构 plans/runbooks 仅作历史追溯。
