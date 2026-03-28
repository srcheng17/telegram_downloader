# 项目整体架构重构与优化设计

- 日期：2026-03-29
- 主题：project-architecture-refactor
- 范围：代码架构、部署运行、测试体系、开发流程、文档规范、分支协作
- 重构策略：可接受激进重构，但按阶段落地，始终保留核心功能回归门禁

## 1. 背景与目标

项目已经从“可用原型”演变为“持续承载复杂功能的工程系统”。现状表明：

- 核心运行时已经收敛到 Go API + Go Worker + PostgreSQL + Redis Streams + Nginx。
- 业务中心已经天然收敛为“任务系统”：URL 下载、压缩包上传、进度、取消、重试、产物下载、Komga 复制、日志展示。
- 项目具备容器化、自动化测试、CI 和多轮真实迭代经验。

问题不在于“功能不可用”，而在于“功能增长速度已经超过原结构承载能力”。本次重构的目标不是更换技术栈，而是将项目收敛为一个**任务领域驱动、边界清晰、可持续演进、运行方式标准化**的工程化系统。

### 重构目标

1. **任务领域成为单一主轴**：任务状态、进度、动作资格、命名、产物语义等规则统一收口。
2. **接口层收薄**：UI / legacy / v2 只负责协议适配，不承载业务规则。
3. **前端模块化**：页面脚本走向页面模块，统一 view model 映射。
4. **运行与发布一致**：本地、Compose、发布、热修、排障形成可复制流程。
5. **测试围绕架构服务**：领域规则、应用用例、基础设施、E2E 分层明确。
6. **文档与流程固化**：模块职责、决策记录、开发规则形成长期约束。

## 2. 当前项目体检

### 2.1 现状优势

#### 技术主线已统一

当前生产主线明确为：

- `go-api`
- `go-worker`
- `postgres`
- `redis`
- `nginx`

这意味着项目不需要经历多运行时混战下的“清场式”重构，而是可以在已有单主线基础上做结构性收敛。

#### 核心业务模型已经成熟

项目最核心的行为链路已经清楚：

- 创建任务
- 任务入队
- worker 执行
- 状态流转
- 产物生成
- 日志呈现
- 下载 / Komga 复制
- 重试 / 取消

这给重构提供了一个天然中心：**任务领域**。

#### 具备基本工程化护栏

项目已经拥有：

- Go 单元/集成测试
- race test
- 前端测试
- Playwright E2E
- Docker Compose
- GitHub Actions CI

这说明重构可以建立在可验证基础上，而不是“盲改”。

#### 团队正在处理真实问题

近期提交已覆盖：

- 上传任务主链路
- Komga 复制挂载问题
- URL 任务进度
- 取消后重试
- 日志字段映射

这表明项目已进入“需要把增量修复系统化”的阶段。

### 2.2 核心问题

#### 后端分层存在重叠与职责交叉

现有目录体现出多个层级：

- `internal/httpapi`
- `internal/httpv2`
- `internal/app`
- `internal/service`
- `internal/worker`
- `internal/store`
- `internal/domain`

问题不是层少，而是职责边界不够刚性。容易出现：

- handler 承担业务判断
- worker 承担流程规则
- adapter 承担状态语义转换
- store 返回对象混入展示含义

表面分层，实际耦合。

#### legacy 与 v2 共存增加理解成本

当前处于“兼容旧接口 + 新主线落地”的过渡期。需求实现经常需要同时修改：

- v2 任务模型
- legacy 日志映射
- httpapi 兼容层
- 前端字段判断

这会提高漏改概率与理解成本，并使 adapter 不断膨胀。

#### 任务领域尚未彻底成为第一公民

任务相关规则仍有分散风险：

- 重试资格
- 取消资格
- 进度显示
- 文件名规则
- Komga 路径规则
- 日志动作显隐

同一业务概念在多处维护，导致“一个需求改四层”的情况持续出现。

#### 前端复杂度已超过散脚本阈值

当前前端承担了越来越多的状态逻辑：

- 首页 URL / 上传模式切换
- 元数据历史回填
- 日志页状态渲染
- 按钮显隐
- 下载 / Komga 模式切换
- 错误反馈与文案映射

如果继续以零散 DOM 操作 + formatter 为主，维护成本会持续上升。

#### 运行部署存在隐性知识

项目存在多项“经验性规则”：

- 前端源码修改后必须同步更新 bundle
- 容器网络/网关 upstream 刷新存在特殊行为
- Komga 目录需要容器路径与宿主机路径对齐
- 热修可能依赖二进制拷贝进容器

这类知识如果不制度化，会持续形成交付风险。

#### 文档偏“结果记录”，还不够“行为约束”

