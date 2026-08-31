# Decisions — why each rule exists, and when to break it

A running log of the real architecture calls. The point isn't the decision — it's
the **why** and the **when-to-break-it**, so a future reader (us, in six months)
can tell whether a rule still earns its keep or has outlived its reason.

Each entry has the same five lines:
- **Decision** — what we do.
- **Context** — the problem it addresses.
- **Why** — what it buys.
- **Cost / when it's a shackle** — what it makes harder.
- **Revisit when** — the concrete condition under which to break it.

Numbered by record order, not by when the decision was made (2–8 were decided
earlier and written up 2026-06-20). Append new entries at the end.

---

## 1 — No build step in the deploy path (2026-06-20)

**Decision.** The deploy build is `go build` and nothing else: single static
binary, assets via `go:embed`, no `node_modules`, no JS bundler, no per-deploy
code generation. **Offline, one-time tooling that produces _committed_ output is
allowed** (e.g. `cn-font-split` for font chunks) — it runs on a dev machine,
commits its result, and the deploy stays pure `go build`.

**Context.** The team is JS/TS-primary and has lived the frontend build-tool tar
pit: webpack→vite→esbuild churn, config rot, lockfile conflicts, dependency
security holes, node-version drift, "works locally, breaks in the test pipeline."
The rule was originally written bluntly as "no build tooling," which wrongly
implied even offline tools are forbidden — and made the font-chunking work feel
like a violation when it wasn't.

**Why.** Clone + `go build` + run. Deploy ships one binary; Render builds it from a
Dockerfile that's just `go build`. Removes a whole class of broken-build incidents.
This speeds us up in the common case, not down.

**Cost / when it's a shackle.** Capabilities that need a precompute step (templ,
Tailwind, TypeScript, font subsetting) need either committed generated output or a
build step. Treating the rule as gospel blocks them needlessly. The honest test is
**deploy-path coupling**, not "is there a tool":
- Bad: a step that must run on *every* deploy (couples to a second toolchain,
  fragile, slow).
- Fine: a step run *occasionally* on a dev machine whose output is committed (off
  the critical path).
By this test, font chunking never violated the rule; templ wouldn't either if its
generated `.go` is committed (templ's real cost is dev-loop friction +
generated-file noise, not deploy coupling).

**Revisit when.** Something we clearly want (templ; a CSS pipeline) is blocked,
*and* it can commit its output / stay off the deploy path. Then add it on purpose
and write it down here — don't quietly erode the rule, and don't treat it as
untouchable.

---

## 2 — Self-host the Chinese font as split chunks, not a CDN (2026-06-20)

**Decision.** Serve the reading serif (Source Han Serif, Traditional) as
self-hosted woff2 chunks split by character range (`cn-font-split` → 668 chunks +
the matching `@font-face` CSS, embedded via `go:embed`). The browser fetches only
the chunks whose characters appear on the page. Do **not** use Google Fonts or a
CDN for now.

**Context.** A full Chinese font is ~20MB (20,000+ glyphs — the unavoidable cost of
a logographic script; see the typography note in plan.md). Shipping it as one file
= ~19.7MB per first visit. The font was also silently broken: its `@font-face`
pointed at `/static/`, which 404s (assets live at `/assets/`), so it fell back to
the system serif.

**Why.** Splitting cuts a dense Chinese page from 19.7MB to ~3.6MB (81.5% less),
cached across pages. Self-hosting keeps the single static binary (Decision 1), adds
no outside dependency, and avoids Google (team finds it distasteful, plus privacy).

**Cost / when it's a shackle.** Each extra self-hosted Chinese family ≈ +34MB
binary, +668 committed files. A multi-font picker (sans + serif + weights +
simplified) would bloat the binary to ~155MB / ~2,700 files. Simplified isn't split
yet — re-run `cn-font-split` when simplified support ships (`fonts-src/README.md`).
The split can't be automatic: the browser can't subset a remote one-piece font; the
emerging web standard that would automate it (Incremental Font Transfer) isn't
deployed yet.

