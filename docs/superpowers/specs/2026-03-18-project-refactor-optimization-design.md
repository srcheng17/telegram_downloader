# 整仓重构优化设计（2026-03-18）

## 背景

当前仓库已经完成多轮 Go 主线收敛与前端模块化改造，但整体结构仍存在明显历史包袱：

- Go 主业务链路位于 `go-backend/` 子目录，主仓结构与真实运行主线不一致。
- Python compatibility bridge、根目录旧脚本、旧模板、旧静态入口仍与当前主线并存。
- 前端虽然已有 `frontend/src/` 模块化结构，但模板入口、历史脚本与构建产物仍未彻底收成一条链路。
- HTTP handler、worker、service、queue、store 之间仍有业务规则分散、状态语义重复和错误映射多点维护的问题。
- 测试、CI、Compose、runbook 已有基础，但仍带有兼容期痕迹，尚未完全围绕“纯 Go 单栈”形成最终标准。

本次工作不是再做一次局部整理，而是发起一次**整仓分阶段重构优化工程**，目标是在保留现有核心功能的前提下，完成从“历史双栈/过渡结构”到“纯 Go 单栈、长期可维护仓库”的彻底收敛。

## 已确认约束

- **范围**：A/B/C/D 全部都做，即架构收口、后端重构、前端重构、测试与部署优化一起推进。
- **变更类型**：允许破坏性调整。
- **目标运行形态**：最终收敛为纯 Go 单栈。
- **功能策略**：现有核心功能尽量全保留。
- **优先级**：维护性、性能、稳定性、运维性都要，核心是长期可维护。
- **交付方式**：分阶段落地，每阶段可运行、可验收、可回滚。

## 目标

### 业务目标

在不丢失现有核心能力的前提下，完成整仓结构、后端核心、前端入口、测试链路、部署链路与历史兼容层的统一收敛。

### 技术目标

1. Go 成为唯一业务运行时。
2. 仓库结构直接表达当前主线架构，而不是让主线藏在子目录中。
3. HTTP、worker、业务流程、领域规则、基础设施边界清晰。
4. 前端只保留一个源码事实来源和一条构建产物链路。
5. 测试、CI、Compose、迁移文档、回滚文档全部围绕单主线组织。

### 非目标

- 不在本轮引入新的前端框架或重写 UI 技术栈。
- 不无边界增加新业务能力。
- 不为了“看起来更现代”而进行与目标无关的大规模花哨重命名。

## 可选路径与采用方案

### 方案 1：一次性重写后整体切换

优点：最终形态最干净。

缺点：风险高、中间不可验收，不符合本项目“分阶段可运行”的要求。

### 方案 2：分阶段收敛，阶段间始终可运行（采用）

先冻结边界与目标架构，再按后端核心、前端边界、测试交付、兼容层下线逐段推进。每阶段都要求可测试、可回滚、可交付。

### 方案 3：以代码整理为主，保留大部分历史运行结构

优点：短期成本较低。

缺点：无法真正达到“纯 Go 单栈”和“长期可维护”的最终目标。

### 采用理由

方案 2 最符合已确认约束：既允许破坏性调整，又能保持阶段性可运行；既覆盖整仓收敛，又能控制迁移风险；既服务长期维护性，也能逐步兑现稳定性、性能和运维目标。

## 总体阶段划分

本次整仓重构定义为一个总项目，拆成 4 个阶段性子项目。

### S1：后端核心收敛

目标：

- 将任务创建、复用、取消、状态推进、产物登记等核心业务流程统一到 Go 应用服务层。
- 让 HTTP 层与 worker 层只保留协议转换和执行入口职责。
- 清理状态映射、下载规则、错误分类的重复定义。

阶段完成态：

- Go 后端形成稳定的业务核心。
- Python compatibility bridge 不再承载业务判断。

### S2：前端边界收敛

目标：

- `frontend/src/` 成为唯一前端源码事实来源。
- 页面入口只做装配，轮询、错误映射、下载预检、消息处理收敛到共享模块。
- 模板只引用构建产物，不再混用历史入口脚本。

阶段完成态：

- 前端模块边界清晰。
- 页面行为与后端契约对齐。

### S3：测试与交付链路收敛

目标：

- 重排单元测试、集成测试、契约测试、E2E 的职责边界。
- 收敛 CI 门禁、Compose 拓扑、runbook 与发布/回滚流程。

阶段完成态：

- 变更具备稳定验证路径。
- 部署和回滚路径清晰一致。

### S4：兼容层下线与仓库清理

目标：

- 删除 Python 主链路残留与重复入口。
- 清理重复脚本、重复模板、重复静态资源。
- 收口 README、迁移文档、运维文档。

阶段完成态：

- 仓库真正成为纯 Go 单栈项目。
- 后续演进不再受历史结构拖累。

## 迁移时序原则

为了兼顾“最终结构正确”和“每阶段可运行可验收”，本次重构采用**先逻辑收敛、后物理迁移**的时序原则：

