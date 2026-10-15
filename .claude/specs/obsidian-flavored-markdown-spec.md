# Obsidian-flavored markdown — section embeds, block refs, hover preview

Spec from /askme, 2026-10-15.

## Goal

An Obsidian user publishes a vault to commaplace and it reads the way it does
in Obsidian: no raw syntax leaks, and links, embeds and previews behave the
same. A non-technical user who only writes on commaplace gets the same
features without managing files. The bar is **both**: imported vaults render
clean (no junk), and every construct we support behaves the way Obsidian's
reading view does, enforced by tests. Obsidian-flavored markdown (OFM) is the
one storage format. Later dialects (Roam, HackMD, WordPress) convert into it
on import.

## Already shipped (do not rebuild)

`internal/markdown/render.go` already handles GFM, footnotes, `%%comments%%`,
`==highlight==`, `$math$`, mermaid/math fences, foldable callouts, heading IDs,
`#tags`, `[[wikilinks]]` with `#anchor` and `|alias`, `![[note]]` embeds
(whole note, depth cap 2) and frontmatter stripping. Hover preview exists:
`static/linkpreview.js` + `GET /api/preview/{user}/{slug}`, which shows the
masonry card.

## Scope (v1)

1. **Section embeds.** `![[note#Heading]]` embeds only that heading's section.
   `![[note#^id]]` embeds only that block, but only while the block-ref flag
   is on (see 2).
2. **Block refs `^id`, built but off.** Code ships behind an in-code flag,
   `var blockRefs = false` in `internal/markdown/subpath.go`. It is a var
   only so tests can flip it.
   - **Always on, flag or not:** a trailing ` ^id` on a paragraph or list item
     is stripped from reading view, so imported vaults stay clean.
   - **Flag off:** `[[note#^id]]` links to the top of the note.
     `![[note#^id]]` embeds the whole note.
   - **Flag on:** the block element gets `id="^id"`. Links scroll to the
     block and embeds slice the block.
3. **Hover preview, global and section-aware.**
   - Bind it to the wiki-link primitive site-wide: load `linkpreview.js` from
     `layout.templ` instead of three page templates. Containers opt out with
     `data-no-preview`. Opt-outs go only where a preview is wrong, such as the
     editor and the preview's own popup.
   - Embeds are covered too, since their links are ordinary wiki links.
   - `[[note#Heading]]` previews that section. The endpoint takes an optional
     anchor parameter, and the cache varies on it.
4. **Editor.**
   - After `[[note#`, the autocomplete lists the target note's headings. This
     needs a headings endpoint, or a `#` branch in `/api/wiki/suggest`.
   - Section embeds render through `POST /preview`. The editor no longer has
     a preview pane; that endpoint now serves the batch-import preview.
5. **Golden tests.** `internal/markdown/testdata/ofm/` holds `.md` inputs and
   expected `.html` outputs. Each Obsidian-parity case cites the behavior rule
   it checks (see "Behavior rules").

## Design decisions

- **app.js is a behavior reference, nothing more.** Obsidian's `app.js`
  contains the editor and reading-view logic, which is exactly the behavior
  we want to match. We study it only to answer specific edge-case questions.
  REA's whole-app analysis stalled on a pdf.js file, so the rules below come
  from reading the prettier-formatted `app.js` directly. Findings are
  written down as prose rules plus golden test cases. No Obsidian code is
  copied into this repo. goldmark's AST is different anyway, so a port would
  not fit.
- **No frontmatter aliases in link resolution.** Letting a note claim extra
  names would let anyone pollute the shared namespace. Only the citing side
  can rename a link (`[[original title|label]]`), and that already works.
  Imported `[[alias]]` links stay unresolved.
- **No properties panel.** Frontmatter stays out of reading view, as it is
  today. This overrides the round-1 pick: the user later said no panel at all.
- **OFM is canonical; convert on import.** One renderer. Future dialect
  importers rewrite into OFM, for example Roam `((uid))` becomes a block ref
  and HackMD `:::info` becomes a callout.
- **The feature flag is an in-code constant, not an env var.** This is the
  user's call. Turning it on takes a deploy.
- **Slice in markdown, then render.** The embed resolver gets `link.Anchor`,
  extracts the section or block from the source markdown, and renders only
  that. Rendering the whole note and cutting the HTML would break footnotes,
  heading IDs and relative structure.

## Behavior rules (from Obsidian 1.14.4 `app.js`, read 2026-10-15)