**Revisit when.** We add a second Chinese font family (the picker, a Should-Have).
That's the line: **don't embed family #2.** Switch delivery to jsDelivr + Fontsource
(a neutral open-source CDN, no Google, already split) — about a 10-minute change.
Self-host-vs-CDN flips at "more than one downloaded family." Per-page traffic stays
bounded either way; binary size is what forces the switch.

---

## 3 — Replace SQLite with Postgres, clean (2026-06-16)

**Decision.** Postgres (via `pgx/v5`) as the only datastore. Random-ID primary
keys, wiki-link edges resolved to a target ID, full-text search via Postgres'
`tsvector` + index. Shipped as a clean rip-and-replace of the old SQLite schema,
with **no migration/rollback tooling** (see `.claude/postgres-railway-rebuild-spec.md`).

**Context.** SQLite-as-one-file needed a 24/7 pinned disk (Fly.io) and was weak
under concurrent social read/write load. No production data existed yet, so a clean
replace cost nothing.

**Why.** Real concurrency, ID-based linking (survives renames — see the
uuid-linking item in plan.md), built-in full-text search, hosted-Postgres ops on
Render. ID edges are the right base for dynamic hypertext.

**Cost / when it's a shackle.** Lost SQLite's zero-dependency single-file
simplicity; now needs a running Postgres service (Render Basic-1gb ~$19/mo — see
`.claude/budget-render.md`). The "no migration tooling" shortcut is a deliberate
early-dev convenience.

**Revisit when.** Real user data exists — then the schema becomes precious and the
no-migration-tooling shortcut must end (add versioned migrations + rollback *before*
the first real users, not after). The database engine choice itself is unlikely to
revert.

---

## 4 — Keep raw htmx; reject the htmx wrapper libraries (2026-06-20)

**Decision.** Vendored `htmx.min.js` + hand-written `hx-*` attributes. Reject
`htmgo` (a whole framework) and `donseba/go-htmx` (a header-helper library). Detect
htmx requests via the native `HX-Request` header, not a `?partial=1` query param.

**Context.** Both libraries were evaluated during the 2026-06-20 architecture review
(three agents + reading the htmx docs). htmx use here is light (~26 `hx-*`
attributes); the patterns are simple (infinite scroll, self-replacing like/follow
fragments).

**Why.** htmgo = a rewrite off the standard library's `net/http`, heavy lock-in,
kills hand-written CSS. go-htmx wraps ~10 lines of header reads we already do —
not worth it. Raw htmx + the standard library *is* the documented best practice
(see `.claude/htmx-rules.md`).

**Cost / when it's a shackle.** We hand-write the `hx-*` attributes and the few
header reads/response headers (`HX-Trigger`, out-of-band swaps,
`Vary: HX-Request`). Fine at this scale.

**Revisit when.** htmx wiring grows complex enough that typed header builders /
fragment helpers measurably cut error-prone boilerplate — unlikely before a much
bigger surface. Adopt the native patterns (out-of-band swaps, `HX-Trigger`,
`hx-boost`) per-feature first; only consider a library if those aren't enough.

---

## 5 — Adopt Alpine.js; templ held back only by sequencing (2026-06-20)

**templ half done 2026-08-31.** All 19 pages ported from `html/template` to
`templ` in the same pass as the Meta-App view substrate (`NoteListView` +
`feedCard` + a `cardRenderers` registry), exactly as this entry called for —
one migration, not two. `internal/handlers/*.templ` + generated
`*_templ.go` (committed), `_base.html`/`templates/` deleted outright. The
Alpine.js half of this decision is untouched — still not adopted, still open.

**Decision.** Alpine.js (vendored, no build step) for the richer client-side state
the design/product founders will want — htmx for the server-driven 90%, Alpine for
the stateful 10%. **templ is wanted, not blocked.** It's held back *only* by
sequencing, and should be folded into the Meta-App view-substrate refactor rather
than done before it.