1. **S1 先在现有 `go-backend/` 内完成业务分层和职责收敛**，避免一开始就触发跨仓目录大挪动。
2. **S2 在保持现有运行链路稳定的前提下收敛前端入口与共享模块**，先统一行为，再移动承载目录。
3. **S3 先收敛测试、Compose、CI 与文档门禁**，确保在大规模目录迁移前已有稳定护栏。
4. **S4 再执行物理层面的目录提升与历史代码下线**，包括将 Go 主线从 `go-backend/` 提升到仓库主结构、移除 Python 主链路和旧模板/静态入口。

这意味着：

- 最终目标结构用于定义完成态；
- 具体实现计划必须避免在第一阶段同时做“业务重写 + 目录大搬迁”；
- 目录提升应作为后段显式任务来执行，而不是在所有阶段中隐式穿插。

## 最终仓库结构设计

建议将当前 `go-backend/` 提升为主仓结构，最终形态类似：

```text
/
  cmd/
    api/
    worker/

  internal/
    domain/
    app/
    transport/
      http/
      web/
      worker/
    infra/
      postgres/
      redis/
      downloader/
      archive/
      filesystem/
    platform/
      config/
      logging/
      bootstrap/

  frontend/
    src/
      home/
      logs/
      settings/
      shared/

  web/
    templates/
    static/
      dist/
      style.css

  migrations/
  tests/
    e2e/
    contract/
  deploy/
  docs/
  scripts/
```

### 结构原则

1. **后端只保留一条主线**：业务逻辑在 `internal/` 中表达，不再同时依赖根目录旧脚本和 `go-backend/` 子仓结构。
2. **前端只保留一条主线**：`frontend/src/` 作为唯一源码来源，`web/static/dist/` 作为唯一运行产物。
3. **模板只承担页面壳层职责**：只引用 bundle，不再混入历史页面脚本。
4. **历史代码退出主链路**：原 `telegram_downloader/`、`app.py`、`downloader_logic.py`、`task_store.py`、根目录旧模板、旧静态入口全部删除或归档。

## 后端核心重组设计

### 分层目标

后端要解决的不是“文件再拆细一点”，而是把真正的业务语义集中起来。

#### `domain`

只放稳定业务规则，不依赖 HTTP、HTML、Redis、Postgres。

建议包含：

- `task`：任务实体、状态、状态流转规则
- `artifact`：产物实体、可下载性判断、缺失/过期语义
- `download`：URL 规范化、图片约束、抓取失败分类
- `metadata`：作者、系列、标题、摘要、标签、类型的归一化与校验
- `errors`：统一业务错误模型

核心原则：任务为什么能复用、为什么失败、为什么能取消、为什么可下载，必须由领域规则单点决定。

#### `app`

负责业务用例编排，整合领域规则与基础设施接口。

建议核心用例：

- `CreateTask`
  - 输入归一化与校验
  - canonical URL 去重
  - 复用成功任务 / 复用活跃任务 / 创建新任务决策
- `CancelTask`
  - 取消合法性判断
  - 记录取消意图
  - 保证对 worker 幂等
- `ListTasks` / `GetTask`
  - 列表查询、详情查询、筛选条件归一化、summary 聚合
- `ClaimArtifact`
  - 产物登记
  - 下载可用性判断
  - 缺失/过期/损坏错误统一
- `RunTask`
  - worker 执行主流程：抓取页面、下载图片、生成 CBZ、回写状态与产物

#### `transport`

- `http/`：API 路由、请求/响应 DTO、错误到 HTTP 映射
- `web/`：HTML 页面路由、模板绑定、页面初始化数据装配
- `worker/`：队列消息解包与应用服务调用

原则：transport 层不存放核心业务判断。

#### `infra`

- `postgres/`：任务与配置仓储实现
- `redis/`：队列、锁、流、事件
- `downloader/`：Telegraph 页面抓取与图片下载实现
- `archive/`：CBZ 与 `ComicInfo.xml` 生成
- `filesystem/`：产物存储、路径解析、清理

#### `platform`

- `config/`：配置读取与校验
- `logging/`：日志与观测
- `bootstrap/`：依赖装配

### 状态机收敛

状态枚举、允许流转路径和状态含义必须单点定义，不能继续由 handler、worker、前端各自推断。

推荐将核心状态统一为：

- `queued`
- `running`
- `succeeded`
- `failed`
- `cancelled`

如有必要可引入过渡状态（如 `waiting_reuse_confirmation`、`cancelling`），但必须仍由统一状态机定义。

### 错误模型收敛

推荐统一错误类别：

- `validation_error`
- `conflict_error`
- `dependency_error`
- `artifact_error`
- `internal_error`

这样可以统一 API 结构、前端提示策略、日志统计与 E2E 断言。

### worker 定位

worker 只负责：

1. 取消息
2. 解包任务标识
3. 调用 `app.RunTask`
4. ack / retry / reclaim
5. 记录执行日志和指标

worker 不应继续承载第二套业务系统，不应自己藏状态合法性、错误分类、文件命名规则等业务语义。

### 仓储接口原则

仓储围绕业务意图定义，而不是围绕表结构定义，例如：

