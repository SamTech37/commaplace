> **Status: DONE.** Shipped 2026-07-05: `opencc.min.js` (vendored, lazy-loaded)
> + `opencc-toggle.js` in `internal/handlers/static/`, `#script-toggle` button
> in `_base.html`. One bug fixed post-implementation: `OpenCC.HTMLConverter`
> only walks subtrees whose root already carries `lang === fromLang`, so the
> root passed to it needs `root.lang = from` set explicitly first — without
> it, `.convert()` runs but silently touches nothing. Verified end-to-end in
> Chrome: 原→简→繁→原 cycle, editor-textarea exclusion, lazy-load (network
> panel confirms `opencc.min.js` only fetches on first real toggle), retry
> after a simulated load failure, and a rapid-double-click race (fixed by
> committing mode/button/localStorage synchronously at click time instead of
> inside the async load callback). Backend 繁簡 search (opencc s2t/t2s in
> `search.go`) shipped separately earlier and is unaffected/unrelated.

# 前端繁簡顯示切換（像深淺色切換一樣的 topbar 按鈕）

## Context（背景）

搜尋已經繁簡互通（上一輪完成），但讀者看到的內容還是作者寫的原字體。要在前端加一個像深淺色切換的按鈕，把**顯示**的文字即時繁簡轉換。已和使用者確認：**三態循環（原文 → 简 → 繁）**、偏好**只存 localStorage**（不加 DB 欄位——伺服器反正無法預先轉換，存 DB 只多跨裝置同步，之後要加很容易）。

基準分支：`main`（d067aba），working tree 上還有未 commit 的搜尋功能改動，不要動到它們。

## 技術方案

用 [nk2028/opencc-js](https://github.com/nk2028/opencc-js)（v1.4.0，UMD `full.js`，含雙向字典）：
- `OpenCC.Converter({from:'tw', to:'cn'})` / `({from:'cn', to:'tw'})`
- `OpenCC.HTMLConverter(converter, root, fromLang, toLang)` → `.convert()` 轉整棵 DOM 文字節點（含 `placeholder`、`aria-label`、`lang` 屬性），`.restore()` 還原原文
- 有 `ignore-opencc` class 的元素（含子樹）不轉換

**Vendor 而非 CDN**：下載到 `internal/handlers/static/opencc.min.js`，`render.go:29` 的 `go:embed all:static` 會自動吃進去，維持單一 binary（binary 約增 1–2MB，字體本來就好幾 MB）。但**懶載入**：不在 `_base.html` 常駐引用，由 toggle script 在第一次需要轉換時動態插入 `<script>`——絕大多數看原文的訪客零成本。

## 修改內容

### 1. 新檔 `internal/handlers/static/opencc.min.js`
從 `https://cdn.jsdelivr.net/npm/opencc-js@1.4.0/dist/umd/full.js` 下載 vendor 進來（比照 `htmx.min.js`）。

### 2. 新檔 `internal/handlers/static/opencc-toggle.js`
比照 `_base.html` 內 theme toggle 的寫法（_base.html:98-139）：
- 狀態 `orig | cn | tw`，存 `localStorage.hanscript`，預設 `orig`；同步到 `<html data-hanscript>`。
- `#script-toggle` 按鈕：點擊循環 原→简→繁→原；按鈕文字顯示目前狀態（原/简/繁），`title`/`aria-label` 比照 theme 的「切換到夜間」風格（如「切換為简体显示」）。
- `loadOpenCC()`：動態插入 `/assets/opencc.min.js`，回傳 Promise，只載一次。
- `apply(mode)`：先 `restore()` 所有既有 handler，`orig` 就到此為止；否則對 `document.body` 建新 `HTMLConverter` 並 `convert()`，handler 存進陣列。
- **排除編輯器**：convert 前先幫 `textarea, .CodeMirror, .cm-editor, .EasyMDEContainer` 加上 `ignore-opencc` class（純 JS 處理，不改編輯器模板；避免使用者的 markdown 原稿被改掉）。
- **HTMX 相容**：監聽 `htmx:afterSettle`，mode ≠ orig 時對 `event.detail.elt` 建新 handler 轉換（無限捲動的 feed 卡片也會被轉到）。
- 載入時（DOMContentLoaded）若存的偏好 ≠ orig → loadOpenCC 後 apply。

### 3. `internal/handlers/templates/_base.html`
- theme-toggle 按鈕（177-214 行）後面加同款 `icon-btn`：`<button type="button" id="script-toggle" class="icon-btn">原</button>`。
- `<head>` 加 `<script src="/assets/opencc-toggle.js" defer></script>`（跟 copy.js 等並列）。

### 4. `internal/handlers/static/style.css`
`#script-toggle` 小調整（文字型 icon-btn 的字級/寬度，跟 theme icon 視覺對齊）。改動控制在幾行。

## 不做 / 已知限制

- graph 頁的節點文字由 graph.js 動態畫（canvas/svg），初版不轉換——屬已知限制，不硬塞 hook。
- 字體只有 Source Han Serif TC 子集：轉成简体後部分字形 fallback 到系統字體，風格略不一致（fonts-src/README 本來就記著 SC 字體未出貨，屬既有議題）。
- KaTeX/mermaid 的輸出照常轉（數學式裡幾乎沒中文，無實害）。
- 不加 DB 欄位、不加 /settings endpoint。

## 驗證

1. `go build ./...` 通過；既有測試不受影響（純前端改動）。
2. 啟動 dev server（`DEBUG=1 SEED_DEV=1 ADDR=:8091 DATABASE_URL=... go run ./cmd/server`），用 Chrome 實測：
   - 首頁/feed 點按鈕 → 简：繁體筆記標題（如「數論」）變「数论」；再點 → 繁；再點 → 還原原文。
   - 重新整理後偏好保留（localStorage）。
   - feed 無限捲動載入的新卡片也是轉換後字體（htmx:afterSettle）。
   - /write 編輯器內容**不被轉換**（textarea/CodeMirror 排除）。
   - 看原文的訪客不會載入 opencc.min.js（Network 面板確認懶載入）。
   - **效能實測**：在 feed 無限捲動載入多批卡片後（最重場景）量切換延遲（`performance.now()` 或 DevTools Performance），單次切換應 <100ms。超標的備案是 requestIdleCallback 分塊轉換，預期用不到。

## 效能說明（與深淺色切換的差異）

深淺色切換 JS 只改 `<html data-theme>` 一個屬性，樣式重算全在瀏覽器原生 CSS 引擎，所以無感。繁簡轉換必須真的改字串，逃不掉 JS 遍歷，但成本是一次性的：第一次載字典約 50–150ms（懶載入），之後每次切換遍歷+查字典+寫回約 10–50ms（本站單頁文字量僅幾十 KB，搜尋/feed 都有分頁上限）。轉換後的捲動與互動零影響；htmx 新載入的內容只轉增量。