已有文档较丰富，但对后续开发最有价值的仍然欠缺：

- 模块职责定义
- 状态机定义
- API 契约边界
- 配置归属规则
- 前后端字段映射规则
- 兼容层演进约束

### 2.3 问题优先级

#### P1：必须优先处理

1. 任务领域规则分散
2. legacy / v2 过渡层职责过重
3. 前端任务状态与动作映射分散
4. 运行配置与部署语义不统一

#### P2：应尽快处理

1. 大文件 / 高耦合模块拆分
2. 测试分层清晰化
3. 配置与环境变量治理
4. 文档从说明型升级为约束型

#### P3：可后置

1. 全面替换前端技术栈
2. 一次性重建数据库模型
3. 完全推翻现有 API 形态
4. 大规模目录名与文件名“美化式”重命名

## 3. 目标架构

目标架构以“任务领域驱动”为核心，建议收敛为五层。

### 3.1 Domain 层

职责：表达纯业务规则，不依赖 HTTP、数据库、Redis、模板或 UI。

应集中收敛：

- 任务类型与任务状态
- 状态流转规则
- 重试 / 取消资格
- 进度语义
- 文件名生成规则
- 元数据规范化规则
- Komga 目标路径规则

目标：任何“规则问题”都优先在 Domain 层找到答案。

### 3.2 Application 层

职责：编排用例，不关心协议细节与底层实现。

核心用例应包括：

- CreateURLTask
- InitUploadTask
- AttachUploadSource
- CancelTask
- RetryTask
- CopyTaskResultToKomga
- DownloadTaskResult
- RecordMetadataHistory
- ListTaskLogs

Application 层依赖：

- domain
- repository interfaces
- queue interfaces
- file storage interfaces

### 3.3 Interface 层

建议分为三类：

1. **Web UI handlers**：负责首页、日志页、设置页、模板渲染。
2. **Public / legacy adapters**：负责现有前端依赖接口兼容与字段适配。
3. **Internal / v2 API**：供 worker / admin / future clients 使用的更干净接口。

原则：Interface 层只做协议适配，不能承载业务规则。

### 3.4 Infrastructure 层

职责：技术实现细节。

包括：

- PostgreSQL repositories
- Redis Streams queue
- File storage
- Archive extraction
- Telegraph downloader
- Environment/config readers

原则：技术细节留在 Infrastructure，业务规则不能反向沉淀到这里。

### 3.5 Worker Runtime 层

职责：异步任务驱动器。

包括：

- 消费任务
- 调用 application 用例
- 处理中断 / 取消
- 上报进度
- 推进状态落库

原则：worker 负责执行与协调，不成为业务规则的第二宿主。

### 3.6 目标目录方向（建议）

```text
internal/
  domain/
    task/
    metadata/
    naming/
    komga/
  app/
    task/
      create_url_task.go
      init_upload_task.go
      attach_upload_source.go
      cancel_task.go
      retry_task.go
      copy_to_komga.go
      list_logs.go
  interfaces/
    web/
    api/
    admin/
  infra/
    postgres/
    redisstream/
    filestore/
    archive/
    telegraph/
  worker/
    runner/
    executor/
```

这不是强制一次性改名方案，而是长期演进的结构目标。

## 4. 前端目标架构

不建议当前阶段引入大型 SPA 框架。问题不在框架缺失，而在模块边界不足。

### 4.1 页面边界组织

建议目录方向：

```text
frontend/src/
  home/
  logs/
  settings/
  shared/
```

每个页面模块内部按职责拆分：

- `api.js`
- `state.js`
- `render.js`
- `actions.js`
- `formatters.js`

### 4.2 引入统一 view model 映射

对日志页尤其重要。应统一由 mapper 决定：

- 状态文案
- 进度文案
- 错误展示
- 按钮显隐
- 行动作能力

避免多处直接解读后端字段。

### 4.3 DOM 操作与业务语义分离

前端负责“如何渲染”，后端/领域负责“什么语义成立”。

例如：

- 前端不定义“哪些状态可重试”
- 前端只消费 `can_retry` / `retryable` 等统一语义

## 5. 测试目标架构

### 5.1 Domain tests

重点覆盖：

- 状态流转
- 重试 / 取消资格
- 文件名规则
- 元数据清洗
- Komga 路径规则

### 5.2 Application tests

重点覆盖：

- 创建任务是否正确入队
- 上传流程是否正确编排
- 取消 / 重试是否复用同一任务
- 下载 / 复制是否走正确动作

### 5.3 Infrastructure tests

重点覆盖：

- SQL 仓储
- Redis queue
- 文件处理
- 压缩包解析

### 5.4 E2E tests

只覆盖关键用户路径：

