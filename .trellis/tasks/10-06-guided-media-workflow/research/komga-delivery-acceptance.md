# 隔离真实 Komga 交付验收

日期：2026-10-07。执行命令：

```sh
bash scripts/check_komga_delivery.sh
```

结果：`TestDeliveryRealKomga1281` PASS，测试用时 8.17 秒。

使用本机缓存的 `gotson/komga:1.28.1`（arm64），image ID：
`sha256:6708c672d7669a7ed300edd95dbfcc75d428fcb23be6cc99e7b35914550f99f4`。
测试创建独立容器、随机 `127.0.0.1` 端口、临时配置和书库，以及合成管理员/API Key；没有读取生产凭据、访问生产书库或部署应用。

## 实际验证

1. 生成包含 1 张 PNG 和 ComicInfo 的合成 CBZ，直接调用项目的 `DeliveryService`；经真实 HTTP client 触发 Komga 扫描并读取实际 book ID、library ID 和元数据，最终 `komga_indexed=verified`。
2. ComicInfo 包含中文标题、编号、摘要、作者、译者、标签、日期和页数。服务的投影校验通过；验收另读取实际 Komga book，核对标题、摘要、2 个作者角色条目及 1 页。
3. 新建 `DeliveryService` 实例重试同一文件，保持同 book ID、文件 inode、mtime 和 SHA-256。
4. 同名但不同 ComicInfo 内容返回 `ErrKomgaTargetConflict`，原目标 SHA-256 不变。
5. 在隔离 Komga 将实际已收录书的摘要改为过期值并锁定。即便书的 media 状态为 `READY`，重新交付也只返回 `pending`，不伪报收录验证成功，不重写 CBZ。
6. 将隔离书的摘要修复为与 ComicInfo 一致并解除锁定，再次交付恢复 `verified`，仍是同 book ID 和文件 inode。
7. 最终实际书库恰好 1 本书；重试及冲突未造成重复收录。

测试后按 `media-workspace-test=komga-delivery` 标签查询，没有遗留测试容器；临时目录由 Go `t.TempDir` 清理。测试为显式 opt-in，普通 `go test ./...` 不会启动 Docker。脚本要求事先存在 Komga 镜像，避免测试期间隐式拉取镜像。

## 浏览器、Task Core、worker 与 Komga 完整串联

随后扩展隔离 E2E runner，执行：

```sh
bash tests/e2e/run-e2e.sh
```

结果：Chromium **32 passed (36.3s)**，退出码 0。真实整链测试用时 4.5 秒。
完整日志位于私有 `/tmp/guided-e2e-complete.log`，权限 `0600`。

- runner 先启动相同版本的独立 Komga 容器，通过合成管理员创建 API Key 和书库；共享临时 `komga` 目录给 Go API 与 Komga，动态配置 `KOMGA_LIBRARY_MAPPINGS`。
- `tests/e2e/bootstrap_komga.py` 经工作台真实管理员登录和 settings HTTP 配置连接，实际连接测试返回 `connected` 和 1 个映射书库。Key 仅在临时私有文件中保存，配置成功后删除；不进入 Playwright 进程或 trace。
- 浏览器上传合成 ZIP，在集中核对页选择 Komga，确认前验证零任务创建；用户一次“确认并开始”后，故意丢弃真实 upload-init 响应，通过提交回执恢复同一任务。
- PostgreSQL Task Core 和实际 worker 完成处理；浏览器当前任务模块自动复制、扫描和读回真实 Komga，显示“Komga 已收录”，响应包含实际 book ID 和 library ID。
- 通过工作台目录 API 再次读取 Komga 的中文标题、编号、已清空简介和作者；下载实际 worker CBZ，逐项核对图片顺序及字节、ComicInfo 中人工设置/继承/清空字段。
- 直接检查共享临时书库：恰好 1 个 CBZ，完整字节与 worker 下载产物相同。重试 copy 返回同一 book ID，目标 inode、mtime、SHA-256 全部不变；Komga 目录仅 1 本书，任务列表仅增加 1 个任务。

本次项目 `telegraph_e2e_4150_2092` 在退出时已删除全部测试容器、PG volume、私网和本次应用镜像；退出后按项目标签查询容器及 volume 均为空，临时目录与合成凭据已清理。

## 验收范围

独立服务测试覆盖真实 Komga pending/verified 区分、同名冲突与重试；完整 E2E 覆盖浏览器一次确认到实际 worker 归档及真实 Komga 收录。Komga、Task Core、worker 和最终文件均为真实运行；upload-init 响应丢失通过浏览器网络拦截注入。没有访问生产作品或发布生产版本。
