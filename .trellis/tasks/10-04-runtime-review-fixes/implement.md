# 执行计划

1. 已批准审查结论与本文件核对；已有隔离 worktree，记录基线通过情况。
2. 三个互不冲突实现分支并行：下载/归档、HTTP/SQL、日志前端。每项先补回归证据，再做最小修复。
3. 使用专用临时 PostgreSQL 容器进行单轮串行 Go test 和 race，禁止共享生产库；检查 retry/create 竞争与错误码。
4. 完成 Go vet、Node tests、lint、Vite build、bundle 一致性和隔离 Compose E2E。
5. 独立 reviewer 检查锁顺序、取消传播、编码语义、前端迟到回调；修复有效问题并重跑受影响检查。
6. 更新相关 spec 和验证记录。代码与 Trellis 台账一同审阅；如本地提交，使用英文 Conventional Commits。不得推送/合并/部署。

Telegram 子任务独立保持 planning，先准备用户的 api_id/api_hash 配置与只读验证，不把未知账号权限当作已通过。
