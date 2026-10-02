# Task Core 优化发布说明

## 发布前

1. 备份 PostgreSQL，确认没有未完成的 worker 执行；停止旧 API/worker 后统一升级，避免旧二进制缺少 generation 校验。
2. 在隔离数据库配置 `TEST_DATABASE_URL`，执行 `bash scripts/verify_release_gates.sh`。正确 bundle 需先暂存；CI 验证提交的产物。测试数据库不可为生产库。
3. 检查 Go 1.27.1、Node 24 和 Docker Compose >= 2.24.4（E2E override 语法）。Docker 镜像构建执行 npm ci/build 并包含静态资源。
4. `APP_UID`/`APP_GID` 默认 10001；共享下载、临时、Komga 目录需可写。`scripts/start_local.sh` 默认使用当前宿主用户 ID；直接 Compose 部署需配置并准备目录。

## 数据变化

迁移 `014_task_core_execution_settings.sql` 增加 task generation、input runtime_settings 和 created_at/id 排序索引。已有 attempt>0 的行初始化 generation=attempt。新任务设置快照持久化；旧无快照任务继续使用 worker 环境默认值。

迁移保留旧表及不再显示的设置字段，不删除历史数据。日志和结果文件没有自动到期清理；成功上传源包在 Complete 之后删除，失败和取消保留。

## 发布后验证

- `/readyz` 正常；提交 URL 和上传任务后能查看真实进度并下载 CBZ。
- 同 URL 活跃提交返回同 task ID；成功产物提交进入确认态；force 新建。
- 取消显示 CANCELING 后最终 CANCELED，不再生成可下载结果；重试沿用原设置。
- Komga 复制实际可读，HTML 413 和历史导航后可继续操作。
- 检查 API/worker 的共享目录权限和 task ID/generation 产物路径。

## 回滚

先停止新 API/worker，再恢复前一版本。保留新增列和索引，不运行破坏性反向迁移；前一版本会忽略 generation/settings，但不具备新 fencing 保证，恢复前必须确认没有旧执行存活。若需恢复旧上传成功任务的源包，只能从备份恢复；应用回滚不会重建已清理源包。

## CI 和远程仓库

代码门禁包含 PostgreSQL Go/race/vet、Node tests、lint、bundle diff 与真实 worker E2E。远程分支保护和必需检查需由仓库设置配置；本轮没有修改远程保护，也没有授权合并 main。
