# 元数据拆分、移动端统计折叠与二期架构升级设计

## 背景
当前系统已支持 CBZ 元数据，但 `Tags` 与 `Genre` 复用同一输入值；首页与日志页统计卡在移动端首屏占位较大；下载执行仍以内嵌线程池为主。用户要求：
1. 标签与类型独立填写并分别写入 ComicInfo。
2. 统计信息在移动端更优雅展示（选 B：默认折叠）。
3. 采用二期架构升级（选 A：Flask + Celery + Redis + Postgres + Compose），允许短暂停机迁移。

## 目标
- 完成元数据字段拆分：`tags` 与 `genres` 独立处理。
- 完成首页/日志移动端统计折叠，PC 端保持展开。
- 引入可运行的 Celery/Redis/Postgres 架构与迁移脚本，并保持现有 API 行为兼容。

## 非目标
- 不在本次实现复杂灰度双写与零停机迁移。
- 不引入新的前端框架或后端 ORM 大重构。

## 方案概览

### 1) 元数据与 ComicInfo 映射
- 新增输入字段：`genres`（类型，可选）。
- 后端新增解析与存储字段：
  - `genres_raw`
  - `genres_normalized`
- ComicInfo 映射改为：
  - `Tags = tags_normalized`
  - `Genre = genres_normalized`
- 两者不互相回填，均支持中英文逗号分隔。

### 2) 移动端统计折叠（B）
- 首页与日志页统计区域改为 `<details>` 容器。
- 默认策略：
  - 桌面端：展开
  - 移动端：折叠
- 通过前端 mount 阶段根据 `matchMedia` 控制 `open` 状态，避免影响桌面端现有布局和 E2E 行为。

### 3) 二期架构升级（A）
- 新增组件：
  - `redis`：队列 broker
  - `postgres`：任务存储
  - `worker`：Celery worker 执行下载
- `web` 进程改为“提交任务 + 入队”，下载执行交由 worker。
- 兼容策略：保留当前 orchestrator 接口，对调用方透明。

### 4) 数据迁移
- 提供停机迁移脚本：`scripts/migrate_sqlite_to_postgres.py`
- 步骤：
  1. 停机（冻结写入）
  2. 从 SQLite 读取任务
  3. 写入 Postgres
  4. 启动新栈并冒烟验证
- 失败回滚：保留 SQLite 备份和旧镜像。

## 风险与应对
- **连接配置风险**：通过环境变量默认值与启动日志提示降低误配概率。
- **并发语义变化**：在 Postgres 的 claim 路径使用事务级 advisory lock 保证同 URL 原子决策。
- **依赖升级风险**：保留本地开发默认模式；测试环境优先走 SQLite/本地执行，避免强依赖 Redis/Postgres。

## 验证
- Python 全量测试通过。
- Playwright E2E 通过（含移动端统计折叠与元数据分离场景）。
- Compose 启动后可完成：提交任务、日志查看、下载产物与 ComicInfo 校验。
