package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"

	"commonplace/internal/auth"
)

func postToggle(t *testing.T, s *Server, path string, uid, noteID uuid.UUID) *httptest.ResponseRecorder {
	t.Helper()
	form := url.Values{"note_id": {noteID.String()}}
	req := httptest.NewRequest("POST", path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: auth.SessionCookie, Value: s.Auth.Sign(uid)})
	rec := httptest.NewRecorder()
	switch path {
	case "/api/save":
		s.PostSave(rec, req)
	case "/api/like":
		s.PostLike(rec, req)
	}
	return rec
}

func savedCount(t *testing.T, s *Server, uid uuid.UUID) int {
	t.Helper()
	var n int
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM saves WHERE user_id=$1`, uid).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestSaveToggleIndependentOfLike(t *testing.T) {
	s := newTestServer(t)
	alice := mkUser(t, s, "alice")
	noteID, err := s.saveNote(context.Background(), alice, "alice", "n", "N", "body", nil)
	if err != nil {
		t.Fatalf("saveNote: %v", err)
	}

	// save toggles on
	if rec := postToggle(t, s, "/api/save", alice, noteID); rec.Code != http.StatusOK {
		t.Fatalf("save: got %d", rec.Code)
	}
	if got := savedCount(t, s, alice); got != 1 {
		t.Fatalf("after save: saves=%d, want 1", got)
	}

	// like must not create a save
	if rec := postToggle(t, s, "/api/like", alice, noteID); rec.Code != http.StatusOK {
		t.Fatalf("like: got %d", rec.Code)
	}
	if got := savedCount(t, s, alice); got != 1 {
		t.Fatalf("like changed saves to %d, want 1", got)
	}

	// save toggles off
	if rec := postToggle(t, s, "/api/save", alice, noteID); rec.Code != http.StatusOK {
		t.Fatalf("unsave: got %d", rec.Code)
	}
	if got := savedCount(t, s, alice); got != 0 {
		t.Fatalf("after unsave: saves=%d, want 0", got)
	}

	// like still stands (still independent) — likes table unaffected by unsave
	var likes int
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM likes WHERE user_id=$1`, alice).Scan(&likes); err != nil {
		t.Fatal(err)
	}
	if likes != 1 {
		t.Fatalf("likes=%d, want 1 (unsave must not touch likes)", likes)
	}
}