- `FindReusableTaskByURL`
- `CreateTask`
- `MarkTaskRunning`
- `MarkTaskSucceeded`
- `MarkTaskFailed`
- `RequestTaskCancellation`
- `SaveArtifact`
- `ListTasks`

## 前端收敛设计

### 最终职责

前端只负责：

1. 采集输入
2. 调用后端契约
3. 将后端明确语义渲染成界面

前端不再负责猜测任务状态、拼装复杂错误语义、推断下载可用性。

### 页面入口

保留 3 个页面入口，但每个入口只做装配：

- `frontend/src/home/`
  - 下载表单
  - 复用确认
  - 首页 summary
- `frontend/src/logs/`
  - 任务列表
  - 状态筛选
  - 下载预检
- `frontend/src/settings/`
  - 设置读取和保存

页面入口不再直接堆积 fetch 细节、轮询策略、消息映射、错误 fallback。

### 共享模块建议

- `api/`
  - `createTask`
  - `listTasks`
  - `getSummary`
  - `cancelTask`
  - `headArtifact`
  - `saveSettings`
- `models/`
  - 最薄的数据整形，只做展示转换，不改变业务语义
- `modules/`
  - `polling`
  - `server_messages`
  - `archive_upload`
  - `task_filters`
  - `download_preflight`
- `bootstrap/`
  - 页面模块装配、初始化顺序、清理逻辑

### 前后端契约原则

后端应尽量直接返回可供页面消费的明确语义，例如：

- `status`
- `status_label`
- `can_cancel`
- `can_download`
- `download_error_code`
- `user_message`

前端不再根据多个原始字段自行推导状态含义。

### 模板与静态资源原则

最终只保留链路：

```text
frontend/src/* -> vite build -> web/static/dist/*
web/templates/* -> 引用 dist bundle
```

因此需删除或停用：

- `static/app.js`
- `static/index.js`
- `static/logs.js`
- 根目录历史模板中的旧入口依赖

## 测试、CI、Compose、部署与迁移设计

### 测试分层

#### 1. 单元测试

覆盖：

- 状态机
- metadata 归一化
- 错误分类
- 产物命名规则
- 前端共享纯函数

#### 2. 集成测试

覆盖：

- app service + repository
- app service + queue
- API handler + service
- worker + service

#### 3. 契约测试

覆盖：

- API 请求/响应结构
- 错误响应结构
- 模板引用 bundle 的约束
- compose 关键服务合同

#### 4. E2E 测试

只保留少量高价值主流程：

- 提交任务
- 查看日志/任务列表
- 下载预检
- 设置保存
- 失败提示
- 取消流程

### CI 门禁

建议最终顺序：

1. `go test ./...`
2. `go test -race ./...`
3. 前端单测
4. 前端 lint
5. 前端 build
6. 契约测试
7. 兼容层残余检查（仅兼容层仍存在时保留）
8. E2E
9. Compose smoke / `readyz`

兼容层下线后，Python 回归测试从主门禁中删除。

### Compose 收敛

最终 Compose 只表达纯 Go 主线：

- `gateway`
- `api`
- `worker`
- `postgres`
- `redis`

不再保留 Python web、Python worker 或仅为过渡存在的重复服务定义。

### 文档要求

#### 迁移文档

必须说明：

- 旧目录到新目录映射
- 旧接口到新接口映射
- 配置项变更
- 数据迁移/清理步骤
- Python compatibility bridge 的下线节点

#### 运维文档

必须说明：

- 本地启动
- CI 验证
- Compose 验收
- 发布步骤
- 回滚步骤
- 常见故障排查

## 风险与控制策略

### 风险 1：整仓改动过大导致阶段失控

控制：严格按 S1~S4 分阶段推进，每阶段定义可验证完成态，不跨阶段同时做大范围删改。

### 风险 2：过早删除兼容层导致行为回归

控制：在 S1/S2 期间保留最小兼容回归测试，只在 S4 明确下线，并先完成迁移说明。

### 风险 3：前后端契约在重构中频繁震荡

控制：引入契约测试，后端明确输出页面语义字段，前端不自行猜测业务状态。

### 风险 4：目录重组影响开发效率

控制：先完成结构映射与计划文档，再按阶段迁移；每阶段结束都保证入口、测试与文档可用。

## 完成定义（DoD）

当以下条件全部满足时，可认为本次整仓重构优化完成：

- 纯 Go 主线可独立运行。
- 现有核心功能仍可用。
- 前端只保留一条源码到构建产物链路。
- Python 不再参与主运行链路。
- 测试分层清晰且门禁稳定通过。
- CI、Compose、README、runbook、migration docs 已同步收敛。
- 仓库结构能直接表达系统架构，新增开发不再依赖历史残留路径。

## 下一步

本设计获批后，进入 implementation plan 阶段，输出按阶段执行的详细任务拆解，包括：

- 每阶段目标文件映射
- 拆分顺序
- TDD/验证步骤
- 迁移顺序
- 每阶段验收命令
- 推荐提交粒度与回滚点
