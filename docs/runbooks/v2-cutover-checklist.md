# V2 Cutover Checklist（Go 单主线）

> 适用范围：将生产入口切换到 `gateway -> go-api/go-worker` 单主线。
>
> 执行日期模板：`YYYY-MM-DD`
>
> 值班角色：发布负责人 / 数据库负责人 / 观察窗口值班人

---

## 1) 预检查（Pre-check）

- [ ] 代码版本已冻结（记录 commit SHA）
- [ ] `docker-compose.yml` 为单主线拓扑（仅 `go-api/go-worker/postgres/redis/gateway`）
- [ ] 网关配置 `deploy/nginx/canary-go-full.conf` 已确认 `location /` 指向 `telegraph_go_api`
- [ ] 数据库备份完成（至少 `tasks`、`v2_tasks`、`v2_task_events`、`app_settings`）
- [ ] Redis 可连接，Streams/消费组参数已确认
- [ ] 环境变量已核对（DB、Redis、stream 名称、下载目录挂载）

### 1.1 发布前自动化验证（建议全部执行）

在 `go-backend` 目录：

```bash
go test ./...
go test -race ./...
```

在仓库根目录：

```bash
npm ci
npm run lint
npm run e2e:test
docker compose config
```

CI 参考：`.github/workflows/ci.yml`

---

## 2) 迁移执行（Migration）

> 如果目标库已具备最新 schema，可跳过。

- [ ] 启动 `postgres` 与 `redis`
- [ ] 执行 schema migration
  - 默认由 `go-api` 启动阶段自动执行 migration runner（`001`~`004`）。
  - 若需手动处理（故障恢复场景），按编号顺序执行：`001_go_full_rewrite.sql` → `002_v2_schema.sql` → `003_app_settings.sql` → `004_v2_reliability.sql`。
- [ ]（如有）执行 legacy -> v2 迁移工具并记录 checksum
- [ ] 复核任务总量与关键状态分布

---

## 3) 切流步骤（Cutover）

```bash
docker compose up -d --build
docker compose ps
```

- [ ] `go-api`、`go-worker`、`gateway`、`postgres`、`redis` 均为 `Up`
- [ ] `gateway` 对外端口可访问（默认 `:5002`）

基础 smoke：

```bash
curl -sS -w '\nHTTP %{http_code}\n' http://localhost:5002/healthz
curl -sS -w '\nHTTP %{http_code}\n' http://localhost:5002/readyz
curl -sS -w '\nHTTP %{http_code}\n' http://localhost:5002/v2/dashboard/summary
curl -sS -w '\nHTTP %{http_code}\n' http://localhost:5002/v2/tasks
```

---

## 4) 验收项（Acceptance）

- [ ] `/healthz` 返回 `200` 且 `service=go-backend`
- [ ] `/readyz` 返回 `200`（无 pending migration）
- [ ] `/`、`/logs`、`/settings` 页面可访问（保持当前 UI）
- [ ] `/v2` 页面可打开，创建任务可成功返回 `202`
- [ ] `/v2/tasks-ui` 可加载任务列表（至少 1 页）
- [ ] `/v2/tasks/{id}/cancel` 可用（202/409 语义正确）
- [ ] `/v2/tasks/{id}/artifact` 在 SUCCESS 任务可下载
- [ ] `/v2/settings` GET/PUT 可用，PUT clamp 规则正确
- [ ] `/v2/dashboard/summary` 口径守恒：`total_tasks == active_tasks + finished_tasks`

---

## 5) 观察窗口（Observation Window）

建议时长：`30~60` 分钟。

重点观察：

- [ ] go-api 5xx 比例
- [ ] go-worker 消费堆积（pending/lag）
- [ ] 任务状态推进异常（长期 RUNNING、取消积压）
- [ ] artifact 下载失败率
- [ ] 数据库连接池与慢查询

---

## 6) 本次执行记录（2026-03-07）

### 6.1 通过项

- `go test ./...`（go-backend）✅
- `go test -race ./...`（go-backend）✅
- `npm run lint` ✅
- `npm run e2e:test` ✅（6/6）
- `docker compose config` ✅
- `docker compose up -d --build` ✅
- `curl /healthz` ✅（HTTP 200）
- `curl /readyz` ✅（HTTP 200）
- `curl /v2` ✅（HTTP 200）
- `curl /v2/dashboard/summary` ✅（HTTP 200）
- `curl /v2/tasks` ✅（HTTP 200）

### 6.2 本机环境问题与处理记录

- 现象：首次执行 `docker compose up -d --build` 触发 `osxkeychain` 不可交互错误。  
  处理：先解锁 keychain，或临时移除 `~/.docker/config.json` 中 `credsStore` 后重试（完成后恢复）。

- 现象：fresh Postgres 下 `/v2/dashboard/summary`、`/v2/tasks` 初次请求返回 500。  
 处理：确认 `go-api` migration runner 已执行到 `004_v2_reliability.sql`；若未执行，按 `001`~`004` 顺序补齐后恢复为 HTTP 200。
