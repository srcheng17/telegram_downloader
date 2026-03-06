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
go vet ./...
go test -race ./...
```

在仓库根目录：

```bash
npm run e2e:test
docker compose config
```

---

## 2) 迁移执行（Migration）

> 如果目标库已具备最新 schema，可跳过。

- [ ] 启动 `postgres` 与 `redis`
- [ ] 执行 schema migration（至少包含 go-backend/postgres migrations）
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
curl -sS -w '\nHTTP %{http_code}\n' http://localhost:5002/v2/dashboard/summary
curl -sS -w '\nHTTP %{http_code}\n' http://localhost:5002/v2/tasks
```

---

## 4) 验收项（Acceptance）

- [ ] `/healthz` 返回 `200` 且 `service=go-backend`
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

## 6) 本次执行记录（2026-03-06）

### 6.1 通过项

- `go test ./...`（go-backend）✅
- `go vet ./...`（go-backend）✅
- `go test -race ./...`（go-backend）✅
- `npm run e2e:test` ✅（8/8）
- `docker compose config` ✅

### 6.2 阻塞项

- `docker compose up -d --build` ⚠️ 阻塞  
  原因：本机 Docker 拉取镜像时触发 keychain 凭据访问失败（当前会话无法交互解锁 keychain）。

- `curl /v2/dashboard/summary`、`curl /v2/tasks` ⚠️ 返回 HTTP 502  
  说明：在 Compose 未成功拉起前执行，结果不构成切流后有效验收结论。

