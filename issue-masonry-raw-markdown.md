# Issue: Masonry card 顯示原始 markdown inline 語法

## 問題描述

Masonry layout 的 card 上看得到 `**text**`（raw markdown），其他兩種 layout 因顯示內容較少而不明顯，但同樣有此 bug。

## 根本原因

`feed.go` 的 `analyzeCardBody` 在處理兩種 variant 時，只呼叫 `markdown.StripMDLinks`，未去除 `**`、`*`、`_`、`` ` `` 等 inline 標記：

1. **list variant**（`feed.go:330`）：
   ```go
   bullets = append(bullets, markdown.StripMDLinks(item))
   ```

2. **quote variant**（`feed.go:280`）：
   ```go
   q := strings.TrimSpace(markdown.StripMDLinks(qb.String()))
   ```

相比之下，`text` variant 使用 `markdown.Excerpt()`，內部有完整的 inline 語法剝除（`**`, `__`, `*`, `_`, `` ` ``）。

Masonry 顯示完整 `<ul>` 所有 items，所以問題最明顯；grid/list 只顯示前 1–2 個 bullet，較容易僥倖沒看到。

## 修復方向

在 `internal/markdown/render.go` 新增 `StripInline(s string) string`，僅負責去除 inline 標記（複用 `Excerpt` 內部的 `strings.NewReplacer`）：

```go
var inlineReplacer = strings.NewReplacer(
    "**", "", "__", "", "*", "", "_", "", "`", "", "\r", "",
)

func StripInline(s string) string {
    return inlineReplacer.Replace(s)
}
```

然後在 `analyzeCardBody` 串接呼叫：

```go
// list variant
bullets = append(bullets, markdown.StripInline(markdown.StripMDLinks(item)))

// quote variant
q := strings.TrimSpace(markdown.StripInline(markdown.StripMDLinks(qb.String())))
```

## 驗收標準

- 含 `**bold**`、`*italic*`、`_text_`、`` `code` `` 的 bullet list note，在三種 layout 下均不顯示原始標記
- quote variant 同上
- text variant 行為不變
