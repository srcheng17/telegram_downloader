# Runtime Mainline Cleanup Design

日期：2026-06-05
项目：telegram-downloader
状态：设计已确认，待实施计划

## 1. 背景

当前仓库已经完成一轮大规模 Task Core 切换和旧 worker / queue / v2 task handler 删除。代码主线已经变成：

```text
gateway -> go-api -> postgres
          go-worker -> postgres
```

`task_core_*` 表和 `internal/app/taskcore` / `internal/worker/taskcore` 是任务生命周期的实际主线。worker 通过 PostgreSQL `READY` 任务、lease、heartbeat 和 recovery 推进任务，而不是通过 Redis Streams 消费消息。

但是仓库仍存在迁移后的残留：

- `docker-compose.yml` 仍声明 Redis 服务，并给 `go-api` / `go-worker` 注入 `REDIS_URL`、`STREAM_NAME` 等变量；
- `internal/config.Config` 仍保留 Redis / stream 相关字段；
- `README.md` 和 `docs/architecture/*` 仍把 Redis Streams 描述成当前主线；
- `scripts/start_local.sh` 仍指向旧 Python / Flask 入口；
- RAR / 7Z 上传依赖 `bsdtar`，但当前 Docker runtime image 没安装对应工具；
- 测试契约还没有明确阻止 Redis / Python runtime 残留回流。

本阶段选择“安全收尾优先”：先让运行拓扑、配置、文档、脚本、容器依赖和契约测试全部与当前 Task Core 主线一致，再进入更深的架构拆分和性能优化。

## 2. 目标与非目标

### 目标

1. 将当前运行主线契约化为 `go-api + go-worker + postgres + gateway`。
2. 删除或改写 Redis Streams 已下线后的配置、Compose、文档和测试残留。
3. 删除或改写旧 Python / Flask 本地启动脚本语义。
4. 修复容器内 RAR / 7Z 上传缺少 `bsdtar` 的运行依赖。
5. 增加或更新契约测试，防止旧 Redis / Python runtime 路径重新出现。
6. 保持用户可见行为不变：URL 下载、上传生成 CBZ、日志、取消、重试、下载、Komga copy 都继续走 Task Core。

### 非目标

1. 不重构 `internal/store/postgres/taskcore/store.go` 的文件组织；这属于下一阶段架构升级。
2. 不引入新的任务状态、数据库 schema 或迁移策略。
3. 不删除历史迁移工具 `scripts/migrate_sqlite_to_postgres.py`。
4. 不做下载器流式化、CBZ 内存优化、worker 并发策略重写；这些属于后续性能阶段。
5. 不重做前端视觉或页面交互。

## 3. 推荐方案

采用“运行主线契约化”方案，把已事实成立的 Task Core 主线固化到配置、文档、脚本和测试。

### 3.1 配置与 Compose 收敛

`docker-compose.yml` 应只保留当前运行需要的服务：

```text
postgres
go-api
go-worker
gateway
```

删除 Redis 服务、Redis volume、Redis healthcheck，以及 `go-api` / `go-worker` 中的 `REDIS_URL`、`STREAM_NAME`、`CONSUMER_GROUP` 等 Redis Streams 配置。

`Config` 中删除不再被运行链路使用的字段：

- `RedisURL`
- `StreamName`
- `V2StreamName`
- `ConsumerGroup`

`ConsumerName` 仍可暂时保留，因为当前 worker 用它作为 lease owner / worker identity。后续架构阶段可将命名重整为 `WorkerID`，并保留 `GO_WORKER_CONSUMER_NAME` 作为兼容 alias。

### 3.2 本地启动脚本校准

`scripts/start_local.sh` 不应再创建 Python venv、安装 `requirements.txt`、设置 `FLASK_APP` 或运行 Flask。

本阶段采用 Compose 作为本地主线启动方式：

```bash
INTERNAL_ENQUEUE_TOKEN="${INTERNAL_ENQUEUE_TOKEN:-local-dev-token}" docker compose up -d --build
```

脚本应创建必要目录、设置默认 `INTERNAL_ENQUEUE_TOKEN`，并打印访问地址 `http://localhost:${APP_PORT:-5002}`。这样它与 README 中的推荐运行方式保持一致。

### 3.3 Docker runtime 依赖修复

上传 `.rar` / `.7z` 任务会通过 `internal/archive/external_extractor.go` 调用：

```text
bsdtar -xf <archive> -C <temp_dir>
```

runtime image 应安装 `libarchive-tools`，确保容器内存在 `bsdtar`。这属于运行 bug 修复，不改变业务逻辑。

### 3.4 文档与 runbook 校准

更新当前事实文档：

- `README.md`
- `docs/architecture/current-system-overview.md`
- `docs/architecture/config-inventory.md`
- `docs/architecture/task-lifecycle-baseline.md`

文档中的当前主线统一描述为：

```text
gateway -> go-api -> postgres
go-worker -> postgres
```