Source: `obsidian-1.14.4.asar.gz` from `obsidianmd/obsidian-releases`. Its
SHA-256 matches `desktop-releases.json`. Rules are restated in prose; no code
was copied.

1. **Subpath parsing.** The text after the first `#` is split on `#`, and
   empty parts are dropped.
   - One part starting with `^` is a block ref.
   - One part of the form `[^x]` is a footnote ref (out of scope).
   - Anything else is a heading path.
2. **Heading match.** Both the heading text and the path part are normalized:
   - Each of ``!"#$%&()*+,.:;<=>?@^`{|}~/[]\`` and CR/LF is replaced with
     a space.
   - Runs of whitespace collapse to one space, and the result is trimmed and
     lowercased.

   Headings are walked in document order. Path part *k* matches only a
   heading deeper than the heading part *k-1* matched, so `#A#C` matches `C`
   anywhere below `A`, not only as a direct child. The first full match wins.
   [Inference] The heading text compared is the heading's source text, not
   its rendered text.
3. **Section extent.** A section runs from its heading to the next heading of
   the same or a higher level (level ≤ the target's), or to end of file. The
   heading line itself is included. Headings inside code fences are not
   headings, because the cache is built from the parsed tree.
4. **Block-ID grammar.** `^` followed by `[A-Za-z0-9-]+`. An ID is recognized
   in two forms:
   - **Inline:** at the very end of a block's text, preceded by whitespace.
     A `^` that starts the text does not count.
   - **Standalone:** a paragraph consisting only of `^id`, followed by end of
     input or a blank line. It attaches to the *previous* sibling block and is
     removed. Standalone as the first block, it is dropped.

   Placement rules:
   - **List items:** the ID found in an item (not counting its nested lists)
     belongs to that item.
   - **Other blocks** (quote, callout, table): an ID anywhere inside belongs
     to the outer block, and the last one wins.
   - **Lookup:** IDs are case-insensitive. A block ref is honored only as the
     sole subpath part, so `#H#^id` is a failed heading lookup.
5. **Block extent.** The block's own source range. For a list item, the range
   extends over every following item nested under it, so the sub-list is
   included.
6. **Missing target.** An embed whose section or block can't be found
   renders an h5 line, "unable to find section *subpath* in *file*". Links
   with a missing subpath still open the note. Our copy is
   `找不到段落 <code>#subpath</code>（<code>file</code>）`, in the existing
   `embed-missing` style.

## Open questions

- **Global hover cost.** Loading `linkpreview.js` on every page brings
  previews to the feed. Feed card excerpts don't render wiki links today, so
  confirm the global load is cheap and a no-op there.
- **Other N+1 queries.** `recomputeLinks` (on save) still runs two queries
  per link. That's the write path and outside this spec.

## Resolved during the build

- **Embed N+1:** fixed. `buildEmbedResolver` now runs one query per nesting
  level plus one for all the links inside embeds. Nested embeds now resolve
  against their own vault (they used to use the outer note's).
- **Nesting cap:** `maxEmbedDepth = 2` in `internal/handlers/links.go`.
  Lower it to 1 there when traffic warrants.
- **Block-ID HTML:** `id="^abc"`. Verified in Chromium 1194:
  - Fragment navigation to `#^abc` scrolls and sets `:target` natively.
  - `getElementById` works.
  - A raw `querySelector('#^abc')` throws, so JS selectors must use
    `CSS.escape`.

  `id="block-abc"` would need JS to make `#^abc` URLs work.

## Verification — for reviewers, agents and playtesters

### 1. Unit and golden tests (no DB)

```sh
go test ./internal/markdown
```

- `subpath_test.go` checks each behavior rule; case names cite the rule
  number.
- `golden_test.go` renders `testdata/ofm/*.md` and compares against the
  `.html` beside each one. After an intended rendering change, run
  `go test ./internal/markdown -run OFMGolden -update` and review the `.html`
  diff line by line. A golden diff you didn't intend is a regression.

### 2. Integration tests (Postgres)

```sh
make test   # or: TEST_DATABASE_URL=... go test -p 1 ./...
```

New cases:
- **`embed_test.go`:** nested embeds resolve in their own vault; the depth
  cap; section embeds and the missing-section line.
- **`preview_test.go`:** `TestPreviewSection` covers `?h=` sections, the
  fallback to the note card, and no draft leak.
- **`wiki_headings_test.go`:** heading suggestions, nested paths, cross-vault
  lookups, and drafts visible only to their author.

### 3. Query count (the N+1 fix)

Turn on statement logging for the dev DB, restart the server, load a note
with 1 embed and then one with 6, and count the statements between loads:

```sh
psql -d commaplace -c "ALTER DATABASE commaplace SET log_statement = 'all'"
# ...load pages, count "statement:" lines in the Postgres log...
psql -d commaplace -c "ALTER DATABASE commaplace RESET log_statement"
```

Measured 2026-10-15:

| Note | Before | After |
|------|--------|-------|
| 1 embed | 16 | 15 |
| 6 embeds | 31 | 15 |

The count must not grow with the number of embeds.

### 4. Browser end-to-end (manual, needs Playwright)

```sh
DEBUG=1 go run ./cmd/server          # or make dev
NODE_PATH=$(npm root -g) node scripts/ofm-e2e.cjs   # BASE=… to target another port
```

It creates two notes as alice (per-run suffix, rerunnable) and checks:
- section, missing-section and block embeds;
- hidden `^id` markers, and block links landing on the note;
- the section hover card;
- `linkpreview.js` loaded exactly once on note, feed, profile, graph and
  search pages;
- `[[note#` autocomplete, both listing and inserting;
- `POST /preview` rendering a section embed;
- no script errors.

It writes screenshots `ofm-hover-section.png` and `ofm-editor-headings.png`
to `$OUT`. Last run: 17/17 PASS.

### 5. Playtest (human, about 15 minutes)

Use `make watch`, logged in as alice. Expected results are in brackets.

1. Write note A with `# Setup`, `## Linux`, `## macOS`, `# Usage`, and a
   paragraph ending in ` ^p1`.
   [In reading view `^p1` is invisible, and the feed card excerpt doesn't
   show it either.]
2. In note B, type `[[` then pick A, then type `#`.
   [A's headings appear with their level, H1 or H2.]
   Type `li` [only Linux], then press Enter.
   [`[[a#Linux]]` is inserted and the popup closes.]
3. In B, write `[[a#Setup#macOS]]`, `![[a#Setup]]`, `![[a#Nope]]` and
   `![[a#^p1]]`, then publish.
   - [`![[a#Setup]]` shows Setup, Linux and macOS, and not Usage.]
   - [`![[a#Nope]]` shows 找不到段落 `#Nope`.]
   - [`![[a#^p1]]` shows the whole of A, because block refs are off.]
4. Hover the `[[a#Setup#macOS]]` link on desktop.
   [After about 350 ms a card shows "A › Setup#macOS" with only the macOS
   section.]
   Click it. [It lands on A and scrolls to macOS.]
5. Hover a plain `[[a]]` link. [The usual note card.]
   Repeat on `/graph`, on your profile, and inside an embed's body.
6. Check dark mode and a phone-width viewport.
   - [The card stays readable and scrolls inside itself when long.]
   - [On touch there is no preview; a tap navigates.]
7. Import an Obsidian vault that uses `^ids` and `![[note#Heading]]`
   (`scripts/make-beta-vault.py` makes one).
   [No raw `^id` text shows, and heading embeds slice correctly.]
8. Optional, block refs on: set `blockRefs = true` locally and rerun steps
   3–4.
   - [`![[a#^p1]]` shows only that paragraph.]
   - [`[[a#^p1]]` scrolls to it.]
   - [The paragraph carries `id="^p1"`.]
   Revert before committing.

### What to scrutinize in review

- **Draft leaks.** Every new read (`?h=` preview, heading suggest, section
  embed) must go through `readableNote` or an author check.
- **Global preview script.** `linkpreview.js` is now on every non-desk page.
  Check for double-binding, and that `data-no-preview` works.
- **Marker stripping.** It is AST-based, so code blocks keep `^id` text.
  `cutInlineTail` aborts when the tail is not plain text.
- **Fragment URLs.** `[[note#A#B]]` now scrolls to `#b`, not `#ab`.

## Out of scope for v0

- Properties panel or any reader-facing frontmatter display.
- Resolving links through frontmatter `aliases`.
- `[[note#^` block-ID autocomplete and inserting generated `^id`s into other
  notes (it writes to a second note).
- Turning on the block-ref flag.
- Dialect importers for Roam, HackMD and WordPress/rich text. The direction
  is fixed (convert to OFM); the work is not scheduled.
- Canvas, Bases, Dataview, Excalidraw, and embedded attachments (no file
  hosting, by design; see `media.go`).
- Per-dialect renderers or a `notes.dialect` column.
