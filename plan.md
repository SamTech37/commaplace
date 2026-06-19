# Plan

## Next Step
- [ ] 測試 Google OAuth 登入功能 
	- [ ] OAuth2.0 要可以提醒使用者他用什麼管道/平台註冊的 (e.g. identify by same email)
	- [ ] 用 magic link 登入又用同一支 gmail 登入的話要歸戶給同一個人
- [ ] review and merge changes from `branch/killer`
- [ ] test this so called "Magic Link" feature and ensure that SMTP actually works and sends mail.
  - stopgap in place: `PLAYTEST_LOGIN_KEY` env var unlocks `/_dev/login?as=<handle>&key=<key>` on a deployed instance without `DEBUG`, so testers can log in before real SMTP is wired up
- [ ] minimal deployment to Railway TONIGHT
  - prep done: Dockerfile builds the Postgres binary as-is, `docker-compose.yml` for local Postgres, env vars documented in `.env.example`/README — not yet actually deployed

> Navigation, Exploration, Interaction.

## Must Have

- [x] CRUD — 筆記的基本增刪改查。
	- [x] progressive load (not pagination) of data
- [ ] wikilink caveats
  - [ ] embed
  - [ ] empty links?
  - [ ] duplicated note names?
- [ ] linking, tagging, mentioning, referencing anything must be through uuid, not the entity name itself.
- [ ] address all of these: [[# some concerns]]
- [ ] 搜尋 — 精確比對、模糊搜尋（仿 Obsidian Ctrl+O）、向量語意搜尋（候選 [sqlite-vector](https://github.com/sqliteai/sqlite-vector)、[pgvector](https://github.com/pgvector/pgvector)）。
  - [ ] ctrl + O search title
  - [ ] ctrl + F search body, and those operators: line(), tag(), section()...
  - [ ] or different keymaps to avoid conflict with browser hotkeys
- [ ] Meta App — 同一份資料多種呈現，並有類似 Obsidian Search & GraphView 的查詢力。
	- [x] masonry (wall)
	- [/] graph (sorta) → note should be like cards
		- [ ] graph 不要「點兩下」
    - [x] global graph
		- [x] local graph
	- [ ] timeline (linear)
    - [ ] horizontal or vertical? 
	- [ ] canvas (like sticky notes on a bulletin board or whiteboard)
    - [ ] like graph view but not shaky and dynamic, only static draggables
	  - [ ] kanban? 
	- [ ] **dora mode.** wiki exploring but better (star-graph, spotlight on current focus node, switch focus)
	- [ ] [[RSVP reader]]
	- [ ] calendar (date view)
	- [ ] ~~tree (?)~~ https://pbellon.github.io/tractatus-tree/#/
- [x] 好的資料模型 — 已改用 Postgres（UUID PK、link 表用 ID 解析），見 `.claude/postgres-railway-rebuild-spec.md`；GraphDB vs SQL、是否走 GraphQL 待評估。
	- [x] ~~postgres > 100 users 再考慮~~
- [x] new backend architecture: postgres DB service 已換完，serverless worker 評估後否決（單一 Go binary on Railway，理由見 spec 的 Design decisions），`flyio` sucks 🛫 2026-06-16 📅 2026-06-20
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
  - [ ] better writing UX see [[editor-medium-style-spec.md]] 
  - [ ] it is not obvious yet how to send a bunch of markdowns to keep the local internal links of a users vault, and start adding external links to other users' online notes. 
- [ ] 權限與授權管控。
- [x] 管理後台（SQLite or postgres 都不附，要自己做）。
- [ ] 付費牆管理。
  - [ ] stripe or something?
- [ ] Dev workflow & engineering best-practices
  - [x] KEEP CLAUDE.MD LEAN
  - [x] explore -> plan -> run 
  - [x] start using skills/commands
  - [ ] use the Harness, build validation hooks (deterministic behavior over probabilistic tuning)
  - [ ] TDD: define clear, concrete deliverables; give clear validation criteria
  - [ ] `/goal` also cool
- [ ] `/random` take people to a random node
- [ ] share button, webshare api...


## Dev & Testing

### Claude Code + Browser Debugger Tool

https://code.claude.com/docs/zh-TW/chrome

### Running Locally

```bash
make dev          # DEBUG=1, demo seed, dev login enabled
make dev-full     # above + SEED_DEV=1 (multi-user fake data)
```

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
- [ ] 繁簡轉換。
  - [ ] [GitHub - BYVoid/OpenCC: Library for conversion between Traditional and Simplified Chinese · GitHub](https://github.com/BYVoid/OpenCC)
- [ ] 思源宋體（or 源漾明體、源流明體）
  - [x] https://github.com/adobe-fonts/source-han-serif
  - [ ] https://github.com/ButTaiwan/genyo-font
  - [ ] that's serif, for sans serif go with 思源 or 源流黑體
- [ ] 本地端字體選項 fontsize, serif or sans serif, simple stuff（參考 Zotero local view options or gitbooks, or whatever）。
- [ ] tag merging issue? 應該多用大家在用的 tag 吧 (based on number of usages of that tag, show that when picking tag, easy)
  - A3. 標籤 chips
  - 有小計數（e.g. 「#音樂 12」）
  - [ ] tag picker UX (deferred from postgres schema design session): autocomplete sorted by usage count desc; picking an existing tag should be the path of least resistance; creating a brand-new tag must be a deliberate, visually separate last step (not just hitting enter on free text) — goal is to stop X/FB-style tag spam (emphasis/color-coding instead of categorization) without banning new tags outright
- [ ] mirroring should be easy
  - [ ] 已有經營 blog 的人如何一鍵同步過來？
  - [ ] 又分 normal blog vs. densely linked hypertext  blog
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
- [x] 計畫 — DB 換 hosted Postgres，但用 Railway 不是 Neon；Go server 維持單一 binary 部署在 Railway，不走 Vercel serverless（理由見 `.claude/postgres-railway-rebuild-spec.md` Design decisions）。
  - [ ] or perhaps cloudflare 全家桶（未評估，仍開放）
- [ ] DDoS issues
- [ ] concurrent users issue
  - [ ] writing queue?
  - [ ] reading?

# Some Concerns (iterative)

- [ ] liked and saved should be separated
  - [ ] e.g. don't like a post but want to save for later, or like a post but don't want to visit later.
- [ ] distinctions
  - [ ] inbound/outbount links
  - [ ] linked by self or by others
- [ ] need random / suprise-me / I'm feeling lucky button or page
- [ ] can users change their @handle (ID)? 
- [x] maybe no "folders"? how to organize notes of a user? collection via tags and just pure linking from notes? why bother with folders? — resolved: folders removed from product, `folder_path` column dropped entirely in the Postgres rebuild.
- [x] what does migration means? we can afford to drop the db anytime now, why are we accumulating techdebt now already? — resolved: no production data existed, so the Postgres rebuild shipped as a clean rip-and-replace with no migration/rollback tooling (see `.claude/postgres-railway-rebuild-spec.md`).
- [ ] migrations: in early dev it's fine to squash and reset the DB periodically; keep the schema clean, not precious
- [ ] need to handle empty links (stubs) like wikipedia or obsidian does. 
- [x] need quick reply to others note
- [ ] feed page card view doesn't render markdown correctly. all returned HTML should not contain un-rendered markdown, except for the editing "textarea" of writing pages/sections
- [x] user avatar image: use dicebear or Hank's NFT-like Weedie.

> Note: the single-note page redesign (formerly B1–B7) is now tracked in `.claude/postgres-railway-rebuild-spec.md`.

