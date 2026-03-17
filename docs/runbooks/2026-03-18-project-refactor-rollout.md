# 项目重构发布门禁与 rollout 指南（2026-03-18）

## 目标

统一本地、CI 与发布前验收路径，确保 Go 主线、前端 bundle、Python compatibility 回归与 E2E 在同一套门禁下通过。

## 标准门禁

优先执行：

```bash
bash scripts/verify_release_gates.sh
```

脚本覆盖：

1. `cd go-backend && go test ./... && go test -race ./...`
2. `npm run test:frontend`
3. `npm run lint`
4. `npm run build`
5. `PYTHONPATH=. <venv>/pytest tests/web/test_go_proxy.py tests/web/test_routes_api_logs.py -q`
6. `npm run e2e:test`

## 显式 Compose smoke

发布前建议追加：

```bash
INTERNAL_ENQUEUE_TOKEN=test-token docker compose up -d --build
curl -fsS http://localhost:5002/healthz
curl -fsS http://localhost:5002/readyz
docker compose down
```

## 通过标准

- Go test / race 全绿
- 前端 test / lint / build 全绿
- Python compatibility 回归全绿
- Playwright 端到端全绿
- `/healthz` 与 `/readyz` 返回 HTTP 200

## 常见问题

- worktree 无 `.venv`：脚本会自动回退到 `../.venv/bin/pytest`。
- macOS Docker keychain：如出现非交互凭据问题，先确认 Docker Desktop 已解锁。
- Playwright 浏览器 preflight 失败：按 `tests/e2e/run-e2e.sh` 提示切换 `PLAYWRIGHT_PROJECT` 或跳过 preflight。
