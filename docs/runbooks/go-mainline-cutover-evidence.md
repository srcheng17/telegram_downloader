# Go Mainline Cutover Evidence（2026-03-07）

> 执行时区：Asia/Shanghai（CST）  
> 目标：验证 Go 单主线（`gateway + go-api + go-worker + postgres + redis`）可发布、可复现测试，并完成回滚命令级 dry-run。

---

## 1) 自动化门禁结果

### 1.1 Go 测试

执行时间：`2026-03-07 14:06 CST`

```bash
cd go-backend
go test ./... -count=1
go test -race ./... -count=1
```

结果：全部 PASS（含 `cmd/server`、`cmd/worker`、`internal/httpapi`、`internal/httpui`、`internal/httpv2`、`internal/store/postgres/migrations`）。

### 1.2 前端与 E2E

执行时间：`2026-03-07 14:07 CST`

```bash
npm ci && npm run lint && npm run e2e:test
```

结果：

- `npm run lint` PASS
- Playwright E2E PASS（`6 passed`）
  - `tests/e2e/specs/backend-smoke.spec.js`
  - `tests/e2e/specs/logs-flow.spec.js`
  - `tests/e2e/specs/settings.spec.js`
  - `tests/e2e/v2/tasks-flow.spec.ts`

---

## 2) Compose 拓扑与启动验证

执行时间：`2026-03-07 14:09 CST`  
执行端口：`APP_PORT=5012`

### 2.1 拓扑检查

```bash
INTERNAL_ENQUEUE_TOKEN=evidence-token APP_PORT=5012 docker compose config
```

关键结果：

- 服务集合：`gateway`, `go-api`, `go-worker`, `postgres`, `redis`
- `gateway` 发布端口：`5012`
- `go-api` 健康检查：`/healthz`

### 2.2 启动与进程状态

```bash
INTERNAL_ENQUEUE_TOKEN=evidence-token APP_PORT=5012 docker compose up -d --build
INTERNAL_ENQUEUE_TOKEN=evidence-token APP_PORT=5012 docker compose ps
```

`docker compose ps` 关键结果：

- `gateway` `Up`
- `go-api` `Up (healthy)`
- `go-worker` `Up`
- `postgres` `Up (healthy)`
- `redis` `Up (healthy)`

---

## 3) 运行时烟雾验证

执行时间：`2026-03-07 14:09 CST`

```bash
curl -sS -w '\nHTTP %{http_code}\n' http://127.0.0.1:5012/healthz
curl -sS -w '\nHTTP %{http_code}\n' http://127.0.0.1:5012/readyz
curl -sS -o /dev/null -w 'ROOT HTTP %{http_code}\n' http://127.0.0.1:5012/
curl -sS -o /dev/null -w 'LOGS HTTP %{http_code}\n' http://127.0.0.1:5012/logs
curl -sS -o /dev/null -w 'SETTINGS HTTP %{http_code}\n' http://127.0.0.1:5012/settings
curl -sS -o /dev/null -w 'V2 PAGE HTTP %{http_code}\n' http://127.0.0.1:5012/v2
curl -sS -w '\nHTTP %{http_code}\n' http://127.0.0.1:5012/v2/dashboard/summary
curl -sS -w '\nHTTP %{http_code}\n' http://127.0.0.1:5012/v2/tasks
```

结果：

- `/healthz` -> `HTTP 200`
- `/readyz` -> `HTTP 200`
- `/` -> `HTTP 200`
- `/logs` -> `HTTP 200`
- `/settings` -> `HTTP 200`
- `/v2` -> `HTTP 200`
- `/v2/dashboard/summary` -> `HTTP 200`
- `/v2/tasks` -> `HTTP 200`

---

## 4) 回滚演练（命令级 Dry-run）

执行时间：`2026-03-07 14:09 CST`

```bash
INTERNAL_ENQUEUE_TOKEN=evidence-token APP_PORT=5012 docker compose down --remove-orphans
```

结果：所有服务与网络正常下线，无残留容器冲突。

> 说明：本证据仅覆盖“回滚命令可执行性”演练，不包含“切回上一稳定 tag 并完成业务验收”的完整回滚场景。

---

## 5) 结论

- Go 单主线发布门禁通过（单元、竞态、Lint、E2E）。
- Compose 启停与健康检查通过（含 `/readyz`）。
- 保留现有前端页面（`/`、`/logs`、`/settings`）并通过端到端回归。
- 可按 `docs/runbooks/v2-cutover-checklist.md` 执行正式切流。
