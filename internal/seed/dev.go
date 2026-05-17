package seed

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"strings"
	"time"
)

var nonSlugRE = regexp.MustCompile(`[^a-z0-9]+`)

// ApplyDev seeds a realistic multi-user dataset for local development.
// Safe to call multiple times — exits early if alice already exists.
// Activate with SEED_DEV=1.
func ApplyDev(ctx context.Context, db *sql.DB, recompute func(ctx context.Context, tx *sql.Tx, sourceID int64, authorHandle, body string) error) error {
	var existing int
	err := db.QueryRowContext(ctx, `SELECT 1 FROM users WHERE handle = 'alice'`).Scan(&existing)
	if err == nil {
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	base := time.Now().Unix()
	tick := func(n int64) int64 { return base - n*3600 } // n hours ago

	insertUser := func(handle, email string) (int64, error) {
		r, err := tx.ExecContext(ctx,
			`INSERT INTO users(handle, email, created_at, onboarded_at) VALUES(?, ?, ?, ?)`,
			handle, email, tick(720), tick(720),
		)
		if err != nil {
			return 0, err
		}
		return r.LastInsertId()
	}

	slugify := func(title string) string {
		s := strings.ToLower(title)
		s = nonSlugRE.ReplaceAllString(s, "-")
		return strings.Trim(s, "-")
	}

	insertNote := func(authorID int64, handle, folder, title, body string, tags []string, hoursAgo int64) (int64, error) {
		slug := slugify(title)
		t := tick(hoursAgo)
		r, err := tx.ExecContext(ctx, `
			INSERT INTO notes(author_id, folder_path, slug, title, body_md, created_at, updated_at)
			VALUES(?, '', ?, ?, ?, ?, ?)`,
			authorID, slug, title, body, t, t,
		)
		if err != nil {
			return 0, err
		}
		nid, _ := r.LastInsertId()
		for _, tag := range tags {
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO note_tags(note_id, tag, created_at) VALUES(?, ?, ?)`, nid, tag, t,
			); err != nil {
				return 0, err
			}
		}
		if err := recompute(ctx, tx, nid, handle, body); err != nil {
			return 0, err
		}
		// Resolve any previously-inserted dangling links pointing to this note.
		if _, err := tx.ExecContext(ctx, `
			UPDATE links SET resolved_target_id = ?
			WHERE resolved_target_id IS NULL
			  AND target_user_handle = ? AND target_slug = ?`,
			nid, handle, slug,
		); err != nil {
			return 0, err
		}
		return nid, nil
	}

	follow := func(followerID, followedID int64) {
		tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO follows(follower_id, followed_id, created_at) VALUES(?, ?, ?)`,
			followerID, followedID, tick(500),
		)
	}

	like := func(userID, noteID int64) {
		tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO likes(user_id, note_id, created_at) VALUES(?, ?, ?)`,
			userID, noteID, tick(10),
		)
	}

	// ── Users ──────────────────────────────────────────────────────────────

	aliceID, _ := insertUser("alice", "alice@dev.local")
	bobID, _ := insertUser("bob", "bob@dev.local")
	carolID, _ := insertUser("carol", "carol@dev.local")
	daveID, _ := insertUser("dave", "dave@dev.local")

	// ── Alice: knowledge management, philosophy ─────────────────────────────

	a1, _ := insertNote(aliceID, "alice", "", "what is a commonplace book", `A commonplace book is a personal knowledge repository — a place to collect ideas, quotes, and observations that matter to you.

The practice dates back to ancient Greece. Renaissance scholars kept them obsessively. John Locke invented an indexing system for his.

The key insight: **writing forces understanding**. Copying a quote isn't enough. You have to put it in your own words to own it.

See also: [[how-i-take-notes]], [[on-linking-ideas]]`, []string{"knowledge", "writing"}, 200)

	a2, _ := insertNote(aliceID, "alice", "", "how-i-take-notes", `My note-taking workflow has three stages:

1. **Capture** — write anything, don't edit. Speed matters here.
2. **Process** — next day, rewrite in my own words. Delete what doesn't hold up.
3. **Connect** — find links to existing notes. This is where understanding happens.

Tools I've tried: Notion (too heavy), Roam (too clever), plain files (too lonely).

This is what I want from [[what-is-a-commonplace-book]]: a place where ideas collide.

Related: [[@bob/zettelkasten]]`, []string{"writing", "workflow"}, 180)

	a3, _ := insertNote(aliceID, "alice", "philosophy", "on-linking-ideas", `Every idea is a node. Its value comes from its connections.

An isolated note is trivia. A connected note is knowledge. A densely connected cluster is understanding.

This is why graph view matters — it's not a gimmick. It shows you where your thinking is sparse and where it's rich.

[[what-is-a-commonplace-book]] started as a single node. Now it links to workflow notes, philosophy notes, tool notes. It has become more than itself.`, []string{"philosophy", "knowledge"}, 96)

	a4, _ := insertNote(aliceID, "alice", "philosophy", "the-collector-fallacy", `There is a trap that every note-taker falls into: collecting material without processing it.

You save 200 articles. You highlight everything. You have a system. But you never write.

The collection becomes the procrastination.

The fix is uncomfortable: **delete what you haven't touched in 60 days**. If it was important, you'll remember it. If you don't remember it, it wasn't load-bearing.

See: [[how-i-take-notes]]`, []string{"writing", "productivity"}, 48)

	// ── Bob: programming, zettelkasten ──────────────────────────────────────

	b1, _ := insertNote(bobID, "bob", "", "zettelkasten", `The Zettelkasten method (German: "slip box") is a note-taking system developed by sociologist Niklas Luhmann.

He produced 90,000 index cards and 70 books. The cards are not organized by topic — they're organized by connection.

The key rule: **one idea per note**. A note that contains two ideas should be two notes.

See: [[atomic-notes]], [[@alice/how-i-take-notes]]`, []string{"knowledge", "zettelkasten"}, 190)

	b2, _ := insertNote(bobID, "bob", "", "atomic-notes", `An atomic note contains exactly one idea.

Not one topic. Not one article. One **claim** that can stand on its own.

Bad: "Notes on WWII" (a topic, not a thought)
Good: "The Molotov-Ribbentrop pact delayed Germany's eastern front by 2 years" (a claim)

Atomic notes are easier to link because they have a clear identity. You know when two notes belong together.

Related: [[zettelkasten]], [[@carol/on-music-theory-as-a-language]]`, []string{"knowledge", "writing"}, 160)

	b3, _ := insertNote(bobID, "bob", "programming", "go-concurrency-patterns", `Three patterns I use constantly in Go:

**1. Fan-out / Fan-in**
Distribute work across goroutines, collect results via a single channel.

**2. Pipeline**
Each stage reads from an input channel, transforms, writes to an output channel. Cancelable via context.

**3. Worker pool**
Fixed-size pool of goroutines pulling from a shared job queue. Prevents goroutine explosion on bursty workloads.

The rule: **channels for coordination, mutexes for state**. Never fight the runtime.`, []string{"programming", "go", "concurrency"}, 72)

	b4, _ := insertNote(bobID, "bob", "programming", "sqlite-is-underrated", `SQLite runs in 3 billion devices. It is the most deployed database in the world.

For most apps, it is the right database. Not because it's simple — it isn't — but because the problems it solves are the actual problems:

- Embedded, no network hop
- ACID transactions
- WAL mode for concurrent readers
- FTS5 for full-text search built in

The moment you need it: when you realize you've been running Postgres for a 3-person app with 50 writes/day.

Related: [[@dave/relational-model]]`, []string{"programming", "databases", "sqlite"}, 24)

	// ── Carol: music, creativity ─────────────────────────────────────────────

	c1, _ := insertNote(carolID, "carol", "", "on-music-theory-as-a-language", `Music theory is not rules. It is a vocabulary.

When someone says "that chord is a ii-V-I," they're not saying "that's correct." They're saying "I recognize that shape, I know where it comes from, I know where it's likely to go."

Theory lets you **name what you're hearing**. Naming is the first step to understanding.

The trap is mistaking the map for the territory. [[@alice/on-linking-ideas]] says something similar about note systems.`, []string{"music", "theory", "creativity"}, 144)

	c2, _ := insertNote(carolID, "carol", "", "transcription-practice", `The fastest way to improve: transcribe music you love.

Not by ear necessarily — transcription means **studying how someone else made decisions**. Why this note here? Why rest there? Why that voicing?

One transcription teaches more than six months of exercises because it is inseparable from music you already love.

See: [[on-music-theory-as-a-language]]`, []string{"music", "practice"}, 60)

	// ── Dave: math, science ──────────────────────────────────────────────────

	d1, _ := insertNote(daveID, "dave", "", "relational-model", `E.F. Codd's 1970 paper "A Relational Model of Data for Large Shared Data Banks" is one of the most consequential technical documents ever written.

The core idea: represent data as **relations** (tables), not as hierarchies or networks. Operations on relations always produce relations. The interface is declarative — describe *what* you want, not *how* to get it.

Fifty years later, SQL is still the dominant query language. The model was right.

See: [[@bob/programming/sqlite-is-underrated]]`, []string{"math", "databases", "history"}, 110)

	d2, _ := insertNote(daveID, "dave", "", "on-abstraction", `An abstraction is useful when it hides the right things.

Good abstractions:
- TCP/IP hides physical medium; you think in packets
- SQL hides storage layout; you think in sets
- Functions hide implementation; you think in transformations

Bad abstractions hide the wrong things — they leak. When a leaky abstraction breaks, you have to understand both layers at once, which is harder than understanding either alone.

Related: [[relational-model]], [[@bob/programming/go-concurrency-patterns]]`, []string{"math", "programming", "philosophy"}, 36)

	// ── Social graph ─────────────────────────────────────────────────────────

	// Alice follows bob and carol
	follow(aliceID, bobID)
	follow(aliceID, carolID)
	// Bob follows alice and dave
	follow(bobID, aliceID)
	follow(bobID, daveID)
	// Carol follows alice
	follow(carolID, aliceID)
	// Dave follows bob and carol
	follow(daveID, bobID)
	follow(daveID, carolID)

	// ── Likes ────────────────────────────────────────────────────────────────

	like(aliceID, b1) // alice likes bob's zettelkasten
	like(aliceID, b2) // alice likes atomic notes
	like(aliceID, c1) // alice likes carol's music theory note
	like(bobID, a1)   // bob likes alice's commonplace book
	like(bobID, a3)   // bob likes alice's linking ideas
	like(carolID, a2) // carol likes alice's note-taking workflow
	like(carolID, b2) // carol likes bob's atomic notes
	like(daveID, b4)  // dave likes bob's sqlite note
	like(daveID, d2)  // dave likes his own abstraction note (yes, this is legal)
	like(aliceID, d1) // alice likes dave's relational model note

	_ = a1; _ = a2; _ = a3; _ = a4
	_ = b1; _ = b2; _ = b3; _ = b4
	_ = c1; _ = c2
	_ = d1; _ = d2

	return tx.Commit()
}
