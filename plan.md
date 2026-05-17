# Plan

兩個待規劃的新功能：

- [x] Google OAuth 登入。
- [ ] 檔案匯入（從本機或其他來源批次上傳筆記）。

範疇用 MoSCoW 分級。

## Must Have

- [x] CRUD — 筆記的基本增刪改查。
  - [x] progressive load (not pagination) of data
- [ ] 搜尋 — 精確比對、模糊搜尋（仿 Obsidian Ctrl+O）、向量語意搜尋（候選 [sqlite-vector](https://github.com/sqliteai/sqlite-vector)、[pgvector](https://github.com/pgvector/pgvector)）。
- [ ] Meta App — 同一份資料多種呈現，並有類似 Obsidian Search & GraphView 的查詢力。
  - [x] graph
  - [ ] timeline
  - [ ] tree
- [ ] 好的資料模型 — 現用 SQLite、未來改 Postgres；GraphDB vs SQL、是否走 GraphQL 待評估。
- [ ] 逆向 Obsidian 的殺手功能 — 
  - [x] 例如 [obsidian-flavored markdown](https://obsidian.md/help/syntax)
  - [ ] others… check [Home - Developer Documentation](https://docs.obsidian.md/Home)
- [ ] 單篇筆記與整個 vault 的一鍵匯出 — 
  - [x] simple download .md, .zip
  - [x] copy-to-clipboard
  - [ ] specialized direct import to obsidian: 參考 [Obsidian URI](https://obsidian.md/help/uri)。
- [x] 簡單的上傳與編輯。
  - [x] empty slate
  - [ ] or start from a markdown
  - [ ] it is not obvious yet how to send a bunch of markdowns to keep the local internal links of a users vault, and start adding external links to other users' online notes. 
- [ ] 權限與授權管控。
- [x] 管理後台（SQLite 不附，要自己做）。
- [ ] 付費牆管理。
  - [ ] stripe or something?

## Dev & Testing

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

- [ ] 繁簡轉換。
  - [ ] [GitHub - BYVoid/OpenCC: Library for conversion between Traditional and Simplified Chinese · GitHub](https://github.com/BYVoid/OpenCC)
- [ ] 思源宋體（or 源漾明體、源流明體）
  - [ ] https://github.com/adobe-fonts/source-han-serif
  - [ ] https://github.com/ButTaiwan/genyo-font
- [x] 明暗主題（已完成）。
- [ ] 本地端字體選項 fontsize, serif or sans serif, simple stuff（參考 Zotero local view options or gitbooks, or whatever）。

## Good to Have

- [ ] 圖片支援（待定）。

## Won't Have

- 影片（含短影音）。
- PDF。
- AI 整合。

## Scaling

- [ x ] 現況 — SQLite 單檔當 DB，需要一台 24/7 不掉資料的專屬機（用 [Fly.io](https://fly.io/)）。
- [ ] 計畫 — DB 換 hosted Postgres（[Neon](https://neon.com/)），Go server 改用 Vercel 等 serverless 平台處理 request、query、HTML render。
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
- [ ] need quick reply to others note

