package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"commonplace/internal/markdown"
)

// TestStubBackfillThroughRealEditFlow guards the bug where a stub link (target
// note doesn't exist yet) never resolved if the target was created through the
// real editor UI (createDraft -> PATCH autosave -> POST publish) instead of
// saveNote (only reachable via /write or import). autosaveNote must backfill
// stubs the same way saveNote does.
func TestStubBackfillThroughRealEditFlow(t *testing.T) {
	s := newTestServer(t)
	ctx := context.Background()
	alice := mkUser(t, s, "alice")
	bob := mkUser(t, s, "bob")

	if _, err := s.saveNote(ctx, alice, "alice", "intro", "Intro", "see [[@bob/futurenote]]", nil); err != nil {
		t.Fatalf("save alice/intro: %v", err)
	}
	if r, u := countResolved(t, s); r != 0 || u != 1 {
		t.Fatalf("after intro: resolved=%d unresolved=%d, want 0/1", r, u)
	}

	draftID, err := s.createDraft(ctx, bob)
	if err != nil {
		t.Fatalf("createDraft: %v", err)
	}
	w := httptest.NewRecorder()
	req := authedRequest(s, bob, http.MethodPatch, "/api/notes/"+draftID.String(),
		"document="+urlenc("Futurenote\n\nhello"))
	req.SetPathValue("id", draftID.String())
	s.PatchNote(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PatchNote: status %d body=%s", w.Code, w.Body)
	}

	pw := httptest.NewRecorder()
	preq := authedRequest(s, bob, http.MethodPost, "/api/notes/"+draftID.String()+"/publish", "")
	preq.SetPathValue("id", draftID.String())
	s.PublishNote(pw, preq)
	if pw.Code != http.StatusOK {
		t.Fatalf("PublishNote: status %d body=%s", pw.Code, pw.Body)
	}

	if r, u := countResolved(t, s); r != 1 || u != 0 {
		t.Fatalf("after bob publishes futurenote via the real edit flow: resolved=%d unresolved=%d, want 1/0", r, u)
	}
}

// TestCrossVaultRenameSafety mirrors TestRenameFollowsInboundLink but across
// two vaults: a renamed note's cross-vault inbound link must still resolve
// (via resolved_target_id, not the stale slug string) and stay correctly
// classified as a cross-vault outgoing link.
func TestCrossVaultRenameSafety(t *testing.T) {
	s := newTestServer(t)
	ctx := context.Background()
	alice := mkUser(t, s, "alice")
	bob := mkUser(t, s, "bob")

	target, err := s.saveNote(ctx, bob, "bob", "target", "Target", "hi", nil)
	if err != nil {
		t.Fatalf("save bob/target: %v", err)
	}
	src, err := s.saveNote(ctx, alice, "alice", "src", "Src", "see [[@bob/target]]", nil)
	if err != nil {
		t.Fatalf("save alice/src: %v", err)
	}

	if _, err := s.DB.Exec(
		`UPDATE notes SET slug='renamed', slug_ci='renamed' WHERE id=$1`, target,
	); err != nil {
		t.Fatalf("rename: %v", err)
	}

	resolver := s.buildResolverForNote(ctx, "alice", src)
	rt := resolver(markdown.WikiLink{User: "bob", Slug: "target"})
	if rt == nil || rt.Handle != "bob" || rt.Slug != "renamed" {
		t.Fatalf("cross-vault resolve after rename = %+v, want handle=bob slug=renamed", rt)
	}

	same, cross, err := s.loadOutgoingSplit(ctx, src, "alice")
	if err != nil {
		t.Fatalf("loadOutgoingSplit: %v", err)
	}
	if len(same) != 0 {
		t.Fatalf("same = %+v, want 0", same)
	}
	if len(cross) != 1 || cross[0].URL != "/bob/renamed" {
		t.Fatalf("cross = %+v, want one entry to /bob/renamed", cross)
	}
}

