# Plan

## Next Step
- [x] review and merge changes from `branch/killer`
- [ ] 測試 Google OAuth 登入功能 
	- [ ] OAuth2.0 要可以提醒使用者他用什麼管道/平台註冊的 (e.g. identify by same email)
	- [ ] 用 magic link 登入又用同一支 gmail 登入的話要歸戶給同一個人
- [ ] test this so called "Magic Link" feature and ensure that SMTP actually works and sends mail.
  - stopgap in place: `PLAYTEST_LOGIN_KEY` env var unlocks `/_dev/login?as=<handle>&key=<key>` on a deployed instance without `DEBUG`, so testers can log in before real SMTP is wired up
- [ ] need random / suprise-me / I'm feeling lucky button or page

- [x] minimal deployment to Render — **LIVE** at https://commaplace.onrender.com
  - Dockerfile builds the Go binary, `docker-compose.yml` for local Postgres, `render.yaml` Blueprint, env vars in `.env.example`/README. Deployed.
- [ ] CI/CD pipeline (not yet established — currently manual)
  - Render DOES auto-deploy on push to the connected branch (Vercel-style): enable Auto-Deploy on the service, push to `main` → Render rebuilds + ships.
  - But Render has **no built-in test gate** like Vercel checks. Render's "Pre-Deploy Command" runs *after* build, *before* traffic-switch — usable for migrations, weak as a test gate (a failure there blocks the deploy but burns a build). Idiomatic split: **GitHub Actions runs `go build` + `go test` on PR/push (the gate); Render auto-deploys on merge to `main` (the deploy).**
  - TODO: add `.github/workflows/ci.yml` (go test) + turn on Render Auto-Deploy.

> Navigation, Exploration, Interaction.

## Must Have

- [x] CRUD — 筆記的基本增刪改查。
	- [x] progressive load (not pagination) of data
- [ ] wikilink caveats
  - [x] embed
  - [ ] empty links?
  - [ ] duplicated note names?
- [/] linking, tagging, mentioning, referencing anything must be through uuid, not the entity name itself.
  - **Rationale:** robustness. Natural-language names and slugs (👎) are not a reliable basis for linkage; UUID is the only sound approach for dynamic hypertext. Authoring is still by name (`[[@user/note]]`), but resolution stores a UUID edge (`links.resolved_target_id`) — that edge is the source of truth.
  - **Status:** believed already satisfied by the Postgres rebuild. **Task = guard against regression**, not new work: confirm `recomputeLinks` stores `resolved_target_id`, and that backlinks/graph/rendering read the UUID edge, not handle+slug, so renames never break links.