- URL 下载
- 上传生成 CBZ
- 失败重试
- 取消
- Komga 复制
- 设置切换

原则：E2E 做关键链路兜底，不承载全部细节验证。

## 6. 运行部署目标架构

### 6.1 明确三种运行模式

#### 开发模式

需要明确：

- API / worker 启动方式
- 前端 bundle 构建或 watch 方式
- 调试数据与本地依赖获取方式

#### Compose 集成模式

需要明确：

- 一条命令启动完整环境
- 一条命令验证健康状态
- 必需挂载目录与默认配置

#### 发布模式

需要明确：

- 镜像构建与版本化方式
- 配置注入规则
- 升级 / 回滚手册

### 6.2 配置治理

环境变量应分组治理：

- App 行为
- Queue / Worker
- Storage / Paths
- UI / Compatibility
- Debug / Ops

### 6.3 消除隐性知识

重点治理：

- 前端变更与 bundle 更新关系
- gateway upstream 刷新策略
- Komga 根目录挂载语义
- 热修/灰度方式

## 7. 文档与流程目标架构

### 7.1 文档分层

#### 架构文档

- 当前架构图
- 目标架构图
- 模块职责边界

#### 运行文档

- 本地启动
- Compose 启动
- 发布/回滚
- 常见故障排查

#### 开发约束文档

- 新功能应放哪层
- handler / adapter 禁止承载什么逻辑
- 前端页面模块组织规则
- 测试落点规则

#### 决策记录

例如：

- 为什么保留 legacy adapter
- 为什么前端不切 SPA
- 为什么 worker 维持当前执行模型

### 7.2 流程目标

建议形成标准节奏：

- spec → plan → implementation → verification → merge

并要求：

- 重构类改动必须附回归清单
- 跨层改动必须说明边界影响
- 新增规则必须说明放入 domain/app/interface/infra 的原因

## 8. 分阶段重构路线图

### Phase 0：盘点与门禁收紧

目标：在真正重构前建立安全护栏。

工作：

- 产出当前模块图、关键调用链、任务状态机、配置清单
- 识别高耦合文件与超长模块
- 固化回归命令
- 建立重构守则（handler / adapter / front-end mapping 的约束）

收益：降低后续激进重构的迷失风险。

### Phase 1：任务领域收口

目标：让任务成为真正单一主轴。

工作：

- 抽出任务领域模型与状态规则
- 抽出元数据规范化与命名规则
- 定义清晰 task use cases
- 降低 worker 中业务规则密度

收益：解决“同一规则多处维护”的根问题。

### Phase 2：接口层与前端映射收敛

目标：让 adapter 变薄，让前端消费统一语义。

工作：

- UI / legacy / v2 职责切分
- 缩薄 legacy adapter
- 建立统一日志 view model mapper
- 以页面为边界整理前端模块

收益：降低前端与兼容层的漏改概率与认知负担。

### Phase 3：运行部署与配置治理

目标：把运行方式从“靠经验”变成“可复制”。

工作：

- 环境变量分层整理
- Compose / 构建 / 健康检查规范化
- 明确前端 bundle 生成策略
- 标准化常见故障排查路径

收益：提升交付稳定性与接手效率。

### Phase 4：文档、流程、开发规范固化

目标：防止重构成果退化。

工作：

- 架构图与模块职责文档
- 开发约束文档
- 决策记录
- 分支与提交流程规范

收益：把重构经验变成长期规则。

## 9. 推荐执行优先级

### 第一优先级

- Phase 0
- Phase 1

### 第二优先级

- Phase 2

### 第三优先级

- Phase 3
- Phase 4

原因：最先影响开发效率与系统稳定性的，是任务主干与接口映射，而非外围文档与部署美化。

## 10. 三个里程碑

### Milestone 1：任务主干重构完成

完成标志：

- 状态、进度、重试、取消、命名规则统一收口
- worker 主链路清晰
- 核心测试补齐

### Milestone 2：界面与接口一致性完成

完成标志：

- legacy adapter 收薄
- 前端日志 / 首页映射统一
- 用户可见状态与后端语义保持一致

### Milestone 3：工程化收尾完成

完成标志：

- Compose / build / runbook / 配置 / 文档 / 流程收敛
- 发布与排障方式标准化

## 11. 建议结论

本次重构不应理解为一次性“全盘推翻”，而应理解为：

1. 先建立架构与回归基线；
2. 再把任务主干抽离为真正单一主轴；
3. 再把接口、前端、运行体系都挂回这条主干；
4. 最后用文档与流程巩固成果。

一句话总结：

> 把当前“多层能跑的系统”，重构成“任务领域驱动、边界清晰、工程化可持续演进的系统”。
