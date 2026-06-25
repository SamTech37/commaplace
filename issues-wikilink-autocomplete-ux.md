# Issue：Wiki 自動完成 UX 修正規格

## 問題 1：輸入 `[[` 後不立即顯示推薦

**狀態：✅ 已修復（2026-06-25）**

### 現狀（修復前）

後端 `GetWikiSuggest`（`internal/handlers/wiki.go:23`）在 `q == ""` 時直接 `return`，不返回任何候選項。使用者輸入 `[[` 後必須再輸入至少一個字元才會看到推薦列表。

### 修復記錄

**`internal/handlers/wiki.go`**：刪除 `if q == "" { return }` 三行。空查詢現在走 `suggestNotes`，`pattern = "%%"` 匹配所有筆記，依 `updated_at DESC` 回傳最近 10 篇。副作用：工具列 `[[ ]]` 按鈕點擊後也立即顯示推薦（先前因同樣原因無效）。

---

## 問題 2：推薦列表出現在 textarea 底部而非游標下方

**狀態：✅ 已修復（2026-06-25）**

### 現狀（修復前）

`.ac-popup` 帶 `margin-top: 0.4em`，在 `position: fixed` 定位下產生多餘偏移；且 popup 超出視窗右側或底部時沒有夾算，會跑出螢幕。

> 注意：原始規格描述的是純 textarea + mirror div 方案。實際上編輯器使用 EasyMDE（CodeMirror 5），`cmeditor.js` 已有 `positionPopup()` 使用 `cm.cursorCoords(true, "window")`，游標追蹤本身已正確；問題只在 CSS 多餘 margin 和缺少 viewport 夾算。

### 修復記錄

**`internal/handlers/static/style.css`**：`.ac-popup` 移除 `margin-top: 0.4em`，改為 `position: fixed; z-index: 200; min-width: 200px; max-width: min(320px, 90vw)`。

**`internal/handlers/static/cmeditor.js`**：`positionPopup()` 移除 inline `popup.style.position = "fixed"`（已移至 CSS），加入右側與底部 viewport 夾算：右側超出靠右貼齊，底部超出則顯示於游標上方。

---

## 問題 3：關聯圖節點超出容器邊界

**狀態：✅ 已修復（2026-06-25，方式與原規格不同）**

### 現狀（修復前）

Graph view 的節點受 force simulation 驅動，沒有邊界約束，座標可超出 canvas 可視範圍，超出部分被截掉且無法點擊。

### 修復記錄

原規格建議對節點座標做 `Math.max`/`Math.min` 夾算（硬邊界）。實際採用更完整的方案：

**`internal/handlers/static/graph.js`**：

- 加入 viewport transform（`panX, panY, zoom`），所有 draw call 包在 `ctx.save() / translate / scale / restore()`，節點座標在 world space 運算，不再受 canvas 大小限制。
- 新增 `screenToWorld()` 做滑鼠座標反轉，`nodeAt()` 改收 world coords。
- 拖空白處平移（`PAN_FRICTION = 0.80`，高摩擦力）；滾輪縮放（`MIN_ZOOM=0.10, MAX_ZOOM=4.0`），以游標為中心縮放。
- 右上角 `fit` 按鈕（screen space overlay）：計算所有節點 bounding box，自動設 zoom/pan 讓全圖顯示於畫面內；模擬首次收斂後也自動觸發一次。

**`internal/handlers/templates/graph.html`**：說明文字更新，加入平移／縮放操作說明。
