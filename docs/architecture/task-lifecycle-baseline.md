# 自动入库任务生命周期

```text
queued -> running -> delivering -> succeeded
queued -> cancelled
running -> failed / cancelled
delivering -> failed
failed / cancelled -> queued（手动重试）
```

以 store.py 和 API 返回的 status_label、phase、available_actions 为准。succeeded 的产品含义是「已入库」，不是仅已生成 CBZ。可用动作只有 cancel/retry，delivering 不提供取消。

## 执行与恢复

URL 或完整上传源持久化后排队，后台串行处理。启动先取得 DATA_DIR 独占锁，queued 保留，遗留 running/delivering 标为中断失败，用户手动重试。

取消仅针对 queued/running；运行取消等待子进程退出和临时文件清理。进入 delivering 时由 store 原子检查是否已有取消请求；交付提交阶段不再接受取消，避免已写入书库却显示取消。

生成失败保留输入。入库失败还保留已生成内部 CBZ、filename 等信息，重试先交付有效缓存，不重复抓取。成功后删除应用产物与上传源，Komga 文件保留。已有同名同内容目标可作为幂等交付；不同内容不覆盖。

## 路径与设置

内部 work/artifacts 按任务隔离，CBZ 和 Komga 目标都用临时文件原子发布；拒绝目录越界、symlink 和不安全成员。

设置包含 komga_dir（现有、可写、应用内可见的绝对共享目录）和可选 komga_url（HTTP/HTTPS 网站链接）。保存设置覆盖初始 KOMGA_DIR，未配置目录时提交返回 409。URL 本身不提供文件传输能力。

系列名清理后作为书库子目录，无系列为 tankobon。API 不返回源路径或可下载产物地址；旧 download/手动 komga 动作不存在。历史去重确认、元数据历史、运行参数设置与分布式代际规则不属于新版本。
