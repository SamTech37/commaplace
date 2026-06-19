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
	graceID, _ := insertUser("grace", "grace@dev.local")
	henryID, _ := insertUser("henry", "henry@dev.local")
	irisID, _ := insertUser("iris", "iris@dev.local")
	jamesID, _ := insertUser("james", "james@dev.local")
	kaiID, _ := insertUser("kai", "kai@dev.local")
	lunaID, _ := insertUser("luna", "luna@dev.local")
	marcusID, _ := insertUser("marcus", "marcus@dev.local")
	ninaID, _ := insertUser("nina", "nina@dev.local")
	oscarID, _ := insertUser("oscar", "oscar@dev.local")
	petraID, _ := insertUser("petra", "petra@dev.local")
	quinnID, _ := insertUser("quinn", "quinn@dev.local")
	rafaelID, _ := insertUser("rafael", "rafael@dev.local")
	saraID, _ := insertUser("sara", "sara@dev.local")
	taroID, _ := insertUser("taro", "taro@dev.local")

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

	// ── Alice: stress-test note (all link types) ────────────────────────────

	aStress, _ := insertNote(aliceID, "alice", "", "link-topology", `# Link Topology in Knowledge Graphs

Every note exists in relation to others. The question is not whether to link, but *how densely* and *in what pattern*.

## Same-vault connections

A flat list of notes linked by topic is a weak graph — many nodes, few edges, low clustering. The interesting structures are:

- **Hubs**: notes so well-connected they become navigation landmarks. See [[on-linking-ideas]] and [[writing-is-thinking]].
- **Bridges**: notes that connect otherwise-separate clusters. [[the-collector-fallacy]] bridges the "workflow" cluster to the "philosophy" cluster.
- **Orphans**: notes that exist but are never linked. A dangerous state. An orphan note has no entry path and no exit path — it's unreachable except by search.

## Cross-vault topology

The interesting case is cross-vault linking. When [[@bob/zettelkasten]] links here and I link back, we've created a **bidirectional cross-vault edge**. This is structurally different from a citation — it's a live connection that can be traversed in either direction.

[[@carol/on-music-theory-as-a-language]] models this: a vocabulary is a graph where the meaning of each term depends on its neighbors. Notes work the same way.

[[@dave/on-abstraction]] gives the engineering angle: a good abstraction hides the right things. A link is an abstraction — it hides the full text of the target and exposes only a pointer. When the abstraction leaks (dead link, stale reference), you're forced to reconstruct what was hidden.

From linguistics: [[@grace/meaning-through-reference]] argues that meaning is always relational, never intrinsic. A link is the typographical enactment of that claim.

From architecture: [[@nina/city-as-hypertext]] — the city as a space where paths between nodes determine meaning more than the nodes themselves. Vaults work the same way.

## Degree distribution

Real knowledge graphs are scale-free: a few notes accumulate most of the links (power law distribution), while most notes have only one or two connections. This is not a problem to fix — it's an emergent property of how ideas cluster.

The implication: don't try to make all notes equally connected. Let hubs form naturally.`, []string{"knowledge", "graphs", "philosophy"}, 14)

	// ── Grace: linguistics, semiotics ────────────────────────────────────────

	g1, _ := insertNote(graceID, "grace", "", "meaning-through-reference", `Frege's distinction between *Sinn* (sense) and *Bedeutung* (reference) is the founding move of analytic philosophy of language.

Two expressions can refer to the same object while having different senses: "the morning star" and "the evening star" both refer to Venus, but they present it differently.

This matters for notes: a [[wikilink]] is a reference. It points. But the *sense* — what the link means in context — depends on the surrounding text.

A link to [[@alice/link-topology]] from a note about graph theory reads differently than the same link from a note about writing practice. Same reference, different sense.`, []string{"linguistics", "philosophy"}, 160)

	g2, _ := insertNote(graceID, "grace", "", "linguistic-relativity-weak", `The strong Sapir-Whorf hypothesis (language *determines* thought) is almost certainly false. Prelinguistic infants and non-human animals solve spatial and numerical problems without language.

The weak version — that language *influences* thought — has better evidence. Speakers of languages with grammatical gender show subtly different associations with gendered nouns. Languages with absolute spatial frames ("the cup is north of the plate") produce better navigators than relative-frame languages ("the cup is to the left").

The implication for note-taking: the vocabulary you use to organize ideas subtly shapes what connections you can make. [[@carol/on-music-theory-as-a-language]] makes this point about music theory as vocabulary.`, []string{"linguistics", "cognition"}, 148)

	g3, _ := insertNote(graceID, "grace", "", "indexicality", `An *indexical* expression is one whose meaning depends on context of utterance: "here," "now," "I," "this."

Most language is not indexical — "photosynthesis" means the same thing regardless of who says it or when. But indexicals are the connective tissue of communication.

Notes have their own indexicals. "See above" is an indexical — it only works in a specific document layout. Wiki links are closer to proper names, but they can break (dead links) in a way proper names can't.

The design question: should a knowledge system try to eliminate indexicality (canonical IDs for everything) or embrace it (links as contextual gestures)?`, []string{"linguistics", "semiotics"}, 120)

	g4, _ := insertNote(graceID, "grace", "", "metaphor-as-cognition", `Lakoff and Johnson's *Metaphors We Live By* (1980): most abstract thought is metaphorical, built on a small set of primary metaphors grounded in embodied experience.

"Argument is war" — we *attack* positions, *defend* claims, *shoot down* ideas. "Time is money" — we *spend*, *waste*, *invest* it.

These are not decorative — they shape which inferences are available. If argument is war, then there must be winners and losers. If argument is instead *collaborative construction*, different inferences follow.

Related: [[@alice/writing-is-thinking]], [[meaning-through-reference]]`, []string{"linguistics", "cognition", "philosophy"}, 90)

	g5, _ := insertNote(graceID, "grace", "", "writing-systems-history", `Writing is not transcribed speech. The earliest writing systems — Sumerian cuneiform, Egyptian hieroglyphics — emerged independently to solve administrative problems: recording grain tallies, tracking temple inventories.

The phonetic principle (representing sounds rather than meanings) appeared later and spread unevenly. Chinese writing never made the full phonetic turn; it retains a morphosyllabic structure where most characters represent meaning-sound combinations.

Alphabets are a late, local invention. The Greek alphabet (adding vowels to the Phoenician abjad) is roughly 2,800 years old. The history of writing is mostly not-alphabets.`, []string{"linguistics", "history"}, 100)

	g6, _ := insertNote(graceID, "grace", "", "etymology-as-archaeology", `Etymology is synchronic archaeology. The layers of a word's history are visible in its current form if you know what to look for.

"Salary" from *salarium* — possibly the salt ration given to Roman soldiers (though some dispute this). "Disaster" from *dis* (bad) + *astrum* (star): calamity attributed to unfavorable stellar alignment. "Trivial" from *trivium* (crossroads): the knowledge of the crossroads, common to all, as opposed to specialized knowledge.

These are not just curiosities. Etymology reveals which concepts were important enough to name first, what domains generated productive metaphors, and how ideas migrated between cultures.

See: [[metaphor-as-cognition]]`, []string{"linguistics", "history"}, 72)

	// ── Henry: history, military ──────────────────────────────────────────────

	h1, _ := insertNote(henryID, "henry", "", "logistics-wins-wars", `Napoleon said an army marches on its stomach. The more precise version: the army that solves logistics wins the war that the other army is still trying to fight.

The German failure in Russia (1941) was fundamentally a logistics failure. The Wehrmacht's supply lines couldn't sustain a campaign of that depth; the tactical brilliance of the armored spearheads outran their fuel. The Soviet rail gauge being different from the European gauge wasn't an accident — the Tsars had designed it that way.

The lesson generalizes. Most catastrophic failures are logistics failures dressed up as strategy failures.

See: [[@alice/link-topology]] — the topology of a supply network has the same structural properties as a knowledge graph.`, []string{"history", "military"}, 180)

	h2, _ := insertNote(henryID, "henry", "", "fog-of-war-clausewitz", `Clausewitz called it *Nebel des Krieges* — the fog of war. Everything in war operates under uncertainty: where is the enemy, what are their intentions, what do they know about you.

The crucial insight: the fog is irreducible. You cannot wait for certainty because certainty never arrives. The competent commander acts on incomplete information while continuously updating as new information comes in.

This is [[@dave/bayes-as-updating]] applied to violence. Prior + evidence → posterior. The prior is your intelligence estimate. The evidence is what your scouts report. The posterior is your updated picture of the battlefield.`, []string{"history", "military", "epistemology"}, 150)

	h3, _ := insertNote(henryID, "henry", "", "the-100-years-war-myth", `The Hundred Years' War (1337–1453) was not a single continuous conflict. It was a series of distinct wars, truces, and peace treaties, separated by decades of relative quiet.

The "hundred years" framing is retrospective — contemporaries didn't experience it as one thing. Historians impose the narrative continuity.

This is a general problem in history: the categories we use to organize events (wars, empires, periods, revolutions) are created after the fact and may not map onto how people experienced their own time.

See: [[@bob/debugging-as-search]] — debugging and historical reconstruction share a structure: working backward from effect to cause through incomplete evidence.`, []string{"history", "historiography"}, 120)

	h4, _ := insertNote(henryID, "henry", "", "roman-engineering-scale", `The Romans built ~80,000 km of roads. Not because they loved roads, but because empire requires logistics, and logistics requires infrastructure.

The road network made possible: rapid troop movement, the cursus publicus (state postal service), commercial integration of a territory spanning from Scotland to Mesopotamia.

When the Western Empire collapsed, the roads didn't disappear — they continued to be used for centuries, slowly degrading without maintenance. The "Dark Ages" in part means "the era when nobody could maintain Roman infrastructure."

Related: [[logistics-wins-wars]]`, []string{"history", "engineering"}, 105)

	h5, _ := insertNote(henryID, "henry", "", "translation-of-empire", `The concept of *translatio imperii* — the transfer of imperial authority from one polity to another — was the dominant framework for legitimate rule in medieval Europe.

The Holy Roman Empire claimed to be the legitimate successor to Rome. Charlemagne's coronation in 800 CE was framed as a *translatio*. The Tsars of Russia called Moscow the "Third Rome."

What's interesting is that the framework required the previous empire to have ended. Continuity would have meant subordination. The claim was always: we are what they were, but better and renewed.`, []string{"history", "politics"}, 88)

	h6, _ := insertNote(henryID, "henry", "", "the-printing-press-speed", `Gutenberg's press (c. 1440) didn't immediately democratize knowledge. The first printed books were expensive luxury items, indistinguishable from manuscripts in prestige.

The disruption happened 50 years later, when printers discovered that cheap pamphlets and broadsheets were more profitable than Bibles. The Reformation was a media event — Luther's 95 Theses spread through Germany in two weeks, a speed previously impossible.

The lesson: a new medium's political effects often lag its invention by decades, and emerge through uses its inventors never intended.

See: [[@rafael/distribution-beats-product]]`, []string{"history", "media", "technology"}, 95)

	// ── Iris: ecology, biology ────────────────────────────────────────────────

	i1, _ := insertNote(irisID, "iris", "", "keystone-species", `A keystone species has an outsized effect on its ecosystem relative to its biomass. Remove it and the ecosystem transforms.

Sea otters are the classic example. Otters eat sea urchins. Urchins eat kelp. Without otters, urchin populations explode, kelp forests collapse, and dozens of dependent species disappear.

The concept generalizes. In social systems, certain nodes have keystone-like properties — their removal causes cascading reorganization. [[@alice/link-topology]] identifies "hubs" in knowledge graphs; keystone species are the ecological analog.`, []string{"ecology", "biology"}, 155)

	i2, _ := insertNote(irisID, "iris", "", "mycorrhizal-networks", `Roughly 90% of plant species form symbiotic relationships with mycorrhizal fungi. The fungal hyphae extend the plant's effective root surface area by orders of magnitude, trading mineral uptake for carbohydrates.

In forests, mycorrhizal networks connect trees of the same species and sometimes different species. Older trees ("mother trees") appear to preferentially feed seedlings through these networks — though the mechanism and the extent of intentionality (if any) is contested.

The popular "wood wide web" framing is evocative but may overstate the cooperative character of what is ultimately a network of resource exchanges under competitive pressure.`, []string{"ecology", "biology"}, 130)

	i3, _ := insertNote(irisID, "iris", "", "succession-theory", `Ecological succession is the process by which a community changes over time toward a stable end state (the "climax community"). Pioneer species colonize disturbed ground; they're replaced by others; eventually a mature community establishes.

The classical model (Clements, early 20th century) treated succession as deterministic and directional — there is a single climax community for any given climate. This is too tidy. The endpoint depends on which species arrive first, in what order, and under what conditions.

The lesson: path dependency matters. History matters. The ecosystem you get depends on the sequence of events, not just the current conditions.

Related: [[keystone-species]], [[@dave/relational-model]]`, []string{"ecology", "biology"}, 110)

	i4, _ := insertNote(irisID, "iris", "", "island-biogeography", `MacArthur and Wilson's *The Theory of Island Biogeography* (1967): species richness on an island is a function of island size (larger = more species) and isolation (more isolated = fewer species).

The counterintuitive finding: island species counts are roughly constant over time even though individual species go extinct. Extinction is balanced by immigration. The equilibrium is dynamic.

This has been applied to habitat fragments — a forest patch surrounded by farmland is functionally an island. The smaller and more isolated the patch, the fewer species it can support.

Related: [[succession-theory]], [[@sara/tipping-points]]`, []string{"ecology", "biology", "conservation"}, 95)

	i5, _ := insertNote(irisID, "iris", "", "phenotypic-plasticity", `Genotype does not determine phenotype uniquely. The same genotype can produce different phenotypes depending on environmental conditions — this is phenotypic plasticity.

The classic example: a single caterpillar species produces different adult wing patterns depending on the temperature it experienced as a larva. The gene for "what wing pattern to produce" interacts with the environment.

This complicates the nature/nurture framing. It's not genes *vs.* environment but genes *and* environment, interacting in ways that can be context-dependent, reversible, and heritable (via epigenetic mechanisms).`, []string{"biology", "genetics"}, 85)

	i6, _ := insertNote(irisID, "iris", "", "biomimicry-limits", `Biomimicry — designing technology by copying natural systems — is a productive but often overstated framework.

What nature optimizes for is *reproductive success under historical conditions*, not human utility, efficiency, or aesthetic quality. Shark skin reduces drag in water; that doesn't make it optimal for airplane surfaces.

The useful version of biomimicry: nature as a proof-of-concept library. If something exists in biology, it's possible. That narrows the design space. But "possible" is a long way from "optimal for your application."

See: [[@marcus/movement-efficiency]]`, []string{"biology", "engineering", "design"}, 78)

	// ── James: economics, behavioral ─────────────────────────────────────────

	j1, _ := insertNote(jamesID, "james", "", "revealed-preference", `Revealed preference theory (Samuelson, 1938): you don't need to know what people say they prefer — observe what they *choose*, and you've revealed their preference.

The insight was a methodological breakthrough: economics could be freed from introspection and psychology. Choices are observable; mental states aren't.

The problem: revealed preference can't distinguish between a choice that reflects stable preferences and one that reflects a mistake, a misunderstanding, or a choice under constraint. The theory treats all choices as preference-revealing, which is too permissive.

See: [[@dave/bayes-as-updating]] — updating beliefs in light of choices is structurally similar.`, []string{"economics", "philosophy"}, 150)

	j2, _ := insertNote(jamesID, "james", "", "loss-aversion", `Kahneman and Tversky found that losses loom roughly twice as large as equivalent gains — losing $100 feels about as bad as gaining $200 feels good.

The evolutionary explanation: in an ancestral environment, losses are often irreversible (dead is dead), while missed gains can be recovered. Asymmetric sensitivity to loss is adaptive.

The design implication: if you want to change behavior, framing something as preventing a loss is typically more effective than framing it as achieving a gain. "Don't lose your savings" beats "grow your wealth."

Related: [[revealed-preference]], [[@alice/the-collector-fallacy]]`, []string{"economics", "psychology", "behavioral"}, 120)

	j3, _ := insertNote(jamesID, "james", "", "coordination-problems", `Many social problems are coordination problems: individually rational behavior produces collectively bad outcomes.

The prisoner's dilemma is the canonical form. But real coordination problems are usually more complex: multiple equilibria, asymmetric payoffs, repeated games, reputation effects.

Traffic is a coordination problem. Money is a coordination solution — it solves the double coincidence of wants problem in barter by providing a universally accepted medium.

The interesting question: what mechanisms (norms, institutions, technology) solve which classes of coordination problems? Not all coordination failures need the same fix.

See: [[revealed-preference]], [[@petra/law-as-coordination]]`, []string{"economics", "game-theory"}, 108)

	j4, _ := insertNote(jamesID, "james", "", "price-signals", `Prices are the most efficient known mechanism for aggregating dispersed information. Hayek's point (1945): no central planner can know what millions of people know about local conditions, needs, and constraints. Prices do this automatically.

The corollary: when prices are distorted (price controls, subsidies, externalities not priced in), information is lost. Economic actors make decisions based on false signals.

Climate change is an externality problem: carbon isn't priced, so the market sends incorrect signals about the cost of carbon-intensive activities. The price mechanism isn't broken — it's just missing a variable.

Related: [[coordination-problems]], [[@sara/carbon-pricing]]`, []string{"economics", "climate"}, 100)

	j5, _ := insertNote(jamesID, "james", "", "long-run-short-run", `In economics, "the long run" is the time horizon over which all inputs are variable. There are no fixed costs in the long run — you can change factories, labor forces, technologies.

The confusion: "long run" and "short run" are analytical categories, not fixed time periods. For a street vendor, "long run" might be a week. For a steel mill, decades.

Keynes: "In the long run we are all dead." The irony is that Keynes cared deeply about the long run — the aphorism was aimed at economists who used long-run equilibrium to dismiss short-run suffering.`, []string{"economics"}, 88)

	j6, _ := insertNote(jamesID, "james", "", "commons-and-tragedy", `Hardin's "Tragedy of the Commons" (1968): shared resources are overexploited because each user captures the full benefit of their use while sharing the costs with everyone.

The paper was influential and partly wrong. Elinor Ostrom (Nobel 2009) documented dozens of cases where communities successfully managed commons without privatization or central regulation — through local norms, monitoring, and graduated sanctions.

The tragedy occurs when communities lack the ability to communicate, monitor, and exclude. It's not an inevitable property of shared resources.

Related: [[coordination-problems]]`, []string{"economics", "ecology"}, 82)

	// ── Kai: photography, visual arts ────────────────────────────────────────

	k1, _ := insertNote(kaiID, "kai", "", "the-decisive-moment", `Cartier-Bresson's *l'instant décisif*: photography is the simultaneous recognition of the significance of an event and the precise organization of forms that give it proper expression.

The phrase is often misread as "act fast, shoot instinctively." What Cartier-Bresson meant was closer to: you must understand geometry well enough that when the moment arrives, you already know where to be standing.

Preparation + patience + geometry. The decisive moment is the outcome of everything that came before it.

See: [[@carol/practice-vs-playing]] — the same structure: visible preparation, invisible technique.`, []string{"photography", "art"}, 145)

	k2, _ := insertNote(kaiID, "kai", "", "zone-system-ansel-adams", `Ansel Adams developed the Zone System to translate the photographer's visualization of a scene into precise exposure and development decisions.

The scale runs from Zone 0 (pure black, no detail) to Zone X (pure white, no detail), with Zone V as middle gray (18% reflectance). The key move: you decide in advance which zone a critical shadow or highlight should fall in, then expose and develop accordingly.

Digital sensors have made the math easier but the underlying logic is unchanged: you are translating luminance values in the world into tonal values in the print.

Related: [[the-decisive-moment]], [[@eve/color-as-information]]`, []string{"photography", "technique"}, 115)

	k3, _ := insertNote(kaiID, "kai", "", "vernacular-photography", `Most photographs ever made are not art. They are records: birthdays, vacations, faces of people who mattered to someone.

Vernacular photography — the snapshot, the family album, the accidental image — has its own aesthetic that professional photography often struggles to imitate. The blurriness, the cropping accidents, the red-eye: these are not mistakes but markers of a particular mode of attention.

Photographers like Martin Parr and Wolfgang Tillmans have absorbed vernacular aesthetics deliberately. The result is work that looks effortless because it has internalized constraints that "correct" photography works hard to avoid.`, []string{"photography", "art", "culture"}, 100)

	k4, _ := insertNote(kaiID, "kai", "", "light-temperature-notes", `Color temperature is measured in Kelvin. Counterintuitively, "warm" light (candles, sunset) has low color temperature (~2700K), while "cool" light (overcast sky) has high color temperature (~7000K).

The Kelvin scale runs backward from intuition because the underlying physics refers to the color a black body radiator emits at that temperature. A cooler star is redder; a hotter star is bluer.

For shooting: tungsten (3200K) renders daylight as blue. Fluorescent (4000K) renders tungsten as orange. White balance corrects for the light source; the choice of whether to correct is aesthetic.`, []string{"photography", "technique"}, 88)

	k5, _ := insertNote(kaiID, "kai", "", "documentary-ethics", `The camera is not neutral. Every photograph involves choices: what to include in the frame, when to press the shutter, how to present the result.

Documentary photography has a trust contract with its subjects and viewers: this is what I saw; I have not materially altered it. The ethics questions are about where the line is. Cropping: fine. Removing an object with generative AI: not fine. But adding flash to an unlit scene, which changes the mood and appearance of everything: common practice, rarely disclosed.

The viewer can't always tell. The photographer knows. That asymmetry is where the ethics live.`, []string{"photography", "ethics"}, 92)

	// ── Luna: cooking, fermentation ───────────────────────────────────────────

	l1, _ := insertNote(lunaID, "luna", "", "maillard-reaction", `The Maillard reaction is responsible for the browning of meat, bread crusts, coffee, and dozens of other foods. It's a cascade of reactions between amino acids and reducing sugars, producing hundreds of distinct flavor and aroma compounds.

It requires heat above ~140°C (284°F). This is why boiling doesn't brown food — water's boiling point (100°C) is too low. You need to get the surface dry and hot enough.

The flavor difference between a seared steak and a boiled one is almost entirely Maillard products. "Caramelization" (pure sugar browning) is a separate reaction, though they often occur together.`, []string{"cooking", "chemistry"}, 135)

	l2, _ := insertNote(lunaID, "luna", "", "fermentation-as-preservation", `Fermentation predates refrigeration by millennia. It's a method of preservation: beneficial microorganisms outcompete pathogens, produce acids or alcohol that inhibit spoilage, and in the process transform the food.

Sauerkraut, kimchi, cheese, salami, miso, soy sauce, wine, beer, bread — fermented foods are most of the interesting things humans eat.

The mechanism: you create conditions (salt concentration, oxygen exclusion, temperature) that favor the microorganisms you want. They do the work. Your job is to create the right environment, then get out of the way.

Related: [[@iris/mycorrhizal-networks]] — the parallel with fungal networks is closer than it looks.`, []string{"cooking", "fermentation", "microbiology"}, 120)

	l3, _ := insertNote(lunaID, "luna", "", "salt-ratios", `Salt in cooking is not primarily for flavor — it's for texture and preservation.

In bread: salt strengthens gluten structure, slows fermentation (controlling yeast), and suppresses off-flavors. Remove the salt and the dough becomes slack and over-ferments quickly.

In meat: salt draws out moisture initially, then the meat reabsorbs the brine it created. Dry-brined meat has better surface dryness (good for browning) and seasons more evenly.

In fermentation: 2–3% salt by weight of vegetables creates conditions where Lactobacillus thrives and competing bacteria can't. The salt isn't in the final product as "saltiness" — it's the scaffold that lets fermentation happen safely.`, []string{"cooking", "technique"}, 108)

	l4, _ := insertNote(lunaID, "luna", "", "knife-sharpening-notes", `A sharp knife is a safer knife. The force required to cut increases as the edge dulls; that force is unpredictable and can slip.

Sharpening removes metal to create a new edge. Honing (steel or ceramic rod) realigns the existing edge without removing metal. You should hone frequently and sharpen rarely.

Angle matters more than the tool: a consistent 15–20° angle per side produces a good kitchen edge. Higher angles (25°+) are more durable but less sharp. Lower angles are sharper but fragile.

The test: the paper test tells you if the edge cuts cleanly. The arm hair test tells you if the edge is sharp enough to shave. Kitchen knives don't need to shave hair.`, []string{"cooking", "technique", "tools"}, 95)

	l5, _ := insertNote(lunaID, "luna", "", "umami-fifth-taste", `Umami was proposed as a fifth basic taste by Kikunae Ikeda in 1908, who isolated glutamate as the compound responsible for the savory quality of kombu dashi.

Umami compounds: glutamate (free amino acid), inosinate (from meat/fish), guanylate (from dried mushrooms). The last two synergize strongly with glutamate — combining them multiplies the perceived intensity far beyond additive.

This is why dashi (glutamate from kombu + inosinate from katsuobushi) is so effective. It's also why parmesan on tomato sauce works: both are high in glutamate, and the combination is 8× more intense than either alone.`, []string{"cooking", "chemistry", "food-science"}, 102)

	l6, _ := insertNote(lunaID, "luna", "", "mise-en-place-as-practice", `*Mise en place* means "everything in its place." In professional kitchens, it describes the preparation work done before service — chopped aromatics, measured spices, prepped proteins, organized tools.

The principle extends: the cognitive overhead of cooking is reduced by separating preparation from execution. When you cook and prep simultaneously, you make errors. When you prep completely before cooking, you can focus on technique.

[[@quinn/cognitive-load-theory]] maps onto this directly. Mise en place is working memory management: externalize everything that doesn't need to be in your head at the moment of action.`, []string{"cooking", "productivity", "workflow"}, 88)

	// ── Marcus: sports science, movement ──────────────────────────────────────

	m1, _ := insertNote(marcusID, "marcus", "", "movement-efficiency", `Efficient movement minimizes the metabolic cost of achieving a task. In running, this means minimizing ground contact time, maximizing elastic energy return, and reducing vertical oscillation (energy wasted moving up and down).

Elite distance runners share a characteristic pattern: short ground contact, slight forward lean, cadence around 180 steps/minute, minimal heel strike. These are consequences of efficiency, not causes — you don't get efficient by copying the pattern; you get the pattern by becoming efficient.

The intervention that works: remove restriction (bad shoes, weak hips, tight calves) and let the body find efficiency on its own.`, []string{"sports-science", "movement", "biomechanics"}, 140)

	m2, _ := insertNote(marcusID, "marcus", "", "progressive-overload", `Progressive overload is the principle that adaptation requires progressively increasing demand. If you lift the same weight every session, you've adapted to that weight — there's no stimulus for further change.

The dose-response curve is nonlinear. Early training: large gains from small doses. Advanced training: small gains from large doses. This is why beginner programs work fast and elite programs require meticulous planning.

The practical consequence: tracking is not optional for advanced athletes. You cannot feel a 2% load increase. You have to measure it.

Related: [[@carol/practice-vs-playing]], [[periodization-notes]]`, []string{"sports-science", "training"}, 118)

	m3, _ := insertNote(marcusID, "marcus", "", "periodization-notes", `Periodization is structured variation in training load over time. The body adapts to stress, then requires new stress to adapt further — but it also requires recovery.

Basic structure: mesocycle (3–6 week block of progressive loading) → deload week (reduced volume, maintained intensity) → new mesocycle at higher baseline.

Hans Selye's General Adaptation Syndrome provides the biological basis: stress → alarm → resistance (adaptation) → exhaustion. Periodization manages the cycle to stay in the resistance phase without reaching exhaustion.`, []string{"sports-science", "training"}, 95)

	m4, _ := insertNote(marcusID, "marcus", "", "sleep-and-performance", `Sleep is the primary recovery mechanism for athletes. During slow-wave sleep, growth hormone peaks. During REM sleep, motor patterns are consolidated.

The effect size is large: restricting sleep to 6 hours/night for two weeks produces cognitive impairment equivalent to 24 hours of total sleep deprivation — and subjects don't perceive themselves as impaired.

For athletic performance: reaction time, accuracy, sprint speed, and endurance all decline measurably with sleep restriction. No recovery modality (ice baths, massage, nutrition) compensates for inadequate sleep.

See: [[progressive-overload]]`, []string{"sports-science", "sleep", "recovery"}, 102)

	m5, _ := insertNote(marcusID, "marcus", "", "proprioception-training", `Proprioception is the sense of body position and movement in space — the information from joints, muscles, and tendons that tells you where your limbs are without looking.

It degrades with injury (especially ankle and knee injuries), with age, and with disuse. It can be trained: balance exercises, unstable surface training, reactive drills.

Why it matters: most injuries occur when a joint is in an unexpected position under load. Athletes with good proprioception catch these situations before they become injuries. It's a trainable injury-prevention system.`, []string{"sports-science", "rehabilitation"}, 88)

	// ── Nina: architecture, urban planning ───────────────────────────────────

	n1, _ := insertNote(ninaID, "nina", "", "city-as-hypertext", `A city is a hypertext. Every building is a node; every street is a link. The meaning of a place is constituted by what you can reach from it and what can reach it.

Jane Jacobs understood this without the vocabulary: the vitality of a city block comes from its connections — to other blocks, to transit, to density. An isolated block, however well-designed as an object, dies.

[[@alice/link-topology]] describes the same phenomenon in knowledge graphs. The parallel isn't metaphorical — both are networks where value is relational, not intrinsic to nodes.`, []string{"architecture", "urbanism", "networks"}, 148)

	n2, _ := insertNote(ninaID, "nina", "", "modulor-critique", `Le Corbusier's Modulor (1948) was a system of proportions based on the human body and the golden ratio, intended to produce buildings scaled to human dimensions.

The problems: it was based on a 6-foot (1.83m) man ("a comfortable English detective"), excluding women and shorter populations. The proportions produce visually harmonious relationships but don't necessarily produce ergonomic spaces. And the system is deterministic where human use is improvisational.

The broader lesson: design systems based on an idealized user will always fail the actual users who don't match the ideal.

Related: [[city-as-hypertext]], [[@eve/grids-and-rhythm]]`, []string{"architecture", "design", "history"}, 120)

	n3, _ := insertNote(ninaID, "nina", "", "thermal-mass-passive-design", `Thermal mass is the capacity of a material to absorb, store, and release heat. High thermal mass (concrete, stone, earth) smooths temperature swings — cool spaces stay cool longer, warm spaces stay warm longer.

In passive solar design, thermal mass positioned to receive winter sun charges during the day and releases heat at night. In hot arid climates, thick earthen walls absorb daytime heat that would otherwise enter the interior.

Modern "energy-efficient" buildings often have low thermal mass and high insulation — they're good at preventing heat transfer but poor at buffering temperature swings. The result: HVAC systems have to work hard to compensate.`, []string{"architecture", "sustainability", "engineering"}, 108)

	n4, _ := insertNote(ninaID, "nina", "", "the-eyes-on-the-street", `Jane Jacobs' concept: safe streets have "eyes on the street" — windows, storefronts, active ground floors that mean people are watching what happens.

The corollary: the modernist superblock — towers set in open green space, with dead zones between buildings — eliminates eyes on the street. The "green space" becomes dangerous because nobody is watching.

This was not a theoretical prediction. Pruitt-Igoe (St. Louis), Cabrini-Green (Chicago), and dozens of other public housing projects confirmed it empirically before the buildings were demolished.

Related: [[city-as-hypertext]]`, []string{"architecture", "urbanism", "safety"}, 102)

	n5, _ := insertNote(ninaID, "nina", "", "adaptive-reuse", `Adaptive reuse — converting buildings to uses other than their original purpose — is both more sustainable and often more interesting than demolition and new construction.

The embodied carbon in an existing building (energy used in original construction) is already spent. Demolition wastes it. Reuse retains it.

Beyond sustainability: old buildings have proportions, materials, and spatial qualities that modern construction rarely replicates. The market for loft apartments in converted warehouses, offices in converted factories — this is demand for spatial qualities that only come with age.`, []string{"architecture", "sustainability"}, 88)

	// ── Oscar: film, cinema ───────────────────────────────────────────────────

	o1, _ := insertNote(oscarID, "oscar", "", "kuleshov-effect", `The Kuleshov effect (1920s): audiences read meaning into the juxtaposition of images that isn't present in either image alone.

Kuleshov showed audiences the same neutral face of actor Ivan Mosjoukine followed by different images: a bowl of soup, a dead child, a woman on a couch. Viewers rated Mosjoukine's expression as conveying hunger, grief, or desire respectively — while the face was identical in all three.

Meaning in film is created by editing, not by what's in the frame. The frame provides elements; editing provides the relationship between elements; the viewer's mind constructs meaning from the relationship.

See: [[@alice/link-topology]] — links create meaning that isn't in either linked note alone.`, []string{"film", "editing", "perception"}, 148)

	o2, _ := insertNote(oscarID, "oscar", "", "deep-focus-controversy", `Deep focus photography (Citizen Kane, Gregg Toland) keeps foreground and background in sharp focus simultaneously, allowing complex staging in a single shot without cutting.

André Bazin argued this was more "realistic" and morally superior to montage — it preserved the unity of space and time, leaving the viewer free to decide where to look. Eisenstein and the Soviet montage theorists argued the opposite: reality is not self-interpreting, and the editor's job is to construct meaning, not present material.

Both positions assume cinema should be doing something. They disagree about what.`, []string{"film", "technique", "theory"}, 118)

	o3, _ := insertNote(oscarID, "oscar", "", "sound-design-diegetic", `Sound in film is classified as diegetic (existing in the world of the story) or non-diegetic (existing only for the audience).

A character listening to music on headphones: diegetic. The orchestral score we hear during the same scene: non-diegetic. These categories can be blurred deliberately: a character hears "non-diegetic" music and looks around for its source.

The diegetic/non-diegetic distinction matters because it determines what the character can hear. This structures dramatic irony: the audience knows things (through non-diegetic sound) that characters don't.`, []string{"film", "sound", "theory"}, 100)

	o4, _ := insertNote(oscarID, "oscar", "", "genre-as-contract", `Genre is a contract between film and audience. The slasher film, the romantic comedy, the western — each makes implicit promises about what will happen and how.

Genre conventions can be honored (fulfilling the contract), subverted (making audiences aware of the contract's existence), or violated (breaking the contract in ways that may be interesting or alienating).

The most interesting films often work in the tension between genre expectation and deviation. The audience experiences the deviation against the background of the expected, which wouldn't be possible if the expectation weren't established first.

Related: [[kuleshov-effect]], [[@grace/metaphor-as-cognition]]`, []string{"film", "theory", "narrative"}, 105)

	o5, _ := insertNote(oscarID, "oscar", "", "color-grading-history", `Color grading as a distinct craft emerged from photochemical timing (adjusting the printer lights during film printing) and became a software practice with digital intermediate processes in the early 2000s.

The "teal and orange" look that dominated 2010s blockbusters is a consequence of skin tones (orange) being complementary to teal — pushing these poles apart on the color wheel maximizes contrast while keeping skin tones readable.

The look became a meme, then a cliché, then a target. The reaction was a return to more naturalistic grading — though "naturalistic" is itself an aesthetic choice, not an absence of one.

See: [[@eve/color-as-information]], [[deep-focus-controversy]]`, []string{"film", "color", "technique"}, 95)

	// ── Petra: law, ethics ───────────────────────────────────────────────────

	pe1, _ := insertNote(petraID, "petra", "", "law-as-coordination", `Law is primarily a coordination mechanism. Traffic drives on the right (or left) — which side doesn't matter much; what matters is that everyone chooses the same side.

Contract law makes commitments credible. Property law defines who can use what and when. Criminal law defines which coordination failures (defection from cooperation) carry sanctions.

The view that law is about justice is not wrong, but it's incomplete. Many laws are Schelling points — arbitrary conventions that stick because everyone expects everyone else to follow them.

Related: [[@james/coordination-problems]], [[@alice/link-topology]]`, []string{"law", "philosophy", "game-theory"}, 140)

	pe2, _ := insertNote(petraID, "petra", "", "rule-of-law-components", `The rule of law is often invoked without specifying which rule-of-law properties are meant. The concept has at least four distinct components:

1. **Generality**: laws apply to everyone, including rulers
2. **Publicity**: laws are known in advance
3. **Prospectivity**: laws don't apply retroactively
4. **Stability**: laws don't change arbitrarily

A legal system can have some of these and lack others. China has a relatively stable, publicly known legal code that doesn't bind the Party elite — it lacks generality. Retroactive legislation is common in emergency contexts.

These are independent properties. Conflating them obscures which element is actually missing in any given case.`, []string{"law", "political-theory"}, 118)

	pe3, _ := insertNote(petraID, "petra", "", "trolley-problem-limits", `The trolley problem (Foot, Thomson) has been discussed for 60 years without resolution. This should tell us something.

What it tells us: moral intuitions are inconsistent. People say they would pull a lever to divert a trolley, killing one to save five, but wouldn't push a large man off a bridge to achieve the same outcome. The number of deaths is identical; the action type differs.

The trolley problem isolates action type from consequence to reveal this inconsistency. That's its value. But it can't tell us which intuition to revise or whether a theory that systematizes either intuition is correct.

Real ethical problems are embedded in contexts, relationships, and histories that the trolley problem deliberately strips away. The abstraction is both the point and the limitation.`, []string{"ethics", "philosophy"}, 108)

	pe4, _ := insertNote(petraID, "petra", "", "precedent-and-stare-decisis", `*Stare decisis* — "to stand by decided things" — is the common law doctrine that courts should follow prior decisions (precedent).

The justification: predictability. If the law means what the last court said it means, parties can plan accordingly. If each case starts fresh, the law is unknowable in advance.

The tension: precedent can lock in bad decisions. *Plessy v. Ferguson* (1896, "separate but equal") was precedent for 58 years. Overruling it required *Brown v. Board of Education* (1954) to engage in exactly the kind of first-principles reasoning that stare decisis is supposed to prevent.

The doctrine doesn't resolve this tension — it manages it.`, []string{"law", "jurisprudence"}, 100)

	// ── Quinn: education, pedagogy ────────────────────────────────────────────

	q1, _ := insertNote(quinnID, "quinn", "", "cognitive-load-theory", `Working memory has limited capacity — roughly 4 items simultaneously, with a narrow time window before they decay. This is not a deficiency; it's a design constraint.

Cognitive load theory (Sweller) distinguishes:
- **Intrinsic load**: the complexity inherent in the material
- **Extraneous load**: complexity introduced by poor instructional design
- **Germane load**: cognitive effort that produces learning (schema formation)

Good instruction minimizes extraneous load (don't make students solve the interface while learning the content) and manages intrinsic load through sequencing (simple to complex).

Related: [[@luna/mise-en-place-as-practice]], [[@bob/debugging-as-search]]`, []string{"education", "cognitive-science"}, 140)

	q2, _ := insertNote(quinnID, "quinn", "", "desirable-difficulties", `A desirable difficulty is an instructional condition that slows initial learning but improves long-term retention and transfer.

The main desirable difficulties: spacing (distributed practice over time vs. massed), interleaving (mixing topics vs. blocked practice), retrieval practice (testing yourself vs. re-reading), and generation (generating an answer before seeing it vs. receiving it).

Each feels worse in the moment — you're slower, you make more errors. This is why students (and teachers) systematically prefer conditions that feel easy but don't produce learning.

See: [[cognitive-load-theory]], [[@carol/practice-vs-playing]]`, []string{"education", "learning-science"}, 118)

	q3, _ := insertNote(quinnID, "quinn", "", "zone-of-proximal-development", `Vygotsky's Zone of Proximal Development (ZPD): the space between what a learner can do independently and what they can do with expert assistance.

Instruction should target the ZPD — too easy produces no growth, too difficult produces frustration and learned helplessness. The ZPD is also an argument for social learning: a peer who is slightly more advanced can pull you through the ZPD in ways that a much more advanced teacher can't.

The concept is rich but hard to operationalize: the ZPD varies by domain, changes quickly, and can't be measured by standard assessments (which measure independent performance, not assisted performance).`, []string{"education", "developmental-psychology"}, 105)

	q4, _ := insertNote(quinnID, "quinn", "", "assessment-drives-learning", `What gets assessed is what gets learned. This is not controversial — it's observed everywhere.

If you assess recall, students will memorize. If you assess problem-solving, students will practice problem-solving. If you assess on a timed, high-stakes, single-sitting format, students will develop skills for performing under those exact conditions.

The implication: curriculum reform without assessment reform is largely performative. You can change what's taught; what's learned will track what's assessed.

See: [[desirable-difficulties]], [[@rafael/metrics-that-matter]]`, []string{"education", "assessment"}, 95)

	q5, _ := insertNote(quinnID, "quinn", "", "socratic-method-mechanics", `The Socratic method, as practiced, is not just asking questions — it's asking questions designed to reveal a contradiction the interlocutor hasn't noticed.

The structure: (1) elicit a confident claim, (2) draw out its implications, (3) produce a case where the implications conflict with another claim the person also holds, (4) sit with the discomfort.

The discomfort is the point. Genuine learning often requires first becoming genuinely confused. Students who avoid confusion avoid learning.

The method works poorly in large groups (too many students, too few Socrates), with traumatized learners (the technique resembles interrogation), and when the teacher uses it to display superiority rather than induce discovery.`, []string{"education", "philosophy", "pedagogy"}, 102)

	// ── Rafael: startups, product ──────────────────────────────────────────────

	r1, _ := insertNote(rafaelID, "rafael", "", "distribution-beats-product", `A great product with no distribution dies. A mediocre product with great distribution wins.

This feels wrong to engineers and product people, who believe quality should win. It usually doesn't, and the exceptions are memorable because they're exceptions.

Distribution is the unsexy, underinvested, strategically decisive variable. [[@henry/the-printing-press-speed]] makes the point historically: Gutenberg's press succeeded when printers figured out distribution (cheap pamphlets), not when the technology improved.

The question isn't "is our product good enough?" but "can we get it in front of people who will use it?"`, []string{"startups", "product", "strategy"}, 140)

	r2, _ := insertNote(rafaelID, "rafael", "", "metrics-that-matter", `Vanity metrics look good and tell you nothing. The number of downloads tells you nothing if you don't know retention. The number of users tells you nothing if you don't know which ones are active.

The metrics that matter are proxies for the outcome you actually care about. Work backward: what behavior indicates that a user has gotten value? Measure that.

For most products: DAU/MAU ratio (stickiness), NPS (do users recommend it), and churn rate are better leading indicators than sign-ups or downloads.

See: [[pmf-signal]], [[@james/revealed-preference]]`, []string{"startups", "metrics", "product"}, 110)

	r3, _ := insertNote(rafaelID, "rafael", "", "pmf-signal", `Product-market fit is when a product has found an audience that needs it badly enough to use it despite its flaws, recommend it without being asked, and pay for it.

Marc Andreessen's description: "when you have it, you know." This is true but operationally useless. Better proxies:

- **Cohort retention**: do users who sign up in month 1 still use it in month 6?
- **Organic growth**: are users arriving without paid acquisition?
- **"Pull" from the market**: are people asking for features you haven't built yet?

You can build a product forever without knowing if you have PMF. These questions force the answer.

Related: [[metrics-that-matter]], [[distribution-beats-product]]`, []string{"startups", "product"}, 100)

	r4, _ := insertNote(rafaelID, "rafael", "", "second-order-thinking", `First-order thinking: "This will solve the problem." Second-order thinking: "This will solve the problem, and then what?"

Most startup failures are first-order successes. The growth hack works — and attracts low-quality users who churn. The viral loop fires — and attracts competitors. The price cut wins customers — and commoditizes the product.

[[@dave/on-abstraction]] is relevant: every solution creates a new layer of abstraction, and abstraction leaks. The second-order consequences emerge from the leak.

Related: [[@james/long-run-short-run]]`, []string{"startups", "thinking", "strategy"}, 95)

	r5, _ := insertNote(rafaelID, "rafael", "", "founder-mode", `Operators manage through layers: strategy → management → execution. Each layer is an abstraction that insulates the founder from the details of what's actually happening.

The trap: the abstraction leaks in exactly the place that matters. The founder who doesn't talk to customers doesn't know what customers actually want. The founder who doesn't review code doesn't know what's actually shipping.

"Founder mode" is the rejection of professional management norms in favor of staying close to the details across all functions. It works at small scale, breaks at large scale (a founder can't review every line of code at Google). The failure mode is micromanagement.

See: [[second-order-thinking]], [[@alice/the-collector-fallacy]]`, []string{"startups", "management"}, 92)

	// ── Sara: climate, environment ────────────────────────────────────────────

	s1, _ := insertNote(saraID, "sara", "", "tipping-points", `Climate tipping points are thresholds beyond which a system self-reinforces in a new direction. The West Antarctic Ice Sheet, the Amazon rainforest, the Atlantic Meridional Overturning Circulation — all are candidates.

What makes them dangerous: they're irreversible on human timescales. Once crossed, reducing emissions doesn't bring the system back. The interventions that would have worked become unavailable.

What makes them analytically difficult: we can't observe them directly; we identify them by the dynamics of the system approaching the threshold. The system can look stable until it isn't.

Related: [[@iris/succession-theory]], [[@james/price-signals]]`, []string{"climate", "ecology"}, 140)

	s2, _ := insertNote(saraID, "sara", "", "carbon-pricing", `Carbon pricing (carbon tax or cap-and-trade) is the most economically efficient instrument for reducing emissions. It prices the externality and lets the market find the cheapest way to reduce.

The politics are difficult: the costs are immediate and concentrated (fuel prices rise); the benefits are diffuse and delayed (future climate stabilization). This is the opposite of what political systems do well.

The evidence from existing carbon prices (EU ETS, British Columbia carbon tax, RGGI in the US Northeast) suggests they reduce emissions meaningfully without major economic disruption. The problem is that existing prices are too low and too narrow.

See: [[@james/price-signals]], [[tipping-points]]`, []string{"climate", "economics", "policy"}, 118)

	s3, _ := insertNote(saraID, "sara", "", "adaptation-vs-mitigation", `Climate policy has two modes: mitigation (reducing emissions) and adaptation (adjusting to changes that are now unavoidable).

Mitigation works on the cause. Adaptation works on the effect. Both are necessary — mitigation determines the ceiling of harm; adaptation determines how much of the unavoidable harm we successfully absorb.

The political economy differs: mitigation requires global coordination (free rider problem); adaptation is mostly local (a sea wall in Miami doesn't help Jakarta). This makes adaptation politically easier and mitigation more important to get right globally.`, []string{"climate", "policy"}, 102)

	s4, _ := insertNote(saraID, "sara", "", "ocean-acidification", `The ocean absorbs about 30% of anthropogenic CO₂. The CO₂ reacts with seawater to form carbonic acid, reducing pH. Ocean pH has dropped by 0.1 units since industrialization — a 26% increase in acidity (the pH scale is logarithmic).

The consequences for calcifying organisms (corals, oysters, pteropods) are significant: at lower pH, building and maintaining calcium carbonate shells requires more energy, and above certain thresholds, shells dissolve.

This is a slower-moving, less-visible threat than sea level rise or extreme weather, which is probably why it gets less attention.`, []string{"climate", "ocean", "biology"}, 95)

	s5, _ := insertNote(saraID, "sara", "", "rewilding-philosophy", `Rewilding reverses the intuition behind conservation. Classical conservation: protect what's left. Rewilding: restore what was lost, then let ecological processes take over.

The approach involves reintroducing keystone species ([[@iris/keystone-species]]) to restore trophic cascades. In Yellowstone, wolf reintroduction changed elk behavior, which allowed riparian vegetation to recover, which changed stream morphology. The effects cascaded through the system.

The philosophical challenge: there is no "natural" baseline to restore to. Ecosystems are historical artifacts, always in process. The question becomes: which historical moment are we targeting, and why?`, []string{"ecology", "conservation", "philosophy"}, 102)

	// ── Taro: traditional craft, Japan ────────────────────────────────────────

	t1, _ := insertNote(taroID, "taro", "", "wabi-sabi-materials", `Wabi-sabi is not a style or aesthetic category — it's an orientation toward impermanence and imperfection. The cracked glaze, the asymmetric bowl, the moss-covered stone: these are not despite their flaws but because of them.

The philosophical root: Buddhist acceptance of *anicca* (impermanence). Nothing lasts; the attempt to make things perfect and lasting is a form of resistance to what is.

This is not a design principle that translates easily. "Make it look imperfect" is the opposite of wabi-sabi — it's the attempt to control the appearance of non-control.

See: [[@frank/on-patience]], [[@kai/vernacular-photography]]`, []string{"craft", "philosophy", "aesthetics", "japan"}, 140)

	t2, _ := insertNote(taroID, "taro", "", "urushi-lacquer-technique", `Urushi (Japanese lacquerware) uses the sap of *Toxicodendron vernicifluum*, the lacquer tree. The sap is applied in thin layers, each cured in a humid environment (paradoxically, urushi polymerizes with moisture, not without it), then polished.

A high-quality piece may have 30–50 layers applied over months. The result is simultaneously hard, lustrous, waterproof, antibacterial, and — if maintained — centuries-durable. Museum pieces from the Jōmon period (over 9,000 years ago) survive.

What's interesting: urushi is both adhesive and finish. The layers of *urushi* in *kintsugi* (gold joinery) bind the broken pieces and create the visible seam simultaneously.`, []string{"craft", "japan", "materials"}, 120)

	t3, _ := insertNote(taroID, "taro", "", "mingei-folk-craft-movement", `Mingei (民藝, "folk crafts") was a movement founded by Yanagi Sōetsu in 1920s Japan to recover and valorize the everyday objects made by anonymous craftspeople.

Yanagi's argument: beauty is most fully expressed in objects made with skill and care for practical use, without self-conscious aestheticism. The tea bowl used daily by a farmer is more beautiful than the tea bowl made for an emperor's collection.

The movement influenced studio pottery (Bernard Leach), architecture (Bruno Taut), and design (MUJI's founders drew on mingei principles). It also carries a tension: once "folk craft" is valorized by intellectuals and sold in museums, is it still folk craft?

See: [[@kai/vernacular-photography]]`, []string{"craft", "aesthetics", "japan", "design"}, 115)

	t4, _ := insertNote(taroID, "taro", "", "shou-sugi-ban-method", `Shou sugi ban (*yakisugi*, 焼杉) is a traditional Japanese technique of charring cedar to preserve it. The outer layer of char seals the wood against moisture, insects, and fire.

The method: burn the surface until it's deeply charred (3–5 mm), then brush off the ash, revealing a stable carbonized surface. The carbon is inert and resists biological decay.

Properly done, yakisugi exterior cladding lasts 80–100 years without treatment. The aesthetic — dark, textured, weathering to silver-gray — has become fashionable in contemporary architecture.

Related: [[@nina/adaptive-reuse]], [[urushi-lacquer-technique]]`, []string{"craft", "japan", "architecture", "materials"}, 100)

	t5, _ := insertNote(taroID, "taro", "", "ceramics-cone-notes", `Firing temperature in ceramics is measured in "cones" (pyrometric cones), which measure heat work — the combination of temperature and time — not temperature alone.

Common ranges:
- **Cone 06–04**: earthenware glaze firing (~1000°C). Porous body; glaze seals surface.
- **Cone 6**: mid-fire stoneware (~1230°C). Good for production pottery; most studio clays.
- **Cone 10**: high-fire stoneware and porcelain (~1300°C). Dense, durable; traditional reduction atmosphere.

The body and glaze must be compatible — they expand and contract at similar rates. Mismatch causes crazing (network of fine cracks) or shivering (glaze flaking).

Related: [[urushi-lacquer-technique]]`, []string{"craft", "ceramics", "technique"}, 95)

	t6, _ := insertNote(taroID, "taro", "", "ma-negative-space", `*Ma* (間) in Japanese aesthetics: the meaningful pause, the empty space, the silence that gives sound its shape.

In music: the silence between notes. In architecture: the threshold space between inside and outside (the *engawa*, veranda). In conversation: the pause that means more than the words around it.

Western aesthetics tends to treat negative space as the absence of positive content. *Ma* treats negative space as content — the thing itself, not the absence of the thing.

Related: [[wabi-sabi-materials]], [[@carol/constraints-are-creative]]`, []string{"aesthetics", "japan", "philosophy"}, 88)

	// ── Grace: additional notes ───────────────────────────────────────────────

	g7, _ := insertNote(graceID, "grace", "", "pragmatics-and-context", `Grice's cooperative principle: speakers follow four maxims — quantity, quality, relation, manner. When they appear to violate one, they implicate something beyond the literal meaning.

"Can you pass the salt?" is a request, not a question about your physical capacity. The violation of manner (indirect) generates the implicature.

What's interesting: the implicature is cancelable ("Can you pass the salt? I'm asking because I'm curious whether you're left-handed") but the literal meaning never is. This asymmetry is the signature of implicature vs. entailment.

Related: [[indexicality]], [[@carol/on-music-theory-as-a-language]]`, []string{"linguistics", "pragmatics"}, 55)

	g8, _ := insertNote(graceID, "grace", "", "code-switching-and-identity", `Code-switching — alternating between languages or dialects within a conversation — is not a sign of deficiency. It is a sophisticated communicative skill that monolingual speakers cannot perform.

The switch carries meaning: it can signal intimacy, mark a topic shift, invoke a community, or create ironic distance. Bilingual speakers code-switch strategically, even if unconsciously.

The deficit framing ("they can't commit to one language") misreads pragmatic competence as grammatical failure. This is a common error in language policy.

See: [[linguistic-relativity-weak]]`, []string{"linguistics", "sociolinguistics"}, 45)

	g9, _ := insertNote(graceID, "grace", "", "the-arbitrariness-of-the-sign", `Saussure's founding claim: the linguistic sign is arbitrary. There is no natural connection between the word "tree" and the concept of a tree. The link is purely conventional.

The implications are larger than they look. If signs were motivated (iconic), you'd expect all languages to converge on similar forms. Instead, languages differ radically in their phonological systems, which proves the arbitrariness.

The partial exceptions — onomatopoeia, phonesthesia — don't disprove the rule. They're islands of iconicity in a sea of arbitrariness.

Related: [[meaning-through-reference]], [[metaphor-as-cognition]]`, []string{"linguistics", "semiotics"}, 38)

	// ── Henry: additional notes ───────────────────────────────────────────────

	h7, _ := insertNote(henryID, "henry", "", "the-mongol-postal-system", `The Yam, the Mongol relay postal system, was one of the largest communication networks in the pre-modern world. Riders changed horses every 25–40 km at dedicated stations; a message could travel 200–400 km per day.

It required an enormous logistical investment: stations, horses, fodder, staff. The Mongol Empire could sustain it because its tax base was vast and its administrative apparatus was surprisingly sophisticated.

The system collapsed with the empire, and no successor state rebuilt it at comparable scale. Most pre-modern "dark ages" are communication collapses as much as political ones.

See: [[roman-engineering-scale]], [[@rafael/distribution-beats-product]]`, []string{"history", "communication"}, 68)

	h8, _ := insertNote(henryID, "henry", "", "total-war-definition", `The concept of "total war" — war that mobilizes entire societies rather than just armies — is usually dated to the French Revolutionary Wars.

Before Napoleon, European warfare was limited by convention, logistics, and the expense of professional armies. Armies avoided unnecessary battles; the goal was often to maneuver the enemy into an untenable position.

The levée en masse (1793) changed this: France conscripted its entire male population, giving it armies of unprecedented size. The other powers had to follow or be overwhelmed.

Total war didn't just change tactics; it changed the relationship between states and populations. War became everyone's war.

Related: [[logistics-wins-wars]], [[fog-of-war-clausewitz]]`, []string{"history", "military"}, 58)

	h9, _ := insertNote(henryID, "henry", "", "the-ottoman-devshirme", `The devshirme was the Ottoman practice of conscripting Christian boys from Balkan territories, converting them to Islam, and training them for elite administrative and military roles.

The paradox: these men — legally slaves, torn from their families, converted — became the most powerful people in the empire. The Janissaries (elite infantry) and many grand viziers came from this system.

The rationale: they had no family networks, no tribal loyalties, no prior claims. Their entire careers depended on the sultan. Loyalty by dependency rather than by birth.

The system worked until the Janissaries became powerful enough to make and break sultans, at which point it backfired catastrophically.`, []string{"history", "ottoman"}, 75)

	// ── Iris: additional notes ────────────────────────────────────────────────

	i7, _ := insertNote(irisID, "iris", "", "r-k-selection-theory", `r/K selection theory describes two ends of a reproductive spectrum. r-strategists produce many offspring with little parental investment (most die young); K-strategists produce few offspring with heavy investment (survival rate high).

Mice vs. elephants is the textbook example. But the axes are continuous and independent: you can have high investment without low fecundity (seabirds), or low investment without high fecundity.

The theory has been extended (sometimes recklessly) beyond biology. The underlying logic — more investment per offspring means fewer can be supported — is sound. The details depend heavily on the specific environment.

Related: [[keystone-species]], [[island-biogeography]]`, []string{"ecology", "biology"}, 62)

	i8, _ := insertNote(irisID, "iris", "", "convergent-evolution", `Convergent evolution: unrelated lineages independently evolving similar traits in response to similar selective pressures.

The eye evolved independently at least 40 times. Dolphins and ichthyosaurs (a Mesozoic reptile) developed nearly identical body plans for similar aquatic lifestyles, from completely different starting points.

The implication: evolution is not purely contingent. Given similar environments, similar solutions recur. This doesn't make evolution deterministic — the specific molecular implementation differs — but it does suggest that the design space is structured.

See: [[phenotypic-plasticity]], [[@dave/on-abstraction]]`, []string{"biology", "evolution"}, 52)

	i9, _ := insertNote(irisID, "iris", "", "the-cambrian-explosion", `The Cambrian explosion (~541 million years ago) saw the rapid diversification of most major animal body plans in a geologically brief window of ~20 million years.

The causes are debated: rising oxygen levels, predator-prey arms races, the evolution of eyes, or the emptiness of ecological niches after the Ediacaran fauna disappeared.

What's certain: most animal phyla that exist today were present by the end of the Cambrian. The basic architectural plans (bilateral symmetry, segmentation, appendages) were established early and have been modified since but not replaced.

Related: [[succession-theory]], [[convergent-evolution]]`, []string{"biology", "evolution", "geology"}, 48)

	// ── James: additional notes ───────────────────────────────────────────────

	j7, _ := insertNote(jamesID, "james", "", "comparative-advantage", `Ricardo's comparative advantage is the most counterintuitive result in economics: even if one party is absolutely better at everything, both parties gain from trade.

The intuition: opportunity cost. If you're faster at both farming and coding but faster at coding by more, you should code. Your trading partner should farm even if they're slower at farming than you — because they're even slower at coding.

The model assumes: constant returns, no factor mobility, no externalities. Real trade violates all three. The case for trade remains strong but not automatic.

Related: [[coordination-problems]], [[price-signals]]`, []string{"economics", "trade"}, 65)

	j8, _ := insertNote(jamesID, "james", "", "anchor-and-adjustment", `Tversky and Kahneman: people estimate quantities by starting from an initial value (anchor) and adjusting insufficiently.

Asked "Is the population of Turkey more or less than 35 million?" then "What IS the population?" people give lower estimates than those asked about 100 million. The arbitrary anchor contaminates the estimate.

The mechanism is still debated (insufficient adjustment, selective accessibility, or both), but the effect is extremely robust. Doctors adjust dosages relative to the first number they see. Negotiators benefit from making the first offer.

See: [[loss-aversion]], [[@alice/the-collector-fallacy]]`, []string{"economics", "behavioral", "psychology"}, 55)

	j9, _ := insertNote(jamesID, "james", "", "public-goods-problem", `A public good is non-excludable (you can't stop non-payers from using it) and non-rival (one person's use doesn't diminish another's). Classic examples: national defense, basic research, clean air.

The problem: if you can't exclude non-payers, why would anyone pay? Free-rider incentives push provision below the socially optimal level. Markets underprovide public goods.

The solutions are all imperfect: government provision (how do you know optimal quantity?), private provision with bundling (radio + advertising), norms and social pressure. None generalizes cleanly.

Related: [[commons-and-tragedy]], [[coordination-problems]]`, []string{"economics", "public-policy"}, 48)

	// ── Kai: additional notes ─────────────────────────────────────────────────

	k6, _ := insertNote(kaiID, "kai", "", "the-35mm-frame", `35mm film became the dominant format for cinema (and then photography) largely through historical accident: Edison chose it, the industry standardized on it, and network effects locked it in.

The aspect ratio (roughly 3:2) is not derived from the golden ratio or human vision — it's an artifact of sprocket spacing and chemical efficiency. Yet generations of photographers have internalized it as "natural."

This matters: when digital sensors deviated from 3:2, photographers protested. But what they were defending was a convention made arbitrary by history, not a perceptual universal.

See: [[the-decisive-moment]], [[@grace/the-arbitrariness-of-the-sign]]`, []string{"photography", "history"}, 55)

	k7, _ := insertNote(kaiID, "kai", "", "film-grain-and-noise", `Film grain and digital noise are both artifacts, but they read very differently. Film grain is random, organic, statistically independent between frames. Digital noise can be banded, colored, and systematic.

The aesthetic preference for grain over noise (even in photographers who've never shot film) is partly learned but has a perceptual basis: organic randomness is less distracting than patterned artifacts.

The irony: expensive cameras now simulate film grain. The artifact of a technical limitation has become a desirable aesthetic quality once the limitation was removed.

Related: [[vernacular-photography]], [[@taro/wabi-sabi-materials]]`, []string{"photography", "aesthetics"}, 48)

	k8, _ := insertNote(kaiID, "kai", "", "negative-space-composition", `In photography, negative space — the area around the subject — does as much compositional work as the subject itself.

A portrait with the subject filling the frame reads as intimate or claustrophobic. The same subject with space to breathe reads as contemplative. The same subject with space they appear to be "entering" creates forward motion.

The mistake beginners make: they fill the frame because they think emptiness is waste. Negative space is not wasted; it's the context that makes the subject legible.

Related: [[the-decisive-moment]], [[@taro/ma-negative-space]]`, []string{"photography", "composition"}, 42)

	// ── Luna: additional notes ────────────────────────────────────────────────

	l7, _ := insertNote(lunaID, "luna", "", "emulsification-science", `An emulsion is a stable mixture of two immiscible liquids (usually fat and water) held together by an emulsifier. Mayonnaise, hollandaise, vinaigrette — all emulsions.

The emulsifier (lecithin in egg yolk, mustard proteins) has hydrophilic and hydrophobic ends. It coats fat droplets, presenting the water-friendly side outward and preventing coalescence.

Temperature matters: heat denatures the emulsifier proteins in hollandaise, breaking the sauce. Cold fat in vinaigrette won't incorporate. Understanding the mechanism tells you why recipes fail — and how to rescue them.

Related: [[maillard-reaction]], [[@iris/phenotypic-plasticity]]`, []string{"cooking", "chemistry"}, 52)

	l8, _ := insertNote(lunaID, "luna", "", "koji-and-enzymatic-cooking", `Koji (*Aspergillus oryzae*) is the mold used to make sake, miso, soy sauce, and mirin. It produces amylases (break down starches) and proteases (break down proteins into amino acids, including glutamate).

Dry-aging beef is partly enzymatic: the meat's own proteases break down connective tissue and muscle proteins over days or weeks. Koji accelerates this to hours. A koji-treated steak develops aged-beef complexity in 48 hours.

The principle generalizes: enzymes are time machines for flavor development. Understanding what enzymes do tells you what time does.

Related: [[fermentation-as-preservation]], [[umami-fifth-taste]]`, []string{"cooking", "fermentation", "chemistry"}, 45)

	l9, _ := insertNote(lunaID, "luna", "", "stock-clarity-technique", `A clear stock requires managing protein precipitation. Proteins denature on heating and aggregate into particles — cloudy if small, clear if removed.

The classical technique: bring stock to a simmer slowly, never boil (vigorous boiling emulsifies fat and protein into the liquid). Skim the gray foam as it rises. For consommé, the raft (ground meat + egg whites + mirepoix) attracts and traps remaining particles.

The raft works because proteins bind to proteins. The final product passes through a fine-mesh strainer or cheesecloth. Clarity is not cosmetic; it indicates the absence of bitter denatured proteins.

See: [[maillard-reaction]], [[salt-ratios]]`, []string{"cooking", "technique"}, 38)

	// ── Marcus: additional notes ──────────────────────────────────────────────

	m6, _ := insertNote(marcusID, "marcus", "", "lactate-threshold-training", `The lactate threshold (LT) is the exercise intensity at which lactate production exceeds clearance, causing blood lactate to rise exponentially. Training at or just below LT produces the largest adaptations in aerobic capacity.

Below LT: aerobic metabolism dominates, lactate is cleared as fast as it's produced. Above LT: anaerobic contribution increases, hydrogen ions accumulate, fatigue follows quickly.

Practically: LT corresponds roughly to "comfortably hard" — you can speak in short sentences but not paragraphs. Most recreational athletes train too hard on easy days and not hard enough on hard days, clustering around a "moderately uncomfortable" zone that stimulates neither adaptation.

Related: [[progressive-overload]], [[periodization-notes]]`, []string{"sports-science", "physiology"}, 55)

	m7, _ := insertNote(marcusID, "marcus", "", "force-velocity-curve", `The force-velocity relationship in muscle physiology: as contraction velocity increases, force production decreases. Maximum force is generated at zero velocity (isometric); maximum velocity occurs at zero external load.

This has practical implications for training: heavy slow lifts (high force, low velocity) don't transfer to fast explosive movements, and vice versa. A complete strength program trains across the curve.

The power peak — where force × velocity is maximized — is the sweet spot for athletic performance. It's somewhere in the middle, roughly 30–40% of maximum isometric force.

Related: [[movement-efficiency]], [[progressive-overload]]`, []string{"sports-science", "biomechanics"}, 48)

	m8, _ := insertNote(marcusID, "marcus", "", "heat-acclimatization", `Heat acclimatization occurs over 10–14 days of heat exposure during exercise. The adaptations: increased plasma volume (more blood = better cooling), lower core temperature threshold for sweating onset, higher sweat rate, lower sweat sodium concentration.

The plasma volume expansion is the most important: more blood means more can be shunted to the skin for cooling without starving the muscles.

Practical implication: an athlete who trained in cool conditions faces a physiological disadvantage in a hot race. Two weeks in the heat before competition largely eliminates the disadvantage.

Related: [[sleep-and-performance]], [[@iris/phenotypic-plasticity]]`, []string{"sports-science", "physiology"}, 42)

	// ── Nina: additional notes ────────────────────────────────────────────────

	n6, _ := insertNote(ninaID, "nina", "", "biophilic-design", `Biophilic design incorporates natural elements — light, plants, water, natural materials, views of nature — into built environments. The hypothesis: humans evolved in natural settings and retain responses to natural stimuli that urban environments suppress.

The evidence is suggestive: hospital patients with views of trees recover faster than those with views of walls. Workers in offices with natural light report higher wellbeing and sleep better. Attention restoration theory proposes that natural environments restore directed attention capacity depleted by urban demands.

The hard question is mechanism: is it the naturalness per se, or specific sensory qualities (complexity, fractal patterns, moving light) that could be achieved artificially?

Related: [[thermal-mass-passive-design]], [[@sara/rewilding-philosophy]]`, []string{"architecture", "psychology", "nature"}, 55)

	n7, _ := insertNote(ninaID, "nina", "", "acoustic-architecture", `Sound in buildings is as designed as light, even when it doesn't feel like it. Reverberation time (RT60 — how long a sound takes to decay 60dB) shapes how a space feels and functions.

A cathedral at 8–10 seconds RT60: each sound hangs in the air, layers with the next, produces the "holy" wash. A recording studio at <0.3s RT60: dead, analytical, no room sound. A good concert hall at 1.5–2s: warm, enveloping, the orchestra sounds larger than it is.

Building materials determine RT60. Concrete reflects; fabric, wood, and irregular surfaces absorb. Modern glass-and-steel buildings often have too-long reverb, making speech intelligible only at close range.

Related: [[city-as-hypertext]], [[thermal-mass-passive-design]]`, []string{"architecture", "acoustics"}, 48)

	n8, _ := insertNote(ninaID, "nina", "", "the-section-cut", `Architecture is often presented in plan — the horizontal cut through a building — but the section (vertical cut) reveals what plan cannot: how spaces stack, how light moves through floors, the relationship between interior height and human scale.

Le Corbusier's section drawings show this: the free plan only makes sense with the section. The pilotis (columns) free the ground level while the roof garden reclaims it, but you only see this in section.

The section is also where structural and spatial decisions are most directly in tension. The beam that spans 12 meters eats into floor-to-ceiling height. Good architecture resolves these tensions in section before committing to structure.`, []string{"architecture", "drawing"}, 42)

	// ── Oscar: additional notes ───────────────────────────────────────────────

	o6, _ := insertNote(oscarID, "oscar", "", "the-long-take", `A long take — an extended shot without cuts — does something editing cannot: it establishes real time passing. The audience cannot be tricked by editing into misreading how long something takes.

Hitchcock's *Rope* (1948) attempted a film in apparent real time using 10-minute takes (the film magazine limit) hidden by swish pans to dark objects. Tarkovsky's *Stalker* uses long takes to create a sense of geological duration.

The technique demands of actors and crew what editing can provide cheaply: everything must work, in order, in real time. The stakes are higher; so is the viewer's sense of witnessing something.

Related: [[deep-focus-controversy]], [[@carol/practice-vs-playing]]`, []string{"film", "technique"}, 52)

	o7, _ := insertNote(oscarID, "oscar", "", "the-unreliable-narrator-in-film", `Literary unreliable narrators are flagged by inconsistencies in the told story. Film has a different problem: the camera appears to show us reality directly, so unreliability must be built into the image itself.

*The Cabinet of Dr. Caligari* (1920) externalizes madness in expressionist sets. *Rashomon* (1950) presents the same event from four contradictory perspectives without resolving which is true. Both force the viewer to read the image critically rather than passively.

The unreliable image is harder to pull off than the unreliable word because audiences default to trusting what they see. Which makes the reveal more disorienting.

See: [[kuleshov-effect]], [[genre-as-contract]]`, []string{"film", "narrative", "theory"}, 45)

	o8, _ := insertNote(oscarID, "oscar", "", "aspect-ratio-choices", `Aspect ratio is a narrative tool, not just a technical parameter. The Academy ratio (1.37:1) is nearly square — intimate, suited to close-ups and interior drama. CinemaScope and its descendants (2.35:1 and wider) spread the frame horizontally, emphasizing landscape, crowds, the smallness of figures in space.

Directors choose ratios for their expressive properties: Wes Anderson's use of 1.37:1 in *The Grand Budapest Hotel* evokes the past (it was standard before television forced widescreen). Xavier Dolan shoots in 1:1 square format for claustrophobic intimacy.

The ratio frames what can be shown in a single image, which shapes what cuts are necessary.

Related: [[deep-focus-controversy]], [[the-long-take]]`, []string{"film", "technique", "composition"}, 38)

	// ── Petra: additional notes ───────────────────────────────────────────────

	pe5, _ := insertNote(petraID, "petra", "", "constitutional-moments", `Bruce Ackerman's theory of "constitutional moments": the US Constitution changes not only through formal amendment but through transformative periods of popular mobilization that are later ratified by the courts.

His examples: Reconstruction (13th-15th Amendments and their interpretation), the New Deal (the "switch in time that saved nine"), the civil rights era. In each case, a political movement achieved changes that formal amendment couldn't or didn't accomplish.

The implication: constitutional law is not purely legal but also political. The formal text underdetermines outcomes; politics fills the gap — but not transparently.

See: [[rule-of-law-components]], [[precedent-and-stare-decisis]]`, []string{"law", "political-theory"}, 55)

	pe6, _ := insertNote(petraID, "petra", "", "strict-vs-loose-construction", `Originalism (interpret the Constitution as understood at ratification) vs. living constitutionalism (interpret it to adapt to contemporary values) is the dominant debate in US constitutional theory.

The debate is partly empirical (what did the framers mean?) and partly normative (why should their meaning bind us?). Originalists answer the normative question with democratic legitimacy — the text was ratified. Living constitutionalists answer with adaptability — a fixed text cannot govern an unimaginably different society.

Both face internal problems. Originalists disagree about whose original understanding matters (framers, ratifiers, public meaning). Living constitutionalists face the counter-majoritarian difficulty: why should nine unelected judges adapt the constitution?

Related: [[rule-of-law-components]], [[@alice/on-linking-ideas]]`, []string{"law", "jurisprudence", "philosophy"}, 48)

	pe7, _ := insertNote(petraID, "petra", "", "international-law-enforcement", `International law is real law, but its enforcement mechanism is categorically different from domestic law. There is no world police, no compulsory jurisdiction, no guarantee that judgments will be implemented.

States comply with international law most of the time — not primarily because they're forced to, but because compliance is in their interest (reciprocity, reputation, domestic politics). This is weaker than domestic law but stronger than nothing.

The interesting cases: when do states comply against their immediate interest? The literature suggests: when they have strong domestic legal cultures, when the rule is highly legitimate, and when the costs of violation are reputationally high.

Related: [[law-as-coordination]], [[rule-of-law-components]]`, []string{"law", "international-relations"}, 42)

	// ── Quinn: additional notes ───────────────────────────────────────────────

	q6, _ := insertNote(quinnID, "quinn", "", "transfer-of-learning", `Transfer — applying learning from one context to another — is the goal of education but is frustratingly difficult to produce.

Near transfer (similar context) is relatively easy. Far transfer (very different context) is rare and hard to predict. Most educational interventions produce near transfer at best.

The conditions for transfer: deep understanding of underlying principles (not surface features), varied practice contexts (prevents overfitting to one situation), explicit attention to structural similarity across domains.

This is why "learning to learn" is valuable — it's the skill of noticing structural similarity. Without it, each new domain feels entirely new.

Related: [[desirable-difficulties]], [[zone-of-proximal-development]]`, []string{"education", "cognitive-science"}, 55)

	q7, _ := insertNote(quinnID, "quinn", "", "the-testing-effect", `The testing effect (retrieval practice effect): testing yourself on material produces better long-term retention than re-studying the same material for the same amount of time.

This seems obvious once stated, but conflicts with students' intuitions. Re-reading feels productive because the material feels familiar. Testing feels uncomfortable because you're aware of what you don't know.

The mechanism: retrieval strengthens the memory trace in ways that passive exposure doesn't. The effort of retrieval is the learning.

Practical implication: flashcards beat highlighting. Practice tests beat re-reading notes. The uncomfortable thing is the right thing.

Related: [[desirable-difficulties]], [[@alice/how-i-take-notes]]`, []string{"education", "memory", "learning-science"}, 48)

	q8, _ := insertNote(quinnID, "quinn", "", "intrinsic-vs-extrinsic-motivation", `Deci and Ryan's self-determination theory: intrinsic motivation (doing something for its own sake) is more robust than extrinsic motivation (reward or punishment) for complex, creative, and long-horizon tasks.

The paradox: adding extrinsic rewards to intrinsically motivated behavior can undermine intrinsic motivation (overjustification effect). Pay a child to read books they already love, and they read less when the payment stops.

The conditions for intrinsic motivation: autonomy (choosing how to do it), competence (feeling capable), relatedness (doing it in connection with others). Educational systems that optimize for grades at the expense of all three predictably produce students who stop learning when grades stop mattering.

Related: [[assessment-drives-learning]], [[@carol/practice-vs-playing]]`, []string{"education", "psychology", "motivation"}, 42)

	// ── Rafael: additional notes ──────────────────────────────────────────────

	r6, _ := insertNote(rafaelID, "rafael", "", "cold-start-problem", `Every marketplace has a cold-start problem: buyers want sellers, sellers want buyers, no one wants to go first.

The solutions are always some version of "subsidize one side first." Uber subsidized drivers (guaranteed minimums) to build supply before demand arrived. OpenTable gave restaurants free software to build their side. Credit cards subsidize cardholders with rewards while charging merchants.

The choice of which side to subsidize is a strategic decision that shapes the long-term business model. Subsidize too long and you have users who only show up for the subsidy and churn when it ends.

Related: [[pmf-signal]], [[@james/coordination-problems]]`, []string{"startups", "marketplaces", "strategy"}, 55)

	r7, _ := insertNote(rafaelID, "rafael", "", "the-pivot-myth", `The pivot is romanticized in startup culture as a bold strategic move. In practice, most pivots are either capitulations (giving up the original thesis without replacing it with anything better) or searches (methodical exploration of the adjacent possible).

Genuine strategic pivots — where you take a hard-won capability and apply it to a dramatically better market — are rare. Instagram pivoting from Burbn (check-in app) to photo sharing is the canonical example, but the success came from recognizing what users were actually doing, not from a strategic epiphany.

The lesson: observe before you pivot. The pivot that works is usually already latent in the data.

Related: [[pmf-signal]], [[founder-mode]]`, []string{"startups", "strategy"}, 48)

	r8, _ := insertNote(rafaelID, "rafael", "", "pricing-as-positioning", `Price is not just a revenue mechanism — it's a signal. A price that's too low communicates low quality (even if the product is excellent). A price increase can increase demand if it moves the product into a different perceived category.

Luxury goods are the extreme case: the demand curve inverts at high prices because the price is part of the product. But the principle applies more broadly: professional services priced too low attract clients who don't value them, demand constant justification, and are slow to pay.

The right question is not "what will people pay?" but "what price positions this product correctly in the buyer's mind?"

Related: [[metrics-that-matter]], [[@james/price-signals]]`, []string{"startups", "pricing", "strategy"}, 42)

	// ── Sara: additional notes ────────────────────────────────────────────────

	s6, _ := insertNote(saraID, "sara", "", "permafrost-feedback", `About 1.5 trillion tonnes of organic carbon are stored in Arctic permafrost — roughly twice the carbon currently in the atmosphere. As permafrost thaws, microbial decomposition releases this carbon as CO₂ and methane.

Methane is ~80× more potent than CO₂ as a greenhouse gas over 20 years. Even if it oxidizes to CO₂ in the atmosphere, the short-term forcing is significant.

The feedback loop: warming thaws permafrost → permafrost releases carbon → more warming → more thawing. This is a positive feedback that current climate models capture only partially, because the microbial dynamics are poorly constrained.

Related: [[tipping-points]], [[@iris/mycorrhizal-networks]]`, []string{"climate", "ecology", "carbon"}, 55)

	s7, _ := insertNote(saraID, "sara", "", "solar-geoengineering-risks", `Stratospheric aerosol injection (SAI) — mimicking volcanic eruptions by injecting reflective particles into the stratosphere — could reduce global temperatures quickly and cheaply. A fleet of aircraft could deploy enough sulfur dioxide to offset several degrees of warming.

The risks: termination shock (if SAI is stopped suddenly, temperatures rebound rapidly), uneven regional effects (the tropics cool more than the poles), disruption of monsoon patterns, and the geopolitics of who controls the thermostat.

The most concerning aspect: SAI is technically accessible to a mid-sized nation or even a well-funded private actor. The governance problem may arrive before the technology is understood.

Related: [[adaptation-vs-mitigation]], [[@petra/international-law-enforcement]]`, []string{"climate", "geoengineering", "policy"}, 48)

	s8, _ := insertNote(saraID, "sara", "", "land-use-and-emissions", `Land use change — primarily deforestation for agriculture — accounts for roughly 10–15% of global greenhouse gas emissions. This makes the food system one of the largest emitters, comparable to all transportation.

The breakdown: beef production is by far the most carbon-intensive food (per calorie), primarily through methane from enteric fermentation and land clearing for grazing. Shifting from beef to chicken reduces food-related emissions dramatically; shifting to plant protein more so.

The political economy is difficult: land use change is diffuse, involves millions of small actors, and intersects with food sovereignty and poverty. There is no single lever.

Related: [[carbon-pricing]], [[@frank/seed-saving-notes]]`, []string{"climate", "food", "land-use"}, 42)

	// ── Taro: additional notes ────────────────────────────────────────────────

	t7, _ := insertNote(taroID, "taro", "", "indigo-dyeing-aizome", `Aizome — Japanese indigo dyeing — uses *Persicaria tinctoria*, a plant grown annually, fermented into sukumo (composted leaves), and used to build a living vat.

The vat is alive: it contains bacteria that maintain the reducing environment necessary for indigo to bond with fiber. Maintaining a healthy vat requires daily attention — checking pH, temperature, alkalinity, "feeding" with lye and bran. A neglected vat dies.

The blue deepens with each dip: one dip yields pale sky; twenty dips yield deep navy. The dyer's skill is knowing when to stop. Over-dipped cloth becomes brittle.

Related: [[urushi-lacquer-technique]], [[@luna/fermentation-as-preservation]]`, []string{"craft", "japan", "textiles"}, 52)

	t8, _ := insertNote(taroID, "taro", "", "temari-geometry", `Temari are Japanese embroidered balls with precise geometric division of the sphere. The designs are not freehand — the sphere is divided into sections using pins and thread guidelines, then embroidered within each section.

The geometry is complex: dividing a sphere into equal sections requires solving spherical trigonometry problems. Traditional temari artisans solved these empirically, passing solutions as oral tradition. The mathematics was worked out much later.

A temari with 8-division symmetry maps onto a cube's geometry; 12-division maps onto a dodecahedron. The craft embeds group theory in textile form.

Related: [[mingei-folk-craft-movement]], [[@dave/on-abstraction]]`, []string{"craft", "japan", "mathematics"}, 45)

	t9, _ := insertNote(taroID, "taro", "", "bamboo-as-material", `Bamboo is not wood — it is a grass. Its cellular structure is entirely different: dense outer fibers, progressively less dense toward the core. This gradient gives it extraordinary stiffness-to-weight ratio in its natural form.

Processed bamboo (cross-laminated, woven, or carbonized) has different properties from the raw culm and should be evaluated on its own terms. "Bamboo is stronger than steel" refers to specific configurations under specific loads and should not be generalized.

The material's genuine advantages: rapid growth (some species 90 cm/day), carbon sequestration, versatility. Its limitations: susceptibility to moisture and insects without treatment, and the difficulty of joining it structurally.

Related: [[shou-sugi-ban-method]], [[@nina/thermal-mass-passive-design]]`, []string{"craft", "materials", "architecture"}, 38)

	// ── Alice: additional notes ───────────────────────────────────────────────

	a7, _ := insertNote(aliceID, "alice", "", "spaced-repetition-systems", `Spaced repetition exploits the spacing effect: memory is stronger when review is distributed over time rather than massed.

The Ebbinghaus forgetting curve shows memory decays predictably; spaced repetition schedules reviews just before forgetting. Each successful review extends the interval until the next.

The failure mode: people use SRS for facts they should understand rather than for facts they genuinely need to recall automatically. Vocabulary, formulas, dates — yes. Concepts that need to be understood contextually — a card won't help.

See: [[how-i-take-notes]], [[@quinn/the-testing-effect]]`, []string{"knowledge", "memory", "tools"}, 22)

	a8, _ := insertNote(aliceID, "alice", "philosophy", "emergence-and-complexity", `Emergence: properties of a system that are not predictable from its components. Wetness is not a property of H₂O molecules; it emerges at scale. Consciousness (if it is what we think it is) is not a property of neurons.

The strong form of emergence (where the higher-level properties are genuinely irreducible) is philosophically contested. The weak form (where we lack computational resources to predict from components) is uncontroversially real and practically important.

Complexity science studies the conditions under which emergent phenomena arise: edge-of-chaos dynamics, self-organized criticality, power law distributions. The tools exist; the unified theory does not.

Related: [[@iris/the-cambrian-explosion]], [[@dave/bayes-as-updating]]`, []string{"philosophy", "complexity", "science"}, 16)

	// ── Bob: additional notes ─────────────────────────────────────────────────

	b7, _ := insertNote(bobID, "bob", "programming", "error-handling-go", `Go's error handling is verbose but explicit. The cost is real; the benefit is also real: every error is handled where it's returned. You cannot accidentally ignore an error without making it look deliberate.

The pattern everyone reaches for: `+"`"+`errors.Is`+"`"+` for sentinels, `+"`"+`errors.As`+"`"+` for type assertions, `+"`"+`fmt.Errorf("... %w", err)`+"`"+` for wrapping. The wrapped error chain is inspectable without depending on string matching.

The missing piece: no stack traces by default. For production debugging, either log context at the call site or use a library that captures them. pkg/errors is the historical choice; the stdlib `+"`"+`errors`+"`"+` package now covers most use cases.

See: [[debugging-as-search]], [[@dave/on-abstraction]]`, []string{"programming", "go", "errors"}, 14)

	b8, _ := insertNote(bobID, "bob", "programming", "data-structures-for-interviews", `Three data structures explain 80% of algorithm interview problems:

**Hash map**: O(1) lookup. First reach for it whenever you need to count, group, or look up by key.

**Stack/queue**: problems involving "process in order" or "undo" structure. Monotonic stack patterns solve a surprising range of range-maximum/minimum problems.

**Two pointers / sliding window**: linear-scan problems on sorted arrays or strings. Eliminates the nested-loop O(n²) that brute force usually produces.

The fourth: heap/priority queue, for "k-th largest" or "merge k sorted lists" families.

Most interview problems combine two of these. Recognition is the skill.`, []string{"programming", "algorithms", "interviews"}, 8)

	// ── Carol: additional notes ───────────────────────────────────────────────

	c5, _ := insertNote(carolID, "carol", "", "modal-harmony", `Modal harmony extends tonal harmony by treating each church mode as a stable tonal center rather than a deviation from major/minor.

Dorian mode (like D minor but with a raised 6th) has a characteristic sound — dark but with a brightness on the IV chord. Miles Davis's *So What* is built on D Dorian for 8 bars, Eb Dorian for 4, back to D for 4. The stability is modal, not functional.

The difference from tonal: in modal music, chords don't resolve — they rest. The drone, not the cadence, establishes the center. Knowing which mode you're in tells you which notes feel stable.

Related: [[on-music-theory-as-a-language]], [[@dave/shannon-information]]`, []string{"music", "theory", "harmony"}, 32)

	c6, _ := insertNote(carolID, "carol", "creativity", "the-beginner-s-mind", `Shunryu Suzuki: "In the beginner's mind there are many possibilities, but in the expert's mind there are few."

The expert's knowledge is also the expert's constraint. They know what works, which means they also know what doesn't — and prune the solution space before exploring it. Sometimes the pruned branch was the right one.

This is why cross-domain thinking is valuable: someone who brings beginner's eyes to a field often finds the solution the experts ruled out ten years ago.

The skill is maintaining beginner's mind deliberately, which is paradoxically expert-level.

Related: [[constraints-are-creative]], [[@alice/the-collector-fallacy]]`, []string{"creativity", "philosophy", "learning"}, 22)

	// ── Dave: additional notes ────────────────────────────────────────────────

	d5, _ := insertNote(daveID, "dave", "", "the-p-vs-np-problem", `P vs. NP is the question of whether every problem whose solution can be verified quickly can also be solved quickly.

P: problems solvable in polynomial time. NP: problems where solutions can be verified in polynomial time. NP-complete: the hardest problems in NP.

If P = NP, most of cryptography breaks. RSA depends on factoring being hard to solve (NP) but easy to verify (P-ish). If they're equal, you can solve it as fast as you can verify it.

The consensus is P ≠ NP, but it's unproven. It may be unprovable within standard mathematics. The difficulty of the problem is itself interesting — we can't even characterize the gap between checking and solving.

See: [[relational-model]], [[@bob/go-concurrency-patterns]]`, []string{"math", "computer-science", "complexity"}, 26)

	d6, _ := insertNote(daveID, "dave", "math", "godel-incompleteness", `Gödel's first incompleteness theorem (1931): any consistent formal system strong enough to express basic arithmetic contains statements that are true but unprovable within the system.

The proof is constructive: Gödel encoded "This statement is not provable in this system" as an arithmetic statement. If it's provable, the system is inconsistent. If it's not provable, it's a true unprovable statement.

The second theorem: such a system cannot prove its own consistency.

What this does NOT mean: mathematics is wrong, or that anything goes. It means formal proofs have inherent limits. Truth outruns provability. This was disturbing in 1931; it is now foundational.

Related: [[p-vs-np-problem]], [[@alice/emergence-and-complexity]]`, []string{"math", "logic", "philosophy"}, 18)

	// ── Eve: additional notes ─────────────────────────────────────────────────

	e4, _ := insertNote(eveID, "eve", "design", "gestalt-principles", `The Gestalt principles describe how the visual system groups elements into perceived wholes. The key ones for design:

**Proximity**: elements close together are perceived as groups. Use spacing to show relationships.

**Similarity**: elements that look alike are grouped. Use it to reinforce hierarchy.

**Continuity**: the eye follows lines and curves. Use alignment to create flow.

**Closure**: the mind completes incomplete shapes. Use it to imply without rendering.

The principles describe perception, not rules. Violating them for emphasis is valid — but you need to understand what you're disrupting.

Related: [[grids-and-rhythm]], [[@grace/meaning-through-reference]]`, []string{"design", "psychology", "perception"}, 35)

	e5, _ := insertNote(eveID, "eve", "", "icon-vs-symbol", `An icon resembles what it represents (a photo of a dog represents dogs). A symbol has an arbitrary relation to its referent (the word "dog" doesn't look like a dog).

In UI design, the distinction matters: icons should be recognizable without labels. If you need a label, the icon is a symbol, not an icon. A floppy disk for "save" is a symbol — it only works because of cultural convention, not resemblance.

The test: show the element to someone unfamiliar with the convention. If they can identify the function, it's an icon. If they need to learn it, it's a symbol — and should be treated as such.

Related: [[typography-is-invisible]], [[@grace/the-arbitrariness-of-the-sign]]`, []string{"design", "UX", "semiotics"}, 25)

	// ── Frank: additional notes ───────────────────────────────────────────────

	f4, _ := insertNote(frankID, "frank", "", "composting-as-system", `A compost pile is not a pile of rotting material. It's a managed microbial ecosystem.

Carbon-to-nitrogen ratio should be roughly 30:1. Too much carbon (wood chips, straw) and the pile goes cold and slow. Too much nitrogen (kitchen scraps, grass clippings) and it goes anaerobic and smells.

Temperature is the diagnostic: a working pile heats to 55–65°C at the center, hot enough to kill pathogens and weed seeds. A cold pile means insufficient nitrogen, not enough moisture, or inadequate size.

Turn the pile to introduce oxygen — anaerobic decomposition produces methane; aerobic produces CO₂, water, and heat.

Related: [[garden-notebook-spring]], [[@iris/mycorrhizal-networks]]`, []string{"gardening", "biology"}, 38)

	f5, _ := insertNote(frankID, "frank", "", "grafting-and-rootstocks", `Grafting joins a scion (the desired variety) to a rootstock (chosen for vigor, disease resistance, or size control). The two grow together as one plant.

Most commercial apple trees are grafted — seed-grown apples don't breed true (an apple from a Honeycrisp seed will not produce Honeycrisp fruit). Grafting is the only way to propagate a specific variety reliably.

Rootstock choice determines tree size, precocity, and disease susceptibility more than scion choice does. The same variety on dwarfing rootstock produces fruit in 2–3 years; on vigorous rootstock, 6–10 years. There is no free lunch: dwarfing rootstocks require more support and irrigation.

See: [[seed-saving-notes]], [[@iris/phenotypic-plasticity]]`, []string{"gardening", "horticulture"}, 28)

	// ── Round 3: push past 200 ────────────────────────────────────────────────

	g10, _ := insertNote(graceID, "grace", "", "language-acquisition-stages", `Children acquire language in a remarkably consistent sequence across all known languages: cooing → babbling → one-word stage (holophrases) → two-word stage → telegraphic speech → full grammar.

The sequence is consistent even in children with very different linguistic input, which suggests the trajectory is partly maturationally determined, not purely learned.

The critical period hypothesis: language acquisition is easiest before puberty, harder after. Evidence: late L1 acquirers (isolated children) never achieve full grammar. Late L2 acquirers have persistent accent and grammatical gaps. The biological window is real.

Related: [[linguistic-relativity-weak]], [[@quinn/zone-of-proximal-development]]`, []string{"linguistics", "development"}, 30)

	h10, _ := insertNote(henryID, "henry", "", "the-black-death-labor", `The Black Death (1347–1351) killed roughly a third of Europe's population. The economic consequence was paradoxical: real wages for surviving workers rose sharply because labor became scarce.

Peasants who had been bound to land and lord suddenly had bargaining power. Lords who needed their fields harvested had to offer better terms. The feudal system, already under stress, accelerated its decline.

This is an example of supply shock forcing institutional change. The plague didn't cause the end of feudalism — but it compressed a process that might have taken centuries into decades.

Related: [[roman-engineering-scale]], [[@james/price-signals]]`, []string{"history", "economics"}, 30)

	i10, _ := insertNote(irisID, "iris", "", "lichen-as-symbiosis", `Lichens are not a single organism — they are a stable symbiosis between fungi and photosynthetic partners (algae or cyanobacteria). The fungus provides structure and water retention; the photosynthetic partner provides carbon.

They colonize bare rock, producing acids that break it down into proto-soil, enabling plant colonization — a keystone role in succession. A lichen can persist on rock that would kill most other organisms.

The classification problem: lichens were classified as plants for centuries because they look like plants. They are neither fungi nor algae alone; the symbiosis is the organism. Taxonomy had no category for them.

Related: [[mycorrhizal-networks]], [[succession-theory]]`, []string{"ecology", "biology", "symbiosis"}, 28)

	j10, _ := insertNote(jamesID, "james", "", "sunk-cost-fallacy", `The sunk cost fallacy: continuing an action because of past investment (time, money, effort), even when the marginal returns no longer justify it.

The correct frame: sunk costs are sunk. The decision should be based entirely on expected future costs and benefits. The past is irrelevant to the forward decision.

The fallacy is adaptive in some contexts (reputation for commitment deters exploitation) and pathological in others (staying in a failing project because you've already spent three years on it). Distinguishing which context you're in requires stepping outside the decision.

Related: [[loss-aversion]], [[long-run-short-run]]`, []string{"economics", "behavioral"}, 25)

	k9, _ := insertNote(kaiID, "kai", "", "analog-vs-digital-photography", `The practical differences between film and digital have narrowed to near-nothing. The meaningful differences are in workflow and relationship to the image.

Film imposes cost per frame — you think harder before shooting. This is not nostalgia; it is a different attention structure. Digital imposes no cost, which produces a different (not inferior) relationship: shoot more, select harder.

The choice is not technical but temperamental. Some photographers think better with constraint; others think better with abundance. Neither is wrong.

Related: [[film-grain-and-noise]], [[the-decisive-moment]]`, []string{"photography", "technique"}, 22)

	l10, _ := insertNote(lunaID, "luna", "", "bread-hydration", `Bread hydration (water weight / flour weight × 100) determines crumb structure and handling difficulty. A 65% hydration dough behaves like clay — stiff, easy to shape. An 80% hydration dough is slack and requires technique.

Higher hydration produces a more open, irregular crumb with large holes (ciabatta, sourdough). Lower hydration produces a tighter, more uniform crumb (sandwich bread). Neither is superior — they suit different purposes.

The difficulty: high-hydration doughs are hard to shape because they stick to everything. Cold retarding (overnight in the fridge) stiffens the dough and makes shaping possible. The tradeoff is time for workability.

Related: [[salt-ratios]], [[fermentation-as-preservation]]`, []string{"cooking", "bread", "technique"}, 20)

	m9, _ := insertNote(marcusID, "marcus", "", "grip-strength-longevity", `Grip strength is the most consistent predictor of all-cause mortality in middle-aged adults across multiple large studies. Stronger grip → longer life.

This is almost certainly not causal in itself — grip strength is a proxy for overall muscle mass, neuromuscular function, and the absence of wasting diseases. But as a proxy it's remarkably clean and easy to measure.

The practical implication: maintain and train grip strength. Deadlifts, farmer carries, hanging. It's both a training target and a biomarker worth tracking over decades.

Related: [[progressive-overload]], [[sleep-and-performance]]`, []string{"sports-science", "longevity"}, 18)

	n9, _ := insertNote(ninaID, "nina", "", "staircase-design", `Staircase design is the intersection of structure, safety regulation, and human movement. The riser-to-tread relationship follows an ergonomic formula (2r + t ≈ 63cm) derived from average human gait.

Violate the formula — too steep, too shallow, irregular — and users stumble. Most staircase accidents happen on the last step, where the rhythm breaks. Consistent geometry trains the body; inconsistency catches it off guard.

Spiral and curved stairs are harder to descend than ascend because the body can't read the geometry in advance. This is why they are regulated differently for egress.

Related: [[acoustic-architecture]], [[the-section-cut]]`, []string{"architecture", "ergonomics"}, 18)

	o9, _ := insertNote(oscarID, "oscar", "", "film-score-vs-soundtrack", `A film score is original music composed for a specific film. A soundtrack is a collection of pre-existing tracks licensed for the film. The distinction matters because they work differently.

A composed score can be precisely synchronized: Bernard Herrmann's strings in *Psycho* were written to match specific cuts. Licensed music arrives with prior associations: "Born to Be Wild" in *Easy Rider* carries biker-culture connotations Steppenwolf created independently.

The most sophisticated films use licensed music against its connotations deliberately. Kubrick's use of "Singin' in the Rain" in *A Clockwork Orange* makes violence cheerful by association — and destroys the song for the audience permanently.

Related: [[sound-design-diegetic]], [[genre-as-contract]]`, []string{"film", "music", "sound"}, 18)

	pe8, _ := insertNote(petraID, "petra", "", "contract-formation", `A contract requires offer, acceptance, and consideration. The consideration requirement — each party must give something — distinguishes contracts from gifts. Promises to give gifts are generally unenforceable.

The modern trend is away from strict consideration doctrine. Promissory estoppel allows enforcement of promises that induced detrimental reliance, even without consideration. Many jurisdictions have statutory exceptions.

What consideration doctrine actually does: it screens out promises made without thinking. The requirement that you get something back forces both parties to engage with what they're agreeing to. It's a formality that creates salience.

Related: [[law-as-coordination]], [[rule-of-law-components]]`, []string{"law", "contracts"}, 15)

	q9, _ := insertNote(quinnID, "quinn", "", "feedback-timing", `Feedback is most effective when it arrives close in time to the action it responds to. Delayed feedback requires the learner to reconstruct the context, which is cognitively expensive and error-prone.

The implication for software: immediate error messages, inline validation, live preview. The implication for education: frequent low-stakes assessment rather than infrequent high-stakes exams.

The limit: feedback that arrives too immediately can prevent learners from generating their own error-correction processes. Some delay — enough to force an attempt at self-evaluation — is optimal.

Related: [[the-testing-effect]], [[desirable-difficulties]]`, []string{"education", "feedback", "learning-science"}, 14)

	r9, _ := insertNote(rafaelID, "rafael", "", "burn-rate-intuition", `Burn rate (monthly cash outflow) and runway (months until cash runs out) are the two numbers every founder should know at all times.

The common mistake: thinking about runway in terms of current burn, not future burn. If you hire three people today, your runway calculation from last week is wrong. Every hiring decision is a runway decision.

The rule of thumb: raise when you have 6+ months of runway, not when you have 3. Fundraising takes longer than expected and investors smell desperation. The best time to raise is when you don't need to.

Related: [[metrics-that-matter]], [[founder-mode]]`, []string{"startups", "finance"}, 12)

	s9, _ := insertNote(saraID, "sara", "", "water-scarcity-geography", `Water scarcity is not primarily a global problem — it is a distribution problem. Total freshwater is sufficient; it is concentrated in the wrong places and flows at the wrong times.

The Ogallala Aquifer (US Great Plains) is being depleted faster than it recharges — effectively a non-renewable resource at the timescale of agriculture. Similar dynamics in northern India, northern China, the Middle East.

The solutions differ by cause: over-extraction requires pricing water at replacement cost (politically difficult); distribution requires infrastructure; timing requires storage. Most current approaches address symptoms, not the extraction rate.

Related: [[tipping-points]], [[@james/price-signals]]`, []string{"climate", "water", "geography"}, 12)

	t10, _ := insertNote(taroID, "taro", "", "paper-making-washi", `Washi (和紙) — Japanese handmade paper — uses different fibers than Western paper (primarily kozo, the inner bark of mulberry). The long fibers produce a paper that is simultaneously thin, strong, and highly stable.

The process: cook fibers in lye, beat to separate, sheet in a mould suspended in water with a mucilaginous agent (neri) that slows drainage and allows fiber alignment. Dry on boards in the sun.

Washi sheets from the 8th century survive in better condition than 19th-century Western machine paper. The fiber length and preparation method produce permanence that modern paper manufacturing abandoned in favor of speed.

Related: [[mingei-folk-craft-movement]], [[indigo-dyeing-aizome]]`, []string{"craft", "japan", "paper"}, 10)

	a9, _ := insertNote(aliceID, "alice", "", "second-brain-critique", `The "second brain" framing (Forte's *Building a Second Brain*) is appealing but misleading. A brain doesn't just store — it associates, forgets, compresses, and generates. A note system does none of these automatically.

What a note system does: makes retrieval easier. This is valuable but limited. The mistake is outsourcing the thinking to the system, then being surprised when the system can't think.

Notes are inputs to thinking, not substitutes for it. The bottleneck is almost never retrieval; it's synthesis. No system automates that.

Related: [[the-collector-fallacy]], [[how-i-take-notes]]`, []string{"knowledge", "writing", "productivity"}, 8)

	b9, _ := insertNote(bobID, "bob", "", "technical-debt-taxonomy", `Technical debt is not one thing. It has at least three distinct flavors:

**Deliberate**: we know this is messy, we chose speed over cleanliness, we'll fix it. This is often fine.

**Inadvertent**: we didn't know better at the time. This is just learning.

**Accumulated complexity**: the system grew in ways nobody planned, and now nobody fully understands it. This is the dangerous kind — it's not a debt you can pay off; it's an entanglement you have to untangle.

The distinction matters because the interventions are different. Deliberate debt: schedule the refactor. Accumulated complexity: rewrite or strangle.

Related: [[debugging-as-search]], [[@dave/on-abstraction]]`, []string{"programming", "software-engineering"}, 6)

	// ── Suppress unused variable warnings ─────────────────────────────────────
	_ = aStress
	_ = g1; _ = g2; _ = g3; _ = g4; _ = g5; _ = g6; _ = g7; _ = g8; _ = g9; _ = g10
	_ = h1; _ = h2; _ = h3; _ = h4; _ = h5; _ = h6; _ = h7; _ = h8; _ = h9; _ = h10
	_ = i1; _ = i2; _ = i3; _ = i4; _ = i5; _ = i6; _ = i7; _ = i8; _ = i9; _ = i10
	_ = j1; _ = j2; _ = j3; _ = j4; _ = j5; _ = j6; _ = j7; _ = j8; _ = j9; _ = j10
	_ = k1; _ = k2; _ = k3; _ = k4; _ = k5; _ = k6; _ = k7; _ = k8; _ = k9
	_ = l1; _ = l2; _ = l3; _ = l4; _ = l5; _ = l6; _ = l7; _ = l8; _ = l9; _ = l10
	_ = m1; _ = m2; _ = m3; _ = m4; _ = m5; _ = m6; _ = m7; _ = m8; _ = m9
	_ = n1; _ = n2; _ = n3; _ = n4; _ = n5; _ = n6; _ = n7; _ = n8; _ = n9
	_ = o1; _ = o2; _ = o3; _ = o4; _ = o5; _ = o6; _ = o7; _ = o8; _ = o9
	_ = pe1; _ = pe2; _ = pe3; _ = pe4; _ = pe5; _ = pe6; _ = pe7; _ = pe8
	_ = q1; _ = q2; _ = q3; _ = q4; _ = q5; _ = q6; _ = q7; _ = q8; _ = q9
	_ = r1; _ = r2; _ = r3; _ = r4; _ = r5; _ = r6; _ = r7; _ = r8; _ = r9
	_ = s1; _ = s2; _ = s3; _ = s4; _ = s5; _ = s6; _ = s7; _ = s8; _ = s9
	_ = t1; _ = t2; _ = t3; _ = t4; _ = t5; _ = t6; _ = t7; _ = t8; _ = t9; _ = t10
	_ = a7; _ = a8; _ = a9
	_ = b7; _ = b8; _ = b9
	_ = c5; _ = c6
	_ = d5; _ = d6
	_ = e4; _ = e5
	_ = f4; _ = f5

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

	// New users follow mix of existing + new
	follow(graceID, aliceID); follow(graceID, bobID); follow(graceID, ninaID)
	follow(henryID, daveID); follow(henryID, irisID); follow(henryID, petraID)
	follow(irisID, saraID); follow(irisID, frankID); follow(irisID, graceID)
	follow(jamesID, daveID); follow(jamesID, saraID); follow(jamesID, rafaelID)
	follow(kaiID, eveID); follow(kaiID, carolID); follow(kaiID, taroID)
	follow(lunaID, carolID); follow(lunaID, quinnID); follow(lunaID, irisID)
	follow(marcusID, carolID); follow(marcusID, quinnID); follow(marcusID, irisID)
	follow(ninaID, eveID); follow(ninaID, aliceID); follow(ninaID, taroID)
	follow(oscarID, kaiID); follow(oscarID, graceID); follow(oscarID, eveID)
	follow(petraID, aliceID); follow(petraID, jamesID); follow(petraID, quinnID)
	follow(quinnID, aliceID); follow(quinnID, bobID); follow(quinnID, marcusID)
	follow(rafaelID, bobID); follow(rafaelID, henryID); follow(rafaelID, jamesID)
	follow(saraID, irisID); follow(saraID, jamesID); follow(saraID, frankID)
	follow(taroID, frankID); follow(taroID, kaiID); follow(taroID, ninaID)
	// Some existing users follow new users back
	follow(aliceID, graceID); follow(aliceID, ninaID)
	follow(bobID, rafaelID); follow(carolID, kaiID)
	follow(daveID, saraID); follow(eveID, ninaID)
	follow(frankID, saraID); follow(frankID, taroID)

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

	// New users' likes — spread across existing + new notes
	like(graceID, aStress); like(graceID, g1); like(graceID, n1)
	like(henryID, aStress); like(henryID, h1); like(henryID, d1)
	like(irisID, aStress); like(irisID, s1); like(irisID, i1)
	like(jamesID, d4); like(jamesID, j1); like(jamesID, s2)
	like(kaiID, e3); like(kaiID, k1); like(kaiID, t1)
	like(lunaID, l1); like(lunaID, f1); like(lunaID, i2)
	like(marcusID, c3); like(marcusID, m1); like(marcusID, q1)
	like(ninaID, aStress); like(ninaID, n1); like(ninaID, e2)
	like(oscarID, aStress); like(oscarID, o1); like(oscarID, g4)
	like(petraID, a5); like(petraID, pe1); like(petraID, j3)
	like(quinnID, a2); like(quinnID, q1); like(quinnID, l6)
	like(rafaelID, b4); like(rafaelID, r1); like(rafaelID, h6)
	like(saraID, i4); like(saraID, s1); like(saraID, j4)
	like(taroID, f3); like(taroID, t1); like(taroID, k3)

	_ = a1; _ = a2; _ = a3; _ = a4; _ = a5; _ = a6
	_ = b1; _ = b2; _ = b3; _ = b4; _ = b5; _ = b6
	_ = c1; _ = c2; _ = c3; _ = c4
	_ = d1; _ = d2; _ = d3; _ = d4
	_ = e1; _ = e2; _ = e3
	_ = f1; _ = f2; _ = f3

	return tx.Commit()
}