// TestBacklinkVisibilityFiltering: loadBacklinksSplit must stop surfacing a
// source note once it's hidden or soft-deleted; loadOutgoingSplit must not
// surface a target that has become an unpublished draft.
func TestBacklinkVisibilityFiltering(t *testing.T) {
	s := newTestServer(t)
	ctx := context.Background()
	alice := mkUser(t, s, "alice")
	bob := mkUser(t, s, "bob")

	target, err := s.saveNote(ctx, bob, "bob", "target", "Target", "hi", nil)
	if err != nil {
		t.Fatalf("save target: %v", err)
	}
	src, err := s.saveNote(ctx, alice, "alice", "src", "Src", "see [[@bob/target]]", nil)
	if err != nil {
		t.Fatalf("save src: %v", err)
	}

	if _, cross, err := s.loadBacklinksSplit(ctx, target, "bob"); err != nil {
		t.Fatalf("loadBacklinksSplit: %v", err)
	} else if len(cross) != 1 {
		t.Fatalf("cross = %+v, want 1 before hiding source", cross)
	}

	if _, err := s.DB.Exec(`UPDATE notes SET hidden_at=1 WHERE id=$1`, src); err != nil {
		t.Fatalf("hide src: %v", err)
	}
	if _, cross, err := s.loadBacklinksSplit(ctx, target, "bob"); err != nil {
		t.Fatalf("loadBacklinksSplit: %v", err)
	} else if len(cross) != 0 {
		t.Fatalf("cross = %+v, want 0 after hiding source", cross)
	}

	if _, err := s.DB.Exec(`UPDATE notes SET hidden_at=NULL, deleted_at=1 WHERE id=$1`, src); err != nil {
		t.Fatalf("delete src: %v", err)
	}
	if _, cross, err := s.loadBacklinksSplit(ctx, target, "bob"); err != nil {
		t.Fatalf("loadBacklinksSplit: %v", err)
	} else if len(cross) != 0 {
		t.Fatalf("cross = %+v, want 0 after deleting source", cross)
	}

	if _, err := s.DB.Exec(`UPDATE notes SET hidden_at=NULL, deleted_at=NULL WHERE id=$1`, src); err != nil {
		t.Fatalf("restore src: %v", err)
	}
	if _, err := s.DB.Exec(`UPDATE notes SET published_at=NULL WHERE id=$1`, target); err != nil {
		t.Fatalf("unpublish target: %v", err)
	}
	same, cross, err := s.loadOutgoingSplit(ctx, src, "alice")
	if err != nil {
		t.Fatalf("loadOutgoingSplit: %v", err)
	}
	if len(same) != 0 || len(cross) != 0 {
		t.Fatalf("outgoing = same:%+v cross:%+v, want both empty once target is an unpublished draft", same, cross)
	}
}

// TestBacklinkFanIn: multiple sources (same-vault and cross-vault, mixed)
// linking to one target must all show up, correctly split.
func TestBacklinkFanIn(t *testing.T) {
	s := newTestServer(t)
	ctx := context.Background()
	alice := mkUser(t, s, "alice")
	bob := mkUser(t, s, "bob")
	carol := mkUser(t, s, "carol")

	target, err := s.saveNote(ctx, bob, "bob", "target", "Target", "hi", nil)
	if err != nil {
		t.Fatalf("save target: %v", err)
	}
	if _, err := s.saveNote(ctx, alice, "alice", "a", "A", "see [[@bob/target]]", nil); err != nil {
		t.Fatalf("save alice/a: %v", err)
	}
	if _, err := s.saveNote(ctx, carol, "carol", "c", "C", "see [[@bob/target]]", nil); err != nil {
		t.Fatalf("save carol/c: %v", err)
	}
	if _, err := s.saveNote(ctx, bob, "bob", "own", "Own", "see [[target]]", nil); err != nil {
		t.Fatalf("save bob/own: %v", err)
	}

	same, cross, err := s.loadBacklinksSplit(ctx, target, "bob")
	if err != nil {
		t.Fatalf("loadBacklinksSplit: %v", err)
	}
	if len(same) != 1 || same[0].Title != "Own" {
		t.Fatalf("same = %+v, want one entry titled Own", same)
	}
	if len(cross) != 2 {
		t.Fatalf("cross = %+v, want 2 entries", cross)
	}
	seen := map[string]bool{}
	for _, bl := range cross {
		seen[bl.AuthorHandle] = true
	}
	if !seen["alice"] || !seen["carol"] {
		t.Fatalf("cross authors = %+v, want alice and carol both present", cross)
	}
}

