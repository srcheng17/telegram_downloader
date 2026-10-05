# 设计

## 最小边界
修正错误发生的层，保留所有现有模块、状态与接口。下载超时判定应检查实际执行上下文，不把 HTTP 单次请求的 DeadlineExceeded 当作父任务取消。归档内部共用自然数字比较，避免新依赖；比较数字长度及内容以避免整数溢出。CBZ 现有输出规则除必要排序外保持不变。

HTTP URL 规范化正确维护 URL.Path/RawPath，不把已编码路径当未编码路径再次转义。查询截断保留有效 UTF-8（长度限制的意图保持不变）。

URL 重试与创建使用一致的 canonical advisory lock，采用一致锁顺序避免死锁；重试检查排除自身，对其他活跃 URL 任务返回现有 ErrConflict。SQL 状态、事件和 generation 契约保持一致，无 schema migration。

日志模块分开编辑中的表单与已提交筛选；刷新只更新结果。新请求发起时清除旧 timer，只有当前请求有权重排 timer。初始化/主动清空等明确操作可同步表单。

## 文件所有权
- 下载 worker：internal/downloader、internal/archive 及必要 internal/worker/taskcore 取消判定。
- 后端 worker：internal/httpapi/api.go、logs_query.go 及其测试，internal/store/postgres/taskcore/store.go 和测试。
- 前端 worker：frontend/src/logs/index.js、回归测试、web/static/dist 构建产物。
- 主 agent：Trellis 文档、验证记录、隔离测试环境、Telegram 验证准备。

## 兼容与回滚
现有 HTTP 字段、数据库 schema、部署变量保持兼容。旧 URL 不批量重写；新提交采用修正编码。回滚代码即可，无数据反向迁移。未执行生产访问或变更。
