// Package seed contains the curated example vault that new users can fork
// during onboarding. Apply ensures the seed users + their notes exist.
// TourNotes is exported so the handlers package can copy it for forks
// without re-querying the seed users.
package seed

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

const TourHandle = "commonplace-tour"
const MakerHandle = "maker"
const DemoHandle = "shawn"

type TourNote struct {
	Author    string // TourHandle or MakerHandle
	Folder    string // "" for root
	Slug      string
	Title     string
	Body      string
	Tags      []string
	IsWelcome bool // pin this note on the new user's /me when forked
}

// DemoNote is the curated content that ships with the repo so a fresh
// `go run ./cmd/server` lands on a feed full of real notes instead of empty.
type DemoNote struct {
	Folder string
	Slug   string
	Title  string
	Body   string
	Tags   []string
}

// ApplyDemo ensures the demo user + DemoNotes exist. Idempotent — if the
// demo user already exists we assume the seed has already run.
func ApplyDemo(ctx context.Context, db *sql.DB, recompute func(ctx context.Context, tx *sql.Tx, sourceID int64, authorHandle, body string) error) error {
	var existing int
	if err := db.QueryRowContext(ctx,
		`SELECT 1 FROM users WHERE handle = ?`, DemoHandle,
	).Scan(&existing); err == nil {
		return nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	now := time.Now().Unix()
	res, err := tx.ExecContext(ctx,
		`INSERT INTO users(handle, email, created_at, onboarded_at) VALUES(?, ?, ?, ?)`,
		DemoHandle, DemoHandle+"@demo.local", now, now)
	if err != nil {
		return err
	}
	authorID, _ := res.LastInsertId()

	for _, n := range DemoNotes {
		res, err := tx.ExecContext(ctx, `
			INSERT INTO notes(author_id, folder_path, slug, title, body_md, created_at, updated_at)
			VALUES(?, ?, ?, ?, ?, ?, ?)`,
			authorID, n.Folder, n.Slug, n.Title, n.Body, now, now,
		)
		if err != nil {
			return err
		}
		nid, _ := res.LastInsertId()
		for _, t := range n.Tags {
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO note_tags(note_id, tag, created_at) VALUES(?, ?, ?)`,
				nid, t, now,
			); err != nil {
				return err
			}
		}
		if err := recompute(ctx, tx, nid, DemoHandle, n.Body); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE links SET resolved_target_id = ?
			WHERE resolved_target_id IS NULL
			  AND target_user_handle = ? AND target_folder_path = ? AND target_slug = ?`,
			nid, DemoHandle, n.Folder, n.Slug,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// TourNotes is the curated content. Order matters because earlier notes are
// linked-to by later ones; we insert them in this order so resolution
// succeeds without needing a second pass.
var TourNotes = []TourNote{
	{
		Author: TourHandle, Folder: "", Slug: "welcome", Title: "Welcome to commonplace",
		IsWelcome: true,
		Tags:      []string{"tour"},
		Body: `commonplace is markdown notes that **link to each other**, including across other people's vaults.

Take the tour:

- [[getting-started/write-your-first-note]]
- [[getting-started/use-folders]]
- [[getting-started/wiki-links]]
- [[getting-started/tags-and-search]]

Or jump to features and examples:

- [[features/markdown-support]]
- [[features/cross-vault]]
- [[features/backlinks]]
- [[features/likes-and-follows]]
- [[examples/connection-map]]

Have fun.`,
	},
	{
		Author: TourHandle, Folder: "getting-started", Slug: "write-your-first-note",
		Title: "Write your first note", Tags: []string{"tour", "getting-started"},
		Body: `Click **write** in the top nav. Give it a title, optionally a folder path, and write the body in markdown.

A few rules:

- The slug is auto-generated from the title (kebab-case).
- The folder path is just "music/mixing" — slashes nest the folder tree on your profile.
- Tags are comma-separated. They show up on the note view as chips.

That's it. Hit publish and you have a note.`,
	},
	{
		Author: TourHandle, Folder: "getting-started", Slug: "use-folders",
		Title: "Use folders", Tags: []string{"tour", "getting-started"},
		Body: `Folders are just paths. You don't create them ahead of time — write a note with folder ` + "`music/mixing`" + ` and the folder appears on your profile.

The folder tree on your profile is built automatically from every distinct folder path you've used.

There's no "rename folder" command. To move a note, edit its folder. (Editing is on the roadmap; for v1 you delete and re-create.)`,
	},
	{
		Author: TourHandle, Folder: "getting-started", Slug: "wiki-links",
		Title: "Wiki links", Tags: []string{"tour", "getting-started"},
		Body: `Type ` + "`[[`" + ` in the editor and an autocomplete pops up. Pick a note, hit Enter.

Four syntax variants:

- ` + "`[[slug]]`" + ` — same vault, root folder
- ` + "`[[folder/slug]]`" + ` — same vault, in a folder
- ` + "`[[@user/slug]]`" + ` — another person's vault
- ` + "`[[@user/folder/slug]]`" + ` — another person's vault, in their folder

Cross-vault links render in a different color so you can see the network.`,
	},
	{
		Author: TourHandle, Folder: "getting-started", Slug: "tags-and-search",
		Title: "Tags and search", Tags: []string{"tour", "getting-started"},
		Body: `Tags are the discovery scaffolding. Add a few comma-separated tags when you write — they show up under the title and on the feed as filter chips.

Click any tag to see every note carrying it (yours and other people's).

The top-nav search box does full-text search across every note on the site. Filter to **我的** or **我追蹤的人** with the chips on the results page.`,
	},
	{
		Author: TourHandle, Folder: "features", Slug: "markdown-support",
		Title: "Markdown support", Tags: []string{"tour", "features"},
		Body: `commonplace ships GitHub-Flavored Markdown:

- headings ` + "`# ## ###`" + `
- **bold**, *italic*, ` + "`code`" + `
- bullet and numbered lists
- > blockquotes
- code fences with no syntax highlighting (yet)
- tables
- task lists`,
	},
	{
		Author: TourHandle, Folder: "features", Slug: "cross-vault",
		Title: "Cross-vault links", Tags: []string{"tour", "features"},
		Body: `Same-vault links connect notes inside one person's vault. Cross-vault links connect across vaults.

Try this: I'm linking to another seeded user — [[@maker/art/process]]. The link is teal because it crosses into someone else's vault.

When the target user writes a note that links back to one of yours, it shows up under the **跨 vault** section of the backlinks panel.`,
	},
	{
		Author: TourHandle, Folder: "features", Slug: "backlinks",
		Title: "Backlinks", Tags: []string{"tour", "features"},
		Body: `Every note shows what links *to* it, automatically.

The backlinks panel splits into two sections:

- **本 vault** — notes from your own vault that link here
- **跨 vault · 連到別人** — notes from other people's vaults that link here

Click any backlink and you land on the source note with a "← 回 X" banner so you can return.`,
	},
	{
		Author: TourHandle, Folder: "features", Slug: "likes-and-follows",
		Title: "Likes and follows", Tags: []string{"tour", "features"},
		Body: `Heart any note (♡) and it lands on your **/me/saved** page.

Follow any user on their profile page; their notes show up under the **追蹤中** tab on the feed.

Both are public.`,
	},
	{
		Author: TourHandle, Folder: "examples", Slug: "connection-map",
		Title: "Connection map", Tags: []string{"tour", "examples"},
		Body: `A note that's mostly links. The feed shows it as a **connection-preview** card with the targets as chips:

- [[welcome]]
- [[features/cross-vault]]
- [[features/backlinks]]
- [[@maker/art/tools-i-use]]
- [[examples/quote-callout]]
- [[examples/list-shape]]`,
	},
	{
		Author: TourHandle, Folder: "examples", Slug: "quote-callout",
		Title: "Quote callout", Tags: []string{"tour", "examples"},
		Body: `> The medium is the message. We shape our tools and thereafter our tools shape us.

A note that opens with a blockquote renders as a **quote callout** card on the feed — the quote becomes the card body.`,
	},
	{
		Author: TourHandle, Folder: "examples", Slug: "list-shape",
		Title: "Bullet list shape", Tags: []string{"tour", "examples"},
		Body: `- this note opens with bullets
- so the feed shows it as a list-style card
- with the first few bullets visible
- skim-friendly without opening
- nice for stacks of small things`,
	},

	// @maker — a second seeded user for cross-vault demos.
	{
		Author: MakerHandle, Folder: "art", Slug: "process",
		Title: "My process", Tags: []string{"art", "process"},
		Body: `Two phases: morning is generative — fast sketches, no editing. Afternoon is critical — pick the strongest, take it to finish.

I learned this from reading about other people's workflows on [[@commonplace-tour/features/cross-vault]] — there's value in publishing the messy steps publicly.`,
	},
	{
		Author: MakerHandle, Folder: "art", Slug: "tools-i-use",
		Title: "Tools I use", Tags: []string{"art", "tools"},
		Body: `- Procreate for sketching
- Pencil + paper for thumbnails
- A two-monitor setup for reference
- Music: ambient, no lyrics

See [[@commonplace-tour/welcome]] if you want to see how this whole linking thing works.`,
	},
}

// Apply ensures the seed users and their notes exist in db. Idempotent —
// if @commonplace-tour already exists we assume the seed has already run.
func Apply(ctx context.Context, db *sql.DB, recompute func(ctx context.Context, tx *sql.Tx, sourceID int64, authorHandle, body string) error) error {
	var existing int
	if err := db.QueryRowContext(ctx,
		`SELECT 1 FROM users WHERE handle = ?`, TourHandle,
	).Scan(&existing); err == nil {
		return nil // already seeded
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	now := time.Now().Unix()
	authors := map[string]int64{}
	for _, h := range []string{TourHandle, MakerHandle} {
		res, err := tx.ExecContext(ctx,
			`INSERT INTO users(handle, email, created_at, onboarded_at) VALUES(?, ?, ?, ?)`,
			h, h+"@seed.local", now, now)
		if err != nil {
			return err
		}
		id, _ := res.LastInsertId()
		authors[h] = id
	}

	for _, n := range TourNotes {
		authorID, ok := authors[n.Author]
		if !ok {
			continue
		}
		res, err := tx.ExecContext(ctx, `
			INSERT INTO notes(author_id, folder_path, slug, title, body_md, created_at, updated_at)
			VALUES(?, ?, ?, ?, ?, ?, ?)`,
			authorID, n.Folder, n.Slug, n.Title, n.Body, now, now,
		)
		if err != nil {
			return err
		}
		nid, _ := res.LastInsertId()
		for _, t := range n.Tags {
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO note_tags(note_id, tag, created_at) VALUES(?, ?, ?)`,
				nid, t, now,
			); err != nil {
				return err
			}
		}
		if err := recompute(ctx, tx, nid, n.Author, n.Body); err != nil {
			return err
		}
		// Re-resolve any earlier-seeded notes whose links pointed at this one.
		if _, err := tx.ExecContext(ctx, `
			UPDATE links SET resolved_target_id = ?
			WHERE resolved_target_id IS NULL
			  AND target_user_handle = ? AND target_folder_path = ? AND target_slug = ?`,
			nid, n.Author, n.Folder, n.Slug,
		); err != nil {
			return err
		}
		if n.IsWelcome {
			if _, err := tx.ExecContext(ctx,
				`UPDATE users SET pinned_note_id = ? WHERE id = ?`, nid, authorID,
			); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}
