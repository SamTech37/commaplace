
# Spec for UI overhaul
Commit 95a9849 (on branch `hank`) — 變更規格
refer to the actual code on that branch then implement those into main (or new feat/ branch)

  1. 個人頁面新增統計資料（profile.go）

  新增兩個 DB 查詢並傳入 template：

  - NoteCount：SELECT COUNT(*) FROM notes WHERE author_id = ? AND hidden_at IS NULL AND deleted_at IS NULL
  - EstYear：SELECT created_at FROM users WHERE id = ?，轉換為年份（time.Unix(createdAt, 0).Year()）

  2. 個人頁面 Header 改版（profile.html）

  舊：
  <div data-reveal>
    <h1 style="margin-bottom: 0.25em">@handle</h1>
  </div>
  <h2 style="...inline styles...">最近的筆記</h2>

  新：
  <header class="vault-masthead" data-reveal>
    <h1 class="vault-name">@handle</h1>
    <div class="vault-meta">Est. {year} · {count} entries</div>
  </header>
  <div class="profile-notes-heading" style="margin-top:1.5em">最近的筆記</div>

  3. Feed 卡片全面改版（feed.html）

  移除 <h1>Feed</h1>。

  container class：masonry → feed-grid（同步修改 feed_partial.html 的 hx-target）

  卡片從 masonry-card variant-{x} 改為統一的 entry：

  ┌────────────────────────────────┬─────────────────────────────────────┐
  │             舊欄位             │               新欄位                │
  ├────────────────────────────────┼─────────────────────────────────────┤
  │ card-title                     │ entry-title                         │
  ├────────────────────────────────┼─────────────────────────────────────┤
  │ card-excerpt                   │ entry-excerpt                       │
  ├────────────────────────────────┼─────────────────────────────────────┤
  │ card-meta（含 like/link 計數） │ entry-meta（僅 author + 相對時間）  │
  ├────────────────────────────────┼─────────────────────────────────────┤
  │ card-tags / card-tag           │ entry-tags / entry-tag              │
  ├────────────────────────────────┼─────────────────────────────────────┤
  │ —                              │ 新增 entry-no（顯示 No.001 流水號） │
  └────────────────────────────────┴─────────────────────────────────────┘

  摘要邏輯：quote / text / list 全部統一渲染成單個 <p class="entry-excerpt">，list 變體取前兩項以 · 連接。

  4. Wiki 跨作者連結渲染改版（render.go）

  舊：統一走一個 switch，輸出 <a class="wiki wiki-cross-resolved">label</a>

  新：分岔為兩個獨立分支：

  跨作者（crossVault）：
  <a href="..." class="wikilink-cross [wiki-cross-unresolved]">
    <span class="arrow">↗</span> @user / slug
  </a>
  - 無 alias 時強制顯示 @user / slug（不再顯示原始 slug）

  同作者：
  <a href="..." class="wiki-resolved | wiki-unresolved">label</a>
  - 移除舊的 wiki base class

  5. Google Fonts（_base.html）

  新增三個 <link> tag，引入 Newsreader + IBM Plex Mono：
  <link rel="preconnect" href="https://fonts.googleapis.com" />
  <link rel="preconnect" href="https://fonts.gstatic.com" crossorigin />
  <link href="https://fonts.googleapis.com/css2?family=Newsreader:ital,opsz,wght@...&family=IBM+Plex+Mono:wght@400;500&d
  isplay=swap" rel="stylesheet" />


  ---
  其餘（style.css 1428 行、graph.js、reveal.js） 為配合上述改動的樣式與動畫重構，無新增邏輯功能。

# additional

and other ui/ux/behavioral issues mentioned in:
@specs/editor-medium-style-spec.md
@specs/postgres-railway-rebuild-spec.md 



● ---
  Handover prompt:

  We ported the hank branch UI overhaul (commit 95a9849) to main. All changes are in local working tree, staged but NOT committed. Here's the state:

  Done — all 9 files modified:
  - internal/handlers/profile.go — NoteCount + EstYear queries (Postgres $1 syntax)
  - internal/handlers/templates/profile.html — vault-masthead header with Est. YYYY · N entries
  - internal/handlers/templates/feed.html — feed-grid layout, entry/entry-* classes, removed <h1>Feed</h1>
  - internal/handlers/templates/feed_partial.html — hx-target updated to .feed-grid
  - internal/handlers/templates/_base.html — Google Fonts (Newsreader + IBM Plex Mono), search placeholder → "search the press..."
  - internal/markdown/render.go — cross-vault wiki links now emit wikilink-cross class with ↗ @user/slug pill display
  - internal/handlers/static/style.css — added .vault-masthead, .feed-grid, .entry* classes; updated cross-vault wiki CSS to pill capsule; changed reveal animation from translateY to
  clip-path horizontal sweep
  - internal/handlers/static/graph.js — nodes redrawn as index cards (rectangles) instead of colored circles; hover highlights connected edges; connectedTo() dimming; mouseleave handler
  - internal/handlers/static/reveal.js — makeObserver() refactor, simplified HTMX handlers

  Static files are go:embed — need recompile to see changes. Run make watch (kills old server, recompiles, hot-reloads).

  Still needs verification (user just restarted server):
  1. /feed — grid layout, No.001 entry numbers, italic serif titles
  2. /alice or any profile — vault masthead "Est. 2026 · N entries"
  3. /graph — nodes should be rectangular index cards, NOT colored circles
  4. Light + dark mode colors throughout (user reported color bugs — not yet diagnosed)
  5. Cross-vault wiki links in notes — should render as black pill with ↗ @user/slug

  Color bug context: hank's CSS uses --ink/--rule/--invert-* variables; main uses --text/--border/--black/--white. The CSS was adapted with those mappings. But color bugs visible in both
  light and dark mode — need Chrome screenshots to diagnose which classes are broken.

  -