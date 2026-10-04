# Telegram 元数据预填设计草案

状态：调研和真实样例 PoC 已通过，尚未接入运行代码。已验证 tdl 独立登录、3870 介绍帖 → 3873 同名 ZIP → 3874 同名 Telegraph 链接、四字段提取和现有 Go ComicInfo 输出。规则仍需移植及完整 UI/API 验收。

## 1. 范围和边界

现有 Go API / worker / PostgreSQL Task Core 保持不变。预览不创建任务、不写 metadata_history、不下载媒体。最终提交继续复用 URL 与上传入口、metadata JSONB 和 ComicInfo 生成。

Telegram 消息链接是元数据来源，不复用仅支持 Telegraph 的 `url` 字段。首页元数据区域新增可选来源输入，URL/上传两模式均可使用；不改变 archive file、input mode、force 或原下载地址。

## 2. 读取与进程契约

- tdl v0.20.4 以专用账号 namespace 和仓库外的私密 storage 运行；首次明确 `login -T qr`，不使用默认 Desktop 会话导入。
- 认证由管理员在本机完成；网页预览接口不接收手机验证码、2FA、session 或应用 hash。
- 先精确识别 Telegram 主机、频道及正整数消息 ID，再通过固定可执行文件和参数数组调用 CLI。不得使用 shell 拼接用户 URL、由用户指定输出路径或任意主机抓取。
- 同一 namespace 串行读取，限定排队/执行时间；页面取消或服务关闭应终止子进程并清理未完成输出。每次独立临时文件，权限 0600，不复用上次结果。
- tdl 的 ID 范围限制输出候选，底层 history 分页仍为 100。网络读取条数若需严格限制，改用 gotd 精确读取，不能宣称 CLI 只向 Telegram 请求 11 条。
- 完成判定同时要求进程成功、JSON 完整、频道身份和目标消息匹配。空数组、仅有邻近消息或部分文件不能证明目标可读。
- tdl 登录遇到 context.Canceled 时可能退出 0；首次授权必须以真实账号 API 或目标读取验证，不把登录退出码/本地密钥文件存在当成成功。
- Telegram API 错误及 FLOOD_WAIT 从受控错误中分类；HTTP 不返回原始日志、账号信息或 JSON raw。

## 3. 消息关联与解析

1. 保留目标正文/caption、文件名、显式链接、reply_to、grouped_id、entities；读取有界候选窗口。
2. 优先明确指向目标的链接或回复。grouped_id 只表示相册，不等于另一条 ZIP 和介绍必然属于同一作品。
3. 其次比较介绍标题与附件文件名；不得仅按最近一条/前一条自动认定。多个作品匹配时返回候选选择。
4. 以真实样本中的标签格式建立可解释规则，不在未读到样本前宣称支持该频道。缺失字段省略，不猜系列名、序号或作者。
5. 翻译组/社团/发布者不自动作为作者；`tags` 与 `genres` 不互相复制；原名等暂无对应字段时不强行塞入其他字段。
6. entities 用 UTF-16 code units；Go 字节下标不可直接切片。原始 64 位标识符不能经 JavaScript number 无损假定。
7. 每字段最多 4000 字节且保持合法 UTF-8，截断给出 `field_truncated`。再复用 `tasks.NormalizeMetadata`，使预览值与最终字段一致。

## 4. API 草案

`POST /api/metadata/preview`

请求：

```json
{"source_url":"https://t.me/MLSHHZ/3870"}
```

成功响应示例中的字段值均为合成示例，不来自该频道：

```json
{
  "ok": true,
  "outcome": "ready",
  "source": {"url":"https://t.me/MLSHHZ/3870","message_id":3870},
  "candidates": [{
    "id":"3870:3869",
    "message_ids":[3869,3870],
    "metadata":{"comic_name":"示例作品","author":"示例作者","tags":"冒险"},
    "evidence":{
      "comic_name":{"message_ids":[3869],"rule":"explicit_title"},
      "author":{"message_ids":[3869],"rule":"explicit_author"}
    },
    "warnings":[]
  }]
}
```