历史设计文档和旧计划可保留 Redis / legacy 描述，因为它们记录过去阶段，不作为当前运行事实。但当前 README、architecture baseline 和运行脚本不能继续误导新接手者。

### 3.5 契约测试与验证

新增或更新测试来固定以下事实：

1. Compose 不声明 Redis 服务。
2. Compose 不向 Go 服务注入 `REDIS_URL`、`STREAM_NAME`、`CONSUMER_GROUP`。
3. `Config` 不暴露 Redis / stream 字段。
4. `scripts/start_local.sh` 不引用 `python`、`pip`、`requirements.txt`、`FLASK_APP`、`flask run` 或 `app.py`。
5. Dockerfile runtime image 安装 `libarchive-tools`。
6. README 当前拓扑不再把 Redis Streams 描述成默认运行主线。

实现时遵循 TDD：先更新契约测试并确认失败，再改 Compose、配置、脚本和文档让测试通过。

## 4. 影响范围

### 会修改

- `docker-compose.yml`
- `.env.example`
- `Dockerfile`
- `internal/config/config.go`
- `internal/config/config_test.go`
- `internal/config/compose_contract_test.go`
- `scripts/start_local.sh`
- `scripts/verify_no_legacy_runtime.sh`
- `README.md`
- `docs/architecture/current-system-overview.md`
- `docs/architecture/config-inventory.md`
- `docs/architecture/task-lifecycle-baseline.md`

### 可能修改

- `go.mod`
- `go.sum`

如果 Redis client 已无任何 Go import，运行 `go mod tidy` 移除未使用依赖。若仍存在测试或工具引用，应先定位引用用途，不做盲删。

### 不修改

- `internal/store/postgres/taskcore/store.go`
- `internal/app/taskcore/*`
- `internal/worker/taskcore/*`
- `frontend/src/*`
- `web/static/dist/*`
- `internal/store/postgres/migrations/*`

本阶段不触碰核心任务行为和前端 bundle。

## 5. 风险与处理

### 5.1 Redis 删除导致隐藏依赖暴露

风险：某些测试或运行路径仍隐式依赖 Redis env。

处理：

- 先用 `rg` 审计 Redis / stream 引用；
- 对每个引用判断是当前运行路径、历史文档还是旧计划；
- 当前运行路径引用必须删除或重命名；
- 历史文档不改，避免篡改阶段记录。

### 5.2 本地脚本改为 Compose 依赖 Docker

风险：用户希望不通过 Compose 直接本地跑 Go。

处理：

- 本阶段优先统一现有 README 推荐路径；
- 如需纯本地 Go dev server，后续单独设计 `scripts/start_go_local.sh`，明确依赖外部 Postgres。

### 5.3 Alpine 包名不匹配

风险：`libarchive-tools` 包名在构建环境中不可用。

处理：

- 通过 Docker build 或 Compose build 验证；
- 如果包名失败，改用 Alpine 中实际提供 `bsdtar` 的包，并把契约测试改为检查最终 Dockerfile 包含正确包名。

### 5.4 go mod tidy 删除过多依赖

风险：删除间接依赖造成工具或未来迁移编译失败。

处理：

- 只接受 `go test ./...` 后仍通过的 tidy 结果；
- 不删除仍被 `tools/migration` 或测试使用的依赖。

## 6. 验收标准

本阶段完成时必须满足：

1. `rg -n "REDIS_URL|STREAM_NAME|V2_STREAM_NAME|CONSUMER_GROUP" docker-compose.yml .env.example README.md docs/architecture internal/config` 不再出现当前运行主线残留。
2. `rg -n "FLASK_APP|flask run|requirements.txt|app.py" scripts/start_local.sh README.md .github/workflows/ci.yml docker-compose.yml scripts/verify_release_gates.sh` 无旧 Python runtime 引用。
3. `docker-compose.yml` 不包含 `redis:` 服务。
4. `Dockerfile` runtime stage 安装 `libarchive-tools`，并通过构建或静态契约测试证明容器内 RAR / 7Z 解包依赖已声明。
5. `go test ./...` 通过。
6. `npm run test:frontend` 通过。
7. `npm run lint` 通过。
8. `npm run build` 通过。

如果本地 Docker 环境可用，额外运行：

```bash
INTERNAL_ENQUEUE_TOKEN=test-token docker compose config
```

如果 Docker 不可用，记录未运行原因，并用契约测试覆盖 Compose 静态结构。

## 7. 后续阶段

完成本阶段后，下一阶段再进入架构升级：

1. 拆分 `internal/store/postgres/taskcore/store.go` 为 create / claim / transition / query / scan 文件；
2. 将 HTTP Task Core handler 按 URL、upload、actions、artifact 拆分；
3. 清理 `internal/domain` 中旧 legacy 类型与 Task Core 类型的命名冲突；
4. 再进入性能阶段：下载器内存占用、CBZ 流式写入、上传解包流式化和 worker 并发控制。
