# `Comma,` Dev Roadmap

## 資料夾匯入 Beta — 2026-09-05

書寫頁新增「匯入筆記 Beta」，支援選取 Markdown 檔案或資料夾、在本機查看原文並逐檔勾選、發布勾選內容、進度／結果報告；預設全部不勾選。補齊 profile、tag、saved、following 的複合游標。目標是實際觀察大量匯入帶來的好與壞事件。待討論問題、beta 邊界與 30 分鐘驗證流程見 [`docs/BETA-vault-import.md`](docs/BETA-vault-import.md)。批次 feed 卡已定案（#42）：一次匯入 ≥ `FEED_BATCH_MIN` 篇，在推薦 feed 收成一張「@alice 匯入了 N 篇筆記」卡，連到 `/{user}?batch=<id>`。原始日期排序仍未定案。

## Meta-App view Substrate + Templ — 已完成（2026-08-31）

19 個模板全轉 templ、`NoteListView`/`feedCard`/`cardRenderers` registry 收斂六個消費者。完整記錄（含順便修的兩個 bug、決定不做的版面切換鈕）在 `.claude/specs/feed-layout-toggle-meta-app-interface-spec.md`「v1 — as built」。

## MVP 設定收尾 — 三人內部發文

三個團隊成員能在 https://commaplace.app 登入、發文、互相看到。其他一切先不管。功能夠了（CRUD、feed、wikilink、like 都能用），剩下的幾乎都是設定。

- [ ] **SMTP** — 申請 Resend / Brevo / Postmark free tier，填 `SMTP_HOST/PORT/USER/PASS/FROM`。magic link 程式早就寫完，純設定。目前靠 `PLAYTEST_LOGIN_KEY` + `/_dev/login?as=<handle>&key=<key>` 頂著
- [ ] **magic link 實測** — 連同「同一支 gmail 走 magic link 與 Google OAuth 要歸戶給同一個人」。程式寫了，沒測過
  - [x] 歸戶邏輯有自動測試了（`internal/auth/linking_test.go`，假 Google server，兩種順序）。抓到一個真 bug：Google 回大小寫不同的 email 會開第二個帳號，已改成比對 `lower(email)`。真寄信、真 Google 還是要上線實測
- [x] 登入時提醒使用者他當初是用哪個管道註冊的（用 email 認人）— `/login` 在這台瀏覽器上次用的管道旁標「上次使用」（`last_login` cookie）。刻意不做「輸入 email 查註冊管道」：那會讓任何人查出哪些信箱註冊過、用什麼註冊。換裝置就沒有提示
- [ ] **`ADMIN_HANDLE` / `ADMIN_EMAIL`** — 在 Render 後台設好，確認自己進得去 `/admin`
- [ ] **`BASE_URL` 改成 apex** `https://commaplace.app`，Console 補 `https://commaplace.app/auth/google/callback`

**明確不做**（等三人真的用起來再說）：搜尋強化、付費、私人/不公開筆記。

### Render

現況與重建計畫都在 `FRESH_START.md`：沒有任何 Blueprint 在管線上的服務，$17.15/mo，重建後 $13.20/mo。**現在什麼都不用做**，那份重建要三個人（網域、OAuth Console、Render 後台）到齊才動。

已完成：Blueprint 部署、自訂網域（apex 為主，www 301 導過去）、Auto-Deploy 開著、`SEED_DEV=0`、`PLAYTEST_LOGIN_KEY`、`.github/workflows/ci.yml` 測試閘、`db-backup.yml` 手動 pg_dump。破壞性操作的完整程序見 `docs/RUNBOOK-db-purge.md`。

種子帳號已清乾淨（migration 007/008，prod 9 個 migration 全上）。刪帳號時哪些看似種子其實是真人、哪個看似真人其實是種子——完整故事在 `docs/RUNBOOK-db-purge.md`。

### Google OAuth

現在能登入是靠運氣：apex 才是主網域，但 `BASE_URL` 指著 www，`oauth_state` cookie 沒有 `Domain`、只綁 apex——靠 www→apex 的 301 帶著 query string 才把 cookie 送得出去。哪天那個 301 不保留 query，登入就掛 `invalid OAuth state`。上面「`BASE_URL` 改成 apex」那項修的就是這個。其他失敗排查見下面「Google OAuth 本機測試」的對照表。

---

