# 当前项目与 Komga 元数据编辑的差距

只读源码核对，2026-10-05；此文是规划依据，不代表已经实现存量编辑。

## 已有能力

- `internal/app/tasks/komga_copy.go` 将本应用成功产物复制到 `KOMGA_LIBRARY_ROOT`；`internal/domain/komga/path.go` 决定系列/文件名。`internal/httpapi/taskcore_handlers.go` 的 `copy-to-komga` 动作只服务已有成功任务。当前 `/api/tasks` 列出 Task Core 任务，不列 Komga 馆藏。
- `cmd/server/main.go` 注入根目录，`docker-compose.yml` 和 `docker-compose.image.yml` 将宿主 Komga 目录以可写方式挂给 Go API。项目没有 Komga HTTP API 客户端、URL/凭据设置或书籍 ID 映射。
- `internal/domain/metadata/` 的版本化 Document、字段定义及 set/clear 可作为编辑数据模型；`frontend/src/shared/metadata/editor.js` 可作为 UI 基础。`POST /api/metadata/patch` 只转换草稿，不持久化文件或数据库。
- `internal/comicinfo/` 可安全解析和合并 ComicInfo；`internal/archive/metadata_bundle.go` 与 `internal/downloader/metadata_pack.go` 服务新归档制作。现有包装会规范化图片名和顺序，并把未知 XML/竞争元数据放进私密副本，因此不能直接拿来“就地修改”用户已有 CBZ。现有发布函数还拒绝覆盖目标文件。

## 新能力所需边界

1. 书籍/文件列表需要独立的稳定标识、分页与详情来源；任务 ID 不等于 Komga 书籍 ID，也不等于安全的文件路径。
2. 编辑旧文件前须验证配置根目录、symlink/路径穿越、文件类型和当前版本；外部变化后拒绝覆盖，不能复用已有复制动作的覆盖语义。
3. 若选择写 ComicInfo，需新增只更改目标元数据、保留所有非目标 ZIP entry 内容和顺序的明确契约，并有受保护原件备份、临时文件、原子替换、回读及回滚。任何无法安全保留的格式应只读或拒绝，而非假装成功。
4. 若选择 Komga API 编辑，需官方接口、认证/权限和锁定字段语义的版本证据，并证明重新扫描/迁移后数据的预期去向。
5. 编辑入口复用单管理员会话与 CSRF；前端不能读回 Komga 凭据或暴露宿主绝对路径。实际 Komga 的重新扫描与读回是外部验收，不能用本应用的回读代替。
