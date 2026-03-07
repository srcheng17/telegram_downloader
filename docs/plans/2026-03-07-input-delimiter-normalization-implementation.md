# 元数据分隔符标准化 Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 让作者/标签/类型字段在提交 `/download` 时按新规则稳定标准化（作者仅逗号；标签/类型支持逗号+空白+`#`），并保持前端界面结构不变。

**Architecture:** 在 `go-backend/internal/httpapi/api.go` 集中实现标准化函数，并在 `extractFromMap` 统一调用；模板层仅更新说明文案。所有行为通过后端单元测试和 UI 模板渲染断言回归。

**Tech Stack:** Go (net/http + testing), Go HTML templates (`internal/httpui/templates`), 现有 `go test` 测试体系。

---

### Task 1: 先写失败测试，锁定新解析行为（TDD Red）

**Files:**
- Modify: `go-backend/internal/httpapi/api_test.go`
- Test: `go-backend/internal/httpapi/api_test.go`

**Step 1: 新增“作者仅逗号拆分”的失败测试**

在 `api_test.go` 新增测试（推荐命名）：`TestDownloadNormalizesAuthorByCommaOnly`。

测试输入（form）：
- `author=  张三  李四，王五,张三  李四  `（注意中间空格不应作为作者分隔）

断言：
- `repo.claimCalls[0].Task.Author == "张三  李四,王五"`

示例断言片段：
```go
if got := stringValue(repo.claimCalls[0].Task.Author); got != "张三  李四,王五" {
    t.Fatalf("expected normalized author %q, got %q", "张三  李四,王五", got)
}
```

**Step 2: 新增“标签/类型按逗号+空白+#拆分”的失败测试**

新增测试（推荐命名）：`TestDownloadNormalizesTagsAndGenresByCommaSpaceAndHash`。

测试输入（form）：
- `tags= #科幻  冒险，连载#热血   科幻`
- `genres= 悬疑   #青年，热血 悬疑`

断言：
- `TagsNormalized == "科幻,冒险,连载,热血"`
- `GenresNormalized == "悬疑,青年,热血"`

示例断言片段：
```go
if got := stringValue(repo.claimCalls[0].Task.TagsNormalized); got != "科幻,冒险,连载,热血" {
    t.Fatalf("unexpected tags_normalized: %q", got)
}
if got := stringValue(repo.claimCalls[0].Task.GenresNormalized); got != "悬疑,青年,热血" {
    t.Fatalf("unexpected genres_normalized: %q", got)
}
```

**Step 3: 运行测试确认失败（Red）**

Run:
```bash
go test ./internal/httpapi -run 'TestDownloadNormalizesAuthorByCommaOnly|TestDownloadNormalizesTagsAndGenresByCommaSpaceAndHash' -count=1
```

Expected:
- FAIL（当前实现还只处理中文逗号替换，不支持空白/#规则）

**Step 4: 提交测试（可选，若团队允许红测提交）**

```bash
git add go-backend/internal/httpapi/api_test.go
git commit -m "test(httpapi): add delimiter normalization red tests"
```

---

### Task 2: 实现后端标准化并转绿（TDD Green）

**Files:**
- Modify: `go-backend/internal/httpapi/api.go`
- Modify: `go-backend/internal/httpapi/api_test.go`（仅在编译修正时）

**Step 1: 在 `api.go` 增加标准化函数**

新增 3 个函数（建议放在 `extractFromMap` 附近）：
- `normalizeAuthorList(raw string) string`
- `normalizeTagLikeList(raw string) string`
- `normalizeDelimitedList(raw string, splitFn func(rune) bool) string`

实现要点：
```go
func normalizeAuthorList(raw string) string {
    return normalizeDelimitedList(raw, func(r rune) bool {
        return r == ',' || r == '，'
    })
}

func normalizeTagLikeList(raw string) string {
    return normalizeDelimitedList(raw, func(r rune) bool {
        return r == ',' || r == '，' || r == '#' || unicode.IsSpace(r)
    })
}

func normalizeDelimitedList(raw string, splitFn func(rune) bool) string {
    parts := strings.FieldsFunc(raw, splitFn)
    seen := make(map[string]struct{}, len(parts))
    out := make([]string, 0, len(parts))
    for _, p := range parts {
        item := strings.TrimSpace(p)
        if item == "" {
            continue
        }
        if _, exists := seen[item]; exists {
            continue
        }
        seen[item] = struct{}{}
        out = append(out, item)
    }
    return strings.Join(out, ",")
}
```

