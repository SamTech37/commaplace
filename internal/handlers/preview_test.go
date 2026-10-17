package handlers

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// mkPreviewNote inserts a note directly so each test can set the exact
// published/hidden/deleted state it needs.
func mkPreviewNote(t *testing.T, s *Server, author uuid.UUID, slug, title, body, state string) {
	t.Helper()
	cols := `INSERT INTO notes(author_id, slug, slug_ci, title, body_md, created_at, updated_at, published_at`
	vals := `VALUES($1, $2, $2, $3, $4, 1, 1, 1`
	switch state {
	case "published":
	case "draft":
		vals = strings.Replace(vals, ", 1, 1, 1", ", 1, 1, NULL", 1)
	case "hidden":
		cols += `, hidden_at`
		vals += `, 2`
	case "deleted":
		cols += `, deleted_at`
		vals += `, 2`
	default:
		t.Fatalf("unknown state %q", state)
	}
	if _, err := s.DB.Exec(cols+`) `+vals+`)`, author, slug, title, body); err != nil {
		t.Fatalf("mkPreviewNote %s: %v", slug, err)
	}
}

func getPreview(t *testing.T, s *Server, user, slug string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("GET", "/api/preview/"+user+"/"+slug, nil)
	req.SetPathValue("user", user)
	req.SetPathValue("slug", slug)
	w := httptest.NewRecorder()
	s.GetNotePreview(w, req)
	return w
}

func TestPreviewPublishedNote(t *testing.T) {
	s := newTestServer(t)
	alice := mkUser(t, s, "alice")
	mkPreviewNote(t, s, alice, "intro", "Intro Note", "First paragraph of the note.", "published")

	w := getPreview(t, s, "alice", "intro")
	if w.Code != 200 {
		t.Fatalf("want 200, got %d", w.Code)
	}
	body := w.Body.String()
	for _, want := range []string{"Intro Note", "@alice", "First paragraph", `href="/alice/intro"`} {
		if !strings.Contains(body, want) {
			t.Errorf("preview missing %q:\n%s", want, body)
		}
	}
}

func TestPreviewHiddenStates(t *testing.T) {
	s := newTestServer(t)
	alice := mkUser(t, s, "alice")
	mkPreviewNote(t, s, alice, "d", "Draft", "body", "draft")
	mkPreviewNote(t, s, alice, "h", "Hidden", "body", "hidden")
	mkPreviewNote(t, s, alice, "x", "Deleted", "body", "deleted")

	for _, slug := range []string{"d", "h", "x", "nope"} {
		w := getPreview(t, s, "alice", slug)
		if w.Code != 404 {
			t.Errorf("slug %q: want 404, got %d", slug, w.Code)
		}
		if w.Body.Len() != 0 {
			t.Errorf("slug %q: want empty body, got %q", slug, w.Body.String())
		}
	}
	if w := getPreview(t, s, "ghost", "intro"); w.Code != 404 {
		t.Errorf("missing user: want 404, got %d", w.Code)
	}
}

// The preview must agree with the page it previews: GetNote matches u.handle
// case-sensitively, so a case-mismatched handle has no previewable page.
func TestPreviewHandleIsCaseSensitive(t *testing.T) {
	s := newTestServer(t)
	alice := mkUser(t, s, "alice")
	mkPreviewNote(t, s, alice, "intro", "Intro", "body", "published")

	if w := getPreview(t, s, "Alice", "intro"); w.Code != 404 {
		t.Errorf("want 404 for wrong-case handle, got %d", w.Code)
	}
}

// The preview rides the same feedCard model as the feed, so a bullet-list note
// previews as a list card, not a flat excerpt.
func TestPreviewUsesSharedCardVariant(t *testing.T) {
	s := newTestServer(t)
	alice := mkUser(t, s, "alice")
	mkPreviewNote(t, s, alice, "list", "Listy", "- one\n- two\n- three\n", "published")

	w := getPreview(t, s, "alice", "list")
	body := w.Body.String()
	if !strings.Contains(body, "variant-list") || !strings.Contains(body, "<li>one</li>") {
		t.Errorf("expected list variant card:\n%s", body)
	}
}

// ?h= previews just that section; an unknown section, or a draft, falls back
// to what the plain preview would do.
func TestPreviewSection(t *testing.T) {
	s := newTestServer(t)
	alice := mkUser(t, s, "alice")
	mkPreviewNote(t, s, alice, "doc", "Doc", "# A\nIN-A\n## A1\nIN-A1\n# B\nIN-B\n", "published")
	mkPreviewNote(t, s, alice, "draft", "Draft", "# A\nSECRET\n", "draft")

	get := func(slug, h string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", "/api/preview/alice/"+slug+"?h="+h, nil)
		req.SetPathValue("user", "alice")
		req.SetPathValue("slug", slug)
		w := httptest.NewRecorder()
		s.GetNotePreview(w, req)
		return w
	}

	body := get("doc", "A").Body.String()
	for _, want := range []string{"preview-section", "IN-A", "IN-A1", `href="/alice/doc"`} {
		if !strings.Contains(body, want) {
			t.Errorf("section preview missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "IN-B") {
		t.Errorf("section preview leaked the next section:\n%s", body)
	}

	if body := get("doc", "Nope").Body.String(); strings.Contains(body, "preview-section") || !strings.Contains(body, "masonry-card") {
		t.Errorf("unknown section should fall back to the card:\n%s", body)
	}
	if w := get("draft", "A"); w.Code != 404 || strings.Contains(w.Body.String(), "SECRET") {
		t.Errorf("draft section: want empty 404, got %d %q", w.Code, w.Body.String())
	}
}
