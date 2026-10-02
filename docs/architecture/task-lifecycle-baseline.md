# Task Core 任务生命周期

当前状态以 `internal/domain/taskcore/status.go` 为准：`CREATED`、`READY`、`RUNNING`、`CANCELING`、`SUCCEEDED`、`FAILED`、`CANCELED`。页面消费 backend `status_label`、`progress` 和 `available_actions`。

## URL 与上传

```text
URL: POST /download -> READY -> RUNNING -> SUCCEEDED / FAILED
上传: init -> CREATED -> upload-source -> READY -> RUNNING -> SUCCEEDED / FAILED
活跃状态 -> CANCELING -> CANCELED
FAILED / CANCELED -> retry -> READY
```

同 URL 活跃任务复用；成功产物可用时返回确认态，force 新建但仍复用活跃任务。每个新任务保存创建时的下载设置快照，重试沿用原设置。

上传字节进度由客户端 XHR 展示；落盘完成后后端进度进入 preparing。URL 图片发现和完成回调写入 downloading current/total，打包进入 packaging，成功进入 done。不得用时间推算伪进度。

## 取消与重试

取消接口返回 `CANCELING`，不代表执行已停止。运行中的 worker 取消 context，等 Execute 返回并清理未发布产物，再写 `CANCELED`；尚未领取任务由 recovery 确认取消。

attempt 是自动恢复重试预算，手动 retry 可清零；generation 每次 claim 单调递增，不能清零。所有执行写回必须匹配 lease owner 和 generation；终态另检查 attempt。

上传失败或取消保留源包；成功且 Complete 提交后清理源包。未附着源包的取消任务不能执行重试，用户重新选择文件上传。

## 下载和 Komga

成功任务经 `HEAD /api/tasks/{id}/download` 预检，再 GET 下载。路径需在 DOWNLOAD_PATH 真实边界内；物理目录为 task ID/generation，下载文件名保留元数据规则。

`POST /api/tasks/{id}/copy-to-komga` 写入配置 root 的系列目录，无系列则 `tankobon`。前端同时检查 HTTP 和 payload.ok 才展示成功。失败保留在当前页面。
