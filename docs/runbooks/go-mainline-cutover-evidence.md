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

## 5) 历史结论（截至 2026-03-07）

- Go 单主线发布门禁通过（单元、竞态、Lint、E2E）。
- Compose 启停与健康检查通过（含 `/readyz`）。
- 保留现有前端页面（`/`、`/logs`、`/settings`）并通过端到端回归。
- 可按 `docs/runbooks/v2-cutover-checklist.md` 执行正式切流。

> 注：本节仅代表 `2026-03-07` 当日验证结论。当前发布门禁以 **6.4 Release Gate 决策** 为唯一权威状态；截至 `2026-03-16`，gate 为 `BLOCKED`。

---

## 6) 2026-03-16 合并前全量验证增补（Task 7）

执行时区：`Asia/Shanghai (CST)`  
执行窗口：`2026-03-16 05:36-05:39 CST`  
执行环境：`/Users/ryancheng/project/telegram-downloader-src/.worktrees/fullstack-refactor-stability`  
端口说明：此 worktree 默认 `APP_PORT=5002`；本节所有 Compose 冒烟与 HTTP 检查统一使用 `127.0.0.1:5002`（历史证据中的 `5012` 为早期手工避冲突端口，不作为当前默认值）。

### 6.1 验证 Checklist（可复制执行）

1) Go tests（fresh run）

```bash
cd go-backend && go test ./... -count=1
```

结果：PASS。

2) Go race tests（fresh run）

```bash
cd go-backend && go test -race ./... -count=1
```

结果：PASS。

3) Frontend test/lint/build

```bash
npm run test:frontend && npm run lint && npm run build
```

结果：PASS（前端单测 `13 passed`，lint/build 通过）。

4) Python compatibility

```bash
PYTHONPATH=. ../.venv/bin/pytest tests/web/test_go_proxy.py tests/web/test_routes_api_logs.py -q
```

结果：PASS（`27 passed, 1 warning`）。  
说明：worktree 路径下需使用 `../.venv/bin/pytest`，不能使用 `.venv/bin/pytest`。

5) E2E

```bash
npm run e2e:test
```

结果：PASS（Playwright `6 passed`）。

6) Compose smoke（显式 env + teardown-safe）

```bash
INTERNAL_ENQUEUE_TOKEN=verification-token APP_PORT=5002 bash -lc 'set -euo pipefail; trap "docker compose down --remove-orphans" EXIT; docker compose up -d --build; docker compose ps; curl -fsS http://127.0.0.1:5002/healthz; curl -fsS -w "\nHTTP %{http_code}\n" http://127.0.0.1:5002/readyz'
```

结果：FAIL（Docker keychain 非交互锁定，镜像凭据拉取失败）。

### 6.2 Go fresh-run 输出摘录（替换 cached 证据）

```bash
$ cd go-backend && go test ./... -count=1
ok  	github.com/ryancheng/telegram-downloader/go-backend/cmd/server	0.552s
ok  	github.com/ryancheng/telegram-downloader/go-backend/internal/httpapi	2.779s
ok  	github.com/ryancheng/telegram-downloader/go-backend/internal/store/postgres/migrations	4.469s
ok  	github.com/ryancheng/telegram-downloader/go-backend/internal/worker	5.291s
```

```bash
$ cd go-backend && go test -race ./... -count=1
ok  	github.com/ryancheng/telegram-downloader/go-backend/cmd/server	1.453s
ok  	github.com/ryancheng/telegram-downloader/go-backend/internal/httpapi	3.776s
ok  	github.com/ryancheng/telegram-downloader/go-backend/internal/store/postgres/migrations	4.326s
ok  	github.com/ryancheng/telegram-downloader/go-backend/internal/worker	5.373s
```

### 6.3 Caveats / Blockers 与清理状态

- Compose 冒烟未完成：当前会话无法解锁 macOS keychain，Docker 拉取基础镜像凭据失败（非代码问题）。
- Docker keychain 阻塞错误摘录（traceability）：

```bash
$ INTERNAL_ENQUEUE_TOKEN=verification-token APP_PORT=5002 docker compose up -d --build
...
error getting credentials ... keychain cannot be accessed because the current session does not allow user interaction
```
- Compose 命令已改为显式要求 `INTERNAL_ENQUEUE_TOKEN` 和 `APP_PORT=5002`，并通过 `trap` 保证异常退出也会执行 `docker compose down --remove-orphans`。
- Python 兼容测试在 worktree 里应使用 `../.venv/bin/pytest`；路径修正后测试通过。

### 6.4 Release Gate 决策

- `Release gate: BLOCKED`
- `Action`: 解锁 Docker keychain 后，重新执行 6.1 第 6 条 Compose smoke 全命令；当 `docker compose ps` 正常且 `/healthz`、`/readyz` 均返回 `HTTP 200` 时，更新本证据并将 gate 改为 `GO`。
