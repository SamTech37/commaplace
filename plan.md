# `Comma,` Dev Roadmap

## 下一步 — Meta-App view substrate，直接用 templ 寫

規格：`.claude/specs/feed-layout-toggle-meta-app-interface-spec.md`（v0 已上線，這是 v1）。

**現況就是反 DRY 的樣板**：四個卡片型別（`feedItem`、`feedCard`、`profileNote`、`searchHit`）、七個 scan loop、**五個**版面各自手刻 `.note-card` 清單（`feed`、`tag`、`search`、`profile`、`saved`——規格只寫了四個，漏掉 `saved`）。加一個版面要改五個地方，所以沒人加；excerpt 處理也各是各的。

**做完長這樣**：一份資料來源、多種呈現。四（五）個 handler 變成只產查詢的薄殼，共用一個 `NoteListView{ Cards, Layout, OlderURL, Empty }`，版面切換鈕與 `?layout=` 到處都有。之後加 timeline / canvas / calendar / dora 是註冊一個 renderer，不是開一個新頁面——那才是 Meta-App 真正的意思，也是 timeline 跟 dora 這兩個卡上線的項目一直沒動的原因。

**graph 那半邊已經是這樣了，照抄就好。** `/api/graph` 是**一個** endpoint 吃 `?user=` / `?tag=` 篩選（`graph.go:55` `GetGraphData`），`GetGraph` / `GetUserGraph` / `GetTagGraph` 只是三個薄殼負責渲染頁面。全域、標籤、使用者圖回傳同一個形狀，所以加一種圖不用開新 handler。清單那半邊反過來——五個 handler、五份模板、四個卡片型別。要抄的樣板不用發明，就在 `graph.go`。

一句話講完：**UI 是狀態的函數**。substrate 應該按**資料形狀**切，不是按頁面切。今天只有兩種形狀——筆記清單、筆記關聯圖——圖已經統一了，清單還沒。

**用 templ 直接寫，不要先用 stdlib 重構一次再遷移**（`docs/DECISIONS.md` 5）。這個重構本來就是把 18 個模板重排，遷兩次等於白做一次。templ 是 Go 工具（`go install`、`go.mod` 釘版本、`templ generate` 產 `.go`），把產生的 `.go` commit 進去，部署還是純 `go build`——不違反「部署路徑不加 build 步驟」（`docs/DECISIONS.md` 1）。順帶把 `map[string]any` 資料袋的無聲 typo 變成編譯期錯誤。

順序：

1. `templ generate` 接上 air 的 dev loop，先遷一個模板驗證流程 → 驗證：`make watch` 改 `.templ` 會重編，`go build ./...` 綠
2. 抽出共用 view（toggle + 三種版面 + 三個卡片模板），定義 `NoteListView` → 驗證：`/feed` 行為與現在完全相同
3. 五個版面逐一接上去，一次一個，每個都有測試 → 驗證：每頁都有切換鈕、無限捲動維持當前版面、沒有生 markdown 外洩
4. 砍掉 `feedItem` / `profileNote` / `searchHit` 與多餘的 scan loop → 驗證：`grep` 不到，測試全綠

**每一步都要盯 N+1。** 把五個 handler 收斂成共用 view 的時候最容易長出來——共用的卡片元件想多要一個欄位（作者頭像、標籤、讚數），最省事的寫法就是每張卡各查一次。查詢次數必須跟卡片數無關；不確定就 `EXPLAIN ANALYZE`。我們是按用量付費的。

**不在這一輪**：graph 併進版面列表（先留在自己的頁）、排序控制、版面偏好存 DB（維持 localStorage）、timeline / canvas / kanban。

**待決**：偏好是全站共用一把 `feed.layout`，還是每個版面各自記？（單一比較簡單；分開的話使用者可以 profile 用清單、feed 用瀑布流。）

## MVP 設定收尾 — 三人內部發文

