# Issue：Wiki 自動完成 UX 修正規格

## 問題 1：輸入 `[[` 後不立即顯示推薦

### 現狀

後端 `GetWikiSuggest`（`internal/handlers/wiki.go:23`）在 `q == ""` 時直接 `return`，不返回任何候選項。使用者輸入 `[[` 後必須再輸入至少一個字元才會看到推薦列表。

### 期望行為

輸入 `[[` 後（查詢字串為空字串 `""`），應立即顯示推薦列表，不需再輸入任何字元。

### 修改規格

**`internal/handlers/wiki.go`**

移除 `q == ""` 的提前返回：

```go
// 現有（刪除）
if q == "" {
    return
}

// 改為讓空查詢繼續走 suggestNotes，pattern 自然為 "%%"
```

`suggestNotes` 中 `pattern` 的生成邏輯不需額外修改：`"%" + "" + "%" = "%"`，即 `LIKE '%'`，匹配所有筆記。

查詢優先順序（同現有，q 為空時自動列出最近筆記）：
1. 自己的筆記，依 `updated_at DESC`，最多 10 筆
2. 追蹤對象的筆記，補到 10 筆
3. 全域筆記（未登入亦可），去重後取到 10 筆

---

## 問題 2：推薦列表出現在 textarea 底部而非游標下方

### 現狀

`.ac-popup` 為文檔流中的元素（`position: static`），位於 `<textarea>` 之後，加上 `margin-top: 0.4em`。無論游標在哪一行，popup 永遠出現在整個 textarea 正下方。textarea 較長時，popup 與游標距離甚遠。

### 期望行為

popup 應錨定於 `[[` 觸發字元的視覺位置正下方（游標下一行），隨游標位置動態出現。

### 修改規格

**`internal/handlers/static/style.css`**

```css
.ac-popup {
  position: absolute;
  z-index: 200;
  /* 移除 margin-top: 0.4em */
  /* 其餘樣式不變 */
}
```

`<textarea>` 的外層容器（`<label>` 或新增的包裝 div）需有 `position: relative`。

**`internal/handlers/static/editor.js`**

新增 `getCaretPixelPos(textarea, index)` 函式，回傳游標相對於 textarea 左上角的像素座標，採 mirror div 技術：

```
1. 建立不可見 div（mirror），複製 textarea 的 font、padding、border、
   line-height、white-space: pre-wrap、word-wrap: break-word 等 computed style
2. 將 textarea.value.slice(0, index) 填入 mirror，末尾附加 <span id="caret">
3. 短暫插入 DOM（visibility: hidden、position: absolute）
4. 讀取 span.offsetTop / offsetLeft
5. 從 DOM 移除 mirror
6. 回傳 { top: spanTop - textarea.scrollTop, left: spanLeft }
```

在 `fetchAndRender` 成功後顯示 popup 前執行定位：

```js
const caretPos = getCaretPixelPos(ta, queryStart - 2); // [[ 起始位置
const lineH = parseFloat(getComputedStyle(ta).lineHeight);
popup.style.top  = (caretPos.top + lineH) + 'px';
popup.style.left = caretPos.left + 'px';
```

邊界條件：
- popup 寬度：`min(320px, 90vw)`
- 若右側超出視窗：靠右對齊（`left = window.innerWidth - popupWidth - 8px`）
- 若下方超出視窗：顯示於游標上方（`top = caretPos.top - popupHeight`）
