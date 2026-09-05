package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func publishedAt(t *testing.T, s *Server, id uuid.UUID) (val int64, valid bool) {
	t.Helper()
	var pa *int64
	if err := s.DB.QueryRow(`SELECT published_at FROM notes WHERE id = $1`, id).Scan(&pa); err != nil {
		t.Fatalf("publishedAt: %v", err)
	}
	if pa == nil {
		return 0, false
	}
	return *pa, true
}

func feedHasSlug(t *testing.T, s *Server, slug string) bool {
	t.Helper()
	cards, err := s.queryRecommendedCards(context.Background(), "", feedCursor{}, 50)
	if err != nil {
		t.Fatalf("queryRecommendedCards: %v", err)
	}
	for _, c := range cards {
		if strings.HasSuffix(c.URL, "/"+slug) {
			return true
		}
	}
	return false
}

func TestDraftLifecycle(t *testing.T) {
	s := newTestServer(t)
	ctx := context.Background()
	alice := mkUser(t, s, "alice")
	bob := mkUser(t, s, "bob")

	// A published note exists alongside the draft.
	if _, err := s.saveNote(ctx, alice, "alice", "pub", "Pub", "hello", nil); err != nil {
		t.Fatalf("saveNote: %v", err)
	}

	draftID, err := s.createDraft(ctx, alice)
	if err != nil {
		t.Fatalf("createDraft: %v", err)
	}
	if _, ok := publishedAt(t, s, draftID); ok {
		t.Fatal("fresh draft should have NULL published_at")
	}

	// Autosave content into the draft.
	w := httptest.NewRecorder()
	req := authedRequest(s, alice, http.MethodPatch, "/api/notes/"+draftID.String(),
		"document="+urlenc("My Draft\n\nsome body #topic"))
	req.SetPathValue("id", draftID.String())
	s.PatchNote(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PatchNote: status %d body=%s", w.Code, w.Body)
	}

	var title, body, slug string
	if err := s.DB.QueryRow(`SELECT title, body_md, slug FROM notes WHERE id=$1`, draftID).
		Scan(&title, &body, &slug); err != nil {
		t.Fatalf("scan draft: %v", err)
	}
	if title != "My Draft" {
		t.Fatalf("title = %q, want %q", title, "My Draft")
	}
	if body != "\nsome body #topic" {
		t.Fatalf("body = %q", body)
	}
	if slug != "my-draft" {
		t.Fatalf("slug = %q, want my-draft", slug)
	}
	if _, ok := publishedAt(t, s, draftID); ok {
		t.Fatal("draft must stay unpublished after autosave")
	}

	// Draft is hidden from public views.
	if feedHasSlug(t, s, "my-draft") {
		t.Fatal("draft leaked into feed")
	}
	if stats, _ := loadAuthorStats(ctx, s.DB, alice); stats.Notes != 1 {
		t.Fatalf("author note count = %d, want 1 (draft excluded)", stats.Notes)
	}

	// Owner sees own draft via drafts tab; stranger does not.
	ownerReq := authedRequest(s, alice, http.MethodGet, "/alice?tab=drafts", "")
	owned, _, _ := loadRecentNotes(ownerReq, s.DB, alice, "alice", alice, "drafts", feedCursor{})
	if !hasNoteTitle(owned, "My Draft") {
		t.Fatal("owner should see own draft on drafts tab")
	}
	strangerReq := authedRequest(s, bob, http.MethodGet, "/alice", "")
	seen, _, _ := loadRecentNotes(strangerReq, s.DB, alice, "alice", bob, "", feedCursor{})
	if hasNoteTitle(seen, "My Draft") {
		t.Fatal("stranger should not see alice's draft")
	}

	// GetNote: 404 for stranger, 200 for author.
	if code := getNoteCode(s, bob, "alice", "my-draft", false); code != http.StatusNotFound {
		t.Fatalf("stranger GetNote draft = %d, want 404", code)
	}
	if code := getNoteCode(s, alice, "alice", "my-draft", true); code != http.StatusOK {
		t.Fatalf("author GetNote draft = %d, want 200", code)
	}

	// Publish flips visibility.
	pw := httptest.NewRecorder()
	preq := authedRequest(s, alice, http.MethodPost, "/api/notes/"+draftID.String()+"/publish", "")
	preq.SetPathValue("id", draftID.String())
	s.PublishNote(pw, preq)
	if pw.Code != http.StatusOK {
		t.Fatalf("PublishNote: status %d body=%s", pw.Code, pw.Body)
	}
	if _, ok := publishedAt(t, s, draftID); !ok {
		t.Fatal("published_at should be set after publish")
	}
	if !feedHasSlug(t, s, "my-draft") {
		t.Fatal("published note should appear in feed")
	}
}

