# Fullstack Refactor Migration Runbook（2026-03-16）

## 发布级验收（Task 6）

在 worktree `.worktrees/fullstack-refactor-stability` 实跑时，仓库根目录等价验收命令如下（按顺序）：

```bash
cd go-backend && go test ./... && go test -race ./...
cd .. && npm run test:frontend && npm run lint && npm run build
PYTHONPATH=. .venv/bin/pytest tests/web/test_go_proxy.py tests/web/test_routes_api_logs.py -q
npm run e2e:test
docker compose up -d --build
trap 'docker compose down' EXIT
curl -fsS http://localhost:5002/healthz
curl -fsS http://localhost:5002/readyz
docker compose down
trap - EXIT
```

> 若在 sibling worktree 中直接执行且复用仓库根虚拟环境，可将 pytest 命令替换为 `PYTHONPATH=. ../.venv/bin/pytest ...`。

实跑结果：

- ✅ Go：`go test ./...`、`go test -race ./...` 全部通过。
- ✅ Frontend：`test:frontend`、`lint`、`build` 全部通过。
- ✅ Python compatibility：`27 passed`。
- ✅ E2E：`6 passed`。
- ⚠️ Compose smoke：失败（见“Known Issues”）。

## 破坏性变更映射（old -> new）

| 范围 | old | new | 迁移动作 |
|---|---|---|---|
| Legacy API 错误体 | 仅 `{"error": "..."}` 或字段不稳定 | 统一为 `{"error","code","message","details"}`（仍保留 `error`） | 客户端改为优先使用 `code`，保留 `error` 兜底。 |
| startup recovery 统计字段 | `failed_tasks` / `canceled_tasks` 混用 | 规范化读取 `recovered_total` / `recovered_failed` / `recovered_canceled`（兼容旧字段输入） | 若上游仍发旧字段可继续工作；新逻辑建议按新字段输出。 |
| Python bridge 透传 | 代理层可能丢失 `code` | 代理层透传 Go 错误 `code` | 前端与调用方可直接按 `code` 做分支处理。 |

## 升级步骤（生产）

1. 准备环境：确保已配置 `INTERNAL_ENQUEUE_TOKEN`，并确认 Docker 凭据可在当前会话使用。
2. 拉取代码后执行发布级验收命令（见上方“发布级验收”）。
3. 通过后执行部署：
   ```bash
   docker compose up -d --build
   ```
4. 健康检查：
   ```bash
   curl -fsS http://localhost:5002/healthz
   curl -fsS http://localhost:5002/readyz
   ```
5. 记录产线验收证据（测试输出、健康检查返回 JSON、容器状态）。

## 回滚步骤

1. 下线当前版本：
   ```bash
   docker compose down
   ```
2. 切回上一稳定版本（tag/commit），保留同一 `INTERNAL_ENQUEUE_TOKEN` 与数据库连接配置。
3. 重新启动旧版本：
   ```bash
   docker compose up -d --build
   ```
4. 回滚后验证：
   ```bash
   curl -fsS http://localhost:5002/healthz
   curl -fsS http://localhost:5002/readyz
   ```

> 说明：本轮数据库变更以新增索引为主（`idx_v2_tasks_status_updated_at`），回滚应用版本通常不需要回滚该索引。

## 生产验收命令与预期输出

- `cd go-backend && go test ./... && go test -race ./...`：退出码 0。
- `cd .. && npm run test:frontend && npm run lint && npm run build`：退出码 0，Vite build 成功。
- `PYTHONPATH=. .venv/bin/pytest tests/web/test_go_proxy.py tests/web/test_routes_api_logs.py -q`：`27 passed`。
- `npm run e2e:test`：`6 passed`。
- Compose smoke（teardown-safe）：
  ```bash
  docker compose up -d --build
  trap 'docker compose down' EXIT
  curl -fsS http://localhost:5002/healthz
  curl -fsS http://localhost:5002/readyz
  docker compose down
  trap - EXIT
  ```
  预期 `healthz/readyz` 均返回 HTTP 200 + JSON，且失败场景也会执行 `docker compose down`。

## Known Issues（Task 6 实跑）

1. 未设置 `INTERNAL_ENQUEUE_TOKEN` 时，`docker compose up -d --build` 会直接失败：
   - `required variable INTERNAL_ENQUEUE_TOKEN is missing a value: required`
2. 在当前 macOS 非交互会话下，Docker 可能报 keychain 凭据错误：
   - `keychain cannot be accessed because the current session does not allow user interaction`

建议：

- 先导出 `INTERNAL_ENQUEUE_TOKEN` 再执行 Compose 命令。
- 在执行会话前解锁 keychain（`security -v unlock-keychain ~/Library/Keychains/login.keychain-db`），或使用可用的无交互 Docker 凭据配置。