三個團隊成員能在 https://commaplace.app 登入、發文、互相看到。其他一切先不管。功能夠了（CRUD、feed、wikilink、like 都能用），剩下的幾乎都是設定。

- [ ] **SMTP** — 申請 Resend / Brevo / Postmark free tier，填 `SMTP_HOST/PORT/USER/PASS/FROM`。magic link 程式早就寫完，純設定。目前靠 `PLAYTEST_LOGIN_KEY` + `/_dev/login?as=<handle>&key=<key>` 頂著
- [ ] **magic link 實測** — 連同「同一支 gmail 走 magic link 與 Google OAuth 要歸戶給同一個人」。程式寫了，沒測過
- [ ] 登入時提醒使用者他當初是用哪個管道註冊的（用 email 認人）
- [ ] **`ADMIN_HANDLE`** 後台設成 `admin`，但 prod 沒有這個 user，等於沒人進得去 `/admin`。改成自己的 handle
- [ ] **`BASE_URL` 改成 apex** `https://commaplace.app`，Console 補 `https://commaplace.app/auth/google/callback`

**明確不做**（等三人真的用起來再說）：timeline、dora mode、搜尋強化、tag 文字雲、付費、私有筆記。

### Render

現況與重建計畫都在 `FRESH_START.md`：沒有任何 Blueprint 在管線上的服務，$17.15/mo，重建後 $13.20/mo。**現在什麼都不用做**，那份重建要三個人（網域、OAuth Console、Render 後台）到齊才動。

已完成：Blueprint 部署、自訂網域（apex 為主，www 301 導過去）、Auto-Deploy 開著、`SEED_DEV=0`、`PLAYTEST_LOGIN_KEY`、`.github/workflows/ci.yml` 測試閘、`db-backup.yml` 手動 pg_dump。破壞性操作的完整程序見 `docs/RUNBOOK-db-purge.md`。

種子帳號已清乾淨（migration 007/008，prod 9 個 migration 全上）。刪的條件是 email 網域 `@dev.local`，不是手寫 handle 名單——名單漏一個就是誤刪真人。`hankforwork2315`、`noshawn50` 長得像種子但是真人；`shawn` 反而是 `ApplyDemo` 的種子，已改名 `shawn_demo`（`seed.DemoHandle` 要一起改，否則守衛失效會再長一份 demo vault）。這些只有拿真 prod dump 試跑才看得到，dev DB 看不出來。

### Google OAuth

redirect URI 是 `BASE_URL + /auth/google/callback`（`cmd/server/main.go:176`），Google 逐字比對，`www.` 有無算兩個 host。後台手改的 env var 會蓋掉 `render.yaml`。`Error 400: invalid_request` 就去看 `redirect_uri=` 那串。

現在能登入是靠運氣：apex 才是主網域，但 `BASE_URL` 指著 www。`oauth_state` cookie 沒有 `Domain`、只綁 apex；Google 把人送到 www 的 callback，www 301 回 apex 時**有帶 query string**，cookie 才送得出去。哪天那個 301 不保留 query，登入就掛 `invalid OAuth state`。這是上面「`BASE_URL` 改成 apex」那項要修的。

---

## Must Have

- [x] CRUD、progressive load
- [x] wikilink：embed、stub、同名處理（per-author `UNIQUE(author_id, slug_ci)` + `@user` 語法，全域撞名不可能發生）
- [x] **連結一律走 UUID，不走名字**。`links.resolved_target_id` 是唯一真實來源，改名不斷鏈；`link_regression_test.go` 守著。順手抓到真 bug：編輯器路徑（autosave → publish）漏了 stub backfill，`/write` 跟 import 有做，`autosaveNote` 現在也補上了
- [ ] 搜尋
  - [x] Cmd+K 模糊標題（不是 Ctrl+O，避開瀏覽器熱鍵）
  - [ ] Ctrl+F 內文搜尋 + `line()` / `tag()` / `section()` 運算子 — 運算子語意未定案，先確認規格