**Correction (the reason this entry is spelled out).** templ was earlier waved off
as "breaks no-build-tooling" (Decision 1). That was a category error: Decision 1
targets the **JS** build tar pit (node_modules, npm lockfiles, security holes,
vite/webpack rot). templ is a **Go** tool — `go install`, pinned in go.mod,
`templ generate` emits `.go`. Commit the generated `.go` and the deploy stays pure
`go build`. **Decision 1 does not block templ.** The win is real: compile-checked
templates kill the silent `map[string]any` data-bag typos, and the JSX-like style
suits a JS/TS team.

**Why hold back at all.** Only to avoid migrating twice: adopting templ now and then
restructuring the same 18 templates in the view-substrate refactor = rewriting them
twice. The refactor *is* a template restructure.

**Cost / when it's a shackle.** templ's real costs (not tooling): a `templ generate`
dev-loop step (has a `--watch` mode, works with `air`), generated `.go` noise in the
tree, and the one-time port of the existing templates.

**Revisit when.** The view-substrate refactor is scoped — do it **in templ directly**
(one migration, not two). If that refactor slips and the `map[string]any` typo
problem keeps biting first, adopt templ on its own sooner; nothing structural stops
it.

---

## 6 — Single Go process on Render, not serverless (short — 2026-06-16)

One long-lived Go process on Render, not Vercel/serverless functions. Reason: a
background worker (external-vault crawl) + in-process caches (tag chips) + simple
ops all favor a stateful process; serverless cold-starts and statelessness fight the
design. Details in `.claude/postgres-railway-rebuild-spec.md` Design decisions.
*Expand if serverless is reconsidered (e.g. Cloudflare's stack, still open).*

## 7 — All-public note visibility for the first launch (short — 2026-06-20)

Launch with draft/published only; everything published is world-readable. No
private/unlisted tiers at launch (planned afterward as paid features). Keeps the
permission model trivial for the first version. *Expand when private/unlisted ships.*

## 8 — No folders; organize by tags + links (short — earlier)

The folder concept was removed; the `folder_path` column was dropped entirely in the
Postgres rebuild. Organization is by tags + wiki-links, not a hierarchy. Reason:
folders fight the densely-linked model; collections emerge from links.

---

## 9 — Destructive DB changes go through a dump + a migration (2026-08-30)

**Decision.** Destructive prod SQL is written as a numbered file in
`internal/db/migrations/` and preceded by a verified dump — never typed into an
ad-hoc psql session.

**Context.** Wiping the playtest seed data (plan.md Step 0) first looked like a
dashboard-and-terminal chore: fetch the external DB URL, run DELETEs by hand,
flip `SEED_DEV` in Render's UI. Every step of that is manual, unreviewable, and
unrepeatable at the 封測 → 公測 cutover.

**Why.** The migration runner already applies files once, transactionally, on
Render at boot with the injected `DATABASE_URL`. So the destructive statements
never leave the deploy, no prod credential lands on a laptop, and exactly what
ran is in git. The dump half is `.github/workflows/db-backup.yml`; "verified"
means restored into a scratch DB, because an unrestored dump is a guess.
Procedure: `RUNBOOK-db-purge.md`.

**Cost / when it's a shackle.** A purge needs a deploy, so it is not instant, and
the file also runs against dev and test DBs (harmless for seed purges — they
reseed). One irreducibly manual bit remains: the `PROD_DATABASE_URL` repo secret,
since a credential cannot live in git. Render's internal `dpg-*-a` hostname is
unreachable outside their network, so the external URL is the only option.

**Revisit when.** Real user data exists and a purge needs to be surgical or
reversible mid-flight — then this wants a proper maintenance job with a dry-run
mode, not a one-way migration. This entry is the concrete answer to Decision 3's
"add versioned migrations + rollback *before* the first real users."

---

## Template (copy for a new entry)

**Decision.**
**Context.**
**Why.**
**Cost / when it's a shackle.**
**Revisit when.**
