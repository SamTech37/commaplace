package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"commonplace/internal/auth"
	"github.com/google/uuid"
)

func TestPostNoteImageEndpoint(t *testing.T) {
	s := newTestServer(t)
	ctx := context.Background()
	alice := mkUser(t, s, "alice")
	bob := mkUser(t, s, "bob")

	draft, err := s.createDraft(ctx, alice)
	if err != nil {
		t.Fatalf("createDraft: %v", err)
	}

	png := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, 'f', 'a', 'k', 'e'}
	post := func(userID uuid.UUID, filename, ctype string, body []byte) *httptest.ResponseRecorder {
		req := imageUploadRequest(t, filename, ctype, body)
		req.AddCookie(&http.Cookie{Name: auth.SessionCookie, Value: s.Auth.Sign(userID)})
		req.SetPathValue("id", draft.String())
		w := httptest.NewRecorder()
		s.PostNoteImage(w, req)
		return w
	}

	// Author uploads → 200 + URL.
	w := post(alice, "a.png", "image/png", png)
	if w.Code != http.StatusOK {
		t.Fatalf("upload: status %d body=%s", w.Code, w.Body)
	}
	if !strings.Contains(w.Body.String(), "/api/notes/"+draft.String()+"/image") {
		t.Fatalf("missing url in %s", w.Body)
	}
	if n := imageRowCount(t, s, draft); n != 1 {
		t.Fatalf("image rows = %d, want 1", n)
	}

	// Second upload replaces (still one row).
	png2 := append([]byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}, []byte("other")...)
	if w := post(alice, "b.png", "image/png", png2); w.Code != http.StatusOK {
		t.Fatalf("replace: status %d", w.Code)
	}
	if n := imageRowCount(t, s, draft); n != 1 {
		t.Fatalf("after replace rows = %d, want 1", n)
	}

	// Non-author → 403.
	if w := post(bob, "c.png", "image/png", png); w.Code != http.StatusForbidden {
		t.Fatalf("non-author status %d, want 403", w.Code)
	}

	// Unsupported type → 400 (fresh draft with no prior image).
	draft2, err := s.createDraft(ctx, alice)
	if err != nil {
		t.Fatalf("createDraft2: %v", err)
	}
	req := imageUploadRequest(t, "x.txt", "text/plain", []byte("hello world not an image"))
	req.AddCookie(&http.Cookie{Name: auth.SessionCookie, Value: s.Auth.Sign(alice)})
	req.SetPathValue("id", draft2.String())
	w2 := httptest.NewRecorder()
	s.PostNoteImage(w2, req)
	if w2.Code != http.StatusBadRequest {
		t.Fatalf("bad type status %d, want 400", w2.Code)
	}
}

func imageRowCount(t *testing.T, s *Server, noteID uuid.UUID) int {
	t.Helper()
	var n int
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM note_images WHERE note_id=$1`, noteID).Scan(&n); err != nil {
		t.Fatalf("count images: %v", err)
	}
	return n
}