- [ ] **Meta App** — 同一份資料多種呈現，有 Obsidian Search / GraphView 等級的查詢力
  - [x] list / grid / masonry、global + local graph（單擊即跳）、calendar、RSVP 快速閱讀
  - [ ] **2-hop graph** — 目前 local graph 只有直接的 inlink/outlink（一跳）。兩跳才看得到「朋友的朋友」那層結構，也是 dora mode 真正需要的資料。`/api/graph` 已經是單一 endpoint 吃篩選，加一個 `?hops=` 比開新 handler 合理
  - [ ] **timeline（linear）— 卡上線**。橫的還直的？
  - [ ] **dora mode — 卡上線**。star-graph，聚光燈打在當前節點，可換焦點
  - [ ] canvas（靜態可拖曳的便利貼牆，不要 graph 那種會抖的）、kanban — backlog
- [x] Tag page 文字雲（可關），依使用次數 — 是**相關標籤**雲（與當前標籤同篇出現的），不是全站標籤。全站雲會隨標籤語彙無上限成長，而且那個聚合本來就是最慢的查詢（見下方 benchmark）；「跟這個標籤一起出現的是什麼」也才是有用的問題，點下去就進入那個標籤自己的雲。範圍外的標籤走標籤搜尋
- [x] design 加 Small Caps — 用 Alegreya SC（獨立的 small-caps 字族，小寫碼位畫的是真的小型大寫，cap height 的 78%），不是 `font-variant-caps`：思源宋體沒有 `smcp`（fontTools 驗過），瀏覽器只會拿大寫縮放合成，正是 Butterick 說不要的假貨。走 jsDelivr（`docs/DECISIONS.md` 2），Latin-only `unicode-range`，只用在作者 handle 一種角色
- [x] 資料模型 / 後端架構 — Postgres（UUID PK、link 表用 ID 解析）、單一 Go binary on Render，serverless 否決。理由見 `docs/DECISIONS.md` 3 與 6
- [ ] 逆向 Obsidian 的殺手功能 — obsidian-flavored markdown 做了，其餘翻 [Developer Documentation](https://docs.obsidian.md/Home)
  - [vscode-markdown-preview-enhanced](https://github.com/shd101wyy/vscode-markdown-preview-enhanced) 實作了很相近的一套功能，比較簡單但夠穩，可以直接讀它怎麼做（[DeepWiki](https://deepwiki.com/shd101wyy/vscode-markdown-preview-enhanced)）
  - Obsidian 的 `app.js` 沒混淆，反編譯就看得到——真的要對齊行為時的最後手段
- [x] 匯出 — .md / .zip 下載、複製到剪貼簿、`obsidian://` 一鍵開啟
- [x] 上傳與編輯 — 空白起手、從 markdown 起手、Medium 風格編輯器
- [ ] 批次匯入
  - [x] 內部連結跨批次保留 — `saveNote` 在目標筆記一出現就 backfill stub link，順序無所謂
  - [ ] 匯入時撰寫 `[[@user/note]]` 跨 vault 連結 — 還沒有這回事，跟「保留」是兩件事
- [ ] 權限
  - **MVP 全公開**，只有草稿 vs 已發布，作者本人才能改刪
  - private（僅自己）與 unlisted（有連結才看得到、不進 feed/search/graph）留到上線後。見 `docs/DECISIONS.md` 7
- [x] 管理後台 — [ ] 還沒實際試用過
- [ ] 付費牆
  - **Stripe out**：沒有美國公司。不卡上線，先免費跑，等有人表現出付費意願再做——瓶頸是需求不是串接
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

**各華語區的名字**（構想，未定案）：台灣「逗陣」、中國「逗知」、港澳「佬逗」、新馬待想、海外華僑維持 **Comma,**。這跟繁簡切換是同一件事的兩面——同一個站按讀者所在地換字形也換名字。真要做的話，站名就不能寫死在模板裡，得跟繁簡三態鈕走同一條路。

## Should Have

- [x] 明暗主題、減少動畫、本地端字體選項（Aa popover：字級 + 宋/黑體）
- [x] 繁簡轉換 — 後端搜尋互通（opencc s2t/t2s 接進 `/search`、wiki-link、tag autocomplete）＋ 前端 原→简→繁 三態鈕
- [ ] 字體 — 思源宋體已上（見下方 CJK 交付）；黑體待選：[genyo-font](https://github.com/ButTaiwan/genyo-font)、思源或源流黑體
- [ ] **tag picker 剩下的**：選現成標籤要是阻力最小的路，開新標籤必須是刻意、視覺上分開的最後一步，不能是在自由輸入框按 Enter。目標是擋掉 X/FB 那種把標籤當強調色用的濫用，但不禁止開新標籤。**還沒做**——現在 Enter 就能塞任意自由文字，完全沒有「現有 vs 新建」的區別
  - [x] 依使用次數排序並顯示計數（編輯器 `#` popup 與 feed 標籤搜尋共用 `GetTagSuggest`）
- [ ] 一鍵鏡像 — 已經在經營 blog 的人怎麼同步過來？普通 blog 與密集連結的 hypertext blog 又是兩種做法
  - 抄 [obsidian-clipper](https://github.com/obsidianmd/obsidian-clipper)：開源，抓網頁轉乾淨 markdown 的部分正是「從既有 blog 匯入」缺的那塊，不用自己寫爬蟲跟正文抽取
- [ ] 維持個人 vault 內部結構之外，為什麼該跟 Comma 上的人互動？
  - THE WHOLE PREMISE AND VALUE PROPOSITION IS COMMUNITY. COMMA: THINK TOGETHER WITH US.

## Could Have

- [x] 圖片支援（1 張/篇，bytea）
  - [ ] 放寬到 1–10 張：成本仍是每篇 O(1)（固定上限，不隨內容成長）；schema 用 `note_images` 表加 CHECK/trigger 限 10 列
- [ ] Library page：優質公有領域文本，像 Project Gutenberg

## Won't Have

影片（含短影音）、PDF、AI 整合。

---

## Tech Stack

架構決策的來龍去脈在 `docs/DECISIONS.md`，這裡只記還沒做的。

- **Server**：Go `net/http` + `html/template` + `go:embed`，單一靜態 binary，不加 build 步驟。Postgres via `pgx/v5`，FTS 用 `tsvector`+GIN
- **GraphQL / GraphDB：不用，問題不成立。** GraphQL 解的是「JSON client 過度或不足抓取」，我們的 handler 回的是 HTML fragment，沒有那個 client。GraphDB 解的是深度不定的遍歷，我們的圖查詢是有界的（一跳，之後兩跳），`links` 表加索引就夠——benchmark 裡 backlinks 走 bitmap scan 本來就快。真的要無界遍歷再回來看，別為了名字裡有 graph 就換資料庫
- **htmx**：vendored + 手寫 `hx-*`。已用原生 `HX-Request` header（不是 `?partial=1`）。OOB swap、`HX-Trigger`、`hx-indicator`、`hx-boost` 有需要再逐個接，別預先鋪
- **Alpine.js**：要接，vendored 無 build。先接 feed 版型記憶，再接 wiki autocomplete 的 popup 狀態（`cmeditor.js` 那個手刻狀態機最爛）。EasyMDE 跟 graph canvas 不要碰，Alpine 幫不上忙
- **templ**：跟 Meta-App view substrate 重構**一起**做，見最上面的「下一步」。不要先用 stdlib 重構一次再遷 templ，那是同樣 18 個模板遷兩次

### 待還的技術債

- [ ] `notes.go` 800 行（CRUD + link resolution + backlinks + stats）拆成 `notes.go` / `links.go` / `notestats.go`。「主要邏輯在哪個檔案」今天沒有答案就是因為這個
- [ ] `feedItem` / `profileNote` / `searchHit` 三個卡片型別 + 7 個 scan loop 收斂成一個 `feedCard` — 「下一步」的第 4 步
- [x] ~~`map[string]any` 換成 typed struct~~ — templ 直接給了型別安全的元件，這項被「下一步」吸收掉了

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
- [ ] Cloudflare 全家桶（未評估，仍開放）
- [ ] DDoS
- [ ] 並發：寫入佇列？讀取？

### Benchmark（2026-06-20，本機 `benchmark` DB，100 users × 1000 notes = 100K notes / 300K note_tags / 100K resolved links）

單一查詢、無並發，看相對值不是絕對延遲。

| 查詢 | @100K | 判讀 |
| --- | --- | --- |
| Feed（`idx_notes_feed`） | **0.07 ms** | 跟 200 列一樣快——partial index 掃到 LIMIT 就停，不會排序 90K。撐得住預估規模 |
| **Tag chips**（`loadTopTagChips`，**每次 feed 都跑**） | **~40 ms** | **唯一真瓶頸**。整個 note_tags⋈notes join 做 HashAggregate，隨表成長 |
| Tag page | ~21 ms | 最慢的頁，可接受，盯著 |
| Backlinks（`idx_links_resolved`） | 快 | bitmap scan，沒問題 |
| FTS | n/a | 測試無效（每篇 seed 內文都命中），要換多樣語料重測 |

- [ ] **Tag chips 的修法（等它真的痛再做，40 ms 現在無所謂）**：它變得很慢，所以快取（Render Key Value 或 in-process TTL map），或乾脆移出 feed 的同步渲染路徑
- [ ] **並發沒測過** — 單查詢 SQL ≠ 並發負載。要用 k6 / vegeta 打真的 binary，看連線池飽和與寫入爭用

DB 們：prod（Render）、`commaplace`（dev）、`benchmark`（壓測）、`commaplace_test`（測試）。

## Some Concerns

- [ ] **上線前把 `001_init.sql`…`009_*.sql` 壓成一個檔**。趁 DB 還能隨便丟的時候做，有真用戶就沒這個自由了（`docs/DECISIONS.md` 3 的 "Revisit when"）
- [x] 讚與收藏分開 — `saves` 表（migration 006）。**`/me/saved` 從空的開始**，006 只建表沒搬資料。要帶的話：`INSERT INTO saves SELECT user_id, note_id, created_at FROM likes ON CONFLICT DO NOTHING;`
  - [ ] 多清單 / playlist 管理 — 目前只有單一收藏清單
- [x] 連結分區 — outbound（這篇連到誰）vs inbound（誰引用這篇），各自再分「自己的 vault」vs「其他人」
- [x] 改 `@handle` — 連結靠 UUID 不斷鏈
- [x] 沒有資料夾，用 tag + 連結組織（`folder_path` 欄位已整個刪掉）
- [x] stub 連結、快速回覆、feed 卡片正確渲染 markdown、頭像產生器
- [x] autosave 堆積空草稿 — 根因是 `/write` 每次載入都插一列空草稿，而 `sweepOrphanDrafts` 只在下次有人訪問 `/write` 時順手清（7 天門檻），沒有排程。沒改清理節奏（範圍外），改成給手動出口：個人頁草稿分頁可多選批次刪除
- [x] publish guard 誤擋有標題的筆記 — 兩個真 bug：(1) autosave 失敗（例如 slug 撞名）不會擋下發布點擊，server 端還握著舊的空標題；(2) 純 emoji/標點的標題讓 `kebabSlug` 回傳空字串，slug 就永遠卡在 `draft-*`，而 publish guard 正是擋這個
- [x] 個人頁不再印自己的 email（本來就只有本人看得到，但也沒理由印）