func TestSweepOrphanDrafts(t *testing.T) {
	s := newTestServer(t)
	ctx := context.Background()
	alice := mkUser(t, s, "alice")

	const day = int64(86400)
	now := nowUnix()

	// Helper to make a draft and force its created_at.
	mkDraft := func(title, body string, ageDays int64) uuid.UUID {
		id, err := s.createDraft(ctx, alice)
		if err != nil {
			t.Fatalf("createDraft: %v", err)
		}
		if _, err := s.DB.Exec(
			`UPDATE notes SET title=$1, body_md=$2, created_at=$3 WHERE id=$4`,
			title, body, now-ageDays*day, id); err != nil {
			t.Fatalf("backdate: %v", err)
		}
		return id
	}

	oldEmpty := mkDraft("", "", 8)  // should be swept
	newEmpty := mkDraft("", "", 6)  // spared (too new)
	oldFull := mkDraft("x", "y", 8) // spared (not empty)
	pub, err := s.saveNote(ctx, alice, "alice", "keep", "Keep", "body", nil)
	if err != nil {
		t.Fatalf("saveNote: %v", err)
	}
	if _, err := s.DB.Exec(`UPDATE notes SET created_at=$1 WHERE id=$2`, now-8*day, pub); err != nil {
		t.Fatalf("backdate pub: %v", err)
	}

	if err := s.sweepOrphanDrafts(ctx, alice); err != nil {
		t.Fatalf("sweep: %v", err)
	}

	exists := func(id uuid.UUID) bool {
		var n int
		s.DB.QueryRow(`SELECT COUNT(*) FROM notes WHERE id=$1`, id).Scan(&n)
		return n == 1
	}
	if exists(oldEmpty) {
		t.Error("old empty draft should be swept")
	}
	if !exists(newEmpty) {
		t.Error("recent empty draft should be spared")
	}
	if !exists(oldFull) {
		t.Error("old non-empty draft should be spared")
	}
	if !exists(pub) {
		t.Error("published note should be spared")
	}
}

// --- small helpers ---

func urlenc(s string) string {
	r := strings.NewReplacer("\n", "%0A", " ", "+", "#", "%23")
	return r.Replace(s)
}

func hasNoteTitle(notes []feedCard, title string) bool {
	for _, n := range notes {
		if n.Title == title {
			return true
		}
	}
	return false
}

// TestBulkDeleteDrafts: checkbox multi-select bulk-delete only soft-deletes
// the caller's own unpublished drafts — a published note or another author's
// note passed in the ids list must be left untouched.
func TestBulkDeleteDrafts(t *testing.T) {
	s := newTestServer(t)
	ctx := context.Background()
	alice := mkUser(t, s, "alice")
	bob := mkUser(t, s, "bob")

	draft1, err := s.createDraft(ctx, alice)
	if err != nil {
		t.Fatalf("createDraft: %v", err)
	}
	draft2, err := s.createDraft(ctx, alice)
	if err != nil {
		t.Fatalf("createDraft: %v", err)
	}
	pub, err := s.saveNote(ctx, alice, "alice", "pub", "Pub", "hello", nil)
	if err != nil {
		t.Fatalf("saveNote: %v", err)
	}
	bobDraft, err := s.createDraft(ctx, bob)
	if err != nil {
		t.Fatalf("createDraft: %v", err)
	}

	w := httptest.NewRecorder()
	req := authedRequest(s, alice, http.MethodPost, "/api/notes/bulk-delete",
		"ids="+draft1.String()+"&ids="+draft2.String()+"&ids="+pub.String()+"&ids="+bobDraft.String())
	s.PostBulkDeleteDrafts(w, req)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("PostBulkDeleteDrafts: status %d body=%s", w.Code, w.Body)
	}

	deletedAt := func(id uuid.UUID) bool {
		var da *int64
		if err := s.DB.QueryRow(`SELECT deleted_at FROM notes WHERE id=$1`, id).Scan(&da); err != nil {
			t.Fatalf("scan deleted_at: %v", err)
		}
		return da != nil
	}
	if !deletedAt(draft1) {
		t.Error("draft1 should be soft-deleted")
	}
	if !deletedAt(draft2) {
		t.Error("draft2 should be soft-deleted")
	}
	if deletedAt(pub) {
		t.Error("published note must NOT be deleted even if its id is in the request")
	}
	if deletedAt(bobDraft) {
		t.Error("bob's draft must NOT be deleted by alice's request")
	}
}

func getNoteCode(s *Server, viewer uuid.UUID, handle, slug string, authed bool) int {
	var req *http.Request
	if authed {
		req = authedRequest(s, viewer, http.MethodGet, "/"+handle+"/"+slug, "")
	} else {
		req = httptest.NewRequest(http.MethodGet, "/"+handle+"/"+slug, nil)
	}
	req.SetPathValue("user", handle)
	req.SetPathValue("slug", slug)
	w := httptest.NewRecorder()
	s.GetNote(w, req)
	return w.Code
}