## Must Have

- [x] CRUD、progressive load
- [x] wikilink：embed、stub、同名處理（per-author `UNIQUE(author_id, slug_ci)` + `@user` 語法，全域撞名不可能發生）
- [x] **連結一律走 UUID，不走名字**。`links.resolved_target_id` 是唯一真實來源，改名不斷鏈；`link_regression_test.go` 守著。順手抓到真 bug：編輯器路徑（autosave → publish）漏了 stub backfill，`/write` 跟 import 有做，`autosaveNote` 現在也補上了
- [x] **hover 連結預覽** — 滑過 `[[wikilink]]` 就浮出那篇的卡片（照 LessWrong 那套），不用整頁載完才知道值不值得跳；`[[@user/note]]` 同樣有，跨 vault 的兔子洞才不用每次付一次 page load。卡片直接是 feed 的 `masonry_card` 吃 `feedCard`——沒有第二套卡片模型，所以清單那半邊重構時它跟著走。`GET /api/preview/{user}/{slug}` 只出已發布的，gate 寫在 SQL 裡，因此與看的人無關、可快取
  - [x] **段落預覽** — `[[note#Heading]]` 浮出的是那一段，不是整張卡（`?h=`，找不到段落就退回整張卡）。`linkpreview.js` 改由 layout 載入，每頁都有，`data-no-preview` 可排除；卡片太高時縮到連結上下較寬的那側、不鑽進 sticky topbar 底下（PR #41）
  - [ ] **外部連結預覽** — `linkpreview.js` 的 `previewURLFor()` 就是留好的接口，缺的是抓 og tag 那半（fetcher + 快取表 + SSRF 防護）
- [ ] 搜尋
  - [x] Cmd+K 模糊標題（不是 Ctrl+O，避開瀏覽器熱鍵）
  - [ ] Ctrl+F 內文搜尋 + `line()` / `tag()` / `section()` 運算子 — 運算子語意未定案，先確認規格 → maybe `cmd+shift+K`, using similar set of hotkeys for fuzzy/exact search 
- [ ] **Meta App** — 同一份資料多種呈現，有 Obsidian Search / GraphView 等級的查詢力
  - [x] masonry（list/grid 曾經上線又被 `014ed21` revert 掉，見 spec 的 v0 update；substrate 現在是 pluggable registry，加回 list/grid 是一個 func + 一行 registry，還沒人要）、global + local graph（單擊即跳）、calendar、RSVP 快速閱讀
  - [x] **2-hop graph** — `/api/graph/local` 加了 `?hops=`（1 或 2，預設 1），`expandNeighbors` 按層擴張，`maxLocalGraphNodes=150` 防一個 hub 兩跳炸開畫布。筆記頁的關聯圖盒子仍用預設一跳（試過兩跳，畫面太亂，改回去，見 `notes_pages.templ`）；API 本身留著兩跳能力，dora mode 或別的消費者要用隨時接
  - [x] **timeline（linear）** — 直的。跟 calendar 一起併進個人頁，`?view=timeline`（預設）/ `?view=calendar` 切換，獨立的 `/me/calendar` 路由已移除。時間軸是左側日期軌 + 右側卡片（`NoteListView.GroupByDate` → `timeline-group`）
  - [ ] **dora mode — 卡上線**。star-graph，聚光燈打在當前節點，可換焦點
  - [ ] **canvas**（靜態可拖曳的便利貼牆，不要 graph 那種會抖的）、kanban — backlog 
	  - like a workbench with a bunch of post-it notes to shuffle around