> 需要在 import 中增加 `unicode`。

**Step 2: 在 `extractFromMap` 中接入新规则**

将原逻辑：
```go
author: optionalString(normalizePayloadText(payload["author"], maxMetadataFieldLength)),
tagsNormalized: optionalString(strings.ReplaceAll(tagsRaw, "，", ",")),
genresNormalized: optionalString(strings.ReplaceAll(genresRaw, "，", ",")),
```

替换为：
```go
rawAuthor := normalizePayloadText(payload["author"], maxMetadataFieldLength)
...
author: optionalString(normalizeAuthorList(rawAuthor)),
tagsNormalized: optionalString(normalizeTagLikeList(tagsRaw)),
genresNormalized: optionalString(normalizeTagLikeList(genresRaw)),
```

**Step 3: 运行新测试确认通过（Green）**

Run:
```bash
go test ./internal/httpapi -run 'TestDownloadNormalizesAuthorByCommaOnly|TestDownloadNormalizesTagsAndGenresByCommaSpaceAndHash' -count=1
```

Expected:
- PASS

**Step 4: 运行模块回归**

Run:
```bash
go test ./internal/httpapi -count=1
```

Expected:
- PASS（无回归）

**Step 5: 提交后端实现**

```bash
git add go-backend/internal/httpapi/api.go go-backend/internal/httpapi/api_test.go
git commit -m "feat(httpapi): normalize author/tags/genres delimiters"
```

---

### Task 3: 更新首页帮助文案并补渲染断言

**Files:**
- Modify: `go-backend/internal/httpui/templates/index.html`
- Modify: `go-backend/internal/httpui/handler_test.go`
- Test: `go-backend/internal/httpui/handler_test.go`

**Step 1: 先写失败断言（Red）**

在 `TestIndexPageUsesDynamicGuardrailsAndHTMXLinks` 或新增专用测试中添加文案断言：
```go
assertContains(t, body, "作者支持英文逗号 (,) 与中文逗号（，）分隔")
assertContains(t, body, "标签支持英文逗号 (,) / 中文逗号（，）/ 空格 / # 分隔")
assertContains(t, body, "类型支持英文逗号 (,) / 中文逗号（，）/ 空格 / # 分隔")
```

**Step 2: 运行测试确认失败（Red）**

Run:
```bash
go test ./internal/httpui -run TestIndexPageUsesDynamicGuardrailsAndHTMXLinks -count=1
```

Expected:
- FAIL（模板尚未更新）

**Step 3: 修改模板文案（保持 UI 结构不变）**

在 `go-backend/internal/httpui/templates/index.html`：
- 作者提示改为仅逗号分隔；
- 标签/类型提示改为逗号 + 空格 + `#` 分隔，并说明会清理空格。

**Step 4: 运行测试确认通过（Green）**

Run:
```bash
go test ./internal/httpui -run TestIndexPageUsesDynamicGuardrailsAndHTMXLinks -count=1
```

Expected:
- PASS

**Step 5: 提交文案更新**

```bash
git add go-backend/internal/httpui/templates/index.html go-backend/internal/httpui/handler_test.go
git commit -m "chore(httpui): update metadata delimiter help text"
```

---

### Task 4: 全量验证与收尾

**Files:**
- Modify: none (verification only)

**Step 1: 运行后端全量测试**

Run:
```bash
go test ./... -count=1
```

Expected:
- PASS

**Step 2: 运行竞争检测（可选但推荐）**

Run:
```bash
go test -race ./... -count=1
```

Expected:
- PASS

**Step 3: 若需页面链路确认，执行 e2e**

Run:
```bash
npm run e2e:test
```

Expected:
- PASS（现有首页提交流程不受影响）

**Step 4: 最终提交（若前面未分批提交）**

```bash
git add -A
git commit -m "feat: normalize metadata delimiters for download form"
```

