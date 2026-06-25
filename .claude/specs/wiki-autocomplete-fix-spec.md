# Wiki Autocomplete Fix Spec

## Fix A: 輸入 `[[` 後立即顯示推薦

### 問題

`internal/handlers/wiki.go:25-27` 的早期返回：

```go
if q == "" {
    return
}
```

導致空查詢直接 return，`fetchSuggest("")` 拿到空 response，popup 永遠不出現。
也影響工具列的 `[[ ]]` 按鈕——點按後游標移入 `[[]]`，觸發 `fetchSuggest("")`，但同樣什麼都不顯示。

### 修改

**`internal/handlers/wiki.go`**

刪除 25–27 行那個 early return，讓空 query 繼續走 `suggestNotes`。

`suggestNotes` 裡 `pattern = "%" + "" + "%" = "%%"`（即 `LIKE '%%'`），等同於不過濾，自動列出最近筆記。
`suggestNotesForUser` 的 `q == ""` 分支（line 82）已有獨立邏輯，不受影響。

### 驗收

- 輸入 `[[` 即出現推薦（不需再輸入任何字元）
- 點工具列 `[[ ]]` 按鈕後即出現推薦
- `@` 前綴路由（用戶搜尋）不受影響
- `@bob/` 前綴路由（特定用戶的筆記）不受影響

---

## Fix B: Popup 錨定在游標下方

### 問題

`cmeditor.js` 已有 `positionPopup()` 使用 `cm.cursorCoords(true, "window")` 做 `position: fixed` 定位，**游標追蹤本身是正確的**。

剩餘問題：
1. CSS `.ac-popup { margin-top: 0.4em }` 在 `position: fixed` 下仍然生效，在 JS 設定的 `top` 之外再加一段空白，導致 popup 比游標低一行字距。
2. Popup 靠右或靠下超出 viewport 時沒有夾算，會跑出螢幕。
3. `position: fixed` 只在 JS 裡用 inline style 設定，CSS 裡缺少宣告（視覺上 OK，但語意不明）。

### 修改

**`internal/handlers/static/style.css`**

`.ac-popup` 改為：
```css
.ac-popup {
  position: fixed;        /* 由 JS positionPopup() 控制 top/left */
  z-index: 200;
  min-width: 200px;
  max-width: min(320px, 90vw);
  /* 移除 margin-top: 0.4em */
  background: var(--bg);
  border: 1px solid var(--border);
  border-radius: var(--r-card);
  box-shadow: 0 8px 30px rgba(0, 0, 0, 0.12);
}
```

**`internal/handlers/static/cmeditor.js`** — `positionPopup()`

加入 viewport 夾算，防止 popup 跑出畫面：

```js
function positionPopup() {
  var coords = cm.cursorCoords(true, "window");
  var pw = popup.offsetWidth  || 260;
  var ph = popup.offsetHeight || 120;

  var top  = coords.bottom + 4;
  var left = coords.left;

  // 右側超出：靠右貼螢幕
  if (left + pw > window.innerWidth - 8)
    left = Math.max(8, window.innerWidth - pw - 8);

  // 下方超出：顯示在游標上方
  if (top + ph > window.innerHeight - 8)
    top = coords.top - ph - 4;

  popup.style.top  = top  + "px";
  popup.style.left = left + "px";
}
```

同時移除 `positionPopup()` 裡的 `popup.style.position = "fixed"` 這行（已移至 CSS）。

### 驗收

- 在多行文件的中間某行輸入 `[[`，popup 緊貼游標下方（不在 textarea 最底部）
- 在編輯器右側邊緣輸入 `[[`，popup 不超出螢幕右側
- 在編輯器最後一行輸入 `[[`，popup 顯示在游標上方

---

## 改動清單

| 檔案 | 改動 |
|---|---|
| `internal/handlers/wiki.go` | 刪除 `if q == "" { return }` (lines 25–27) |
| `internal/handlers/static/style.css` | `.ac-popup`: 移除 `margin-top`, 加 `position:fixed; z-index:200; min-width/max-width` |
| `internal/handlers/static/cmeditor.js` | `positionPopup()`: 移除 inline `position`, 加 viewport 夾算 |

無後端 schema 或路由變更。