- `outcome`: `ready / needs_selection / empty`。无可靠字段与有多个候选均可 HTTP 200，但不自动填写歧义结果。
- metadata 白名单为现有七字段：author、series_name、series_number、comic_name、summary、tags、genres，均为字符串。未知字段拒绝进入表单。
- evidence 保存消息 ID 和解析规则；不伪造百分比置信度。
- 错误复用现有 `{error,code,message,details}` 格式。

| HTTP | code | 行为 |
| --- | --- | --- |
| 400 | validation_error | 非支持链接/非法 ID/输入超长 |
| 503 | telegram_not_configured / telegram_auth_required | 提示管理员连接账号 |
| 403 | telegram_access_denied | 明确的访问拒绝，不提示“未找到” |
| 404 | telegram_message_not_found | 有充分证据证明目标缺失 |
| 429 | telegram_rate_limited | 返回 Retry-After；前端不紧密重试 |
| 504 | telegram_timeout | 恢复表单操作，可手动重试 |
| 502 | telegram_upstream_error | 未分类上游错误，不带原始日志 |

## 5. 前端合并规则

- 来源变化后取消旧请求并递增 generation；响应必须匹配 mount、generation 和当前 URL。
- 每字段跟踪用户编辑版本与上次自动填写值。自动填写仅覆盖空且未编辑，或仍属于当前自动结果且未被修改的字段。
- 来源 A 换 B 时，只清理 A 自动填写且未被用户修改的值；用户主动清空也属于编辑，不再次自动填回。
- 历史回填是明确用户操作，应取消预览并清除自动填写归属。不能复用会覆盖所有字段和切换任务来源的 `applyHistoryEntryToForm`。
- 候选选择后仍遵守字段保护。响应用 value/textContent 渲染。
- 识别期间仍可编辑；提交按钮合并 `submitting || previewBusy`。识别结束不能把正在上传的按钮解锁，失败/超时也不得永久锁住表单。

## 6. 现有代码落点

| 环节 | 文件/函数 |
| --- | --- |
| 七字段表单 | `web/templates/index.html` 元数据区域 |
| 表单提交字段 | `frontend/src/home/state.js: collectMetadataPayload` |
| URL / 上传提交 | `frontend/src/home/index.js: submitForm / submitUpload` |
| 生命周期 | `home/index.js: mount / unmount`，拟独立 `home/metadata_preview.js` |
| HTTP 注册与注入 | `internal/httpapi/api.go: RouterOptions / NewRouterWithOptions` |
| 既有清洗 | `internal/app/tasks/metadata.go: NormalizeMetadata` |
| JSONB 保存 | `taskcore_handlers.go: taskCoreMetadataMap` → PostgreSQL inputs.metadata |
| ComicInfo 映射 | `internal/worker/taskcore/downloader.go: metadataFromInput` |
| XML 生成 | `internal/downloader/cbz_writer.go: WriteComicInfoXML` |

读取适配在基础设施层；消息关联/提取规则在 `internal/domain/metadata/`；应用层编排预览用例；HTTP 仅适配请求和结果。只有这一条已验证来源路径，不提前引入插件框架。

## 7. 实现前的验证门槛

- 登录及目标消息可读，检查实际字段格式和关联证据；没有样本时只完成接入设计，不宣称解析准确。
- 合成及脱敏样本覆盖：明确关联、邻近的另一部作品、多个候选、空正文、文件 caption、emoji/UTF-16、标签去重、超长中文和 XML 特殊字符。
- 预览无任务/历史写入；两个提交入口的同一字段生成相同 ComicInfo，手工修改以提交时值为准。
- 倒序响应、编辑/主动清空、来源切换、历史回填、卸载重挂、提交启动后不会被旧请求覆盖。
- 登录失效、访问拒绝、缺失、限流、超时、部分 JSON 都有明确行为。
- 当前 64 MiB 上传上限保持不变。样例附件若超过上限，需另行设计容量/下载链路；元数据识别成功不等于附件处理完成。