- [ ] address all of these: [[# some concerns]]
- [ ] 搜尋 — 精確比對、模糊搜尋（仿 Obsidian Ctrl+O）、向量語意搜尋（候選 [sqlite-vector](https://github.com/sqliteai/sqlite-vector)、[pgvector](https://github.com/pgvector/pgvector)）。
  - [x] ctrl + O search title — shipped as Cmd+K fuzzy note-title palette (`c0dc24e`), not Ctrl+O, precisely to dodge the browser-hotkey conflict below
  - [ ] ctrl + F search body, and those operators: line(), tag(), section()…
  - [x] or different keymaps to avoid conflict with browser hotkeys
- [ ] Meta App — 同一份資料多種呈現，並有類似 Obsidian Search & GraphView 的查詢力。
	- **Launch-gating views: timeline + dora mode.** Everything else below is post-launch backlog (calendar + embed have since shipped to main — not part of this gate).
	- [x] list / grid / masonry (wall)
	- [/] graph (sorta) → note should be like cards
		- [x] graph 不要「點兩下」
    - [x] global graph
		- [x] local graph
	- [ ] **timeline (linear) — LAUNCH-GATING**
    - [ ] horizontal or vertical? 
	- [ ] **dora mode — LAUNCH-GATING.** wiki exploring but better (star-graph, spotlight on current focus node, switch focus)
	- [ ] canvas (like sticky notes on a bulletin board or whiteboard) — backlog
    - [ ] like graph view but not shaky and dynamic, only static draggables
	  - [ ] kanban? — backlog
	- [ ] [[RSVP reader]] — backlog
	- [x] calendar (date view) — shipped to main (`4868ecc`)
	- [ ] ~~tree (?)~~ https://pbellon.github.io/tractatus-tree/#/
- [ ] Tag page 文字雲功能 (on/off of course), based on how many times the tag is used
- [ ] add Small Caps to our design
- [x] 好的資料模型 — 已改用 Postgres（UUID PK、link 表用 ID 解析），見 `.claude/postgres-railway-rebuild-spec.md`；GraphDB vs SQL、是否走 GraphQL 待評估。
	- [x] ~~postgres > 100 users 再考慮~~
- [x] new backend architecture: postgres DB service 已換完，serverless worker 評估後否決（單一 Go binary on Render，理由見 spec 的 Design decisions），`flyio` sucks 🛫 2026-06-16 📅 2026-06-20
- [ ] 逆向 Obsidian 的殺手功能 — 
  - [x] 例如 [obsidian-flavored markdown](https://obsidian.md/help/syntax)
  - [ ] others… check [Home - Developer Documentation](https://docs.obsidian.md/Home)
- [ ] 單篇筆記與整個 vault 的一鍵匯出 — 
  - [x] simple download .md, .zip
  - [x] copy-to-clipboard
  - [x] specialized direct import to obsidian: 參考 [Obsidian URI](https://obsidian.md/help/uri)。— shipped `obsidian://` open-in-Obsidian link (`c3af349` + 2 encoding fixes)
- [x] 簡單的上傳與編輯。
  - [x] empty slate
  - [x] or start from a markdown
  - [x] better writing UX see [[editor-medium-style-spec.md]] 
- [ ] it is not obvious yet how to send a bunch of markdowns to keep the local internal links of a users vault, and start adding external links to other users' online notes. 
- [/] 權限與授權管控 (permissions/authz)
  - **MVP = all-public.** Only axis is draft vs published; everything published is world-readable. No private notes at launch. Author-only edit/delete stays.
  - **Not blockers, keep on the radar:** *private* (vault-only) notes and *unlisted* (link-only, hidden from feed/search/graph) tiers — planned, post-launch.
- [x] 管理後台（SQLite or postgres 都不附，要自己做）。
  - [ ] 需要實際試用看看
- [ ] 付費牆管理 (payment handling)
  - **Stripe is out:** no US company + geopolitical friction blocks direct Stripe.
  - **Does this gate launch? No.** Monetization gates revenue, not release. Launch free, validate the cross-vault rabbit-hole thesis with real users, add payments once there's demonstrated willingness to pay. The real blocker is demand, not the integration. (Don't build the paywall before there's something worth paying for.)
  - **When needed → Merchant-of-Record (MoR).** The MoR is the legal seller of record; they handle entity, global tax/VAT, and pay out to you — no US presence required. This is the standard "no US company" answer.
    - Candidates: **Paddle**, **Lemon Squeezy**, **Gumroad** (all MoR, indie-friendly).
    - Taiwan-local alternative (needs a TW business entity): **ECPay (綠界)**, **NewebPay (藍新)**, **TapPay** — native TW methods (信用卡/ATM/超商).
    - ⚠️ VERIFY Taiwan **seller/payout eligibility** on each platform's supported-countries page before committing — this changes and is unverified.
  - **DECISION: launch free + lightweight tips/donations now; full MoR paywall later.** Tips double as the willingness-to-pay signal that tells us *when* to build the real paywall.
  - **Tips/donations vendor (no company needed):** **Ko-fi** or **Buy Me a Coffee** — both take one-time tips, route through their own PayPal/Stripe, payout to a TW bank, zero/low platform cut, just embed a link/button. Recommended over a raw PayPal.me button because [Unverified] Taiwan PayPal accounts have historically had receiving/withdrawal restrictions — verify before relying on bare PayPal.
  - Full paywall (later) = Merchant-of-Record, see candidates above.
  - paid features and incentives: 
    - can have private/unlisted notes (drafts are only kept 3~7 days)
    - can have more images
    - can recurse deeper into nested embeds
    - can have "unlimited" playlist/collections, instead of just two, "likes" & "later"
- [ ] Dev workflow & engineering best-practices
  - [x] KEEP CLAUDE.MD LEAN
  - [x] explore -> plan -> run 
  - [x] start using skills/commands
  - [x] use the Harness, build validation hooks (deterministic behavior over probabilistic tuning)
  - [ ] TDD: define clear, concrete deliverables; give clear validation criteria
  - [ ] `/goal` also cool
- [ ] `/random` page take people to a random node
- [ ] share button, webshare api, open graph, …
- [ ] 面向華語用戶，所以中文 UI/UX 要做好
- [ ] 服務條款、隱私政策、…


## Tech Stack & Frontend Direction

Decision record (architecture review 2026-06-20, 3 agents + 4 lib evaluations). Context: 3-founder team — one backend/ponytail, two design/product who *will* push richer UI/UX over the product cycle. So the call is not "least JS now" but "what substrate survives feature #20 without a React rewrite."

### Server / Rendering — Keep As-is
- Go `net/http` (1.22 patterns) + stdlib `html/template` + `//go:embed` → **single static binary, no build tooling.** This is the right shape; not changing it.
- Postgres via `pgx/v5` (README was stale, said SQLite — fixed). FTS = `tsvector`+GIN.

### Htmx — Keep, Adopt the Best Practices We're Missing
- Vendored `htmx.min.js` + hand-written `hx-*` attributes **is** the best practice.
- **Reject** `htmgo` (full framework rewrite off stdlib, max lock-in) and `donseba/go-htmx` (wraps ~10 lines of header reads we already do — negative ROI).
- **Fix the one wheel we reinvented:** replace the `?partial=1` query param with the native `HX-Request` header (`r.Header.Get("HX-Request")`).
- **Adopt incrementally as features need them** (all native, zero deps): OOB swaps (`hx-swap-oob`), `HX-Trigger` response header (decoupled toasts/events), `hx-indicator`+view-transitions (FOUC fix), `hx-boost`.

### Alpine.js — ADOPT (Vendored, no bUild sTep)
- The forward bet for client-side state the design/product founders will want (popovers, multi-step UI, optimistic toggles, persisted prefs via `$persist`).
- htmx for server-driven 90%, Alpine for the stateful 10% — the proven pairing.
- One vendored `alpine.min.js` + `defer` in `_base.html`. Single binary intact.
- **Roll in incrementally, not big-bang:** first the feed layout-restore script, then the wiki-autocomplete popup state (the worst hand-rolled state machine, cmeditor.js). Leave EasyMDE (editor core) and the canvas graph alone — Alpine doesn't help either.

### Templ — DEFER (Adopt lAter, not nOw)
- Type-safe Go templates (JSX-like, compiles `.templ`→`.go`). Genuinely attractive for a JS/TS-primary team and would catch the silent `map[string]any` template-bag typos at compile time.
- **Cost blocks it today:** adds a `templ generate` codegen step → breaks the "no build tooling" property and complicates `go:embed` (embed generated Go, not `.html`). Both Go reviewers said no *at current size* (18 templates).
- **Sequencing rule (important):** do NOT migrate to templ *before* the Meta-App view-substrate refactor — that's a double migration. Refactor in stdlib first, let the shared component boundary (`NoteListView`) stabilize, *then* templ is a mechanical port of one clean component instead of 18 ad-hoc templates.
- **Adopt trigger:** when templates exceed ~25, OR a second founder starts writing templates regularly, OR the view-substrate refactor has landed and we want the shared card components type-checked. Revisit then.

### Cheap Win — Get the Partial Benefit Free now
- Replace the loose `map[string]any` template data bags with typed structs per page — most of templ's type-safety, zero toolchain cost.

### Code-organization Debt (Not a Framework Problem)
- `notes.go` is an 800-line god file (CRUD + link resolution + backlinks + stats). Split into `notes.go` / `links.go` / `notestats.go` — tidy within existing structure. This is why "which file is the main logic" is unanswerable today.
- Card-type duplication (`feedItem`/`profileNote`/`searchHit` + 5 scan loops) → collapse to `feedCard` in the Meta-App view-substrate refactor (see spec).

### Fonts / CJK Delivery — Current + Options on the Table

**Now (shipped):** self-hosted, unicode-range-split Source Han Serif **TC** via `cn-font-split` (668 woff2 chunks + generated `@font-face` CSS in `static/fonts/tc/`). Browser fetches only chunks with on-page glyphs (~3.6MB/dense page, cached) instead of the 19.7MB monolith. Originals kept in `fonts-src/` (out of `go:embed`). Self-hosted = single static binary, no external dep, no Google. **SC not split yet** — re-run cn-font-split when simplified-Chinese ships (`fonts-src/README.md`).

**Why chunking isn't automatic:** the browser can't subset a remote monolithic font; the split must be precomputed by a tool. CDNs (Google/Adobe/Fontsource) do it on their server — that's the convenience. Self-hosting = we own the step. The truly-automatic future is W3C **Incremental Font Transfer (IFT)**, not deployed yet.

**The scaling ceiling:** each extra self-hosted CJK family ≈ +34MB binary, +668 files. One reading serif is fine; a multi-font **picker** (sans + serif + weights + SC variants) would bloat the binary (~155MB / ~2,700 files for 4 families). Per-page egress stays bounded (user loads one font), but repo/binary size doesn't.

**Options when we add more fonts (don't embed family #2 — switch delivery):**
- **jsDelivr + Fontsource** (recommended non-Google) — serves Noto Sans/Serif CJK already unicode-range-chunked. jsDelivr is a neutral open-source CDN (no Google tracking). Zero binary weight, cross-site browser cache, N families ~free.
- **Google Fonts CSS API** — most automatic, best-tuned chunking, but external dep + Google privacy (user finds distasteful). Fallback if jsDelivr insufficient.
- **System sans is already free** — UI chrome uses `-apple-system…sans-serif`; system CJK sans (PingFang / MS YaHei / Noto Sans) needs no download. The *downloaded* font is the reading serif (the differentiator) — we may never need a downloaded CJK sans.
- **Decision rule:** keep the single self-hosted serif now; when the font picker ships (Should-Have, post-launch), move CJK webfonts to jsDelivr/Fontsource rather than embedding a 2nd 34MB family. Embedding family #2 is the line not to cross. Swapping to the CDN is a ~10-min change.

## Dev & Testing

### Claude Code + Browser Debugger Tool

https://code.claude.com/docs/zh-TW/chrome


Log in without email at `/_dev/login?as=alice` (creates user if needed).

### Google OAuth Local Testing

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
- [/] 繁簡轉換。
  - [x] 後端搜尋互通：opencc s2t/t2s，已接入 `/search`、wiki-link 與 tag 的 autocomplete（`searchVariants`/`likeAnyVariant`，見 `internal/handlers/search.go`）。
  - [ ] 前端顯示切換：讀者看到的內容仍是作者原字體，尚未實作 topbar 三態切換（原文/简/繁）— 規劃見 `.claude/frontend-opencc-plan.md`，尚未動工。
  - [x] [GitHub - BYVoid/OpenCC: Library for conversion between Traditional and Simplified Chinese · GitHub](https://github.com/BYVoid/OpenCC)
- [ ] 思源宋體（or 源漾明體、源流明體）
  - [x] https://github.com/adobe-fonts/source-han-serif
  - [ ] https://github.com/ButTaiwan/genyo-font
  - [ ] that's serif, for sans serif go with 思源 or 源流黑體
- [ ] 本地端字體選項 fontsize, serif or sans serif, simple stuff（參考 Zotero local view options or gitbooks, or whatever）。
- [ ] 減少動畫設定 (reduce-motion toggle) — pure frontend, no DB/route needed; split out from the autocomplete/tag-picker plan to keep that scope tight. See design below.

### Reduce Motion — Design (Pure fRontend, no bAckend)

Covers the `.content` `pageFadeIn` CSS keyframe (style.css ~198-210) and the `[data-reveal]` scroll-reveal system (`reveal.js` + style.css ~166-195), including the htmx afterSwap/beforeSwap inline opacity fade in `reveal.js` ~79-90. Single on/off toggle, `localStorage` only — no `users` DB column, no new route/handler. (Original draft mirrored the `users.theme` DB-column pattern; unnecessary here — this preference has no reason to sync across devices, unlike dark/light mode, and a one-time flash of full motion on first load elsewhere is low-stakes.)

- **`style.css`** (append near the existing `pageFadeIn`/`[data-reveal]` rules, ~line 210):

  ```css
  @media (prefers-reduced-motion: reduce) {
    .content { animation: none; }
    [data-reveal] { transition-duration: 0.001ms; }
  }
  html[data-motion="reduced"] .content {
    animation: none;
    transition-duration: 0.001ms !important; /* beats reveal.js's inline
      transition set on every htmx swap (content.style.transition = "opacity
      0.15s ease") — without !important the htmx page-fade ignores this toggle */
  }
  html[data-motion="reduced"] [data-reveal] {
    transition-duration: 0.001ms; /* near-zero, not none/0 — reveal.js listens
      for transitionend on clip-path to clean up inline styles after the
      reveal; a transition that never actually transitions never fires it */
  }
  ```

  `.content`'s keyframe animation has no completion listener, so `animation: none` is fine there; `[data-reveal]`'s transition does have one (the clip-path cleanup in `reveal.js`), so it needs a non-zero-but-tiny duration instead, or the cleanup never fires and leaves elements clipped.

- **`_base.html`**: inline script near the top (same place the existing theme FOUC-avoidance IIFE lives):
  ```js
  (function () {
    var v = null;
    try { v = localStorage.getItem("motion"); } catch (e) {}
    if (v === "reduced") document.documentElement.setAttribute("data-motion", "reduced");
  })();
  ```
  Add a checkbox reachable from the nav (simplest: next to `#theme-toggle`, or a small `<details>` popover — no new page/route needed). On change:
  ```js
  var reduced = checkbox.checked;
  var html = document.documentElement;
  if (reduced) html.setAttribute("data-motion", "reduced");
  else html.removeAttribute("data-motion");
  try { localStorage.setItem("motion", reduced ? "reduced" : "normal"); } catch (e) {}
  ```

Verification: DevTools Rendering tab → emulate `prefers-reduced-motion: reduce` with no explicit toggle set → page fade + feed reveal instant, no stuck-clipped elements, no console errors. Check the toggle → animations off immediately, persists across reload (attribute re-applied pre-paint by the inline script). htmx nav (feed → note) → `.content` opacity flip instant, not a visible 0.15s fade. Uncheck → animations return.
- [x] tag merging issue? 應該多用大家在用的 tag 吧 (based on number of usages of that tag, show that when picking tag, easy) — `#` autocomplete (editor) 與 feed 的標籤搜尋都已改成依使用次數排序並顯示計數
  - A3. 標籤 chips
  - [x] 有小計數（e.g. 「#音樂 12」）— `GetTagSuggest` 現在回傳 `#tag  N`
  - [x] tag picker UX: autocomplete sorted by usage count desc — done (both editor `#` popup and feed tag-search picker, shared `GetTagSuggest` endpoint)
  - [ ] tag picker UX (remaining): picking an existing tag should be the path of least resistance; creating a brand-new tag must be a deliberate, visually separate last step (not just hitting enter on free text) — goal is to stop X/FB-style tag spam (emphasis/color-coding instead of categorization) without banning new tags outright. Not implemented — current autocomplete lets Enter insert arbitrary free-typed text with no existing/new distinction.
- [ ] mirroring should be easy
  - [ ] 已有經營 blog 的人如何一鍵同步過來？
  - [ ] 又分 normal blog vs. densely linked hypertext blog
- [ ] 維持個人 vault 內部結構之外，為什麼應該跟 Comma 上的人互動？ Because connection with others is the whole point?

## Could Have

- [x] 圖片支援 — note image upload (1/note, bytea, dedicated route, same pattern as avatar PNG)。
  - [ ] consider raising limit to 1–10 images/note: cost stays O(1) per note (constant cap, not O(n) content), complexity stays manageable; schema option: `note_images` table with CHECK/trigger enforcing max 10 rows per note_id.
- [ ] Library page: 優質公有領域文本，like Project Gutenberg

## Won't Have

- 影片（含短影音）。
- PDF。
- AI 整合。

## Scaling Issues

- [x] ~~現況 — SQLite 單檔當 DB，需要一台 24/7 不掉資料的專屬機（用 [Fly.io](https://fly.io/)）。~~ 已換 Postgres，這項已成過去式。
- [x] 計畫 — DB 換 hosted Postgres，但用 Render 不是 Neon；Go server 維持單一 binary 部署在 Render，不走 Vercel serverless（理由見 `.claude/postgres-railway-rebuild-spec.md` Design decisions）。
  - [ ] or perhaps cloudflare 全家桶（未評估，仍開放）
- [ ] DDoS issues
- [ ] concurrent users issue
  - [ ] writing queue?
  - [ ] reading?

### Benchmark Findings (2026-06-20, Local `benchmark` DB, 100 Users × 1000 Notes = 100K Notes / 300K note_tags / 100K Resolved Links)

Method: seeded a dedicated `benchmark` Postgres DB (separate from dev/test/prod), `EXPLAIN ANALYZE` on the hot query paths. Numbers are local single-query (no concurrency); treat as relative, not absolute prod latency.

| Query                                                                | @100K       | Verdict                                                                                                                  |
| -------------------------------------------------------------------- | ----------- | ------------------------------------------------------------------------------------------------------------------------ |
| Feed (recommended, `idx_notes_feed`)                                 | **0.07 ms** | Flat vs 200 rows — partial index does index-scan-stop-at-LIMIT, never sorts 90K. Index **vindicated** at forecast scale. |
| **Tag-chips aggregate** (`loadTopTagChips`, runs on EVERY feed load) | **~40 ms**  | 🔴 **Only real bottleneck.** HashAggregate over full note_tags⋈notes join. Grows with table.                             |
| Tag page (sort after join)                                           | ~21 ms      | 🟡 Slowest page, tolerable, watch.                                                                                       |
| Backlinks (`idx_links_resolved`)                                     | fast        | Bitmap scan, fine.                                                                                                       |
| FTS                                                                  | n/a         | Test invalid (every seeded body matched the term); re-test with varied corpus.                                           |

- **Conclusion:** feed architecture holds at the 100K forecast. The index/N+1 changes from the arch review are unmeasurable at current 200-row scale but correct forward — feed stays O(LIMIT) not O(N).
- [ ] **Tag-chips fix (when it bites, not now — 40 ms is fine today):** it changes slowly, so cache it (Render Key Value / in-process TTL map) or drop it off the synchronous feed render. Don't optimize until it's on the measured hot path for real traffic. See "speed up tags" options below.
- [ ] **Concurrency untested** — single-query SQL ≠ concurrent load. Needs an HTTP load tool (k6 / vegeta) against the running binary: connection-pool saturation, write contention. Separate exercise.
- Reproduce: `benchmark` DB lives in the local docker postgres; reseed script in session notes. DBs: prod (Render), `commaplace` (dev), `benchmark` (load), `commaplace_test` (tests).

# Some Concerns (Iterative)

- [ ] migrations: in early dev stage it's fine to squash and reset the DB periodically; keep the schema clean, not precious
	- [x] what does migration means? we can afford to drop the db anytime now, why are we accumulating techdebt now already? — resolved: no production data existed, so the Postgres rebuild shipped as a clean rip-and-replace with no migration/rollback tooling (see `.claude/postgres-railway-rebuild-spec.md`).
- [ ] liked and saved should be separated
  - [ ] e.g. don't like a post but want to save for later, or like a post but don't want to visit later.
  - [ ] 收藏清單, playlist 管理, *cf.* Spotify's feels clunky, Youtube's alright, 小紅書 might be good
- [ ] distinctions
  - [ ] inbound/outbound links
  - [ ] linked by self or by others
- [ ] can users change their `@handle`? 
	- uuid should handle all the linkage already, so probably it'll be fine to permit changes
- [x] maybe no "folders"? how to organize notes of a user? collection via tags and just pure linking from notes? why bother with folders? — resolved: folders removed from product, `folder_path` column dropped entirely in the Postgres rebuild.
- [x] need to handle empty links (stubs) like wikipedia or obsidian does. 
- [x] need quick reply to others note
- [x] feed page card view doesn't render markdown correctly. all returned HTML should not contain un-rendered markdown, except for the editing "textarea" of writing pages/sections
- [x] user avatar image: use dicebear or Hank's NFT-like Weedie.