// TestCJKTitleSlugRoundTrip documents/guards the expected behavior: a note
// created through the normal write flow with a CJK-only title has slug ==
// title (kebabSlug treats Han ideographs as letters), so its percent-encoded
// URL must resolve. (A hand-set slug that diverges from a CJK title, as seed
// data intentionally does for one demo note, is a separate, unrelated case —
// not what this test covers.)
func TestCJKTitleSlugRoundTrip(t *testing.T) {
	s := newTestServer(t)
	ctx := context.Background()
	alice := mkUser(t, s, "alice")

	if _, err := s.saveNote(ctx, alice, "alice", "日記", "日記", "hello", nil); err != nil {
		t.Fatalf("save: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/alice/%E6%97%A5%E8%A8%98", nil)
	w := httptest.NewRecorder()
	s.GetCatchAll(http.NotFoundHandler())(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("percent-encoded CJK slug URL: status %d body=%s", w.Code, w.Body)
	}
}

// TestEmojiOnlyTitlePublish is the regression guard for bug #3: an
// emoji/punctuation-only title (kebabSlug returns "") must still get a
// resolvable, non-"draft-"-prefixed slug so PublishNote's guard doesn't block
// it forever.
func TestEmojiOnlyTitlePublish(t *testing.T) {
	s := newTestServer(t)
	ctx := context.Background()
	alice := mkUser(t, s, "alice")

	draftID, err := s.createDraft(ctx, alice)
	if err != nil {
		t.Fatalf("createDraft: %v", err)
	}

	w := httptest.NewRecorder()
	req := authedRequest(s, alice, http.MethodPatch, "/api/notes/"+draftID.String(),
		"document="+urlenc("🎉🎊\n\nbody"))
	req.SetPathValue("id", draftID.String())
	s.PatchNote(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PatchNote: status %d body=%s", w.Code, w.Body)
	}

	var slug string
	if err := s.DB.QueryRow(`SELECT slug FROM notes WHERE id=$1`, draftID).Scan(&slug); err != nil {
		t.Fatalf("scan slug: %v", err)
	}
	if strings.HasPrefix(slug, "draft-") {
		t.Fatalf("slug = %q, still has draft- prefix after an emoji-only title patch", slug)
	}

	pw := httptest.NewRecorder()
	preq := authedRequest(s, alice, http.MethodPost, "/api/notes/"+draftID.String()+"/publish", "")
	preq.SetPathValue("id", draftID.String())
	s.PublishNote(pw, preq)
	if pw.Code != http.StatusOK {
		t.Fatalf("PublishNote with emoji-only title: status %d body=%s", pw.Code, pw.Body)
	}
}

// TestStubSurvivesTargetRename guards the hole the shawn -> shawn_demo prod
// rename exposed: links recorded the target's *handle*, so renaming a user
// stranded every inbound stub addressed to the old name — backfillStubLinks
// could never match again. Identity is the user's uuid; the handle is a label.
func TestStubSurvivesTargetRename(t *testing.T) {
	s := newTestServer(t)
	ctx := context.Background()
	alice := mkUser(t, s, "alice")
	bob := mkUser(t, s, "bob")

	// alice points at a note bob has not written yet -> one stub link.
	if _, err := s.saveNote(ctx, alice, "alice", "intro", "Intro", "see [[@bob/futurenote]]", nil); err != nil {
		t.Fatalf("save alice/intro: %v", err)
	}
	if r, u := countResolved(t, s); r != 0 || u != 1 {
		t.Fatalf("after intro: resolved=%d unresolved=%d, want 0/1", r, u)
	}

	// bob renames himself through the real endpoint. Same row, same uuid.
	if rec := postHandle(t, s, bob, "bobby"); rec.Code != http.StatusSeeOther {
		t.Fatalf("rename bob->bobby: status %d body=%s", rec.Code, rec.Body)
	}

	// bob writes the note alice was waiting on. The stub must resolve.
	if _, err := s.saveNote(ctx, bob, "bobby", "futurenote", "Futurenote", "hello", nil); err != nil {
		t.Fatalf("save bobby/futurenote: %v", err)
	}
	if r, u := countResolved(t, s); r != 1 || u != 0 {
		t.Fatalf("after the renamed user creates the target: resolved=%d unresolved=%d, want 1/0", r, u)
	}
}
