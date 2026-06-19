package seed

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

var nonSlugRE = regexp.MustCompile(`[^a-z0-9]+`)

// ApplyDev seeds a realistic multi-user dataset for local development.
// Safe to call multiple times — exits early if alice already exists.
// Activate with SEED_DEV=1.
func ApplyDev(ctx context.Context, db *sql.DB, recompute func(ctx context.Context, tx *sql.Tx, sourceID uuid.UUID, authorHandle, body string) error) error {
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

	insertUser := func(handle, email string) (uuid.UUID, error) {
		var id uuid.UUID
		err := tx.QueryRowContext(ctx,
			`INSERT INTO users(handle, handle_ci, email, created_at, onboarded_at) VALUES($1, $2, $3, $4, $4) RETURNING id`,
			handle, strings.ToLower(handle), email, tick(720),
		).Scan(&id)
		return id, err
	}

	slugify := func(title string) string {
		s := strings.ToLower(title)
		s = nonSlugRE.ReplaceAllString(s, "-")
		return strings.Trim(s, "-")
	}

	insertNote := func(authorID uuid.UUID, handle, folder, title, body string, tags []string, hoursAgo int64) (uuid.UUID, error) {
		slug := slugify(title)
		t := tick(hoursAgo)
		var nid uuid.UUID
		err := tx.QueryRowContext(ctx, `
			INSERT INTO notes(author_id, slug, slug_ci, title, body_md, created_at, updated_at, published_at)
			VALUES($1, $2, $3, $4, $5, $6, $6, $6) RETURNING id`,
			authorID, slug, strings.ToLower(slug), title, body, t,
		).Scan(&nid)
		if err != nil {
			return uuid.UUID{}, err
		}
		for _, tag := range tags {
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO note_tags(note_id, tag, created_at) VALUES($1, $2, $3)`, nid, tag, t,
			); err != nil {
				return uuid.UUID{}, err
			}
		}
		if err := recompute(ctx, tx, nid, handle, body); err != nil {
			return uuid.UUID{}, err
		}
		// Resolve any previously-inserted dangling links pointing to this note.
		if _, err := tx.ExecContext(ctx, `
			UPDATE links SET resolved_target_id = $1
			WHERE resolved_target_id IS NULL
			  AND target_user_handle = $2 AND target_slug = $3`,
			nid, handle, slug,
		); err != nil {
			return uuid.UUID{}, err
		}
		return nid, nil
	}

	follow := func(followerID, followedID uuid.UUID) {
		tx.ExecContext(ctx,
			`INSERT INTO follows(follower_id, followed_id, created_at) VALUES($1, $2, $3) ON CONFLICT (follower_id, followed_id) DO NOTHING`,
			followerID, followedID, tick(500),
		)
	}

	like := func(userID, noteID uuid.UUID) {
		tx.ExecContext(ctx,
			`INSERT INTO likes(user_id, note_id, created_at) VALUES($1, $2, $3) ON CONFLICT (user_id, note_id) DO NOTHING`,
			userID, noteID, tick(10),
		)
	}

	// ── Users ──────────────────────────────────────────────────────────────

	aliceID, _ := insertUser("alice", "alice@dev.local")
	bobID, _ := insertUser("bob", "bob@dev.local")
	carolID, _ := insertUser("carol", "carol@dev.local")
	daveID, _ := insertUser("dave", "dave@dev.local")
	eveID, _ := insertUser("eve", "eve@dev.local")
	frankID, _ := insertUser("frank", "frank@dev.local")

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

	a5, _ := insertNote(aliceID, "alice", "philosophy", "writing-is-thinking", `If you can't write it down clearly, you don't understand it. That is not a metaphor — it's a diagnostic.

The page is unforgiving in a way conversation isn't. In conversation, vague gestures pass for thoughts. On the page, "the thing" has to become a thing.

This is why I distrust ideas that have only ever lived in my head. Until I've tried to say them, they're suspicion, not knowledge.

See: [[the-collector-fallacy]], [[how-i-take-notes]]`, []string{"writing", "philosophy"}, 30)

	a6, _ := insertNote(aliceID, "alice", "", "reading-list-2026", `Currently working through:

- *How to Take Smart Notes* — Ahrens. Mostly Zettelkasten apologetics, but the chapter on writing is excellent.
- *The Information* — Gleick. A history of communication from drums to Shannon.
- *Seeing Like a State* — Scott. Why top-down planning fails. Surprisingly relevant to software.

Abandoned: two novels I won't name. The [[the-collector-fallacy]] applies to books too.`, []string{"reading", "books"}, 18)

	// ── Bob: programming, zettelkasten ──────────────────────────────────────

	b5, _ := insertNote(bobID, "bob", "programming", "interfaces-not-inheritance", `Go got this right: small interfaces, no inheritance.

A type satisfies an interface by having the methods. No "implements" keyword. No taxonomy to maintain. The interface is just a contract, defined where it's used.

This means interfaces stay tiny — usually 1 or 2 methods — because nobody is tempted to bundle unrelated concerns into a base class.

The result: composition wins. Code is easier to test because the seams are everywhere.

See: [[go-concurrency-patterns]], [[@dave/on-abstraction]]`, []string{"programming", "go", "design"}, 50)

	b6, _ := insertNote(bobID, "bob", "", "debugging-as-search", `Debugging is search through a state space. The space is huge, and most of it is irrelevant.

Good debuggers are good at **pruning**:
- Bisect: cut the space in half each step (git bisect, binary search through commits)
- Minimize: shrink the input until the bug disappears, then bring it back one piece at a time
- Doubt everything: the bug is rarely where you first looked

The worst debugging move: re-reading the same code expecting it to look different.`, []string{"programming", "debugging"}, 12)

	// ── Carol: music, creativity ─────────────────────────────────────────────

	c3, _ := insertNote(carolID, "carol", "", "practice-vs-playing", `Practice is the part that's uncomfortable. Playing is the part that's fun.

If your "practice" session feels like playing, you're not practicing. You're just running scales you already know, songs you already play well.

Practice means working at the edge of what you can do, slowly, with attention. It is boring. It is the only thing that produces growth.

Related: [[transcription-practice]]`, []string{"music", "practice", "discipline"}, 84)

	c4, _ := insertNote(carolID, "carol", "creativity", "constraints-are-creative", `Give yourself unlimited choices and you'll freeze. Give yourself three notes and a 4-bar phrase and you'll find ideas you'd never have found otherwise.

Constraints are not limitations on creativity — they are the **substrate** of it. A blank page is paralysis. A blank page with a rule is a starting point.

This is why writing in someone else's form (a sonnet, a fugue, a haiku) often produces better work than "write whatever you want."

Related: [[transcription-practice]], [[@alice/writing-is-thinking]]`, []string{"creativity", "music", "writing"}, 40)

	// ── Dave: math, science ──────────────────────────────────────────────────

	d3, _ := insertNote(daveID, "dave", "math", "shannon-information", `Information is **surprise**. Shannon defined it precisely: the information content of an event is -log(p), where p is its probability.

Three consequences:
- Certain events carry zero information (you already knew).
- Rare events carry a lot (a coin landing on its edge tells you something).
- The average information of a distribution is its **entropy**.

This frames data compression: encode common symbols in few bits, rare symbols in many. Huffman coding falls out of this directly.

See: [[relational-model]], [[on-abstraction]]`, []string{"math", "information-theory"}, 56)

	d4, _ := insertNote(daveID, "dave", "", "bayes-as-updating", `Bayes' theorem isn't really about probability. It's about **how to update beliefs in light of evidence**.

Start with a prior — what you believed before. See evidence. The posterior is your new belief, weighted by how surprising the evidence would be under each hypothesis.

The mistake people make: treating their prior as if it were the truth. The mistake people also make: throwing away the prior because new evidence arrived.

Good thinking is calibrated updating. It is also rare.`, []string{"math", "epistemology"}, 20)

	// ── Eve: design, typography ─────────────────────────────────────────────

	e1, _ := insertNote(eveID, "eve", "", "typography-is-invisible", `Good typography is the typography you don't notice.

The moment you think "oh, nice font" — something is wrong. The text has called attention to itself rather than to its meaning.

Bad typography is loud. Good typography disappears into the reading experience and leaves only the ideas.

This is true of most design. The best UI is the one you don't remember using.

See: [[grids-and-rhythm]], [[@dave/on-abstraction]]`, []string{"design", "typography"}, 90)

	e2, _ := insertNote(eveID, "eve", "design", "grids-and-rhythm", `A grid is a promise to the reader: things in similar places mean similar things.

Break the grid only with intent. Every deviation is a signal — make sure it's the signal you mean.

Rhythm matters too: consistent spacing creates the same comfort that meter creates in poetry. Inconsistent spacing reads as anxiety even when the content is calm.

Related: [[typography-is-invisible]]`, []string{"design", "layout"}, 44)

	e3, _ := insertNote(eveID, "eve", "design", "color-as-information", `Color is the most overused tool in design.

A useful test: convert your design to grayscale. Does the hierarchy still hold? If not, you're relying on color to do work it shouldn't be doing alone.

Color should reinforce structure, not create it. Accessibility constraints (red/green colorblindness affects ~8% of men) are not just compliance — they expose where your design was leaning on color as a crutch.

See: [[grids-and-rhythm]]`, []string{"design", "color", "accessibility"}, 16)

	// ── Frank: gardening, slow living ───────────────────────────────────────

	f1, _ := insertNote(frankID, "frank", "", "garden-notebook-spring", `Tomatoes in by mid-April — too early last year, lost six to a late frost. The almanac says May 5 for this zone but the almanac doesn't know my yard.

Basil started indoors three weeks ago. Leggy. Need a stronger light next year.

Crop rotation working: where the brassicas were last year, the beans are thriving. Nitrogen fixers earning their keep.

Related: [[seed-saving-notes]]`, []string{"gardening", "spring"}, 70)

	f2, _ := insertNote(frankID, "frank", "", "seed-saving-notes", `The seeds you save are adapted to your soil, your water, your sun. The seeds you buy are adapted to a greenhouse in California.

Five years of saving my own tomato seeds and the plants are visibly hardier than the catalog stock. Same variety. Different lineage.

This is evolution operating on a timescale you can watch.

See: [[garden-notebook-spring]], [[@dave/bayes-as-updating]]`, []string{"gardening", "seeds"}, 34)

	f3, _ := insertNote(frankID, "frank", "philosophy", "on-patience", `A garden teaches patience because it has to. You cannot rush a tomato.

But patience isn't passivity. The gardener is busy — every day there's something to water, prune, weed, observe. Patience just means accepting that the *result* arrives on its own schedule, not yours.

Most things worth doing work this way. Writing. Learning an instrument. Raising children.

See: [[@carol/practice-vs-playing]], [[@alice/writing-is-thinking]]`, []string{"philosophy", "gardening"}, 8)

	// ── Social graph ─────────────────────────────────────────────────────────

	// Alice follows bob, carol, eve
	follow(aliceID, bobID)
	follow(aliceID, carolID)
	follow(aliceID, eveID)
	// Bob follows alice, dave, frank
	follow(bobID, aliceID)
	follow(bobID, daveID)
	follow(bobID, frankID)
	// Carol follows alice, eve, frank
	follow(carolID, aliceID)
	follow(carolID, eveID)
	follow(carolID, frankID)
	// Dave follows bob, carol, eve
	follow(daveID, bobID)
	follow(daveID, carolID)
	follow(daveID, eveID)
	// Eve follows alice, carol, dave
	follow(eveID, aliceID)
	follow(eveID, carolID)
	follow(eveID, daveID)
	// Frank follows alice, carol
	follow(frankID, aliceID)
	follow(frankID, carolID)

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
	like(aliceID, e1) // alice likes eve's typography note
	like(aliceID, f3) // alice likes frank's patience note
	like(bobID, d3)   // bob likes dave's shannon-information
	like(bobID, e2)   // bob likes eve's grids
	like(carolID, c4) // carol self-like on constraints
	like(carolID, f3) // carol likes frank's patience
	like(carolID, e3) // carol likes eve's color note
	like(daveID, a5)  // dave likes alice's writing-is-thinking
	like(daveID, b5)  // dave likes bob's interfaces note
	like(eveID, a3)   // eve likes alice's linking ideas
	like(eveID, c1)   // eve likes carol's music theory
	like(eveID, d2)   // eve likes dave's abstraction
	like(frankID, a4) // frank likes alice's collector fallacy
	like(frankID, c3) // frank likes carol's practice note
	like(frankID, d4) // frank likes dave's bayes note

	_ = a1; _ = a2; _ = a3; _ = a4; _ = a5; _ = a6
	_ = b1; _ = b2; _ = b3; _ = b4; _ = b5; _ = b6
	_ = c1; _ = c2; _ = c3; _ = c4
	_ = d1; _ = d2; _ = d3; _ = d4
	_ = e1; _ = e2; _ = e3
	_ = f1; _ = f2; _ = f3

	return tx.Commit()
}
