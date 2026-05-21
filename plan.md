# Plan

## Next Step
- [ ] 測試 Google OAuth 登入功能
  - [ ] OAuth2.0 要可以提醒使用者他用什麼管道/平台註冊的(e.g. identify by same email)
  - [ ] 用 magic link 登入又用同一支 gmail 登入的話要歸戶給同一個人
- [ ] review and merge changes from `branch/killer`
- [ ] test this so called "Magic Link" feature and ensure that SMTP actually works and sends mail.



> Navigation, Exploration, Interaction.

## Must Have

- [x] CRUD — 筆記的基本增刪改查。
  - [x] progressive load (not pagination) of data
- [ ] 搜尋 — 精確比對、模糊搜尋（仿 Obsidian Ctrl+O）、向量語意搜尋（候選 [sqlite-vector](https://github.com/sqliteai/sqlite-vector)、[pgvector](https://github.com/pgvector/pgvector)）。
- [ ] Meta App — 同一份資料多種呈現，並有類似 Obsidian Search & GraphView 的查詢力。
  - [x] graph (sorta)
  - [ ] local graph
  - [x] timeline (linear)
  - [x] masonry (wall)
  - [ ] canvas (like sticky notes on a bulletin board or whiteboard)
  - [ ] **dora mode.** wiki exploring but better (star-graph, spotlight, switch focus)
  - [ ] kanban?
  - [ ] calendar (date view)
  - [ ] ~~tree (?)~~ https://pbellon.github.io/tractatus-tree/#/
- [ ] 好的資料模型 — 現用 SQLite、未來改 Postgres；GraphDB vs SQL、是否走 GraphQL 待評估。
  - [ ] postgres > 100 users 再考慮
- [ ] 逆向 Obsidian 的殺手功能 — 
  - [x] 例如 [obsidian-flavored markdown](https://obsidian.md/help/syntax)
  - [ ] others… check [Home - Developer Documentation](https://docs.obsidian.md/Home)
- [ ] 單篇筆記與整個 vault 的一鍵匯出 — 
  - [x] simple download .md, .zip
  - [x] copy-to-clipboard
  - [ ] specialized direct import to obsidian: 參考 [Obsidian URI](https://obsidian.md/help/uri)。
- [x] 簡單的上傳與編輯。
  - [x] empty slate
  - [x] or start from a markdown
  - [ ] it is not obvious yet how to send a bunch of markdowns to keep the local internal links of a users vault, and start adding external links to other users' online notes. 
- [ ] 權限與授權管控。
- [x] 管理後台（SQLite 不附，要自己做）。
- [ ] 付費牆管理。
  - [ ] stripe or something?
- [ ] Dev workflow & engineering best-practices
  - [ ] KEEP CLAUDE.MD LEAN
  - [ ] explore -> plan -> run 
  - [ ] `/goal` also cool
  - [ ] use the Harness, build validation hooks (deterministic behavior over probabilistic tuning)
  - [ ] TDD: define clear, concrete deliverables; give clear validation criteria

## Dev & Testing


### Claude code + browser debugger tool

https://code.claude.com/docs/zh-TW/chrome


### Running locally

```bash
make dev          # DEBUG=1, demo seed, dev login enabled
make dev-full     # above + SEED_DEV=1 (multi-user fake data)
```

Log in without email at `/_dev/login?as=alice` (creates user if needed).

### Google OAuth local testing

Google OAuth requires real credentials — there is no mock mode. Steps:

1. Go to [console.cloud.google.com](https://console.cloud.google.com) → APIs & Services → Credentials
2. Create OAuth 2.0 Client ID → Web application
3. Add authorized redirect URI: `http://localhost:8080/auth/google/callback`
4. Copy Client ID and Client Secret into `.claude/CLAUDE.md` (gitignored)
5. Run:
   ```bash
   GOOGLE_CLIENT_ID=xxx GOOGLE_CLIENT_SECRET=yyy make dev
   ```
6. The "Continue with Google" button appears on `/login` only when both vars are set.

**Debugging OAuth failures:**
- `invalid OAuth state` → state cookie expired (>5 min between start and callback) or browser blocked cookies
- `token exchange` error → wrong client secret, or redirect URI doesn't exactly match what's in Google Console
- `no email in Google userinfo` → scopes missing; ensure `openid` and `email` are listed

## Should Have

- [x] 明暗主題
- [ ] 繁簡轉換。
  - [ ] [GitHub - BYVoid/OpenCC: Library for conversion between Traditional and Simplified Chinese · GitHub](https://github.com/BYVoid/OpenCC)
- [ ] 思源宋體（or 源漾明體、源流明體）
  - [x] https://github.com/adobe-fonts/source-han-serif
  - [ ] https://github.com/ButTaiwan/genyo-font
  - [ ] that's serif, for sans serif go with 思源or源流黑體
- [ ] 本地端字體選項 fontsize, serif or sans serif, simple stuff（參考 Zotero local view options or gitbooks, or whatever）。
- [ ] tag merging issue? 應該多用大家在用的 tag 吧 (based on number of usages of that tag, show that when picking tag, easy)
  - A3. 標籤 chips
  - 有小計數（e.g. 「#音樂 12」）
- [ ] mirroring with easy
  - [ ] 已有經營 blog 的人如何一鍵同步過來？
  - [ ] 又分 normal blog vs. hypertext densely linked blog
- [ ] 維持個人vault內部結構之外，為什麼應該跟 Comma 上的人互動？ Because connection with others is the whole point?



## Could Have

- [ ] 圖片支援（待定）。
- [ ] Library page: 優質公有領域文本，like Project Gutenberg

## Won't Have

- 影片（含短影音）。
- PDF。
- AI 整合。

## Scaling Issues

- [ x ] 現況 — SQLite 單檔當 DB，需要一台 24/7 不掉資料的專屬機（用 [Fly.io](https://fly.io/)）。
- [ ] 計畫 — DB 換 hosted Postgres（[Neon](https://neon.com/)），Go server 改用 Vercel 等 serverless 平台處理 request、query、HTML render。
  - [ ] or perhaps cloudflare 全家桶
- [ ] DDoS issues
- [ ] concurrent users issue
  - [ ] writing queue?
  - [ ] reading? 



# some concerns (iterative)

- [ ] liked and saved should be separated
  - [ ] e.g. don't like a post but want to save for later, or like a post but don't want to visit later.
- [ ] distinctions
  - [ ] inbound/outbount links
  - [ ] linked by self or by others
- [ ] need random / suprise-me / I'm feeling lucky button or page
- [ ] can users change their @handle (ID)? 
- [ ] maybe no "folders"?  how to organize notes of a user? collection via tags and just pure linking from notes? why bother with folders?
- [ ]   what does migration means? we can afford to drop the db anytime now, why are we accumulating techdebt now already?
- [ ] migrations: in early dev it's fine to squash and reset the DB periodically; keep the schema clean, not precious
- [ ] need to handle empty links (stubs) like wikipedia or obsidian does. 
- [x] need quick reply to others note
- [ ] feed page card view doesn't render markdown correctly. all returned HTML should not contain un-rendered markdown, except for the editing "textarea" of writing pages/sections
- [ ] user avatar image: use dicebear or Hank's NFT-like Weedie.

# Old（2026-05-16）單篇筆記頁改版（/{user}/{path}）, check if all done

這頁差距比 feed 大得多。現在比較像「Markdown 文件 + 下方清單」，參考檔則把它做成「**有作者識別 + 行動按鈕 + 內文 + 結構化的關聯區塊**」的內容頁。

## B1. 頂部 navbar
- 跟 feed 統一：左邊一個「←」回上頁、中間搜尋欄、右邊頭像。
- 取代現在的「← 回 XX」一行 banner。

## B2. 作者橫條（新增）
- **現在**：沒有。作者資訊只夾在標題下方的小字裡（`@handle / 資料夾 · 3 天前`）。
- **參考**：標題上方一條獨立的作者橫條，含：
  - 較大的圓形頭像（40px、配色用作者主題色）
  - 作者名 + 「/ 資料夾」
  - 一行統計：「1,247 訂閱者 · 142 篇筆記 · 24 個資料夾」
  - 右側 **「+ 追蹤」按鈕**（資料庫已有 follows 表，但目前 UI 上看不到追蹤入口；要做）
- **要做**：新增這條 author-bar；訂閱數先用真實資料（追蹤者數、筆記數、資料夾數）。

## B3. 文章 meta 列（改版）
- **現在**：標題下一行小字 + 一列 tag chips。
- **參考**：
  - 左：「更新於 3 天前 · 約 6 分鐘 · #tag #tag #tag」（**閱讀時間是新東西**，用字數估算）
  - 右：三顆動作按鈕——`♡ 89`、**`⬇ 匯入到我的 vault`**（藍底強調）、`⋯`
- **要做**：
  - 補閱讀時間（依字數 / 250 字一分鐘）
  - **「匯入到我的 vault」是新功能**——你要決定：它是「下載 .md」（現在已有），還是「直接複製到我自己的 vault 裡」（這要做後端，等於借用別人的筆記，需要先想清楚怎麼標記來源、是否要徵得作者同意）
  - 「⋯」內含：copy markdown、report、原本的 download .md

## B4. 標題
- **現在**：跟內文同字體大小、無特別處理。
- **參考**：大標 24px、字距微縮、行高 1.3。
- **要做**：放大標題層級。

## B5. 內文 wikilink 樣式
- **現在**：靠 markdown 渲染器產生連結（樣式平淡）。
- **參考**：藍色字 + 藍色虛線底線；hover 時底線變實線、加淺藍底。
- **要做**：在 CSS 對 `.wikilink` class 加上這套樣式。

## B6. 「這篇連到的筆記」區塊（to complement local graph）
- **現在**：用 canvas 畫一張小型 local graph。
- **參考**：用**迷你卡片網格**列出這篇連出的筆記，分成兩個子區塊：
  - 同個 vault · 8 篇
  - 跨 vault · 連到其他人的筆記 · 4 篇（每張卡右上角貼「跨 vault」綠色小標籤）
- **要做**：資料來源 `links` 表已有，新增這個區塊。**Local graph 要保留還是換掉？** 兩個都顯示會太擠，建議：
  - 預設顯示**迷你卡片區塊**（資訊密度高、可點開）
  - graph 視覺化收在最上面 navbar 旁邊或 footer 一個按鈕，按了才展開
  - **等你決定**

## B7. 「引用了這篇筆記的人」區塊（backlinks 改版）
- **現在**：兩個小區塊（本 vault / 跨 vault）+ 簡單清單。
- **參考**：合併成一個區塊，每筆是一橫列：「↺ 圖示 + 標題 + 右邊作者小頭像 · 資料夾」。資訊清楚很多。
- **要做**：合併並改版面，保留「本 / 跨 vault」標示但用 badge 或小字。

