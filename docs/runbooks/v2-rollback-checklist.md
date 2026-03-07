# V2 Rollback Checklist（Go 单主线）

> 适用范围：Go 单主线发布后出现异常，需要快速回退到“上一已知可用版本”。
>
> 注意：当前发布主路径是 Go 单主线，Python/Flask 为 legacy 兼容代码，不建议作为默认热回退路径。

---

## 1) 回滚触发条件（任一满足即触发）

- [ ] 网关 5xx 持续超过阈值（例如 >5% 持续 5~10 分钟）
- [ ] `go-worker` 积压快速增长且无恢复趋势
- [ ] 核心功能不可用（创建任务 / 列表 / 下载 artifact）
- [ ] 数据一致性告警（状态推进异常、事件写入异常）
- [ ] 运维值班判断存在高风险并批准回滚

---

## 2) 回滚前保护动作（Data Safety）

- [ ] 立即记录当前版本 SHA、容器镜像 tag、告警时间线
- [ ] 导出关键表快照（`v2_tasks`、`v2_task_events`、`app_settings`）
- [ ] 保留 `downloaded_images` 与任务日志，不做清理
- [ ] 暂停可能破坏数据的手工操作（drop/truncate/覆盖导入）

---

## 3) 回滚步骤（最小可执行）

### 3.1 回滚到上一稳定 release（推荐）

1. 切回上一稳定 commit/tag（包含 compose/nginx/config）：

```bash
git checkout <last-known-good-tag-or-sha>
```

2. 重新拉起上一稳定版本：

```bash
docker compose up -d --build
docker compose ps
```

3. 若仅需快速止血，可先重启入口与 API：

```bash
docker compose restart gateway go-api go-worker
```

### 3.2 临时降级（应急）

- [ ] 若镜像拉取或构建异常，优先使用本地已存在的上一稳定镜像 tag 启动
- [ ] 确保数据库 schema 不向后破坏（如有破坏性 migration，禁止直接回滚二进制）

---

## 4) 回滚后验证（必须执行）

```bash
curl -sS -w '\nHTTP %{http_code}\n' http://localhost:5002/healthz
curl -sS -w '\nHTTP %{http_code}\n' http://localhost:5002/readyz
curl -sS -w '\nHTTP %{http_code}\n' http://localhost:5002/v2/dashboard/summary
curl -sS -w '\nHTTP %{http_code}\n' http://localhost:5002/v2/tasks
```

- [ ] `/healthz` 恢复 200
- [ ] `/readyz` 恢复 200（无 pending migration）
- [ ] `/`、`/logs`、`/settings` 页面可访问
- [ ] v2 summary/list 接口恢复可用
- [ ] 创建任务、取消任务、下载 artifact 至少走通 1 次
- [ ] 错误率与消费积压回落到阈值内

---

## 5) 回滚后收尾

- [ ] 标记“回滚完成时间点”
- [ ] 冻结进一步发布，进入故障复盘
- [ ] 归档日志、监控图、关键 SQL 与命令输出
- [ ] 输出修复计划（问题根因、修复项、二次发布门禁）

---

## 6) 数据保护注意事项（强制）

- 禁止在未备份前执行破坏性 SQL（`DROP/TRUNCATE/DELETE without where`）。
- 若回滚涉及 schema 版本差异，必须先做兼容评估，必要时使用只读/降级模式。
- 对迁移工具写入的 `MIGRATED` 事件保持幂等校验，禁止手工批量改写事件链。
- 回滚期间保留下载产物目录挂载，避免用户可下载文件丢失。