- [x] Tag page 文字雲（可關），依使用次數 — 是**相關標籤**雲（與當前標籤同篇出現的），不是全站標籤。全站雲會隨標籤語彙無上限成長，而且那個聚合本來就是最慢的查詢（見下方 benchmark）；「跟這個標籤一起出現的是什麼」也才是有用的問題，點下去就進入那個標籤自己的雲。範圍外的標籤走標籤搜尋
- [x] design 加 Small Caps — 用 Alegreya SC（獨立的 small-caps 字族，小寫碼位畫的是真的小型大寫，cap height 的 78%），不是 `font-variant-caps`：思源宋體沒有 `smcp`（fontTools 驗過），瀏覽器只會拿大寫縮放合成，正是 Butterick 說不要的假貨。走 jsDelivr（`docs/DECISIONS.md` 2），Latin-only `unicode-range`，只用在作者 handle 一種角色
- [x] 資料模型 / 後端架構 — Postgres（UUID PK、link 表用 ID 解析）、單一 Go binary on Render，serverless 否決。理由見 `docs/DECISIONS.md` 3 與 6
- [ ] 逆向 Obsidian 的殺手功能 — obsidian-flavored markdown 做了，其餘翻 [Developer Documentation](https://docs.obsidian.md/Home)
  - [x] **段落 embed / 標題連結 / 標題自動完成**（PR #41，spec：`.claude/specs/obsidian-flavored-markdown-spec.md`）— `![[note#Heading]]` 只嵌那一段、`#A#B` 可跳層、比對忽略標點與大小寫，規則照 Obsidian 1.14.4 `app.js`。`[[note#` 列出目標筆記的標題，插入的是資料庫裡的 slug/handle。embed 與存檔的 N+1 一併修掉（一層 embed 一次查詢；連結、標籤各一個 statement）
  - [x] **`^id` 區塊標記一律隱藏**，閱讀與 feed 摘要都看不到。代價：段落結尾的 ` ^字`（如 `r ^2`）也會被吃掉——Obsidian 同一條規則，已決定接受
  - [ ] **打開 block refs** — `internal/markdown/subpath.go` 的 `blockRefs = false`。已做好：`[[note#^id]]` 捲到該區塊、`![[note#^id]]` 只嵌該區塊。還缺：`[[note#^` 自動完成、替別篇筆記產生 `^id`（會寫到第二篇筆記）
  - [ ] frontmatter `aliases` 解析連結、Properties 面板
  - Obsidian 的 `app.js` 沒有完全混淆原始碼，反編譯就看得到——真的要對齊行為時的最後手段
  - [vscode-markdown-preview-enhanced](https://github.com/shd101wyy/vscode-markdown-preview-enhanced) 實作了很相近的一套功能，比較簡單但夠穩，可以直接讀它怎麼做（[DeepWiki](https://deepwiki.com/shd101wyy/vscode-markdown-preview-enhanced)）
- [x] 匯出 — .md / .zip 下載、複製到剪貼簿、`obsidian://` 一鍵開啟
- [x] 上傳與編輯 — 空白起手、從 markdown 起手、Medium 風格編輯器
- [x] **批次匯入 Beta** — 書寫頁選取檔案或資料夾、本機檢查並勾選、一次發布與結果報告；細節和仍待驗證的 Obsidian 相容性見 `docs/BETA-vault-import.md`
  - [x] 內部連結跨批次保留 — `saveNote` 在目標筆記一出現就 backfill stub link，順序無所謂
  - [ ] 匯入時撰寫 `[[@user/note]]` 跨 vault 連結 — 還沒有這回事，跟「保留」是兩件事
- [ ] 權限
  - **MVP 全公開**，只有草稿 vs 已發布，作者本人才能改刪
  - private（僅自己）與 unlisted（有連結才看得到、不進 feed/search/graph）留到上線後。見 `docs/DECISIONS.md` 7
- [x] 管理後台 
	- [ ] 還沒實際試用過
- [ ] 付費牆
  - ***Stripe* is out**：沒有美國公司。不卡上線，先免費跑，等有人表現出付費意願再做——瓶頸是需求不是串接
  - 真要做就走 Merchant-of-Record（Paddle / Lemon Squeezy / Gumroad），台灣本地選項 ECPay / NewebPay / TapPay 需要公司實體。⚠️ 每家的台灣賣家/出金資格都要先查，會變
  - [ ] **先擺一顆 Buy Me a Coffee 鈕**（或 Ko-fi，都不用公司）。不用等付費牆，這是最小的付費意願訊號。卡在你先去申請帳號拿連結——拿到就是加一個連結的事
  - 付費項目構想：private/unlisted 筆記（免費版草稿只留 3~7 天）、更多圖、更深的巢狀 embed、無限收藏清單（免費只有「喜歡」跟「稍後」兩個）
- [ ] Dev workflow
  - [x] CLAUDE.md 保持精簡、explore → plan → run、skills/commands、驗證 hook（要確定性行為，不要機率性調參）
  - [ ] TDD：明確可驗收的交付物與驗證條件
  - [ ] `/goal` 也不錯
- [x] `/random`「漫遊」、分享鈕 + Open Graph、繁中 UI
- [x] `/terms` + `/privacy`（v0 繁中草稿，上線前需法務校訂）

**中文 UI 現況**：使用者流程幾乎都中文了；`admin_dashboard.html` / `admin_reports.html` 刻意留英文（內部工具）。i18n 框架**還不需要**——沒有語言切換機制，真要做約是 20 個模板裡數百條字串。繁簡轉換是另一回事（中文內部的字形轉換），沒有共用管線。


## Should Have

- [x] 明暗主題、減少動畫、本地端字體選項（Aa popover：字級 + 宋/黑體）
- [x] 繁簡轉換 — 後端搜尋互通（opencc s2t/t2s 接進 `/search`、wiki-link、tag autocomplete）＋ 前端 原→简→繁 三態鈕
- [ ] 字體 — 思源宋體已上（見下方 CJK 交付）；黑體目前沒有字檔，走 `--font-sans` 系統字（各平台各長一樣：PingFang TC／微軟正黑體／Noto Sans CJK）。待選：[genyo-font](https://github.com/ButTaiwan/genyo-font)、思源或源流黑體
- [x] **tag picker**：選現成標籤要是阻力最小的路，開新標籤必須是刻意、視覺上分開的最後一步。目標是擋掉 X/FB 那種把標籤當強調色用的濫用，但不禁止開新標籤。決定只做編輯器 popup，行內 `#foo ` 不擋
  - [x] 依使用次數排序並顯示計數（編輯器 `#` popup 與 feed 標籤搜尋共用 `GetTagSuggest`）
  - [x] 編輯器 `#` popup：現成標籤排前面，「新標籤 #foo」是分隔線下的最後一列，Enter／Tab 都不會選它，要方向鍵或點擊。只剩新標籤可選時，Enter 就是換行。打 `#foo` 加空白照樣是標籤（行內 markdown，刻意不擋）
- [ ] 一鍵鏡像 — 已經在經營 blog 的人怎麼同步過來？普通 blog 與密集連結的 hypertext blog 又是兩種做法
  - 抄 [obsidian-clipper](https://github.com/obsidianmd/obsidian-clipper)：開源，抓網頁轉乾淨 markdown 的部分正是「從既有 blog 匯入」缺的那塊，不用自己寫爬蟲跟正文抽取
- [ ] 維持個人 vault 內部結構之外，為什麼該跟 Comma 上的人互動？
  - THE WHOLE PREMISE AND VALUE PROPOSITION IS COMMUNITY. **COMMA,** THINK TOGETHER WITH US.

## Could Have

- [x] 圖片支援（1 張/篇，bytea）
  - [ ] 放寬到 1–10 張：成本仍是每篇 O(1)（固定上限，不隨內容成長）；schema 用 `note_images` 表加 CHECK/trigger 限 10 列
- [ ] Library page：優質公有領域文本，像 Project Gutenberg

## Won't Have

影片（含短影音）、PDF、AI 整合。

---

## Tech Stack

架構決策的來龍去脈在 `docs/DECISIONS.md`，這裡只記還沒做的。

- **Server**：Go `net/http` + `templ`（`html/template` 已於 2026-08-31 全部換掉，見下）+ `go:embed`，單一靜態 binary，不加 build 步驟。Postgres via `pgx/v5`，FTS 用 `tsvector`+GIN
- **GraphQL / GraphDB：不用，問題不成立。** GraphQL 解的是「JSON client 過度或不足抓取」，我們的 handler 回的是 HTML fragment，沒有那個 client。GraphDB 解的是深度不定的遍歷，我們的圖查詢是有界的（一跳，之後兩跳），`links` 表加索引就夠——benchmark 裡 backlinks 走 bitmap scan 本來就快。真的要無界遍歷再回來看，別為了名字裡有 graph 就換資料庫
- **htmx**：vendored + 手寫 `hx-*`。已用原生 `HX-Request` header（不是 `?partial=1`）。OOB swap、`HX-Trigger`、`hx-indicator`、`hx-boost` 有需要再逐個接，別預先鋪
- **Alpine.js**：vendored 無 build（`static/alpine.min.js` 3.14.9，只在編輯頁載入）。[x] wiki／tag autocomplete 的 popup 狀態已改成 `Alpine.store("ac")`。feed 版型記憶不需要了（list/grid 切換已 revert，只剩 masonry）。EasyMDE 跟 graph canvas 不要碰，Alpine 幫不上忙
- [x] ~~**templ**：跟 Meta-App view substrate 重構**一起**做~~ — 2026-08-31 做完了，見最上面「Meta-App view substrate + templ — 已完成」

### 待還的技術債

- [x] ~~`notes.go` 800 行拆成 `notes.go` / `links.go` / `notestats.go`~~ — `cfdc0af` 做完了（631 / 283 / 56 行），順便把五段各自的 query+scan 收斂成單一 `noteCardColumns` + `scanCards`
- [x] ~~`feedItem` / `profileNote` / `searchHit` 三個卡片型別 + 7 個 scan loop 收斂成一個 `feedCard`~~ — 2026-08-31 做完了
- [x] ~~`map[string]any` 換成 typed struct~~ — templ 直接給了型別安全的元件，這項被上面的 Meta-App view substrate 工作吸收掉了

### CJK 字體交付

思源宋體 TC 自架，`cn-font-split` 切成 668 個 woff2 chunk，瀏覽器只抓頁面上有的字（密集中文頁 ~3.6MB，原本 19.7MB）。原始檔在 `fonts-src/`（不進 `go:embed`）。**簡體還沒切**，等簡中支援時重跑。

天花板：每多一個自架 CJK 字族 ≈ +34MB binary、+668 檔案。字體選擇器（4 個字族 ≈ 155MB / 2,700 檔）就是那條線。**不要 embed 第二個字族**——改用 jsDelivr + Fontsource（中立開源 CDN，非 Google，本來就切好了），約 10 分鐘的事。UI chrome 用系統 sans，本來就不用下載。

## Dev & Testing

`/_dev/login?as=alice` 免 email 登入（不存在就建）。瀏覽器除錯：https://code.claude.com/docs/zh-TW/chrome

### Google OAuth 本機測試

沒有 mock 模式，要真憑證。Console → APIs & Services → Credentials → 建 OAuth 2.0 Client ID（Web application）→ 加 redirect URI `http://localhost:8080/auth/google/callback` → `GOOGLE_CLIENT_ID=xxx GOOGLE_CLIENT_SECRET=yyy make dev`。兩個變數都在，`/login` 才會出現「Continue with Google」。

失敗對照：

| 症狀 | 原因 |
| --- | --- |
| `invalid OAuth state` | state cookie 過期（start 到 callback 超過 5 分鐘）或瀏覽器擋 cookie |
| `token exchange` error | client secret 錯，或 redirect URI 跟 Console 不是逐字相同 |
| `no email in Google userinfo` | scope 少了，要有 `openid` 跟 `email` |
| `Error 400: invalid_request` | 讀錯誤訊息裡的 `redirect_uri=`。沒有 scheme 表示 `BASE_URL` 缺 `https://`；有的話就是 Console 沒註冊（`www.` 算數） |

## Scaling

- [x] Postgres on Render，Go server 單一 binary，不走 serverless
- [ ] Render.com more expensive tiers 
- [x] DDoS — 量級攻擊由 Render 前面的 Cloudflare 擋（免費、自動）；應用層濫用由 app 內限流擋（#36，`go-chi/httprate`：每 IP、每登入者寫入、每登入 email）
  - [ ] **prod 拿不到 `CF-Connecting-IP`（2026-10-15 部署後 log 出現 `ratelimit: request without CF-Connecting-IP; per-IP limits are off`）**。這行只在第一個缺 header 的請求印一次，所以還不知道是**所有**使用者請求都缺，還是只有 Render 內部請求缺。若是前者，每 IP 限流（全站 1200/分、匿名寫入、登入 300/10 分、email+IP）在 prod 等於沒開；每登入者寫入與每 email 每小時上限照常有效
    - Render 沒有文件說它設哪個 client IP header。社群說法：`X-Forwarded-For` 第一個是真實 IP，但 Render 不清掉 client 自己送的，所以可偽造（[1](https://community.render.com/t/accessing-client-ips-in-a-node-express-app/36282)、[2](https://feedback.render.com/features/p/send-the-correct-xforwardedfor)）
    - 下一步：加一次性診斷 log（第一個有／沒有 `CF-Connecting-IP` 的請求各印一行：path、有哪些轉送 header 的名稱、`X-Forwarded-For` 有幾段，不印 IP），部署後看實際 header 再決定 key 怎麼取（`internal/handlers/ratelimit.go` `clientIP`）
- [ ] 並發：寫入佇列？讀取？

- [x] **Tag chips 的修法** — 已有 5 分鐘 in-process cache（`feed.go` `topTagChips`）；100K 筆記時沒快取的聚合 36–49 ms，每 5 分鐘最多跑一次
- [x] **並發測過了（2026-10-14，100K 筆記）** — 寫入不是問題（autosave 100/s p99 13 ms、同一篇按讚 300/s p99 16 ms、全部 200），不需要寫入佇列。讀取在混合流量 20→40 req/s 之間到頂，瓶頸是 4 核 CPU（97%），不是連線池大小：
  - [x] `SetMaxIdleConns(25)`（#32）— 原本預設 2，每波流量都重連 Postgres
  - [x] `/feed` recommended — 改成存好的 `feed_rank` + keyset（#42），~200 ms → 1–3 ms；順便修掉 per-author 上限讓筆記永遠被跳過的 bug。批次匯入收成一張卡、連發會被往後推，參數 `FEED_BATCH_MIN` / `FEED_BURST_STEP` / `FEED_BURST_WINDOW` 是先猜的，等真實資料調
  - [x] `/search` — handle 條件改成 `author_id = ANY(...)`（#35），~280 ms → 選擇性高的詞 11–50 ms。只打一兩個字母、命中大部分 handle 時仍是全掃
  - [x] 熱門標籤頁 — 相關標籤雲 5 分鐘快取（#33），快取命中 5–15 ms
  - [x] `/api/wiki/suggest` — pg_trgm 索引（#39），少見的 3 字以上查詢 ~150–360 ms → <10 ms；1–2 字查詢 trigram 幫不上，維持原樣
    - [x] prod log（2026-10-15）：`pg_trgm: CJK titles use trigram index = true`，中文 3 字以上查詢走索引
  - [ ] `/api/graph` 全域圖 19 MB 沒上限

原始數字：`docs/BENCHMARK-2026-10-12.md`（取代 06-20 那份；重跑：`scripts/bench-seed.sql` + `scripts/bench-load.sh`）。

## Some Concerns

- [x] 讚與收藏分開 — `saves` 表（migration 006）。**`/me/saved` 從空的開始**，006 只建表沒搬資料。要帶的話：`INSERT INTO saves SELECT user_id, note_id, created_at FROM likes ON CONFLICT DO NOTHING;`
- [ ] 多清單 / playlist 管理 — 目前只有單一收藏清單
- [ ] 兩個不同作者**同時**發布互相連結的筆記會 deadlock（Postgres `40P01`，其中一個發布失敗 500）。#42 之前就存在；同作者的已由 #42 的鎖順序修掉，跨作者的沒有。人少時幾乎碰不到
- [ ] **上線前把 `001_init.sql`…`009_*.sql` 壓成一個檔**。趁 DB 還能隨便丟的時候做，有真用戶就沒這個自由了（`docs/DECISIONS.md` 3 的 "Revisit when"）
- [x] 連結分區 — outbound（這篇連到誰）vs inbound（誰引用這篇），各自再分「自己的 vault」vs「其他人」
- [x] 改 `@handle` — 連結靠 UUID 不斷鏈
- [x] 沒有資料夾，用 tag + 連結組織（`folder_path` 欄位已整個刪掉）
- [x] stub 連結、快速回覆、feed 卡片正確渲染 markdown、頭像產生器
- [x] autosave 堆積空草稿 — 根因是 `/write` 每次載入都插一列空草稿，而 `sweepOrphanDrafts` 只在下次有人訪問 `/write` 時順手清（7 天門檻），沒有排程。沒改清理節奏（範圍外），改成給手動出口：個人頁草稿分頁可多選批次刪除
- [x] publish guard 誤擋有標題的筆記 — 兩個真 bug：(1) autosave 失敗（例如 slug 撞名）不會擋下發布點擊，server 端還握著舊的空標題；(2) 純 emoji/標點的標題讓 `kebabSlug` 回傳空字串，slug 就永遠卡在 `draft-*`，而 publish guard 正是擋這個
- [x] 個人頁不再印自己的 email（本來就只有本人看得到，但也沒理由印）
