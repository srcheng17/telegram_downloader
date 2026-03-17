# 项目重构回滚指南（2026-03-18）

## 适用场景

当重构阶段的代码已经合入当前分支，但发布级门禁、E2E 或 Compose smoke 无法通过时，使用本指南回退到最近一个绿色提交点。

## 回滚原则

1. **先停止继续开发**，不要在失败状态上叠加更多改动。
2. **确认最近绿色提交点**：优先选择已完成一个 task/phase 且回归通过的提交。
3. **保留失败证据**：保存 `tests/e2e/.artifacts`、`playwright-report`、`test-results`、终端输出。
4. **回滚后重跑门禁**，确认恢复成功。

## 推荐步骤

### 1. 查看最近阶段提交

```bash
git log --oneline --decorate -10
```

### 2. 保存当前证据

```bash
mkdir -p /tmp/project-refactor-debug
cp -R tests/e2e/.artifacts playwright-report test-results /tmp/project-refactor-debug 2>/dev/null || true
```

### 3. 回退到最近绿色点

```bash
git reset --hard <green-commit-sha>
```

### 4. 重跑统一门禁

```bash
bash scripts/verify_release_gates.sh
```

### 5. 必要时追加 Compose smoke

```bash
INTERNAL_ENQUEUE_TOKEN=test-token docker compose up -d --build
curl -fsS http://localhost:5002/healthz
curl -fsS http://localhost:5002/readyz
docker compose down
```

## 回滚判定

满足以下条件才算回滚成功：

- 当前分支回到已知绿色提交
- `bash scripts/verify_release_gates.sh` 再次通过
- 若做了 Compose smoke，则 `/healthz`、`/readyz` 正常

## 备注

- 若失败根因来自架构级问题（不是单个 task 的实现问题），应暂停执行计划，回到 spec/plan 更新流程。
- 若失败根因仅是单个 task 回归，可在绿色点上重新按 TDD 修复后继续。
