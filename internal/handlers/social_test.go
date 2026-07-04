package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"

	"commonplace/internal/auth"
	"commonplace/internal/seed"
)

func authedRequest(s *Server, userID uuid.UUID, method, target string, body string) *http.Request {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: auth.SessionCookie, Value: s.Auth.Sign(userID)})
	return req
}

// imageUploadRequest builds a POST request with a single multipart "image"
// field, declaring contentType (which the handler under test may or may not
// trust — that's the point of several tests below).
func imageUploadRequest(t *testing.T, filename, contentType string, body []byte) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", `form-data; name="image"; filename="`+filename+`"`)
	h.Set("Content-Type", contentType)
	part, err := mw.CreatePart(h)
	if err != nil {
		t.Fatalf("create part: %v", err)
	}
	part.Write(body)
	mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/write", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return req
}

func TestPostLikeToggle(t *testing.T) {
	s := newTestServer(t)
	ctx := context.Background()
	alice := mkUser(t, s, "alice")
	noteID, err := s.saveNote(ctx, alice, "alice", "n1", "N1", "body", nil)
	if err != nil {
		t.Fatalf("saveNote: %v", err)
	}

	w := httptest.NewRecorder()
	s.PostLike(w, authedRequest(s, alice, http.MethodPost, "/api/like", "note_id="+noteID.String()))
	if w.Code != http.StatusOK {
		t.Fatalf("like: status %d body=%s", w.Code, w.Body)
	}
	if !strings.Contains(w.Body.String(), "liked") {
		t.Fatalf("expected liked fragment, got %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `<span class="count">1</span>`) {
		t.Fatalf("expected count 1, got %s", w.Body.String())
	}

	w2 := httptest.NewRecorder()
	s.PostLike(w2, authedRequest(s, alice, http.MethodPost, "/api/like", "note_id="+noteID.String()))
	if strings.Contains(w2.Body.String(), "liked") {
		t.Fatalf("expected unliked fragment, got %s", w2.Body.String())
	}
	if !strings.Contains(w2.Body.String(), `<span class="count">0</span>`) {
		t.Fatalf("expected count 0, got %s", w2.Body.String())
	}
}

func TestPostFollowToggle(t *testing.T) {
	s := newTestServer(t)
	alice := mkUser(t, s, "alice")
	bob := mkUser(t, s, "bob")

	w := httptest.NewRecorder()
	s.PostFollow(w, authedRequest(s, alice, http.MethodPost, "/api/follow", "user_id="+bob.String()))
	if w.Code != http.StatusOK {
		t.Fatalf("follow: status %d body=%s", w.Code, w.Body)
	}
	if !strings.Contains(w.Body.String(), "following") {
		t.Fatalf("expected following fragment, got %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "1 follower<") {
		t.Fatalf("expected '1 follower', got %s", w.Body.String())
	}

	w2 := httptest.NewRecorder()
	s.PostFollow(w2, authedRequest(s, alice, http.MethodPost, "/api/follow", "user_id="+bob.String()))
	if strings.Contains(w2.Body.String(), "following") {
		t.Fatalf("expected unfollowed fragment, got %s", w2.Body.String())
	}
	if !strings.Contains(w2.Body.String(), "0 followers<") {
		t.Fatalf("expected '0 followers', got %s", w2.Body.String())
	}
}

// TestSearchFindsNote is a regression test for a join-precedence bug: the
// original query used "FROM notes n, to_tsquery(...) query JOIN users u ON
// u.id = n.author_id", which makes "n" invisible to the explicit JOIN's ON
// clause and fails every search with a SQL error swallowed by the caller.
func TestSearchFindsNote(t *testing.T) {
	s := newTestServer(t)
	ctx := context.Background()
	alice := mkUser(t, s, "alice")
	if _, err := s.saveNote(ctx, alice, "alice", "n1", "Search Me", "a unique zzzqux term here", nil); err != nil {
		t.Fatalf("saveNote: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/search?q=zzzqux", nil)
	w := httptest.NewRecorder()
	s.GetSearch(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("search: status %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "<mark>zzzqux</mark>") {
		t.Fatalf("expected <mark>zzzqux</mark> in body, got: %s", w.Body.String())
	}
}

// TestSearchCrossScript: a query in Simplified Chinese must match Traditional
// content and vice versa (OpenCC query expansion in buildTSQueryVariants).
func TestSearchCrossScript(t *testing.T) {
	s := newTestServer(t)
	ctx := context.Background()
	alice := mkUser(t, s, "alice")
	if _, err := s.saveNote(ctx, alice, "alice", "n1", "數論", "數論是研究整數性質的學問", nil); err != nil {
		t.Fatalf("saveNote: %v", err)
	}
	if _, err := s.saveNote(ctx, alice, "alice", "n2", "机器学习", "机器学习是人工智能的分支", nil); err != nil {
		t.Fatalf("saveNote: %v", err)
	}

	for _, tc := range []struct{ query, wantTitle string }{
		{"数论", "數論"},       // Simplified query → Traditional note
		{"數論", "數論"},       // same-script still works
		{"機器學習", "机器学习"}, // Traditional query → Simplified note
	} {
		req := httptest.NewRequest(http.MethodGet, "/search?q="+url.QueryEscape(tc.query), nil)
		w := httptest.NewRecorder()
		s.GetSearch(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("search %q: status %d", tc.query, w.Code)
		}
		if !strings.Contains(w.Body.String(), tc.wantTitle) {
			t.Errorf("search %q: expected result titled %q, got: %s", tc.query, tc.wantTitle, w.Body.String())
		}
	}
}

func TestSearchNoMatch(t *testing.T) {
	s := newTestServer(t)
	ctx := context.Background()
	alice := mkUser(t, s, "alice")
	if _, err := s.saveNote(ctx, alice, "alice", "n1", "Search Me", "some unrelated body", nil); err != nil {
		t.Fatalf("saveNote: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/search?q=nosuchtermanywhere", nil)
	w := httptest.NewRecorder()
	s.GetSearch(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("search: status %d", w.Code)
	}
	if strings.Contains(w.Body.String(), "<mark>") {
		t.Fatalf("expected no matches, got: %s", w.Body.String())
	}
}

func TestGraphDataAndLocal(t *testing.T) {
	s := newTestServer(t)
	ctx := context.Background()
	alice := mkUser(t, s, "alice")
	bob := mkUser(t, s, "bob")

	fooID, err := s.saveNote(ctx, bob, "bob", "foo", "Foo", "", nil)
	if err != nil {
		t.Fatalf("saveNote bob/foo: %v", err)
	}
	introID, err := s.saveNote(ctx, alice, "alice", "intro", "Intro", "See [[@bob/foo]].", nil)
	if err != nil {
		t.Fatalf("saveNote alice/intro: %v", err)
	}

	w := httptest.NewRecorder()
	s.GetGraphData(w, httptest.NewRequest(http.MethodGet, "/api/graph", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("graph: status %d", w.Code)
	}
	var payload graphPayload
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode graph: %v", err)
	}
	if len(payload.Nodes) != 2 {
		t.Fatalf("nodes = %d, want 2: %+v", len(payload.Nodes), payload.Nodes)
	}
	found := false
	for _, e := range payload.Edges {
		if e.Source == introID.String() && e.Target == fooID.String() {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected edge intro->foo, got %+v", payload.Edges)
	}
	for _, n := range payload.Nodes {
		if strings.Contains(n.ID, ":") {
			t.Fatalf("node id %q should be a bare UUID, no prefix", n.ID)
		}
	}

	w2 := httptest.NewRecorder()
	s.GetGraphLocal(w2, httptest.NewRequest(http.MethodGet, "/api/graph/local?note="+fooID.String(), nil))
	if w2.Code != http.StatusOK {
		t.Fatalf("graph local: status %d body=%s", w2.Code, w2.Body)
	}
	var local struct {
		graphPayload
		Center string `json:"center"`
	}
	if err := json.Unmarshal(w2.Body.Bytes(), &local); err != nil {
		t.Fatalf("decode graph local: %v", err)
	}
	if local.Center != fooID.String() {
		t.Fatalf("center = %s, want %s", local.Center, fooID.String())
	}
	if len(local.Nodes) != 2 {
		t.Fatalf("local nodes = %d, want 2: %+v", len(local.Nodes), local.Nodes)
	}
}

func TestForkTourCopiesNotesAndPinsWelcome(t *testing.T) {
	s := newTestServer(t)
	ctx := context.Background()
	carol := mkUser(t, s, "carol")

	welcomeID, err := s.forkTour(ctx, carol, "carol")
	if err != nil {
		t.Fatalf("forkTour: %v", err)
	}
	if welcomeID == uuid.Nil {
		t.Fatal("expected non-nil welcome id")
	}

	wantCount := 0
	for _, n := range seed.TourNotes {
		if n.Author == seed.TourHandle {
			wantCount++
		}
	}
	var count int
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM notes WHERE author_id = $1`, carol).Scan(&count); err != nil {
		t.Fatalf("count notes: %v", err)
	}
	if count != wantCount {
		t.Fatalf("forked notes = %d, want %d (MakerHandle notes must not be copied)", count, wantCount)
	}

	var slug string
	if err := s.DB.QueryRow(`SELECT slug FROM notes WHERE id = $1`, welcomeID).Scan(&slug); err != nil {
		t.Fatalf("lookup welcome note: %v", err)
	}
	if slug != "welcome" {
		t.Fatalf("welcome slug = %q, want %q", slug, "welcome")
	}
}

func TestPostOnboardingForkPinsWelcomeAndMarksOnboarded(t *testing.T) {
	s := newTestServer(t)
	dave := mkUser(t, s, "dave")

	req := authedRequest(s, dave, http.MethodPost, "/onboarding/fork", "")
	w := httptest.NewRecorder()
	s.PostOnboardingFork(w, req)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("fork: status %d, want %d", w.Code, http.StatusSeeOther)
	}

	var pinnedSlug string
	var onboardedAt int64
	if err := s.DB.QueryRow(`
		SELECT n.slug, u.onboarded_at FROM users u
		JOIN notes n ON n.id = u.pinned_note_id
		WHERE u.id = $1`, dave,
	).Scan(&pinnedSlug, &onboardedAt); err != nil {
		t.Fatalf("lookup pinned note: %v", err)
	}
	if pinnedSlug != "welcome" {
		t.Fatalf("pinned slug = %q, want %q", pinnedSlug, "welcome")
	}
	if onboardedAt == 0 {
		t.Fatal("expected onboarded_at to be set")
	}
}

func TestNoteImageUploadAndFetch(t *testing.T) {
	s := newTestServer(t)
	ctx := context.Background()
	alice := mkUser(t, s, "alice")
	noteID, err := s.saveNote(ctx, alice, "alice", "n1", "N1", "body", nil)
	if err != nil {
		t.Fatalf("saveNote: %v", err)
	}

	pngBytes := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, 'f', 'a', 'k', 'e'}
	uploadReq := imageUploadRequest(t, "test.png", "image/png", pngBytes)
	if err := s.saveNoteImage(uploadReq, noteID); err != nil {
		t.Fatalf("saveNoteImage: %v", err)
	}

	if !noteHasImage(uploadReq, s.DB, noteID) {
		t.Fatal("expected noteHasImage = true")
	}

	getReq := httptest.NewRequest(http.MethodGet, "/api/notes/"+noteID.String()+"/image", nil)
	getReq.SetPathValue("id", noteID.String())
	w := httptest.NewRecorder()
	s.GetNoteImage(w, getReq)
	if w.Code != http.StatusOK {
		t.Fatalf("get image: status %d body=%s", w.Code, w.Body)
	}
	if ct := w.Header().Get("Content-Type"); ct != "image/png" {
		t.Fatalf("content-type = %q, want image/png", ct)
	}
	if !bytes.Equal(w.Body.Bytes(), pngBytes) {
		t.Fatalf("image bytes mismatch: got %v, want %v", w.Body.Bytes(), pngBytes)
	}
}

func TestNoteImageRejectsDisallowedType(t *testing.T) {
	s := newTestServer(t)
	ctx := context.Background()
	alice := mkUser(t, s, "alice")
	noteID, err := s.saveNote(ctx, alice, "alice", "n1", "N1", "body", nil)
	if err != nil {
		t.Fatalf("saveNote: %v", err)
	}

	req := imageUploadRequest(t, "evil.svg", "image/svg+xml", []byte("<svg></svg>"))
	if err := s.saveNoteImage(req, noteID); err != nil {
		t.Fatalf("saveNoteImage: %v", err)
	}
	if noteHasImage(req, s.DB, noteID) {
		t.Fatal("disallowed content type should not be stored")
	}
}

// TestNoteImageRejectsSpoofedContentType proves saveNoteImage sniffs actual
// bytes rather than trusting the attacker-controlled declared Content-Type —
// a part declaring image/png but containing HTML must be rejected.
func TestNoteImageRejectsSpoofedContentType(t *testing.T) {
	s := newTestServer(t)
	ctx := context.Background()
	alice := mkUser(t, s, "alice")
	noteID, err := s.saveNote(ctx, alice, "alice", "n1", "N1", "body", nil)
	if err != nil {
		t.Fatalf("saveNote: %v", err)
	}

	req := imageUploadRequest(t, "evil.png", "image/png", []byte("<html><script>alert(1)</script></html>"))
	if err := s.saveNoteImage(req, noteID); err != nil {
		t.Fatalf("saveNoteImage: %v", err)
	}
	if noteHasImage(req, s.DB, noteID) {
		t.Fatal("spoofed Content-Type with non-image bytes should not be stored")
	}
}

// TestNoteImageHiddenGate is a regression test: GetNoteImage must mirror
// GetNote's visibility rules. Before this fix it served images for hidden
// and deleted notes to any viewer holding the note's UUID.
func TestNoteImageHiddenGate(t *testing.T) {
	s := newTestServer(t)
	ctx := context.Background()
	alice := mkUser(t, s, "alice")
	noteID, err := s.saveNote(ctx, alice, "alice", "n1", "N1", "body", nil)
	if err != nil {
		t.Fatalf("saveNote: %v", err)
	}
	pngBytes := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, 'f', 'a', 'k', 'e'}
	uploadReq := imageUploadRequest(t, "test.png", "image/png", pngBytes)
	if err := s.saveNoteImage(uploadReq, noteID); err != nil {
		t.Fatalf("saveNoteImage: %v", err)
	}

	if _, err := s.DB.Exec(`UPDATE notes SET hidden_at = $1 WHERE id = $2`, nowUnix(), noteID); err != nil {
		t.Fatalf("hide note: %v", err)
	}

	anonReq := httptest.NewRequest(http.MethodGet, "/api/notes/"+noteID.String()+"/image", nil)
	anonReq.SetPathValue("id", noteID.String())
	w := httptest.NewRecorder()
	s.GetNoteImage(w, anonReq)
	if w.Code != http.StatusNotFound {
		t.Fatalf("anon fetch of hidden note's image: status %d, want 404", w.Code)
	}

	authorReq := httptest.NewRequest(http.MethodGet, "/api/notes/"+noteID.String()+"/image", nil)
	authorReq.SetPathValue("id", noteID.String())
	authorReq.AddCookie(&http.Cookie{Name: auth.SessionCookie, Value: s.Auth.Sign(alice)})
	w2 := httptest.NewRecorder()
	s.GetNoteImage(w2, authorReq)
	if w2.Code != http.StatusOK {
		t.Fatalf("author fetch of own hidden note's image: status %d, want 200", w2.Code)
	}

	if _, err := s.DB.Exec(`UPDATE notes SET hidden_at = NULL, deleted_at = $1 WHERE id = $2`, nowUnix(), noteID); err != nil {
		t.Fatalf("delete note: %v", err)
	}
	w3 := httptest.NewRecorder()
	s.GetNoteImage(w3, authorReq)
	if w3.Code != http.StatusNotFound {
		t.Fatalf("fetch of deleted note's image: status %d, want 404", w3.Code)
	}
}
